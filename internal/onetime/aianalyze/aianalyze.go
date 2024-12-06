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

// Package aianalyze implements one time execution mode for
// aianalyze. This is used to analyze logs using genAI
// and provide insights to the support team.
package aianalyze

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"flag"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/rest"

	cpb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// GenerateContentResponse is the response for streamGenerateContent which mirrors the proto.
	// Mirroring the proto is necessary for unmarshalling the response.
	// Original struct: GoogleCloudAiplatformV1GenerateContentResponse,
	// which can be found at //third_party/golang/google_api/aiplatform/v1/aiplatform-gen.go
	generateContentResponse struct {
		Candidates    []candidate    `json:"candidates"`
		ModelVersion  string         `json:"modelVersion"`
		UsageMetadata *usageMetadata `json:"usageMetadata,omitempty"` // Optional field
	}

	candidate struct {
		Content       content        `json:"content"`
		SafetyRatings []safetyRating `json:"safetyRatings,omitempty"` // Optional field
		FinishReason  string         `json:"finishReason,omitempty"`  // Optional field
	}

	content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}

	part struct {
		Text string `json:"text"`
	}

	safetyRating struct {
		Category    string `json:"category"`
		Probability string `json:"probability"`

		ProbabilityScore float32 `json:"probabilityScore"`
		Severity         string  `json:"severity"`
		SeverityScore    float32 `json:"severityScore"`
	}

	usageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	}

	// AiAnalyzer has args for ai analyze collection one time mode.
	AiAnalyzer struct {
		Sid               string                        `json:"sid"`
		InstanceNumber    string                        `json:"instance-number"`
		InstanceName      string                        `json:"instance-name"`
		SupportBundlePath string                        `json:"support-bundle-path"`
		EventOverview     bool                          `json:"event-overview"`
		EventAnalysis     bool                          `json:"event-analysis"`
		BeforeEventWindow int                           `json:"before-event-window"`
		AfterEventWindow  int                           `json:"after-event-window"`
		OutputPath        string                        `json:"output-path"`
		Timestamp         string                        `json:"timestamp"`
		Help              bool                          `json:"help,string"`
		LogPath           string                        `json:"log-path"`
		LogLevel          string                        `json:"loglevel"`
		IIOTEParams       *onetime.InternallyInvokedOTE `json:"-"`
		project           string
		region            string
		logparserPath     string
		httpGet           httpGet
		exec              commandlineexecutor.Execute
		oteLogger         *onetime.OTELogger
		fs                filesystem.FileSystem
		rest              RestService
	}

	// RestService is the interface for rest.Rest.
	RestService interface {
		NewRest()
		GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error)
	}

	httpClient interface {
		Do(req *http.Request) (*http.Response, error)
	}

	// httpGet abstracts the http.Get function for testing purposes.
	httpGet func(url string) (*http.Response, error)
)

var (
	//go:embed guides/analysis_guide.txt
	analysisGuide string

	//go:embed guides/decision_tree.txt
	decisionTree string
)

// Name implements the subcommand interface for collecting support analyzer report collection for support team.
func (*AiAnalyzer) Name() string {
	return "aianalyze"
}

// Synopsis implements the subcommand interface for support analyzer report collection for support team.
func (*AiAnalyzer) Synopsis() string {
	return "analyze logs and provide insights to the support team"
}

// Usage implements the subcommand interface for support analyzer report collection for support team.
func (*AiAnalyzer) Usage() string {
	return `Usage: aianalyze [-sid=<sid>] [-event-overview] [-event-analysis]
	[-instance-number=<instance-number>] [-instance-name=<instance-name>] [-support-bundle-path=<support-bundle-path>]
	[-before-event-window=<before-event-window>] [-after-event-window=<after-event-window>]
	[-timestamp=<timestamp>] [-bucket=<bucket>]
	[-h] [-loglevel=<debug|info|warn|error>]
	Example: aianalyze -sid="DEH"` + "\n"
}

// SetFlags implements the subcommand interface for support analyzer report collection.
func (a *AiAnalyzer) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&a.Sid, "sid", "", "SAP System Identifier - (required for event analysis)")
	fs.StringVar(&a.InstanceNumber, "instance-number", "", "SAP Instance Number - (required for event analysis)")
	fs.StringVar(&a.InstanceName, "instance-name", "", "SAP Instance Name - (required for event analysis)")
	fs.StringVar(&a.SupportBundlePath, "support-bundle-path", "", "Path to the support bundle. (required)")
	fs.BoolVar(&a.EventOverview, "event-overview", false, "Get an overview of the logs for event analysis. (optional) Default: false")
	fs.BoolVar(&a.EventAnalysis, "event-analysis", false, "Analyze the logs for event analysis. (optional) Default: false")
	fs.IntVar(&a.BeforeEventWindow, "before-event-window", 4500, "Window before event of concern for further analysis. (optional) Default: 4500")
	fs.IntVar(&a.AfterEventWindow, "after-event-window", 1800, "Window after event of concern for further analysis. (optional) Default: 1800")
	fs.StringVar(&a.OutputPath, "output-path", "", "Path to the output file. (optional) Default: /var/log/google-cloud-sap-agent/aianalyze-<overview|analysis>-<support-bundle-name>-<current-time>.txt")
	fs.StringVar(&a.Timestamp, "timestamp", "", "Timestamp of the event of concern. (optional, but required for event analysis)")
	fs.BoolVar(&a.Help, "h", false, "Displays help")
	fs.StringVar(&a.LogLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&a.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/supportbundle.log")
}

// Execute implements the subcommand interface for support analyzer report collection.
func (a *AiAnalyzer) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     a.Name(),
		Help:     a.Help,
		LogLevel: a.LogLevel,
		LogPath:  a.LogPath,
		Fs:       f,
		IIOTE:    a.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	_, exitStatus = a.Run(ctx, onetime.CreateRunOptions(cp, false))
	return exitStatus
}

func (a *AiAnalyzer) Run(ctx context.Context, opts *onetime.RunOptions) (string, subcommands.ExitStatus) {
	a.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	log.CtxLogger(ctx).Info("Running support analyzer")

	if err := a.validateParameters(ctx); err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while validating parameters", err)
		return "Error while validating parameters", subcommands.ExitUsageError
	}
	if err := a.setDefaults(ctx, opts.CloudProperties); err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while setting defaults", err)
		return "Error while setting defaults", subcommands.ExitFailure
	}

	return a.supportAnalyzerHandler(ctx, opts)
}

func (a *AiAnalyzer) supportAnalyzerHandler(ctx context.Context, opts *onetime.RunOptions) (string, subcommands.ExitStatus) {
	a.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting Support Analyzer Report for Agent for SAP...")

	if a.EventOverview {
		if err := a.getOverview(ctx); err != nil {
			a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while getting overview", err)
			return "Error while getting overview", subcommands.ExitFailure
		}
	} else {
		if err := a.getAnalysis(ctx); err != nil {
			a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while getting analysis", err)
			return "Error while getting analysis", subcommands.ExitFailure
		}
	}
	return "Support Analyzer Report collected successfully", subcommands.ExitSuccess
}

// protosToJSONList converts a list of protos to a JSON list.
// This is necessary for passing the protos to the REST API.
func protosToJSONList(ctx context.Context, contentList []*cpb.Content) ([]byte, error) {
	jsonList := make([]json.RawMessage, len(contentList))

	for i, content := range contentList {
		jsonString, err := protojson.Marshal(content)
		if err != nil {
			return nil, fmt.Errorf("error converting proto to JSON: %w", err)
		}
		jsonList[i] = json.RawMessage(jsonString)
	}

	jsonMap := map[string]any{"contents": jsonList}
	data, err := json.Marshal(jsonMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json, err: %w", err)
	}
	log.CtxLogger(ctx).Debugw("protosToJSONList", "data", string(data))
	return data, nil
}

// getOverview calls the streamGenerateContent REST API for Vertex API
// and gets an overview of the logs for event analysis.
func (a *AiAnalyzer) getOverview(ctx context.Context) (err error) {
	if err := a.downloadLogparser(ctx, a.httpGet); err != nil {
		return fmt.Errorf("error while downloading logparser: %w", err)
	}
	parsedLogs, err := a.runLogParser(ctx)
	if err != nil {
		return fmt.Errorf("error while running log parser: %w", err)
	}

	prompt := fmt.Sprintf("Analyze the logs: %s", parsedLogs)

	content := cpb.Content{
		Role:  "user",
		Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: prompt}}},
	}
	body, err := protosToJSONList(ctx, []*cpb.Content{&content})
	if err != nil {
		return fmt.Errorf("error while converting protos to JSON: %w", err)
	}

	var overview []generateContentResponse
	if overview, err = a.generateContentREST(ctx, body); err != nil {
		return fmt.Errorf("error while generating content REST: %w", err)
	}

	var promptResponse string
	for _, resp := range overview {
		for _, candidate := range resp.Candidates {
			for _, part := range candidate.Content.Parts {
				promptResponse = promptResponse + part.Text
			}
		}
	}

	log.CtxLogger(ctx).Infow("getOverview", "promptResponse", promptResponse)
	if err := a.savePromptResponse(ctx, promptResponse); err != nil {
		return fmt.Errorf("error while saving prompt response: %w", err)
	}

	a.oteLogger.LogMessageToFileAndConsole(ctx, "Overview complete.")
	a.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Overview saved to file: %s", a.OutputPath))
	return nil
}

func (a *AiAnalyzer) getAnalysis(ctx context.Context) (err error) {
	categories := ``

	var pacemakerLogs, nameserverLogs string
	if pacemakerLogs, err = a.fetchPacemakerLogs(ctx); err != nil {
		return fmt.Errorf("error while fetching pacemaker logs: %w", err)
	}
	if nameserverLogs, err = a.fetchNameserverTraces(ctx); err != nil {
		return fmt.Errorf("error while fetching nameserver traces: %w", err)
	}

	prompt := fmt.Sprintf("Categories: %s\n\n", categories) +
		fmt.Sprintf("Decision tree: %s\n\n", decisionTree) +
		fmt.Sprintf("Reference document:\n%s\n\n", analysisGuide) +
		fmt.Sprintf("Analyze the logs: \n: %s\n\n%s", pacemakerLogs, nameserverLogs)

	content := cpb.Content{
		Role:  "user",
		Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: prompt}}},
	}
	body, err := protosToJSONList(ctx, []*cpb.Content{&content})
	if err != nil {
		return err
	}

	var analysis []generateContentResponse
	if analysis, err = a.generateContentREST(ctx, body); err != nil {
		return fmt.Errorf("error while generating content REST: %w", err)
	}

	var promptResponse string
	for _, resp := range analysis {
		for _, candidate := range resp.Candidates {
			for _, part := range candidate.Content.Parts {
				promptResponse = promptResponse + part.Text
			}
		}
	}

	log.CtxLogger(ctx).Infow("getAnalysis", "promptResponse", promptResponse)
	if err := a.savePromptResponse(ctx, promptResponse); err != nil {
		return fmt.Errorf("error while saving prompt response: %w", err)
	}

	a.oteLogger.LogMessageToFileAndConsole(ctx, "Analysis complete.")
	a.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Analysis saved to file: %s", a.OutputPath))
	return nil
}

// generateContentREST calls the streamGenerateContent REST API for Vertex API and returns the response.
func (a *AiAnalyzer) generateContentREST(ctx context.Context, data []byte) ([]generateContentResponse, error) {
	model := "gemini-1.5-pro-001"
	baseURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent", a.region, a.project, a.region, model)
	bodyBytes, err := a.rest.GetResponse(ctx, "POST", baseURL, data)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while getting response", "err", err)
		return nil, err
	}

	log.CtxLogger(ctx).Debugw("generateContentREST", "bodyBytes", string(bodyBytes))
	var content []generateContentResponse
	if err := json.Unmarshal(bodyBytes, &content); err != nil {
		log.CtxLogger(ctx).Errorw("Error while unmarshalling response", "err", err)
		return nil, err
	}

	return content, nil
}

// validateParameters validates the set of parameters for support analyzer collection.
func (a *AiAnalyzer) validateParameters(ctx context.Context) error {
	switch {
	case a.EventOverview:
		switch {
		case a.SupportBundlePath == "":
			return fmt.Errorf("no support bundle path provided, -support-bundle-path is required")
		}
	case a.EventAnalysis:
		switch {
		case a.Sid == "":
			return fmt.Errorf("no SID passed, SID is required")
		case a.InstanceNumber == "":
			return fmt.Errorf("no instance number passed, instance number is required")
		case a.SupportBundlePath == "":
			return fmt.Errorf("no support bundle path provided, -support-bundle-path is required")
		case a.Timestamp == "":
			return fmt.Errorf("no timestamp passed, timestamp is required")
		}
	default:
		return fmt.Errorf("no event analysis or event overview passed, -event-overview or -event-analysis is required")
	}

	return nil
}

// setDefaults sets the default values for the parameters if they are not provided.
func (a *AiAnalyzer) setDefaults(ctx context.Context, cp *ipb.CloudProperties) error {
	if a.InstanceName == "" {
		a.InstanceName = cp.GetInstanceName()
	}
	a.rest = &rest.Rest{}
	a.rest.NewRest()

	a.fs = filesystem.Helper{}
	a.logparserPath = "/tmp/logparser.py"

	a.exec = commandlineexecutor.ExecuteCommand
	a.httpGet = http.Get

	a.project = cp.GetProjectId()
	zone := cp.GetZone()
	parts := strings.Split(zone, "-")
	if len(parts) < 3 {
		return fmt.Errorf("invalid zone, cannot fetch region from it: %s", zone)
	}
	a.region = strings.Join(parts[:len(parts)-1], "-")

	if a.OutputPath == "" {
		timestamp := time.Now().UTC().UnixMilli()
		if a.EventOverview {
			a.OutputPath = fmt.Sprintf("/var/log/google-cloud-sap-agent/aianalyze-overview-%s-%s.txt", filepath.Base(a.SupportBundlePath), fmt.Sprintf("%d", timestamp))
		} else {
			a.OutputPath = fmt.Sprintf("/var/log/google-cloud-sap-agent/aianalyze-analysis-%s-%s.txt", filepath.Base(a.SupportBundlePath), fmt.Sprintf("%d", timestamp))
		}
	}

	return nil
}
