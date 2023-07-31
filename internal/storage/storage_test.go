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

package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

var (
	fakeServer = fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "object.txt",
			},
			Content: defaultContent,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  "test-bucket",
				Name:        "compressed-object.txt",
				ContentType: compressedContentType,
			},
			Content: defaultCompressedContent(),
		},
	})
	defaultBucketHandle = fakeServer.Client().Bucket("test-bucket")
	defaultFileCopier   = func(dst io.Writer, src io.Reader) (written int64, err error) { return 1, nil }
	defaultFileWriter   = func(dst io.Writer, src io.Reader) (written int64, err error) {
		n, err := dst.Write(make([]byte, 1*1024*1024))
		return int64(n), err
	}
	defaultStorageClient = func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
		return fakeServer.Client(), nil
	}
	defaultContent = []byte("test content")
)

func objectAttrs(object *storage.ObjectHandle) []*storage.ObjectAttrs {
	var attrs []*storage.ObjectAttrs
	if attr, err := object.Attrs(context.Background()); err == nil {
		attrs = append(attrs, attr)
	}
	return attrs
}

func defaultBuffer() *bytes.Buffer {
	return bytes.NewBuffer(defaultContent)
}

func defaultCompressedContent() []byte {
	compressedContent := bytes.NewBufferString("")
	gzipWriter := gzip.NewWriter(compressedContent)
	if _, err := io.Copy(gzipWriter, bytes.NewBuffer(defaultContent)); err != nil {
		return nil
	}
	gzipWriter.Close()
	return compressedContent.Bytes()
}

func emptyServer(bucketName string) *fakestorage.Server {
	server := fakestorage.NewServer([]fakestorage.Object{})
	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: bucketName})
	return server
}

func TestConnectToBucket(t *testing.T) {
	tests := []struct {
		name   string
		config *bpb.BackintConfiguration
		client Client
		want   *storage.BucketHandle
		wantOk bool
	}{
		{
			name:   "ClientCreateFail",
			config: &bpb.BackintConfiguration{},
			client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				return nil, errors.New("client create error")
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ClientCreateFailServiceAccount",
			config: &bpb.BackintConfiguration{
				ServiceAccount: "test-account",
			},
			client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				return nil, errors.New("client create error")
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ConnectFail",
			config: &bpb.BackintConfiguration{
				Bucket: "fake-bucket",
			},
			client: defaultStorageClient,
			want:   nil,
			wantOk: false,
		},
		{
			name: "ConnectSuccess",
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			client: defaultStorageClient,
			want:   fakeServer.Client().Bucket("test-bucket"),
			wantOk: true,
		},
		{
			name: "ConnectSuccessServiceAccount",
			config: &bpb.BackintConfiguration{
				Bucket:         "test-bucket",
				ServiceAccount: "test-account",
			},
			client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				if opts == nil {
					return nil, errors.New("client create error")
				}
				return fakeServer.Client(), nil
			},
			want:   fakeServer.Client().Bucket("test-bucket"),
			wantOk: true,
		},
		{
			name: "ConnectSuccessEmptyBucket",
			config: &bpb.BackintConfiguration{
				Bucket: "empty-bucket",
			},
			client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				return emptyServer("empty-bucket").Client(), nil
			},
			want:   emptyServer("empty-bucket").Client().Bucket("empty-bucket"),
			wantOk: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotOk := ConnectToBucket(context.Background(), test.client, test.config.GetServiceAccount(), test.config.GetBucket())
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.IgnoreUnexported(storage.BucketHandle{})); diff != "" {
				t.Errorf("ConnectToBucket(%v, %v) had unexpected diff (-want +got):\n%s", test.config.GetServiceAccount(), test.config.GetBucket(), diff)
			}
			if gotOk != test.wantOk {
				t.Errorf("ConnectToBucket(%v, %v) = %v, want %v", test.config.GetServiceAccount(), test.config.GetBucket(), gotOk, test.wantOk)
			}
		})
	}
}

func TestUpload(t *testing.T) {
	tests := []struct {
		name      string
		rw        *ReadWriter
		want      int64
		wantError error
	}{
		{
			name: "NoHandle",
			rw: &ReadWriter{
				Reader: defaultBuffer(),
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "WriteFail",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				Reader:       defaultBuffer(),
				Copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("write error")
				},
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "DumpDataWriteFail",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				Reader:       defaultBuffer(),
				Copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("write error")
				},
				DumpData: true,
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "CompressWriteFail",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				Reader:       defaultBuffer(),
				Copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("write error")
				},
				Compress: true,
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "UploadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ChunkSizeMb:  1,
				Copier:       io.Copy,
				Reader:       defaultBuffer(),
				LogDelay:     time.Nanosecond,
				ObjectName:   "object.txt",
			},
			want:      int64(defaultBuffer().Len()),
			wantError: nil,
		},
		{
			name: "UploadSuccessWithRateLimit",
			rw: &ReadWriter{
				BucketHandle:   defaultBucketHandle,
				ChunkSizeMb:    1,
				Copier:         io.Copy,
				Reader:         defaultBuffer(),
				LogDelay:       time.Nanosecond,
				RateLimitBytes: 1,
				ObjectName:     "object.txt",
			},
			want:      int64(defaultBuffer().Len()),
			wantError: nil,
		},
		{
			name: "CompressUploadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ChunkSizeMb:  1,
				Copier:       io.Copy,
				Reader:       defaultBuffer(),
				LogDelay:     time.Nanosecond,
				Compress:     true,
				ObjectName:   "object.txt",
			},
			want:      int64(defaultBuffer().Len()),
			wantError: nil,
		},
		{
			name: "DumpDataUploadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ChunkSizeMb:  1,
				Copier:       io.Copy,
				Reader:       defaultBuffer(),
				LogDelay:     time.Nanosecond,
				DumpData:     true,
				ObjectName:   "object.txt",
			},
			want:      int64(defaultBuffer().Len()),
			wantError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotError := test.rw.Upload(context.Background())
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("%#v.Upload() = %v, want %v", test.rw, gotError, test.wantError)
			}
			if got != test.want {
				t.Errorf("%#v.Upload() = %v, want %v", test.rw, got, test.want)
			}
		})
	}
}

func TestDownload(t *testing.T) {
	tests := []struct {
		name      string
		rw        *ReadWriter
		want      int64
		wantError error
	}{
		{
			name: "NoHandle",
			rw: &ReadWriter{
				Writer: defaultBuffer(),
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "ObjectNotFound",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "fake-object.txt",
				Writer:       defaultBuffer(),
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "WriteFail",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "object.txt",
				Writer:       defaultBuffer(),
				Copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("write error")
				},
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "DecompressWriteFail",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "compressed-object.txt",
				Writer:       defaultBuffer(),
				Copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("write error")
				},
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "DownloadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "object.txt",
				Writer:       defaultBuffer(),
				Copier:       io.Copy,
				LogDelay:     time.Nanosecond,
			},
			want:      int64(len(defaultContent)),
			wantError: nil,
		},
		{
			name: "DownloadSuccessWithRateLimit",
			rw: &ReadWriter{
				BucketHandle:   defaultBucketHandle,
				ObjectName:     "object.txt",
				Writer:         defaultBuffer(),
				Copier:         io.Copy,
				LogDelay:       time.Nanosecond,
				RateLimitBytes: 1,
			},
			want:      int64(len(defaultContent)),
			wantError: nil,
		},
		{
			name: "DecompressDownloadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "compressed-object.txt",
				Writer:       defaultBuffer(),
				Copier:       io.Copy,
				LogDelay:     time.Nanosecond,
			},
			want:      int64(len(defaultContent)),
			wantError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotError := test.rw.Download(context.Background())
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("%#v.Download() = %v, want %v", test.rw, gotError, test.wantError)
			}
			if got != test.want {
				t.Errorf("%#v.Download() = %v, want %v", test.rw, got, test.want)
			}
		})
	}
}

func TestListObjects(t *testing.T) {
	tests := []struct {
		name      string
		bucket    *storage.BucketHandle
		prefix    string
		want      []*storage.ObjectAttrs
		wantError error
	}{
		{
			name:      "NoHandle",
			want:      nil,
			wantError: cmpopts.AnyError,
		},
		{
			name:      "PrefixNotFound",
			bucket:    defaultBucketHandle,
			prefix:    "fake-object.txt",
			want:      nil,
			wantError: nil,
		},
		{
			name:      "PrefixFound",
			bucket:    defaultBucketHandle,
			prefix:    "object.txt",
			want:      objectAttrs(fakeServer.Client().Bucket("test-bucket").Object("object.txt")),
			wantError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotError := ListObjects(context.Background(), test.bucket, test.prefix)
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("ListObjects(%s) = %v, want %v", test.prefix, gotError, test.wantError)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("ListObjects(%s) had unexpected diff (-want +got):\n%s", test.prefix, diff)
			}
		})
	}
}

func TestDeleteObject(t *testing.T) {
	tests := []struct {
		name   string
		bucket *storage.BucketHandle
		object string
		want   error
	}{
		{
			name: "NoHandle",
			want: cmpopts.AnyError,
		},
		{
			name:   "FileNotFound",
			bucket: defaultBucketHandle,
			object: "fake-object.txt",
			want:   cmpopts.AnyError,
		},
		{
			name:   "FileDeleted",
			bucket: defaultBucketHandle,
			object: "object.txt",
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DeleteObject(context.Background(), test.bucket, test.object)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("DeleteObject(%s) = %v, want %v", test.object, got, test.want)
			}
		})
	}
}
