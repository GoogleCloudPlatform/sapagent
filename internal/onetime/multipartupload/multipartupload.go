/*
Copyright 2024 Google LLC

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

// Package multipartupload implements the one time execution mode for
// uploading a file to a GCS bucket using XML Multipart API.
package multipartupload

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"flag"
	s "cloud.google.com/go/storage"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/storage"
)

// OpenFileFunc provides a testable replacement for os.OpenFile.
type OpenFileFunc func(string, int, os.FileMode) (*os.File, error)

// MultipartUpload has args for multipartupload subcommands.
type MultipartUpload struct {
	SourceFile      string
	TargetPath      string
	ParallelThreads int64
	help            bool

	bucketName   string
	objectName   string
	bucketHandle *s.BucketHandle
	copier       storage.IOFileCopier
	client       storage.Client
	fileOpener   OpenFileFunc

	IIOTEParams *onetime.InternallyInvokedOTE
	oteLogger   *onetime.OTELogger
}

// Name implements the subcommand interface for multipartupload.
func (*MultipartUpload) Name() string { return "multipartupload" }

// Synopsis implements the subcommand interface for multipartupload.
func (*MultipartUpload) Synopsis() string {
	return "upload a file to a GCS bucket using XML Multipart API"
}

// Usage implements the subcommand interface for multipartupload.
func (*MultipartUpload) Usage() string {
	return `Usage: multipartupload --source-file=<path-to-source-file>
       --target-path=<bucket and path in the format gs://<bucket-name>/<target-file-path> >
       --parallel-threads=<integer, defaults to 16>

`
}

// SetFlags implements the subcommand interface for multipartupload.
func (m *MultipartUpload) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&m.SourceFile, "source-file", "", "The source file to upload")
	fs.StringVar(&m.SourceFile, "s", "", "The source file to upload")
	fs.StringVar(&m.TargetPath, "target-path", "", "The target path in the format gs://<bucket-name>/<target-dir>")
	fs.StringVar(&m.TargetPath, "t", "", "The target path in the format gs://<bucket-name>/<target-dir>")
	fs.Int64Var(&m.ParallelThreads, "parallel-threads", 16, "The number of parallel threads to use for uploading")
	fs.Int64Var(&m.ParallelThreads, "p", 16, "The number of parallel threads to use for uploading")
	fs.BoolVar(&m.help, "h", false, "Displays help")
}

// Execute implements the subcommand interface for multipartupload.
func (m *MultipartUpload) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     m.Name(),
		Help:     m.help,
		Fs:       f,
		LogLevel: "info",
		IIOTE:    m.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	return m.Run(ctx, onetime.CreateRunOptions(nil, false))
}

// Run performs the functionality specified by the multipartupload subcommand.
func (m *MultipartUpload) Run(ctx context.Context, runOpts *onetime.RunOptions) subcommands.ExitStatus {
	m.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	if m.SourceFile == "" {
		m.oteLogger.LogErrorToFileAndConsole(ctx, "MultipartUpload usage error:", fmt.Errorf("source file must be provided"))
		return subcommands.ExitUsageError
	}
	if m.TargetPath == "" {
		m.oteLogger.LogErrorToFileAndConsole(ctx, "MultipartUpload usage error:", fmt.Errorf("target path must be provided"))
		return subcommands.ExitUsageError
	}
	if !strings.HasPrefix(m.TargetPath, "gs://") {
		m.oteLogger.LogErrorToFileAndConsole(ctx, "MultipartUpload usage error:", fmt.Errorf("target path must start with gs://"))
		return subcommands.ExitUsageError
	}
	splitPath := strings.SplitN(strings.TrimPrefix(m.TargetPath, "gs://"), "/", 2)
	if len(splitPath) != 2 {
		m.oteLogger.LogErrorToFileAndConsole(ctx, "MultipartUpload usage error:", fmt.Errorf("target path must contain a bucket and a path"))
		return subcommands.ExitUsageError
	}
	m.bucketName = splitPath[0]
	m.objectName = splitPath[1]

	bucketHandle, ok := storage.ConnectToBucket(ctx, &storage.ConnectParameters{
		BucketName:       m.bucketName,
		StorageClient:    m.client,
		VerifyConnection: true,
		UserAgent:        configuration.StorageAgentName(),
	})
	if !ok {
		m.oteLogger.LogErrorToFileAndConsole(ctx, "MultipartUpload failed:", fmt.Errorf("failed to connect to bucket %s", m.bucketName))
		m.oteLogger.LogUsageError(usagemetrics.MultipartUploadFailure)
		return subcommands.ExitFailure
	}
	m.bucketHandle = bucketHandle
	m.copier = io.Copy
	m.client = s.NewClient
	m.fileOpener = os.OpenFile
	exitStatus, err := m.multipartUploadHandler(ctx)
	if err != nil {
		m.oteLogger.LogErrorToFileAndConsole(ctx, "MultipartUpload failed:", err)
		m.oteLogger.LogUsageError(usagemetrics.MultipartUploadFailure)
	}
	return exitStatus
}

// multipartUploadHandler uploads a file to a GCS bucket using XML Multipart API.
func (m *MultipartUpload) multipartUploadHandler(ctx context.Context) (subcommands.ExitStatus, error) {
	m.oteLogger.LogMessageToFileAndConsole(ctx, "MultipartUpload starting")
	m.oteLogger.LogUsageAction(usagemetrics.MultipartUploadStarted)
	f, err := m.fileOpener(m.SourceFile, os.O_RDONLY, 0)
	if err != nil {
		return subcommands.ExitFailure, err
	}
	defer f.Close()
	fileInfo, err := f.Stat()
	if err != nil || fileInfo.Mode()&0444 == 0 {
		return subcommands.ExitFailure, fmt.Errorf("backup file does not have readable permissions: %v", err)
	}

	rw := storage.ReadWriter{
		Reader:              f,
		Copier:              m.copier,
		BucketHandle:        m.bucketHandle,
		BucketName:          m.bucketName,
		ChunkSizeMb:         100,
		ObjectName:          m.objectName,
		TotalBytes:          fileInfo.Size(),
		MaxRetries:          5,
		VerifyUpload:        true,
		XMLMultipartUpload:  true,
		XMLMultipartWorkers: m.ParallelThreads,
	}
	m.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Uploading file '%s' to '%s' with %d multipart workers", m.SourceFile, m.TargetPath, m.ParallelThreads))
	startTime := time.Now()
	bytesWritten, err := rw.Upload(ctx)
	uploadTime := time.Since(startTime)
	if err != nil {
		return subcommands.ExitFailure, fmt.Errorf("failed to upload file: %v", err)
	}
	log.CtxLogger(ctx).Infow("File uploaded", "bucket", m.bucketName, "fileName", m.TargetPath, "obj", m.objectName, "bytesWritten", bytesWritten, "fileSize", fileInfo.Size(), "uploadTimeSec", uploadTime.Round(time.Millisecond))
	m.oteLogger.LogMessageToFileAndConsole(ctx, "MultipartUpload: SUCCESS")
	m.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("\nDetailed logs are at %s", onetime.LogFilePath(m.Name(), m.IIOTEParams)))
	m.oteLogger.LogUsageAction(usagemetrics.MultipartUploadFinished)
	return subcommands.ExitSuccess, nil
}
