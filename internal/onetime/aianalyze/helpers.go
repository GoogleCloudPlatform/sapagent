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

package aianalyze

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// extractTimeWindowFromText extracts logs within
// the given time window from the text. This is used for
// extracting logs from
// - either pacemaker logs, or
// - nameserver traces.
func (a *AiAnalyzer) extractTimeWindowFromText(ctx context.Context, text string, isPacemaker bool) (string, error) {
	var timestampFormat, timestampWithoutMicrosecondsFormat string
	var logPattern *regexp.Regexp

	if isPacemaker {
		timestampFormat = "2006-01-02 15:04:05.000"
		timestampWithoutMicrosecondsFormat = "2006-01-02 15:04:05"
		// Example: "Jan 02 15:04:05.000"
		logPattern = regexp.MustCompile(`(\w{3} \d{2} \d{2}:\d{2}:\d{2}\.\d{3})`)
	} else {
		timestampFormat = "2006-01-02 15:04:05.999999"
		timestampWithoutMicrosecondsFormat = "2006-01-02 15:04:05"
		// Example: [31641]{-1}[-1/-1] 2024-07-18 20:30:31.545848
		logPattern = regexp.MustCompile(`\[\d+\]\{-?\d+}\[-?\d+.*? (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d{6})?)`)
	}

	var targetTime, logTime time.Time
	var err error
	targetTime, err = time.Parse(timestampFormat, a.Timestamp)
	if err != nil {
		targetTime, err = time.Parse(timestampWithoutMicrosecondsFormat, a.Timestamp)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Error parsing target timestamp", "error", err)
			return "", err
		}
	}

	startTime := targetTime.Add(-time.Duration(a.BeforeEventWindow) * time.Second)
	endTime := targetTime.Add(time.Duration(a.AfterEventWindow) * time.Second)

	windowStart := -1
	windowEnd := -1
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		match := logPattern.FindStringSubmatch(line)
		if len(match) > 0 {
			if isPacemaker {
				logTimeStr := match[0]
				logTime, err = time.Parse("Jan 2 15:04:05.000", logTimeStr)
				if err != nil {
					log.CtxLogger(ctx).Errorw("Error parsing log timestamp", "line", line, "error", err)
					continue
				}
				logTime = logTime.AddDate(targetTime.Year(), 0, 0)
			} else {
				logTimeStr := match[1]
				logTime, err = time.Parse(timestampFormat, logTimeStr)
				if err != nil {
					logTime, err = time.Parse(timestampWithoutMicrosecondsFormat, logTimeStr)
					if err != nil {
						log.CtxLogger(ctx).Errorw("Error parsing log timestamp", "line", line, "error", err)
						continue
					}
				}
			}

			if logTime.Before(startTime) {
				continue
			} else if logTime.After(endTime) {
				break
			} else {
				if windowStart == -1 {
					windowStart = i
				}
				windowEnd = i
			}
		} else {
			if windowStart != -1 {
				windowEnd = i
			}
		}
	}

	if windowStart != -1 {
		return strings.Join(lines[windowStart:windowEnd+1], "\n"), nil
	} else {
		log.CtxLogger(ctx).Warn("No logs found within the time window")
		return "", nil
	}
}

// fetchPacemakerLogs fetches the filtered pacemaker logs required for event analysis.
func (a *AiAnalyzer) fetchPacemakerLogs(ctx context.Context) (string, error) {
	logPath := path.Join(a.SupportBundlePath, "/var/log/pacemaker/pacemaker.log")
	logBytes, err := a.fs.ReadFile(logPath)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while reading pacemaker log", "error", err)
		return "", err
	}
	data := string(logBytes)
	return a.extractTimeWindowFromText(ctx, data, true)
}

// fetchNameserverTraces fetches the filtered nameserver traces required for event analysis.
func (a *AiAnalyzer) fetchNameserverTraces(ctx context.Context) (string, error) {
	traceDirPath := path.Join(a.SupportBundlePath, fmt.Sprintf("usr/sap/%s/HDB%s/%s/trace", a.Sid, a.InstanceNumber, a.InstanceName))
	files, err := a.fs.ReadDir(traceDirPath)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while reading support bundle directory", "error", err)
		return "", fmt.Errorf("error while reading support bundle directory: %w", err)
	}

	var relevantLines string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if filterNameserverTraces(ctx, file.Name()) {
			content, err := a.fs.ReadFile(path.Join(traceDirPath, file.Name()))
			if err != nil {
				log.CtxLogger(ctx).Errorw("Error while reading nameserver trace", "error", err)
				return "", fmt.Errorf("error while reading nameserver trace: %w", err)
			}

			filteredContent, err := a.extractTimeWindowFromText(ctx, string(content), false)
			if err != nil {
				log.CtxLogger(ctx).Errorw("Error while extracting time window from text", "error", err)
				return "", fmt.Errorf("error while extracting time window from text: %w", err)
			} else if filteredContent != "" {
				relevantLines += filteredContent + "\n"
			}
		}
	}

	return relevantLines, nil
}

// filterNameserverTraces filters the nameserver traces required for event analysis.
func filterNameserverTraces(ctx context.Context, fileName string) bool {
	if !strings.HasPrefix(fileName, "nameserver_") || !strings.HasSuffix(fileName, ".trc") {
		return false
	}

	parts := strings.Split(fileName, ".")
	if len(parts) <= 3 {
		return false
	}
	if _, err := strconv.Atoi(parts[len(parts)-3]); err != nil {
		return false
	}

	for _, part := range parts {
		if strings.Contains(part, "00000") {
			return false
		}
	}

	return true
}

// downloadLogparser downloads the publicly available logparser.py script
// from the GoogleCloudPlatform/pacemaker-log-parser-for-sap-on-gcp repository.
func (a *AiAnalyzer) downloadLogparser(ctx context.Context, httpGet httpGet) error {
	if _, err := a.fs.Stat(a.logparserPath); err != nil {
		if !os.IsExist(err) {
			return err
		}
		log.CtxLogger(ctx).Info("Logparser.py script already exists at ", a.logparserPath)
		return nil
	}

	outFile, err := a.fs.OpenFile(a.logparserPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while creating logparser file", "error", err)
		return fmt.Errorf("error while creating logparser file: %w", err)
	}
	defer outFile.Close()

	fileURL := "https://raw.githubusercontent.com/GoogleCloudPlatform/pacemaker-log-parser-for-sap-on-gcp/refs/heads/main/logparser.py"
	resp, err := httpGet(fileURL)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while getting logparser file", "error", err)
		return fmt.Errorf("error while getting logparser file: %w", err)
	}
	defer resp.Body.Close()

	_, err = a.fs.Copy(outFile, resp.Body)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while writing logparser file", "error", err)
		return fmt.Errorf("error while writing logparser file: %w", err)
	}

	log.CtxLogger(ctx).Info("Successfully downloaded logparser.py script at ", a.logparserPath)
	return nil
}

// runLogParser runs the logparser.py script to parse the messages and pacemaker logs
// and returns the output.
func (a *AiAnalyzer) runLogParser(ctx context.Context) (string, error) {
	messagesPath := path.Join(a.SupportBundlePath, fmt.Sprintf("%s_VAR_LOG_MESSAGES.txt", a.InstanceName))
	pacemakerPath := path.Join(a.SupportBundlePath, "var/log/pacemaker/pacemaker.log")
	tempOutputPath := fmt.Sprintf("/tmp/logparser_output-%s.txt", strings.Replace(time.Now().Format(time.RFC3339), ":", "-", -1))

	cmd := "sudo"
	args := []string{
		fmt.Sprintf("python3 %s -s %s -p %s -o %s", a.logparserPath, messagesPath, pacemakerPath, tempOutputPath),
	}
	res := a.exec(ctx, commandlineexecutor.Params{
		Executable:  cmd,
		ArgsToSplit: strings.Join(args, " "),
	})
	if res.ExitCode != 0 || strings.Contains(res.StdErr, "error") || strings.Contains(res.StdErr, "Cannot find/open/read file") {
		log.CtxLogger(ctx).Errorw("Error while running logparser.py script", "err", errors.New(res.StdErr))
		return "", errors.New(res.StdErr)
	}

	parsedLogs, err := a.fs.ReadFile(tempOutputPath)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while reading logparser output file", "error", err)
		return "", err
	}
	if err := a.fs.RemoveAll(tempOutputPath); err != nil {
		log.CtxLogger(ctx).Errorw("Error while deleting logparser output file", "error", err)
		return "", err
	}
	return string(parsedLogs), nil
}

func (a *AiAnalyzer) savePromptResponse(ctx context.Context, response string) error {
	responseFile, err := a.fs.OpenFile(a.OutputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while creating output file to save aianalyze response", "error", err)
		return fmt.Errorf("error while creating output file to save aianalyze response: %w", err)
	}
	defer responseFile.Close()

	_, err = a.fs.Copy(responseFile, strings.NewReader(response))
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while writing to output file to save aianalyze response", "error", err)
		return fmt.Errorf("error while writing to output file to save aianalyze response: %w", err)
	}
	return nil
}
