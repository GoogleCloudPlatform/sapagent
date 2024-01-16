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

package maintenance

import (
	"context"
	"os"
	"testing"
	"time"

	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	backoff "github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/googleapis/gax-go/v2"
	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	mockedFileReader struct {
		expectedData []byte
		expectedErr  error
	}

	mockedFileWriter struct {
		expectedErrForMakeDirs error
		expectedErrForWrite    error
	}

	fakeTimeSeriesCreator struct {
		calls []*mpb.CreateTimeSeriesRequest
	}
)

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func (f *fakeTimeSeriesCreator) CreateTimeSeries(ctx context.Context, req *mpb.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
	f.calls = append(f.calls, req)
	return nil
}

func (mfr mockedFileReader) Read(name string) ([]byte, error) {
	return mfr.expectedData, mfr.expectedErr
}

func (mfw mockedFileWriter) Write(name string, data []byte, perm os.FileMode) error {
	return mfw.expectedErrForWrite
}

func (mfw mockedFileWriter) MakeDirs(path string, perm os.FileMode) error {
	return mfw.expectedErrForMakeDirs
}

var (
	defaultConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:   false,
			ProcessMetricsFrequency: 5,
		},
		CloudProperties: &iipb.CloudProperties{
			ProjectId:        "test-project",
			InstanceId:       "test-instance",
			Zone:             "test-zone",
			InstanceName:     "test-instance",
			Image:            "test-image",
			NumericProjectId: "123456",
		},
	}
)

func TestReadMaintenanceMode(t *testing.T) {
	tests := []struct {
		name    string
		fr      FileReader
		want    []string
		wantErr error
	}{
		{
			name: "FileDoesNotExist",
			fr: mockedFileReader{
				expectedData: []byte(""),
				expectedErr:  os.ErrNotExist,
			},
			want:    nil,
			wantErr: nil,
		},
		{
			name: "PermissionDenied",
			fr: mockedFileReader{
				expectedData: []byte(`{"sids":["deh"]}`),
				expectedErr:  os.ErrPermission,
			},
			want:    nil,
			wantErr: os.ErrPermission,
		},
		{
			name: "EmptyFile",
			fr: mockedFileReader{
				expectedData: []byte(""),
				expectedErr:  nil,
			},
			want:    nil,
			wantErr: nil,
		},
		{
			name: "MaintenanceModeOn",
			fr: mockedFileReader{
				expectedData: []byte(`{"sids":["deh"]}`),
				expectedErr:  nil,
			},
			want:    []string{"deh"},
			wantErr: nil,
		},
		{
			name: "MaintenanceModeOff",
			fr: mockedFileReader{
				expectedData: []byte(`{"sids":[]}`),
				expectedErr:  nil,
			},
			want:    []string{},
			wantErr: nil,
		},
		{
			name: "MalformedMaintenanceModeJSON",
			fr: mockedFileReader{
				expectedData: []byte(`{"sids":"abc"`),
				expectedErr:  nil,
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ReadMaintenanceMode(test.fr)
			if !slices.Equal(got, test.want) || !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Got (%v, %s) != Want (%v, %s)", got, gotErr, test.want, test.wantErr)
			}
		})
	}
}

func TestUpdateMaintenanceMode(t *testing.T) {
	tests := []struct {
		name    string
		fr      mockedFileReader
		fw      mockedFileWriter
		mntMode bool
		sid     string
		want    []string
		wantErr error
	}{
		{
			name: "CouldNotReadMaintenanceJsonFile",
			fr: mockedFileReader{
				expectedErr:  os.ErrPermission,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw:      mockedFileWriter{},
			mntMode: true,
			sid:     "deh",
			want:    nil,
			wantErr: os.ErrPermission,
		},
		{
			name: "CreatingDirectoriesNotAllowed",
			fr: mockedFileReader{
				expectedErr:  nil,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw: mockedFileWriter{
				expectedErrForMakeDirs: os.ErrPermission,
				expectedErrForWrite:    nil,
			},
			mntMode: true,
			sid:     "abc",
			want:    nil,
			wantErr: os.ErrPermission,
		},
		{
			name: "WritingFileNotAllowed",
			fr: mockedFileReader{
				expectedErr:  nil,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw: mockedFileWriter{
				expectedErrForMakeDirs: nil,
				expectedErrForWrite:    os.ErrPermission,
			},
			mntMode: true,
			sid:     "abc",
			want:    nil,
			wantErr: os.ErrPermission,
		},
		{
			name: "DisableMaintenanceModeNoUpdateNeeded",
			fr: mockedFileReader{
				expectedErr:  nil,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw: mockedFileWriter{
				expectedErrForMakeDirs: nil,
				expectedErrForWrite:    nil,
			},
			mntMode: false,
			sid:     "abc",
			want:    []string{"deh"},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EnableMaintenanceModeNoUpdateNeeded",
			fr: mockedFileReader{
				expectedErr:  nil,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw: mockedFileWriter{
				expectedErrForMakeDirs: nil,
				expectedErrForWrite:    nil,
			},
			mntMode: true,
			sid:     "deh",
			want:    []string{"deh"},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DisableMaintenanceMode",
			fr: mockedFileReader{
				expectedErr:  nil,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw:      mockedFileWriter{expectedErrForMakeDirs: nil, expectedErrForWrite: nil},
			mntMode: false,
			sid:     "deh",
			want:    []string{},
			wantErr: nil,
		},
		{
			name: "EnableMaintenanceMode",
			fr: mockedFileReader{
				expectedErr:  nil,
				expectedData: []byte(`{"sids":["deh"]}`),
			},
			fw:      mockedFileWriter{expectedErrForMakeDirs: nil, expectedErrForWrite: nil},
			mntMode: true,
			sid:     "abc",
			want:    []string{"deh", "abc"},
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := UpdateMaintenanceMode(test.mntMode, test.sid, test.fr, test.fw)
			if !slices.Equal(got, test.want) || !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Got (%s,%s) != Want (%s,%s)", got, gotErr, test.want, test.wantErr)
			}
		})
	}
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name           string
		config         *cpb.Configuration
		fr             mockedFileReader
		sids           map[string]bool
		skippedMetrics map[string]bool
		wantCount      int
		trueCount      int
		wantErr        error
	}{
		{
			name:      "CannotReadMaintenanceModeJSONFile",
			config:    defaultConfig,
			fr:        mockedFileReader{expectedErr: os.ErrPermission},
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
			trueCount: 0,
		},
		{
			name:      "CanReadMaintenanceModeJSONFile",
			config:    defaultConfig,
			fr:        mockedFileReader{expectedData: []byte(`{"sids":["deh"]}`)},
			sids:      map[string]bool{"deh": true, "abc": true},
			wantCount: 2,
			trueCount: 1,
		},
		{
			name: "SkippedMetric",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{mntmodePath},
				},
			},
			skippedMetrics: map[string]bool{
				mntmodePath: true,
			},
			fr:        mockedFileReader{expectedData: []byte(`{"sids":["deh"]}`)},
			sids:      map[string]bool{"deh": true, "abc": true},
			wantCount: 0,
			trueCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testInstanceProperties := &InstanceProperties{
				Config:         test.config,
				Client:         &fakeTimeSeriesCreator{},
				Reader:         test.fr,
				Sids:           test.sids,
				SkippedMetrics: test.skippedMetrics,
			}
			got, gotErr := testInstanceProperties.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Got (%d) != Want (%d)", len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Got (%v) != Want (%v)", gotErr, test.wantErr)
			}
			trueCount := 0
			for _, v := range got {
				if v.GetPoints()[0].GetValue().GetBoolValue() {
					trueCount++
				}
			}
			if trueCount != test.trueCount {
				t.Errorf("Got TrueCount (%d) != WantTrueCount (%d)", trueCount, test.trueCount)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name string
		list []string
		item string
		want bool
	}{
		{
			name: "ReturnsTrue",
			list: []string{"hdbdaemon", "hdbnameserver"},
			item: "hdbdaemon",
			want: true,
		},
		{name: "ReturnsFalse",
			list: []string{"hdbdaemon", "hdbnameserver"},
			item: "random",
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := contains(test.list, test.item)
			if got != test.want {
				t.Errorf("contains(%v, %v) returned unexpected value, got=%t, want=%t", test.list, test.item, got, test.want)
			}
		})
	}
}

func TestIndexOf(t *testing.T) {
	tests := []struct {
		name string
		list []string
		item string
		want int
	}{
		{
			name: "ReturnsIndex",
			list: []string{"hdbdaemon", "hdbnameserver"},
			item: "hdbdaemon",
			want: 0,
		},
		{
			name: "IndexNotFound",
			list: []string{"hdbdaemon", "hdbnameserver"},
			item: "random",
			want: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := indexOf(tc.list, tc.item)
			if got != tc.want {
				t.Errorf("indexOf(%v, %v) = %v, want: %v", tc.list, tc.item, got, tc.want)
			}
		})
	}
}

func TestCollectWithRetry(t *testing.T) {
	c := context.Background()
	p := &InstanceProperties{
		Config: defaultConfig,
		Client: &fakeTimeSeriesCreator{},
		Reader: mockedFileReader{
			expectedData: []byte(`{"sids":["deh"]}`),
			expectedErr:  os.ErrPermission,
		},
		PMBackoffPolicy: defaultBOPolicy(c),
	}
	_, err := p.CollectWithRetry(c)
	if err == nil {
		t.Errorf("CollectWithRetry() = nil, want error")
	}
}
