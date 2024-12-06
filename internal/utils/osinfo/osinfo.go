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

// Package osinfo contains the OS related utils for the agent.
package osinfo

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/zieckey/goini"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

type (
	// Data contains the OS related data.
	Data struct {
		OSName    string
		OSVendor  string
		OSVersion string
	}

	// FileReadCloser is a function which reads an input file and returns the io.ReadCloser. It is
	// created to facilitate testing.
	FileReadCloser func(string) (io.ReadCloser, error)
)

// ReadData returns the OS related information. In case of windows, the vendor Id and version are
// empty.
func ReadData(ctx context.Context, rc FileReadCloser, releaseInfoFile string) (Data, error) {
	res := Data{}
	res.OSName = runtime.GOOS
	if res.OSName == "windows" {
		log.CtxLogger(ctx).Info("Reading OS data for windows is not supported")
		return res, nil
	}
	var err error
	res.OSVendor, res.OSVersion, err = readReleaseInfo(ctx, rc, releaseInfoFile)
	if err != nil {
		log.CtxLogger(ctx).Errorw("error while reading release OS info", "error", err.Error())
	}
	return res, err
}

// readReleaseInfo parses the OS release file and retrieves the values for the
// osVendorID and osVersion.
func readReleaseInfo(ctx context.Context, cfr FileReadCloser, osReleaseFilePath string) (osVendorID, osVersion string, err error) {
	if cfr == nil || osReleaseFilePath == "" {
		return "", "", fmt.Errorf("both ConfigFileReader and OSReleaseFilePath must be set")
	}

	file, err := cfr(osReleaseFilePath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	ini := goini.New()
	if err := ini.ParseFrom(file, "\n", "="); err != nil {
		return "", "", fmt.Errorf("failed to parse %s: %v", osReleaseFilePath, err)
	}

	id, ok := ini.Get("ID")
	if !ok {
		log.CtxLogger(ctx).Warn(fmt.Sprintf("Could not read ID from %s", osReleaseFilePath))
		id = ""
	}
	osVendorID = strings.ReplaceAll(strings.TrimSpace(id), `"`, "")

	version, ok := ini.Get("VERSION")
	if !ok {
		log.CtxLogger(ctx).Warn(fmt.Sprintf("Could not read VERSION from %s", osReleaseFilePath))
		version = ""
	}
	if vf := strings.Fields(version); len(vf) > 0 {
		osVersion = strings.ReplaceAll(strings.TrimSpace(vf[0]), `"`, "")
	}

	return osVendorID, osVersion, nil
}
