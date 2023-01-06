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

package main

import (
	"flag"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// (TODO: b/246271686): Enhance tests in main_test.go
// - Test to ensure services return control to startServices() asynchrounously.
// - Test to cover provideSapHostAgentMetrics and LogToFile.

type (
	mockedFileReader struct {
		expectedData []byte
		expectedErr  error
	}

	mockedFileWriter struct {
		expectedErrForMakeDirs error
		expectedErrForWrite    error
	}
)

var (
	defaultCP *iipb.CloudProperties = &iipb.CloudProperties{
		ProjectId:        "test-project",
		InstanceId:       "test-instanceId",
		Zone:             "test-zone",
		InstanceName:     "test-instanceName",
		Image:            "test-image",
		NumericProjectId: "12345",
	}
)

func (mfr mockedFileReader) Read(name string) ([]byte, error) {
	return mfr.expectedData, mfr.expectedErr
}

func (mfw mockedFileWriter) Write(name string, data []byte, perm os.FileMode) error {
	return mfw.expectedErrForWrite
}

func (mfw mockedFileWriter) MakeDirs(path string, perm os.FileMode) error {
	return mfw.expectedErrForMakeDirs
}

func TestSetupFlagsAndParse(t *testing.T) {
	tests := []struct {
		name    string
		fs      *flag.FlagSet
		osArgs  []string
		pattern string
		mfr     mockedFileReader
		mfw     mockedFileWriter
		wantErr error
	}{
		{
			name:    "HasHelp",
			fs:      flag.NewFlagSet("help", flag.ExitOnError),
			osArgs:  []string{"HasHelpFlagSet", "-help"},
			pattern: "help",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: errQuiet,
		},
		{
			name:    "HasConfig",
			fs:      flag.NewFlagSet("config", flag.ExitOnError),
			osArgs:  []string{"HasConfigFlagSet", "-config", "path/to/file"},
			pattern: "config",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: nil,
		},
		{
			name:    "HasConfigShortFlag",
			fs:      flag.NewFlagSet("config", flag.ExitOnError),
			osArgs:  []string{"HasConfigFlagSet", "-c", "path/to/file"},
			pattern: "config",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: nil,
		},
		{
			name:    "HasMaintenanceMode",
			fs:      flag.NewFlagSet("maintenancemode", flag.ExitOnError),
			osArgs:  []string{"HasMaintenanceModeFlagSet", "-maintenancemode", "true"},
			pattern: "maintenancemode",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: errQuiet,
		},
		{
			name:    "HasMaintenanceModeShortFlag",
			fs:      flag.NewFlagSet("mm", flag.ExitOnError),
			osArgs:  []string{"HasMaintenanceModeShortFlagSet", "-mm", "true"},
			pattern: "mm",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: errQuiet,
		},
		{
			name:    "UpdateMaintenanceModeUnsuccessful",
			fs:      flag.NewFlagSet("mm", flag.ExitOnError),
			osArgs:  []string{"UpdateMaintenanceModeUnsuccessfulFlagSet", "-mm", "true"},
			pattern: "mm",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: os.ErrPermission, expectedErrForMakeDirs: nil},
			wantErr: os.ErrPermission,
		},
		{
			name:    "HasMaintenanceModeShow",
			fs:      flag.NewFlagSet("maintenancemode-show", flag.ExitOnError),
			osArgs:  []string{"HasMaintenanceModeShowFlagSet", "-maintenancemode-show"},
			pattern: "maintenancemode-show",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: errQuiet,
		},
		{
			name:    "HasMaintenanceModeShowShortFlag",
			fs:      flag.NewFlagSet("mm-show", flag.ExitOnError),
			osArgs:  []string{"HasMaintenanceModeShowShortFlagSet", "-mm-show"},
			pattern: "mm-show",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: nil},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: errQuiet,
		},
		{
			name:    "ShowMaintenanceModeUnsuccessful",
			fs:      flag.NewFlagSet("mm-show", flag.ExitOnError),
			osArgs:  []string{"ShowMaintenanceModeUnsuccessfulFlagSet", "-mm-show"},
			pattern: "mm-show",
			mfr:     mockedFileReader{expectedData: []byte(`{"maintenance_mode":true}`), expectedErr: os.ErrPermission},
			mfw:     mockedFileWriter{expectedErrForWrite: nil, expectedErrForMakeDirs: nil},
			wantErr: os.ErrPermission,
		},
		{
			name:    "HasLogUsage",
			fs:      flag.NewFlagSet("log-usage", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-log-usage"},
			pattern: "log-usage",
			wantErr: errQuiet,
		},
		{
			name:    "HasLogUsageShortFlag",
			fs:      flag.NewFlagSet("lu", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu"},
			pattern: "lu",
			wantErr: errQuiet,
		},
		{
			name:    "HasLogUsagePriorVersion",
			fs:      flag.NewFlagSet("log-usage-prior-version", flag.ExitOnError),
			osArgs:  []string{"HasLogUsagePriorVersionFlagSet", "-lu", "-lus", "UPDATED", "-log-usage-prior-version", "2.6"},
			pattern: "log-usage-prior-version",
			wantErr: nil,
		},
		{
			name:    "HasLogUsagePriorVersionShortFlag",
			fs:      flag.NewFlagSet("lup", flag.ExitOnError),
			osArgs:  []string{"HasLogUsagePriorVersionFlagSet", "-lu", "-lus", "UPDATED", "-lup", "2.6"},
			pattern: "lup",
			wantErr: nil,
		},
		{
			name:    "HasEmptyLogUsagePriorVersion",
			fs:      flag.NewFlagSet("log-usage-prior-version", flag.ExitOnError),
			osArgs:  []string{"HasEmptyLogUsagePriorVersion", "-lu", "-log-usage-status", "UPDATED", "-lup", ""},
			pattern: "lup",
			wantErr: errQuiet,
		},
		{
			name:    "HasLogUsageStatus",
			fs:      flag.NewFlagSet("log-usage-status", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu", "-log-usage-status", "INSTALLED"},
			pattern: "log-usage-status",
			wantErr: nil,
		},
		{
			name:    "HasLogUsageStatusShortFlag",
			fs:      flag.NewFlagSet("lus", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu", "-lus", "INSTALLED"},
			pattern: "lus",
			wantErr: nil,
		},
		{
			name:    "HasEmptyLogUsageStatus",
			fs:      flag.NewFlagSet("log-usage-status", flag.ExitOnError),
			osArgs:  []string{"HasEmptyLogUsageStatus", "-lu", "-log-usage-status", ""},
			pattern: "log-usage-status",
			wantErr: errQuiet,
		},
		{
			name:    "HasLogUsageAction",
			fs:      flag.NewFlagSet("log-usage-action", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu", "-lus", "ACTION", "-log-usage-action", "1"},
			pattern: "log-usage-action",
			wantErr: nil,
		},
		{
			name:    "HasLogUsageActionShortFlag",
			fs:      flag.NewFlagSet("lua", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu", "-lus", "ACTION", "-lua", "1"},
			pattern: "lua",
			wantErr: nil,
		},
		{
			name:    "MissingLogUsageAction",
			fs:      flag.NewFlagSet("log-usage-action", flag.ExitOnError),
			osArgs:  []string{"MissingLogUsageAction", "-lu", "-lus", "ACTION"},
			pattern: "log-usage-status",
			wantErr: errQuiet,
		},
		{
			name:    "HasLogUsageError",
			fs:      flag.NewFlagSet("log-usage-error", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu", "-lus", "ERROR", "-log-usage-error", "1"},
			pattern: "log-usage-error",
			wantErr: nil,
		},
		{
			name:    "HasLogUsageErrorShortFlag",
			fs:      flag.NewFlagSet("lue", flag.ExitOnError),
			osArgs:  []string{"HasLogUsageFlagSet", "-lu", "-lus", "ERROR", "-lue", "1"},
			pattern: "lue",
			wantErr: nil,
		},
		{
			name:    "MissingLogUsageError",
			fs:      flag.NewFlagSet("log-usage-error", flag.ExitOnError),
			osArgs:  []string{"MissingLogUsageError", "-lu", "-lus", "ERROR"},
			pattern: "log-usage-status",
			wantErr: errQuiet,
		},
		{
			name:    "HasProejct",
			fs:      flag.NewFlagSet("project", flag.ExitOnError),
			osArgs:  []string{"HasProjectFlagSet", "-project", "p"},
			pattern: "project",
			wantErr: nil,
		},
		{
			name:    "HasInstance",
			fs:      flag.NewFlagSet("instance", flag.ExitOnError),
			osArgs:  []string{"HasInstanceFlagSet", "-instance", "i"},
			pattern: "instance",
			wantErr: nil,
		},
		{
			name:    "HasZone",
			fs:      flag.NewFlagSet("zone", flag.ExitOnError),
			osArgs:  []string{"HasZoneFlagSet", "-zone", "z"},
			pattern: "zone",
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotError := setupFlagsAndParse(test.fs, test.osArgs, test.mfr, test.mfw)
			got := test.fs.Lookup(test.pattern)
			if got == nil {
				t.Errorf("failure in setupFlagsAndParse(). got-flag: nil  want-flag: %s", test.pattern)
			}
			if gotError != test.wantErr {
				t.Errorf("Failure in setupFlagsAndParse(), gotError: %v wantError: %v", gotError, test.wantErr)
			}
		})
	}
}

func TestIsFlagPresent(t *testing.T) {
	tests := []struct {
		name     string
		fs       *flag.FlagSet
		flagName string
		flagVal  string
		want     bool
	}{
		{
			name:     "FlagSet",
			fs:       flag.NewFlagSet("mm", flag.ExitOnError),
			flagName: "mm",
			flagVal:  "true",
			want:     true,
		},
		{
			name:     "FlagNotSet",
			fs:       flag.NewFlagSet("mm", flag.ExitOnError),
			flagName: "config",
			flagVal:  "false",
			want:     false,
		},
	}

	for _, test := range tests {
		test.fs.Bool(test.flagName, true, "test-usage")
		if test.name == "FlagSet" {
			test.fs.Parse([]string{"-" + test.flagName, test.flagVal})
		}
		got := isFlagPresent(test.fs, test.flagName)
		if got != test.want {
			t.Errorf("Got(%v) != Want(%v)", got, test.want)
		}
	}
}

func TestLogUsageStatus(t *testing.T) {
	// Prevent requests to the compute endpoint during test execution
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		LogUsageMetrics: false,
	})

	tests := []struct {
		name     string
		status   string
		actionID int
		errorID  int
		want     error
	}{
		{
			name:   "Running",
			status: "RUNNING",
			want:   nil,
		},
		{
			name:   "Started",
			status: "STARTED",
			want:   nil,
		},
		{
			name:   "Stopped",
			status: "STOPPED",
			want:   nil,
		},
		{
			name:   "Configured",
			status: "CONFIGURED",
			want:   nil,
		},
		{
			name:   "Misconfigured",
			status: "MISCONFIGURED",
			want:   nil,
		},
		{
			name:    "Error",
			status:  "ERROR",
			errorID: 1,
			want:    nil,
		},
		{
			name:   "Installed",
			status: "INSTALLED",
			want:   nil,
		},
		{
			name:   "Updated",
			status: "UPDATED",
			want:   nil,
		},
		{
			name:   "Uninstalled",
			status: "UNINSTALLED",
			want:   nil,
		},
		{
			name:     "Action",
			status:   "ACTION",
			actionID: 1,
			want:     nil,
		},
		{
			name:   "InvalidStatusReturnsError",
			status: "INVALID",
			want:   cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := logUsageStatus(test.status, test.actionID, test.errorID)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("logUsageStatus(%q, %q, %q) got: %v, want nil", test.status, test.actionID, test.errorID, got)
			}
		})
	}
}
