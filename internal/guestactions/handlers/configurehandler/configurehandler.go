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

// Package configurehandler implements the handler for the configure command.
package configurehandler

import (
	"context"
	"strconv"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configure"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// RestartAgent indicates that the agent should be restarted after the configure guest action has been handled.
const RestartAgent = true

func noOpRestart(ctx context.Context) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("Delaying restart until after we respond to UAP.")
	return subcommands.ExitSuccess
}

// TODO - Consider defining constants for the command parameters.
// TODO - Investigate marshalling the command parameters into JSON and then unmarshalling into configure.Configure struct.
func buildConfigureFromMap(command *gpb.AgentCommand) configure.Configure {
	fields := command.GetParameters()
	c := configure.Configure{
		RestartAgent: noOpRestart,
	}
	feature, ok := fields["feature"]
	if ok {
		c.Feature = feature
	}
	logLevel, ok := fields["loglevel"]
	if ok {
		c.LogLevel = logLevel
	}
	setting, ok := fields["setting"]
	if ok {
		c.Setting = setting
	}
	path, ok := fields["path"]
	if ok {
		c.Path = path
	}
	skipMetrics, ok := fields["process_metrics_to_skip"]
	if ok {
		c.SkipMetrics = skipMetrics
	}
	validationMetricsFrequency, ok := fields["workload_evaluation_metrics_frequency"]
	if ok {
		c.ValidationMetricsFrequency, _ = strconv.ParseInt(validationMetricsFrequency, 10, 64)
	}
	dbFrequency, ok := fields["workload_evaluation_db_metrics_frequency"]
	if ok {
		c.DbFrequency, _ = strconv.ParseInt(dbFrequency, 10, 64)
	}
	fastMetricsFrequency, ok := fields["process_metrics_frequency"]
	if ok {
		c.FastMetricsFrequency, _ = strconv.ParseInt(fastMetricsFrequency, 10, 64)
	}
	slowMetricsFrequency, ok := fields["slow_process_metrics_frequency"]
	if ok {
		c.SlowMetricsFrequency, _ = strconv.ParseInt(slowMetricsFrequency, 10, 64)
	}
	agentMetricsFrequency, ok := fields["agent_metrics_frequency"]
	if ok {
		c.AgentMetricsFrequency, _ = strconv.ParseInt(agentMetricsFrequency, 10, 64)
	}
	agentHealthFrequency, ok := fields["agent_health_frequency"]
	if ok {
		c.AgentHealthFrequency, _ = strconv.ParseInt(agentHealthFrequency, 10, 64)
	}
	heartbeatFrequency, ok := fields["heartbeat_frequency"]
	if ok {
		c.HeartbeatFrequency, _ = strconv.ParseInt(heartbeatFrequency, 10, 64)
	}
	reliabilityMetricsFrequency, ok := fields["reliability_metrics_frequency"]
	if ok {
		c.ReliabilityMetricsFrequency, _ = strconv.ParseInt(reliabilityMetricsFrequency, 10, 64)
	}
	sampleIntervalSec, ok := fields["sample_interval_sec"]
	if ok {
		c.SampleIntervalSec, _ = strconv.ParseInt(sampleIntervalSec, 10, 64)
	}
	queryTimeoutSec, ok := fields["query_timeout_sec"]
	if ok {
		c.QueryTimeoutSec, _ = strconv.ParseInt(queryTimeoutSec, 10, 64)
	}
	help, ok := fields["help"]
	if ok {
		c.Help, _ = strconv.ParseBool(help)
	}
	version, ok := fields["version"]
	if ok {
		c.Version, _ = strconv.ParseBool(version)
	}
	enable, ok := fields["enable"]
	if ok {
		c.Enable, _ = strconv.ParseBool(enable)
	}
	disable, ok := fields["disable"]
	if ok {
		c.Disable, _ = strconv.ParseBool(disable)
	}
	showall, ok := fields["showall"]
	if ok {
		c.Showall, _ = strconv.ParseBool(showall)
	}
	add, ok := fields["add"]
	if ok {
		c.Add, _ = strconv.ParseBool(add)
	}
	remove, ok := fields["remove"]
	if ok {
		c.Remove, _ = strconv.ParseBool(remove)
	}
	return c
}

// ConfigureHandler is the handler for the configure command.
func ConfigureHandler(ctx context.Context, command *gpb.AgentCommand) (string, subcommands.ExitStatus, bool) {
	log.CtxLogger(ctx).Debugw("Handling command", "command", command)
	c := buildConfigureFromMap(command)
	msg, exitStatus := c.ExecuteAndGetMessage(ctx, nil)
	log.CtxLogger(ctx).Debugw("handled command result -", "msg", msg, "exitStatus", exitStatus)
	return msg, exitStatus, RestartAgent
}
