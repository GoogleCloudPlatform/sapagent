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

// Package restore downloads Backint backups from a GCS bucket.
package restore

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	store "cloud.google.com/go/storage"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

// Execute logs information and performs the requested restore. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) bool {
	log.Logger.Infow("RESTORE starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintRestoreStarted)
	if err := restore(ctx, config, bucketHandle, input, output); err != nil {
		log.Logger.Errorw("RESTORE failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintRestoreFailure)
		return false
	}
	log.Logger.Infow("RESTORE finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintRestoreFinished)
	return true
}

// restore downloads files from the bucket based on each line of the input. Results for each
// restore are written to the output. Issues with file operations will return errors.
func restore(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) error {
	wp := workerpool.New(int(config.GetThreads()))
	mu := &sync.Mutex{}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if err := parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#NULL") {
			s := parse.Split(line)
			if len(s) < 2 {
				return fmt.Errorf("malformed restore input line, got: %s, want: #NULL <file_name> [<dest_name>]", line)
			}
			fileName := strings.Trim(s[1], `"`)
			destName := fileName
			// Destination is an optional parameter.
			if len(s) > 2 {
				destName = strings.Trim(s[2], `"`)
			}
			wp.Submit(func() {
				out := restoreFile(ctx, config, bucketHandle, io.Copy, fileName, destName, "")
				mu.Lock()
				defer mu.Unlock()
				output.Write([]byte(out))
			})
		} else if strings.HasPrefix(line, "#EBID") {
			s := parse.Split(line)
			if len(s) < 3 {
				return fmt.Errorf("malformed restore input line, got: %s, want: #EBID <external_backup_id> <file_name> [<dest_name>]", line)
			}
			externalBackupID := strings.Trim(s[1], `"`)
			fileName := strings.Trim(s[2], `"`)
			destName := fileName
			// Destination is an optional parameter.
			if len(s) > 3 {
				destName = strings.Trim(s[3], `"`)
			}
			wp.Submit(func() {
				out := restoreFile(ctx, config, bucketHandle, io.Copy, fileName, destName, externalBackupID)
				mu.Lock()
				defer mu.Unlock()
				output.Write([]byte(out))
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

// restoreFile queries the bucket to see if the backup exists.
// If externalBackupID is not specified, the latest backup for fileName is used.
// If found, the file is downloaded and saved to destName.
func restoreFile(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, copier storage.IOFileCopier, fileName, destName, externalBackupID string) []byte {
	prefix := config.GetUserId() + fileName
	if externalBackupID != "" {
		prefix += fmt.Sprintf("/%s.bak", externalBackupID)
	}

	log.Logger.Infow("Restoring file", "userID", config.GetUserId(), "fileName", fileName, "destName", destName, "prefix", prefix, "externalBackupID", externalBackupID)
	objects, err := storage.ListObjects(ctx, bucketHandle, prefix)
	if err != nil {
		log.Logger.Errorw("Error listing objects", "fileName", fileName, "prefix", prefix, "err", err, "externalBackupID", externalBackupID)
		return []byte(fmt.Sprintf("#ERROR %q\n", fileName))
	} else if len(objects) == 0 {
		log.Logger.Warnw("No objects found", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID)
		return []byte(fmt.Sprintf("#NOTFOUND %q\n", fileName))
	}

	destFile, err := os.Create(destName)
	if err != nil {
		log.Logger.Errorw("Error opening dest file", "destName", destName, "err", err)
		return []byte(fmt.Sprintf("#ERROR %q\n", fileName))
	}
	defer destFile.Close()
	rw := storage.ReadWriter{
		Writer:       destFile,
		Copier:       copier,
		BucketHandle: bucketHandle,
		BucketName:   config.GetBucket(),
		// The objects will be sorted by creation date, latest first.
		// Thus, the requested restore will always be the first in the objects slice.
		ObjectName:     objects[0].Name,
		TotalBytes:     objects[0].Size,
		LogDelay:       time.Duration(config.GetLogDelaySec()) * time.Second,
		RateLimitBytes: config.GetRateLimitMb() * 1024 * 1024,
	}
	bytesWritten, err := rw.Download(ctx)
	if err != nil {
		log.Logger.Errorw("Error downloading file", "bucket", config.GetBucket(), "destName", destName, "obj", objects[0].Name, "err", err)
		return []byte(fmt.Sprintf("#ERROR %q\n", fileName))
	}
	log.Logger.Infow("File restored", "bucket", config.GetBucket(), "destName", destName, "obj", objects[0].Name, "bytesWritten", bytesWritten)
	externalBackupID = strings.TrimSuffix(filepath.Base(objects[0].Name), ".bak")
	return []byte(fmt.Sprintf("#RESTORED %q %q\n", externalBackupID, fileName))
}
