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

// Package storage uploads and downloads data from a GCS bucket.
package storage

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"golang.org/x/oauth2/google"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// DefaultLogDelay sets the default upload and download progress logging to once a minute.
const DefaultLogDelay = time.Minute

// DefaultChunkSizeMb provides a default chunk size when uploading data.
const DefaultChunkSizeMb = 16

const compressedContentType = "application/gzip"

// IOFileCopier abstracts copying data from an io.Reader to an io.Writer.
type IOFileCopier func(dst io.Writer, src io.Reader) (written int64, err error)

// Client abstracts creating a new storage client to connect to GCS.
type Client func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error)

// BucketConnector abstracts ConnectToBucket() for unit testing purposes.
type BucketConnector func(ctx context.Context, p *ConnectParameters) (*storage.BucketHandle, bool)

// ReadWriter wraps io.Reader and io.Writer to provide
// progress updates when uploading or downloading files.
type ReadWriter struct {
	// Reader must be set in order to upload data.
	Reader io.Reader

	// Writer must be set in order to download data.
	Writer io.Writer

	// Abstracted for testing purposes, production users should use io.Copy().
	Copier IOFileCopier

	// ChunkSizeMb sets the max bytes that are written at a time to the bucket during uploads.
	// Objects smaller than the size will be sent in a single request, while larger
	// objects will be split over multiple requests.
	//
	// If ChunkSizeMb is set to zero, chunking will be disabled and the object will
	// be uploaded in a single request without the use of a buffer. This will
	// further reduce memory used during uploads, but will also prevent the writer
	// from retrying in case of a transient error from the server or resuming an
	// upload that fails midway through, since the buffer is required in order to
	// retry the failed request.
	ChunkSizeMb int64

	// Call ConnectToBucket() to generate a bucket handle.
	BucketHandle *storage.BucketHandle

	// The name of the bucket must match the handle.
	BucketName string

	// ObjectName is the destination or source in the bucket for uploading or downloading data.
	ObjectName string

	// Metadata is an optional parameter to add metadata to uploads.
	Metadata map[string]string

	// CustomTime is an optional parameter to set the custom time for uploads.
	CustomTime time.Time

	// If TotalBytes is not set, percent completion cannot be calculated when logging progress.
	TotalBytes int64

	// If LogDelay is not set, it will be defaulted to DefaultLogDelay.
	LogDelay time.Duration

	// Compress enables client side compression with gzip for uploads.
	// Downloads will decompress automatically based on the file's content type.
	Compress bool

	// EncryptionKey enables customer-supplied server side encryption with a
	// base64 encoded AES-256 key string.
	// Providing both EncryptionKey and KMSKey will result in an error.
	EncryptionKey string

	// KMSKey enables customer-managed server side encryption with a cloud
	// key management service encryption key.
	// Providing both EncryptionKey and KMSKey will result in an error.
	KMSKey string

	// VerifyUpload ensures the object is in the bucket and bytesWritten matches
	// the object's size in the bucket. Read access on the bucket is required.
	VerifyUpload bool

	// StorageClass sets the storage class for uploads, default is "STANDARD".
	StorageClass string

	// DumpData discards bytes during upload rather than write to the bucket.
	DumpData bool

	// RateLimitBytes caps the maximum number of bytes transferred per second.
	// A default value of 0 prevents rate limiting.
	RateLimitBytes int64

	// MaxRetries sets the maximum amount of retries when executing an API call.
	// An exponential backoff delays each subsequent retry.
	MaxRetries int64

	// RetryBackoffInitial is an optional parameter to set the initial backoff
	// interval. The default value is 10 seconds.
	RetryBackoffInitial time.Duration

	// RetryBackoffMax is an optional parameter to set the maximum backoff
	// interval. The default value is 300 seconds.
	RetryBackoffMax time.Duration

	// RetryBackoffMultiplier is an optional parameter to set the multiplier for
	// the backoff interval. Must be greater than 1 and the default value is 2.
	RetryBackoffMultiplier float64

	// XMLMultipartUpload is an optional parameter to configure the upload to use
	// the XML multipart API rather than the GCS storage client API.
	XMLMultipartUpload bool

	// XMLMultipartWorkers defines the number of workers, or parts, used in the
	// XML multipart upload. XMLMultipartUpload must be set to true to use.
	XMLMultipartWorkers int64

	// XMLMultipartServiceAccount will override application default credentials
	// with a JSON credentials file for authenticating API requests.
	XMLMultipartServiceAccount string

	// XMLMultipartEndpoint will override the default client endpoint used for
	// making requests.
	XMLMultipartEndpoint string

	// ParallelDownloadWorkers defines the number of workers, or parts, used in the
	// parallel reader download. If 0, the download will happen sequentially.
	ParallelDownloadWorkers int64

	// ParallelDownloadConnectParams provides parameters for bucket connection for
	// downloading in parallel restore.
	ParallelDownloadConnectParams *ConnectParameters

	numRetries                int64
	bytesTransferred          int64
	lastBytesTransferred      int64
	rateLimitBytesTransferred int64
	lastRateLimit             time.Duration
	lastLog                   time.Time
	lastTransferTime          time.Duration
	totalTransferTime         time.Duration
}

// discardCloser provides no-ops for a io.WriteCloser interface.
type discardCloser struct{}

func (discardCloser) Close() error                { return nil }
func (discardCloser) Write(p []byte) (int, error) { return len(p), nil }

// ConnectParameters provides parameters for bucket connection.
type ConnectParameters struct {
	StorageClient    Client
	ServiceAccount   string
	BucketName       string
	UserAgentSuffix  string
	Endpoint         string
	VerifyConnection bool
	MaxRetries       int64
}

// ConnectToBucket creates the storage client with custom retry logic and
// attempts to connect to the GCS bucket. Returns false if there is a connection
// failure (bucket does not exist, invalid credentials, etc.)
// userAgentSuffix is an optional parameter to set the User-Agent header.
// verifyConnection requires bucket read access to list the bucket's objects.
func ConnectToBucket(ctx context.Context, p *ConnectParameters) (*storage.BucketHandle, bool) {
	var opts []option.ClientOption
	userAgent := fmt.Sprintf("google-cloud-sap-agent/%s (GPN: Agent for SAP)", configuration.AgentVersion)
	if p.UserAgentSuffix != "" {
		userAgent = fmt.Sprintf("%s %s)", strings.TrimSuffix(userAgent, ")"), p.UserAgentSuffix)
	}
	log.CtxLogger(ctx).Debugw("Setting User-Agent header", "userAgent", userAgent)
	opts = append(opts, option.WithUserAgent(userAgent))
	if p.ServiceAccount != "" {
		opts = append(opts, option.WithCredentialsFile(p.ServiceAccount))
	}
	if p.Endpoint != "" {
		opts = append(opts, option.WithEndpoint(p.Endpoint))
	}
	client, err := p.StorageClient(ctx, opts...)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create GCS client. Ensure your default credentials or service account file are correct.", "error", err)
		return nil, false
	}

	bucket := client.Bucket(p.BucketName)

	if !p.VerifyConnection {
		log.CtxLogger(ctx).Infow("Created bucket but did not verify connection. Read/write calls may fail.", "bucket", p.BucketName)
		return bucket, true
	}
	log.CtxLogger(ctx).Debugw("Verifying connection to bucket", "bucket", p.BucketName)
	rw := &ReadWriter{MaxRetries: p.MaxRetries}
	it := bucket.Retryer(rw.retryOptions("Failed to verify bucket connection, retrying.")...).Objects(ctx, nil)
	if _, err := it.Next(); err != nil && err != iterator.Done {
		log.CtxLogger(ctx).Errorw("Failed to connect to bucket. Ensure the bucket exists and you have permission to access it.", "bucket", p.BucketName, "error", err)
		return nil, false
	}
	log.CtxLogger(ctx).Infow("Connected to bucket", "bucket", p.BucketName)
	return bucket, true
}

// Upload writes the data contained in src to the ObjectName in the bucket.
// Returns bytesWritten and any error due to copy operations, writing, or closing the writer.
func (rw *ReadWriter) Upload(ctx context.Context) (int64, error) {
	if rw.BucketHandle == nil {
		return 0, errors.New("no bucket defined")
	}

	object := rw.BucketHandle.Object(rw.ObjectName).Retryer(rw.retryOptions("Failed to upload data to Google Cloud Storage, retrying.")...)
	var writer io.WriteCloser
	var err error
	if rw.EncryptionKey != "" || rw.KMSKey != "" {
		log.CtxLogger(ctx).Infow("Encryption enabled for upload", "bucket", rw.BucketName, "object", rw.ObjectName)
	}
	if rw.DumpData {
		log.CtxLogger(ctx).Warnw("dump_data set to true, discarding data during upload", "bucket", rw.BucketName, "object", rw.ObjectName)
		writer = discardCloser{}
	} else if rw.XMLMultipartUpload {
		log.CtxLogger(ctx).Infow("XML Multipart API enabled for upload", "bucket", rw.BucketName, "object", rw.ObjectName, "workers", rw.XMLMultipartWorkers)
		writer, err = rw.NewMultipartWriter(ctx, defaultNewClient, google.DefaultTokenSource, google.CredentialsFromJSON)
		if err != nil {
			return 0, err
		}
	} else {
		if rw.EncryptionKey != "" {
			decodedKey, err := base64.StdEncoding.DecodeString(rw.EncryptionKey)
			if err != nil {
				return 0, err
			}
			object = object.Key(decodedKey)
		}
		objectWriter := object.NewWriter(ctx)
		objectWriter.KMSKeyName = rw.KMSKey
		objectWriter.ChunkSize = int(rw.ChunkSizeMb) * 1024 * 1024
		objectWriter.Metadata = rw.Metadata
		if !rw.CustomTime.IsZero() {
			objectWriter.CustomTime = rw.CustomTime
		}
		if rw.Compress {
			objectWriter.ObjectAttrs.ContentType = compressedContentType
		}
		if rw.StorageClass != "" {
			objectWriter.ObjectAttrs.StorageClass = rw.StorageClass
		}
		// Set an individual chunk retry deadline to 10 minutes.
		// Note, even with custom retries declared this value must also be set.
		objectWriter.ChunkRetryDeadline = 10 * time.Minute
		writer = objectWriter
	}
	rw.Writer = writer

	rw = rw.defaultArgs()
	if rw.ChunkSizeMb == 0 {
		log.CtxLogger(ctx).Warn("ChunkSizeMb set to 0, uploads cannot be retried.")
	}

	log.CtxLogger(ctx).Infow("Upload starting", "bucket", rw.BucketName, "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	var bytesWritten int64
	if rw.Compress {
		log.CtxLogger(ctx).Infow("Compression enabled for upload", "bucket", rw.BucketName, "object", rw.ObjectName)
		gzipWriter := gzip.NewWriter(rw)
		if bytesWritten, err = rw.Copier(gzipWriter, rw.Reader); err != nil {
			return 0, err
		}
		// Closing the gzip writer flushes data to the underlying writer and writes the gzip footer.
		// The underlying writer must be closed after this call to flush its buffer to the bucket.
		if err := gzipWriter.Close(); err != nil {
			return 0, err
		}
	} else {
		if bytesWritten, err = rw.Copier(rw, rw.Reader); err != nil {
			return 0, err
		}
	}

	closeStart := time.Now()
	if err := writer.Close(); err != nil {
		return 0, err
	}
	rw.totalTransferTime += time.Since(closeStart)

	// Verify object is in the bucket and bytesWritten matches the object's size in the bucket.
	objectSize := int64(0)
	if !rw.DumpData && rw.VerifyUpload {
		attrs, err := rw.BucketHandle.Object(rw.ObjectName).Attrs(ctx)
		if err != nil {
			return bytesWritten, err
		}
		objectSize = attrs.Size
		if bytesWritten != objectSize && !rw.Compress {
			return bytesWritten, fmt.Errorf("upload error for object: %v, bytesWritten: %d does not equal the object's size: %d", rw.ObjectName, bytesWritten, objectSize)
		}
	}
	avgTransferSpeedMBps := float64(rw.bytesTransferred) / rw.totalTransferTime.Seconds() / 1024 / 1024
	log.CtxLogger(ctx).Infow("Upload success", "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", bytesWritten, "bytesTransferred", rw.bytesTransferred, "totalBytes", rw.TotalBytes, "objectSizeInBucket", objectSize, "percentComplete", 100, "avgTransferSpeedMBps", fmt.Sprintf("%g", math.Round(avgTransferSpeedMBps)))
	return bytesWritten, nil
}

// Download reads the data contained in ObjectName in the bucket and outputs to dest.
// Returns bytesWritten and any error due to creating the reader or reading the data.
func (rw *ReadWriter) Download(ctx context.Context) (int64, error) {
	if rw.BucketHandle == nil {
		return 0, errors.New("no bucket defined")
	}

	if rw.EncryptionKey != "" || rw.KMSKey != "" {
		log.CtxLogger(ctx).Infow("Decryption enabled for download", "bucket", rw.BucketName, "object", rw.ObjectName)
	}
	object := rw.BucketHandle.Object(rw.ObjectName)
	var decodedKey []byte
	if rw.EncryptionKey != "" {
		var err error
		decodedKey, err = base64.StdEncoding.DecodeString(rw.EncryptionKey)
		if err != nil {
			return 0, err
		}
		object = object.Key(decodedKey)
	}

	var reader io.ReadCloser
	var err error
	if rw.ParallelDownloadWorkers > 1 {
		reader, err = rw.NewParallelReader(ctx, decodedKey)
	} else {
		object = object.Retryer(rw.retryOptions("Failed to download data from Google Cloud Storage, retrying.")...)
		reader, err = object.NewReader(ctx)
	}
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	rw.Reader = reader

	rw = rw.defaultArgs()
	log.CtxLogger(ctx).Infow("Download starting", "bucket", rw.BucketName, "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	var bytesWritten int64
	attrs, err := object.Attrs(ctx)
	if err != nil {
		return 0, err
	}
	if attrs.ContentType == compressedContentType {
		log.CtxLogger(ctx).Infow("Compressed file detected, decompressing during download", "bucket", rw.BucketName, "object", rw.ObjectName)
		gzipReader, err := gzip.NewReader(rw)
		if err != nil {
			return 0, err
		}
		if bytesWritten, err = rw.Copier(rw.Writer, gzipReader); err != nil {
			return 0, err
		}
		if err := gzipReader.Close(); err != nil {
			return 0, err
		}
	} else {
		if bytesWritten, err = rw.Copier(rw.Writer, rw); err != nil {
			return 0, err
		}
	}

	avgTransferSpeedMBps := float64(rw.bytesTransferred) / rw.totalTransferTime.Seconds() / 1024 / 1024
	log.CtxLogger(ctx).Infow("Download success", "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", bytesWritten, "bytesTransferred", rw.bytesTransferred, "totalBytes", rw.TotalBytes, "percentComplete", 100, "avgTransferSpeedMBps", fmt.Sprintf("%g", math.Round(avgTransferSpeedMBps)))
	return bytesWritten, nil
}

// ListObjects returns all objects in the bucket with the given prefix sorted by latest creation.
// The prefix can be empty and can contain multiple folders separated by forward slashes.
// The optional filter will only return filenames that contain the filter.
// Errors are returned if no handle is defined or if there are iteration issues.
func ListObjects(ctx context.Context, bucketHandle *storage.BucketHandle, prefix, filter string, maxRetries int64) ([]*storage.ObjectAttrs, error) {
	if bucketHandle == nil {
		return nil, errors.New("no bucket defined")
	}

	rw := &ReadWriter{MaxRetries: maxRetries}
	it := bucketHandle.Retryer(rw.retryOptions("Failed to list objects, retrying.")...).Objects(ctx, &storage.Query{Prefix: prefix})
	var result []*storage.ObjectAttrs
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error while iterating objects for prefix: %s, err: %v", prefix, err)
		}
		if !strings.Contains(attrs.Name, filter) {
			log.CtxLogger(ctx).Debugw("Discarding object due to filter", "fileName", attrs.Name, "filter", filter)
			continue
		}
		result = append(result, attrs)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}

// DeleteObject deletes the specified object from the bucket.
func DeleteObject(ctx context.Context, bucketHandle *storage.BucketHandle, objectName string, maxRetries int64) error {
	if bucketHandle == nil {
		return errors.New("no bucket defined")
	}

	rw := &ReadWriter{MaxRetries: maxRetries, ObjectName: objectName}
	object := bucketHandle.Object(objectName).Retryer(rw.retryOptions("Failed to delete object, retrying.")...)
	if err := object.Delete(ctx); err != nil {
		log.CtxLogger(ctx).Errorw("Failed to delete object.", "objectName", objectName, "error", err)
		return err
	}
	return nil
}

// Read wraps io.Reader to provide download progress updates.
func (rw *ReadWriter) Read(p []byte) (n int, err error) {
	start := time.Now()
	n, err = rw.Reader.Read(p)
	if err == nil {
		rw.logProgress("Download progress", int64(n))
		rw.rateLimit(int64(n), time.Since(start))
	}
	readTime := time.Since(start)
	rw.lastTransferTime += readTime
	rw.totalTransferTime += readTime
	return n, err
}

// Write wraps io.Writer to provide upload progress updates.
func (rw *ReadWriter) Write(p []byte) (n int, err error) {
	start := time.Now()
	n, err = rw.Writer.Write(p)
	if err == nil {
		rw.logProgress("Upload progress", int64(n))
		rw.rateLimit(int64(n), time.Since(start))
	}
	writeTime := time.Since(start)
	rw.lastTransferTime += writeTime
	rw.totalTransferTime += writeTime
	return n, err
}

// logProgress logs download/upload status including percent completion and transfer speed in MBps.
func (rw *ReadWriter) logProgress(logMessage string, n int64) {
	rw.bytesTransferred += int64(n)
	if time.Since(rw.lastLog) > rw.LogDelay {
		percentComplete := float64(0)
		if rw.TotalBytes > 0 {
			percentComplete = float64(rw.bytesTransferred) / float64(rw.TotalBytes) * 100.0
		}
		lastTransferSpeedMBps := float64(rw.bytesTransferred-rw.lastBytesTransferred) / rw.lastTransferTime.Seconds() / 1024 / 1024
		totalTransferSpeedMBps := float64(rw.bytesTransferred) / rw.totalTransferTime.Seconds() / 1024 / 1024
		log.Logger.Infow(logMessage, "bucket", rw.BucketName, "object", rw.ObjectName, "bytesTransferred", rw.bytesTransferred, "totalBytes", rw.TotalBytes, "percentComplete", fmt.Sprintf("%g", math.Round(percentComplete)), "lastTransferSpeedMBps", fmt.Sprintf("%g", math.Round(lastTransferSpeedMBps)), "totalTransferSpeedMBps", fmt.Sprintf("%g", math.Round(totalTransferSpeedMBps)))
		rw.lastLog = time.Now()
		rw.lastBytesTransferred = rw.bytesTransferred
		// Prevent potential divide by zero by defaulting to 1 nanosecond.
		rw.lastTransferTime = time.Nanosecond
	}
}

// rateLimit limits the bytes transferred by introducing a sleep up to 1 second
// if the threshold is reached. RateLimitBytes set to 0 prevents rate limiting.
func (rw *ReadWriter) rateLimit(bytes int64, transferTime time.Duration) {
	if rw.RateLimitBytes == 0 {
		return
	}

	rw.rateLimitBytesTransferred += bytes
	rw.lastRateLimit += transferTime
	if rw.rateLimitBytesTransferred >= rw.RateLimitBytes {
		time.Sleep(time.Second - rw.lastRateLimit)
		// Prevent potential divide by zero by defaulting to 1 nanosecond.
		rw.lastRateLimit = time.Nanosecond
		rw.rateLimitBytesTransferred = 0
	}
}

// defaultArgs prepares ReadWriter for a new upload/download.
func (rw *ReadWriter) defaultArgs() *ReadWriter {
	rw.numRetries = 0
	rw.bytesTransferred = 0
	rw.lastBytesTransferred = 0
	rw.rateLimitBytesTransferred = 0
	rw.lastLog = time.Now()
	// Prevent potential divide by zero by defaulting to 1 nanosecond.
	rw.lastTransferTime = time.Nanosecond
	rw.totalTransferTime = time.Nanosecond
	rw.lastRateLimit = time.Nanosecond

	if rw.LogDelay <= 0 {
		log.Logger.Debugf("LogDelay defaulted to %.f seconds", DefaultLogDelay.Seconds())
		rw.LogDelay = DefaultLogDelay
	}
	return rw
}

// retryOptions uses an exponential backoff to retry all errors except 404.
func (rw *ReadWriter) retryOptions(failureMessage string) []storage.RetryOption {
	backoff := backoff(rw.RetryBackoffInitial, rw.RetryBackoffMax, rw.RetryBackoffMultiplier)
	log.Logger.Debugw("Using exponential backoff strategy for retries", "objectName", rw.ObjectName, "backoffInitial", backoff.Initial, "backoffMax", backoff.Max, "backoffMultiplier", backoff.Multiplier, "maxRetries", rw.MaxRetries)

	return []storage.RetryOption{storage.WithErrorFunc(func(err error) bool {
		var e *googleapi.Error
		if err == nil || errors.As(err, &e) && e.Code == http.StatusNotFound {
			rw.numRetries = 0
			return false
		}

		rw.numRetries++
		if rw.numRetries > rw.MaxRetries {
			log.Logger.Errorw("Max retries exceeded, cancelling operation.", "numRetries", rw.numRetries, "maxRetries", rw.MaxRetries, "objectName", rw.ObjectName, "error", err)
			return false
		}
		log.Logger.Infow(failureMessage, "numRetries", rw.numRetries, "maxRetries", rw.MaxRetries, "objectName", rw.ObjectName, "error", err)
		return true
	}),
		storage.WithBackoff(backoff),
		storage.WithPolicy(storage.RetryAlways)}
}

// backoff returns a gax.Backoff object with the given overrides.
func backoff(retryBackoffInitial, retryBackoffMax time.Duration, retryBackoffMultiplier float64) gax.Backoff {
	backoff := gax.Backoff{
		Initial:    10 * time.Second,
		Max:        300 * time.Second,
		Multiplier: 2,
	}
	if retryBackoffInitial > 0 {
		backoff.Initial = retryBackoffInitial
	}
	if retryBackoffMax > 0 {
		backoff.Max = retryBackoffMax
	}
	if retryBackoffMultiplier > 1 {
		backoff.Multiplier = retryBackoffMultiplier
	}
	return backoff
}
