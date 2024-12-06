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

// Package agenttime maintains time values that are necessary for metrics collection.
package agenttime

import (
	"time"

	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

// TimeSource can be faked by tests to ensure repeatable results.
type TimeSource interface {
	Now() time.Time
}

// Clock is used to provide a real timeSource to the gcagentUtils package.
type Clock struct{}

// Now implements the timeSource.Now interface, returning the value of time.Now().
func (Clock) Now() time.Time { return time.Now() }

// AgentTime struct holds application state that is required for various gcagent functionality.
type AgentTime struct {
	timeSource         TimeSource
	cloudMetricRefresh time.Time
	localRefresh       time.Time
	startup            time.Time
}

// New initializes a AgentTime instance with the provided timeSource.
func New(t TimeSource) *AgentTime {
	startup := t.Now()
	log.Logger.Debugw("startup time", "unix", startup.Unix(), "time", startup.String())
	new := &AgentTime{timeSource: t, startup: startup}
	// Make an initial call to UpdateRefreshTimes to guard against unexpected zero values.
	new.UpdateRefreshTimes()
	return new
}

// CloudMetricRefresh returns the current refresh time to be used when collecting cloud metrics.
func (u *AgentTime) CloudMetricRefresh() time.Time {
	return u.cloudMetricRefresh
}

// LocalRefresh returns the current refresh time to be used when collecting metrics.
func (u *AgentTime) LocalRefresh() time.Time {
	return u.localRefresh
}

// Startup returns the current startup time.
//
// The startup time will be used for reporting a last refresh time for static metrics,
// where the underlying metric value is not changing over time.
func (u *AgentTime) Startup() time.Time {
	return u.startup
}

// UpdateRefreshTimes sets a new time value for cloudMetricRefresh and localRefresh times.
//
// To account for the availability of cloud metrics, a refresh time of 3 minutes ago is chosen.
func (u *AgentTime) UpdateRefreshTimes() {
	now := u.timeSource.Now()
	u.cloudMetricRefresh = now.Add(-180 * time.Second)
	u.localRefresh = now
	log.Logger.Debugw("cloudMetricRefresh time", "unix", u.cloudMetricRefresh.Unix(), "time", u.cloudMetricRefresh.String())
	log.Logger.Debugw("localRefresh time", "unix", u.localRefresh.Unix(), "time", u.localRefresh.String())
}
