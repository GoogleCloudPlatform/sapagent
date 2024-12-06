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

package reliability

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"flag"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tpb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/storage"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultBackoff      = cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond)
	defaultBucketHandle = fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "default-bucket",
				Name:       "/object.json",
			},
			Content: []byte("test content"),
		}}).Client().Bucket("default-bucket")
)

func TestExecuteReliability(t *testing.T) {
	tests := []struct {
		name string
		r    Reliability
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailAssertSecondArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgs",
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "FailConnectToBucket",
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			r: Reliability{
				bucketName: "does_not_exist",
			},
		},
		{
			name: "FailCreateQueryClient",
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			r: Reliability{
				serviceAccount: "does_not_exist",
			},
		},
		{
			name: "SuccessForHelp",
			r: Reliability{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.r, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForReliability(t *testing.T) {
	want := "read reliability data from Cloud Monitoring"
	r := Reliability{}
	got := r.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForReliability(t *testing.T) {
	r := Reliability{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"project", "o", "bucket", "service-account", "h", "loglevel", "log-path"}
	r.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestReliabilityHandler(t *testing.T) {
	tests := []struct {
		name   string
		r      Reliability
		copier storage.IOFileCopier
		want   subcommands.ExitStatus
	}{
		{
			name: "NoOutputFolder",
			want: subcommands.ExitFailure,
		},
		{
			name: "QueryFailure",
			r: Reliability{
				outputFolder: t.TempDir(),
				queries:      defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{
						Err: fmt.Errorf("query failure"),
					},
					BackOffs: defaultBackoff,
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "ParseFailure",
			r: Reliability{
				outputFolder: t.TempDir(),
				queries:      defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{
						TS: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{}}},
					BackOffs: defaultBackoff,
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "WriteFailure",
			r: Reliability{
				outputFolder: t.TempDir(),
				queries:      []queryInfo{{identifier: "identifier/with/forward/slashes"}},
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{},
					BackOffs:    defaultBackoff,
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "UploadFailure",
			r: Reliability{
				outputFolder: t.TempDir(),
				queries:      defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{},
					BackOffs:    defaultBackoff,
				},
				bucket: defaultBucketHandle,
			},
			copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
				return 0, cmpopts.AnyError
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "QueryAndWriteSuccess",
			r: Reliability{
				outputFolder: t.TempDir(),
				queries:      defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{},
					BackOffs:    defaultBackoff,
				},
			},
			want: subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		test.r.oteLogger = onetime.CreateOTELogger(false)
		t.Run(test.name, func(t *testing.T) {
			got := test.r.reliabilityHandler(context.Background(), test.copier)
			if got != test.want {
				t.Errorf("reliabilityHandler()=%v want %v", got, test.want)
			}
		})
	}
}

func TestExecuteQuery(t *testing.T) {
	tests := []struct {
		name string
		r    Reliability
		want error
	}{
		{
			name: "FailedToQuery",
			r: Reliability{
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{
						Err: fmt.Errorf("query failure"),
					},
					BackOffs: defaultBackoff,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SuccessfulQuery",
			r: Reliability{
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{},
					BackOffs:    defaultBackoff,
				},
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := test.r.executeQuery(context.Background(), "test", "testQuery")
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("executeQuery()=%v want %v", got, test.want)
			}
		})
	}
}

func TestParseData(t *testing.T) {
	tests := []struct {
		name       string
		wantLabels []string
		data       []*mrpb.TimeSeriesData
		want       [][]string
		wantError  error
	}{
		{
			name: "NoData",
			want: [][]string{{"label", "value", "start_time", "end_time"}},
		},
		{
			name:       "IncorrectLabels",
			wantLabels: []string{"test"},
			data:       []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{}},
			wantError:  cmpopts.AnyError,
		},
		{
			name:       "ProperParseSameValue",
			wantLabels: []string{"test"},
			data: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{
				LabelValues: []*mrpb.LabelValue{{Value: &mrpb.LabelValue_StringValue{StringValue: "test"}}},
				PointData: []*mrpb.TimeSeriesData_PointData{
					{
						Values:       []*cpb.TypedValue{{Value: &cpb.TypedValue_Int64Value{Int64Value: 1}}},
						TimeInterval: &cpb.TimeInterval{StartTime: &tpb.Timestamp{Seconds: 2}, EndTime: &tpb.Timestamp{Seconds: 2}},
					},
					{
						Values:       []*cpb.TypedValue{{Value: &cpb.TypedValue_Int64Value{Int64Value: 1}}},
						TimeInterval: &cpb.TimeInterval{StartTime: &tpb.Timestamp{Seconds: 1}, EndTime: &tpb.Timestamp{Seconds: 1}},
					},
				},
			}},
			want: [][]string{
				{"label", "value", "start_time", "end_time"},
				{"test", "1", "1", "2"},
			},
		},
		{
			name:       "ProperParseDifferentValues",
			wantLabels: []string{"test"},
			data: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{
				LabelValues: []*mrpb.LabelValue{{Value: &mrpb.LabelValue_StringValue{StringValue: "test"}}},
				PointData: []*mrpb.TimeSeriesData_PointData{
					{
						Values:       []*cpb.TypedValue{{Value: &cpb.TypedValue_Int64Value{Int64Value: 2}}},
						TimeInterval: &cpb.TimeInterval{StartTime: &tpb.Timestamp{Seconds: 2}, EndTime: &tpb.Timestamp{Seconds: 2}},
					},
					{
						Values:       []*cpb.TypedValue{{Value: &cpb.TypedValue_Int64Value{Int64Value: 1}}},
						TimeInterval: &cpb.TimeInterval{StartTime: &tpb.Timestamp{Seconds: 1}, EndTime: &tpb.Timestamp{Seconds: 1}},
					},
				},
			}},
			want: [][]string{
				{"label", "value", "start_time", "end_time"},
				{"test", "2", "2", "2"},
				{"test", "1", "1", "1"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r Reliability
			got, err := r.parseData(context.Background(), []string{"label", "value", "start_time", "end_time"}, tc.wantLabels, tc.data)
			if !cmp.Equal(err, tc.wantError, cmpopts.EquateErrors()) {
				t.Fatalf("parseData(%v, %v) returned an unexpected error: %v", tc.wantLabels, tc.data, err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("parseData(%v, %v) returned an unexpected diff (-want +got): %v", tc.wantLabels, tc.data, diff)
			}
		})
	}
}

func TestWriteResults(t *testing.T) {
	tests := []struct {
		name    string
		r       Reliability
		results [][]string
		want    error
	}{
		{
			name: "NoData",
			r: Reliability{
				outputFolder: t.TempDir(),
			},
			want: nil,
		},
		{
			name: "FailedToWrite",
			r: Reliability{
				outputFolder: t.TempDir() + "/does_not_exist",
			},
			results: [][]string{{"test"}},
			want:    cmpopts.AnyError,
		},
		{
			name: "SuccessfulWrite",
			r: Reliability{
				outputFolder: t.TempDir(),
			},
			results: [][]string{{"test"}},
			want:    nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := test.r.writeResults(test.results, "test")
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("writeResults(%v)=%v want %v", test.results, got, test.want)
			}
		})
	}
}

func TestUploadFile(t *testing.T) {
	tests := []struct {
		name     string
		r        Reliability
		fileName string
		copier   storage.IOFileCopier
		want     error
	}{
		{
			name: "NoBucket",
			want: cmpopts.AnyError,
		},
		{
			name: "NoFile",
			r: Reliability{
				bucket: defaultBucketHandle,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "UploadFailure",
			r: Reliability{
				bucket: defaultBucketHandle,
			},
			fileName: t.TempDir() + "/upload_failure.json",
			copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
				return 0, fmt.Errorf("upload failure")
			},
			want: cmpopts.AnyError,
		},
		{
			name: "UploadSuccess",
			r: Reliability{
				bucket: defaultBucketHandle,
			},
			fileName: t.TempDir() + "/object.json",
			copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
				return 0, nil
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.fileName != "" {
				if err := os.WriteFile(test.fileName, []byte("test content"), os.ModePerm); err != nil {
					t.Fatalf("Failed to write file %v: %v", test.fileName, err)
				}
			}

			got := test.r.uploadFile(context.Background(), test.fileName, test.copier)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("uploadFile(%v)=%v want %v", test.fileName, got, test.want)
			}
		})
	}
}
