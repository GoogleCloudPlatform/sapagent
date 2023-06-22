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

	store "cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
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
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if err := parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#NULL") {
			s := parse.Split(line)
			// File name is an optional parameter for #NULL.
			fileName := ""
			if len(s) > 1 {
				fileName = strings.Trim(s[1], `"`)
			}
			prefix := config.GetUserId() + fileName
			output.Write(inquireFiles(ctx, bucketHandle, prefix, fileName, ""))
		} else if strings.HasPrefix(line, "#EBID") {
			s := parse.Split(line)
			if len(s) < 3 {
				return fmt.Errorf("malformed inquire input line, got: %s, want: #EBID <external_backup_id> <file_name>", line)
			}
			externalBackupID := strings.Trim(s[1], `"`)
			fileName := strings.Trim(s[2], `"`)
			prefix := config.GetUserId() + fileName + "/" + externalBackupID + ".bak"
			output.Write(inquireFiles(ctx, bucketHandle, prefix, fileName, externalBackupID))
		} else {
			log.Logger.Infow("Input line did not contain a specified prefix", "line", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// inquireFiles queries the bucket with the specified prefix and returns all
// objects found according to SAP HANA formatting specifications.
func inquireFiles(ctx context.Context, bucketHandle *store.BucketHandle, prefix, fileName, externalBackupID string) []byte {
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
			result = append(result, fmt.Sprintf(" %q", fileName)...)
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
		fileName := dirs[1]
		log.Logger.Infow("Found object", "name", object.Name, "externalBackupID", externalBackupID, "fileName", fileName)
		result = append(result, fmt.Sprintf("#BACKUP %q %q %q\n", externalBackupID, fileName, object.Created.Format(parse.BackintRFC3339Millis))...)
	}
	return result
}
