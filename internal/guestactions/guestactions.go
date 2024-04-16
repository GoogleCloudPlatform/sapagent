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

// Package guestactions connects to UAP Highway and handles guest actions in the agent.
package guestactions

import (
	"context"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/recovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/uap"
)

const (
	defaultChannel  = "wlm-sap-channel"
	defaultEndpoint = ""
)

type guestActionsOptions struct {
	channel  string
	endpoint string
}

func messageHandler(ctx context.Context, command string) (string, error) {
	executable, args, _ := strings.Cut(command, " ")
	result := commandlineexecutor.ExecuteCommand(
		ctx,
		commandlineexecutor.Params{Executable: executable, ArgsToSplit: args},
	)
	return result.StdOut, result.Error
}

func start(ctx context.Context, a any) {
	args, ok := a.(guestActionsOptions)
	if !ok {
		log.CtxLogger(ctx).Warn("args is not of type guestActionsArgs")
		return
	}
	uap.CommunicateWithUAP(ctx, args.endpoint, args.channel, messageHandler)
}

// StartUAPCommunication establishes communication with UAP Highway.
// Returns true if the goroutine is started, and false otherwise.
func StartUAPCommunication(ctx context.Context, config *cpb.Configuration) bool {
	if !config.GetUapConfiguration().GetEnabled().GetValue() {
		log.CtxLogger(ctx).Info("Not configured to communicate with UAP")
		return false
	}
	dailyMetricsRoutine := &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.GuestActionsStarted) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		ExpectedMinDuration: 24 * time.Hour,
	}
	dailyMetricsRoutine.StartRoutine(ctx)

	communicateRoutine := &recovery.RecoverableRoutine{
		Routine:             start,
		RoutineArg:          guestActionsOptions{channel: defaultChannel, endpoint: defaultEndpoint},
		ErrorCode:           usagemetrics.GuestActionsFailure,
		ExpectedMinDuration: 10 * time.Second,
	}
	log.CtxLogger(ctx).Info("Starting UAP communication routine")
	communicateRoutine.StartRoutine(ctx)
	return true
}
