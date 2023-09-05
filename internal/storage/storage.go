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

	// DumpData discards bytes during upload rather than write to the bucket.
	DumpData bool

	// RateLimitBytes caps the maximum number of bytes transferred per second.
	// A default value of 0 prevents rate limiting.
	RateLimitBytes int64

	// MaxRetries sets the maximum amount of retries when executing an API call.
	// An exponential backoff delays each subsequent retry.
	MaxRetries int64

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

// ConnectToBucket creates the storage client with custom retry logic and
// attempts to connect to the GCS bucket. Returns false if there is a connection
// failure (bucket does not exist, invalid credentials, etc.)
// userAgentSuffix is an optional parameter to set the User-Agent header.
func ConnectToBucket(ctx context.Context, storageClient Client, serviceAccount, bucketName, userAgentSuffix string) (*storage.BucketHandle, bool) {
	var opts []option.ClientOption
	userAgent := fmt.Sprintf("google-cloud-sap-agent/%s (GPN: Agent for SAP)", configuration.AgentVersion)
	if userAgentSuffix != "" {
		userAgent = fmt.Sprintf("%s %s)", strings.TrimSuffix(userAgent, ")"), userAgentSuffix)
	}
	log.Logger.Infow("Setting User-Agent header", "userAgent", userAgent)
	opts = append(opts, option.WithUserAgent(userAgent))
	if serviceAccount != "" {
		opts = append(opts, option.WithCredentialsFile(serviceAccount))
	}
	client, err := storageClient(ctx, opts...)
	if err != nil {
		log.Logger.Errorw("Failed to create GCS client. Ensure your default credentials or service account file are correct.", "error", err)
		return nil, false
	}

	bucket := client.Bucket(bucketName)
	if _, err := bucket.Objects(ctx, nil).Next(); err != nil && err != iterator.Done {
		log.Logger.Errorw("Failed to connect to bucket. Ensure the bucket exists and you have permission to access it.", "bucket", bucketName, "error", err)
		return nil, false
	}
	log.Logger.Infow("Connected to bucket", "bucket", bucketName)
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
	if rw.EncryptionKey != "" || rw.KMSKey != "" {
		log.Logger.Infow("Encryption enabled for upload", "bucket", rw.BucketName, "object", rw.ObjectName)
	}
	if rw.DumpData {
		log.Logger.Warnw("dump_data set to true, discarding data during upload", "bucket", rw.BucketName, "object", rw.ObjectName)
		writer = discardCloser{}
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
		if rw.Compress {
			objectWriter.ObjectAttrs.ContentType = compressedContentType
		}
		writer = objectWriter
	}
	rw.Writer = writer

	rw = rw.defaultArgs()
	if rw.ChunkSizeMb == 0 {
		log.Logger.Warn("ChunkSizeMb set to 0, uploads cannot be retried.")
	}

	log.Logger.Infow("Upload starting", "bucket", rw.BucketName, "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	var bytesWritten int64
	var err error
	if rw.Compress {
		log.Logger.Infow("Compression enabled for upload", "bucket", rw.BucketName, "object", rw.ObjectName)
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

	if err := writer.Close(); err != nil {
		return 0, err
	}
	// Verify object is in the bucket and bytesWritten matches the object's size in the bucket.
	objectSize := int64(0)
	if !rw.DumpData {
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
	log.Logger.Infow("Upload success", "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", bytesWritten, "bytesTransferred", rw.bytesTransferred, "totalBytes", rw.TotalBytes, "objectSizeInBucket", objectSize, "percentComplete", 100, "avgTransferSpeedMBps", math.Round(avgTransferSpeedMBps))
	return bytesWritten, nil
}

// Download reads the data contained in ObjectName in the bucket and outputs to dest.
// Returns bytesWritten and any error due to creating the reader or reading the data.
func (rw *ReadWriter) Download(ctx context.Context) (int64, error) {
	if rw.BucketHandle == nil {
		return 0, errors.New("no bucket defined")
	}

	if rw.EncryptionKey != "" || rw.KMSKey != "" {
		log.Logger.Infow("Decryption enabled for download", "bucket", rw.BucketName, "object", rw.ObjectName)
	}
	object := rw.BucketHandle.Object(rw.ObjectName)
	if rw.EncryptionKey != "" {
		decodedKey, err := base64.StdEncoding.DecodeString(rw.EncryptionKey)
		if err != nil {
			return 0, err
		}
		object = object.Key(decodedKey)
	}
	object = object.Retryer(rw.retryOptions("Failed to download data from Google Cloud Storage, retrying.")...)
	reader, err := object.NewReader(ctx)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	rw.Reader = reader

	rw = rw.defaultArgs()
	log.Logger.Infow("Download starting", "bucket", rw.BucketName, "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	var bytesWritten int64
	if reader.Attrs.ContentType == compressedContentType {
		log.Logger.Infow("Compressed file detected, decompressing during download", "bucket", rw.BucketName, "object", rw.ObjectName)
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
	log.Logger.Infow("Download success", "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", bytesWritten, "bytesTransferred", rw.bytesTransferred, "totalBytes", rw.TotalBytes, "percentComplete", 100, "avgTransferSpeedMBps", math.Round(avgTransferSpeedMBps))
	return bytesWritten, nil
}

// ListObjects returns all objects in the bucket with the given prefix sorted by latest creation.
// The prefix can be empty and can contain multiple folders separated by forward slashes.
// Errors are returned if no handle is defined or if there are iteration issues.
func ListObjects(ctx context.Context, bucketHandle *storage.BucketHandle, prefix string, maxRetries int64) ([]*storage.ObjectAttrs, error) {
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
		log.Logger.Errorw("Failed to delete object.", "objectName", objectName, "error", err)
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
		log.Logger.Infow(logMessage, "bucket", rw.BucketName, "object", rw.ObjectName, "bytesTransferred", rw.bytesTransferred, "totalBytes", rw.TotalBytes, "percentComplete", math.Round(percentComplete), "lastTransferSpeedMBps", math.Round(lastTransferSpeedMBps), "totalTransferSpeedMBps", math.Round(totalTransferSpeedMBps))
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
		log.Logger.Warnf("LogDelay defaulted to %.f seconds", DefaultLogDelay.Seconds())
		rw.LogDelay = DefaultLogDelay
	}
	return rw
}

// retryOptions uses an exponential backoff to retry all errors except 404.
func (rw *ReadWriter) retryOptions(failureMessage string) []storage.RetryOption {
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
		storage.WithBackoff(gax.Backoff{
			Initial:    10 * time.Second,
			Max:        120 * time.Second,
			Multiplier: 2,
		}),
		storage.WithPolicy(storage.RetryAlways)}
}
