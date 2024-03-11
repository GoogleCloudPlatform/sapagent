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
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
)

const (
	defaultObjectName = "object.txt"
	fakeUploadID      = "fake-upload-id"

	xmlResponseBodyTemplate = `<?xml version="1.0" encoding="UTF-8"?>
	<ListPartsResult>
		<Bucket>test-bucket</Bucket>
		<Key>object.txt</Key>
		<UploadId>fake-upload-id</UploadId>
	</ListPartsResult>
	`
)

var (
	gcsURLPrefix               = fmt.Sprintf("https://%s.storage.googleapis.com/%s", defaultBucketName, defaultObjectName)
	initMultipartUploadURL     = gcsURLPrefix + "?uploads"
	completeMultipartUploadURL = gcsURLPrefix + fmt.Sprintf("?uploadId=%s", fakeUploadID)
	uploadPartURLPrefix        = gcsURLPrefix + fmt.Sprintf("?uploadId=%s&partNumber=", fakeUploadID)
	partNumberExtracter        = regexp.MustCompile("partNumber=([0-9]+)")

	defaultTokenGetter = func(context.Context, ...string) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "fake-token"}), nil
	}

	defaultReadWriter = ReadWriter{
		BucketHandle:           defaultBucketHandle,
		BucketName:             defaultBucketName,
		ObjectName:             defaultObjectName,
		Metadata:               map[string]string{"X-Backup-Type": "FILE"},
		ChunkSizeMb:            DefaultChunkSizeMb,
		XMLMultipartUpload:     true,
		XMLMultipartWorkers:    16,
		MaxRetries:             3,
		RetryBackoffInitial:    time.Millisecond,
		RetryBackoffMax:        time.Millisecond,
		RetryBackoffMultiplier: 2,
	}

	httpClientError = func(timeout time.Duration, trans *http.Transport) httpClient {
		return &mockHTTPClient{
			do: func(r *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("error")
			},
		}
	}
	httpClientServerError = func(timeout time.Duration, trans *http.Transport) httpClient {
		return &mockHTTPClient{
			do: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					Status:     "500",
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("internal server error")),
				}, nil
			},
		}
	}
	httpClientSuccess = func(timeout time.Duration, trans *http.Transport) httpClient {
		return &mockHTTPClient{
			do: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					Status:     "OK",
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(xmlResponseBodyTemplate)),
				}, nil
			},
		}
	}
)

func httpClientVerify(url string, headers map[string]string) HTTPClient {
	return func(timeout time.Duration, trans *http.Transport) httpClient {
		return &mockHTTPClient{
			do: func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != url {
					return nil, fmt.Errorf("request.URL.String() = %v, want: %v", r.URL.String(), url)
				}
				for k, v := range headers {
					if r.Header.Get(k) != v {
						return nil, fmt.Errorf("request.Header.Get(%v) = %v, want: %v", k, r.Header.Get(k), v)
					}
				}

				return &http.Response{
					Status:     "OK",
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(xmlResponseBodyTemplate)),
				}, nil
			},
		}
	}
}

type mockAuthToken struct {
	token string
}

func (t *mockAuthToken) SetAuthHeader(r *http.Request) {
	r.Header.Set("Authorization", t.token)
}

type mockHTTPClient struct {
	do func(*http.Request) (*http.Response, error)
}

func (c *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.do(req)
}

func createRespBody(text string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(text))
}

func TestNewMultipartWriter(t *testing.T) {
	tests := []struct {
		name         string
		rw           ReadWriter
		newClient    HTTPClient
		tokenGetter  DefaultTokenGetter
		jsonCred     JSONCredentialsGetter
		wantUploadID string
		wantErr      error
	}{
		{
			name: "TokenFetchFailureDefault",
			rw:   defaultReadWriter,
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return nil, fmt.Errorf("token fetch failure")
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TokenFetchFailureServiceAccount",
			rw:   ReadWriter{XMLMultipartServiceAccount: "fake-service-account"},
			jsonCred: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return nil, fmt.Errorf("token fetch failure")
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "InitMultipartUploadFailure",
			rw:          defaultReadWriter,
			tokenGetter: defaultTokenGetter,
			newClient:   httpClientError,
			wantErr:     cmpopts.AnyError,
		},
		{
			name:        "CheckResponseFailure",
			rw:          defaultReadWriter,
			tokenGetter: defaultTokenGetter,
			newClient:   httpClientServerError,
			wantErr:     cmpopts.AnyError,
		},
		{
			name:        "InitMultipartUploadSuccess",
			rw:          defaultReadWriter,
			tokenGetter: defaultTokenGetter,
			newClient: httpClientVerify(initMultipartUploadURL, map[string]string{
				"Content-Length": "0",
				"Content-Type":   "application/octet-stream",
				"Authorization":  "Bearer fake-token",
			}),
			wantUploadID: fakeUploadID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.rw.NewMultipartWriter(context.Background(), test.newClient, test.tokenGetter, test.jsonCred)
			if got != nil && got.uploadID != test.wantUploadID {
				t.Errorf("NewMultipartWriter() = %v, want %v", got.uploadID, test.wantUploadID)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("NewMultipartWriter() = %v, want %v", gotErr, test.wantErr)
			}
		})
	}
}
