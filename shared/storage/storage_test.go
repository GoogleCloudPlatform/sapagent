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
	"os"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func fakeServer(bucketName string) *fakestorage.Server {
	return fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object.txt",
			},
			Content: defaultContent,
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName:  bucketName,
				Name:        "compressed-object.txt",
				ContentType: compressedContentType,
			},
			Content: defaultCompressedContent(),
		},
	})
}

func bucketHandle(bucketName string, server *fakestorage.Server) *storage.BucketHandle {
	return server.Client().Bucket(bucketName)
}

const (
	defaultBucketName  = "test-bucket"
	deletionBucketName = "test-bucket-deletion"
	listingBucketName  = "test-bucket-listing"
)

var (
	defaultFakeServer     = fakeServer(defaultBucketName)
	fakeServerForDeletion = fakeServer(deletionBucketName)
	fakeServerForListing  = fakeServer(listingBucketName)
	defaultBucketHandle   = bucketHandle(defaultBucketName, defaultFakeServer)
	deletionBucketHandle  = bucketHandle(deletionBucketName, fakeServerForDeletion)
	listingBucketHandle   = bucketHandle(listingBucketName, fakeServerForListing)
	defaultStorageClient  = func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
		return defaultFakeServer.Client(), nil
	}
	defaultContent = []byte("test content")
)

func objectAttrs(objects []*storage.ObjectHandle) []*storage.ObjectAttrs {
	var attrs []*storage.ObjectAttrs
	for _, object := range objects {
		if attr, err := object.Attrs(context.Background()); err == nil {
			attrs = append(attrs, attr)
		}
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
		p      *ConnectParameters
		want   *storage.BucketHandle
		wantOk bool
	}{
		{
			name: "ClientCreateFail",
			p: &ConnectParameters{
				StorageClient: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
					return nil, errors.New("client create error")
				},
				VerifyConnection: true,
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ClientCreateFailServiceAccount",
			p: &ConnectParameters{
				StorageClient: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
					return nil, errors.New("client create error")
				},
				VerifyConnection: true,
				ServiceAccount:   "test-account",
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ConnectFail",
			p: &ConnectParameters{
				StorageClient:    defaultStorageClient,
				VerifyConnection: true,
				BucketName:       "fake-bucket",
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ConnectFailNoVerify",
			p: &ConnectParameters{
				StorageClient: defaultStorageClient,
				BucketName:    "fake-bucket",
			},
			want:   emptyServer("fake-bucket").Client().Bucket("fake-bucket"),
			wantOk: true,
		},
		{
			name: "ConnectSuccess",
			p: &ConnectParameters{
				StorageClient:    defaultStorageClient,
				BucketName:       "test-bucket",
				VerifyConnection: true,
			},
			want:   defaultFakeServer.Client().Bucket("test-bucket"),
			wantOk: true,
		},
		{
			name: "ConnectSuccessServiceAccount",
			p: &ConnectParameters{
				StorageClient: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
					if opts == nil {
						return nil, errors.New("client create error")
					}
					return defaultFakeServer.Client(), nil
				},
				BucketName:       "test-bucket",
				VerifyConnection: true,
				ServiceAccount:   "test-account",
				UserAgentSuffix:  "test-user-agent",
				Endpoint:         "test-endpoint",
			},
			want:   defaultFakeServer.Client().Bucket("test-bucket"),
			wantOk: true,
		},
		{
			name: "ConnectSuccessEmptyBucket",
			p: &ConnectParameters{
				StorageClient: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
					return emptyServer("empty-bucket").Client(), nil
				},
				BucketName:       "empty-bucket",
				VerifyConnection: true,
			},
			want:   emptyServer("empty-bucket").Client().Bucket("empty-bucket"),
			wantOk: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotOk := ConnectToBucket(context.Background(), test.p)
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.IgnoreUnexported(storage.BucketHandle{})); diff != "" {
				t.Errorf("ConnectToBucket(%v) had unexpected diff (-want +got):\n%s", test.p, diff)
			}
			if gotOk != test.wantOk {
				t.Errorf("ConnectToBucket(%v) = %v, want %v", test.p, gotOk, test.wantOk)
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
			name: "XMLMultipartWriteFail",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				Reader:       defaultBuffer(),
				Copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("write error")
				},
				XMLMultipartUpload: true,
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "FailBothEncryptionAndKMSDefined",
			rw: &ReadWriter{
				BucketHandle:  defaultBucketHandle,
				Copier:        io.Copy,
				Reader:        defaultBuffer(),
				ObjectName:    "object.txt",
				EncryptionKey: "fake-key",
				KMSKey:        "fake-kms-key",
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
				StorageClass: "STANDARD",
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
		{
			name: "EncryptionKeyUploadFailure",
			rw: &ReadWriter{
				BucketHandle:  defaultBucketHandle,
				Copier:        io.Copy,
				Reader:        defaultBuffer(),
				ObjectName:    "object.txt",
				EncryptionKey: "this-is-a-fake-key-with-32-bytes",
			},
			want:      0,
			wantError: cmpopts.AnyError,
		},
		{
			name: "EncryptionKeyUploadSuccess",
			rw: &ReadWriter{
				BucketHandle:  defaultBucketHandle,
				Copier:        io.Copy,
				Reader:        defaultBuffer(),
				ObjectName:    "object.txt",
				EncryptionKey: "dGhpcy1pcy1hLWZha2Uta2V5LXdpdGgtMzItYnl0ZXM=",
			},
			want:      int64(defaultBuffer().Len()),
			wantError: nil,
		},
		{
			name: "KMSKeyUploadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				Copier:       io.Copy,
				Reader:       defaultBuffer(),
				ObjectName:   "object.txt",
				KMSKey:       "fake-kms-key",
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
			name: "ObjectNotFoundWithRetries",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "fake-object.txt",
				Writer:       defaultBuffer(),
				MaxRetries:   1,
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
		{
			name: "EncryptionKeyDownloadFailure",
			rw: &ReadWriter{
				BucketHandle:  defaultBucketHandle,
				ObjectName:    "object.txt",
				Writer:        defaultBuffer(),
				Copier:        io.Copy,
				LogDelay:      time.Nanosecond,
				EncryptionKey: "this-is-a-fake-key-with-32-bytes",
			},
			want:      0,
			wantError: cmpopts.AnyError,
		},
		{
			name: "EncryptionKeyDownloadSuccess",
			rw: &ReadWriter{
				BucketHandle:  defaultBucketHandle,
				ObjectName:    "object.txt",
				Writer:        defaultBuffer(),
				Copier:        io.Copy,
				LogDelay:      time.Nanosecond,
				EncryptionKey: "dGhpcy1pcy1hLWZha2Uta2V5LXdpdGgtMzItYnl0ZXM=",
			},
			want:      int64(len(defaultContent)),
			wantError: nil,
		},
		{
			name: "KMSKeyDownloadSuccess",
			rw: &ReadWriter{
				BucketHandle: defaultBucketHandle,
				ObjectName:   "object.txt",
				Writer:       defaultBuffer(),
				Copier:       io.Copy,
				LogDelay:     time.Nanosecond,
				KMSKey:       "fake-kms-key",
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
		filter    string
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
			bucket:    listingBucketHandle,
			prefix:    "fake-object.txt",
			want:      nil,
			wantError: nil,
		},
		{
			name:      "PrefixFound",
			bucket:    listingBucketHandle,
			prefix:    "object.txt",
			want:      objectAttrs([]*storage.ObjectHandle{fakeServerForListing.Client().Bucket("test-bucket-listing").Object("object.txt")}),
			wantError: nil,
		},
		{
			name:      "AllObjects",
			bucket:    listingBucketHandle,
			want:      objectAttrs([]*storage.ObjectHandle{fakeServerForListing.Client().Bucket("test-bucket-listing").Object("compressed-object.txt"), fakeServerForListing.Client().Bucket("test-bucket-listing").Object("object.txt")}),
			wantError: nil,
		},
		{
			name:      "AllObjectsWithFilter",
			bucket:    listingBucketHandle,
			filter:    "compressed",
			want:      objectAttrs([]*storage.ObjectHandle{fakeServerForListing.Client().Bucket("test-bucket-listing").Object("compressed-object.txt")}),
			wantError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotError := ListObjects(context.Background(), test.bucket, test.prefix, test.filter, 0)
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("ListObjects(%s) = %v, want %v", test.prefix, gotError, test.wantError)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("ListObjects(%s) had unexpected diff (-want +got):\n%s", test.name, diff)
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
			bucket: deletionBucketHandle,
			object: "fake-object.txt",
			want:   cmpopts.AnyError,
		},
		{
			name:   "FileDeleted",
			bucket: deletionBucketHandle,
			object: "object.txt",
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DeleteObject(context.Background(), test.bucket, test.object, 0)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("DeleteObject(%s) = %v, want %v", test.object, got, test.want)
			}
		})
	}
}
