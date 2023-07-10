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
	"time"

	store "cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

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
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if err := parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#PIPE") || strings.HasPrefix(line, "#FILE") {
			s := parse.Split(line)
			if len(s) < 2 {
				return fmt.Errorf("malformed backup input line, got: %s, want: #<type> <file_name> <max_size>", line)
			}
			fileType := s[0]
			fileName := s[1]
			// Filesize is an optional parameter
			fileSize := 0
			if len(s) > 2 {
				var err error
				if fileSize, err = strconv.Atoi(s[2]); err != nil {
					return fmt.Errorf("could not convert file size to int for backup input line: %s", line)
				}
			}
			output.Write([]byte(backupFile(ctx, config, bucketHandle, fileType, fileName, int64(fileSize)) + "\n"))
		} else {
			log.Logger.Infow("Unknown prefix encountered, treated as a comment", "line", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func backupFile(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, fileType, fileName string, fileSize int64) string {
	// TODO: If type is #FILE and parallel_streams > 1, chunk the file and upload in parallel.
	log.Logger.Infow("Backing up file", "fileType", fileType, "fileName", fileName, "fileSize", fileSize)
	fileNameTrim := strings.Trim(fileName, `"`)
	f, err := os.Open(fileNameTrim)
	if err != nil {
		log.Logger.Errorw("Error opening backup file", "fileName", fileName, "err", err)
		return fmt.Sprintf("#ERROR %q", fileNameTrim)
	}
	defer f.Close()

	// Generate a unique identifier for this backup which also allows indexing based on time.
	// The externalBackupID will be the number of milliseconds elapsed since January 1, 1970 UTC.
	externalBackupID := strconv.FormatInt(time.Now().UnixMilli(), 10)
	object := config.GetUserId() + fileNameTrim + "/" + externalBackupID + ".bak"
	rw := storage.ReadWriter{
		Reader:       f,
		Copier:       io.Copy,
		BucketHandle: bucketHandle,
		BucketName:   config.GetBucket(),
		ChunkSizeMb:  config.GetBufferSizeMb(),
		ObjectName:   object,
		TotalBytes:   fileSize,
		LogDelay:     time.Duration(config.GetLogDelaySec()) * time.Second,
		Compress:     config.GetCompress(),
	}
	bytesWritten, err := rw.Upload(ctx)
	if err != nil {
		log.Logger.Errorw("Error uploading file", "bucket", config.GetBucket(), "file", fileName, "obj", object, "err", err)
		return fmt.Sprintf("#ERROR %q", fileNameTrim)
	}
	log.Logger.Infow("File uploaded", "bucket", config.GetBucket(), "file", fileName, "obj", object)
	return fmt.Sprintf("#SAVED %q %q %s", externalBackupID, fileNameTrim, strconv.FormatInt(bytesWritten, 10))
}
