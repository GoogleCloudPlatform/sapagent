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

// Package parse provides common parsing functions for Backint operations.
package parse

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// BackintRFC3339Millis is a reference for timestamps to Backint specifications.
const BackintRFC3339Millis = "2006-01-02T15:04:05.000Z07:00"

// Split performs a custom split on spaces based on the following SAP HANA Backint specifications:
//
// * Parameters may be quoted with double quotation marks ".
//
// * Parameters containing a space must be quoted with double quotation marks.
//
// * If a parameter contains a double quote, the double quote must be escaped with a backslash.
//
// * A backslash escapes double quotes only. A backslash in a parameter value must not be escaped.
func Split(s string) []string {
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

// WriteSoftwareVersion writes the Backint and agent software versions to the output.
// Returns the backint software version.
func WriteSoftwareVersion(line string, output io.Writer) (string, error) {
	s := Split(line)
	if len(s) < 2 {
		return "", fmt.Errorf("malformed input line, got: %s, want: #SOFTWAREID <backint_version> <software_version>", line)
	}
	backint := TrimAndClean(s[1])
	log.Logger.Infow("Version information", "backint", backint, configuration.AgentName, configuration.AgentVersion)
	output.Write([]byte(fmt.Sprintf(`#SOFTWAREID %q "Google %s %s"`, backint, configuration.AgentName, configuration.AgentVersion) + "\n"))
	return strings.Trim(backint, "backint "), nil
}

// TrimAndClean trims quotes, removes backslashes from escaped embedded quotes,
// and creates a raw literal string to preserve all special characters.
func TrimAndClean(str string) string {
	str = strings.Trim(str, `"`)
	str = strings.ReplaceAll(str, `\"`, `"`)
	return fmt.Sprintf(`%s`, str)
}

// RestoreFilename adds a preceding forward slash, fully escapes the string,
// then removes any escaped backslashes to conform to the specification.
func RestoreFilename(str string) string {
	str = fmt.Sprintf("%q", "/"+str)
	return strings.ReplaceAll(str, `\\`, `\`)
}

// CreateObjectPath combines the folder prefix, user id, file name, ebid,
// and extension to create the object name in the bucket. Optionally
// shorten the file name's long folder structure by taking the basepath and
// the preceding sub folder (in HANA systems this will be the tenant db).
func CreateObjectPath(config *bpb.BackintConfiguration, fileNameTrim, externalBackupID, extension string) string {
	if config.GetShortenFolderPath() && !strings.HasPrefix(fileNameTrim, fmt.Sprintf("/usr/sap/%s/SYS/global/hdb/backint", config.GetUserId())) {
		log.Logger.Infow("Unable to shorten folder path due to invalid file name prefix", "fileName", fileNameTrim, "want", fmt.Sprintf("/usr/sap/%s/SYS/global/hdb/backint", config.GetUserId()))
	} else if config.GetShortenFolderPath() {
		split := strings.Split(fileNameTrim, "/")
		if len(split) > 2 {
			// '/usr/sap/DEH/SYS/global/hdb/backint/SYSTEMDB/log_backup_0_0_0_0'
			// would shorten to: '/SYSTEMDB/log_backup_0_0_0_0'
			fileNameShort := "/" + split[len(split)-2] + "/" + split[len(split)-1]
			log.Logger.Infow("Shortening folder path", "old", fileNameTrim, "new", fileNameShort)
			fileNameTrim = fileNameShort
		}
	}
	return config.GetFolderPrefix() + config.GetUserId() + fileNameTrim + "/" + externalBackupID + extension
}

// OpenFileWithRetries will retry opening a file with an
// exponential backoff until the timeout.
func OpenFileWithRetries(name string, flag int, perm os.FileMode, timeoutMs int64) (*os.File, error) {
	if timeoutMs <= 0 {
		log.Logger.Warn("timeoutMs defaulted to 60000")
		timeoutMs = 60000
	}
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 500 * time.Millisecond
	bo.MaxInterval = 3 * time.Second
	bo.MaxElapsedTime = time.Duration(timeoutMs) * time.Millisecond

	var file *os.File
	err := backoff.Retry(func() error {
		var err error
		file, err = os.OpenFile(name, flag, perm)
		if err != nil && bo.GetElapsedTime().Milliseconds() < timeoutMs {
			log.Logger.Infow("Failed to open file, retrying.", "fileName", name, "elapsedTimeMs", bo.GetElapsedTime().Milliseconds(), "timeoutMs", timeoutMs, "err", err)
		}
		return err
	}, bo)
	if err != nil {
		log.Logger.Errorw("Timeout opening file", "fileName", name, "elapsedTimeMs", bo.GetElapsedTime().Milliseconds(), "timeoutMs", timeoutMs, "err", err)
	}
	return file, err
}

// CustomTime returns the custom time to be set in the metadata of the object.
// If custom_time is not set or if there is an error parsing the time, an empty time is returned.
// If custom_time is set to UTCNow, the current time is returned.
// If custom_time is set to UTCNow+<INT>d, the current time plus the number of days is returned.
// If custom_time is set to a time in RFC3339 format, the time is returned.
func CustomTime(ctx context.Context, customTime string, now time.Time) time.Time {
	if customTime == "" {
		return time.Time{}
	}

	if customTime == "UTCNow" {
		return now
	}

	if strings.HasPrefix(customTime, "UTCNow") {
		// Regex to capture the number and the unit (d for days).
		re := regexp.MustCompile(`UTCNow\+(\d+)(d)`)
		matches := re.FindStringSubmatch(customTime)

		if len(matches) != 3 {
			log.Logger.Warnw("Could not parse custom_time field. Duration should be in the format of UTCNow+<INT>d(ex: UTCNow+1d). CustomTime field will not be set.", "customTime", customTime, "Expected Format", "UTCNow+<INT>d")
			return time.Time{}
		}
		numDays, err := strconv.Atoi(matches[1])
		if err != nil {
			log.Logger.Warnw("Could not parse custom_time field. Duration should be in the format of UTCNow+<INT>d(ex: UTCNow+1d). CustomTime feild will not be set.", "customTime", customTime, "Expected Format", "UTCNow+<INT>d", "err", err)
			return time.Time{}
		}
		// Add the specified number of days.
		return now.AddDate(0, 0, numDays)
	}

	ct, err := time.Parse(time.RFC3339, customTime)
	if err != nil {
		log.Logger.Warnw("Could not parse custom_time field. CustomTime feild will not be set.", "err", err, "customTime", customTime, "Expected Format", time.RFC3339)
	}
	return ct
}
