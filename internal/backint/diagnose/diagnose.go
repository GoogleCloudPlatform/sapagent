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

// Package diagnose runs all Backint functions (backup, inquire, restore, delete)
// including bucket/API connections to test and diagnose Backint issues.
package diagnose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	store "cloud.google.com/go/storage"
	"golang.org/x/sys/unix"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/backup"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/delete"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/inquire"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/restore"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const oneGB = int64(1024 * 1024 * 1024)

var (
	smallFileSize = oneGB
	largeFileSize = 16 * oneGB
	fileName1     = "backint-diagnose-file1.txt"
	fileName2     = "backint-diagnose-file2.txt"
	fileNotExists = "/tmp/backint-diagnose-file-not-exists.txt"
)

// diagnoseFile holds relevant info for diagnosing backup, restore, inquire, and delete functions.
type diagnoseFile struct {
	fileSize         int64
	fileName         string
	externalBackupID string
}

// executeFunc abstracts the execution of each of the Backint functions.
type executeFunc func(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, input io.Reader, output io.Writer, cloudProps *ipb.CloudProperties) bool

// removeFunc abstracts removing a file from the file system.
type removeFunc func(name string) error

// diagnoseOptions holds options for performing the requested diagnostic operation.
type diagnoseOptions struct {
	config        *bpb.BackintConfiguration
	connectParams *storage.ConnectParameters
	cloudProps    *ipb.CloudProperties
	output        io.Writer
	files         []*diagnoseFile
	execute       executeFunc
	input         string
	want          string
}

// Execute logs information and performs the diagnostic. Returns false on failures.
func Execute(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, output io.Writer, cloudProps *ipb.CloudProperties) bool {
	log.CtxLogger(ctx).Infow("DIAGNOSE starting", "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintDiagnoseStarted)
	if err := diagnose(ctx, config, connectParams, output, cloudProps); err != nil {
		log.CtxLogger(ctx).Errorw("DIAGNOSE failed", "err", err)
		usagemetrics.Error(usagemetrics.BackintDiagnoseFailure)
		output.Write([]byte(fmt.Sprintf("\nDIAGNOSE failed: %v\n", err.Error())))
		return false
	}
	log.CtxLogger(ctx).Infow("DIAGNOSE succeeded", "outFile", config.GetOutputFile())
	usagemetrics.Action(usagemetrics.BackintDiagnoseFinished)
	output.Write([]byte("\nDIAGNOSE succeeded\n"))
	return true
}

// diagnose runs all Backint functions (backup, inquire, restore, delete).
// Several files will be created, uploaded, queried, downloaded, and deleted.
// Results are written to the output. Any issues will return errors after
// attempting to clean up the temporary files locally and in the bucket.
func diagnose(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, output io.Writer, cloudProps *ipb.CloudProperties) error {
	dir := fmt.Sprintf("/tmp/backint-diagnose/%s/", strconv.FormatInt(time.Now().UnixMilli(), 10))
	if config.GetDiagnoseTmpDirectory() != "" {
		dir = fmt.Sprintf("%s/%s/", config.GetDiagnoseTmpDirectory(), strconv.FormatInt(time.Now().UnixMilli(), 10))
		log.Logger.Infof("Diagnose tmp directory overridden to %s", dir)
	}
	if config.GetDiagnoseFileMaxSizeGb() > 0 {
		log.Logger.Infof("Max diagnose file size overridden to %d GB", config.GetDiagnoseFileMaxSizeGb())
		largeFileSize = config.GetDiagnoseFileMaxSizeGb() * oneGB
	}
	files, err := createFiles(ctx, dir, fileName1, fileName2, smallFileSize, largeFileSize)
	if err != nil {
		return fmt.Errorf("createFiles error: %v", err)
	}
	opts := diagnoseOptions{config: config, connectParams: connectParams, output: output, files: files, cloudProps: cloudProps}
	defer removeFiles(ctx, opts, os.Remove)

	opts.execute = backup.Execute
	if err := diagnoseBackup(ctx, opts); err != nil {
		return fmt.Errorf("backup error: %v", err)
	}
	opts.execute = inquire.Execute
	if err := diagnoseInquire(ctx, opts); err != nil {
		return fmt.Errorf("inquire error: %v", err)
	}
	opts.execute = restore.Execute
	if err := diagnoseRestore(ctx, opts); err != nil {
		return fmt.Errorf("restore error: %v", err)
	}
	opts.execute = delete.Execute
	if err := diagnoseDelete(ctx, opts); err != nil {
		return fmt.Errorf("delete error: %v", err)
	}
	return nil
}

// createFiles creates 2 files filled with '0's of different sizes.
// Issues with file operations will return errors.
func createFiles(ctx context.Context, dir, fileName1, fileName2 string, fileSize1, fileSize2 int64) ([]*diagnoseFile, error) {
	fileName1 = dir + fileName1
	fileName2 = dir + fileName2
	log.CtxLogger(ctx).Infow("Creating files for diagnostics.", "fileName1", fileName1, "fileName2", fileName2)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	var stat unix.Statfs_t
	unix.Statfs(dir, &stat)
	availableBytes := int64(stat.Bavail * uint64(stat.Bsize))
	// Add a 1GB buffer to total 18GB needed for diagnostics.
	needBytes := fileSize1 + fileSize2 + oneGB
	if availableBytes < needBytes {
		return nil, fmt.Errorf("not enough space on disk to run diagnostics. Available bytes: %d, bytes needed: %d, dir: %s", availableBytes, needBytes, dir)
	}

	file1, err := os.Create(fileName1)
	if err != nil {
		return nil, err
	}
	defer file1.Close()
	if err := file1.Truncate(fileSize1); err != nil {
		os.Remove(fileName1)
		return nil, err
	}

	file2, err := os.Create(fileName2)
	if err != nil {
		os.Remove(fileName1)
		return nil, err
	}
	defer file2.Close()
	if err := file2.Truncate(fileSize2); err != nil {
		os.Remove(fileName1)
		os.Remove(fileName2)
		return nil, err
	}

	// The first file is added twice to test multiple versions of the same file in the bucket.
	files := []*diagnoseFile{
		{fileSize: fileSize1, fileName: fileName1},
		{fileSize: fileSize1, fileName: fileName1},
		{fileSize: fileSize2, fileName: fileName2}}
	return files, nil
}

// removeFiles cleans up the local and bucket files used for diagnostics.
// Returns true if all files were deleted and false if there was a deletion error.
func removeFiles(ctx context.Context, opts diagnoseOptions, remove removeFunc) bool {
	log.CtxLogger(ctx).Infow("Cleaning up files created for diagnostics.")
	allFilesDeleted := true
	for _, file := range opts.files {
		log.CtxLogger(ctx).Infow("Removing local file", "file", file.fileName)
		if !strings.HasPrefix(file.fileName, "/tmp/") {
			log.CtxLogger(ctx).Errorw(`File not located in "/tmp/", cannot remove`, "file", file.fileName)
			allFilesDeleted = false
			continue
		}
		if err := remove(file.fileName); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.CtxLogger(ctx).Errorw("Failed to remove local file", "file", file.fileName, "err", err)
			allFilesDeleted = false
		}
		if opts.connectParams != nil {
			bucketHandle, _ := storage.ConnectToBucket(ctx, opts.connectParams)
			object := parse.CreateObjectPath(opts.config, file.fileName, file.externalBackupID, ".bak")
			log.CtxLogger(ctx).Infow("Removing Cloud Storage Bucket file", "object", object)
			if err := bucketHandle.Object(object).Delete(ctx); err != nil && !errors.Is(err, store.ErrObjectNotExist) {
				log.CtxLogger(ctx).Errorw("Failed to remove Cloud Storage Bucket file", "link", fmt.Sprintf("https://console.cloud.google.com/storage/browser/_details/%s/%s", opts.config.GetBucket(), object), "object", object, "err", err)
				allFilesDeleted = false
			}
		}
	}
	return allFilesDeleted
}

// diagnoseBackup uploads the files to the bucket.
// The external backup IDs are saved for future diagnostics.
// Also ensure an error is present if a fake file is requested to backup.
func diagnoseBackup(ctx context.Context, opts diagnoseOptions) error {
	if len(opts.files) == 0 {
		return fmt.Errorf("no files to backup")
	}
	opts.output.Write([]byte("Testing BACKUP...\n"))
	for _, file := range opts.files {
		opts.input = fmt.Sprintf("#FILE %q %d", file.fileName, file.fileSize)
		opts.want = "#SAVED <external_backup_id> <file_name> <size>"
		splitLines, err := performDiagnostic(ctx, opts)
		if err != nil {
			return err
		}
		file.externalBackupID = strings.Trim(splitLines[0][1], `"`)
	}

	opts.input = fmt.Sprintf("#PIPE %q %d", fileNotExists, smallFileSize)
	opts.want = "#ERROR <file_name>"
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}
	return nil
}

// diagnoseInquire ensures all the files are present in the bucket when queried individually.
// Also check that the backups are returned in the proper order if NULL is specified.
// Lastly check that a fake filename is not found in the bucket.
func diagnoseInquire(ctx context.Context, opts diagnoseOptions) error {
	if len(opts.files) == 0 {
		return fmt.Errorf("no files to inquire")
	}
	opts.output.Write([]byte("Testing INQUIRE...\n"))
	for _, file := range opts.files {
		// No version supplied, the default is to leave out creation_timestamp.
		opts.input = fmt.Sprintf("#EBID %q %q", file.externalBackupID, file.fileName)
		opts.want = "#BACKUP <external_backup_id> <file_name>"
		if _, err := performDiagnostic(ctx, opts); err != nil {
			return err
		}
	}

	// Version is supplied as >= 1.50, expecting creation_timestamp is present.
	opts.input = fmt.Sprintf(`#SOFTWAREID "backint 1.50" "Google %s %s"`+"\n#NULL %q", configuration.AgentName, configuration.AgentVersion, opts.files[0].fileName)
	opts.want = "#SOFTWAREID <backint_version> <agent_version>\n#BACKUP <external_backup_id> <file_name> <creation_timestamp>\n#BACKUP <external_backup_id> <file_name> <creation_timestamp>"
	splitLines, err := performDiagnostic(ctx, opts)
	if err != nil {
		return err
	}
	// Ensure the creation_timestamp for the first line is older than the second line.
	if splitLines[1][3] < splitLines[2][3] {
		return fmt.Errorf("inquiry files are out of order based on creation time, file1: %s, file2: %s", splitLines[1], splitLines[2])
	}

	opts.input = fmt.Sprintf("#NULL %q", fileNotExists)
	opts.want = "#NOTFOUND <file_name>"
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}

	opts.input = fmt.Sprintf("#NULL %q", fileNotExists)
	opts.want = "#ERROR <file_name>"
	// Monkey patch the bucket name to an empty string to test inquire error.
	defer func(bucketName string) {
		opts.connectParams.BucketName = bucketName
	}(opts.connectParams.BucketName)
	opts.connectParams.BucketName = ""
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}
	return nil
}

// diagnoseRestore downloads the files from the bucket.
// The download will overwrite the previously created local files.
// Also check that a fake filename is not restored from the bucket.
func diagnoseRestore(ctx context.Context, opts diagnoseOptions) error {
	if len(opts.files) == 0 {
		return fmt.Errorf("no files to restore")
	}
	opts.output.Write([]byte("Testing RESTORE...\n"))
	for _, file := range opts.files {
		opts.input = fmt.Sprintf("#EBID %q %q", file.externalBackupID, file.fileName)
		opts.want = "#RESTORED <external_backup_id> <file_name>"
		if _, err := performDiagnostic(ctx, opts); err != nil {
			return err
		}
	}

	opts.input = fmt.Sprintf("#NULL %q", opts.files[0].fileName)
	opts.want = "#RESTORED <external_backup_id> <file_name>"
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}

	opts.input = fmt.Sprintf("#NULL %q", fileNotExists)
	opts.want = "#NOTFOUND <file_name>"
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}

	opts.input = fmt.Sprintf("#NULL %q", fileNotExists)
	opts.want = "#ERROR <file_name>"
	// Monkey patch the bucket name to an empty string to test restore error.
	defer func(bucketName string) {
		opts.connectParams.BucketName = bucketName
	}(opts.connectParams.BucketName)
	opts.connectParams.BucketName = ""
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}
	return nil
}

// diagnoseDelete removes all files from the bucket and
// then ensures they are not found on a subsequent request.
func diagnoseDelete(ctx context.Context, opts diagnoseOptions) error {
	if len(opts.files) == 0 {
		return fmt.Errorf("no files to delete")
	}
	opts.output.Write([]byte("Testing DELETE...\n"))
	for _, file := range opts.files {
		opts.input = fmt.Sprintf("#EBID %q %q", file.externalBackupID, file.fileName)
		opts.want = "#DELETED <external_backup_id> <file_name>"
		if _, err := performDiagnostic(ctx, opts); err != nil {
			return err
		}
	}

	for _, file := range opts.files {
		opts.input = fmt.Sprintf("#EBID %q %q", file.externalBackupID, file.fileName)
		opts.want = "#NOTFOUND <external_backup_id> <file_name>"
		if _, err := performDiagnostic(ctx, opts); err != nil {
			return err
		}
	}

	opts.input = fmt.Sprintf("#EBID %q %q", "12345", fileNotExists)
	opts.want = "#ERROR <external_backup_id> <file_name>"
	// Monkey patch the bucket name to an empty string to test delete error.
	defer func(bucketName string) {
		opts.connectParams.BucketName = bucketName
	}(opts.connectParams.BucketName)
	opts.connectParams.BucketName = ""
	if _, err := performDiagnostic(ctx, opts); err != nil {
		return err
	}
	return nil
}

// performDiagnostic executes the desired Backint function and verifies the output lines.
// The return is 2D slice where each output line is a row and each line's split value is a column.
func performDiagnostic(ctx context.Context, opts diagnoseOptions) ([][]string, error) {
	in := bytes.NewBufferString(opts.input)
	out := bytes.NewBufferString("")
	if ok := opts.execute(ctx, opts.config, opts.connectParams, in, out, opts.cloudProps); !ok {
		return nil, fmt.Errorf("failed to execute: %s", opts.input)
	}
	log.CtxLogger(ctx).Infof("Output: %s", out.Bytes())

	// Remove the final newline before splitting the lines on newlines.
	outTrim := strings.Trim(out.String(), "\n")
	lines := strings.Split(outTrim, "\n")
	wantLines := strings.Split(opts.want, "\n")
	if len(lines) != len(wantLines) {
		return nil, fmt.Errorf("unexpected number of output lines for input: %s, got: %d, want: %d", opts.input, len(lines), len(wantLines))
	}

	var splitLines [][]string
	for i, line := range lines {
		wantSplit := parse.Split(wantLines[i])
		if !strings.HasPrefix(line, wantSplit[0]) {
			return nil, fmt.Errorf("malformed output line for input: %s, got: %s, want prefix: %s", opts.input, line, wantSplit[0])
		}
		s := parse.Split(line)
		if len(s) != len(wantSplit) {
			return nil, fmt.Errorf("malformed output line for input: %s, got: %s, want format: %s", opts.input, line, wantLines[i])
		}
		splitLines = append(splitLines, s)
	}
	return splitLines, nil
}
