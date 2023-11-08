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

package parse

import (
	"bytes"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmpopts/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []string
	}{
		{
			name: "EmptyString",
			s:    "",
			want: []string{""},
		},
		{
			name: "OneParameter",
			s:    "Hello",
			want: []string{"Hello"},
		},
		{
			name: "MultipleParameters",
			s:    "backup restore inquire delete",
			want: []string{"backup", "restore", "inquire", "delete"},
		},
		{
			name: "ParameterWithSpaces",
			s:    `"Hello World"`,
			want: []string{`"Hello World"`},
		},
		{
			name: "ParameterWithDoubleQuote",
			s:    `"\"Hello \"World"`,
			want: []string{`"\"Hello \"World"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Split(test.s)
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("split(%v) had unexpected diff (-want +got):\n%s", test.s, diff)
			}
		})
	}
}

func TestWriteSoftwareVersion(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    string
		wantErr error
	}{
		{
			name:    "EmptyString",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ParseVersion",
			line: `#SOFTWAREID "backint 1.50" "hdb nameserver 2.00.50"`,
			want: "1.50",
		},
	}
	for _, tc := range tests {
		output := bytes.NewBufferString("")
		got, err := WriteSoftwareVersion(tc.line, output)
		if cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()) != "" {
			t.Errorf("WriteSoftwareVersion(%v) = %v, wantError: %v", tc.line, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("WriteSoftwareVersion(%v) = %v, want: %v", tc.line, got, tc.want)
		}
	}
}

func TestTrimAndClean(t *testing.T) {
	tests := []struct {
		name string
		str  string
		want string
	}{
		{
			name: "EmptyString",
		},
		{
			name: "TrimQuotes",
			str:  `"Hello World"`,
			want: `Hello World`,
		},
		{
			name: "EmbeddedQuotesAndBackslashes",
			str:  `Hello\" W\orld`,
			want: `Hello" W\orld`,
		},
	}

	for _, tc := range tests {
		got := TrimAndClean(tc.str)
		if got != tc.want {
			t.Errorf("TrimAndClean(%v) = %v, want: %v", tc.str, got, tc.want)
		}
	}
}

func TestRestoreFilename(t *testing.T) {
	tests := []struct {
		name string
		str  string
		want string
	}{
		{
			name: "EmptyString",
			want: `"/"`,
		},
		{
			name: "RegularFileName",
			str:  `tmp/test.txt`,
			want: `"/tmp/test.txt"`,
		},
		{
			name: "EmbeddedQuotesAndBackslashes",
			str:  `tmp/test".\txt`,
			want: `"/tmp/test\".\txt"`,
		},
	}

	for _, tc := range tests {
		got := RestoreFilename(tc.str)
		if got != tc.want {
			t.Errorf("RestoreFilename(%v) = %v, want: %v", tc.str, got, tc.want)
		}
	}
}

func TestOpenFileWithRetries(t *testing.T) {
	tests := []struct {
		name       string
		fileName   string
		createFile bool
		wantError  error
	}{
		{
			name:       "FileFound",
			fileName:   t.TempDir() + "/found.txt",
			createFile: true,
			wantError:  nil,
		},
		{
			name:       "FileNotFound",
			fileName:   t.TempDir() + "/not_found.txt",
			createFile: false,
			wantError:  cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		if tc.createFile {
			f, err := os.Create(tc.fileName)
			if err != nil {
				t.Errorf("os.Create(%v) failed: %v", tc.fileName, err)
			}
			defer f.Close()
		}

		_, err := OpenFileWithRetries(tc.fileName, os.O_RDONLY, 0, 0)
		if cmp.Diff(err, tc.wantError, cmpopts.EquateErrors()) != "" {
			t.Errorf("OpenFileWithRetries(%v) = %v, wantError: %v", tc.fileName, err, tc.wantError)
		}
	}
}
