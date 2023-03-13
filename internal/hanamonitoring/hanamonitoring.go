/*
Copyright 2023 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package hanamonitoring queries HANA databases and sends the results as metrics to Cloud Monitoring.
package hanamonitoring

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

type gceInterface interface {
	GetSecret(ctx context.Context, projectID, secretName string) (string, error)
}

// queryFunc provides an easily testable translation to the SQL API.
type queryFunc func(ctx context.Context, query string, args ...any) (*sql.Rows, error)

// Parameters hold the parameters necessary to invoke Start().
type Parameters struct {
	Config     *cpb.Configuration
	GCEService gceInterface
}

// database holds the relevant information for querying and debugging the database.
type database struct {
	queryFunc queryFunc
	instance  *cpb.HANAInstance
}

// Start begins the query goroutines if enabled.
// Returns true if the query goroutines are started, and false otherwise.
func Start(ctx context.Context, params Parameters) bool {
	cfg := params.Config.GetHanaMonitoringConfiguration()
	if !cfg.GetEnabled() {
		log.Logger.Info("HANA Monitoring disabled, not starting HANA Monitoring.")
		return false
	}
	if len(cfg.GetQueries()) == 0 {
		log.Logger.Info("HANA Monitoring enabled but no queries defined, not starting HANA Monitoring.")
		usagemetrics.Error(usagemetrics.MalformedConfigFile)
		return false
	}
	databases := connectToDatabases(ctx, params)
	if len(databases) == 0 {
		log.Logger.Info("No HANA databases to query, not starting HANA Monitoring.")
		usagemetrics.Error(usagemetrics.HANAMonitoringCollectionFailure)
		return false
	}

	log.Logger.Info("Starting HANA Monitoring.")
	usagemetrics.Error(usagemetrics.CollectHANAMonitoringMetrics)
	for _, db := range databases {
		for _, query := range params.Config.GetHanaMonitoringConfiguration().GetQueries() {
			sampleInterval := cfg.GetSampleIntervalSec()
			if query.GetSampleIntervalSec() >= 5 {
				sampleInterval = query.GetSampleIntervalSec()
			}
			go queryAndSend(ctx, db, query, cfg.GetQueryTimeoutSec(), sampleInterval)
		}
	}
	return true
}

// queryAndSend runs the perpetual querying and sending of results to cloud monitoring workflow.
// If any errors occur during query or send, they are logged and the workflow continues to try again next sampleInterval.
// For unit testing, the caller can cancel the context to terminate the workflow which will return the last error seen or nil if no errors occurred.
func queryAndSend(ctx context.Context, db *database, query *cpb.Query, timeout, sampleInterval int64) error {
	var lastErr error
	user, host, port, queryName := db.instance.GetUser(), db.instance.GetHost(), db.instance.GetPort(), query.GetName()
	for {
		select {
		case <-ctx.Done():
			log.Logger.Info("Context cancelled, exiting queryAndSend.")
			return lastErr
		default:
			ctxTimeout, cancel := context.WithTimeout(ctx, time.Second*time.Duration(timeout))
			sent, batchCount, err := queryAndSendOnce(ctxTimeout, db, query)
			if err != nil {
				log.Logger.Errorw("Error querying database or sending metrics", "user", user, "host", host, "port", port, "query", queryName, "error", err)
				usagemetrics.Error(usagemetrics.HANAMonitoringCollectionFailure)
				lastErr = err
			}
			log.Logger.Debugw("Sent metrics from queryAndSend.", "user", user, "host", host, "port", port, "query", queryName, "sent", sent, "batches", batchCount, "sleeping", sampleInterval)
			cancel()
			time.Sleep(time.Duration(sampleInterval) * time.Second)
		}
	}
}

// queryAndSendOnce queries the database, packages the results into time series, and sends those results as metrics to cloud monitoring.
func queryAndSendOnce(ctx context.Context, db *database, query *cpb.Query) (sent, batchCount int64, err error) {
	rows, cols, err := queryDatabase(ctx, db.queryFunc, query)
	if err != nil {
		return 0, 0, err
	}

	for rows.Next() {
		if err := rows.Scan(cols...); err != nil {
			return 0, 0, err
		}
		// TODO: Package and send results to cloud monitoring.
	}
	return 0, 0, nil
}

// createColumns creates pointers to the types defined in the configuration for each column in a query.
func createColumns(queryColumns []*cpb.Column) []any {
	if len(queryColumns) == 0 {
		return nil
	}
	cols := make([]any, len(queryColumns))
	for i, c := range queryColumns {
		var col any
		switch c.GetValueType() {
		case cpb.ValueType_VALUE_INT64:
			col = new(int64)
		case cpb.ValueType_VALUE_DOUBLE:
			col = new(float64)
		case cpb.ValueType_VALUE_BOOL:
			col = new(bool)
		case cpb.ValueType_VALUE_STRING:
			col = new(string)
		default:
			// Rows.Scan() is able to populate any cell as *interface{} by "copying the value provided by the underlying driver without conversion". https://pkg.go.dev/database/sql#Rows.Scan
			col = new(any)
		}
		cols[i] = col
	}
	return cols
}

// queryDatabase attempts to execute the specified query, returning a sql.Rows iterator and a slice for storing the column results of each row.
func queryDatabase(ctx context.Context, queryFunc queryFunc, query *cpb.Query) (*sql.Rows, []any, error) {
	if query == nil {
		return nil, nil, errors.New("no query specified")
	}
	cols := createColumns(query.GetColumns())
	if cols == nil {
		return nil, nil, errors.New("no columns specified")
	}
	rows, err := queryFunc(ctx, query.GetSql())
	if err != nil {
		return nil, nil, err
	}
	return rows, cols, nil
}

// connectToDatabases attempts to create a *sql.DB connection for each HANAInstance.
func connectToDatabases(ctx context.Context, params Parameters) []*database {
	var databases []*database
	for _, i := range params.Config.GetHanaMonitoringConfiguration().GetHanaInstances() {
		password := i.GetPassword()
		if password == "" && i.GetSecretName() == "" {
			log.Logger.Errorf("Both password and secret name are empty, cannot connect to database %s", i.GetName())
			continue
		}

		if password == "" && i.GetSecretName() != "" {
			if secret, err := params.GCEService.GetSecret(ctx, params.Config.GetCloudProperties().GetProjectId(), i.GetSecretName()); err == nil {
				password = secret
			}
		}

		if handle, err := databaseconnector.Connect(i.GetUser(), password, i.GetHost(), i.GetPort()); err == nil {
			databases = append(databases, &database{
				queryFunc: handle.QueryContext,
				instance:  i})
		}
	}
	return databases
}
