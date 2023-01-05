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
	"errors"
	"os"
	"testing"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/googleapis/gax-go/v2"
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
		calls []*monitoringpb.CreateTimeSeriesRequest
	}
)

func (f *fakeTimeSeriesCreator) CreateTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
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
			CollectProcessMetrics:       false,
			ProcessMetricsFrequency:     5,
			ProcessMetricsSendFrequency: 60,
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
		name        string
		fr          FileReader
		wantMntMode bool
		wantErr     error
	}{
		{
			name: "FileDoesNotExist",
			fr: mockedFileReader{
				expectedData: []byte(""),
				expectedErr:  os.ErrNotExist,
			},
			wantMntMode: false,
			wantErr:     nil,
		},
		{
			name: "PermissionDenied",
			fr: mockedFileReader{
				expectedData: []byte(`{"maintenance_mode":true}`),
				expectedErr:  os.ErrPermission,
			},
			wantMntMode: false,
			wantErr:     os.ErrPermission,
		},
		{
			name: "EmptyFile",
			fr: mockedFileReader{
				expectedData: []byte(""),
				expectedErr:  nil,
			},
			wantMntMode: false,
			wantErr:     nil,
		},
		{
			name: "MaintenanceModeOn",
			fr: mockedFileReader{
				expectedData: []byte(`{"maintenance_mode":true}`),
				expectedErr:  nil,
			},
			wantMntMode: true,
			wantErr:     nil,
		},
		{
			name: "MaintenanceModeOff",
			fr: mockedFileReader{
				expectedData: []byte(`{"maintenance_mode":false}`),
				expectedErr:  nil,
			},
			wantMntMode: false,
			wantErr:     nil,
		},
		{
			name: "MalformedMaintenanceModeJSON",
			fr: mockedFileReader{
				expectedData: []byte(`{"maintnenace_mode":false`),
				expectedErr:  nil,
			},
			wantMntMode: false,
			wantErr:     errors.New("unexpected end of JSON input"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMntMode, gotErr := ReadMaintenanceMode(test.fr)
			if gotMntMode != test.wantMntMode || !cmpErrs(gotErr, test.wantErr) {
				t.Errorf("Got (%v, %s) != Want (%v, %s)", gotMntMode, gotErr, test.wantMntMode, test.wantErr)
			}
		})
	}
}

func TestUpdateMaintenanceMode(t *testing.T) {
	tests := []struct {
		name    string
		fw      mockedFileWriter
		mntMode bool
		wantErr error
	}{
		{
			name: "CreatingDirectoriesNotAllowed",
			fw: mockedFileWriter{
				expectedErrForMakeDirs: os.ErrPermission,
				expectedErrForWrite:    nil,
			},
			mntMode: true,
			wantErr: os.ErrPermission,
		},
		{
			name: "WritingFileNotAllowed",
			fw: mockedFileWriter{
				expectedErrForMakeDirs: nil,
				expectedErrForWrite:    os.ErrPermission,
			},
			mntMode: true,
			wantErr: os.ErrPermission,
		},
		{
			name: "WriteFileSuccessfully",
			fw: mockedFileWriter{
				expectedErrForMakeDirs: nil,
				expectedErrForWrite:    nil,
			},
			mntMode: true,
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotErr := UpdateMaintenanceMode(test.mntMode, test.fw)
			if gotErr != test.wantErr {
				t.Errorf("Got (%s) != Want (%s)", gotErr, test.wantErr)
			}
		})
	}
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name      string
		readErr   error
		wantCount int
	}{
		{
			name:      "CannotReadMaintenanceModeJSONFile",
			readErr:   os.ErrPermission,
			wantCount: 0,
		},
		{
			name:      "CanReadMaintenanceModeJSONFile",
			readErr:   nil,
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testInstanceProperties := &InstanceProperties{
				Config: defaultConfig,
				Client: &fakeTimeSeriesCreator{},
				Reader: mockedFileReader{expectedErr: test.readErr},
			}
			got := testInstanceProperties.Collect()
			if len(got) != test.wantCount {
				t.Errorf("Got (%d) != Want (%d)", len(got), test.wantCount)
			}
		})
	}
}

// cmpErrs compares two errors and returns true if they are same errors
func cmpErrs(got, want error) bool {
	if got == want {
		return true
	}
	if got == nil || want == nil {
		return false
	}
	if got.Error() != want.Error() {
		return false
	}
	return true
}
