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
	"context"
	"errors"
	"io"
	"math"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

// DefaultLogDelay sets the default upload and download progress logging to once a minute.
const DefaultLogDelay = time.Minute

// IOFileCopier abstracts copying data from an io.Reader to an io.Writer.
type IOFileCopier func(dst io.Writer, src io.Reader) (written int64, err error)

// StorageClient abstracts creating a new storage client to connect to GCS.
type StorageClient func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error)

// StorageReadWriter wraps io.Reader and io.Writer to provide
// progress updates when uploading or downloading files.
type StorageReadWriter struct {
	reader           io.Reader
	writer           io.Writer
	bucketName       string
	objectName       string
	totalBytes       int64
	bytesWritten     int64
	lastBytesWritten int64
	lastLog          time.Time
	logDelay         time.Duration
}

// ConnectToBucket creates the storage client with custom retry logic and
// attempts to connect to the GCS bucket. Returns false if there is a connection
// failure (bucket does not exist, invalid credentials, etc.)
func (b *Backint) ConnectToBucket(ctx context.Context, storageClient StorageClient) (*storage.BucketHandle, bool) {
	var opts []option.ClientOption
	if b.config.GetServiceAccount() != "" {
		opts = append(opts, option.WithCredentialsFile(b.config.GetServiceAccount()))
	}
	client, err := storageClient(ctx, opts...)
	if err != nil {
		log.Logger.Errorw("Failed to create GCS client. Ensure your default credentials or service account file are correct.", "error", err)
		return nil, false
	}
	// TODO: Add custom retry logic

	bucket := client.Bucket(b.config.GetBucket())
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to connect to bucket. Ensure the bucket exists and you have permission to access it.", "bucket", b.config.GetBucket(), "error", err)
		return nil, false
	}
	log.Logger.Debugw("The bucket exists and has attributes: %#v\n", attrs)

	if attrs.RetentionPolicy != nil && b.config.GetParallelStreams() > 1 {
		log.Logger.Errorw("Parallel streams are not supported on buckets with retention policies - 'parallel_streams' must be set to 1 in order to connect to this bucket", "bucket", b.config.GetBucket(), "parallelStreams", b.config.GetParallelStreams(), "retentionPolicy", attrs.RetentionPolicy)
		return nil, false
	}

	return bucket, true
}

// Upload writes the data contained in src to the objectName in the bucket.
// Returns non-nil for failures due to copy operations, writing, or closing the writer.
func (b *Backint) Upload(ctx context.Context, objectName string, src io.Reader, totalBytes int64, logDelay time.Duration, copier IOFileCopier) error {
	if b.bucketHandle == nil {
		return errors.New("no bucket defined")
	}

	writer := b.bucketHandle.Object(objectName).NewWriter(ctx)
	writer.ChunkSize = int(b.config.GetBufferSizeMb()) * 1024 * 1024
	s := &StorageReadWriter{
		reader:     src,
		bucketName: b.config.GetBucket(),
		objectName: objectName,
		totalBytes: totalBytes,
		lastLog:    time.Now(),
		logDelay:   logDelay,
	}

	log.Logger.Infow("Upload starting", "bucket", b.config.GetBucket(), "object", objectName, "totalBytes", s.totalBytes)
	bytesWritten, err := copier(writer, s)
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	log.Logger.Infow("Upload success", "bucket", b.config.GetBucket(), "object", objectName, "bytesWritten", bytesWritten, "totalBytes", s.totalBytes)
	return nil
}

// Download reads the data contained in objectName in the bucket and outputs to dest.
// Returns non-nil for failures due to creating the reader or reading the data.
func (b *Backint) Download(ctx context.Context, objectName string, dest io.Writer, logDelay time.Duration, copier IOFileCopier) error {
	if b.bucketHandle == nil {
		return errors.New("no bucket defined")
	}

	reader, err := b.bucketHandle.Object(objectName).NewReader(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()
	s := &StorageReadWriter{
		writer:     dest,
		bucketName: b.config.GetBucket(),
		objectName: objectName,
		totalBytes: reader.Attrs.Size,
		lastLog:    time.Now(),
		logDelay:   logDelay,
	}

	log.Logger.Infow("Download starting", "bucket", b.config.GetBucket(), "object", objectName, "totalBytes", s.totalBytes)
	bytesWritten, err := copier(s, reader)
	if err != nil {
		return err
	}
	log.Logger.Infow("Download success", "bucket", b.config.GetBucket(), "object", objectName, "bytesWritten", bytesWritten, "totalBytes", s.totalBytes)
	return nil
}

// Read wraps io.Reader to provide upload progress updates.
func (s *StorageReadWriter) Read(p []byte) (n int, err error) {
	n, err = s.reader.Read(p)
	if err == nil {
		s.logProgress("Upload progress", int64(n))
	}
	return n, err
}

// Write wraps io.Writer to provide download progress updates.
func (s *StorageReadWriter) Write(p []byte) (n int, err error) {
	n, err = s.writer.Write(p)
	if err == nil {
		s.logProgress("Download progress", int64(n))
	}
	return n, err
}

// logProgress logs download/upload status including percent completion and transfer speed in MBps.
func (s *StorageReadWriter) logProgress(logMessage string, n int64) {
	s.bytesWritten += int64(n)
	if time.Since(s.lastLog) > s.logDelay {
		percentComplete := float64(s.bytesWritten) / float64(s.totalBytes) * 100.0
		transferSpeedMBps := float64(s.bytesWritten-s.lastBytesWritten) / time.Since(s.lastLog).Seconds() / 1024 / 1024
		log.Logger.Infow(logMessage, "bucket", s.bucketName, "object", s.objectName, "bytesWritten", s.bytesWritten, "totalBytes", s.totalBytes, "percentComplete", math.Round(percentComplete), "transferSpeedMBps", math.Round(transferSpeedMBps))
		s.lastLog = time.Now()
		s.lastBytesWritten = s.bytesWritten
	}
}
