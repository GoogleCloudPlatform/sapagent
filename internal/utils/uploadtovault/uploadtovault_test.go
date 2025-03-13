/*
Copyright 2025 Google LLC

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

package uploadtovault

import (
	"context"
	"os"
	"testing"

	st "cloud.google.com/go/storage"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/secret"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/storage"
)

type fakeReadWriter struct {
	err error
}

func (f *fakeReadWriter) Upload(ctx context.Context) (int64, error) {
	return 0, f.err
}

func TestUploadFiles(t *testing.T) {
	tests := []struct {
		name    string
		params  Params
		wantErr bool
	}{
		{
			name: "success",
			params: Params{
				FilesPath: []string{"/tmp/file1.txt", "/tmp/file2.txt"},
				FS: &fake.FileSystem{
					OpenErr:  []error{nil, nil},
					OpenResp: []*os.File{&os.File{}, &os.File{}},
					StatErr:  []error{nil, nil},
					StatResp: []os.FileInfo{
						fake.MockedFileInfo{FileName: "file1.txt", FileSize: 100, FileMode: 0777},
						fake.MockedFileInfo{FileName: "file2.txt", FileSize: 200, FileMode: 0777},
					},
				},
				RW: func(rw storage.ReadWriter) uploader {
					return &fakeReadWriter{
						err: nil,
					}
				},
				BucketName: "test-bucket",
				OAuthToken: secret.String("test-token"),
				ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
					return &st.BucketHandle{}, true
				},
			},
			wantErr: false,
		},
		{
			name: "openFileFail",
			params: Params{
				FilesPath: []string{"/tmp/file1.txt", "/tmp/file2.txt"},
				FS: &fake.FileSystem{
					OpenErr:  []error{cmpopts.AnyError},
					OpenResp: []*os.File{nil},
					StatErr:  []error{cmpopts.AnyError},
					StatResp: []os.FileInfo{},
				},
				RW: func(rw storage.ReadWriter) uploader {
					return &fakeReadWriter{
						err: nil,
					}
				},
				BucketName: "test-bucket",
				OAuthToken: secret.String("test-token"),
				ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
					return &st.BucketHandle{}, true
				},
			},
			wantErr: true,
		},
		{
			name: "statFileFail",
			params: Params{
				FilesPath: []string{"/tmp/file1.txt", "/tmp/file2.txt"},
				FS: &fake.FileSystem{
					OpenErr:  []error{nil, nil},
					OpenResp: []*os.File{&os.File{}, &os.File{}},
					StatErr:  []error{cmpopts.AnyError, nil},
					StatResp: []os.FileInfo{
						fake.MockedFileInfo{FileName: "file1.txt", FileSize: 100, FileMode: 0777},
						fake.MockedFileInfo{FileName: "file2.txt", FileSize: 200, FileMode: 0777},
					},
				},
				RW: func(rw storage.ReadWriter) uploader {
					return &fakeReadWriter{
						err: nil,
					}
				},
				BucketName: "test-bucket",
				OAuthToken: secret.String("test-token"),
				ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
					return &st.BucketHandle{}, true
				},
			},
			wantErr: true,
		},
		{
			name: "connectToBucketFail",
			params: Params{
				FilesPath: []string{"/tmp/file1.txt", "/tmp/file2.txt"},
				FS: &fake.FileSystem{
					OpenErr:  []error{nil, nil},
					OpenResp: []*os.File{&os.File{}, &os.File{}},
					StatErr:  []error{nil, nil},
					StatResp: []os.FileInfo{
						fake.MockedFileInfo{FileName: "file1.txt", FileSize: 100, FileMode: 0777},
						fake.MockedFileInfo{FileName: "file2.txt", FileSize: 200, FileMode: 0777},
					},
				},
				RW: func(rw storage.ReadWriter) uploader {
					return &fakeReadWriter{
						err: nil,
					}
				},
				BucketName: "test-bucket",
				OAuthToken: secret.String("test-token"),
				ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
					return nil, false
				},
			},
			wantErr: true,
		},
		{
			name: "uploadFail",
			params: Params{
				FilesPath: []string{"/tmp/file1.txt", "/tmp/file2.txt"},
				FS: &fake.FileSystem{
					OpenErr:  []error{nil, nil},
					OpenResp: []*os.File{&os.File{}, &os.File{}},
					StatErr:  []error{nil, nil},
					StatResp: []os.FileInfo{
						fake.MockedFileInfo{FileName: "file1.txt", FileSize: 100, FileMode: 0777},
						fake.MockedFileInfo{FileName: "file2.txt", FileSize: 200, FileMode: 0777},
					},
				},
				RW: func(rw storage.ReadWriter) uploader {
					return &fakeReadWriter{
						err: cmpopts.AnyError,
					}
				},
				BucketName: "test-bucket",
				OAuthToken: secret.String("test-token"),
				ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
					return &st.BucketHandle{}, true
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if gotErr := uploadFiles(context.Background(), tc.params); (gotErr != nil) != tc.wantErr {
				t.Errorf("uploadFiles(%v) returned an unexpected error: %v", tc.params, gotErr)
			}
		})
	}
}
