/*
Copyright 2022 Google LLC

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

// Package cloudmonitoring provides functionality to interact with the Cloud Monitoring API.
package cloudmonitoring

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/googleapis/gax-go/v2"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// timeSeriesKey is a struct which holds the information which can uniquely identify each time series
// and can be used as a Map key since every field is comparable.
type timeSeriesKey struct {
	MetricType        string
	MetricKind        string
	MetricLabels      string
	MonitoredResource string
	ResourceLabels    string
}

// BackOffIntervals holds the initial intervals for the different back off mechanisms.
type BackOffIntervals struct {
	LongExponential, ShortConstant time.Duration
}

// Defaults for the different back off intervals.
const (
	DefaultLongExponentialBackOffInterval = 2 * time.Second
	DefaultShortConstantBackOffInterval   = 5 * time.Second
)

const maxTSPerRequest = 200 // Reference: https://cloud.google.com/monitoring/quotas

// NewBackOffIntervals is a constructor for the back off intervals.
func NewBackOffIntervals(longExponential, shortConstant time.Duration) *BackOffIntervals {
	return &BackOffIntervals{
		LongExponential: longExponential,
		ShortConstant:   shortConstant,
	}
}

// NewDefaultBackOffIntervals is a default constructor, utilizing the default back off intervals.
func NewDefaultBackOffIntervals() *BackOffIntervals {
	return &BackOffIntervals{
		LongExponential: DefaultLongExponentialBackOffInterval,
		ShortConstant:   DefaultShortConstantBackOffInterval,
	}
}

// TimeSeriesCreator provides an easily testable translation to the cloud monitoring API.
type TimeSeriesCreator interface {
	CreateTimeSeries(ctx context.Context, req *mpb.CreateTimeSeriesRequest, opts ...gax.CallOption) error
}

// TimeSeriesQuerier provides an easily testable translation to the cloud monitoring API.
type TimeSeriesQuerier interface {
	QueryTimeSeries(ctx context.Context, req *mpb.QueryTimeSeriesRequest, opts ...gax.CallOption) ([]*mrpb.TimeSeriesData, error)
}

// CreateTimeSeriesWithRetry decorates TimeSeriesCreator.CreateTimeSeries with a retry mechanism.
func CreateTimeSeriesWithRetry(ctx context.Context, client TimeSeriesCreator, req *mpb.CreateTimeSeriesRequest, bo *BackOffIntervals) error {

	attempt := 1
	if bo == nil {
		bo = NewDefaultBackOffIntervals()
	}

	err := backoff.Retry(func() error {
		if err := client.CreateTimeSeries(ctx, req); err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.CtxLogger(ctx).Warnw("Error in CreateTimeSeries, Permission denied - Enable the Monitoring Metrics Writer IAM role for the Service Account", "attempt", attempt, "error", err)
			} else {
				log.CtxLogger(ctx).Warnw("Error in CreateTimeSeries", "attempt", attempt, "error", err)
			}
			attempt++
			return err
		}
		return nil
	}, ShortConstantBackOffPolicy(ctx, bo.ShortConstant, 2))

	if err != nil {
		log.CtxLogger(ctx).Errorw("CreateTimeSeries retry limit exceeded", "request", req)
		return err
	}
	return nil
}

// QueryTimeSeriesWithRetry decorates TimeSeriesQuerier.QueryTimeSeries with a retry mechanism.
func QueryTimeSeriesWithRetry(ctx context.Context, client TimeSeriesQuerier, req *mpb.QueryTimeSeriesRequest, bo *BackOffIntervals) ([]*mrpb.TimeSeriesData, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeriesData
	)

	if bo == nil {
		bo = NewDefaultBackOffIntervals()
	}

	err := backoff.Retry(func() error {
		var err error
		res, err = client.QueryTimeSeries(ctx, req)
		if err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.CtxLogger(ctx).Warnw("Error in QueryTimeSeries, Permission denied - Enable the Monitoring Viewer IAM role for the Service Account", "attempt", attempt, "error", err)
			} else {
				log.CtxLogger(ctx).Warnw("Error in QueryTimeSeries", "attempt", attempt, "error", err)
			}
			attempt++
		}
		return err
	}, LongExponentialBackOffPolicy(ctx, bo.LongExponential, 4, time.Minute, 15*time.Second))
	if err != nil {
		log.CtxLogger(ctx).Errorw("QueryTimeSeries retry limit exceeded", "request", req, "error", err, "attempt", attempt)
		return nil, err
	}
	return res, nil
}

// LongExponentialBackOffPolicy returns a backoff policy with exponential backoff.
func LongExponentialBackOffPolicy(ctx context.Context, initial time.Duration, retries uint64, maxElapsedTime time.Duration, maxInterval time.Duration) backoff.BackOffContext {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = initial
	exp.MaxInterval = maxInterval
	exp.MaxElapsedTime = maxElapsedTime
	log.CtxLogger(ctx).Debug("LongExponentialBackOffPolicy", "exp", exp)
	return backoff.WithContext(backoff.WithMaxRetries(exp, retries), ctx)
}

// LongExponentialBackOffPolicyForProcessMetrics returns a backoff policy with exponential backoff.
func LongExponentialBackOffPolicyForProcessMetrics(ctx context.Context, initial time.Duration, retries uint64, maxElapsedTime time.Duration, maxInterval time.Duration) backoff.BackOffContext {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = initial
	exp.MaxInterval = maxInterval
	exp.MaxElapsedTime = maxElapsedTime
	exp.Multiplier = 2
	log.CtxLogger(ctx).Debug("LongExponentialBackOffPolicyForProcessMetrics", "exp", exp)
	return backoff.WithContext(backoff.WithMaxRetries(exp, retries), ctx)
}

// ShortConstantBackOffPolicy returns a backoff policy with 15s MaxElapsedTime.
func ShortConstantBackOffPolicy(ctx context.Context, initial time.Duration, retries uint64) backoff.BackOffContext {
	constantBackoff := backoff.NewConstantBackOff(initial)
	return backoff.WithContext(backoff.WithMaxRetries(constantBackoff, retries), ctx)
}

func flattenLabels(labels map[string]string) string {
	var metricLabels []string
	for k, v := range labels {
		metricLabels = append(metricLabels, k+"+"+v)
	}
	sort.Strings(metricLabels)
	return strings.Join(metricLabels, ",")
}

// prepareKey creates the key which can be used to group a time series
// based on MetricType, MetricKind, MetricLabels, MonitoredResource and ResourceLabels.
func prepareKey(t *mrpb.TimeSeries) timeSeriesKey {
	mtype := t.GetMetric().GetType()
	mkind := t.GetMetricKind().String()
	mresource := t.GetResource().GetType()
	tsk := timeSeriesKey{
		MetricType:        mtype,
		MetricKind:        mkind,
		MonitoredResource: mresource,
	}

	tsk.MetricLabels = flattenLabels(t.GetMetric().GetLabels())
	tsk.ResourceLabels = flattenLabels(t.GetResource().GetLabels())

	return tsk
}

// SendTimeSeries sends all the time series objects to cloud monitoring.
// maxTSPerRequest is used as an upper limit to batch send time series values per request.
// If a cloud monitoring API call fails even after retries, the remaining measurements are discarded.
func SendTimeSeries(ctx context.Context, timeSeries []*mrpb.TimeSeries, timeSeriesCreator TimeSeriesCreator, bo *BackOffIntervals, projectID string) (sent, batchCount int, err error) {
	var batchTimeSeries []*mrpb.TimeSeries

	for _, t := range timeSeries {
		batchTimeSeries = append(batchTimeSeries, t)

		if len(batchTimeSeries) == maxTSPerRequest {
			log.CtxLogger(ctx).Debug("Maximum batch size has been reached, sending the batch.")
			batchCount++
			if err := sendBatch(ctx, batchTimeSeries, timeSeriesCreator, bo, projectID); err != nil {
				return sent, batchCount, err
			}
			sent += len(batchTimeSeries)
			batchTimeSeries = nil
		}
	}
	if len(batchTimeSeries) == 0 {
		return sent, batchCount, nil
	}
	batchCount++
	if err := sendBatch(ctx, batchTimeSeries, timeSeriesCreator, bo, projectID); err != nil {
		return sent, batchCount, err
	}
	return sent + len(batchTimeSeries), batchCount, nil
}

// sendBatch sends one batch of metrics to cloud monitoring using an API call with retries. Returns an error in case of failures.
func sendBatch(ctx context.Context, batchTimeSeries []*mrpb.TimeSeries, timeSeriesCreator TimeSeriesCreator, bo *BackOffIntervals, projectID string) error {
	log.CtxLogger(ctx).Debugw("Sending a batch of metrics to cloud monitoring.", "numberofmetrics", len(batchTimeSeries), "metrics", batchTimeSeries)
	req := &mpb.CreateTimeSeriesRequest{
		Name:       fmt.Sprintf("projects/%s", projectID),
		TimeSeries: pruneBatch(batchTimeSeries),
	}

	return CreateTimeSeriesWithRetry(ctx, timeSeriesCreator, req, bo)
}

func pruneBatch(batchTimeSeries []*mrpb.TimeSeries) []*mrpb.TimeSeries {
	ts := make(map[timeSeriesKey]bool)
	var finalBatch []*mrpb.TimeSeries
	for _, t := range batchTimeSeries {
		tsk := prepareKey(t)
		if _, ok := ts[tsk]; ok {
			log.Logger.Debug("Pruned a duplicate time series", "tsk:", tsk)
			continue
		}
		ts[tsk] = true

		finalBatch = append(finalBatch, t)
	}
	return finalBatch
}
