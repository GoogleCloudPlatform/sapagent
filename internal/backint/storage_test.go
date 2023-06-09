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

package backint

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

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
			Content: []byte("test content"),
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
	defaultBuffer = bytes.NewBufferString("test data")
)

func TestConnectToBucket(t *testing.T) {
	tests := []struct {
		name   string
		b      *Backint
		client StorageClient
		want   *storage.BucketHandle
		wantOk bool
	}{
		{
			name: "ClientCreateFail",
			b: &Backint{
				config: &bpb.BackintConfiguration{},
			},
			client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				return nil, errors.New("client create error")
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ClientCreateFailServiceAccount",
			b: &Backint{
				config: &bpb.BackintConfiguration{
					ServiceAccount: "test-account",
				},
			},
			client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				return nil, errors.New("client create error")
			},
			want:   nil,
			wantOk: false,
		},
		{
			name: "ConnectFail",
			b: &Backint{
				config: &bpb.BackintConfiguration{
					Bucket: "fake-bucket",
				},
			},
			client: defaultStorageClient,
			want:   nil,
			wantOk: false,
		},
		{
			name: "ConnectSuccess",
			b: &Backint{
				config: &bpb.BackintConfiguration{
					Bucket: "test-bucket",
				},
			},
			client: defaultStorageClient,
			want:   fakeServer.Client().Bucket("test-bucket"),
			wantOk: true,
		},
		{
			name: "ConnectSuccessServiceAccount",
			b: &Backint{
				config: &bpb.BackintConfiguration{
					Bucket:         "test-bucket",
					ServiceAccount: "test-account",
				},
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotOk := test.b.ConnectToBucket(context.Background(), test.client)
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.IgnoreUnexported(storage.BucketHandle{})); diff != "" {
				t.Errorf("ConnectToBucket(%v, %v) had unexpected diff (-want +got):\n%s", test.b.config.GetServiceAccount(), test.b.config.GetBucket(), diff)
			}
			if gotOk != test.wantOk {
				t.Errorf("ConnectToBucket(%v, %v) = %v, want %v", test.b.config.GetServiceAccount(), test.b.config.GetBucket(), gotOk, test.wantOk)
			}
		})
	}
}

func TestUpload(t *testing.T) {
	tests := []struct {
		name   string
		b      *Backint
		src    *bytes.Buffer
		copier IOFileCopier
		want   error
	}{
		{
			name: "NoHandle",
			b:    &Backint{},
			src:  defaultBuffer,
			want: cmpopts.AnyError,
		},
		{
			name: "WriteFail",
			b:    &Backint{bucketHandle: defaultBucketHandle},
			copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
				return 0, errors.New("write error")
			},
			src:  defaultBuffer,
			want: cmpopts.AnyError,
		},
		{
			name: "UploadSuccess",
			b: &Backint{
				bucketHandle: defaultBucketHandle,
				config: &bpb.BackintConfiguration{
					BufferSizeMb: 1,
				},
			},
			copier: io.Copy,
			src:    defaultBuffer,
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.Upload(context.Background(), "object.txt", test.src, int64(test.src.Len()), 0, test.copier)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("%#v.Upload(%s) = %v, want %v", test.b, test.src, got, test.want)
			}
		})
	}
}

func TestDownload(t *testing.T) {
	tests := []struct {
		name   string
		b      *Backint
		copier IOFileCopier
		dest   io.Writer
		object string
		want   error
	}{
		{
			name: "NoHandle",
			b:    &Backint{},
			want: cmpopts.AnyError,
		},
		{
			name:   "ObjectNotFound",
			b:      &Backint{bucketHandle: defaultBucketHandle},
			object: "fake-object.txt",
			want:   cmpopts.AnyError,
		},
		{
			name: "WriteFail",
			b:    &Backint{bucketHandle: defaultBucketHandle},
			copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
				return 0, errors.New("write error")
			},
			object: "object.txt",
			want:   cmpopts.AnyError,
		},
		{
			name:   "DownloadSuccess",
			b:      &Backint{bucketHandle: defaultBucketHandle},
			copier: io.Copy,
			object: "object.txt",
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.Download(context.Background(), test.object, defaultBuffer, 0, test.copier)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("%#v.Download(%s) = %v, want %v", test.b, test.object, got, test.want)
			}
		})
	}
}
