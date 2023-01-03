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

package usagemetrics_test

import (
	"github.com/jonboulle/clockwork"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
)

func Example_standardLogger() {
	// If using the standard logger exposed by the package, first configure its properties.
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		/* AgentProperties configuration omitted */
		LogUsageMetrics: false, // Do not log in examples.
	})
	usagemetrics.SetCloudProperties(&iipb.CloudProperties{
		/* CloudProperties configuration omitted */
	})

	// Then, the top-level package functions can be used to report a formatted User-Agent header
	// to a compute server API endpoint.
	usagemetrics.Running() // Logs a header of: "sap-core-eng/<AgentName>/<AgentVersion>/<Image OS-Version>/RUNNING"
}

func Example_customLogger() {
	// Alternatively, a custom Logger instance can be instantiated.
	agentProps := &cpb.AgentProperties{
		/* AgentProperties configuration omitted */
		LogUsageMetrics: false, // Do not log in examples.
	}
	cloudProps := &iipb.CloudProperties{ /* CloudProperties configuration omitted */ }
	logger := usagemetrics.NewLogger(agentProps, cloudProps, clockwork.NewRealClock())

	// Then, the methods on the logger can be used to report a formatted User-Agent header
	// to a compute server API endpoint.
	logger.Running() // Logs a header of: "sap-core-eng/<AgentName>/<AgentVersion>/<Image OS-Version>/RUNNING"
}
