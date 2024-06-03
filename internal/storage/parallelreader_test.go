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

package storage

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	defaultParallelReader = func() *ParallelReader {
		r := &ParallelReader{
			bucket: defaultBucketHandle,
		}
		return r
	}
)

func TestNewParallelReader(t *testing.T) {
	tests := []struct {
		name    string
		rw      ReadWriter
		wantErr error
	}{
		{
			name:    "Placeholder",
			rw:      defaultReadWriter,
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := test.rw.NewParallelReader(context.Background())
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("NewParallelReader() = %v, want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name      string
		r         *ParallelReader
		p         []byte
		wantBytes int
		wantErr   error
	}{
		{
			name:      "Placeholder",
			r:         defaultParallelReader(),
			p:         []byte("123456789"),
			wantBytes: 9,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotBytes, gotErr := test.r.Read(test.p)

			if gotBytes != test.wantBytes {
				t.Errorf("Read() = %v, want %v", gotBytes, test.wantBytes)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Read() = %v, want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestReaderClose(t *testing.T) {
	tests := []struct {
		name    string
		r       *ParallelReader
		p       []byte
		wantErr error
	}{
		{
			name:    "Placeholder",
			r:       defaultParallelReader(),
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.r.Read(test.p)
			gotErr := test.r.Close()
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Close() = %v, want %v", gotErr, test.wantErr)
			}
		})
	}
}
