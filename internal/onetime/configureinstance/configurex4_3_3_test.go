/*
Copyright 2024 Google LLC

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

package configureinstance

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestConfigureX43_3(t *testing.T) {
	tests := []struct {
		name    string
		c       ConfigureInstance
		want    bool
		wantErr error
	}{
		{
			name: "Megamem1920FailedToReadReleaseFile",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
				MachineType: "x4-megamem-1920",
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailRegenerateSystemConf",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", ""}),
				WriteFile:   defaultWriteFile(1),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailRegenerateLoginConf",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", "", ""}),
				WriteFile:   defaultWriteFile(2),
				MkdirAll:    defaultMkdirAll(2),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailRegenerateModprobe",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", "", "", ""}),
				WriteFile:   defaultWriteFile(3),
				MkdirAll:    defaultMkdirAll(3),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailRunDracut",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil}, []string{"Name=RHEL", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
				WriteFile:   defaultWriteFile(4),
				MkdirAll:    defaultMkdirAll(4),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailRegenerateGrub",
			c: ConfigureInstance{
				ReadFile:       defaultReadFile([]error{nil, nil, nil, nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", "", "", "", ""}),
				ExecuteFunc:    defaultExecute([]int{0}, []string{""}),
				WriteFile:      defaultWriteFile(5),
				MkdirAll:       defaultMkdirAll(5),
				Apply:          true,
				MachineType:    "x4-megamem-1920",
				HyperThreading: hyperThreadingDefault,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailRemoveNosmt",
			c: ConfigureInstance{
				ReadFile:       defaultReadFile([]error{nil, nil, nil, nil, nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", "", "", "", "", ""}),
				ExecuteFunc:    defaultExecute([]int{0}, []string{""}),
				WriteFile:      defaultWriteFile(5),
				MkdirAll:       defaultMkdirAll(5),
				Apply:          true,
				HyperThreading: hyperThreadingOn,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailGrub2Mkconfig",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Apply:       true,
			},
			want:    true,
			wantErr: nil,
		},
		{
			name: "Success",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0}, []string{"", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Check:       true,
			},
			want:    true,
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.c.configureX43_3(context.Background())
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("configureX4(%v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("configureX4(%v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestConfigureX4SLES3_3(t *testing.T) {
	tests := []struct {
		name    string
		c       ConfigureInstance
		want    bool
		wantErr error
	}{
		{
			name: "FailedToReadReleaseFile",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NotSLESMachine",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{"RHEL"}),
			},
			want:    false,
			wantErr: nil,
		},
		{
			name: "FailedSaptuneService",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"Name=SLES"}),
				ExecuteFunc: defaultExecute([]int{4, 4}, []string{"", ""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailedToWriteX4Conf",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, fmt.Errorf("failed to read")}, []string{"Name=SLES", ""}),
				ExecuteFunc: defaultExecute([]int{4, 0}, []string{"", ""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailedSaptuneReapply",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{"Name=SLES", string(googleX4Conf3_3)}),
				ExecuteFunc: defaultExecute([]int{4, 0, 0, 4}, []string{"", "", "", ""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{"Name=SLES", string(googleX4Conf3_3)}),
				ExecuteFunc: defaultExecute([]int{4, 0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", "", ""}),
			},
			want:    true,
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.c.configureX4SLES3_3(context.Background())
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("configureX4SLES(%v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("configureX4SLES(%v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestSaptuneService3_3(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
		want error
	}{
		{
			name: "SapconfFailDisable",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SapconfFailStop",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0, 0, 1}, []string{"", "", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceNotFound",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{4, 4}, []string{"", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceFailedToEnable",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{4, 1, 1}, []string{"", "", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceFailedToStart",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{4, 1, 0, 1}, []string{"", "", "", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceStartedAfterStopped",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{4, 0, 0, 0}, []string{"", "", "", ""}),
			},
			want: nil,
		},
		{
			name: "ServiceAlreadyRunning",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{4, 0}, []string{"", ""}),
			},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.saptuneService3_3(context.Background())
			if !cmp.Equal(got, tc.want, cmpopts.EquateErrors()) {
				t.Errorf("saptuneService(%#v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestSaptuneSolutions3_3(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
		want bool
	}{
		{
			name: "SolutionUpdate",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{"enabled Solution: NOT_HANA"}),
			},
			want: true,
		},
		{
			name: "NoteUpdate",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{"enabled Solution: HANA\nadditional enabled Notes: NOT_google-x4"}),
			},
			want: true,
		},
		{
			name: "NoUpdateRequired",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{"enabled Solution: HANA\nadditional enabled Notes: google-x4"}),
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.saptuneSolutions3_3(context.Background())
			if got != tc.want {
				t.Errorf("saptuneSolutions(%#v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestSaptuneReapply3_3(t *testing.T) {
	tests := []struct {
		name           string
		sapTuneReapply bool
		c              ConfigureInstance
		wantErr        error
	}{
		{
			name:           "ReapplyNotRequired",
			sapTuneReapply: false,
		},
		{
			name:           "CheckMode",
			sapTuneReapply: true,
			c: ConfigureInstance{
				Check: true,
			},
		},
		{
			name:           "FailSolutionRevert",
			sapTuneReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:           "FailSolutionApply",
			sapTuneReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:           "FailNoteRevert",
			sapTuneReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 0, 1}, []string{"", "", ""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:           "FailNoteApply",
			sapTuneReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 1}, []string{"", "", "", ""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:           "Success",
			sapTuneReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0}, []string{"", "", "", ""}),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.c.saptuneReapply3_3(context.Background(), tc.sapTuneReapply)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("saptuneReapply(%v) returned error: %v, want error: %v", tc.sapTuneReapply, gotErr, tc.wantErr)
			}
		})
	}
}
