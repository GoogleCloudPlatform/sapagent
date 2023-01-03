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
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/googleapis/gax-go/v2"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

const initialBackoffInterval = 2 * time.Second

// TimeSeriesCreator provides an easily testable translation to the cloud monitoring API.
type TimeSeriesCreator interface {
	CreateTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest, opts ...gax.CallOption) error
}

// TimeSeriesLister provides an easily testable translation to the cloud monitoring API.
type TimeSeriesLister interface {
	ListTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest, opts ...gax.CallOption) ([]*mrpb.TimeSeries, error)
}

// TimeSeriesQuerier provides an easily testable translation to the cloud monitoring API.
type TimeSeriesQuerier interface {
	QueryTimeSeries(ctx context.Context, req *monitoringpb.QueryTimeSeriesRequest, opts ...gax.CallOption) ([]*mrpb.TimeSeriesData, error)
}

// CreateTimeSeriesWithRetry decorates TimeSeriesCreator.CreateTimeSeries with a retry mechanism.
func CreateTimeSeriesWithRetry(ctx context.Context, client TimeSeriesCreator, req *monitoringpb.CreateTimeSeriesRequest) error {
	attempt := 1

	err := backoff.Retry(func() error {
		if err := client.CreateTimeSeries(ctx, req); err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.Logger.Error(fmt.Sprintf("Error in CreateTimeSeries (attempt %d), Permission denided - Enable the Monitoring Metrics Writer IAM role for the Service Account", attempt), log.Error(err))
			} else {
				log.Logger.Error(fmt.Sprintf("Error in CreateTimeSeries (attempt %d)", attempt), log.Error(err))
			}
			attempt++
			return err
		}
		return nil
	}, shortConstantBackOffPolicy(ctx))

	if err != nil {
		log.Logger.Errorf("CreateTimeSeries retry limit exceeded. req: %v", req)
		return err
	}
	return nil
}

// ListTimeSeriesWithRetry decorates TimeSeriesLister.ListTimeSeries with a retry mechanism.
func ListTimeSeriesWithRetry(ctx context.Context, client TimeSeriesLister, req *monitoringpb.ListTimeSeriesRequest) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)

	err := backoff.Retry(func() error {
		var err error
		res, err = client.ListTimeSeries(ctx, req)
		if err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.Logger.Error(fmt.Sprintf("Error in ListTimeSeries (attempt %d), Permission denied - Enable the Monitoring Viewer IAM role for the Service Account", attempt), log.Error(err))
			} else {
				log.Logger.Error(fmt.Sprintf("Error in ListTimeSeries (attempt %d)", attempt), log.Error(err))
			}
			attempt++
		}
		return err
	}, longExponentialBackOffPolicy(ctx))
	if err != nil {
		log.Logger.Errorf("ListTimeSeries retry limit exceeded. req: %v", req)
		return nil, err
	}
	return res, nil
}

// QueryTimeSeriesWithRetry decorates TimeSeriesQuerier.QueryTimeSeries with a retry mechanism.
func QueryTimeSeriesWithRetry(ctx context.Context, client TimeSeriesQuerier, req *monitoringpb.QueryTimeSeriesRequest) ([]*mrpb.TimeSeriesData, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeriesData
	)

	err := backoff.Retry(func() error {
		var err error
		res, err = client.QueryTimeSeries(ctx, req)
		if err != nil {
			if strings.Contains(err.Error(), "PermissionDenied") {
				log.Logger.Error(fmt.Sprintf("Error in QueryTimeSeries (attempt %d), Permission denied - Enable the Monitoring Viewer IAM role for the Service Account", attempt), log.Error(err))
			} else {
				log.Logger.Error(fmt.Sprintf("Error in QueryTimeSeries (attempt %d)", attempt), log.Error(err))
			}
			attempt++
		}
		return err
	}, longExponentialBackOffPolicy(ctx))
	if err != nil {
		log.Logger.Errorf("QueryTimeSeries retry limit exceeded. req: %v", req)
		return nil, err
	}
	return res, nil
}

// longExponentialBackOffPolicy returns a backoff policy with a One Minute MaxElapsedTime.
func longExponentialBackOffPolicy(ctx context.Context) backoff.BackOffContext {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = initialBackoffInterval
	exp.MaxInterval = 15 * time.Second
	exp.MaxElapsedTime = time.Minute
	return backoff.WithContext(backoff.WithMaxRetries(exp, 4), ctx) // 4 retries = 5 total attempts
}

// shortConstantBackOffPolicy returns a backoff policy with 15s MaxElapsedTime.
func shortConstantBackOffPolicy(ctx context.Context) backoff.BackOffContext {
	constantBackoff := backoff.NewConstantBackOff(5 * time.Second)
	return backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 2), ctx) // 2 retries = 3 total attempts
}
