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
	"bytes"
	"context"
	"io"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
)

func fakeServerParallel(bucketName string) *fakestorage.Server {
	return fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object.txt",
			},
			Content: testingContent,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "emptyObject.txt",
			},
			Content: emptyContent,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "largeObject.txt",
			},
			Content: testingLargeContent,
		},
	})
}

func bucketHandleParallel(bucketName string, server *fakestorage.Server) *storage.BucketHandle {
	return server.Client().Bucket(bucketName)
}

func defaultReader(objectName string, offset int64, length int64) io.Reader {
	reader, _ := defaultBucketHandleParallel.Object(objectName).NewRangeReader(context.Background(), offset, length)
	return reader
}

const (
	bucketNameParallel = "test-bucket"
)

var (
	defaultfakeServerParallel   = fakeServerParallel(bucketNameParallel)                              //	server for parallel tests
	defaultBucketHandleParallel = bucketHandleParallel(bucketNameParallel, defaultfakeServerParallel) //	bucket handle for parallel tests
	defaultPBufferSize          = 7
	testingContent              = []byte("testing")
	emptyContent                = []byte("")
	testingLargeContent         = []byte("Testing when Read() is called multiple times")
)

func TestNewParallelReader(t *testing.T) {
	tests := []struct {
		name    string
		object  *storage.ObjectHandle
		rw      ReadWriter
		wantErr error
	}{
		{
			name:    "NoObjectPassed",
			object:  nil,
			rw:      defaultReadWriter,
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := test.rw.NewParallelReader(context.Background(), test.object)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("NewParallelReader() = %v, want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name       string
		r          *ParallelReader
		p          []byte
		wantBytes  int
		wantErr    error
		wantBuffer []byte
	}{
		{
			name:       "NilReaderFailure",
			r:          &ParallelReader{reader: nil, bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  0,
			wantErr:    cmpopts.AnyError,
			wantBuffer: []byte(""),
		},
		{
			name:       "NilParallelReaderFailure",
			r:          nil,
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  0,
			wantErr:    cmpopts.AnyError,
			wantBuffer: []byte(""),
		},
		{
			name:       "ReadSuccess",
			r:          &ParallelReader{reader: defaultReader("object.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(testingContent)),
			wantErr:    nil,
			wantBuffer: []byte(testingContent),
		},
		{
			name:       "ReadEmptySuccess",
			r:          &ParallelReader{reader: defaultReader("emptyObject.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(emptyContent))},
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(emptyContent)),
			wantErr:    io.EOF,
			wantBuffer: []byte(emptyContent),
		},
		{
			name:       "ReadMoreThanPBufferSizeSuccess",
			r:          &ParallelReader{reader: defaultReader("object.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:          make([]byte, defaultPBufferSize-1),
			wantBytes:  defaultPBufferSize - 1,
			wantErr:    nil,
			wantBuffer: []byte(testingContent[:defaultPBufferSize-1]),
		},
		{
			name:       "ReadLessThanPBufferSizeSuccess",
			r:          &ParallelReader{reader: defaultReader("object.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:          make([]byte, defaultPBufferSize+1),
			wantBytes:  len(testingContent),
			wantErr:    nil,
			wantBuffer: []byte(testingContent),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotBytes, gotErr := test.r.Read(test.p)
			gotBuffer := test.p[:gotBytes]

			if gotBytes != test.wantBytes {
				t.Errorf("Read() = %v, want %v", gotBytes, test.wantBytes)
			}
			if !bytes.Equal(gotBuffer, test.wantBuffer) {
				t.Errorf("Read() = %v, want %v", gotBuffer, test.wantBuffer)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Read() = %v, want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestReadMultiple(t *testing.T) {
	tests := []struct {
		name       string
		r          *ParallelReader
		p          []byte
		wantBytes  int
		wantErr    error
		wantBuffer []byte
	}{
		{
			name:       "ReadSuccess",
			r:          &ParallelReader{reader: defaultReader("object.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(testingContent)),
			wantErr:    io.EOF,
			wantBuffer: testingContent,
		},
		{
			name:       "ReadEmptySuccess",
			r:          &ParallelReader{reader: defaultReader("emptyObject.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(emptyContent))},
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(emptyContent)),
			wantErr:    io.EOF,
			wantBuffer: emptyContent,
		},
		{
			name:       "ReadMoreThanPBufferSizeSuccess",
			r:          &ParallelReader{reader: defaultReader("object.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:          make([]byte, defaultPBufferSize-1),
			wantBytes:  (len(testingContent)),
			wantErr:    io.EOF,
			wantBuffer: testingContent,
		},
		{
			name:       "ReadLargeContentSuccess",
			r:          &ParallelReader{reader: defaultReader("largeObject.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingLargeContent))},
			p:          make([]byte, 2),
			wantBytes:  (len(testingLargeContent)),
			wantErr:    io.EOF,
			wantBuffer: testingLargeContent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotBytes int
			var gotErr error
			var gotBuffer []byte
			for gotErr == nil {
				var currentBytes int
				currentBytes, gotErr = test.r.Read(test.p)
				gotBytes += currentBytes
				gotBuffer = append(gotBuffer, test.p[:currentBytes]...)
			}

			if gotBytes != test.wantBytes {
				t.Errorf("Read() = %v, want %v", gotBytes, test.wantBytes)
			}
			if !bytes.Equal(gotBuffer, test.wantBuffer) {
				t.Errorf("Read() = %v, want %v", gotBuffer, test.wantBuffer)
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
			name:    "EmptyDataSuccess",
			r:       &ParallelReader{reader: defaultReader("emptyObject.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(emptyContent))},
			p:       make([]byte, defaultPBufferSize),
			wantErr: nil,
		},
		{
			name:    "ReadAndCloseSuccess",
			r:       &ParallelReader{reader: defaultReader("object.txt", 0, -1), bucket: defaultBucketHandleParallel, objectSize: int64(len(testingContent))},
			p:       make([]byte, defaultPBufferSize),
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
