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
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// defaultWriteFile will return nil error up to numNil,
// after which they will return an error.
func defaultWriteFile(numNil int) func(string, []byte, os.FileMode) error {
	return func(string, []byte, os.FileMode) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

// The following funcs will return the specified exit code and std
// out each run. Note: The slices should be equal length.
func defaultReadFile(errors []error, contents []string) func(string) ([]byte, error) {
	i := 0
	return func(string) ([]byte, error) {
		if i >= len(errors) || i >= len(contents) {
			i = 0
		}
		content := []byte(contents[i])
		error := errors[i]
		i++
		return content, error
	}
}

func defaultExecute(exitCodes []int, stdOuts []string) func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
	i := 0
	return func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
		if i >= len(exitCodes) || i >= len(stdOuts) {
			i = 0
		}
		result := commandlineexecutor.Result{ExitCode: exitCodes[i], StdOut: stdOuts[i]}
		i++
		return result
	}
}

// verifyWrite will return an error if the write contents don't match.
// Ensure `Apply: true` is set for the configuration.
func verifyWrite(want string) func(file string, got []byte, mode os.FileMode) error {
	return func(file string, got []byte, mode os.FileMode) error {
		if string(got) != want {
			return fmt.Errorf("write file %q contents don't match, got: %q, want: %q", file, got, want)
		}
		return nil
	}
}

func TestExecuteConfigureInstance(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
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
			name: "SuccessForAgentVersion",
			c: ConfigureInstance{
				version: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			c: ConfigureInstance{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "NoSubcommandSupplied",
			want: subcommands.ExitUsageError,
			c:    ConfigureInstance{},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "BothSubcommandsSupplied",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Check: true,
				Apply: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "InvalidHyperThreading",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Check:          true,
				HyperThreading: "invalid",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "UnsupportedMachineType",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Apply:          true,
				machineType:    "",
				HyperThreading: hyperThreadingDefault,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.c, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForConfigureInstance(t *testing.T) {
	want := "check and apply OS settings to support SAP HANA workloads"
	c := ConfigureInstance{}
	got := c.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForConfigureInstance(t *testing.T) {
	c := ConfigureInstance{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"check", "apply", "overrideType", "hyperThreading", "h", "v"}
	c.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestConfigureInstanceHandler(t *testing.T) {
	tests := []struct {
		name    string
		c       ConfigureInstance
		want    subcommands.ExitStatus
		wantErr error
	}{
		{
			name: "UnsupportedMachineType",
			c: ConfigureInstance{
				machineType: "",
			},
			want:    subcommands.ExitUsageError,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "x4SuccessApply",
			c: ConfigureInstance{
				machineType: "x4-megamem-1920",
				readFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=SLES", string(googleX4Conf), "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", ""}),
				writeFile:   defaultWriteFile(5),
				Apply:       true,
			},
			want:    subcommands.ExitSuccess,
			wantErr: nil,
		},
		{
			name: "x4SuccessCheck",
			c: ConfigureInstance{
				machineType: "x4-megamem-1920",
				readFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=SLES", string(googleX4Conf), "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", ""}),
				writeFile:   defaultWriteFile(5),
				Check:       true,
			},
			want:    subcommands.ExitFailure,
			wantErr: nil,
		},
		{
			name: "X4Fail",
			c: ConfigureInstance{
				machineType: "x4-megamem-1920",
				readFile:    defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.c.configureInstanceHandler(context.Background())
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("configureInstanceHandler()=%v want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("configureInstanceHandler()=%v want %v", got, test.want)
			}
		})
	}
}

func TestRemoveLines(t *testing.T) {
	tests := []struct {
		name            string
		c               ConfigureInstance
		removeLines     []string
		wantRegenerated bool
		wantErr         error
	}{
		{
			name: "ReadFileFailure",
			c: ConfigureInstance{
				readFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			removeLines:     []string{""},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "CommentedOutKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"#key=value"}),
				writeFile: verifyWrite(""),
				Apply:     true,
			},
			removeLines:     []string{"key="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultiValueForKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				writeFile: verifyWrite(""),
				Apply:     true,
			},
			removeLines:     []string{"key="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultipleKeysRemoveBoth",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"key1=value1\nkey2=value2"}),
				writeFile: verifyWrite("\n"),
				Apply:     true,
			},
			removeLines:     []string{"key1=", "key2="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultipleKeysRemoveOne",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"key1=value1\nkey2=value2"}),
				writeFile: verifyWrite("\nkey2=value2"),
				Apply:     true,
			},
			removeLines:     []string{"key1="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "KeyNotFound",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				writeFile: verifyWrite(`key="val test=1"`),
				Apply:     true,
			},
			removeLines:     []string{"another_key"},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "FailedToWrite",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				writeFile: defaultWriteFile(0),
				Apply:     true,
			},
			removeLines:     []string{"key"},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRegenerated, gotErr := tc.c.removeLines(context.Background(), "", tc.removeLines)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("removeLines(%#v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if gotRegenerated != tc.wantRegenerated {
				t.Errorf("removeLines(%#v) = %v, want: %v", tc.c, gotRegenerated, tc.wantRegenerated)
			}
		})
	}
}

func TestRemoveValues(t *testing.T) {
	tests := []struct {
		name            string
		c               ConfigureInstance
		removeLines     []string
		wantRegenerated bool
		wantErr         error
	}{
		{
			name: "ReadFileFailure",
			c: ConfigureInstance{
				readFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			removeLines:     []string{""},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "InvalidInputFormat",
			c: ConfigureInstance{
				readFile: defaultReadFile([]error{nil}, []string{"key=value"}),
				Apply:    true,
			},
			removeLines:     []string{"value"},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "CommentedOutKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"#key=value"}),
				writeFile: verifyWrite("#key="),
				Apply:     true,
			},
			removeLines:     []string{"key=value"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultiValueForKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="value test=1"`}),
				writeFile: verifyWrite(`key="value"`),
				Apply:     true,
			},
			removeLines:     []string{"key=test=1"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultipleKeys",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"key1=value1\nkey2=value2"}),
				writeFile: verifyWrite("key1=value1\nkey2="),
				Apply:     true,
			},
			removeLines:     []string{"key2=value2"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "KeyNotFound",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key=value`}),
				writeFile: verifyWrite(`key=value`),
				Apply:     true,
			},
			removeLines:     []string{"another_key=another_value"},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "FailedToWrite",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key=value`}),
				writeFile: defaultWriteFile(0),
				Apply:     true,
			},
			removeLines:     []string{"key=value"},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRegenerated, gotErr := tc.c.removeValues(context.Background(), "", tc.removeLines)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("removeValues(%#v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if gotRegenerated != tc.wantRegenerated {
				t.Errorf("removeValues(%#v) = %v, want: %v", tc.c, gotRegenerated, tc.wantRegenerated)
			}
		})
	}
}

func TestCheckAndRegenerateFile(t *testing.T) {
	tests := []struct {
		name            string
		c               ConfigureInstance
		wantRegenerated bool
		wantErr         error
	}{
		{
			name: "FailedToRead",
			c: ConfigureInstance{
				readFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "OutOfDateFileWithCheck",
			c: ConfigureInstance{
				readFile: defaultReadFile([]error{nil}, []string{"key=wrong_value"}),
				Check:    true,
			},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "OutOfDateFileWithApplyFailedToWrite",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"key=wrong_value"}),
				writeFile: defaultWriteFile(0),
				Apply:     true,
			},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "OutOfDateFileWithApplySuccessfulWrite",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"key=wrong_value"}),
				writeFile: verifyWrite("key=value"),
				Apply:     true,
			},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "NoUpdatesNeeded",
			c: ConfigureInstance{
				readFile: defaultReadFile([]error{nil}, []string{"key=value"}),
			},
			wantRegenerated: false,
			wantErr:         nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.c.checkAndRegenerateFile(context.Background(), "", []byte("key=value"))
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("checkAndRegenerateFile(%#v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if got != tc.wantRegenerated {
				t.Errorf("checkAndRegenerateFile(%#v) = %v, want: %v", tc.c, got, tc.wantRegenerated)
			}
		})
	}
}

func TestCheckAndRegenerateLines(t *testing.T) {
	tests := []struct {
		name            string
		c               ConfigureInstance
		wantLines       []string
		wantRegenerated bool
		wantErr         error
	}{
		{
			name: "ReadFileFailure",
			c: ConfigureInstance{
				readFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			wantLines:       []string{""},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "CommentedOutKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"#key=value"}),
				writeFile: verifyWrite("key=value"),
				Apply:     true,
			},
			wantLines:       []string{"key=value"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "NewValueForKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{"key=1"}),
				writeFile: verifyWrite("key=2"),
				Apply:     true,
			},
			wantLines:       []string{"key=2"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "NoUpdatesNeeded",
			c: ConfigureInstance{
				readFile: defaultReadFile([]error{nil}, []string{"key=1"}),
				Apply:    true,
			},
			wantLines:       []string{"key=1"},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "MultiValueForKey",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				writeFile: verifyWrite(`key="val test=2 new=3"`),
				Apply:     true,
			},
			wantLines:       []string{`key="test=2 new=3"`},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "KeyNotFound",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				writeFile: verifyWrite(`key="val test=1"` + "\n" + `another_key=value` + "\n"),
				Apply:     true,
			},
			wantLines:       []string{`another_key=value`},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "FailedToWrite",
			c: ConfigureInstance{
				readFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				writeFile: defaultWriteFile(0),
				Apply:     true,
			},
			wantLines:       []string{`another_key=value`},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRegenerated, gotErr := tc.c.checkAndRegenerateLines(context.Background(), "", tc.wantLines)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("checkAndRegenerateLines(%#v) returned error: %v, want error: %v", tc.c, gotErr, tc.wantErr)
			}
			if gotRegenerated != tc.wantRegenerated {
				t.Errorf("checkAndRegenerateLines(%#v) = %v, want: %v", tc.c, gotRegenerated, tc.wantRegenerated)
			}
		})
	}
}

func TestRegenerateLine(t *testing.T) {
	tests := []struct {
		name        string
		gotLine     string
		wantLine    string
		wantUpdated bool
		wantOutput  string
	}{
		{
			name:        "SingleValueMatch",
			gotLine:     "key=1",
			wantLine:    "key=1",
			wantUpdated: false,
			wantOutput:  "key=1",
		},
		{
			name:        "SingleValueMismatch",
			gotLine:     "key=1",
			wantLine:    "key=2",
			wantUpdated: true,
			wantOutput:  "key=2",
		},
		{
			name:        "MultiValueMatch",
			gotLine:     `key="val test=2"`,
			wantLine:    `key="val test=2"`,
			wantUpdated: false,
			wantOutput:  `key="val test=2"`,
		},
		{
			name:        "MultiValueMismatch",
			gotLine:     `key="val=1 test=3"`,
			wantLine:    `key="val=2 test=2"`,
			wantUpdated: true,
			wantOutput:  `key="val=2 test=2"`,
		},
		{
			name:        "MultiValueMissingValue",
			gotLine:     `key="val test=3"`,
			wantLine:    `key="missing=2 another"`,
			wantUpdated: true,
			wantOutput:  `key="val test=3 missing=2 another"`,
		},
		{
			name:        "MultiValueSingleValUpdate",
			gotLine:     `key="val test=3"`,
			wantLine:    `key=test=2`,
			wantUpdated: true,
			wantOutput:  `key="val test=2"`,
		},
		{
			name:        "MultiValueSingleValAdded",
			gotLine:     `key="val test=3"`,
			wantLine:    `key=missing`,
			wantUpdated: true,
			wantOutput:  `key="val test=3 missing"`,
		},
		{
			name:        "InvalidWantFormat",
			gotLine:     `key="val test=3"`,
			wantLine:    `key`,
			wantUpdated: false,
			wantOutput:  `key="val test=3"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotUpdated, gotOutput := regenerateLine(context.Background(), tc.gotLine, tc.wantLine)
			if gotUpdated != tc.wantUpdated {
				t.Errorf("regenerateLine(%q, %q) = %v, want: %v", tc.gotLine, tc.wantLine, gotUpdated, tc.wantUpdated)
			}
			if gotOutput != tc.wantOutput {
				t.Errorf("regenerateLine(%q, %q) = %v, want: %v", tc.gotLine, tc.wantLine, gotOutput, tc.wantOutput)
			}
		})
	}
}
