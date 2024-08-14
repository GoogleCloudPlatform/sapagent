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

package balanceirq

import (
	"context"
	"fmt"
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
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
func verifyWrite(want []string) func(file string, got []byte, mode os.FileMode) error {
	i := 0
	return func(file string, got []byte, mode os.FileMode) error {
		if i >= len(want) {
			i = 0
		}
		if string(got) != want[i] {
			return fmt.Errorf("write file %q contents don't match, got: %q, want: %q", file, got, want[i])
		}
		i++
		return nil
	}
}

func TestExecuteBalanceIRQ(t *testing.T) {
	tests := []struct {
		name string
		b    BalanceIRQ
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
			b: BalanceIRQ{
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
			got := test.b.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.b, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForBalanceIRQ(t *testing.T) {
	want := "provides optimal IRQ balancing on X4 instances"
	b := BalanceIRQ{}
	got := b.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForBalanceIRQ(t *testing.T) {
	b := BalanceIRQ{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"install", "h"}
	b.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestBalanceIRQHandler(t *testing.T) {
	tests := []struct {
		name    string
		b       BalanceIRQ
		want    subcommands.ExitStatus
		wantErr error
	}{
		{
			name: "FailInstall",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
				readFile:    defaultReadFile([]error{nil}, []string{""}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SuccessInstall",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{nil}, []string{""}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want:    subcommands.ExitSuccess,
			wantErr: nil,
		},
		{
			name: "SuccessNoSocketsOrInterrupts",
			b: BalanceIRQ{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{nil}, []string{""}),
				writeFile:   verifyWrite([]string{""}),
			},
			want:    subcommands.ExitSuccess,
			wantErr: nil,
		},
		{
			name: "FailDisableIRQBalance",
			b: BalanceIRQ{
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
				readFile:    defaultReadFile([]error{nil}, []string{""}),
				writeFile:   verifyWrite([]string{""}),
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailReadSockets",
			b: BalanceIRQ{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
				writeFile:   verifyWrite([]string{""}),
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailReadInterrupts",
			b: BalanceIRQ{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{nil, nil, cmpopts.AnyError}, []string{"", "", ""}),
				writeFile:   verifyWrite([]string{""}),
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailWrite",
			b: BalanceIRQ{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{nil, nil, nil}, []string{"1", "1-2,3-4", "123: idpf-eth0-TxRx-1"}),
				writeFile:   verifyWrite([]string{"1234"}),
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SuccessWrite",
			b: BalanceIRQ{
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{nil, nil, nil}, []string{"1", "1-2,3-4", "123: idpf-eth0-TxRx-1"}),
				writeFile:   verifyWrite([]string{"1-2,3-4"}),
			},
			want:    subcommands.ExitSuccess,
			wantErr: nil,
		},
	}
	for _, test := range tests {
		test.b.oteLogger = onetime.CreateOTELogger(false)
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.b.balanceIRQHandler(context.Background())
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("balanceIRQHandler()=%v want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("balanceIRQHandler()=%v want %v", got, test.want)
			}
		})
	}
}

func TestReadInterrupts(t *testing.T) {
	test := []struct {
		name    string
		b       BalanceIRQ
		want    []string
		wantErr error
	}{
		{
			name: "FailRead",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{nil}, []string{"123\n2283: 0000:00:1f.7\n2284: idpf-Mailbox-0\n2285: idpf-eth0-TxRx-1"}),
			},
			want:    []string{"2285"},
			wantErr: nil,
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.b.readInterrupts(context.Background())
			if !cmp.Equal(got, test.want, cmpopts.EquateEmpty()) {
				t.Errorf("%v.readInterrupts()=%v, want %v", test.b, got, test.want)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("%v.readInterrupts()=%v, want %v", test.b, gotErr, test.wantErr)
			}
		})
	}
}

func TestReadSockets(t *testing.T) {
	test := []struct {
		name    string
		b       BalanceIRQ
		want    []socketCores
		wantErr error
	}{
		{
			name: "FailRead",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailParse",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{nil}, []string{"abc-1"}),
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoNodes",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{nil}, []string{""}),
			},
			want:    []socketCores{{}},
			wantErr: nil,
		},
		{
			name: "FailReadNode",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{nil, cmpopts.AnyError}, []string{"", ""}),
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{nil, nil}, []string{"1", "1-2,3-4"}),
			},
			want:    []socketCores{{socket: "1", cores: "1-2,3-4"}},
			wantErr: nil,
		},
		{
			name: "SuccessMultipleSockets",
			b: BalanceIRQ{
				readFile: defaultReadFile([]error{nil, nil, nil}, []string{"1-2", "1-2,3-4", "5-6,7-8"}),
			},
			want:    []socketCores{{socket: "1", cores: "1-2,3-4"}, {socket: "2", cores: "5-6,7-8"}},
			wantErr: nil,
		},
		{
			name: "SuccessPinToSocket",
			b: BalanceIRQ{
				readFile:    defaultReadFile([]error{nil, nil, nil}, []string{"1-2", "1-2,3-4", "5-6,7-8"}),
				pinToSocket: "1",
			},
			want:    []socketCores{{socket: "1", cores: "1-2,3-4"}},
			wantErr: nil,
		},
		{
			name: "PinToSocketNotFound",
			b: BalanceIRQ{
				readFile:    defaultReadFile([]error{nil, nil, nil}, []string{"1-2", "1-2,3-4", "5-6,7-8"}),
				pinToSocket: "3",
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.b.readSockets(context.Background())
			if len(got) != len(test.want) {
				t.Errorf("%v.readSockets()=%v, want %v", test.b, got, test.want)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("%v.readSockets()=%v, want %v", test.b, gotErr, test.wantErr)
			}
		})
	}
}

func TestParseNumberRanges(t *testing.T) {
	test := []struct {
		name    string
		input   string
		want    []string
		wantErr error
	}{
		{
			name:    "EmptyInput",
			input:   "",
			want:    []string{""},
			wantErr: nil,
		},
		{
			name:    "SingleNumber",
			input:   "1",
			want:    []string{"1"},
			wantErr: nil,
		},
		{
			name:    "Range",
			input:   "0-1",
			want:    []string{"0", "1"},
			wantErr: nil,
		},
		{
			name:    "MultipleRanges",
			input:   "0-1,3-5",
			want:    []string{"0", "1", "3", "4", "5"},
			wantErr: nil,
		},
		{
			name:    "MultipleRangesAndSingleNumber",
			input:   "0-1,3-5,20",
			want:    []string{"0", "1", "3", "4", "5", "20"},
			wantErr: nil,
		},
		{
			name:    "InvalidRange",
			input:   "0--1",
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "InvalidStart",
			input:   "abc-1",
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "InvalidEnd",
			input:   "0-abc",
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			var b BalanceIRQ
			got, gotErr := b.parseNumberRanges(test.input)
			if !cmp.Equal(got, test.want) {
				t.Errorf("parseNumberRanges(%v)=%v, want %v", test.input, got, test.want)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("parseNumberRanges(%v)=%v, want %v", test.input, gotErr, test.wantErr)
			}
		})
	}
}

func TestDisableProvidedIRQBalance(t *testing.T) {
	test := []struct {
		name string
		b    BalanceIRQ
		want error
	}{
		{
			name: "IRQBalanceNotInstalled",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{4}, []string{""}),
			},
			want: nil,
		},
		{
			name: "FailDisable",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailStop",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0, 0, 1}, []string{"", "", ""}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0, 0, 0}, []string{"", "", ""}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: nil,
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.disableProvidedIRQBalance(context.Background())
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("%v.disableProvidedIRQBalance()=%v, want %v", test.b, got, test.want)
			}
		})
	}
}

func TestInstallSystemdService(t *testing.T) {
	test := []struct {
		name string
		b    BalanceIRQ
		want error
	}{
		{
			name: "FailReadOS",
			b: BalanceIRQ{
				install:  true,
				readFile: defaultReadFile([]error{cmpopts.AnyError}, []string{""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteFile",
			b: BalanceIRQ{
				install:   true,
				readFile:  defaultReadFile([]error{nil}, []string{"SLES"}),
				writeFile: verifyWrite([]string{""}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailReload",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{1}, []string{""}),
				readFile:    defaultReadFile([]error{nil}, []string{"SLES"}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailEnable",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0, 1}, []string{"", ""}),
				readFile:    defaultReadFile([]error{nil}, []string{"SLES"}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailStart",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0, 0, 1}, []string{"", "", ""}),
				readFile:    defaultReadFile([]error{nil}, []string{"SLES"}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			b: BalanceIRQ{
				install:     true,
				ExecuteFunc: defaultExecute([]int{0}, []string{""}),
				readFile:    defaultReadFile([]error{nil}, []string{"SLES"}),
				writeFile:   verifyWrite([]string{systemdContent}),
			},
			want: nil,
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.installSystemdService(context.Background())
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("%v.installSystemdService()=%v, want %v", test.b, got, test.want)
			}
		})
	}
}
