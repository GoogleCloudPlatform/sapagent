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

// Package inquire queries a GCS bucket and outputs information on Backint backups.
package inquire

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	store "cloud.google.com/go/storage"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// Execute logs information and performs the requested inquiry. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer, cloudProps *ipb.CloudProperties) bool {
	log.CtxLogger(ctx).Infow("INQUIRE starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintInquireStarted)
	if err := inquire(ctx, config, connectParams, input, output); err != nil {
		log.CtxLogger(ctx).Errorw("INQUIRE failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintInquireFailure)
		return false
	}
	log.CtxLogger(ctx).Infow("INQUIRE finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintInquireFinished)
	return true
}

// inquire queries the bucket for objects based on each line of the input. Results for each
// inquiry are written to the output. Issues with file operations will return errors.
func inquire(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer) error {
	wp := workerpool.New(int(config.GetThreads()))
	mu := &sync.Mutex{}
	backintVersion := ""
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		log.CtxLogger(ctx).Infow("Executing inquire input", "line", line)
		if strings.HasPrefix(line, "#SOFTWAREID") {
			var err error
			if backintVersion, err = parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#NULL") {
			s := parse.Split(line)
			// File name is an optional parameter for #NULL.
			fileName := ""
			if len(s) > 1 {
				fileName = s[1]
			}
			prefix := config.GetFolderPrefix() + config.GetUserId() + parse.TrimAndClean(fileName)
			wp.Submit(func() {
				bucketHandle, _ := storage.ConnectToBucket(ctx, connectParams)
				out := inquireFiles(ctx, bucketHandle, prefix, fileName, "", backintVersion, "", config.GetRetries())
				mu.Lock()
				defer mu.Unlock()
				output.Write(out)
			})
		} else if strings.HasPrefix(line, "#EBID") {
			s := parse.Split(line)
			if len(s) < 2 {
				return fmt.Errorf("malformed inquire input line, got: %s, want: #EBID <external_backup_id> <file_name> or #EBID <external_backup_id>", line)
			}
			externalBackupID := parse.TrimAndClean(s[1])
			// fileName is an optional parameter not present in Backint 1.00
			fileName := ""
			prefix := config.GetFolderPrefix() + config.GetUserId()
			filter := ""
			if len(s) > 2 {
				fileName = s[2]
				prefix += parse.TrimAndClean(fileName) + "/" + externalBackupID + ".bak"
			} else {
				filter = externalBackupID + ".bak"
			}
			wp.Submit(func() {
				bucketHandle, _ := storage.ConnectToBucket(ctx, connectParams)
				out := inquireFiles(ctx, bucketHandle, prefix, fileName, externalBackupID, backintVersion, filter, config.GetRetries())
				mu.Lock()
				defer mu.Unlock()
				output.Write(out)
			})
		} else {
			log.CtxLogger(ctx).Infow("Unknown prefix encountered, treated as a comment", "line", line)
		}
	}
	wp.StopWait()
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// inquireFiles queries the bucket with the specified prefix and returns all
// objects found according to SAP HANA formatting specifications.
func inquireFiles(ctx context.Context, bucketHandle *store.BucketHandle, prefix, fileName, externalBackupID, backintVersion, filter string, retries int64) []byte {
	var result []byte
	log.CtxLogger(ctx).Infow("Listing objects", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID, "filter", filter)
	objects, err := storage.ListObjects(ctx, bucketHandle, prefix, filter, retries)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error listing objects", "fileName", fileName, "prefix", prefix, "err", err, "externalBackupID", externalBackupID, "filter", filter)
		result = []byte("#ERROR")
	} else if len(objects) == 0 {
		log.CtxLogger(ctx).Warnw("No objects found", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID, "filter", filter)
		result = []byte("#NOTFOUND")
	}
	// If there was an error or no objects were found, append the optional parameters and return.
	if len(result) > 0 {
		if externalBackupID != "" {
			result = append(result, fmt.Sprintf(" %q", externalBackupID)...)
		}
		if fileName != "" {
			result = append(result, fmt.Sprintf(" %s", fileName)...)
		}
		return append(result, "\n"...)
	}

	for _, object := range objects {
		// The backup object name is in the format <userID>/<fileName>/<externalBackupID>.bak
		externalBackupID := strings.TrimSuffix(filepath.Base(object.Name), ".bak")
		dirs := strings.SplitN(filepath.Dir(object.Name), "/", 2)
		if len(dirs) < 2 {
			log.CtxLogger(ctx).Errorw("Unexpected object name, cannot generate original file name", "name", object.Name)
			result = append(result, fmt.Sprintf("#ERROR %q %q\n", externalBackupID, object.Name)...)
			continue
		}
		fileName = parse.RestoreFilename(dirs[1])
		log.CtxLogger(ctx).Infow("Found object", "name", object.Name, "externalBackupID", externalBackupID, "fileName", fileName)

		// Backint versions prior to 1.50 do not include the creation timestamp in the INQUIRE output.
		result = append(result, fmt.Sprintf("#BACKUP %q %s", externalBackupID, fileName)...)
		if backintVersion >= "1.50" {
			result = append(result, fmt.Sprintf(" %q", object.Created.Format(parse.BackintRFC3339Millis))...)
		}
		result = append(result, "\n"...)
	}
	return result
}
