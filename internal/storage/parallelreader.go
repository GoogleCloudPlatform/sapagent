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
	"errors"
	"io"

	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// ParallelReader is a reader capable of downloading from GCS in parallel.
type ParallelReader struct {
	reader     io.Reader
	bucket     *storage.BucketHandle
	offset     int64
	objectSize int64
}

// NewParallelReader creates a reader and workers for parallel download.
func (rw *ReadWriter) NewParallelReader(ctx context.Context, object *storage.ObjectHandle) (*ParallelReader, error) {
	if object == nil {
		return nil, errors.New("no object defined")
	}

	r := &ParallelReader{
		bucket:     rw.BucketHandle,
		objectSize: rw.TotalBytes,
	}

	log.Logger.Infow("Peforming parallel restore", "objectName", rw.ObjectName, "parallelWorkers", rw.ParallelDownloadWorkers)
	var err error
	r.reader, err = object.NewRangeReader(ctx, 0, -1)
	if err != nil {
		log.Logger.Errorw("Failed to create range reader", "err", err)
		return r, err
	}
	return r, nil
}

// Read asynchronously downloads data and returns the properly ordered bytes.
func (r *ParallelReader) Read(p []byte) (int, error) {
	if r == nil {
		return 0, errors.New("no parallel reader defined")
	}
	if r.reader == nil {
		return 0, errors.New("no reader defined")
	}

	length := min(int64(len(p)), r.objectSize-r.offset)
	n, err := r.reader.Read(p[:length])
	if n == 0 && (err == nil || err == io.EOF) {
		return 0, io.EOF
	}

	if err != nil {
		log.Logger.Errorw("Failed to read from range reader", "err", err)
		return 0, err
	}
	r.offset += int64(n)
	return n, nil
}

// Close cancels any in progress transfers and performs any necessary clean up.
func (r *ParallelReader) Close() error {
	return nil
}
