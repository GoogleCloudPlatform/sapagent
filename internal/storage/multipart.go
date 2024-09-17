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
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	defaultClientEndpoint = "storage.googleapis.com"
	tokenScope            = "https://www.googleapis.com/auth/cloud-platform"
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

	objectName    string
	fileType      string
	baseURL       string
	storageClass  string
	uploadID      string
	partSizeBytes int64
	partNum       int64

	uploadErr              error
	maxRetries             int64
	retryBackoffInitial    time.Duration
	retryBackoffMax        time.Duration
	retryBackoffMultiplier float64

	mu            *sync.Mutex
	parts         map[int64]objectPart
	workers       []*uploadWorker
	currentWorker *uploadWorker
	idleWorkers   chan *uploadWorker
	customTime    time.Time
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
		storageClass:           rw.StorageClass,
		partSizeBytes:          rw.ChunkSizeMb * 1024 * 1024,
		partNum:                1,
		maxRetries:             rw.MaxRetries,
		retryBackoffInitial:    rw.RetryBackoffInitial,
		retryBackoffMax:        rw.RetryBackoffMax,
		retryBackoffMultiplier: rw.RetryBackoffMultiplier,
		mu:                     &sync.Mutex{},
		parts:                  make(map[int64]objectPart),
		workers:                make([]*uploadWorker, rw.XMLMultipartWorkers),
		idleWorkers:            make(chan *uploadWorker, rw.XMLMultipartWorkers),
		customTime:             rw.CustomTime,
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
	bytesWritten := 0
	for bytesWritten < len(p) {
		if w.currentWorker == nil {
			w.currentWorker = <-w.idleWorkers
		}
		if w.uploadErr != nil {
			w.abortMultipartUpload()
			return 0, w.uploadErr
		}
		n := copy(w.currentWorker.buffer[w.currentWorker.offset:], p[bytesWritten:])
		bytesWritten += n
		w.currentWorker.offset += int64(n)
		if w.currentWorker.offset >= w.partSizeBytes {
			go w.currentWorker.uploadPartAsync(w.partNum)
			w.partNum++
			w.currentWorker = nil
		}
	}
	return bytesWritten, nil
}

// Close waits for all transfers to complete then generates the final object.
func (w *MultipartWriter) Close() error {
	if w.currentWorker != nil {
		go w.currentWorker.uploadPartAsync(w.partNum)
	}
	for i := 0; i < len(w.workers); i++ {
		<-w.idleWorkers
	}
	if w.uploadErr != nil {
		w.abortMultipartUpload()
		return w.uploadErr
	}
	if err := w.completeMultipartUpload(); err != nil {
		w.abortMultipartUpload()
		return err
	}
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
	if w.storageClass != "" {
		req.Header.Add("x-goog-storage-class", w.storageClass)
	}
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

// abortMultipartUpload aborts the upload to free resources from the bucket.
func (w *MultipartWriter) abortMultipartUpload() error {
	log.Logger.Infow("Aborting multipart upload", "object", w.objectName, "uploadID", w.uploadID)
	abortURL := fmt.Sprintf("%s?uploadId=%s", w.baseURL, w.uploadID)
	req, err := http.NewRequest("DELETE", abortURL, nil)
	if err != nil {
		log.Logger.Errorw("Failed to create request for abort", "object", w.objectName, "uploadID", w.uploadID, "err", err)
		return err
	}
	req.Header.Add("Content-Length", "0")
	req.Header.Add("Date", time.Now().Format(http.TimeFormat))
	w.token.SetAuthHeader(req)
	resp, err := w.httpClient.Do(req)
	defer googleapi.CloseBody(resp)
	if err != nil || checkResponse(resp) != nil {
		log.Logger.Errorw("Failed to abort multipart upload.", "object", w.objectName, "uploadID", w.uploadID, "err", err, "resp", resp)
		return err
	}
	log.Logger.Infow("Successfully aborted multipart upload.", "object", w.objectName, "uploadID", w.uploadID)
	return nil
}

// completeMultipartUpload sends the final POST request to complete the
// multipart upload, then writes final metadata to the object in the bucket.
func (w *MultipartWriter) completeMultipartUpload() error {
	bodyXML, err := completeMultipartUploadXML(w.parts)
	if err != nil {
		return fmt.Errorf("failed to build complete multipart upload XML for %v, err: %w", w.objectName, err)
	}
	completeURL := fmt.Sprintf("%s?uploadId=%s", w.baseURL, w.uploadID)

	req, err := http.NewRequest("POST", completeURL, strings.NewReader(bodyXML))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Length", fmt.Sprintf("%v", len(bodyXML)))
	req.Header.Add("Date", time.Now().Format(http.TimeFormat))
	req.Header.Add("Content-Type", "application/xml")
	w.token.SetAuthHeader(req)
	resp, err := w.httpClient.Do(req)
	defer googleapi.CloseBody(resp)
	if err != nil {
		return err
	}
	if err := checkResponse(resp); err != nil {
		return err
	}

	// XML headers will force this key to be lowercase, set it after the upload.
	update := storage.ObjectAttrsToUpdate{
		Metadata:   map[string]string{"X-Backup-Type": w.fileType},
		CustomTime: w.customTime,
	}
	if _, err := w.bucket.Object(w.objectName).Update(req.Context(), update); err != nil {
		return err
	}
	return nil
}

// completeMultipartUploadXML creates the XML for the final POST request.
// All parts and their ETag data must be included.
func completeMultipartUploadXML(parts map[int64]objectPart) (string, error) {
	upload := struct {
		XMLName xml.Name     `xml:"CompleteMultipartUpload"`
		Parts   []objectPart `xml:"Part"`
	}{}
	// Order parts in the XML request.
	upload.Parts = make([]objectPart, 0, len(parts))
	for partNum := int64(1); partNum <= int64(len(parts)); partNum++ {
		part, ok := parts[partNum]
		if !ok {
			return "", fmt.Errorf("part %v not contained in parts", partNum)
		}
		upload.Parts = append(upload.Parts, part)
	}

	xmlStr := strings.Builder{}
	encoder := xml.NewEncoder(&xmlStr)
	encoder.Indent("", "  ")
	if err := encoder.Encode(upload); err != nil {
		return "", err
	}
	return xmlStr.String(), nil
}

// checkResponse verifies the response of http commands, returning any errors.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
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

// token fetches a token with default, service account or workload identity
// federation credentials.
func token(ctx context.Context, serviceAccount string, tokenGetter DefaultTokenGetter, jsonCredentialsGetter JSONCredentialsGetter) (*oauth2.Token, error) {
	if serviceAccount != "" {
		serviceAccountBytes, err := os.ReadFile(serviceAccount)
		if err != nil {
			return nil, fmt.Errorf("failed to read service account file, err: %w", err)
		}
		cred, err := jsonCredentialsGetter(ctx, serviceAccountBytes, tokenScope)
		if err != nil {
			return nil, err
		}
		return cred.TokenSource.Token()
	}

	// TODO: Use token functions from go oauth2 libraries.
	tokenSource, err := tokenGetter(ctx, tokenScope)
	if err != nil {
		return nil, err
	}
	return tokenSource.Token()
}

// uploadPartAsync continues upload attempts until a success,
// or the upload fails too many times.
func (uw *uploadWorker) uploadPartAsync(partNum int64) {
	backoff := backoff(uw.w.retryBackoffInitial, uw.w.retryBackoffMax, uw.w.retryBackoffMultiplier)
	for {
		if err := uw.uploadPart(partNum); err != nil {
			uw.numRetries++
			if uw.numRetries > uw.w.maxRetries {
				log.Logger.Errorw("Max retries exceeded, cancelling operation.", "partNum", partNum, "numRetries", uw.numRetries, "maxRetries", uw.w.maxRetries, "objectName", uw.w.objectName, "error", err)
				uw.w.mu.Lock()
				uw.w.uploadErr = fmt.Errorf("failed to upload part %v too many times, err: %w", partNum, err)
				uw.w.mu.Unlock()
				uw.w.idleWorkers <- uw
				return
			}
			log.Logger.Infow("Failed to upload data to Google Cloud Storage, retrying.", "partNum", partNum, "numRetries", uw.numRetries, "maxRetries", uw.w.maxRetries, "objectName", uw.w.objectName, "error", err)
			time.Sleep(backoff.Pause())
			continue
		}
		uw.offset = 0
		uw.numRetries = 0
		uw.w.idleWorkers <- uw
		return
	}
}

// uploadPart uploads a part to the ongoing multipart upload.
func (uw *uploadWorker) uploadPart(partNum int64) error {
	data := uw.buffer[:uw.offset]
	url := fmt.Sprintf("%s?uploadId=%s&partNumber=%v", uw.w.baseURL, uw.w.uploadID, partNum)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Length", fmt.Sprintf("%d", len(data)))
	req.Header.Add("Date", time.Now().Format(http.TimeFormat))
	uw.w.token.SetAuthHeader(req)
	resp, err := uw.httpClient.Do(req)
	defer googleapi.CloseBody(resp)
	if err != nil {
		return err
	}
	if err := checkResponse(resp); err != nil {
		return err
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		respStr := &strings.Builder{}
		resp.Write(respStr)
		return fmt.Errorf("uploadPart did not return in the expected format. Unable to get the ETag for part %v. Resp: %v", partNum, respStr.String())
	}
	uw.w.mu.Lock()
	defer uw.w.mu.Unlock()
	uw.w.parts[partNum] = objectPart{PartNumber: partNum, ETag: etag}
	return nil
}
