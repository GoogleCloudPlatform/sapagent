/*
Copyright 2023 Google LLC

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

package workloadmanager

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/zieckey/goini"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/preprocessor"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

/*
OSStatReader abstracts os.FileInfo reading. OSStatReader Example usage:

	OSStatReader(func(f string) (os.FileInfo, error) {
		return os.Stat(f)
	})
*/
type OSStatReader func(string) (os.FileInfo, error)

// DefaultTokenGetter obtains a "default" oauth2 token source within the getDefaultBearerToken function.
type DefaultTokenGetter func(context.Context, ...string) (oauth2.TokenSource, error)

// JSONCredentialsGetter obtains a JSON oauth2 google credentials within the getJSONBearerToken function.
type JSONCredentialsGetter func(context.Context, []byte, ...string) (*google.Credentials, error)

type gceInterface interface {
	GetSecret(ctx context.Context, projectID, secretName string) (string, error)
}

// Parameters holds the parameters for all of the Collect* function calls.
type Parameters struct {
	Config                *cpb.Configuration
	WorkloadConfig        *wlmpb.WorkloadValidation
	Remote                bool
	ConfigFileReader      ConfigFileReader
	OSStatReader          OSStatReader
	Execute               commandlineexecutor.Execute
	Exists                commandlineexecutor.Exists
	InstanceInfoReader    instanceinfo.Reader
	TimeSeriesCreator     cloudmonitoring.TimeSeriesCreator
	DefaultTokenGetter    DefaultTokenGetter
	JSONCredentialsGetter JSONCredentialsGetter
	OSType                string
	BackOffs              *cloudmonitoring.BackOffIntervals
	HeartbeatSpec         *heartbeat.Spec
	InterfaceAddrsGetter  InterfaceAddrsGetter
	OSReleaseFilePath     string
	GCEService            gceInterface
	WLMService            wlmInterface
	// fields derived from parsing the file specified by OSReleaseFilePath
	osVendorID string
	osVersion  string
	// fields derived from running discovery on the SAP system
	sapApplications *sapb.SAPInstances
	// fields derived from reading HANA Insights rules
	hanaInsightRules []*rpb.Rule
}

// Init runs additional setup that is a prerequisite for WLM metric collection.
func (p *Parameters) Init(ctx context.Context) {
	p.osVendorID, p.osVersion = setOSReleaseInfo(p.ConfigFileReader, p.OSReleaseFilePath)
	p.sapApplications = runDiscovery(ctx)
	p.hanaInsightRules = readHANAInsightsRules()
}

// setOSReleaseInfo parses the OS release file and retrieves the values for the
// osVendorID and osVersion.
func setOSReleaseInfo(configFileReader ConfigFileReader, osReleaseFilePath string) (osVendorID, osVersion string) {
	if configFileReader == nil || osReleaseFilePath == "" {
		log.Logger.Debug("A ConfigFileReader and OSReleaseFilePath must be set.")
		return
	}

	file, err := configFileReader(osReleaseFilePath)
	if err != nil {
		log.Logger.Warnw(fmt.Sprintf("Could not read from %s", osReleaseFilePath), "error", err)
		return
	}
	defer file.Close()

	ini := goini.New()
	if err := ini.ParseFrom(file, "\n", "="); err != nil {
		log.Logger.Warnw(fmt.Sprintf("Failed to parse from %s", osReleaseFilePath), "error", err)
		return
	}

	id, ok := ini.Get("ID")
	if !ok {
		log.Logger.Warn(fmt.Sprintf("Could not read ID from %s", osReleaseFilePath))
		id = ""
	}
	osVendorID = strings.ReplaceAll(strings.TrimSpace(id), `"`, "")

	version, ok := ini.Get("VERSION")
	if !ok {
		log.Logger.Warn(fmt.Sprintf("Could not read VERSION from %s", osReleaseFilePath))
		version = ""
	}
	if vf := strings.Fields(version); len(vf) > 0 {
		osVersion = strings.ReplaceAll(strings.TrimSpace(vf[0]), `"`, "")
	}

	return osVendorID, osVersion
}

// runDiscovery runs the discovery process to find HANA / NetWeaver instances.
func runDiscovery(ctx context.Context) *sapb.SAPInstances {
	// TODO: Share SAP System Discovery results across agent.
	log.CtxLogger(ctx).Info("Discovering SAP Applications for Workload Manager Metrics.")
	return sapdiscovery.SAPApplications(ctx)
}

// readHANAInsightsRules reads the HANA Insights rules.
func readHANAInsightsRules() []*rpb.Rule {
	hanaInsightRules, err := preprocessor.ReadRules(preprocessor.RuleFilenames)
	if err != nil {
		log.Logger.Errorw("Error Reading HANA Insights rules", "error", err)
		return nil
	}
	return hanaInsightRules
}
