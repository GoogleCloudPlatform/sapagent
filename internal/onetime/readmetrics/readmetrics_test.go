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

package readmetrics

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"flag"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultQueries = map[string]string{
		"default_hana_availability":    defaultHanaAvailability,
		"default_hana_ha_availability": defaultHanaHAAvailability,
	}
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

func TestExecuteReadMetrics(t *testing.T) {
	tests := []struct {
		name string
		r    ReadMetrics
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
			name: "FailCreateQueryMap",
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			r: ReadMetrics{
				inputFile: "does_not_exist",
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
			r: ReadMetrics{
				bucketName: "does_not_exist",
			},
		},
		{
			name: "FailCreateMetricClient",
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			r: ReadMetrics{
				serviceAccount: "does_not_exist",
			},
		},
		{
			name: "SuccessForHelp",
			r: ReadMetrics{
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

func TestSynopsisForReadMetrics(t *testing.T) {
	want := "read metrics from Cloud Monitoring"
	r := ReadMetrics{}
	got := r.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForReadMetrics(t *testing.T) {
	r := ReadMetrics{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"project", "i", "o", "send-status-to-monitoring", "bucket", "service-account", "h", "loglevel", "log-path"}
	r.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestReadMetricsHandler(t *testing.T) {
	tests := []struct {
		name   string
		r      ReadMetrics
		copier storage.IOFileCopier
		want   subcommands.ExitStatus
	}{
		{
			name: "NoOutputFolder",
			want: subcommands.ExitFailure,
		},
		{
			name: "NoQueries",
			r: ReadMetrics{
				outputFolder: t.TempDir(),
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "EmptyQuery",
			r: ReadMetrics{
				outputFolder: t.TempDir(),
				queries:      map[string]string{"test": ""},
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "QueryFailure",
			r: ReadMetrics{
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
			name: "WriteFailure",
			r: ReadMetrics{
				outputFolder: t.TempDir(),
				queries:      map[string]string{"identifier/with/forward/slashes": "testQuery"},
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{},
					BackOffs:    defaultBackoff,
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "UploadFailure",
			r: ReadMetrics{
				outputFolder: t.TempDir(),
				queries:      defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{
						TS: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{}}},
					BackOffs: defaultBackoff,
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
			r: ReadMetrics{
				outputFolder: t.TempDir(),
				queries:      defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{
						TS: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{}}},
					BackOffs: defaultBackoff,
				},
			},
			want: subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.readMetricsHandler(context.Background(), test.copier)
			if got != test.want {
				t.Errorf("readMetricsHandler()=%v want %v", got, test.want)
			}
		})
	}
}

func TestCreateQueryMap(t *testing.T) {
	tests := []struct {
		name     string
		r        ReadMetrics
		fileData string
		want     map[string]string
		wantErr  error
	}{
		{
			name:    "NoInputFile",
			r:       ReadMetrics{},
			want:    defaultQueries,
			wantErr: nil,
		},
		{
			name: "ReadFileError",
			r: ReadMetrics{
				inputFile: "does_not_exist",
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UnmarshallError",
			r: ReadMetrics{
				inputFile: t.TempDir() + "/UnmarshallError.json",
			},
			fileData: `{"key":"value`,
			want:     nil,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "SuccessOverrideDefaults",
			r: ReadMetrics{
				inputFile: t.TempDir() + "/SuccessOverrideDefaults.json",
			},
			fileData: `{"default_hana_availability":"", "default_hana_ha_availability": ""}`,
			want: map[string]string{
				"default_hana_availability":    "",
				"default_hana_ha_availability": "",
			},
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.fileData != "" {
				if err := os.WriteFile(test.r.inputFile, []byte(test.fileData), os.ModePerm); err != nil {
					t.Fatalf("Failed to write file %v: %v", test.r.inputFile, err)
				}
			}

			got, gotErr := test.r.createQueryMap()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("createQueryMap() had unexpected diff: (-want +got):\n%s", diff)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("createQueryMap()=%v want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestExecuteQuery(t *testing.T) {
	tests := []struct {
		name string
		r    ReadMetrics
		want error
	}{
		{
			name: "FailedToQuery",
			r: ReadMetrics{
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
			r: ReadMetrics{
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

func TestWriteResults(t *testing.T) {
	tests := []struct {
		name string
		r    ReadMetrics
		data []*mrpb.TimeSeriesData
		want error
	}{
		{
			name: "NoData",
			r: ReadMetrics{
				outputFolder: t.TempDir(),
			},
			want: nil,
		},
		{
			name: "FailedToWrite",
			r: ReadMetrics{
				outputFolder: t.TempDir() + "/does_not_exist",
			},
			data: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{}},
			want: cmpopts.AnyError,
		},
		{
			name: "SuccessfulWrite",
			r: ReadMetrics{
				outputFolder: t.TempDir(),
			},
			data: []*mrpb.TimeSeriesData{&mrpb.TimeSeriesData{}},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := test.r.writeResults(test.data, "test")
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("writeResults(%v)=%v want %v", test.data, got, test.want)
			}
		})
	}
}

func TestUploadFile(t *testing.T) {
	tests := []struct {
		name     string
		r        ReadMetrics
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
			r: ReadMetrics{
				bucket: defaultBucketHandle,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "UploadFailure",
			r: ReadMetrics{
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
			r: ReadMetrics{
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

func TestSendStatusToMonitoring(t *testing.T) {
	tests := []struct {
		name string
		r    ReadMetrics
		want bool
	}{
		{
			name: "DontSendToMonitoring",
			want: false,
		},
		{
			name: "FailedSendToMonitoring",
			r: ReadMetrics{
				sendToMonitoring: true,
				timeSeriesCreator: &fake.TimeSeriesCreator{
					Err: fmt.Errorf("failure"),
				},
			},
			want: false,
		},
		{
			name: "Success",
			r: ReadMetrics{
				sendToMonitoring:  true,
				timeSeriesCreator: &fake.TimeSeriesCreator{},
			},
			want: true,
		},
	}
	for _, test := range tests {
		got := test.r.sendStatusToMonitoring(context.Background(), cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond))
		if got != test.want {
			t.Errorf("sendStatusToMonitoring() = %v, want: %v", got, test.want)
		}
	}
}
