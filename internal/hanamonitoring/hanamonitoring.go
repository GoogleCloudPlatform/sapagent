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
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/recovery"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/timeseries"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

const (
	metricURL = "workload.googleapis.com/sap/hanamonitoring"
)

type (
	gceInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
	}

	// timeSeriesKey is a struct which holds the information which can uniquely identify each timeseries
	// and can be used as a Map key since every field is comparable.
	timeSeriesKey struct {
		MetricType   string
		MetricKind   string
		MetricLabels string
	}

	// prevVal struct stores the value of the last datapoint in the timeseries. It is needed to build
	// a cumulative timeseries which uses the previous data point value and timestamp since the process
	// started.
	prevVal struct {
		val       any
		startTime *tspb.Timestamp
	}

	// isAuthErrorFunc determines if an error is an authentication error.
	isAuthErrorFunc func(err error) bool

	// queryFunc provides an easily testable translation to the SQL API.
	queryFunc func(ctx context.Context, query string, exec commandlineexecutor.Execute) (*databaseconnector.QueryResults, error)

	// hanaReplicationConfig provides an easily testable translation to invoking the sapdiscovery package function HANAReplicationConfig.
	hanaReplicationConfig func(ctx context.Context, user, sid, instID string, systemDiscoveryInterface system.SapSystemDiscoveryInterface) (int, int64, *sapb.HANAReplicaSite, error)

	// Parameters hold the parameters necessary to invoke Start().
	Parameters struct {
		Config                  *cpb.Configuration
		GCEService              gceInterface
		BackOffs                *cloudmonitoring.BackOffIntervals
		TimeSeriesCreator       cloudmonitoring.TimeSeriesCreator
		dailyMetricsRoutine     *recovery.RecoverableRoutine
		createWorkerPoolRoutine *recovery.RecoverableRoutine
		HRC                     hanaReplicationConfig
		SystemDiscovery         system.SapSystemDiscoveryInterface
		ConnectionRetryInterval time.Duration
	}

	// queryOptions holds parameters for the queryAndSend workflows.
	queryOptions struct {
		db              *database
		query           *cpb.Query
		timeout         int64
		sampleInterval  int64
		failCount       int64
		params          Parameters
		wp              *workerpool.WorkerPool
		runningSum      map[timeSeriesKey]prevVal
		isAuthErrorFunc isAuthErrorFunc
	}

	// database holds the relevant information for querying and debugging the database.
	database struct {
		queryFunc queryFunc
		instance  *cpb.HANAInstance
	}

	// submitQueriesToWorkerPoolArgs holds the parameters necessary to invoke the routine submitQueriesToWorkerPool().
	submitQueriesToWorkerPoolArgs struct {
		params    Parameters
		wp        *workerpool.WorkerPool
		databases []*database
	}
)

// Start validates the configuration and creates the database connections.
// Returns true if the query goroutine is started, and false otherwise.
func Start(ctx context.Context, params Parameters) bool {
	cfg := params.Config.GetHanaMonitoringConfiguration()
	if !cfg.GetEnabled() {
		log.CtxLogger(ctx).Info("HANA Monitoring disabled, not starting HANA Monitoring.")
		return false
	}
	if len(cfg.GetQueries()) == 0 {
		log.CtxLogger(ctx).Info("HANA Monitoring enabled but no queries defined, not starting HANA Monitoring.")
		usagemetrics.Error(usagemetrics.MalformedConfigFile)
		return false
	}

	if len(cfg.GetHanaInstances()) == 0 {
		log.CtxLogger(ctx).Info("HANA Monitoring enabled but no HANA instances defined, not starting HANA Monitoring.")
		usagemetrics.Error(usagemetrics.MalformedConfigFile)
		return false
	}

	// Log usagemetric if any one of the HANA instances has hdbuserstore key configured.
	for _, i := range params.Config.GetHanaMonitoringConfiguration().GetHanaInstances() {
		if i.GetHdbuserstoreKey() != "" {
			usagemetrics.Action(usagemetrics.HDBUserstoreKeyConfigured)
			break
		}
	}

	log.CtxLogger(ctx).Info("Starting HANA Monitoring.")
	params.dailyMetricsRoutine = &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.CollectHANAMonitoringMetrics) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 24 * time.Hour,
	}
	params.dailyMetricsRoutine.StartRoutine(ctx)

	createWorkerPoolRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			if params, ok := a.(Parameters); ok {
				createWorkerPool(ctx, params)
			}
		},
		RoutineArg:          params,
		ErrorCode:           usagemetrics.HANAMonitoringCreateWorkerPoolFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: time.Minute,
	}
	createWorkerPoolRoutine.StartRoutine(ctx)
	return true
}

func createWorkerPool(ctx context.Context, params Parameters) {
	ticker := time.NewTicker(params.ConnectionRetryInterval)
	connectedDBs := map[string]bool{}
	for _, i := range params.Config.GetHanaMonitoringConfiguration().GetHanaInstances() {
		connectedDBs[fmt.Sprintf("%s:%s:%s", i.GetHost(), i.GetUser(), i.GetPort())] = false
	}
	authErrorDBs := map[string]bool{}
	wp := workerpool.New(int(params.Config.GetHanaMonitoringConfiguration().GetExecutionThreads()))
	databases := connectToDatabases(ctx, params, connectedDBs, authErrorDBs)
	invokeSubmitQueriesToWorkerPool(ctx, params, wp, databases)
	for {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("HANA Monitoring context cancelled, exiting")
			return
		case <-ticker.C:
			databases := connectToDatabases(ctx, params, connectedDBs, authErrorDBs)
			paramsCopy := params
			wpCopy := wp
			invokeSubmitQueriesToWorkerPool(ctx, paramsCopy, wpCopy, databases)
			retry := false
			for _, dbsConnected := range connectedDBs {
				if !dbsConnected {
					log.CtxLogger(ctx).Debugw("Some DBs are not connected, retrying connection", "ConnectedDBs", dbsConnected)
					retry = true
					break
				}
			}
			if !retry {
				log.CtxLogger(ctx).Info("All DBs connected, exiting the connection retrial loop")
				return
			}
		}
	}
}

func invokeSubmitQueriesToWorkerPool(ctx context.Context, params Parameters, wp *workerpool.WorkerPool, databases []*database) {
	if len(databases) == 0 {
		log.CtxLogger(ctx).Info("No databases to submit queries to workerpool")
		return
	}
	submitQueriesToWPArgs := submitQueriesToWorkerPoolArgs{
		params:    params,
		databases: databases,
		wp:        wp,
	}
	params.createWorkerPoolRoutine = &recovery.RecoverableRoutine{
		Routine:             submitQueriesToWorkerPool,
		RoutineArg:          submitQueriesToWPArgs,
		ErrorCode:           usagemetrics.HANAMonitoringCreateWorkerPoolFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: time.Minute,
	}
	params.createWorkerPoolRoutine.StartRoutine(ctx)
}

// submitQueriesToWorkerPool creates a job for each query on each database. If the SID
// is not present in the config, the database will be queried to populate it.
func submitQueriesToWorkerPool(ctx context.Context, a any) {
	var args submitQueriesToWorkerPoolArgs
	var ok bool
	if args, ok = a.(submitQueriesToWorkerPoolArgs); !ok {
		log.CtxLogger(ctx).Infow(
			"args is not of type createWorkerPoolArgs",
			"typeOfArgs", reflect.TypeOf(a),
		)
		return
	}

	cfg := args.params.Config.GetHanaMonitoringConfiguration()
	var wp *workerpool.WorkerPool
	if args.wp != nil {
		wp = args.wp
	} else {
		wp = workerpool.New(int(cfg.GetExecutionThreads()))
	}
	queryNamesMap := queryMap(cfg.GetQueries())
	var queryNames []string
	for qn := range queryNamesMap {
		queryNames = append(queryNames, qn)
	}
	for _, db := range args.databases {
		if db.instance.GetSid() == "" {
			ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			sid, err := fetchSID(ctxTimeout, db)
			cancel()
			if err != nil {
				log.CtxLogger(ctx).Errorw("Error while fetching SID for HANA Instance", "host", db.instance.GetHost(), "error", err)
			}
			db.instance.Sid = sid
		}
		if db.instance.GetQueriesToRun() == nil {
			db.instance.QueriesToRun = &cpb.QueriesToRun{
				QueryNames: queryNames,
				RunAll:     true,
			}
		} else if db.instance.GetQueriesToRun().GetRunAll() || len(db.instance.GetQueriesToRun().GetQueryNames()) == 0 {
			db.instance.QueriesToRun.QueryNames = queryNames
		}
		for _, qn := range db.instance.GetQueriesToRun().GetQueryNames() {
			sampleInterval := cfg.GetSampleIntervalSec()
			query, ok := queryNamesMap[qn]
			if !ok {
				log.CtxLogger(ctx).Warnf("Query not found in config file", "queryName", qn)
				continue
			}
			if query.GetSampleIntervalSec() >= 5 {
				sampleInterval = query.GetSampleIntervalSec()
			}
			// Since wp.Submit() is non-blocking, the for loop might progress before the
			// task is executed in the workerpool. Create a copy of db and query outside
			// of Submit() to ensure we copy the correct database and query into the call.
			// Reference: https://go.dev/doc/faq#closures_and_goroutines
			dbCopy := db
			queryCopy := query
			wp.Submit(func() {
				queryAndSend(ctx, queryOptions{
					db:             dbCopy,
					query:          queryCopy,
					timeout:        cfg.GetQueryTimeoutSec(),
					sampleInterval: sampleInterval,
					params:         args.params,
					wp:             wp,
					runningSum:     make(map[timeSeriesKey]prevVal),
				})
			})
		}
	}
}

// queryMap prepares a queryName to *cpb.Query Map data structure.
func queryMap(queries []*cpb.Query) map[string]*cpb.Query {
	res := make(map[string]*cpb.Query)
	for _, q := range queries {
		res[q.GetName()] = q
	}
	return res
}

// queryAndSend perpetually queries databases and sends results to cloud monitoring.
// If any errors occur during query or send, they are logged.
// After several consecutive errors, the query will not be restarted.
// Returns true if the query is queued back to the workerpool, false if it is canceled.
func queryAndSend(ctx context.Context, opts queryOptions) (bool, error) {
	user, host, port, queryName := opts.db.instance.GetUser(), opts.db.instance.GetHost(), opts.db.instance.GetPort(), opts.query.GetName()
	ctxTimeout, cancel := context.WithTimeout(ctx, time.Second*time.Duration(opts.timeout))
	if opts.isAuthErrorFunc == nil {
		opts.isAuthErrorFunc = databaseconnector.IsAuthError
	}
	select {
	case <-ctx.Done():
		log.CtxLogger(ctx).Debugw("Context cancelled, stopping queryAndSend worker", "err", ctx.Err())
		cancel()
		return false, ctx.Err()
	default:
		sent, batchCount, err := queryAndSendOnce(ctxTimeout, opts.db, opts.query, opts.params, opts.runningSum)
		cancel()
		if err != nil {
			opts.failCount++
			log.CtxLogger(ctx).Errorw("Error querying database or sending metrics", "user", user, "host", host, "port", port, "query", queryName, "failCount", opts.failCount, "error", err)
			usagemetrics.Error(usagemetrics.HANAMonitoringCollectionFailure)
		} else {
			opts.failCount = 0
			log.CtxLogger(ctx).Debugw("Sent metrics from queryAndSend.", "user", user, "host", host, "port", port, "query", queryName, "sent", sent, "batches", batchCount, "sleeping", opts.sampleInterval)
		}

		if opts.isAuthErrorFunc(err) {
			log.CtxLogger(ctx).Errorw("Query resulted in authentication error, not restarting to prevent user lockout", "user", user, "host", host, "port", port, "query", queryName, "failCount", opts.failCount)
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
}

// matchQueryAndInstanceType checks if the query should be run on the current instance by matching
// the runOn field in Query and Instance Type
// There are queries which should only run on either Primary or Secondary instances and since
// this is dynamic we need to check if a query should run or not.
func matchQueryAndInstanceType(ctx context.Context, opts queryOptions) bool {
	instance := opts.db.instance
	if !instance.IsLocal {
		log.CtxLogger(ctx).Debugw("Instance not marked local in the config, cannot refresh system replication config", "instance", instance.Name)
		return true
	}
	if instance.InstanceNum == "" {
		log.CtxLogger(ctx).Debugw("Instance number information not specified in the config, executing the query", "instance", instance.Name)
		return true
	}
	if opts.query.RunOn == cpb.RunOn_RUN_ON_UNSPECIFIED {
		log.CtxLogger(ctx).Debugw("runOn information not specified in the config", "query", opts.query.GetName(), "host", instance.GetHost(), "user", instance.GetUser(), "port", instance.GetPort())
		return true
	}
	if opts.query.RunOn == cpb.RunOn_ALL {
		log.CtxLogger(ctx).Debugw("Query configured to run on all instances", "query", opts.query.GetName(), "host", instance.GetHost(), "user", instance.GetUser(), "port", instance.GetPort())
		return true
	}
	sidUser := fmt.Sprintf("%sadm", strings.ToLower(instance.Sid))
	site, _, _, err := opts.params.HRC(ctx, sidUser, instance.Sid, instance.InstanceNum, opts.params.SystemDiscovery)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error getting HANA replication config", "sidUser", sidUser, "instanceNum", instance.InstanceNum, "error", err)
		return false
	}
	log.CtxLogger(ctx).Debugw("Site for the instance: ", "site", site, "instance", instance.Name)
	switch site {
	case 0: // stand alone mode
		return true
	case int(opts.query.RunOn.Number()):
		return true
	default:
		return false
	}
}

// queryAndSendOnce queries the database, packages the results into time series, and sends those results as metrics to cloud monitoring.
func queryAndSendOnce(ctx context.Context, db *database, query *cpb.Query, params Parameters, runningSum map[timeSeriesKey]prevVal) (sent, batchCount int, err error) {
	if collectExpiementalMetrics(ctx, params) && !matchQueryAndInstanceType(ctx, queryOptions{db: db, query: query, params: params}) {
		log.CtxLogger(ctx).Infow("Query should not run on this instance type in this cycle ", "query", query.GetName(), "host", db.instance.GetHost(), "user", db.instance.GetUser(), "port", db.instance.GetPort())
		return 0, 0, nil
	}
	queryStartTime := time.Now()
	rows, cols, err := queryDatabase(ctx, db.queryFunc, query)
	responseTime := time.Since(queryStartTime).Milliseconds()
	if err != nil {
		return 0, 0, err
	}
	log.CtxLogger(ctx).Infow("Successfully executed: ", "query", query.GetName(), "host", db.instance.GetHost(), "user", db.instance.GetUser(), "port", db.instance.GetPort(), "response time", responseTime)
	var metrics []*mrpb.TimeSeries
	if params.Config.GetHanaMonitoringConfiguration().GetSendQueryResponseTime() {
		metrics = append(metrics, createQueryResponseTimeMetric(ctx, db.instance.GetName(), db.instance.GetSid(), query, params, responseTime, tspb.Now()))
	}
	for rows.Next() {
		if err := rows.ReadRow(cols...); err != nil {
			return 0, 0, err
		}
		metrics = append(metrics, createMetricsForRow(ctx, db.instance.GetName(), db.instance.GetSid(), query, cols, params, runningSum)...)
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

// queryDatabase attempts to execute the specified query, returning a QueryResults iterator and a slice for storing the column results of each row.
func queryDatabase(ctx context.Context, queryFunc queryFunc, query *cpb.Query) (*databaseconnector.QueryResults, []any, error) {
	if query == nil {
		return nil, nil, errors.New("no query specified")
	}
	cols := createColumns(query.GetColumns())
	if cols == nil {
		return nil, nil, errors.New("no columns specified")
	}
	rows, err := queryFunc(ctx, query.GetSql(), commandlineexecutor.ExecuteCommand)
	if err != nil {
		return nil, nil, err
	}
	return rows, cols, nil
}

// connectToDatabases attempts to create a DB handle for each HANAInstance.
func connectToDatabases(ctx context.Context, params Parameters, connectedDBs map[string]bool, authErrorDBs map[string]bool) []*database {
	var databases []*database
	for _, i := range params.Config.GetHanaMonitoringConfiguration().GetHanaInstances() {
		if connected, ok := connectedDBs[fmt.Sprintf("%s:%s:%s", i.GetHost(), i.GetUser(), i.GetPort())]; connected && ok {
			continue
		}
		if _, ok := authErrorDBs[fmt.Sprintf("%s:%s:%s", i.GetHost(), i.GetUser(), i.GetPort())]; ok {
			log.CtxLogger(ctx).Debugw("Instance is misconfigured with wrong credentials, not retrying to connect to the instance", "name", i.GetName())
			continue
		}
		hanaMonitoringConfig := params.Config.GetHanaMonitoringConfiguration()

		dbp := databaseconnector.Params{
			Username:       i.GetUser(),
			Host:           i.GetHost(),
			Password:       i.GetPassword(),
			PasswordSecret: i.GetSecretName(),
			Port:           i.GetPort(),
			EnableSSL:      i.GetEnableSsl(),
			HostNameInCert: i.GetHostNameInCertificate(),
			RootCAFile:     i.GetTlsRootCaFile(),
			HDBUserKey:     i.GetHdbuserstoreKey(),
			SID:            i.GetSid(),
			GCEService:     params.GCEService,
			Project:        params.Config.GetCloudProperties().GetProjectId(),
		}

		connectTimeout := hanaMonitoringConfig.GetConnectionTimeout()
		if connectTimeout.GetSeconds() > 0 {
			dbp.PingSpec = &databaseconnector.PingSpec{
				Timeout:    time.Duration(connectTimeout.GetSeconds()) * time.Second,
				MaxRetries: int(hanaMonitoringConfig.GetMaxConnectRetries().GetValue()),
			}
		}

		handle, err := databaseconnector.CreateDBHandle(ctx, dbp)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Error connecting to database", "name", i.GetName(), "error", err.Error())
			usagemetrics.Error(usagemetrics.HANAMonitoringCollectionFailure)
			if databaseconnector.IsAuthError(err) {
				authErrorDBs[fmt.Sprintf("%s:%s:%s", i.GetHost(), i.GetUser(), i.GetPort())] = true
				log.CtxLogger(ctx).Errorw("Auth error connecting to database, not retrying to connect to the instance", "name", i.GetName(), "error", err.Error())
			}
			continue
		}
		connectedDBs[fmt.Sprintf("%s:%s:%s", i.GetHost(), i.GetUser(), i.GetPort())] = true
		databases = append(databases, &database{queryFunc: handle.Query, instance: i})
	}
	return databases
}

// createQueryResponseTimeMetric builds a cloud monitoring time series with an int point value for the time taken by query.
func createQueryResponseTimeMetric(ctx context.Context, dbName, sid string, query *cpb.Query, params Parameters, timeTaken int64, timestamp *tspb.Timestamp) *mrpb.TimeSeries {
	labels := map[string]string{
		"instance_name": dbName,
		"sid":           sid,
	}
	ts := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(params.Config.GetCloudProperties()),
		MetricType:   metricURL + "/" + query.GetName() + "/time_taken_ms",
		MetricLabels: labels,
		Timestamp:    timestamp,
		BareMetal:    params.Config.GetBareMetal(),
		Int64Value:   timeTaken,
	}
	return timeseries.BuildInt(ts)
}

// createMetricsForRow will loop through each column in a query row result twice.
// First populate the metric labels, then create metrics for GAUGE and CUMULATIVE types.
func createMetricsForRow(ctx context.Context, dbName, sid string, query *cpb.Query, cols []any, params Parameters, runningSum map[timeSeriesKey]prevVal) []*mrpb.TimeSeries {
	labels := map[string]string{
		"instance_name": dbName,
		"sid":           sid,
	}
	labels = createLabels(query, cols, labels)

	var metrics []*mrpb.TimeSeries
	// The second loop will create metrics for each GAUGE and CUMULATIVE type.
	for i, c := range query.GetColumns() {
		if c.GetMetricType() == cpb.MetricType_METRIC_GAUGE {
			if metric, ok := createGaugeMetric(c, cols[i], labels, query.GetName(), params, tspb.Now()); ok {
				metrics = append(metrics, metric)
			}
		} else if c.GetMetricType() == cpb.MetricType_METRIC_CUMULATIVE {
			if metric, ok := createCumulativeMetric(ctx, c, cols[i], labels, query.GetName(), params, tspb.Now(), runningSum); ok {
				metrics = append(metrics, metric)
			}
		}
	}
	return metrics
}

func createLabels(query *cpb.Query, cols []any, labels map[string]string) map[string]string {
	for i, c := range query.GetColumns() {
		if c.GetMetricType() == cpb.MetricType_METRIC_LABEL {
			// String type is enforced by the config validator for METRIC_LABEL.
			// Type asserting to a pointer due to the coupling with sql.Rows.Scan() populating the columns as such.
			if result, ok := cols[i].(*string); ok {
				labels[c.GetName()] = *result
			}
		}
	}
	return labels
}

// createGaugeMetric builds a cloud monitoring time series with a boolean, int, or float point value for the specified column.
func createGaugeMetric(c *cpb.Column, val any, labels map[string]string, queryName string, params Parameters, timestamp *tspb.Timestamp) (*mrpb.TimeSeries, bool) {
	metricPath := metricURL + "/" + queryName + "/" + c.GetName()
	if c.GetNameOverride() != "" {
		metricPath = metricURL + "/" + c.GetNameOverride()
	}

	ts := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(params.Config.GetCloudProperties()),
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
func createCumulativeMetric(ctx context.Context, c *cpb.Column, val any, labels map[string]string, queryName string, params Parameters, timestamp *tspb.Timestamp, runningSum map[timeSeriesKey]prevVal) (*mrpb.TimeSeries, bool) {
	metricPath := metricURL + "/" + queryName + "/" + c.GetName()
	if c.GetNameOverride() != "" {
		metricPath = metricURL + "/" + c.GetNameOverride()
	}

	ts := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(params.Config.GetCloudProperties()),
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
			log.CtxLogger(ctx).Debugw("Found already existing key.", "Key", tsKey, "prevVal", lastVal)
			ts.Int64Value = ts.Int64Value + lastVal.val.(int64)
			ts.StartTime = lastVal.startTime
			runningSum[tsKey] = prevVal{val: ts.Int64Value, startTime: ts.StartTime}
			return timeseries.BuildInt(ts), true
		}
		log.CtxLogger(ctx).Debugw("Key does not yet exist, not creating metric.", "Key", tsKey)
		runningSum[tsKey] = prevVal{val: ts.Int64Value, startTime: ts.StartTime}
		return nil, false
	case cpb.ValueType_VALUE_DOUBLE:
		if result, ok := val.(*float64); ok {
			ts.Float64Value = *result
		}
		if lastVal, ok := runningSum[tsKey]; ok {
			log.CtxLogger(ctx).Debugw("Found already existing key.", "Key", tsKey, "prevVal", lastVal)
			ts.Float64Value = ts.Float64Value + lastVal.val.(float64)
			ts.StartTime = lastVal.startTime
			runningSum[tsKey] = prevVal{val: ts.Float64Value, startTime: ts.StartTime}
			return timeseries.BuildFloat64(ts), true
		}
		log.CtxLogger(ctx).Debugw("Key does not yet exist, not creating metric.", "Key", tsKey)
		runningSum[tsKey] = prevVal{val: ts.Float64Value, startTime: ts.StartTime}
		return nil, false
	default:
		return nil, false
	}
}

// fetchSID is responsible for fetching the SID for a HANA instance if it not
// already set by executing a query on the M_DATABASE table.
func fetchSID(ctx context.Context, db *database) (string, error) {
	rows, err := db.queryFunc(ctx, "SELECT SYSTEM_ID AS sid FROM M_DATABASE LIMIT 1;", commandlineexecutor.ExecuteCommand)
	if err != nil {
		return "", err
	}
	rows.Next()
	var sid string
	if err := rows.ReadRow(&sid); err != nil {
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

func collectExpiementalMetrics(ctx context.Context, params Parameters) bool {
	cc := params.Config.GetCollectionConfiguration()
	if cc == nil {
		log.CtxLogger(ctx).Error("No collection configuration specified")
		return false
	}
	return cc.GetCollectExperimentalMetrics()
}
