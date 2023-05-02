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
	"sort"
	"strings"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const (
	metricURL = "workload.googleapis.com/sap/hanamonitoring"

	// HANA DB locks the user out after 3 failed authentication attempts, so we
	// will have only two max fail counts.
	maxQueryFailures = 2
)

type gceInterface interface {
	GetSecret(ctx context.Context, projectID, secretName string) (string, error)
}

// timeSeriesKey is a struct which holds the information which can uniquely identify each timeseries
// and can be used as a Map key since every field is comparable.
type timeSeriesKey struct {
	MetricType   string
	MetricKind   string
	MetricLabels string
}

// prevVal struct stores the value of the last datapoint in the timeseries. It is needed to build
// a cumulative timeseries which uses the previous data point value and timestamp since the process
// started.
type prevVal struct {
	val       any
	startTime *tspb.Timestamp
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

// queryOptions holds parameters for the queryAndSend workflows.
type queryOptions struct {
	db             *database
	query          *cpb.Query
	timeout        int64
	sampleInterval int64
	failCount      int64
	params         Parameters
	wp             *workerpool.WorkerPool
	runningSum     map[timeSeriesKey]prevVal
}

// database holds the relevant information for querying and debugging the database.
type database struct {
	queryFunc queryFunc
	instance  *cpb.HANAInstance
}

// Start validates the configuration and creates the database connections.
// Returns true if the query goroutine is started, and false otherwise.
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
	go usagemetrics.LogActionDaily(usagemetrics.CollectHANAMonitoringMetrics)
	go createWorkerPool(ctx, params, databases)
	return true
}

// createWorkerPool creates a job for each query on each database. If the SID
// is not present in the config, the database will be queried to populate it.
func createWorkerPool(ctx context.Context, params Parameters, databases []*database) {
	cfg := params.Config.GetHanaMonitoringConfiguration()
	wp := workerpool.New(int(cfg.GetExecutionThreads()))
	for _, db := range databases {
		if db.instance.GetSid() == "" {
			ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			sid, err := fetchSID(ctxTimeout, db)
			cancel()
			if err != nil {
				log.Logger.Errorw("Error while fetching SID for HANA Instance", db.instance.GetHost(), "error", err)
			}
			db.instance.Sid = sid
		}
		for _, query := range cfg.GetQueries() {
			sampleInterval := cfg.GetSampleIntervalSec()
			if query.GetSampleIntervalSec() >= 5 {
				sampleInterval = query.GetSampleIntervalSec()
			}
			// Since wp.Submit() is non-blocking, the for loop might progress before the
			// task is executed in the workerpool. Create a copy of db and query outside
			// of Submit() to ensure we copy the correct database and query into the call.
			// Reference: https://go.dev/doc/faq#closures_and_goroutines
			db := db
			query := query
			wp.Submit(func() {
				queryAndSend(ctx, queryOptions{
					db:             db,
					query:          query,
					timeout:        cfg.GetQueryTimeoutSec(),
					sampleInterval: sampleInterval,
					params:         params,
					wp:             wp,
					runningSum:     make(map[timeSeriesKey]prevVal),
				})
			})
		}
	}
}

// queryAndSend perpetually queries databases and sends results to cloud monitoring.
// If any errors occur during query or send, they are logged.
// After several consecutive errors, the query will not be restarted.
// Returns true if the query is queued back to the workerpool, false if it is canceled.
func queryAndSend(ctx context.Context, opts queryOptions) (bool, error) {
	user, host, port, queryName := opts.db.instance.GetUser(), opts.db.instance.GetHost(), opts.db.instance.GetPort(), opts.query.GetName()
	ctxTimeout, cancel := context.WithTimeout(ctx, time.Second*time.Duration(opts.timeout))
	sent, batchCount, err := queryAndSendOnce(ctxTimeout, opts.db, opts.query, opts.params, opts.runningSum)
	cancel()
	if err != nil {
		opts.failCount++
		log.Logger.Errorw("Error querying database or sending metrics", "user", user, "host", host, "port", port, "query", queryName, "failCount", opts.failCount, "error", err)
		usagemetrics.Error(usagemetrics.HANAMonitoringCollectionFailure)
	} else {
		opts.failCount = 0
		log.Logger.Debugw("Sent metrics from queryAndSend.", "user", user, "host", host, "port", port, "query", queryName, "sent", sent, "batches", batchCount, "sleeping", opts.sampleInterval)
	}

	if opts.failCount >= maxQueryFailures {
		log.Logger.Errorw("Query reached max failure count, not restarting.", "user", user, "host", host, "port", port, "query", queryName, "failCount", opts.failCount)
		return false, err
	}
	// Schedule to insert this query back into the task queue after the sampleInterval.
	// Also release this worker back to the pool since AfterFunc() is non-blocking.
	time.AfterFunc(time.Duration(opts.sampleInterval)*time.Second, func() {
		opts.wp.Submit(func() {
			queryAndSend(ctx, opts)
		})
	})
	return true, err
}

// queryAndSendOnce queries the database, packages the results into time series, and sends those results as metrics to cloud monitoring.
func queryAndSendOnce(ctx context.Context, db *database, query *cpb.Query, params Parameters, runningSum map[timeSeriesKey]prevVal) (sent, batchCount int, err error) {
	rows, cols, err := queryDatabase(ctx, db.queryFunc, query)
	if err != nil {
		return 0, 0, err
	}
	log.Logger.Infow("Successfuly executed: ", "query", query.GetName(), "host", db.instance.GetHost(), "user", db.instance.GetUser(), "port", db.instance.GetPort())
	var metrics []*mrpb.TimeSeries
	for rows.Next() {
		if err := rows.Scan(cols...); err != nil {
			return 0, 0, err
		}
		metrics = append(metrics, createMetricsForRow(db.instance.GetName(), db.instance.GetSid(), query, cols, params, runningSum)...)
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
			// Rows.Scan() is able to populate any cell as *interface{} by
			// "copying the value provided by the underlying driver without conversion".
			// Reference: https://pkg.go.dev/database/sql#Rows.Scan
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
		dbp := databaseconnector.Params{
			Username:       i.GetUser(),
			Host:           i.GetHost(),
			Password:       password,
			Port:           i.GetPort(),
			EnableSSL:      i.GetEnableSsl(),
			HostNameInCert: i.GetHostNameInCertificate(),
			RootCAFile:     i.GetTlsRootCaFile(),
		}
		if handle, err := databaseconnector.Connect(dbp); err == nil {
			databases = append(databases, &database{
				queryFunc: handle.QueryContext,
				instance:  i})
		}
	}
	return databases
}

// createMetricsForRow will loop through each column in a query row result twice.
// First populate the metric labels, then create metrics for GAUGE and CUMULATIVE types.
func createMetricsForRow(dbName, sid string, query *cpb.Query, cols []any, params Parameters, runningSum map[timeSeriesKey]prevVal) []*mrpb.TimeSeries {
	labels := map[string]string{
		"instance_name": dbName,
		"sid":           sid,
	}
	// The first loop through the columns will add all labels for the metrics.
	for i, c := range query.GetColumns() {
		if c.GetMetricType() == cpb.MetricType_METRIC_LABEL {
			// String type is enforced by the config validator for METRIC_LABEL.
			// Type asserting to a pointer due to the coupling with sql.Rows.Scan() populating the columns as such.
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
		} else if c.GetMetricType() == cpb.MetricType_METRIC_CUMULATIVE {
			if metric, ok := createCumulativeMetric(c, cols[i], labels, query.GetName(), params, tspb.Now(), runningSum); ok {
				metrics = append(metrics, metric)
			}
		}
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

// createCumulativeMetric builds a cloudmonitoring timeseries with an int or float point value for
// the specified column. It returns (nil, false) when it is unable to build the timeseries.
func createCumulativeMetric(c *cpb.Column, val any, labels map[string]string, queryName string, params Parameters, timestamp *tspb.Timestamp, runningSum map[timeSeriesKey]prevVal) (*mrpb.TimeSeries, bool) {
	metricPath := metricURL + "/" + queryName + "/" + c.GetName()
	if c.GetNameOverride() != "" {
		metricPath = metricURL + "/" + c.GetNameOverride()
	}

	ts := timeseries.Params{
		CloudProp:    params.Config.GetCloudProperties(),
		MetricType:   metricPath,
		MetricLabels: labels,
		Timestamp:    timestamp,
		StartTime:    timestamp,
		MetricKind:   mpb.MetricDescriptor_CUMULATIVE,
		BareMetal:    params.Config.GetBareMetal(),
	}

	tsKey := prepareKey(metricPath, ts.MetricKind.String(), labels)

	// Type asserting to pointers due to the coupling with sql.Rows.Scan() populating the columns as such.
	switch c.GetValueType() {
	case cpb.ValueType_VALUE_INT64:
		if result, ok := val.(*int64); ok {
			ts.Int64Value = *result
		}
		if lastVal, ok := runningSum[tsKey]; ok {
			log.Logger.Debugw("Found already existing key.", "Key", tsKey, "prevVal", lastVal)
			ts.Int64Value = ts.Int64Value + lastVal.val.(int64)
			ts.StartTime = lastVal.startTime
			runningSum[tsKey] = prevVal{val: ts.Int64Value, startTime: ts.StartTime}
			return timeseries.BuildInt(ts), true
		}
		log.Logger.Debugw("Key does not yet exist, not creating metric.", "Key", tsKey)
		runningSum[tsKey] = prevVal{val: ts.Int64Value, startTime: ts.StartTime}
		return nil, false
	case cpb.ValueType_VALUE_DOUBLE:
		if result, ok := val.(*float64); ok {
			ts.Float64Value = *result
		}
		if lastVal, ok := runningSum[tsKey]; ok {
			log.Logger.Debugw("Found already existing key.", "Key", tsKey, "prevVal", lastVal)
			ts.Float64Value = ts.Float64Value + lastVal.val.(float64)
			ts.StartTime = lastVal.startTime
			runningSum[tsKey] = prevVal{val: ts.Float64Value, startTime: ts.StartTime}
			return timeseries.BuildFloat64(ts), true
		}
		log.Logger.Debugw("Key does not yet exist, not creating metric.", "Key", tsKey)
		runningSum[tsKey] = prevVal{val: ts.Float64Value, startTime: ts.StartTime}
		return nil, false
	default:
		return nil, false
	}
}

// fetchSID is responsible for fetching the SID for a HANA instance if it not
// already set by executing a query on the M_DATABASE table.
func fetchSID(ctx context.Context, db *database) (string, error) {
	rows, err := db.queryFunc(ctx, "SELECT SYSTEM_ID AS sid FROM M_DATABASE LIMIT 1;")
	if err != nil {
		return "", err
	}
	rows.Next()
	var sid string
	if err := rows.Scan(&sid); err != nil {
		return "", err
	}
	return sid, nil
}

// prepareKey creates the key which can be used to group a timeseries
// based on MetricType, MetricKind and MetricLabels.
func prepareKey(mtype, mkind string, labels map[string]string) timeSeriesKey {
	tsk := timeSeriesKey{
		MetricType: mtype,
		MetricKind: mkind,
	}
	var metricLabels []string
	for k, v := range labels {
		metricLabels = append(metricLabels, k+":"+v)
	}
	sort.Strings(metricLabels)
	tsk.MetricLabels = strings.Join(metricLabels, ",")
	return tsk
}
