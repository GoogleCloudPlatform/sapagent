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

// Package usagemetrics provides logging utility for the operational status of the GC SAP Agent.
package usagemetrics

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// Status enumerates the supported usage logging statuses.
type Status string

// The following status values are supported.
const (
	StatusRunning       Status = "RUNNING"
	StatusStarted       Status = "STARTED"
	StatusStopped       Status = "STOPPED"
	StatusConfigured    Status = "CONFIGURED"
	StatusMisconfigured Status = "MISCONFIGURED"
	StatusError         Status = "ERROR"
	StatusInstalled     Status = "INSTALLED"
	StatusUpdated       Status = "UPDATED"
	StatusUninstalled   Status = "UNINSTALLED"
	StatusAction        Status = "ACTION"
)

// Agent wide error code mappings.
const (
	UnknownError = iota
	CloudPropertiesNotSet
	GCEServiceCreateFailure
	MetricClientCreateFailure
	QueryClientCreateFailure
	BareMetalCloudPropertiesNotSet
	LocalHTTPListenerCreateFailure
	ConfigFileReadFailure
	MalformedConfigFile
	WLMMetricCollectionFailure
	ProcessMetricsMetricClientCreateFailure
	NoSAPInstancesFound
	HANAMonitoringCollectionFailure
	HANAMonitoringConfigReadFailure
	MalformedHANAMonitoringConfigFile
	MalformedDefaultHANAMonitoringQueriesFile
	AgentMetricsServiceCreateFailure
	HeartbeatMonitorCreateFailure
	HeartbeatMonitorRegistrationFailure
)

// Agent wide action mappings.
const (
	UnknownAction = iota
	CollectWLMMetrics
	CollectHostMetrics
	CollectProcessMetrics
	CollectHANAMonitoringMetrics
)

var (
	// projectNumbers contains known project numbers for test instances.
	projectNumbers = map[string]bool{
		"922508251869":  true,
		"155261204042":  true,
		"114837167255":  true,
		"161716815775":  true,
		"607888266690":  true,
		"863817768072":  true,
		"39979408140":   true,
		"510599941441":  true,
		"1038306394601": true,
		"714149369409":  true,
		"450711760461":  true,
		"600915385160":  true,
		"208472317671":  true,
		"824757391322":  true,
		"977154783768":  true,
		"148036532291":  true,
		"425380551487":  true,
		"811811474621":  true,
		"975534532604":  true,
		"475132212764":  true,
		"201338458013":  true,
		"269972924358":  true,
		"605897091243":  true,
		"1008799658123": true,
		"916154365516":  true,
		"843031526114":  true,
	}
	imageProjects [12]string = [12]string{
		"centos-cloud",
		"cos-cloud",
		"debian-cloud",
		"fedora-coreos-cloud",
		"rhel-cloud",
		"rhel-sap-cloud",
		"suse-cloud",
		"suse-sap-cloud",
		"ubuntu-os-cloud",
		"ubuntu-os-pro-cloud",
		"windows-cloud",
		"windows-sql-cloud",
	}
	imagePattern = regexp.MustCompile(
		fmt.Sprintf("projects[/](?:%s)[/]global[/]images[/](.*)", strings.Join(imageProjects[:], "|")),
	)
)

// The TimeSource interface is a wrapper around time functionality needed for usage metrics logging.
// A fake TimeSource can be supplied by tests to ensure test stability.
type TimeSource interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// A Logger is used to report the status of the agent to an internal metadata server.
type Logger struct {
	agentProps    *configpb.AgentProperties
	cloudProps    *instancepb.CloudProperties
	timeSource    TimeSource
	image         string
	isTestProject bool
	lastCalled    map[Status]time.Time
}

// NewLogger creates a new Logger with an initialized hash map of Status to a last called timestamp.
func NewLogger(agentProps *configpb.AgentProperties, cloudProps *instancepb.CloudProperties, timeSource TimeSource) *Logger {
	l := &Logger{
		agentProps: agentProps,
		timeSource: timeSource,
		lastCalled: make(map[Status]time.Time),
	}
	l.setCloudProps(cloudProps)
	return l
}

// Running logs the RUNNING status. This status is reported at most once per day.
func (l *Logger) Running() {
	l.logOncePerDay(StatusRunning, "")
}

// Started logs the STARTED status.
func (l *Logger) Started() {
	l.logStatus(StatusStarted, "")
}

// Stopped logs the STOPPED status.
func (l *Logger) Stopped() {
	l.logStatus(StatusStopped, "")
}

// Configured logs the CONFIGURED status.
func (l *Logger) Configured() {
	l.logStatus(StatusConfigured, "")
}

// Misconfigured logs the MISCONFIGURED status.
func (l *Logger) Misconfigured() {
	l.logStatus(StatusMisconfigured, "")
}

// Error logs the ERROR status. This status is reported at most once per day.
//
// Any calls to Error should have an id mapping in this mapping sheet: go/sap-core-eng-tool-mapping.
func (l *Logger) Error(id int) {
	l.logOncePerDay(StatusError, fmt.Sprintf("%d", id))
}

// Installed logs the INSTALLED status.
func (l *Logger) Installed() {
	l.logStatus(StatusInstalled, "")
}

// Updated logs the UPDATED status.
func (l *Logger) Updated(version string) {
	l.logStatus(StatusUpdated, version)
}

// Uninstalled logs the UNINSTALLED status.
func (l *Logger) Uninstalled() {
	l.logStatus(StatusUninstalled, "")
}

// Action logs the ACTION status.
func (l *Logger) Action(id int) {
	l.logStatus(StatusAction, fmt.Sprintf("%d", id))
}

func (l *Logger) log(s string) {
	log.Logger.Debugw("logging status", "status", s)
	err := l.requestComputeAPIWithUserAgent(buildComputeURL(l.cloudProps), buildUserAgent(l.agentProps, l.image, s))
	if err != nil {
		log.Logger.Warnw("Failed to send agent status", "error", err)
	}
}

func (l *Logger) logOncePerDay(s Status, v string) {
	if !l.agentProps.GetLogUsageMetrics() {
		return
	}
	if l.timeSource.Since(l.lastCalled[s]) < 24*time.Hour {
		log.Logger.Debugw("logging status once per day", "status", s, "lastcalled", l.lastCalled[s])
		return
	}
	l.logStatus(s, v)
}

func (l *Logger) logStatus(s Status, v string) {
	if !l.agentProps.GetLogUsageMetrics() {
		return
	}
	msg := string(s)
	if v != "" {
		msg = fmt.Sprintf("%s/%s", string(s), v)
	}
	l.log(msg)
	l.lastCalled[s] = l.timeSource.Now()
}

// requestComputeAPIWithUserAgent submits a GET request to the compute API with a custom user agent.
func (l *Logger) requestComputeAPIWithUserAgent(url, ua string) error {
	if l.isTestProject {
		return nil
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	req.Header.Add("User-Agent", ua)
	if _, err = http.DefaultClient.Do(req); err != nil {
		return err
	}
	return nil
}

// setCloudProps sets the cloud properties and ensures that dependent fields are kept in sync.
func (l *Logger) setCloudProps(cp *instancepb.CloudProperties) {
	l.cloudProps = cp
	if cp != nil {
		l.image = parseImage(cp.GetImage())
		l.isTestProject = projectNumbers[cp.GetNumericProjectId()]
	} else {
		l.image = metadataserver.ImageUnknown
		l.isTestProject = false
	}
}

// buildComputeURL returns a compute API URL with the proper projectId, zone, and instance name specified.
func buildComputeURL(cp *instancepb.CloudProperties) string {
	computeAPIURL := "https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s"
	if cp == nil {
		return fmt.Sprintf(computeAPIURL, "unknown", "unknown", "unknown")
	}
	return fmt.Sprintf(computeAPIURL, cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName())
}

// buildUserAgent returns a User-Agent string that will be submitted to the compute API.
//
// User-Agent is of the form "sap-core-eng/AgentName/Version/image-os-version/logged-status"
func buildUserAgent(ap *configpb.AgentProperties, image, status string) string {
	ua := fmt.Sprintf("sap-core-eng/%s/%s/%s/%s", ap.GetName(), ap.GetVersion(), image, status)
	ua = strings.ReplaceAll(strings.ReplaceAll(ua, " ", ""), "\n", "")
	return ua
}

// parseImage retrieves the OS and version from the image URI.
//
// The metadata server returns image as "projects/PROJECT_NAME/global/images/OS-VERSION".
func parseImage(image string) string {
	if image == metadataserver.ImageUnknown {
		return image
	}
	match := imagePattern.FindStringSubmatch(image)
	if len(match) >= 2 {
		return match[1]
	}
	return metadataserver.ImageUnknown
}
