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
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// Execute logs information and performs the requested inquiry. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) bool {
	log.Logger.Infow("INQUIRE starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintInquireStarted)
	if err := inquire(ctx, config, bucketHandle, input, output); err != nil {
		log.Logger.Errorw("INQUIRE failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintInquireFailure)
		return false
	}
	log.Logger.Infow("INQUIRE finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintInquireFinished)
	return true
}

// inquire queries the bucket for objects based on each line of the input. Results for each
// inquiry are written to the output. Issues with file operations will return errors.
func inquire(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) error {
	wp := workerpool.New(int(config.GetThreads()))
	mu := &sync.Mutex{}
	backintVersion := ""
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		log.Logger.Infow("Executing inquire input", "line", line)
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
			prefix := config.GetUserId() + parse.TrimAndClean(fileName)
			wp.Submit(func() {
				out := inquireFiles(ctx, bucketHandle, prefix, fileName, "", backintVersion)
				mu.Lock()
				defer mu.Unlock()
				output.Write(out)
			})
		} else if strings.HasPrefix(line, "#EBID") {
			s := parse.Split(line)
			if len(s) < 3 {
				return fmt.Errorf("malformed inquire input line, got: %s, want: #EBID <external_backup_id> <file_name>", line)
			}
			externalBackupID := parse.TrimAndClean(s[1])
			fileName := s[2]
			prefix := config.GetUserId() + parse.TrimAndClean(fileName) + "/" + externalBackupID + ".bak"
			wp.Submit(func() {
				out := inquireFiles(ctx, bucketHandle, prefix, fileName, externalBackupID, backintVersion)
				mu.Lock()
				defer mu.Unlock()
				output.Write(out)
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

// inquireFiles queries the bucket with the specified prefix and returns all
// objects found according to SAP HANA formatting specifications.
func inquireFiles(ctx context.Context, bucketHandle *store.BucketHandle, prefix, fileName, externalBackupID, backintVersion string) []byte {
	var result []byte
	log.Logger.Infow("Listing objects", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID)
	objects, err := storage.ListObjects(ctx, bucketHandle, prefix)
	if err != nil {
		log.Logger.Errorw("Error listing objects", "fileName", fileName, "prefix", prefix, "err", err, "externalBackupID", externalBackupID)
		result = []byte("#ERROR")
	} else if len(objects) == 0 {
		log.Logger.Warnw("No objects found", "fileName", fileName, "prefix", prefix, "externalBackupID", externalBackupID)
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
			log.Logger.Errorw("Unexpected object name, cannot generate original file name", "name", object.Name)
			result = append(result, fmt.Sprintf("#ERROR %q %q\n", externalBackupID, object.Name)...)
			continue
		}
		fileName = parse.RestoreFilename(dirs[1])
		log.Logger.Infow("Found object", "name", object.Name, "externalBackupID", externalBackupID, "fileName", fileName)

		// Backint versions prior to 1.50 do not include the creation timestamp in the INQUIRE output.
		result = append(result, fmt.Sprintf("#BACKUP %q %s", externalBackupID, fileName)...)
		if backintVersion >= "1.50" {
			result = append(result, fmt.Sprintf(" %q", object.Created.Format(parse.BackintRFC3339Millis))...)
		}
		result = append(result, "\n"...)
	}
	return result
}
