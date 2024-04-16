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

// Package delete removes Backint files from a GCS bucket.
package delete

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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

// Execute logs information and performs the requested deletion. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer, cloudProps *ipb.CloudProperties) bool {
	log.CtxLogger(ctx).Infow("DELETE starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintDeleteStarted)
	if err := delete(ctx, config, connectParams, input, output); err != nil {
		log.CtxLogger(ctx).Errorw("DELETE failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintDeleteFailure)
		return false
	}
	log.CtxLogger(ctx).Infow("DELETE finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintDeleteFinished)
	return true
}

// delete deletes objects in the bucket based on each line of the input. Results for each
// deletion are written to the output. Issues with file operations will return errors.
func delete(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer) error {
	wp := workerpool.New(int(config.GetThreads()))
	mu := &sync.Mutex{}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		log.CtxLogger(ctx).Infow("Executing delete input", "line", line)
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if _, err := parse.WriteSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#EBID") {
			s := parse.Split(line)
			if len(s) < 3 {
				return fmt.Errorf("malformed delete input line, got: %s, want: #EBID <external_backup_id> <file_name>", line)
			}
			externalBackupID := strings.Trim(s[1], `"`)
			fileName := s[2]
			object := config.GetFolderPrefix() + config.GetUserId() + parse.TrimAndClean(fileName) + "/" + externalBackupID + ".bak"
			wp.Submit(func() {
				log.CtxLogger(ctx).Infow("Deleting object", "object", object)
				bucketHandle, _ := storage.ConnectToBucket(ctx, connectParams)
				err := storage.DeleteObject(ctx, bucketHandle, object, config.GetRetries())
				mu.Lock()
				defer mu.Unlock()
				if errors.Is(err, store.ErrObjectNotExist) {
					log.CtxLogger(ctx).Errorw("Object not found", "object", object, "err", err)
					output.Write([]byte(fmt.Sprintf("#NOTFOUND %q %s\n", externalBackupID, fileName)))
				} else if err != nil {
					log.CtxLogger(ctx).Errorw("Error deleting object", "object", object, "err", err)
					output.Write([]byte(fmt.Sprintf("#ERROR %q %s\n", externalBackupID, fileName)))
				} else {
					log.CtxLogger(ctx).Infow("Object deleted", "object", object)
					output.Write([]byte(fmt.Sprintf("#DELETED %q %s\n", externalBackupID, fileName)))
				}
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
