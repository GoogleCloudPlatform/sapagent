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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"flag"
	"google.golang.org/api/googleapi"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/rest"

	cpb "cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
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
		Sid          string                        `json:"sid"`
		InstanceName string                        `json:"instance-name"`
		Project      string                        `json:"project"`
		Region       string                        `json:"region"`
		Help         bool                          `json:"help,string"`
		LogPath      string                        `json:"log-path"`
		LogLevel     string                        `json:"loglevel"`
		IIOTEParams  *onetime.InternallyInvokedOTE `json:"-"`
		oteLogger    *onetime.OTELogger
		rest         RestService
	}

	// RestService is the interface for rest.Rest.
	RestService interface {
		NewRest()
		GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error)
	}

	errorResponse struct {
		Err googleapi.Error `json:"error"`
	}

	httpClient interface {
		Do(req *http.Request) (*http.Response, error)
	}
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
	return `Usage: aianalyze [-sid=<SAP System Identifier>] [-h] [-loglevel=<debug|info|warn|error>]
	Example: aianalyze -sid="DEH"` + "\n"
}

// SetFlags implements the subcommand interface for support analyzer report collection.
func (a *AiAnalyzer) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&a.Sid, "sid", "", "SAP System Identifier - required for collecting HANA traces")
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

	return a.supportAnalyzerHandler(ctx, opts)
}

func (a *AiAnalyzer) supportAnalyzerHandler(ctx context.Context, opts *onetime.RunOptions) (string, subcommands.ExitStatus) {
	if err := a.validateParameters(ctx, opts.CloudProperties); err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while validating parameters", err)
		return "Error while validating parameters", subcommands.ExitUsageError
	}
	a.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting Support Analyzer Report for Agent for SAP...")

	if err := a.getOverview(ctx); err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while getting overview", err)
		return "Error while getting overview", subcommands.ExitFailure
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
	log.CtxLogger(ctx).Infow("protosToJSONList", "data", string(data))
	return data, nil
}

// getOverview calls the streamGenerateContent REST API for Vertex API
// and gets an overview of the logs for event analysis.
func (a *AiAnalyzer) getOverview(ctx context.Context) (err error) {
	content := cpb.Content{
		Role:  "user",
		Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: "What is 4+4?"}}},
	}
	body, err := protosToJSONList(ctx, []*cpb.Content{&content})
	if err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while converting protos to JSON", err)
		return err
	}

	var overview []generateContentResponse
	if overview, err = a.generateContentREST(ctx, body); err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while generating content REST", err)
		return err
	}
	log.CtxLogger(ctx).Infow("getOverview", "overview", overview)
	return nil
}

// generateContentREST calls the streamGenerateContent REST API for Vertex API and returns the response.
func (a *AiAnalyzer) generateContentREST(ctx context.Context, data []byte) ([]generateContentResponse, error) {
	model := "gemini-1.5-pro-001"
	baseURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent", a.Region, a.Project, a.Region, model)
	bodyBytes, err := a.rest.GetResponse(ctx, "POST", baseURL, data)
	if err != nil {
		a.oteLogger.LogErrorToFileAndConsole(ctx, "Error while getting response", err)
		return nil, err
	}

	log.CtxLogger(ctx).Infow("generateContentREST", "bodyBytes", string(bodyBytes))
	var content []generateContentResponse
	if err := json.Unmarshal(bodyBytes, &content); err != nil {
		log.CtxLogger(ctx).Errorw("Error while unmarshalling response", "err", err)
		return nil, err
	}

	return content, nil
}

// validateParameters validates the parameters for support analyzer collection.
// It also sets the default values for the parameters if they are not provided.
func (a *AiAnalyzer) validateParameters(ctx context.Context, cp *ipb.CloudProperties) error {
	a.rest = &rest.Rest{}
	a.rest.NewRest()
	if a.Sid == "" {
		return fmt.Errorf("no SID passed, SID is required")
	}

	if a.Project == "" {
		a.Project = cp.GetProjectId()
	}
	zone := cp.GetZone()
	if a.Region == "" {
		parts := strings.Split(zone, "-")
		if len(parts) < 3 {
			return fmt.Errorf("invalid zone, cannot fetch region from it: %s", zone)
		}
		a.Region = strings.Join(parts[:len(parts)-1], "-")
	}

	return nil
}
