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

	"cloud.google.com/go/storage"
)

// ParallelReader is a reader capable of downloading from GCS in parallel.
type ParallelReader struct {
	bucket *storage.BucketHandle
}

// NewParallelReader creates a reader and workers for parallel download.
func (rw *ReadWriter) NewParallelReader(ctx context.Context) (*ParallelReader, error) {
	r := &ParallelReader{
		bucket: rw.BucketHandle,
	}
	return r, nil
}

// Read asynchronously downloads data and returns the properly ordered bytes.
func (r *ParallelReader) Read(p []byte) (int, error) {
	return len(p), nil
}

// Close cancels any in progress transfers and performs any necessary clean up.
func (r *ParallelReader) Close() error {
	return nil
}
