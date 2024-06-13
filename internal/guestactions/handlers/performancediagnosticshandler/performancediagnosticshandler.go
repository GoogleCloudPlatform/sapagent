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

// Package performancediagnosticshandler contains the handler for the performance diagnostics command.
package performancediagnosticshandler

import (
	"context"
	"runtime"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/performancediagnostics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// RestartAgent indicates that the agent should be restarted after the performance diagnostics guest action has been handled.
const RestartAgent = false

func buildPerformanceDiagnosticsCommand(command *gpb.AgentCommand) performancediagnostics.Diagnose {
	d := performancediagnostics.Diagnose{}

	params := command.GetParameters()
	if scope, ok := params["scope"]; ok {
		d.Scope = scope
	}
	if testBucket, ok := params["test-bucket"]; ok {
		d.TestBucket = testBucket
	}
	if paramFile, ok := params["param-file"]; ok {
		d.ParamFile = paramFile
	}
	if resultBucket, ok := params["result-bucket"]; ok {
		d.ResultBucket = resultBucket
	}
	if bundleName, ok := params["bundle-name"]; ok {
		d.BundleName = bundleName
	}
	if path, ok := params["path"]; ok {
		d.Path = path
	}
	if hyperThreading, ok := params["hyper-threading"]; ok {
		d.HyperThreading = hyperThreading
	}
	if logLevel, ok := params["loglevel"]; ok {
		d.LogLevel = logLevel
	}
	return d
}

// PerformanceDiagnosticsHandler is the handler for the performance diagnostics command.
func PerformanceDiagnosticsHandler(ctx context.Context, command *gpb.AgentCommand, cp *ipb.CloudProperties) (string, subcommands.ExitStatus, bool) {
	d := buildPerformanceDiagnosticsCommand(command)
	lp := log.Parameters{OSType: runtime.GOOS}
	message, exitStatus := d.ExecuteAndGetMessage(ctx, nil, lp, cp)
	return message, exitStatus, RestartAgent
}
