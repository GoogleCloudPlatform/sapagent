/*
Copyright 2022 Google LLC

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

package netweaver

// Tool 'dpmon' stands for Dispatch Monitoring is a command line tool by SAP.
// DPMON helps get overview of ABAP work processes, queues from operating system.
// Reference: https://help.sap.com/doc/saphelp_nw74/7.4.16/en-US/19/28d51a81c748b399947f3e354d2ffb/content.htm?no_cache=true

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

var (
	emptyChars           = regexp.MustCompile(`[\s\t\n\r]`)
	sessionTableHeader   = regexp.MustCompile(`^\|\s*Logon-Type`)
	validSessionTableRow = regexp.MustCompile(`^\|\s*[A-Za-z0-9_]+`)
	validRFCTableRow     = regexp.MustCompile(`^\|\s*[0-9]+`)
)

// parseABAPSessionStats parses text output of 'dpmon v' command for ABAP session stats.
// Returns:
//   - sessionCount - A map with key->logon_type of session and value->session_count.
//   - totalCount - total number of sessions.
//   - error in case of failures, nil otherwise.
func parseABAPSessionStats(text string) (sessionCount map[string]int, totalCount int, err error) {
	var (
		abapSessionsTable [][]string
		numberOfColumns   = 15
		sessionColumn     = 1
	)
	// Parse the textual output to a 2D array/table of strings.
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if sessionTableHeader.MatchString(line) {
			// Ignore the header line.
			continue
		}
		if validSessionTableRow.MatchString(line) {
			line = emptyChars.ReplaceAllString(line, "")
			abapSessionsTable = append(abapSessionsTable, strings.Split(line, "|"))
		}
	}
	log.Logger.Debugf("ABAP sessions table: %q.", abapSessionsTable)

	sessionCount = make(map[string]int)
	for _, row := range abapSessionsTable {
		log.Logger.Debugf("Processing row %q of length: %d.", row, len(row))
		if len(row) == numberOfColumns {
			sessionCount[row[sessionColumn]]++
			totalCount++
		}
	}
	if totalCount == 0 {
		return nil, 0, fmt.Errorf("no active sessions found")
	}
	return sessionCount, totalCount, nil
}

// parseRFCStats parses the output of 'dpmon c' command to come up with RFC Connection stats.
// Returns:
// rfcStateCount - a map with key->{rfcType}_{state} and value->RFC_count.
// Two additional keys "server" and "client" are added with values as the
// conection count of respective type. All keys are in lowercase letters.
func parseRFCStats(text string) (rfcStateCount map[string]int) {
	var (
		rfcConnectionsTable [][]string
		numberOfColumns     = 11
		rfcTypeColumn       = 6
		rfcStateColumn      = 5
	)

	// Parse the textual output to a 2D array/table of strings.
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if validRFCTableRow.MatchString(line) {
			line = emptyChars.ReplaceAllString(line, "")
			rfcConnectionsTable = append(rfcConnectionsTable, strings.Split(line, "|"))
		}
	}

	rfcStateCount = make(map[string]int)
	for _, row := range rfcConnectionsTable {
		log.Logger.Debugf("Processing RFC table row: %q of length: %d", row, len(row))
		if len(row) != numberOfColumns {
			continue
		}

		rfcTypeWithState := strings.ToLower(row[rfcTypeColumn]) + "_" + strings.ToLower(row[rfcStateColumn])
		rfcStateCount[rfcTypeWithState]++

		if strings.ToUpper(row[rfcTypeColumn]) == "SERVER" {
			rfcStateCount["server"]++
		} else if strings.ToUpper(row[rfcTypeColumn]) == "CLIENT" {
			rfcStateCount["client"]++
		}
	}
	return rfcStateCount
}
