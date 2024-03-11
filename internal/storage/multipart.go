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
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
)

const (
	defaultClientEndpoint = "storage.googleapis.com"
)

var (
	defaultNewClient = func(timeout time.Duration, trans *http.Transport) httpClient {
		return &http.Client{Timeout: timeout, Transport: trans}
	}
	defaultTransport = func() *http.Transport {
		return &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxConnsPerHost:       100,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       10 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPClient abstracts creating a new HTTP client to connect to GCS.
type HTTPClient func(timeout time.Duration, trans *http.Transport) httpClient

// DefaultTokenGetter abstracts obtaining a default oauth2 token source.
type DefaultTokenGetter func(context.Context, ...string) (oauth2.TokenSource, error)

// JSONCredentialsGetter abstracts obtaining JSON oauth2 google credentials.
type JSONCredentialsGetter func(context.Context, []byte, ...string) (*google.Credentials, error)

// objectPart stores part numbers and ETag data for the final XML request.
type objectPart struct {
	PartNumber int64  `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

// MultipartWriter is a writer following GCS multipart upload protocol.
type MultipartWriter struct {
	bucket     *storage.BucketHandle
	token      *oauth2.Token
	httpClient httpClient

	objectName        string
	fileType          string
	baseURL           string
	uploadID          string
	partSizeBytes     int64
	writtenTotalBytes int64
	partNum           int64

	uploadErr              error
	maxRetries             int64
	retryBackoffInitial    time.Duration
	retryBackoffMax        time.Duration
	retryBackoffMultiplier float64

	parts         map[int64]objectPart
	workers       []*uploadWorker
	currentWorker *uploadWorker
	idleWorkers   chan *uploadWorker
}

// uploadWorker will buffer and retry uploading a single part.
type uploadWorker struct {
	w          *MultipartWriter
	httpClient httpClient
	buffer     []byte
	offset     int64
	numRetries int64
}

// NewMultipartWriter creates a writer and workers for a multipart upload.
func (rw *ReadWriter) NewMultipartWriter(ctx context.Context, newClient HTTPClient, tokenGetter DefaultTokenGetter, jsonCredentialsGetter JSONCredentialsGetter) (*MultipartWriter, error) {
	if rw.XMLMultipartEndpoint == "" {
		rw.XMLMultipartEndpoint = defaultClientEndpoint
	}
	baseURL := fmt.Sprintf("https://%s.%s/%s", rw.BucketName, rw.XMLMultipartEndpoint, rw.ObjectName)
	token, err := token(ctx, rw.XMLMultipartServiceAccount, tokenGetter, jsonCredentialsGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch auth token, err: %w", err)
	}

	w := &MultipartWriter{
		bucket:                 rw.BucketHandle,
		objectName:             rw.ObjectName,
		fileType:               rw.Metadata["X-Backup-Type"],
		token:                  token,
		httpClient:             newClient(10*time.Minute, defaultTransport()),
		baseURL:                baseURL,
		partSizeBytes:          rw.ChunkSizeMb * 1024 * 1024,
		partNum:                1,
		maxRetries:             rw.MaxRetries,
		retryBackoffInitial:    rw.RetryBackoffInitial,
		retryBackoffMax:        rw.RetryBackoffMax,
		retryBackoffMultiplier: rw.RetryBackoffMultiplier,
		parts:                  make(map[int64]objectPart),
		workers:                make([]*uploadWorker, rw.XMLMultipartWorkers),
		idleWorkers:            make(chan *uploadWorker, rw.XMLMultipartWorkers),
	}
	if w.uploadID, err = w.initMultipartUpload(); err != nil {
		return nil, fmt.Errorf("failed to init multipart upload, err: %w", err)
	}

	// Each worker needs a dedicated transport to prevent throttling.
	for i := 0; i < int(rw.XMLMultipartWorkers); i++ {
		w.workers[i] = &uploadWorker{
			w:          w,
			httpClient: newClient(10*time.Minute, defaultTransport()),
			buffer:     make([]byte, w.partSizeBytes),
		}
		w.idleWorkers <- w.workers[i]
	}
	return w, nil
}

// Write buffers a full part then asynchronously sends the data.
func (w *MultipartWriter) Write(p []byte) (int, error) {
	// TODO: Fill in this function
	return len(p), nil
}

// Close waits for all transfers to complete then generates the final object.
func (w *MultipartWriter) Close() error {
	// TODO: Fill in this function
	return nil
}

// initMultipartUpload starts the multipart upload and returns the upload ID.
func (w *MultipartWriter) initMultipartUpload() (string, error) {
	initialPostURL := fmt.Sprintf("%s?uploads", w.baseURL)
	req, err := http.NewRequest("POST", initialPostURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request, err: %w", err)
	}
	req.Header.Add("Content-Length", "0")
	req.Header.Add("Date", time.Now().Format(http.TimeFormat))
	req.Header.Add("Content-Type", "application/octet-stream")
	w.token.SetAuthHeader(req)

	resp, err := w.httpClient.Do(req)
	defer googleapi.CloseBody(resp)
	if err != nil {
		return "", fmt.Errorf("failed to init multipart upload, err: %w", err)
	}
	if err := checkResponse(resp); err != nil {
		return "", fmt.Errorf("multipart upload failed, err: %w", err)
	}
	parsedResult := &struct {
		UploadID string `xml:"UploadId"`
	}{}
	if err := xml.NewDecoder(resp.Body).Decode(parsedResult); err != nil {
		return "", fmt.Errorf("failed to decode multipart upload result, err: %w", err)
	}
	return parsedResult.UploadID, nil
}

// checkResponse verifies the response of http commands, returning any errors.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	errStr := http.StatusText(resp.StatusCode)
	debugID := resp.Header.Get("x-guploader-uploadid")
	if resp.Body != nil {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("%w (failed to read response body); %s; x-guploader-uploadid=%s", readErr, errStr, debugID)
		}
		if bodyStr := string(body); bodyStr != "" {
			errStr = bodyStr
		}
	}
	return fmt.Errorf("%s; x-guploader-uploadid=%s", errStr, debugID)
}

// token fetches a token with default or service account credentials.
func token(ctx context.Context, serviceAccount string, tokenGetter DefaultTokenGetter, jsonCredentialsGetter JSONCredentialsGetter) (*oauth2.Token, error) {
	if serviceAccount != "" {
		serviceAccountBytes, err := os.ReadFile(serviceAccount)
		if err != nil {
			return nil, fmt.Errorf("failed to read service account file, err: %w", err)
		}
		cred, err := jsonCredentialsGetter(ctx, serviceAccountBytes, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, err
		}
		return cred.TokenSource.Token()
	}

	tokenSource, err := tokenGetter(ctx)
	if err != nil {
		return nil, err
	}
	return tokenSource.Token()
}
