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
	"strings"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

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

func defaultMkdirAll(numNil int) func(string, os.FileMode) error {
	return func(string, os.FileMode) error {
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
		log.CtxLogger(context.Background()).Infof("i: %d, exitCodes: %v, stdOuts: %v", i, exitCodes[i], stdOuts[i])
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
			name: "SuccessForHelp",
			c: ConfigureInstance{
				Help: true,
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
			name: "DescribeAndCheckSupplied",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Check:    true,
				Describe: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "DescribeAndApplySupplied",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Apply:    true,
				Describe: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "DescribeCheckAndApplySupplied",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Check:    true,
				Apply:    true,
				Describe: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "InvalidFormatSupplied",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				Describe: true,
				Format:   "invalid",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "DescribeSuccessCSV",
			want: subcommands.ExitSuccess,
			c: ConfigureInstance{
				Describe:       true,
				Format:         "csv",
				MachineType:    "x4-megamem-1920",
				HyperThreading: hyperThreadingOn,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "DescribeSuccessJSON",
			want: subcommands.ExitSuccess,
			c: ConfigureInstance{
				Describe:       true,
				Format:         "json",
				MachineType:    "x4-megamem-1920",
				HyperThreading: hyperThreadingOn,
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
				MachineType:    "",
				HyperThreading: hyperThreadingOn,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "FailReadFileX4",
			want: subcommands.ExitFailure,
			c: ConfigureInstance{
				Apply:          true,
				MachineType:    "x4-megamem-1920",
				HyperThreading: hyperThreadingOn,
				ReadFile:       defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
				PrintDiff:      true,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessX4",
			want: subcommands.ExitSuccess,
			c: ConfigureInstance{
				Apply:          true,
				MachineType:    "x4-megamem-1920",
				HyperThreading: hyperThreadingOn,
				ReadFile:       defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=SLES", string(googleX4Conf), "", "", "", ""}),
				ExecuteFunc:    defaultExecute([]int{0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", ""}),
				WriteFile:      defaultWriteFile(5),
				MkdirAll:       defaultMkdirAll(5),
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
	flags := []string{"check", "apply", "printDiff", "overrideType", "hyperThreading", "h", "log-path", "timeoutSec"}
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
		name            string
		c               ConfigureInstance
		want            subcommands.ExitStatus
		wantMsgFragment string
	}{
		{
			name: "UnsupportedMachineType",
			c: ConfigureInstance{
				MachineType: "",
			},
			want:            subcommands.ExitUsageError,
			wantMsgFragment: "machine type",
		},
		{
			name: "x4SuccessApply",
			c: ConfigureInstance{
				MachineType: "x4-megamem-1920",
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=SLES", string(googleX4Conf), "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Apply:       true,
			},
			want:            subcommands.ExitSuccess,
			wantMsgFragment: "",
		},
		{
			name: "x4SuccessCheck",
			c: ConfigureInstance{
				MachineType: "x4-megamem-1920",
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=SLES", string(googleX4Conf), "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Check:       true,
				PrintDiff:   true,
			},
			want:            subcommands.ExitFailure,
			wantMsgFragment: "",
		},
		{
			name: "X4Fail",
			c: ConfigureInstance{
				MachineType: "x4-megamem-1920",
				ReadFile:    defaultReadFile([]error{fmt.Errorf("ReadFile failed")}, []string{""}),
			},
			want:            subcommands.ExitFailure,
			wantMsgFragment: "ReadFile failed",
		},
		{
			name: "x5SuccessApply",
			c: ConfigureInstance{
				MachineType: "x5-megamem-96",
				ReadFile:    defaultReadFile([]error{nil, nil, nil, nil, nil, nil}, []string{"Name=SLES", string(googleX5Conf), "", "", "", ""}),
				ExecuteFunc: defaultExecute([]int{0, 0, 0, 0, 0, 0, 0, 0, 0}, []string{"", "", "", "", "", "", "", "", ""}),
				WriteFile:   defaultWriteFile(5),
				MkdirAll:    defaultMkdirAll(5),
				Apply:       true,
			},
			want:            subcommands.ExitSuccess,
			wantMsgFragment: "",
		},
		{
			name: "x5Fail",
			c: ConfigureInstance{
				MachineType: "x5-megamem-96",
				ReadFile:    defaultReadFile([]error{fmt.Errorf("ReadFile failed")}, []string{""}),
			},
			want:            subcommands.ExitFailure,
			wantMsgFragment: "ReadFile failed",
		},
	}
	for _, test := range tests {
		test.c.oteLogger = onetime.CreateOTELogger(false)
		t.Run(test.name, func(t *testing.T) {
			got, msg := test.c.configureInstanceHandler(context.Background())
			if got != test.want {
				t.Errorf("configureInstanceHandler()=%v want %v", got, test.want)
			}
			if !strings.Contains(msg, test.wantMsgFragment) {
				t.Errorf("configureInstanceHandler()=%q, want to contain %q", msg, test.wantMsgFragment)
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
				ReadFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			removeLines:     []string{""},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "CommentedOutKey",
			c: ConfigureInstance{
				ReadFile:  defaultReadFile([]error{nil}, []string{"#key=value"}),
				WriteFile: verifyWrite(""),
				MkdirAll:  defaultMkdirAll(1),
				Apply:     true,
			},
			removeLines:     []string{"key="},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "MultiValueForKey",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile:   verifyWrite(`#key="val test=1"`),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultipleKeysRemoveBoth",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key1=value1\nkey2=value2"}),
				WriteFile:   verifyWrite("#key1=value1\n#key2=value2"),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key1=", "key2="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultipleKeysRemoveOne",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key1=value1\nkey2=value2"}),
				WriteFile:   verifyWrite("#key1=value1\nkey2=value2"),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key1="},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "KeyNotFound",
			c: ConfigureInstance{
				ReadFile:  defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile: verifyWrite(`key="val test=1"`),
				MkdirAll:  defaultMkdirAll(1),
				Apply:     true,
			},
			removeLines:     []string{"another_key"},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "FailedToWrite",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile:   defaultWriteFile(0),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key"},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "FailedToMkdirAll",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				MkdirAll:    defaultMkdirAll(0),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
				PrintDiff:   true,
			},
			removeLines:     []string{"key"},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "FailedToBackup",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile:   verifyWrite(`key="val test=1"`),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
				Apply:       true,
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
				ReadFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			removeLines:     []string{""},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "InvalidInputFormat",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{"key=value"}),
				Apply:    true,
			},
			removeLines:     []string{"value"},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "CommentedOutKey",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"#key=value"}),
				WriteFile:   verifyWrite("#key="),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key=value"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultiValueForKey",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="value test=1"`}),
				WriteFile:   verifyWrite(`key="value"`),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key=test=1"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "MultipleKeys",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key1=value1\nkey2=value2"}),
				WriteFile:   verifyWrite("key1=value1\nkey2="),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			removeLines:     []string{"key2=value2"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "KeyNotFound",
			c: ConfigureInstance{
				ReadFile:  defaultReadFile([]error{nil}, []string{`key=value`}),
				WriteFile: verifyWrite(`key=value`),
				MkdirAll:  defaultMkdirAll(1),
				Apply:     true,
			},
			removeLines:     []string{"another_key=another_value"},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "FailedToWrite",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key=value`}),
				WriteFile:   defaultWriteFile(0),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
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
				ReadFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "OutOfDateFileWithCheck",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key=wrong_value"}),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Check:       true,
			},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "OutOfDateFileWithApplyFailedToWrite",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key=wrong_value"}),
				WriteFile:   defaultWriteFile(0),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "OutOfDateFileWithApplySuccessfulWrite",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key=wrong_value"}),
				WriteFile:   verifyWrite("key=value"),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "NoUpdatesNeeded",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{"key=value"}),
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
				ReadFile: func(string) ([]byte, error) { return nil, fmt.Errorf("failed to read file") },
			},
			wantLines:       []string{""},
			wantRegenerated: false,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "CommentedOutKey",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"#key=value"}),
				WriteFile:   verifyWrite("key=value"),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			wantLines:       []string{"key=value"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "NewValueForKey",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{"key=1"}),
				WriteFile:   verifyWrite("key=2"),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			wantLines:       []string{"key=2"},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "NoUpdatesNeeded",
			c: ConfigureInstance{
				ReadFile: defaultReadFile([]error{nil}, []string{"key=1"}),
				Apply:    true,
			},
			wantLines:       []string{"key=1"},
			wantRegenerated: false,
			wantErr:         nil,
		},
		{
			name: "MultiValueForKey",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile:   verifyWrite(`key="val test=2 new=3"`),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			wantLines:       []string{`key="test=2 new=3"`},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "KeyNotFound",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile:   verifyWrite(`key="val test=1"` + "\n" + `another_key=value` + "\n"),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
			},
			wantLines:       []string{`another_key=value`},
			wantRegenerated: true,
			wantErr:         nil,
		},
		{
			name: "FailedToWrite",
			c: ConfigureInstance{
				ReadFile:    defaultReadFile([]error{nil}, []string{`key="val test=1"`}),
				WriteFile:   defaultWriteFile(0),
				MkdirAll:    defaultMkdirAll(1),
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				Apply:       true,
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
		{
			name:        "MultiValueSimilarNames",
			gotLine:     `key="val=1 test.val=3"`,
			wantLine:    `key="val=2"`,
			wantUpdated: true,
			wantOutput:  `key="val=2 test.val=3"`,
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

func TestSetDefaults(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
		want ConfigureInstance
	}{
		{
			name: "EmptyStruct",
			c:    ConfigureInstance{},
			want: ConfigureInstance{
				Format:         "csv",
				HyperThreading: hyperThreadingOn,
				TimeoutSec:     300,
			},
		},
		{
			name: "NilValuesStruct",
			c: ConfigureInstance{
				HyperThreading: "",
				TimeoutSec:     0,
			},
			want: ConfigureInstance{
				Format:         "csv",
				HyperThreading: hyperThreadingOn,
				TimeoutSec:     300,
			},
		},
		{
			name: "AlreadySet",
			c: ConfigureInstance{
				Format:         "json",
				HyperThreading: "already_set",
				TimeoutSec:     123,
			},
			want: ConfigureInstance{
				Format:         "json",
				HyperThreading: "already_set",
				TimeoutSec:     123,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.c.setDefaults()
			if !cmp.Equal(tc.want, tc.c, cmpopts.IgnoreUnexported(ConfigureInstance{})) {
				t.Errorf("setDefaults() yielded %v, want: %v", tc.c, tc.want)
			}
		})
	}
}

func TestIsSupportedMachineType(t *testing.T) {
	tests := []struct {
		name        string
		machineType string
		want        bool
	}{
		{
			name:        "x4Supported",
			machineType: "x4-megamem-1920",
			want:        true,
		},
		{
			name:        "x5Supported",
			machineType: "x5-megamem-96",
			want:        true,
		},
		{
			name:        "n2Unsupported",
			machineType: "n2-standard-8",
			want:        false,
		},
		{
			name:        "EmptyUnsupported",
			machineType: "",
			want:        false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := ConfigureInstance{MachineType: tc.machineType}
			got := c.IsSupportedMachineType()
			if got != tc.want {
				t.Errorf("IsSupportedMachineType() on machine type %q = %v, want: %v", tc.machineType, got, tc.want)
			}
		})
	}
}

func TestSetFlagsDescribe(t *testing.T) {
	c := &ConfigureInstance{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c.SetFlags(fs)

	if err := fs.Parse([]string{"-describe", "-format=json"}); err != nil {
		t.Fatalf("fs.Parse() failed: %v", err)
	}
	if !c.Describe {
		t.Errorf("c.Describe = false, want true")
	}
	if c.Format != "json" {
		t.Errorf("c.Format = %q, want 'json'", c.Format)
	}
}

func TestReadCurrentLineValue(t *testing.T) {
	c := ConfigureInstance{
		ReadFile: func(path string) ([]byte, error) {
			if path == "error.conf" {
				return nil, cmpopts.AnyError
			}
			return []byte("DefaultTimeoutStartSec=300s\n#UserTasksMax=10\nbarekey_line\n"), nil
		},
	}

	if got := c.readCurrentLineValue("error.conf", "DefaultTimeoutStartSec"); got != "NOT_SET" {
		t.Errorf("readCurrentLineValue(error.conf) = %q, want 'NOT_SET'", got)
	}
	if got := c.readCurrentLineValue("test.conf", "DefaultTimeoutStartSec"); got != "300s" {
		t.Errorf("readCurrentLineValue(test.conf) = %q, want '300s'", got)
	}
	if got := c.readCurrentLineValue("test.conf", "UserTasksMax"); got != "<COMMENTED_OUT>" {
		t.Errorf("readCurrentLineValue(test.conf commented) = %q, want '<COMMENTED_OUT>'", got)
	}
	if got := c.readCurrentLineValue("test.conf", "barekey"); got != "barekey_line" {
		t.Errorf("readCurrentLineValue(test.conf barekey) = %q, want 'barekey_line'", got)
	}
	if got := c.readCurrentLineValue("test.conf", "MissingKey"); got != "NOT_SET" {
		t.Errorf("readCurrentLineValue(test.conf missing) = %q, want 'NOT_SET'", got)
	}
}

func TestFormatDescribeOutput(t *testing.T) {
	rules := []ConfigRule{
		{
			Category:       "Systemd",
			TargetFile:     "/etc/systemd/system.conf",
			ParameterKey:   "DefaultTimeoutStartSec",
			ExpectedValue:  "300s",
			CurrentValue:   "300s",
			RebootRequired: true,
		},
		{
			Category:       "Sysctl",
			TargetFile:     "google-x4.conf",
			ParameterKey:   "net.core.rmem_max",
			ExpectedValue:  "83886080",
			CurrentValue:   "NOT_SET",
			RebootRequired: false,
		},
	}

	t.Run("CSVFormat", func(t *testing.T) {
		c := ConfigureInstance{Format: "csv"}
		status, got := c.formatDescribeOutput("X4", rules)
		if status != subcommands.ExitSuccess {
			t.Errorf("formatDescribeOutput(csv) status = %v, want ExitSuccess", status)
		}
		wantHeader := "Series,Category,TargetFile,ParameterKey,ExpectedValue,CurrentValue,RebootRequired\n"
		wantRow1 := "X4,Systemd,/etc/systemd/system.conf,DefaultTimeoutStartSec,300s,300s,YES\n"
		wantRow2 := "X4,Sysctl,google-x4.conf,net.core.rmem_max,83886080,NOT_SET,NO\n"
		if !strings.Contains(got, wantHeader) || !strings.Contains(got, wantRow1) || !strings.Contains(got, wantRow2) {
			t.Errorf("formatDescribeOutput(csv) = %q, want rows to contain header and rows", got)
		}
	})

	t.Run("JSONFormat", func(t *testing.T) {
		c := ConfigureInstance{Format: "json"}
		status, got := c.formatDescribeOutput("X4", rules)
		if status != subcommands.ExitSuccess {
			t.Errorf("formatDescribeOutput(json) status = %v, want ExitSuccess", status)
		}
		if !strings.Contains(got, `"series": "X4"`) || !strings.Contains(got, `"DefaultTimeoutStartSec"`) {
			t.Errorf("formatDescribeOutput(json) = %q, want JSON to contain series and rule key", got)
		}
	})
}

func TestReadCurrentFileContent(t *testing.T) {
	c := ConfigureInstance{
		ReadFile: func(path string) ([]byte, error) {
			if path == "error.conf" {
				return nil, cmpopts.AnyError
			}
			return []byte("blacklist idxd\nblacklist hpilo\n"), nil
		},
	}

	if got := c.readCurrentFileContent("error.conf"); got != "NOT_SET" {
		t.Errorf("readCurrentFileContent(error.conf) = %q, want 'NOT_SET'", got)
	}
	if got := c.readCurrentFileContent("test.conf"); got != "blacklist idxd\nblacklist hpilo" {
		t.Errorf("readCurrentFileContent(test.conf) = %q, want file content", got)
	}
}

func TestDescribeHandlerX4andX5(t *testing.T) {
	ctx := context.Background()
	c4 := ConfigureInstance{
		Describe:    true,
		Format:      "csv",
		MachineType: "x4-megamem-1920",
	}
	status4, got4 := c4.configureInstanceHandler(ctx)
	if status4 != subcommands.ExitSuccess || !strings.Contains(got4, "Series,Category") {
		t.Errorf("configureInstanceHandler(x4) = status:%v, msg:%q, want ExitSuccess and CSV header", status4, got4)
	}

	c5 := ConfigureInstance{
		Describe:    true,
		Format:      "json",
		MachineType: "x5-megamem-96",
	}
	status5, got5 := c5.configureInstanceHandler(ctx)
	if status5 != subcommands.ExitSuccess || !strings.Contains(got5, `"series": "X5"`) {
		t.Errorf("configureInstanceHandler(x5) = status:%v, msg:%q, want ExitSuccess and JSON series X5", status5, got5)
	}
}

func TestRunSetDefaultsApplied(t *testing.T) {
	c := ConfigureInstance{
		Describe:    true,
		MachineType: "x4-megamem-1920",
		// HyperThreading and TimeoutSec left empty so setDefaults MUST run
	}
	opts := &onetime.RunOptions{
		CloudProperties: &ipb.CloudProperties{
			MachineType: "x4-megamem-1920",
		},
	}
	status, _ := c.Run(context.Background(), opts)
	if status != subcommands.ExitSuccess {
		t.Fatalf("Run() status = %v, want ExitSuccess", status)
	}
	if c.HyperThreading != hyperThreadingOn {
		t.Errorf("c.HyperThreading = %q, want %q", c.HyperThreading, hyperThreadingOn)
	}
	if c.TimeoutSec != 300 {
		t.Errorf("c.TimeoutSec = %d, want 300", c.TimeoutSec)
	}
}
