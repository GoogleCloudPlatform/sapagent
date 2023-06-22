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
	"fmt"
	"io"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

// BackintRFC3339Millis is a reference for timestamps to Backint specifications.
const BackintRFC3339Millis = "2006-01-02T15:04:05.999Z07:00"

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
func WriteSoftwareVersion(line string, output io.Writer) error {
	s := Split(line)
	if len(s) < 2 {
		return fmt.Errorf("malformed input line, got: %s, want: #SOFTWAREID <backint_version> <software_version>", line)
	}
	log.Logger.Infow("Version information", "backint", strings.Trim(s[1], `"`), configuration.AgentName, configuration.AgentVersion)
	output.Write([]byte(fmt.Sprintf(`#SOFTWAREID %s "Google %s %s"`, s[1], configuration.AgentName, configuration.AgentVersion) + "\n"))
	return nil
}
