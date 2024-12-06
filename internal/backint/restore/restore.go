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
	"syscall"
	"time"

	store "cloud.google.com/go/storage"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/metrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/storage"
)

// Execute logs information and performs the requested restore. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer, cloudProps *ipb.CloudProperties) bool {
	log.CtxLogger(ctx).Infow("RESTORE starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintRestoreStarted)
	if err := restore(ctx, config, connectParams, input, output, cloudProps); err != nil {
		log.CtxLogger(ctx).Errorw("RESTORE failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintRestoreFailure)
		return false
	}
	log.CtxLogger(ctx).Infow("RESTORE finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintRestoreFinished)
	return true
}

// restore downloads files from the bucket based on each line of the input. Results for each
// restore are written to the output. Issues with file operations will return errors.
func restore(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer, cloudProps *ipb.CloudProperties) error {
	startTime := time.Now()
	wp := workerpool.New(int(config.GetThreads()))
	mu := &sync.Mutex{}
	scanner := bufio.NewScanner(input)
	var lastFileName string
	for scanner.Scan() {
		line := scanner.Text()
		log.CtxLogger(ctx).Infow("Executing restore input", "line", line)
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if _, err := parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#NULL") {
			s := parse.Split(line)
			if len(s) < 2 {
				return fmt.Errorf("malformed restore input line, got: %s, want: #NULL <file_name> [<dest_name>]", line)
			}
			fileName := s[1]
			destName := fileName
			lastFileName = fileName
			// Destination is an optional parameter.
			if len(s) > 2 {
				destName = s[2]
			}
			restoreFunc := func() {
				bucketHandle, _ := storage.ConnectToBucket(ctx, connectParams)
				out := restoreFile(ctx, config, connectParams, bucketHandle, io.Copy, fileName, destName, "", cloudProps)
				mu.Lock()
				defer mu.Unlock()
				output.Write(out)
			}
			// Log restores happen in serial, so do not submit to the workerpool.
			if strings.Contains(fileName, "log_backup") {
				restoreFunc()
			} else {
				wp.Submit(restoreFunc)
			}
		} else if strings.HasPrefix(line, "#EBID") {
			s := parse.Split(line)
			if len(s) < 3 {
				return fmt.Errorf("malformed restore input line, got: %s, want: #EBID <external_backup_id> <file_name> [<dest_name>]", line)
			}
			externalBackupID := parse.TrimAndClean(s[1])
			fileName := s[2]
			destName := fileName
			lastFileName = fileName
			// Destination is an optional parameter.
			if len(s) > 3 {
				destName = s[3]
			}
			restoreFunc := func() {
				bucketHandle, _ := storage.ConnectToBucket(ctx, connectParams)
				out := restoreFile(ctx, config, connectParams, bucketHandle, io.Copy, fileName, destName, externalBackupID, cloudProps)
				mu.Lock()
				defer mu.Unlock()
				output.Write(out)
			}
			// Log restores happen in serial, so do not submit to the workerpool.
			if strings.Contains(fileName, "log_backup") {
				restoreFunc()
			} else {
				wp.Submit(restoreFunc)
			}
		} else {
			log.CtxLogger(ctx).Infow("Unknown prefix encountered, treated as a comment", "line", line)
		}
	}
	wp.StopWait()
	if err := scanner.Err(); err != nil {
		return err
	}
	metrics.WriteFileTransferLog(ctx, "restore", parse.TrimAndClean(lastFileName), time.Since(startTime), config, cloudProps)
	return nil
}

// restoreFile queries the bucket to see if the backup exists.
// If externalBackupID is not specified, the latest backup for fileName is used.
// If found, the file is downloaded and saved to destName.
func restoreFile(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, bucketHandle *store.BucketHandle, copier storage.IOFileCopier, fileName, destName, externalBackupID string, cloudProps *ipb.CloudProperties) []byte {
	prefix := parse.CreateObjectPath(config, parse.TrimAndClean(fileName), "", "")
	if externalBackupID != "" {
		prefix += fmt.Sprintf("%s.bak", externalBackupID)
	}

	log.CtxLogger(ctx).Infow("Restoring file", "userID", config.GetUserId(), "fileName", fileName, "destName", destName, "prefix", prefix, "externalBackupID", externalBackupID)
	objects, err := storage.ListObjects(ctx, bucketHandle, prefix, "", config.GetRetries())
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error listing objects", "fileName", fileName, "prefix", prefix, "err", err, "externalBackupID", externalBackupID)
		return []byte(fmt.Sprintf("#ERROR %s\n", fileName))
	} else if len(objects) == 0 {
		log.CtxLogger(ctx).Warnw("No objects found", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID)
		return []byte(fmt.Sprintf("#NOTFOUND %s\n", fileName))
	}
	// The objects will be sorted by creation date, latest first.
	// Thus, the requested restore will always be the first in the objects slice.
	object := objects[0]

	// From the specification, files are created by Backint and pipes are created by the database.
	log.CtxLogger(ctx).Infow("Restoring object from bucket", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID, "obj", object.Name, "fileType", object.Metadata["X-Backup-Type"], "destName", destName, "objSize", object.Size)
	var destFile *os.File
	if object.Metadata["X-Backup-Type"] == "FILE" {
		destFile, err = os.Create(parse.TrimAndClean(destName))
	} else {
		destFile, err = parse.OpenFileWithRetries(parse.TrimAndClean(destName), os.O_WRONLY|syscall.O_NONBLOCK, 0, config.GetFileReadTimeoutMs())
	}
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error opening dest file", "destName", destName, "err", err, "fileType", object.Metadata["X-Backup-Type"])
		return []byte(fmt.Sprintf("#ERROR %s\n", fileName))
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			log.CtxLogger(ctx).Errorw("Error closing dest file", "destName", destName, "err", err)
			return
		}
		log.CtxLogger(ctx).Infow("Dest file closed successfully", "destName", destName)
	}()
	if fileInfo, err := destFile.Stat(); err != nil || fileInfo.Mode()&0222 == 0 {
		log.CtxLogger(ctx).Errorw("Dest file does not have writeable permissions", "destName", destName, "fileInfo", fileInfo.Mode(), "err", err)
		return []byte(fmt.Sprintf("#ERROR %s\n", fileName))
	}

	rw := storage.ReadWriter{
		Writer:                        destFile,
		Copier:                        copier,
		BucketHandle:                  bucketHandle,
		BucketName:                    config.GetBucket(),
		ChunkSizeMb:                   config.GetBufferSizeMb(),
		ObjectName:                    object.Name,
		TotalBytes:                    object.Size,
		LogDelay:                      time.Duration(config.GetLogDelaySec()) * time.Second,
		RateLimitBytes:                config.GetRateLimitMb() * 1024 * 1024,
		EncryptionKey:                 config.GetEncryptionKey(),
		KMSKey:                        config.GetKmsKey(),
		MaxRetries:                    config.GetRetries(),
		RetryBackoffInitial:           time.Duration(config.GetRetryBackoffInitial()) * time.Second,
		RetryBackoffMax:               time.Duration(config.GetRetryBackoffMax()) * time.Second,
		RetryBackoffMultiplier:        float64(config.GetRetryBackoffMultiplier()),
		ParallelDownloadWorkers:       config.GetParallelRecoveryStreams(),
		ParallelDownloadConnectParams: connectParams,
	}
	startTime := time.Now()
	bytesWritten, err := rw.Download(ctx)
	downloadTime := time.Since(startTime)
	defer metrics.SendToCloudMonitoring(ctx, "restore", fileName, bytesWritten, downloadTime, config, err == nil, cloudProps, cloudmonitoring.NoBackOff(), metrics.DefaultMetricClient)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error downloading file", "bucket", config.GetBucket(), "destName", destName, "obj", object.Name, "err", err)
		return []byte(fmt.Sprintf("#ERROR %s\n", fileName))
	}
	log.CtxLogger(ctx).Infow("File restored", "bucket", config.GetBucket(), "destName", destName, "obj", object.Name, "bytesWritten", bytesWritten, "downloadTimeSec", downloadTime.Round(time.Millisecond))
	externalBackupID = strings.TrimSuffix(filepath.Base(object.Name), ".bak")
	return []byte(fmt.Sprintf("#RESTORED %q %s\n", externalBackupID, fileName))
}
