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
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

// DefaultLogDelay sets the default upload and download progress logging to once a minute.
const DefaultLogDelay = time.Minute

// DefaultChunkSizeMb provides a default chunk size when uploading data.
const DefaultChunkSizeMb = 16

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

	// If TotalBytes is not set, percent completion cannot be calculated when logging progress.
	TotalBytes int64

	// If LogDelay is not set, it will be defaulted to DefaultLogDelay.
	LogDelay time.Duration

	bytesWritten     int64
	lastBytesWritten int64
	lastLog          time.Time
	firstLog         time.Time
}

// ConnectToBucket creates the storage client with custom retry logic and
// attempts to connect to the GCS bucket. Returns false if there is a connection
// failure (bucket does not exist, invalid credentials, etc.)
func ConnectToBucket(ctx context.Context, storageClient Client, serviceAccount, bucketName string, parallelStreams int64) (*storage.BucketHandle, bool) {
	var opts []option.ClientOption
	if serviceAccount != "" {
		opts = append(opts, option.WithCredentialsFile(serviceAccount))
	}
	client, err := storageClient(ctx, opts...)
	if err != nil {
		log.Logger.Errorw("Failed to create GCS client. Ensure your default credentials or service account file are correct.", "error", err)
		return nil, false
	}
	// TODO: Add custom retry logic

	bucket := client.Bucket(bucketName)
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to connect to bucket. Ensure the bucket exists and you have permission to access it.", "bucket", bucketName, "error", err)
		return nil, false
	}
	log.Logger.Debugw("The bucket exists and has attributes: %#v\n", attrs)

	if attrs.RetentionPolicy != nil && parallelStreams > 1 {
		log.Logger.Errorw("Parallel streams are not supported on buckets with retention policies - 'parallel_streams' must be set to 1 in order to connect to this bucket", "bucket", bucketName, "parallelStreams", parallelStreams, "retentionPolicy", attrs.RetentionPolicy)
		return nil, false
	}

	return bucket, true
}

// Upload writes the data contained in src to the ObjectName in the bucket.
// Returns bytesWritten and any error due to copy operations, writing, or closing the writer.
func (rw *ReadWriter) Upload(ctx context.Context) (int64, error) {
	if rw.BucketHandle == nil {
		return 0, errors.New("no bucket defined")
	}

	writer := rw.BucketHandle.Object(rw.ObjectName).NewWriter(ctx)
	writer.ChunkSize = int(rw.ChunkSizeMb) * 1024 * 1024
	rw.lastLog = time.Now()
	rw.firstLog = time.Now()
	rw.bytesWritten = 0
	rw.lastBytesWritten = 0

	if rw.LogDelay <= 0 {
		log.Logger.Warnf("LogDelay defaulted to %.f seconds", DefaultLogDelay.Seconds())
		rw.LogDelay = DefaultLogDelay
	}
	if rw.ChunkSizeMb == 0 {
		log.Logger.Warn("ChunkSizeMb set to 0, uploads cannot be retried.")
	}

	log.Logger.Infow("Upload starting", "bucket", rw.BucketName, "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	bytesWritten, err := rw.Copier(writer, rw)
	if err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	avgTransferSpeedMBps := float64(bytesWritten) / time.Since(rw.firstLog).Seconds() / 1024 / 1024
	log.Logger.Infow("Upload success", "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", bytesWritten, "totalBytes", rw.TotalBytes, "percentComplete", 100, "avgTransferSpeedMBps", math.Round(avgTransferSpeedMBps))
	return bytesWritten, nil
}

// Download reads the data contained in ObjectName in the bucket and outputs to dest.
// Returns bytesWritten and any error due to creating the reader or reading the data.
func (rw *ReadWriter) Download(ctx context.Context) (int64, error) {
	if rw.BucketHandle == nil {
		return 0, errors.New("no bucket defined")
	}

	reader, err := rw.BucketHandle.Object(rw.ObjectName).NewReader(ctx)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	rw.lastLog = time.Now()
	rw.firstLog = time.Now()
	rw.bytesWritten = 0
	rw.lastBytesWritten = 0

	if rw.LogDelay <= 0 {
		log.Logger.Warnf("LogDelay defaulted to %.f seconds", DefaultLogDelay.Seconds())
		rw.LogDelay = DefaultLogDelay
	}

	log.Logger.Infow("Download starting", "bucket", rw.BucketName, "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	bytesWritten, err := rw.Copier(rw, reader)
	if err != nil {
		return 0, err
	}
	avgTransferSpeedMBps := float64(bytesWritten) / time.Since(rw.firstLog).Seconds() / 1024 / 1024
	log.Logger.Infow("Download success", "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", bytesWritten, "totalBytes", rw.TotalBytes, "percentComplete", 100, "avgTransferSpeedMBps", math.Round(avgTransferSpeedMBps))
	return bytesWritten, nil
}

// ListObjects returns all objects in the bucket with the given prefix sorted by latest creation.
// The prefix can be empty and can contain multiple folders separated by forward slashes.
// Errors are returned if no handle is defined or if there are iteration issues.
func ListObjects(ctx context.Context, bucketHandle *storage.BucketHandle, prefix string) ([]*storage.ObjectAttrs, error) {
	if bucketHandle == nil {
		return nil, errors.New("no bucket defined")
	}

	var result []*storage.ObjectAttrs
	it := bucketHandle.Objects(ctx, &storage.Query{Prefix: prefix})
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
func DeleteObject(ctx context.Context, bucketHandle *storage.BucketHandle, objectName string) error {
	if bucketHandle == nil {
		return errors.New("no bucket defined")
	}
	return bucketHandle.Object(objectName).Delete(ctx)
}

// Read wraps io.Reader to provide upload progress updates.
func (rw *ReadWriter) Read(p []byte) (n int, err error) {
	n, err = rw.Reader.Read(p)
	if err == nil {
		rw.logProgress("Upload progress", int64(n))
	}
	return n, err
}

// Write wraps io.Writer to provide download progress updates.
func (rw *ReadWriter) Write(p []byte) (n int, err error) {
	n, err = rw.Writer.Write(p)
	if err == nil {
		rw.logProgress("Download progress", int64(n))
	}
	return n, err
}

// logProgress logs download/upload status including percent completion and transfer speed in MBps.
func (rw *ReadWriter) logProgress(logMessage string, n int64) {
	rw.bytesWritten += int64(n)
	if time.Since(rw.lastLog) > rw.LogDelay {
		percentComplete := float64(rw.bytesWritten) / float64(rw.TotalBytes) * 100.0
		transferSpeedMBps := float64(rw.bytesWritten-rw.lastBytesWritten) / time.Since(rw.lastLog).Seconds() / 1024 / 1024
		log.Logger.Infow(logMessage, "bucket", rw.BucketName, "object", rw.ObjectName, "bytesWritten", rw.bytesWritten, "totalBytes", rw.TotalBytes, "percentComplete", math.Round(percentComplete), "transferSpeedMBps", math.Round(transferSpeedMBps))
		rw.lastLog = time.Now()
		rw.lastBytesWritten = rw.bytesWritten
	}
}
