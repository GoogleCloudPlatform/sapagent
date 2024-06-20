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
	ctx           context.Context
	object        *storage.ObjectHandle
	objectSize    int64
	objectOffset  int64
	partSizeBytes int64
	workers       []*downloadWorker
}

// downloadWorker will buffer and try downloading a single part.
type downloadWorker struct {
	bucket       *storage.BucketHandle
	reader       io.Reader
	buffer       []byte
	chunkSize    int64
	bufferOffset int64
	BytesRemain  int64
}

// NewParallelReader creates workers with readers for parallel download.
func (rw *ReadWriter) NewParallelReader(ctx context.Context, object *storage.ObjectHandle) (*ParallelReader, error) {
	if object == nil {
		return nil, errors.New("no object defined")
	}
	// temporary addition, currently supports only 1 worker
	// TODO: add support for multiple workers
	rw.ParallelDownloadWorkers = 1

	r := &ParallelReader{
		ctx:           ctx,
		object:        object,
		objectSize:    rw.TotalBytes,
		partSizeBytes: rw.ChunkSizeMb * 1024 * 1024,
		workers:       make([]*downloadWorker, rw.ParallelDownloadWorkers),
	}

	for i := int64(0); i < rw.ParallelDownloadWorkers; i++ {
		r.workers[i] = &downloadWorker{
			buffer: make([]byte, r.partSizeBytes),
		}
		// creating a new bucket handle for each worker to support parallel downloads
		r.workers[i].bucket, _ = ConnectToBucket(ctx, rw.ParallelDownloadConnectParams)
	}
	log.Logger.Infow("Peforming parallel restore", "objectName", rw.ObjectName, "parallelWorkers", rw.ParallelDownloadWorkers)
	return r, nil
}

// Read asynchronously downloads data and returns the properly ordered bytes.
func (r *ParallelReader) Read(p []byte) (int, error) {
	if r == nil {
		return 0, errors.New("no parallel reader defined")
	}
	if r.objectOffset >= r.objectSize {
		return 0, io.EOF
	}

	// if the worker's buffer is empty, fill it with data from GCS
	if r.workers[0].BytesRemain == 0 {
		if err := fillWorkerBuffer(r, r.workers[0]); err != nil {
			return 0, err
		}
	}

	// copying data from the worker's buffer to the provided buffer
	bufOffset := r.workers[0].bufferOffset

	// Copy valid data, avoiding issues with buffer overruns
	dataToCopy := r.workers[0].buffer[bufOffset:r.workers[0].chunkSize]
	n := copy(p, dataToCopy)

	r.objectOffset += int64(n)
	r.workers[0].bufferOffset += int64(n)
	r.workers[0].BytesRemain -= int64(n)

	// Reset worker only if the entire buffer has been consumed
	if r.workers[0].BytesRemain == 0 {
		r.workers[0].reader = nil
		r.workers[0].bufferOffset = 0
	}
	return n, nil
}

// fillWorkerBuffer fills the worker's buffer with data from GCS.
func fillWorkerBuffer(r *ParallelReader, worker *downloadWorker) error {
	if worker == nil {
		return errors.New("no worker defined")
	}

	var err error
	if worker.reader, err = r.object.NewRangeReader(r.ctx, r.objectOffset, r.partSizeBytes); err != nil {
		log.Logger.Errorw("Failed to create range reader", "err", err)
		return err
	}

	// reading data into the worker's buffer until the whole length is read
	bytesRead := int64(0)
	//	to make sure we always enter the loop at least once
	for {
		var n int
		if n, err = worker.reader.Read(worker.buffer[bytesRead:]); err != nil && err != io.EOF {
			log.Logger.Errorw("Failed to read from range reader", "err", err)
			return err
		}
		if n == 0 || err == io.EOF {
			// all bytes are	read, we must exit the loop
			break
		}
		bytesRead += int64(n)
	}
	worker.BytesRemain = bytesRead
	r.workers[0].chunkSize = bytesRead
	return nil
}

// Close cancels any in progress transfers and performs any necessary clean up.
func (r *ParallelReader) Close() error {
	return nil
}
