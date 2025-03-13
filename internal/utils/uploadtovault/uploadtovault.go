/*
Copyright 2025 Google LLC

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

// Package uploadtovault contains util methods to upload files to GCS bucket using an OAuth token.
package uploadtovault

import (
	"context"
	"errors"
	"io"

	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/secret"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/storage"
)

type (
	// Params struct contains the parameters required for the uploadFiles method.
	Params struct {
		FilesPath  []string
		FS         filesystem.FileSystem
		RW         readerWriter
		BucketName string
		OAuthToken secret.String
		ctb        storage.BucketConnector
	}

	// uploader interface provides abstraction for ease of testing.
	uploader interface {
		Upload(ctx context.Context) (int64, error)
	}

	// readerWriter is a function to get the reader writer for uploading the file.
	readerWriter func(rw storage.ReadWriter) uploader
)

func uploadFiles(ctx context.Context, p Params) error {
	cp := storage.ConnectParameters{
		BucketName:       p.BucketName,
		OAuthToken:       p.OAuthToken,
		VerifyConnection: true,
	}
	bucketHandle, ok := p.ctb(ctx, &cp)
	if !ok {
		err := errors.New("error establishing connection to bucket, please check the logs")
		return err
	}
	for _, filePath := range p.FilesPath {
		f, err := p.FS.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()
		fileInfo, err := p.FS.Stat(filePath)
		if err != nil {
			return err
		}
		readWriter := storage.ReadWriter{
			Reader:       f,
			BucketHandle: bucketHandle,
			Copier:       io.Copy,
			BucketName:   p.BucketName,
			ObjectName:   filePath,
			TotalBytes:   fileInfo.Size(),
			VerifyUpload: true,
		}
		rw := p.RW(readWriter)
		var bytesWritten int64
		if bytesWritten, err = rw.Upload(ctx); err != nil {
			return err
		}
		log.CtxLogger(ctx).Infow("File uploaded", "filePath", filePath, "fileSize", fileInfo.Size(), "bucket", p.BucketName, "bytesWritten", bytesWritten)
	}
	return nil
}
