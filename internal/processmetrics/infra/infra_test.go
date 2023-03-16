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

package infra

import (
	"context"
	"errors"
	"testing"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

var (
	defaultConfig = &cpb.Configuration{}

	defaultProperties = &Properties{Config: defaultConfig}
)

func TestCollect(t *testing.T) {
	tests := []struct {
		name                   string
		properties             *Properties
		fakeMetadataServerCall func() (string, error)
		wantCount              int
	}{
		{
			name:       "bareMetal",
			properties: &Properties{Config: &cpb.Configuration{BareMetal: true}},
			wantCount:  0,
		},
		{
			name:                   "GCE",
			properties:             defaultProperties,
			fakeMetadataServerCall: func() (string, error) { return "", nil },
			wantCount:              1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadataServerCall = test.fakeMetadataServerCall
			got := test.properties.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect() returned unexpected metric count: got=%d, want=%d", len(got), test.wantCount)
			}
		})
	}
}

func TestCollectScheduledMigration_MetricCount(t *testing.T) {
	tests := []struct {
		name                   string
		fakeMetadataServerCall func() (string, error)
		wantCount              int
	}{
		{
			name:                   "noMigration",
			fakeMetadataServerCall: func() (string, error) { return "NONE", nil },
			wantCount:              1,
		},
		{
			name:                   "scheduledMigration",
			fakeMetadataServerCall: func() (string, error) { return metadataMigrationResponse, nil },
			wantCount:              1,
		},
		{
			name:                   "error",
			fakeMetadataServerCall: func() (string, error) { return "", errors.New("Error") },
			wantCount:              0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectScheduledMigration(defaultProperties, test.fakeMetadataServerCall)
			// Test one metric is exported in case of successful call to the metadata server.
			if len(got) != test.wantCount {
				t.Errorf("collectScheduledMigration() returned unexpected metric count: got=%d, want=%d", len(got), test.wantCount)
			}
		})
	}
}

func TestCollectScheduledMigration_MetricValue(t *testing.T) {
	tests := []struct {
		name                   string
		fakeMetadataServerCall func() (string, error)
		wantValue              int64
	}{
		{
			name:                   "noMigration",
			fakeMetadataServerCall: func() (string, error) { return "NONE", nil },
			wantValue:              0,
		},
		{
			name:                   "scheduledMigration",
			fakeMetadataServerCall: func() (string, error) { return metadataMigrationResponse, nil },
			wantValue:              1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectScheduledMigration(defaultProperties, test.fakeMetadataServerCall)
			if len(got) != 1 {
				t.Fatalf("collectScheduledMigration() returned unexpected metric count: got=%d, want=%d", len(got), 1)
			}
			gotPointsCount := len(got[0].TimeSeries.Points)
			if gotPointsCount != 1 {
				t.Fatalf("collectScheduledMigration() returned unexpected metric.timeseries.points count: got=%d, want=%d", gotPointsCount, 1)
			}
			// Test exported metric value.
			gotValue := got[0].TimeSeries.Points[0].GetValue().GetInt64Value()
			if gotValue != test.wantValue {
				t.Errorf("collectScheduledMigration() returned unexpected metric.timeseries.point value: got=%d, want=%d", gotValue, test.wantValue)
			}
		})
	}
}

func TestCollectScheduledMigration_MetricType(t *testing.T) {
	want := "workload.googleapis.com/sap/infra/migration"
	got := collectScheduledMigration(defaultProperties, func() (string, error) { return "", nil })
	if len(got) != 1 {
		t.Fatalf("collectScheduledMigration() returned unexpected metric count: got=%d, want=%d", len(got), 1)
	}
	if got[0].TimeSeries.Metric == nil {
		t.Fatal("collectScheduledMigration() returned unexpected metric.timeseries.metric: nil")
	}
	// Test exported metric type.
	gotType := got[0].TimeSeries.Metric.GetType()
	if gotType != want {
		t.Errorf("collectScheduledMigration() returned unexpected metric type: got=%s, want=%s", gotType, want)
	}
}
