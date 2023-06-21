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

// Package function executes the backup, restore, inquire and delete Backint functions.
package function

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	store "cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

// backintRFC3339Millis is a reference for timestamps to Backint specifications.
const backintRFC3339Millis = "2006-01-02T15:04:05.999Z07:00"

// Execute opens the input file and creates the output file then selects which Backint function
// to execute based on the configuration. Issues with file operations or config will return false.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle) bool {
	log.Logger.Infow("Executing Backint function", "function", config.GetFunction().String(), "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	inFile, err := os.Open(config.GetInputFile())
	if err != nil {
		log.Logger.Errorw("Error opening input file", "fileName", config.GetInputFile(), "err", err)
		return false
	}
	defer inFile.Close()
	outFile, err := os.Create(config.GetOutputFile())
	if err != nil {
		log.Logger.Errorw("Error opening output file", "fileName", config.GetOutputFile(), "err", err)
		return false
	}
	defer outFile.Close()

	switch config.GetFunction() {
	case bpb.Function_BACKUP:
		log.Logger.Infow("BACKUP starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
		usagemetrics.Action(usagemetrics.BackintBackupStarted)
		if err := backup(ctx, config, bucketHandle, inFile, outFile); err != nil {
			log.Logger.Errorw("BACKUP failed", "err", err)
			usagemetrics.Error(usagemetrics.BackintBackupFailure)
			return false
		}
		log.Logger.Infow("BACKUP finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
		usagemetrics.Action(usagemetrics.BackintBackupFinished)
	case bpb.Function_INQUIRE:
		log.Logger.Infow("INQUIRE starting", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
		usagemetrics.Action(usagemetrics.BackintInquireStarted)
		if err := inquire(ctx, config, bucketHandle, inFile, outFile); err != nil {
			log.Logger.Errorw("INQUIRE failed", "err", err)
			usagemetrics.Error(usagemetrics.BackintInquireFailure)
			return false
		}
		log.Logger.Infow("INQUIRE finished", "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
		usagemetrics.Action(usagemetrics.BackintInquireFinished)
	default:
		log.Logger.Errorw("Unsupported Backint function", "function", config.GetFunction().String())
		return false
	}
	return true
}

// backup uploads pipes and files based on each line of the input. Results for each upload are
// written to the output. Issues with file operations will return errors.
func backup(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if err := parseSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#PIPE") || strings.HasPrefix(line, "#FILE") {
			s := split(line)
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
	}
	bytesWritten, err := rw.Upload(ctx)
	if err != nil {
		log.Logger.Errorw("Error uploading file", "bucket", config.GetBucket(), "file", fileName, "obj", object, "err", err)
		return fmt.Sprintf("#ERROR %q", fileNameTrim)
	}
	log.Logger.Infow("File uploaded", "bucket", config.GetBucket(), "file", fileName, "obj", object)
	return fmt.Sprintf("#SAVED %q %q %s", externalBackupID, fileNameTrim, strconv.FormatInt(bytesWritten, 10))
}

// inquire queries the bucket for objects based on each line of the input. Results for each
// inquiry are written to the output. Issues with file operations will return errors.
func inquire(ctx context.Context, config *bpb.BackintConfiguration, bucketHandle *store.BucketHandle, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#SOFTWAREID") {
			if err := parseSoftwareVersion(line, output); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "#NULL") {
			s := split(line)
			// File name is an optional parameter for #NULL.
			fileName := ""
			if len(s) > 1 {
				fileName = strings.Trim(s[1], `"`)
			}
			prefix := config.GetUserId() + fileName
			output.Write(inquireFiles(ctx, bucketHandle, prefix, fileName, ""))
		} else if strings.HasPrefix(line, "#EBID") {
			s := split(line)
			if len(s) < 3 {
				return fmt.Errorf("malformed inquire input line, got: %s, want: #EBID <external_backup_id> <file_name>", line)
			}
			externalBackupID := strings.Trim(s[1], `"`)
			fileName := strings.Trim(s[2], `"`)
			prefix := config.GetUserId() + fileName + "/" + externalBackupID + ".bak"
			output.Write(inquireFiles(ctx, bucketHandle, prefix, fileName, externalBackupID))
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
		result = append(result, fmt.Sprintf("#BACKUP %q %q %q\n", externalBackupID, fileName, object.Created.Format(backintRFC3339Millis))...)
	}
	return result
}

// split performs a custom split on spaces based on the following SAP HANA Backint specifications:
//
// * Parameters may be quoted with double quotation marks ".
//
// * Parameters containing a space must be quoted with double quotation marks.
//
// * If a parameter contains a double quote, the double quote must be escaped with a backslash.
//
// * A backslash escapes double quotes only. A backslash in a parameter value must not be escaped.
func split(s string) []string {
	var result []string
	quote := false
	backslash := false
	start := 0
	for end, c := range s {
		if c == '"' {
			if backslash {
				backslash = false
			} else {
				quote = !quote
			}
		} else if c == '\\' {
			backslash = true
		} else if c == ' ' && !quote {
			result = append(result, s[start:end])
			start = end + 1
		}
	}
	return append(result, s[start:])
}

// parseSoftwareVersion writes the Backint and agent software versions to the output.
func parseSoftwareVersion(line string, output io.Writer) error {
	s := split(line)
	if len(s) < 2 {
		return fmt.Errorf("malformed input line, got: %s, want: #SOFTWAREID <backint_version> <software_version>", line)
	}
	log.Logger.Infow("Version information", "backint", strings.Trim(s[1], `"`), configuration.AgentName, configuration.AgentVersion)
	output.Write([]byte(fmt.Sprintf(`#SOFTWAREID %s "Google %s %s"`, s[1], configuration.AgentName, configuration.AgentVersion) + "\n"))
	return nil
}
