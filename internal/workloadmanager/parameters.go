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

	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/preprocessor"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/osinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
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

type discoveryInterface interface {
	GetSAPSystems() []*spb.SapDiscovery
	GetSAPInstances() *sappb.SAPInstances
}

type gceInterface interface {
	GetSecret(ctx context.Context, projectID, secretName string) (string, error)
}

// Parameters holds the parameters for all of the Collect* function calls.
type Parameters struct {
	Config                *cpb.Configuration
	WorkloadConfig        *wlmpb.WorkloadValidation
	WorkloadConfigCh      <-chan *cdpb.CollectionDefinition
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
	Discovery             discoveryInterface
	// fields derived from parsing the file specified by OSReleaseFilePath
	osVendorID string
	osVersion  string
	// fields derived from reading HANA Insights rules
	hanaInsightRules []*rpb.Rule
}

// Init runs additional setup that is a prerequisite for WLM metric collection.
func (p *Parameters) Init(ctx context.Context) {
	osData, err := osinfo.ReadData(ctx, osinfo.FileReadCloser(p.ConfigFileReader), p.OSReleaseFilePath)
	if err != nil {
		log.CtxLogger(ctx).Debugw(fmt.Sprintf("Could not read OS release info from %s", p.OSReleaseFilePath), "error", err)
	}
	p.osVendorID = osData.OSVendor
	p.osVersion = osData.OSVersion
	p.hanaInsightRules = readHANAInsightsRules()
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
