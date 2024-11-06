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

// Package usagemetrics provides logging utility for the operational status of Google Cloud Agents.
package usagemetrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/compute/v1"
	"golang.org/x/oauth2/google"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
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

var (
	lock = sync.Mutex{}
)

// The TimeSource interface is a wrapper around time functionality needed for usage metrics logging.
// A fake TimeSource can be supplied by tests to ensure test stability.
type TimeSource interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// AgentProperties contains the properties of the agent used by UsageMetrics library.
type AgentProperties struct {
	Name             string
	Version          string
	LogUsageMetrics  bool
	LogUsagePrefix   string
	LogUsageOptional string // optional string to be added to the usage log: "UsageLogPrefix/AgentName/AgentVersion[/OptionalString]/Status"
}

func (ap *AgentProperties) getLogUsageMetrics() bool {
	if ap == nil {
		return false
	}
	return ap.LogUsageMetrics
}

// CloudProperties contains the properties of the cloud instance used by UsageMetrics library.
type CloudProperties struct {
	ProjectID     string
	Zone          string
	InstanceName  string
	ProjectNumber string
	Image         string
	InstanceID    string
}

// A Logger is used to report the status of the agent to an internal metadata server.
type Logger struct {
	agentProps             *AgentProperties
	cloudProps             *CloudProperties
	timeSource             TimeSource
	isTestProject          bool
	lastCalled             map[Status]time.Time
	dailyLogRunningStarted bool
	projectExclusions      map[string]bool
}

// NewLogger creates a new Logger with an initialized hash map of Status to a last called timestamp.
func NewLogger(agentProps *AgentProperties, cloudProps *CloudProperties, timeSource TimeSource, projectExclusions []string) *Logger {
	l := &Logger{
		agentProps:        agentProps,
		timeSource:        timeSource,
		lastCalled:        make(map[Status]time.Time),
		projectExclusions: make(map[string]bool),
	}
	l.setProjectExclusions(projectExclusions)
	l.SetCloudProps(cloudProps)
	return l
}

// DailyLogRunningStarted logs the RUNNING status.
func (l *Logger) DailyLogRunningStarted() {
	l.dailyLogRunningStarted = true
}

// IsDailyLogRunningStarted returns true if DailyLogRunningStarted was previously called.
func (l *Logger) IsDailyLogRunningStarted() bool {
	return l.dailyLogRunningStarted
}

// Running logs the RUNNING status.
func (l *Logger) Running() {
	l.LogStatus(StatusRunning, "")
}

// Started logs the STARTED status.
func (l *Logger) Started() {
	l.LogStatus(StatusStarted, "")
}

// Stopped logs the STOPPED status.
func (l *Logger) Stopped() {
	l.LogStatus(StatusStopped, "")
}

// Configured logs the CONFIGURED status.
func (l *Logger) Configured() {
	l.LogStatus(StatusConfigured, "")
}

// Misconfigured logs the MISCONFIGURED status.
func (l *Logger) Misconfigured() {
	l.LogStatus(StatusMisconfigured, "")
}

// Error logs the ERROR status.
func (l *Logger) Error(id int) {
	l.LogStatus(StatusError, fmt.Sprintf("%d", id))
}

// Installed logs the INSTALLED status.
func (l *Logger) Installed() {
	l.LogStatus(StatusInstalled, "")
}

// Updated logs the UPDATED status.
func (l *Logger) Updated(version string) {
	l.LogStatus(StatusUpdated, version)
}

// Uninstalled logs the UNINSTALLED status.
func (l *Logger) Uninstalled() {
	l.LogStatus(StatusUninstalled, "")
}

// Action logs the ACTION status.
func (l *Logger) Action(id int) {
	l.LogStatus(StatusAction, fmt.Sprintf("%d", id))
}

func (l *Logger) log(s string) error {
	log.Logger.Debugw("logging status", "status", s)
	if l.cloudProps == nil || l.cloudProps.Zone == "" {
		log.Logger.Warnw("Unable to send agent status without properly set zone in cloud properties", "l.cloudProps", l.cloudProps)
		return errors.New("unable to send agent status without properly set zone in cloud properties")
	}
	err := l.requestComputeAPIWithUserAgent(buildComputeURL(l.cloudProps), buildUserAgent(l.agentProps, s))
	if err != nil {
		log.Logger.Warnw("failed to send agent status", "error", err)
		return err
	}
	return nil
}

// LogStatus logs the agent status if usage metrics logging is enabled.
func (l *Logger) LogStatus(s Status, v string) {
	if !l.agentProps.getLogUsageMetrics() {
		return
	}
	msg := string(s)
	if v != "" {
		msg = fmt.Sprintf("%s/%s", string(s), v)
	}
	l.log(msg)
	lock.Lock()
	defer lock.Unlock()
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
	client, _ := google.DefaultClient(context.Background(), compute.ComputeScope)
	if client == nil {
		client = http.DefaultClient // If OAUTH fails, use the default http client.
	}
	if _, err = client.Do(req); err != nil {
		return err
	}
	return nil
}

// SetCloudProps sets the cloud properties and ensures that dependent fields are kept in sync.
func (l *Logger) SetCloudProps(cp *CloudProperties) {
	l.cloudProps = cp
	if cp != nil {
		l.isTestProject = l.projectExclusions[cp.ProjectNumber]
	} else {
		l.isTestProject = false
	}
}

// SetAgentProps sets the agent properties
func (l *Logger) SetAgentProps(ap *AgentProperties) {
	l.agentProps = ap
}

// SetProjectExclusions sets the project exclusions dictionary
func (l *Logger) setProjectExclusions(pe []string) {
	for _, p := range pe {
		l.projectExclusions[p] = true
	}
}

// LastCalled returns the last time a status was called.
func (l *Logger) LastCalled(s Status) time.Time {
	lock.Lock()
	defer lock.Unlock()
	return l.lastCalled[s]
}

// buildComputeURL returns a compute API URL with the proper projectId, zone, and instance name specified.
func buildComputeURL(cp *CloudProperties) string {
	computeAPIURL := "https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s"
	if cp == nil {
		return fmt.Sprintf(computeAPIURL, "unknown", "unknown", "unknown")
	}
	return fmt.Sprintf(computeAPIURL, cp.ProjectID, cp.Zone, cp.InstanceName)
}

// buildUserAgent returns a User-Agent string that will be submitted to the compute API.
//
// User-Agent is of the form "UsageLogPrefix/AgentName/AgentVersion[/OptionalString]/Status".
func buildUserAgent(ap *AgentProperties, status string) string {
	ua := fmt.Sprintf("%s/%s/%s", ap.LogUsagePrefix, ap.Name, ap.Version)
	if ap.LogUsageOptional != "" {
		ua = fmt.Sprintf("%s/%s", ua, ap.LogUsageOptional)
	}
	ua = fmt.Sprintf("%s/%s", ua, status)
	ua = strings.ReplaceAll(strings.ReplaceAll(ua, " ", ""), "\n", "")
	return ua
}
