/*
Copyright 2026 Google LLC

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

func TestConfigureX5(t *testing.T) {
	tests := []struct {
		name    string
		c       ConfigureInstance
		want    bool
		wantErr error
	}{
		{
			name: "Megamem1920FailedToReadReleaseFile",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{fmt.Errorf("failed to read")}, []string{""}),
				MachineType: "x5-megamem-96",
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
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", "", "", "", ""}),
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
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", ""}),
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
				ReadFile:       defaultReadFile([]error{nil, nil, nil, nil, nil, fmt.Errorf("failed to read")}, []string{"Name=RHEL", "", "", "", "", ""}),
				ExecuteFunc:    defaultExecute([]int{0}, []string{""}),
				WriteFile:      defaultWriteFile(5),
				MkdirAll:       defaultMkdirAll(5),
				Apply:          true,
				MachineType:    "x5-megamem-96",
				HyperThreading: hyperThreadingOn,
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
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 1}, []string{"", "", "", "", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailGrub2MkconfigWithBLS",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", "", "Name=Red Hat Enterprise Linux\nVERSION_ID=\"9.2\""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 1}, []string{"", "", "", "", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", "", "Name=Red Hat Enterprise Linux\nVERSION_ID=\"8.2\""}),
				ExecuteFunc: defaultExecute([]int{0, 0}, []string{"", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Check:       true,
			},
			want:    true,
			wantErr: nil,
		},
		{
			name: "SuccessHyperThreadingOff",
			c: ConfigureInstance{
				ReadFile:       defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "", "", "Name=Red Hat Enterprise Linux\nVERSION_ID=\"8.2\""}),
				ExecuteFunc:    defaultExecute([]int{0, 0}, []string{"", ""}),
				WriteFile:      defaultWriteFile(5),
				MkdirAll:       defaultMkdirAll(5),
				Apply:          true,
				HyperThreading: hyperThreadingOff,
			},
			want:    true,
			wantErr: nil,
		},
		{
			name: "SuccessTHPAdvise",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=RHEL", "", "", "Name=Red Hat Enterprise Linux\nVERSION_ID=\"9.2\"", "", "Name=Red Hat Enterprise Linux\nVERSION_ID=\"9.2\""}),
				ExecuteFunc: defaultExecute([]int{0, 0}, []string{"", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Apply:       true,
			},
			want:    true,
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.c.configureX5(context.Background())
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("configureX5(%v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("configureX5(%v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestTransparentHugePageAdviseX5(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
		want bool
	}{
		{
			name: "ReadFileFailed",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{fmt.Errorf("failed to read")}, []string{""}),
			},
			want: false,
		},
		{
			name: "WrongMatchReleaseFile",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{"asdf"}),
			},
			want: false,
		},
		{
			name: "RHEL92",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="Red Hat Enterprise Linux"\nVERSION_ID="9.2"`}),
			},
			want: true,
		},
		{
			name: "RHEL91",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="Red Hat Enterprise Linux"\nVERSION_ID="9.1"`}),
			},
			want: false,
		},
		{
			name: "RHEL82",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="Red Hat Enterprise Linux"\nVERSION_ID="8.2"`}),
			},
			want: false,
		},
		{
			name: "SLES12SP5",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="SLES"\nVERSION_ID="12.5"`}),
			},
			want: false,
		},
		{
			name: "SLES15SP5",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="SLES"\nVERSION_ID="15.5"`}),
			},
			want: true,
		},
		{
			name: "SLES15SP4",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="SLES"\nVERSION_ID="15.4"`}),
			},
			want: false,
		},
		{
			name: "NotSLESOrRHEL",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="SHEL"\nVERSION_ID="15.5"`}),
			},
			want: false,
		},
		{
			name: "BadVersion",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{`NAME="SLES"\nVERSION_ID="15.4.asdf.123.a"`}),
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.transparentHugePageAdviseX5(context.Background())
			if got != tc.want {
				t.Errorf("transparentHugePageAdviseX5(%v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestConfigureX5SLES(t *testing.T) {
	tests := []struct {
		name    string
		c       ConfigureInstance
		want    bool
		wantErr error
	}{
		{
			name: "FailedToReadReleaseFile",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{fmt.Errorf("failed to read")}, []string{""}),
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
			name: "FailedToWriteX5Conf",
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
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{"Name=SLES", string(googleX5Conf)}),
				ExecuteFunc: defaultExecute([]int{4, 0, 0, 4}, []string{"", "", "", ""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{"Name=SLES", string(googleX5Conf)}),
				ExecuteFunc: defaultExecute([]int{4, 0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", "", ""}),
			},
			want:    true,
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.c.configureX5SLES(context.Background())
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("configureX5SLES(%v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("configureX5SLES(%v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestSaptuneServiceX5(t *testing.T) {
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
			got := tc.c.saptuneServiceX5(context.Background())
			if !cmp.Equal(got, tc.want, cmpopts.EquateErrors()) {
				t.Errorf("saptuneServiceX5(%#v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestSaptuneSolutionsX5(t *testing.T) {
	tests := []struct {
		name         string
		c            ConfigureInstance
		wantSolution bool
		wantNote     bool
	}{
		{
			name: "BothUpdate",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
			},
			wantSolution: true,
			wantNote:     true,
		},
		{
			name: "SolutionUpdate",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{"enabled Solution: NOT_HANA\nadditional enabled Notes: google-x5"}),
			},
			wantSolution: true,
			wantNote:     false,
		},
		{
			name: "NoteUpdate",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{"enabled Solution: HANA\nadditional enabled Notes: NOT_google-x5"}),
			},
			wantSolution: false,
			wantNote:     true,
		},
		{
			name: "NoUpdateRequired",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{"enabled Solution: NETWEAVER+HANA\nadditional enabled Notes: google-x5"}),
			},
			wantSolution: false,
			wantNote:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSolution, gotNote := tc.c.saptuneSolutionsX5(context.Background())
			if gotSolution != tc.wantSolution || gotNote != tc.wantNote {
				t.Errorf("saptuneSolutionsX5(%#v) = %v, %v, want: %v, %v", tc.c, gotSolution, gotNote, tc.wantSolution, tc.wantNote)
			}
		})
	}
}

func TestSaptuneReapplyX5(t *testing.T) {
	tests := []struct {
		name            string
		solutionReapply bool
		noteReapply     bool
		c               ConfigureInstance
		wantErr         error
	}{
		{
			name:            "ReapplyNotRequired",
			solutionReapply: false,
			noteReapply:     false,
		},
		{
			name:            "CheckMode",
			solutionReapply: true,
			c: ConfigureInstance{
				Check: true,
			},
		},
		{
			name:            "FailSolutionChange",
			solutionReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "FailNoteRevert",
			noteReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "FailNoteApply",
			noteReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:            "Success",
			solutionReapply: true,
			noteReapply:     true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 0, 0}, []string{"", "", ""}),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.c.saptuneReapplyX5(context.Background(), tc.solutionReapply, tc.noteReapply)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("saptuneReapplyX5(%v, %v) returned error: %v, want error: %v", tc.solutionReapply, tc.noteReapply, gotErr, tc.wantErr)
			}
		})
	}
}

func TestConfigureX5RHEL(t *testing.T) {
	tests := []struct {
		name    string
		c       ConfigureInstance
		want    bool
		wantErr error
	}{
		{
			name: "FailedToReadReleaseFile",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{fmt.Errorf("failed to read")}, []string{""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NotRHELMachine",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{"Name=SLES"}),
			},
			want:    false,
			wantErr: nil,
		},
		{
			name: "FailedTunedService",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`NAME="Red Hat Enterprise Linux"`}),
				ExecuteFunc: defaultExecute([]int{4}, []string{""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailedToWriteTunedConf",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, fmt.Errorf("failed to read")}, []string{`NAME="Red Hat Enterprise Linux"`, ""}),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailedTunedReapply",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{`NAME="Red Hat Enterprise Linux"`, string(googleX5TunedConf)}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 1}, []string{"", "", "", ""}),
				WriteFile:   defaultWriteFile(1),
				MkdirAll:    defaultMkdirAll(1),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailedTunedRestart",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{`NAME="Red Hat Enterprise Linux"`, string(googleX5TunedConf)}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 1}, []string{"", "", "", "Current active profile: google-x5", ""}),
				WriteFile:   defaultWriteFile(1),
				MkdirAll:    defaultMkdirAll(1),
				Apply:       true,
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailedTunedVerify",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{`NAME="Red Hat Enterprise Linux"`, string(googleX5TunedConf)}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 1}, []string{"", "", "", "Current active profile: google-x5", ""}),
				WriteFile:   defaultWriteFile(1),
				MkdirAll:    defaultMkdirAll(1),
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil, nil}, []string{`NAME="Red Hat Enterprise Linux"`, string(googleX5Conf)}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "Current active profile: google-x5", "", ""}),
				WriteFile:   defaultWriteFile(1),
				MkdirAll:    defaultMkdirAll(1),
				Apply:       true,
			},
			want:    true,
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.c.configureX5RHEL(context.Background())
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("configureX5RHEL(%v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("configureX5RHEL(%v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestTunedServiceX5(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
		want error
	}{
		{
			name: "ServiceNotFound",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{4}, []string{""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceFailedToEnable",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{1, 1}, []string{"", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceFailedToStart",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{1, 0, 1}, []string{"", "", ""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ServiceStartedAfterStopped",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{1, 0, 0}, []string{"", "", ""}),
			},
			want: nil,
		},
		{
			name: "ServiceAlreadyRunning",
			c: ConfigureInstance{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
			},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.tunedServiceX5(context.Background())
			if !cmp.Equal(got, tc.want, cmpopts.EquateErrors()) {
				t.Errorf("tunedServiceX5(%#v) = %v, want: %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestTunedReapplyX5(t *testing.T) {
	tests := []struct {
		name         string
		tunedReapply bool
		c            ConfigureInstance
		wantErr      error
	}{
		{
			name:         "ReapplyNotRequired",
			tunedReapply: false,
		},
		{
			name:         "CheckModeReapplyNotRequired",
			tunedReapply: false,
			c: ConfigureInstance{
				Check: true,
			},
		},
		{
			name:         "CheckModeReapplyRequired",
			tunedReapply: true,
			c: ConfigureInstance{
				Check: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:         "FailProfile",
			tunedReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:         "FailVerify",
			tunedReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:         "Success",
			tunedReapply: true,
			c: ConfigureInstance{
				Apply:       true,
				ExecuteFunc: defaultExecute([]int{0, 0}, []string{"", "Current active profile: google-x5"}),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.c.tunedReapplyX5(context.Background(), tc.tunedReapply)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("tunedReapplyX5(%v) returned error: %v, want error: %v", tc.tunedReapply, gotErr, tc.wantErr)
			}
		})
	}
}
