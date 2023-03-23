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

	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const metricURL = "workload.googleapis.com/sap/hanamonitoring"

type gceInterface interface {
	GetSecret(ctx context.Context, projectID, secretName string) (string, error)
}

// queryFunc provides an easily testable translation to the SQL API.
type queryFunc func(ctx context.Context, query string, args ...any) (*sql.Rows, error)

// Parameters hold the parameters necessary to invoke Start().
type Parameters struct {
	Config            *cpb.Configuration
	GCEService        gceInterface
	BackOffs          *cloudmonitoring.BackOffIntervals
	TimeSeriesCreator cloudmonitoring.TimeSeriesCreator
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
	wp := workerpool.New(int(cfg.GetExecutionThreads()))
	for _, db := range databases {
		for _, query := range cfg.GetQueries() {
			sampleInterval := cfg.GetSampleIntervalSec()
			if query.GetSampleIntervalSec() >= 5 {
				sampleInterval = query.GetSampleIntervalSec()
			}
			wp.Submit(func() {
				queryAndSend(ctx, db, query, cfg.GetQueryTimeoutSec(), sampleInterval, params, wp)
			})
		}
	}
	return true
}

// queryAndSend runs the perpetual querying and sending of results to cloud monitoring workflow.
// If any errors occur during query or send, they are logged and the workflow continues to try again next sampleInterval.
func queryAndSend(ctx context.Context, db *database, query *cpb.Query, timeout, sampleInterval int64, params Parameters, wp *workerpool.WorkerPool) error {
	user, host, port, queryName := db.instance.GetUser(), db.instance.GetHost(), db.instance.GetPort(), query.GetName()
	ctxTimeout, cancel := context.WithTimeout(ctx, time.Second*time.Duration(timeout))
	sent, batchCount, err := queryAndSendOnce(ctxTimeout, db, query, params)
	if err != nil {
		log.Logger.Errorw("Error querying database or sending metrics", "user", user, "host", host, "port", port, "query", queryName, "error", err)
		usagemetrics.Error(usagemetrics.HANAMonitoringCollectionFailure)
	}
	log.Logger.Debugw("Sent metrics from queryAndSend.", "user", user, "host", host, "port", port, "query", queryName, "sent", sent, "batches", batchCount, "sleeping", sampleInterval)
	cancel()
	// Release this worker back to the pool and schedule to insert this query back into the task queue after the sampleInterval.
	time.AfterFunc(time.Duration(sampleInterval)*time.Second, func() {
		wp.Submit(func() {
			queryAndSend(ctx, db, query, timeout, sampleInterval, params, wp)
		})
	})
	return err
}

// queryAndSendOnce queries the database, packages the results into time series, and sends those results as metrics to cloud monitoring.
func queryAndSendOnce(ctx context.Context, db *database, query *cpb.Query, params Parameters) (sent, batchCount int, err error) {
	rows, cols, err := queryDatabase(ctx, db.queryFunc, query)
	if err != nil {
		return 0, 0, err
	}

	var metrics []*mrpb.TimeSeries
	for rows.Next() {
		if err := rows.Scan(cols...); err != nil {
			return 0, 0, err
		}
		metrics = append(metrics, createMetricsForRow(db.instance.GetName(), db.instance.GetSid(), query, cols, params)...)
	}
	return cloudmonitoring.SendTimeSeries(ctx, metrics, params.TimeSeriesCreator, params.BackOffs, params.Config.GetCloudProperties().GetProjectId())
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

// createMetricsForRow will loop through each column in a query row result to first populate the metric labels, then create metrics for GAUGE and CUMULATIVE types.
func createMetricsForRow(dbName, sid string, query *cpb.Query, cols []any, params Parameters) []*mrpb.TimeSeries {
	labels := map[string]string{
		"instance_name": dbName,
		"sid":           sid,
	}
	// The first loop through the columns will add all labels for the metrics.
	for i, c := range query.GetColumns() {
		if c.GetMetricType() == cpb.MetricType_METRIC_LABEL {
			// String type is enforced by the config validator for METRIC_LABEL. Type asserting to a pointer due to the coupling with sql.Rows.Scan() populating the columns as such.
			if result, ok := cols[i].(*string); ok {
				labels[c.GetName()] = *result
			}
		}
	}

	var metrics []*mrpb.TimeSeries
	// The second loop will create metrics for each GAUGE and CUMULATIVE type.
	for i, c := range query.GetColumns() {
		if c.GetMetricType() == cpb.MetricType_METRIC_GAUGE {
			if metric, ok := createGaugeMetric(c, cols[i], labels, query.GetName(), params, tspb.Now()); ok {
				metrics = append(metrics, metric)
			}
		}
		// TODO: Add support for CUMULATIVE metrics.
	}
	return metrics
}

// createGaugeMetric builds a cloud monitoring time series with a boolean, int, or float point value for the specified column.
func createGaugeMetric(c *cpb.Column, val any, labels map[string]string, queryName string, params Parameters, timestamp *tspb.Timestamp) (*mrpb.TimeSeries, bool) {
	metricPath := metricURL + "/" + queryName + "/" + c.GetName()
	if c.GetNameOverride() != "" {
		metricPath = metricURL + "/" + c.GetNameOverride()
	}

	ts := timeseries.Params{
		CloudProp:    params.Config.GetCloudProperties(),
		MetricType:   metricPath,
		MetricLabels: labels,
		Timestamp:    timestamp,
		BareMetal:    params.Config.GetBareMetal(),
	}

	// Type asserting to pointers due to the coupling with sql.Rows.Scan() populating the columns as such.
	switch c.GetValueType() {
	case cpb.ValueType_VALUE_INT64:
		if result, ok := val.(*int64); ok {
			ts.Int64Value = *result
		}
		return timeseries.BuildInt(ts), true
	case cpb.ValueType_VALUE_DOUBLE:
		if result, ok := val.(*float64); ok {
			ts.Float64Value = *result
		}
		return timeseries.BuildFloat64(ts), true
	case cpb.ValueType_VALUE_BOOL:
		if result, ok := val.(*bool); ok {
			ts.BoolValue = *result
		}
		return timeseries.BuildBool(ts), true
	default:
		return nil, false
	}
}
