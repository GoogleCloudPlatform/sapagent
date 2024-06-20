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
				Name:       "object1.txt",
			},
			Content: testingContent1,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object2.txt",
			},
			Content: testingContent2,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object3.txt",
			},
			Content: testingContent3,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object4.txt",
			},
			Content: testingContent4,
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
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "contentwEOL.txt",
			},
			Content: testingContentwEOL,
		},
	})
}

func bucketHandleParallel(bucketName string, server *fakestorage.Server) *storage.BucketHandle {
	return server.Client().Bucket(bucketName)
}

const (
	bucketNameParallel = "test-bucket"
)

var (
	defaultfakeServerParallel   = fakeServerParallel(bucketNameParallel)                              //	server for parallel tests
	defaultBucketHandleParallel = bucketHandleParallel(bucketNameParallel, defaultfakeServerParallel) //	bucket handle for parallel tests
	defaultPBufferSize          = 4
	deafultPartSize             = 10
	testingContent1             = []byte("test")                                         //	objSize = 4
	testingContent2             = []byte("test.")                                        //	objSize = 5
	testingContent3             = []byte("chunk-len")                                    //	objSize = 9
	testingContent4             = []byte("chunk-len.")                                   //	objSize = 10
	emptyContent                = []byte("")                                             //	objSize = 0
	testingLargeContent         = []byte("Testing when Read() is called multiple times") //	objSize = 44
	testingContentwEOL          = []byte("abc\n")                                        // content with explicit end of line, objSize = 4

	defaultParallelReader = func(objName string, content []byte) *ParallelReader {
		r := &ParallelReader{
			ctx:           context.Background(),
			object:        defaultBucketHandleParallel.Object(objName),
			objectSize:    int64(len(content)),
			partSizeBytes: int64(deafultPartSize),
			workers:       make([]*downloadWorker, 1),
		}
		for i := 0; i < len(r.workers); i++ {
			r.workers[i] = &downloadWorker{
				bucket: defaultBucketHandleParallel,
				buffer: make([]byte, r.partSizeBytes),
			}
		}
		return r
	}
)

func TestNewParallelReader(t *testing.T) {
	tests := []struct {
		name    string
		object  *storage.ObjectHandle
		rw      ReadWriter
		wantErr error
	}{
		{
			name:    "NoObjectFailure",
			rw:      defaultReadWriter,
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "NewParallelReaderSuccess",
			object: defaultBucketHandleParallel.Object("object1.txt"),
			rw: ReadWriter{
				TotalBytes:              int64(len(testingContent1)),
				ChunkSizeMb:             DefaultChunkSizeMb,
				ParallelDownloadWorkers: 1,
				ParallelDownloadConnectParams: &ConnectParameters{
					StorageClient:    defaultStorageClient,
					BucketName:       bucketNameParallel,
					VerifyConnection: true,
				},
			},
			wantErr: nil,
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
			name:       "NilParallelReaderFailure",
			r:          nil,
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  0,
			wantErr:    cmpopts.AnyError,
			wantBuffer: []byte(""),
		},
		{
			name:       "NoObjectFailure",
			r:          defaultParallelReader("", nil),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  0,
			wantErr:    cmpopts.AnyError,
			wantBuffer: []byte(""),
		},
		{
			name:       "ReadSuccess",
			r:          defaultParallelReader("object1.txt", testingContent1),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(testingContent1)),
			wantErr:    nil,
			wantBuffer: testingContent1,
		},
		{
			name:       "ReadEmptySuccess",
			r:          defaultParallelReader("emptyObject.txt", emptyContent),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(emptyContent)),
			wantErr:    io.EOF,
			wantBuffer: emptyContent,
		},
		{
			name:       "ReadMoreThanPBufferSizeSuccess",
			r:          defaultParallelReader("object2.txt", testingContent2),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  defaultPBufferSize,
			wantErr:    nil,
			wantBuffer: testingContent2[:defaultPBufferSize],
		},
		{
			name:       "ReadLargeContentSuccess",
			r:          defaultParallelReader("largeObject.txt", testingLargeContent),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  defaultPBufferSize,
			wantErr:    nil,
			wantBuffer: testingLargeContent[:defaultPBufferSize],
		},
		{
			name:       "ReadContentwEOLSuccess",
			r:          defaultParallelReader("contentwEOL.txt", testingContentwEOL),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  defaultPBufferSize,
			wantErr:    nil,
			wantBuffer: testingContentwEOL,
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
			r:          defaultParallelReader("object1.txt", testingContent1),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(testingContent1)),
			wantErr:    io.EOF,
			wantBuffer: testingContent1,
		},
		{
			name:       "ReadEmptySuccess",
			r:          defaultParallelReader("emptyObject.txt", emptyContent),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  (len(emptyContent)),
			wantErr:    io.EOF,
			wantBuffer: emptyContent,
		},
		{
			name:       "ReadMoreThanPBufferSizeSuccess",
			r:          defaultParallelReader("object2.txt", testingContent2),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  len(testingContent2),
			wantErr:    io.EOF,
			wantBuffer: testingContent2,
		},
		{
			name:       "ReadLessThanOneChunkSuccess",
			r:          defaultParallelReader("object3.txt", testingContent3),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  len(testingContent3),
			wantErr:    io.EOF,
			wantBuffer: testingContent3,
		},
		{
			name:       "ReadOneChunkSuccess",
			r:          defaultParallelReader("object4.txt", testingContent4),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  len(testingContent4),
			wantErr:    io.EOF,
			wantBuffer: testingContent4,
		},
		{
			name:       "ReadLargeContentSuccess",
			r:          defaultParallelReader("largeObject.txt", testingLargeContent),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  len(testingLargeContent),
			wantErr:    io.EOF,
			wantBuffer: testingLargeContent,
		},
		{
			name:       "ReadContentwEOLSuccess",
			r:          defaultParallelReader("contentwEOL.txt", testingContentwEOL),
			p:          make([]byte, defaultPBufferSize),
			wantBytes:  defaultPBufferSize,
			wantErr:    io.EOF,
			wantBuffer: testingContentwEOL,
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

func TestFillWorkerBuffer(t *testing.T) {
	tests := []struct {
		name    string
		r       *ParallelReader
		worker  *downloadWorker
		wantErr error
	}{
		{
			name:    "NoWorkerFailure",
			r:       defaultParallelReader("object1.txt", testingContent1),
			worker:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FillDataSuccess",
			r:    defaultParallelReader("object1.txt", testingContent1),
			worker: &downloadWorker{
				bucket: defaultBucketHandleParallel,
				buffer: make([]byte, deafultPartSize),
			},
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotErr := fillWorkerBuffer(test.r, test.worker)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("fillWorkerBuffer() = %v, want %v", gotErr, test.wantErr)
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
			r:       defaultParallelReader("emptyObject.txt", emptyContent),
			p:       make([]byte, defaultPBufferSize),
			wantErr: nil,
		},
		{
			name:    "ReadAndCloseSuccess",
			r:       defaultParallelReader("object1.txt", testingContent1),
			p:       make([]byte, defaultPBufferSize),
			wantErr: nil,
		},
		{
			name:    "ReadAndCloseLargeContentSuccess",
			r:       defaultParallelReader("largeObject.txt", testingLargeContent),
			p:       make([]byte, defaultPBufferSize),
			wantErr: nil,
		},
		{
			name:    "ReadAndCloseContentwEOLSuccess",
			r:       defaultParallelReader("contentwEOL.txt", testingContentwEOL),
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
