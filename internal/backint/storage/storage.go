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

// Package storage uploads and downloads Backint data from a GCS bucket.
package storage

import (
	"context"
	"errors"
	"io"
	"math"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

// DefaultLogDelay sets the default upload and download progress logging to once a minute.
const DefaultLogDelay = time.Minute

// IOFileCopier abstracts copying data from an io.Reader to an io.Writer.
type IOFileCopier func(dst io.Writer, src io.Reader) (written int64, err error)

// Client abstracts creating a new storage client to connect to GCS.
type Client func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error)

// ReadWriter wraps io.Reader and io.Writer to provide
// progress updates when uploading or downloading files.
type ReadWriter struct {
	Reader           io.Reader
	Writer           io.Writer
	Copier           IOFileCopier
	Config           *bpb.BackintConfiguration
	BucketHandle     *storage.BucketHandle
	ObjectName       string
	TotalBytes       int64
	bytesWritten     int64
	lastBytesWritten int64
	lastLog          time.Time
	LogDelay         time.Duration
}

// ConnectToBucket creates the storage client with custom retry logic and
// attempts to connect to the GCS bucket. Returns false if there is a connection
// failure (bucket does not exist, invalid credentials, etc.)
func ConnectToBucket(ctx context.Context, storageClient Client, config *bpb.BackintConfiguration) (*storage.BucketHandle, bool) {
	var opts []option.ClientOption
	if config.GetServiceAccount() != "" {
		opts = append(opts, option.WithCredentialsFile(config.GetServiceAccount()))
	}
	client, err := storageClient(ctx, opts...)
	if err != nil {
		log.Logger.Errorw("Failed to create GCS client. Ensure your default credentials or service account file are correct.", "error", err)
		return nil, false
	}
	// TODO: Add custom retry logic

	bucket := client.Bucket(config.GetBucket())
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to connect to bucket. Ensure the bucket exists and you have permission to access it.", "bucket", config.GetBucket(), "error", err)
		return nil, false
	}
	log.Logger.Debugw("The bucket exists and has attributes: %#v\n", attrs)

	if attrs.RetentionPolicy != nil && config.GetParallelStreams() > 1 {
		log.Logger.Errorw("Parallel streams are not supported on buckets with retention policies - 'parallel_streams' must be set to 1 in order to connect to this bucket", "bucket", config.GetBucket(), "parallelStreams", config.GetParallelStreams(), "retentionPolicy", attrs.RetentionPolicy)
		return nil, false
	}

	return bucket, true
}

// Upload writes the data contained in src to the ObjectName in the bucket.
// Returns non-nil for failures due to copy operations, writing, or closing the writer.
func (rw *ReadWriter) Upload(ctx context.Context) error {
	if rw.BucketHandle == nil {
		return errors.New("no bucket defined")
	}

	writer := rw.BucketHandle.Object(rw.ObjectName).NewWriter(ctx)
	writer.ChunkSize = int(rw.Config.GetBufferSizeMb()) * 1024 * 1024
	rw.lastLog = time.Now()
	rw.bytesWritten = 0
	rw.lastBytesWritten = 0

	log.Logger.Infow("Upload starting", "bucket", rw.Config.GetBucket(), "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	bytesWritten, err := rw.Copier(writer, rw)
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	log.Logger.Infow("Upload success", "bucket", rw.Config.GetBucket(), "object", rw.ObjectName, "bytesWritten", bytesWritten, "totalBytes", rw.TotalBytes)
	return nil
}

// Download reads the data contained in ObjectName in the bucket and outputs to dest.
// Returns non-nil for failures due to creating the reader or reading the data.
func (rw *ReadWriter) Download(ctx context.Context) error {
	if rw.BucketHandle == nil {
		return errors.New("no bucket defined")
	}

	reader, err := rw.BucketHandle.Object(rw.ObjectName).NewReader(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()
	rw.lastLog = time.Now()
	rw.bytesWritten = 0
	rw.lastBytesWritten = 0

	log.Logger.Infow("Download starting", "bucket", rw.Config.GetBucket(), "object", rw.ObjectName, "totalBytes", rw.TotalBytes)
	bytesWritten, err := rw.Copier(rw, reader)
	if err != nil {
		return err
	}
	log.Logger.Infow("Download success", "bucket", rw.Config.GetBucket(), "object", rw.ObjectName, "bytesWritten", bytesWritten, "totalBytes", rw.TotalBytes)
	return nil
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
		log.Logger.Infow(logMessage, "bucket", rw.Config.GetBucket(), "object", rw.ObjectName, "bytesWritten", rw.bytesWritten, "totalBytes", rw.TotalBytes, "percentComplete", math.Round(percentComplete), "transferSpeedMBps", math.Round(transferSpeedMBps))
		rw.lastLog = time.Now()
		rw.lastBytesWritten = rw.bytesWritten
	}
}
