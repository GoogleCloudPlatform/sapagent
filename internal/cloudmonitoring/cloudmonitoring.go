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
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/googleapis/gax-go/v2"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// BackOffIntervals holds the initial intervals for the different back off mechanisms.
type BackOffIntervals struct {
	LongExponential, ShortConstant time.Duration
}

// Defaults for the different back off intervals.
const (
	DefaultLongExponentialBackOffInterval = 2 * time.Second
	DefaultShortConstantBackOffInterval   = 5 * time.Second
)

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
	CreateTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest, opts ...gax.CallOption) error
}

// TimeSeriesQuerier provides an easily testable translation to the cloud monitoring API.
type TimeSeriesQuerier interface {
	QueryTimeSeries(ctx context.Context, req *monitoringpb.QueryTimeSeriesRequest, opts ...gax.CallOption) ([]*mrpb.TimeSeriesData, error)
}

// CreateTimeSeriesWithRetry decorates TimeSeriesCreator.CreateTimeSeries with a retry mechanism.
func CreateTimeSeriesWithRetry(ctx context.Context, client TimeSeriesCreator, req *monitoringpb.CreateTimeSeriesRequest, bo *BackOffIntervals) error {
	attempt := 1

	err := backoff.Retry(func() error {
		if err := client.CreateTimeSeries(ctx, req); err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.Logger.Errorw("Error in CreateTimeSeries, Permission denided - Enable the Monitoring Metrics Writer IAM role for the Service Account", "attempt", attempt, "error", err)
			} else {
				log.Logger.Errorw("Error in CreateTimeSeries", "attempt", attempt, "error", err)
			}
			attempt++
			return err
		}
		return nil
	}, shortConstantBackOffPolicy(ctx, bo.ShortConstant))

	if err != nil {
		log.Logger.Errorw("CreateTimeSeries retry limit exceeded", "request", req)
		return err
	}
	return nil
}

// QueryTimeSeriesWithRetry decorates TimeSeriesQuerier.QueryTimeSeries with a retry mechanism.
func QueryTimeSeriesWithRetry(ctx context.Context, client TimeSeriesQuerier, req *monitoringpb.QueryTimeSeriesRequest, bo *BackOffIntervals) ([]*mrpb.TimeSeriesData, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeriesData
	)

	err := backoff.Retry(func() error {
		var err error
		res, err = client.QueryTimeSeries(ctx, req)
		if err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.Logger.Errorw("Error in QueryTimeSeries, Permission denied - Enable the Monitoring Viewer IAM role for the Service Account", "attempt", attempt, "error", err)
			} else {
				log.Logger.Errorw("Error in QueryTimeSeries", "attempt", attempt, "error", err)
			}
			attempt++
		}
		return err
	}, longExponentialBackOffPolicy(ctx, bo.LongExponential))
	if err != nil {
		log.Logger.Errorw("QueryTimeSeries retry limit exceeded", "request", req, "error", err, "attempt", attempt)
		return nil, err
	}
	return res, nil
}

// longExponentialBackOffPolicy returns a backoff policy with a One Minute MaxElapsedTime.
func longExponentialBackOffPolicy(ctx context.Context, initial time.Duration) backoff.BackOffContext {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = initial
	exp.MaxInterval = 15 * time.Second
	exp.MaxElapsedTime = time.Minute
	return backoff.WithContext(backoff.WithMaxRetries(exp, 4), ctx) // 4 retries = 5 total attempts
}

// shortConstantBackOffPolicy returns a backoff policy with 15s MaxElapsedTime.
func shortConstantBackOffPolicy(ctx context.Context, initial time.Duration) backoff.BackOffContext {
	constantBackoff := backoff.NewConstantBackOff(initial)
	return backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 2), ctx) // 2 retries = 3 total attempts
}
