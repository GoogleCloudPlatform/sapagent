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

// Package backup saves Backint files to a GCS bucket.
package backup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	store "cloud.google.com/go/storage"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"google3/third_party/sapagent/shared/log/log"
)

// statFunc abstracts stating a file.
type statFunc func(name string) (os.FileInfo, error)

// parameters holds options for backing up files.
type parameters struct {
	// reader is an optional parameter. If no reader is supplied, the fileName will be opened.
	reader io.Reader

	// Backups should contain a .bak extension. For temporary chunks used during
	// parallel uploads, the extension should be .bak<i>, where <i> is the chunk number.
	extension string

	// Generate a unique identifier for this backup which also allows indexing based on time.
	// The externalBackupID will be the number of milliseconds elapsed since January 1, 1970 UTC.
	externalBackupID string

	config       *bpb.BackintConfiguration
	bucketHandle *store.BucketHandle
	fileType     string
	fileName     string
	fileSize     int64
	output       io.Writer
	copier       storage.IOFileCopier
	wp           *workerpool.WorkerPool
	mu           *sync.Mutex
	stat         statFunc
}

// Execute logs information and performs the requested backup. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) bool {
	log.Logger.Infow("BACKUP starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintBackupStarted)
	if err := backup(ctx, config, bucketHandle, input, output); err != nil {
		log.Logger.Errorw("BACKUP failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintBackupFailure)
		return false
	}
	log.Logger.Infow("BACKUP finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintBackupFinished)
	return true
}

// backup uploads pipes and files based on each line of the input. Results for each upload are
// written to the output. Issues with file operations will return errors.
func backup(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) error {
	wp := workerpool.New(int(config.GetThreads()))
	mu := &sync.Mutex{}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		log.Logger.Infow("Executing backup input", "line", line)
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if _, err := parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#PIPE") || strings.HasPrefix(line, "#FILE") {
			s := parse.Split(line)
			if len(s) < 2 {
				return fmt.Errorf("malformed backup input line, got: %s, want: #<type> <file_name> <max_size>", line)
			}
			p := parameters{
				config:           config,
				bucketHandle:     bucketHandle,
				fileType:         s[0],
				fileName:         s[1],
				wp:               wp,
				mu:               mu,
				output:           output,
				extension:        ".bak",
				externalBackupID: strconv.FormatInt(time.Now().UnixMilli(), 10),
				stat:             os.Stat,
				copier:           io.Copy,
			}
			// Filesize is an optional parameter
			if len(s) > 2 {
				fileSize, err := strconv.Atoi(s[2])
				if err != nil {
					return fmt.Errorf("could not convert file size to int for backup input line: %s", line)
				}
				p.fileSize = int64(fileSize)
			}

			if strings.HasPrefix(line, "#FILE") && config.GetParallelStreams() > 1 {
				// An error indicates the parallel jobs were not started so write to the output.
				if out, err := backupFileParallel(ctx, p); err != nil {
					mu.Lock()
					output.Write([]byte(out))
					mu.Unlock()
				}
				continue
			}
			wp.Submit(func() {
				out := backupFile(ctx, p)
				mu.Lock()
				output.Write([]byte(out))
				mu.Unlock()
			})
		} else {
			log.Logger.Infow("Unknown prefix encountered, treated as a comment", "line", line)
		}
	}
	wp.StopWait()
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// backupFile attempts to upload a file to the bucket using the storage package.
// String results are returned on success or failure.
func backupFile(ctx context.Context, p parameters) string {
	fileNameTrim := parse.TrimAndClean(p.fileName)
	object := p.config.GetUserId() + fileNameTrim + "/" + p.externalBackupID + p.extension
	log.Logger.Infow("Backing up file", "fileType", p.fileType, "fileName", p.fileName, "obj", object, "fileSize", p.fileSize, "fileType", p.fileType)
	if p.reader == nil {
		f, err := os.Open(fileNameTrim)
		if err != nil {
			log.Logger.Errorw("Error opening backup file", "fileName", p.fileName, "obj", object, "fileType", p.fileType, "err", err)
			return fmt.Sprintf("#ERROR %s\n", p.fileName)
		}
		defer f.Close()
		if fileInfo, err := f.Stat(); err != nil || fileInfo.Mode()&0444 == 0 {
			log.Logger.Errorw("Backup file does not have readable permissions", "fileName", p.fileName, "obj", object, "fileType", p.fileType, "err", err)
			return fmt.Sprintf("#ERROR %s\n", p.fileName)
		}
		p.reader = f
	}

	rw := storage.ReadWriter{
		Reader:         p.reader,
		Copier:         p.copier,
		BucketHandle:   p.bucketHandle,
		BucketName:     p.config.GetBucket(),
		ChunkSizeMb:    p.config.GetBufferSizeMb(),
		ObjectName:     object,
		TotalBytes:     p.fileSize,
		LogDelay:       time.Duration(p.config.GetLogDelaySec()) * time.Second,
		Compress:       p.config.GetCompress(),
		DumpData:       p.config.GetDumpData(),
		RateLimitBytes: p.config.GetRateLimitMb() * 1024 * 1024,
		EncryptionKey:  p.config.GetEncryptionKey(),
		KMSKey:         p.config.GetKmsKey(),
		// Match the previous Backint implementation's metadata format.
		Metadata: map[string]string{"X-Backup-Type": strings.ReplaceAll(p.fileType, "#", "")},
	}
	bytesWritten, err := rw.Upload(ctx)
	if err != nil {
		log.Logger.Errorw("Error uploading file", "bucket", p.config.GetBucket(), "fileName", p.fileName, "obj", object, "fileType", p.fileType, "err", err)
		return fmt.Sprintf("#ERROR %s\n", p.fileName)
	}
	log.Logger.Infow("File uploaded", "bucket", p.config.GetBucket(), "fileName", p.fileName, "obj", object, "bytesWritten", bytesWritten, "fileSize", p.fileSize, "fileType", p.fileType)
	return fmt.Sprintf("#SAVED %q %s %s\n", p.externalBackupID, p.fileName, strconv.FormatInt(bytesWritten, 10))
}

// backupFileParallel chunks a file and uploads the sections in parallel
// to the bucket. The last chunk uploaded will trigger a compose operation
// which combines all chunks into 1 object and deletes the temporary chunks.
// A returned error indicates the parallel jobs were not started.
func backupFileParallel(ctx context.Context, p parameters) (string, error) {
	fileNameTrim := parse.TrimAndClean(p.fileName)
	f, err := os.Open(fileNameTrim)
	if err != nil {
		log.Logger.Errorw("Error opening backup file", "fileName", p.fileName, "err", err)
		return fmt.Sprintf("#ERROR %s\n", p.fileName), err
	}
	if p.fileSize == 0 {
		fi, err := p.stat(fileNameTrim)
		if err != nil {
			log.Logger.Errorw("Cannot determine file size, cannot chunk file for parallel upload.", "fileName", p.fileName, "err", err)
			return fmt.Sprintf("#ERROR %s\n", p.fileName), err
		}
		p.fileSize = fi.Size()
	}
	if fileInfo, err := f.Stat(); err != nil || fileInfo.Mode()&0444 == 0 {
		log.Logger.Errorw("Backup file does not have readable permissions", "fileName", p.fileName, "err", err)
		return fmt.Sprintf("#ERROR %s\n", p.fileName), fmt.Errorf("backup file does not have readable permissions")
	}

	sectionLength := p.fileSize / p.config.GetParallelStreams()
	chunksCompleted := int64(0)
	chunkError := false
	log.Logger.Infow("Sectioning file into equal sized chunks for parallel upload", "fileName", p.fileName, "fileSize", p.fileSize, "parallelStreams", p.config.GetParallelStreams(), "sectionLength", sectionLength, "externalBackupID", p.externalBackupID)
	for i := int64(0); i < p.config.GetParallelStreams(); i++ {
		p.wp.Submit(func() {
			offset := sectionLength * i
			length := sectionLength
			// If this is the last chunk, ensure it uploads the remainder of the file
			if i == p.config.GetParallelStreams()-1 {
				length = p.fileSize - offset
			}

			chunkParameters := p
			chunkParameters.extension += strconv.FormatInt(i, 10)
			chunkParameters.reader = io.NewSectionReader(f, offset, length)
			chunkParameters.fileSize = length
			result := backupFile(ctx, chunkParameters)

			p.mu.Lock()
			defer p.mu.Unlock()
			if strings.HasPrefix(result, "#ERROR") {
				log.Logger.Errorw("Error uploading chunk", "bucket", p.config.GetBucket(), "fileName", p.fileName, "err", err, "offset", offset, "sectionLength", length, "chunk", i)
				chunkError = true
			} else {
				log.Logger.Infow("Chunk uploaded", "bucket", p.config.GetBucket(), "fileName", p.fileName, "offset", offset, "sectionLength", length, "chunk", i)
			}
			chunksCompleted++
			if chunksCompleted == p.config.GetParallelStreams() {
				p.output.Write([]byte(composeChunks(ctx, p, chunkError)))
				f.Close()
			}
		})
	}
	return "", nil
}

// composeChunks composes all chunks into 1 object in the bucket and deletes
// the temporary chunks. Any chunk error will result in a failure.
func composeChunks(ctx context.Context, p parameters, chunkError bool) string {
	fileNameTrim := parse.TrimAndClean(p.fileName)
	object := p.config.GetUserId() + fileNameTrim + "/" + p.externalBackupID + ".bak"

	ret := func() string {
		if chunkError {
			return fmt.Sprintf("#ERROR %s\n", p.fileName)
		}
		return fmt.Sprintf("#SAVED %q %s %s\n", p.externalBackupID, p.fileName, strconv.FormatInt(p.fileSize, 10))
	}
	if p.config.GetDumpData() {
		log.Logger.Warnw("dump_data set to true, not composing objects.", "chunks", p.config.GetParallelStreams(), "fileName", p.fileName, "object", object)
		return ret()
	}

	log.Logger.Infow("All chunks uploaded, composing into 1 object", "chunks", p.config.GetParallelStreams(), "fileName", p.fileName, "object", object)
	var srcs []*store.ObjectHandle
	// The composer can take between 1 to 32 objects to compose into 1 object
	// parallel_streams are limited to 32 in the configuration so only 1 compose call is needed.
	for i := int64(0); i < p.config.GetParallelStreams(); i++ {
		srcs = append(srcs, p.bucketHandle.Object(object+strconv.FormatInt(i, 10)))
	}

	// Default to octet-stream, but set to the first chunk's content type if available.
	contentType := "application/octet-stream"
	if attrs, err := p.bucketHandle.Object(object + "0").Attrs(ctx); err == nil {
		contentType = attrs.ContentType
	}
	composer := p.bucketHandle.Object(object).ComposerFrom(srcs...)
	composer.ObjectAttrs.ContentType = contentType
	if _, err := composer.Run(ctx); err != nil {
		log.Logger.Errorw("Error composing object", "object", object, "chunks", p.config.GetParallelStreams(), "err", err)
		chunkError = true
	} else {
		log.Logger.Infow("Object composed", "object", object, "chunks", p.config.GetParallelStreams())
	}

	log.Logger.Infow("Deleting temporary chunked objects", "object", object, "chunks", p.config.GetParallelStreams())
	for i := int64(0); i < p.config.GetParallelStreams(); i++ {
		if err := storage.DeleteObject(ctx, p.bucketHandle, object+strconv.FormatInt(i, 10)); err != nil {
			log.Logger.Errorw("Error deleting temporary chunked object", "object", object+strconv.FormatInt(i, 10), "err", err)
			chunkError = true
		}
	}

	return ret()
}
