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

// Package system contains types and functions needed to perform SAP System discovery operations.
package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	logging "cloud.google.com/go/logging"
	workloadmanager "google.golang.org/api/workloadmanager/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type cloudLogInterface interface {
	Log(e logging.Entry)
	Flush() error
}

type wlmInterface interface {
	WriteInsight(project, location string, writeInsightRequest *workloadmanager.WriteInsightRequest) error
}

type cloudDiscoveryInterface interface {
	DiscoverComputeResources(context.Context, string, []string, *ipb.CloudProperties) []*spb.SapDiscovery_Resource
}

type hostDiscoveryInterface interface {
	DiscoverCurrentHost(context.Context) []string
}

type sapDiscoveryInterface interface {
	DiscoverSAPApps(ctx context.Context, cp *ipb.CloudProperties) []appsdiscovery.SapSystemDetails
}

func removeDuplicates(res []*spb.SapDiscovery_Resource) []*spb.SapDiscovery_Resource {
	var out []*spb.SapDiscovery_Resource
	uris := make(map[string]*spb.SapDiscovery_Resource)
	for _, r := range res {
		outRes, ok := uris[r.ResourceUri]
		if !ok {
			uris[r.ResourceUri] = r
			out = append(out, r)
		} else {
			for _, rel := range r.RelatedResources {
				if !slices.Contains(outRes.RelatedResources, rel) {
					outRes.RelatedResources = append(outRes.RelatedResources, rel)
				}
			}
		}
	}
	return out
}

// Discovery is a type used to perform SAP System discovery operations.
type Discovery struct {
	WlmService              wlmInterface
	CloudLogInterface       cloudLogInterface
	CloudDiscoveryInterface cloudDiscoveryInterface
	HostDiscoveryInterface  hostDiscoveryInterface
	SapDiscoveryInterface   sapDiscoveryInterface
}

func insightResourceFromSystemResource(r *spb.SapDiscovery_Resource) *workloadmanager.SapDiscoveryResource {

	return &workloadmanager.SapDiscoveryResource{
		RelatedResources: r.RelatedResources,
		ResourceKind:     r.ResourceKind.String(),
		ResourceType:     r.ResourceType.String(),
		ResourceUri:      r.ResourceUri,
		UpdateTime:       r.UpdateTime.AsTime().Format(time.RFC3339),
	}
}

func insightComponentFromSystemComponent(comp *spb.SapDiscovery_Component) *workloadmanager.SapDiscoveryComponent {
	iComp := &workloadmanager.SapDiscoveryComponent{
		HostProject: comp.HostProject,
		Sid:         comp.Sid,
	}

	for _, r := range comp.Resources {
		iComp.Resources = append(iComp.Resources, insightResourceFromSystemResource(r))
	}

	switch x := comp.Properties.(type) {
	case *spb.SapDiscovery_Component_ApplicationProperties_:
		iComp.ApplicationProperties = &workloadmanager.SapDiscoveryComponentApplicationProperties{
			ApplicationType: x.ApplicationProperties.GetApplicationType().String(),
			AscsUri:         x.ApplicationProperties.GetAscsUri(),
			NfsUri:          x.ApplicationProperties.GetNfsUri(),
		}
	case *spb.SapDiscovery_Component_DatabaseProperties_:
		iComp.DatabaseProperties = &workloadmanager.SapDiscoveryComponentDatabaseProperties{
			DatabaseType:       x.DatabaseProperties.GetDatabaseType().String(),
			PrimaryInstanceUri: x.DatabaseProperties.GetPrimaryInstanceUri(),
			SharedNfsUri:       x.DatabaseProperties.GetSharedNfsUri(),
		}
	}

	return iComp
}

func insightFromSAPSystem(sys *spb.SapDiscovery) *workloadmanager.Insight {
	iDiscovery := &workloadmanager.SapDiscovery{
		SystemId:   sys.SystemId,
		UpdateTime: sys.UpdateTime.AsTime().Format(time.RFC3339),
	}
	if sys.ApplicationLayer != nil {
		iDiscovery.ApplicationLayer = insightComponentFromSystemComponent(sys.ApplicationLayer)

	}
	if sys.DatabaseLayer != nil {
		iDiscovery.DatabaseLayer = insightComponentFromSystemComponent(sys.DatabaseLayer)
	}

	return &workloadmanager.Insight{SapDiscovery: iDiscovery}
}

// StartSAPSystemDiscovery Initializes the discovery object and starts the discovery subroutine.
// Returns true if the discovery goroutine is started, and false otherwise.
func StartSAPSystemDiscovery(ctx context.Context, config *cpb.Configuration, d *Discovery) bool {
	// Start SAP system discovery only if sap_system_discovery is enabled.
	if !config.GetCollectionConfiguration().GetSapSystemDiscovery().GetValue() {
		log.CtxLogger(ctx).Info("Not starting SAP system discovery.")
		return false
	}

	go runDiscovery(ctx, config, d)
	return true
}

func runDiscovery(ctx context.Context, config *cpb.Configuration, d *Discovery) {
	cp := config.GetCloudProperties()
	if cp == nil {
		log.CtxLogger(ctx).Warn("No Metadata Cloud Properties found, cannot collect resource information from the Compute API")
		return
	}

	for {
		sapSystems := d.discoverSAPSystems(ctx, cp)

		locationParts := strings.Split(cp.GetZone(), "-")
		region := strings.Join([]string{locationParts[0], locationParts[1]}, "-")

		log.CtxLogger(ctx).Info("Sending systems to WLM API")
		for _, sys := range sapSystems {
			// Send System to DW API
			req := &workloadmanager.WriteInsightRequest{
				Insight: insightFromSAPSystem(sys),
			}
			req.Insight.InstanceId = cp.GetInstanceId()

			err := d.WlmService.WriteInsight(cp.ProjectId, region, req)
			if err != nil {
				log.CtxLogger(ctx).Warnw("Encountered error writing to WLM", "error", err)
			}

			if d.CloudLogInterface == nil {
				continue
			}
			err = d.writeToCloudLogging(sys)
			if err != nil {
				log.CtxLogger(ctx).Warnw("Encountered error writing to cloud logging", "error", err)
			}
		}

		log.CtxLogger(ctx).Info("Done SAP System Discovery")
		// Perform discovery at most every 4 hours.
		time.Sleep(4 * 60 * 60 * time.Second)
	}
}

func (d *Discovery) discoverSAPSystems(ctx context.Context, cp *ipb.CloudProperties) []*spb.SapDiscovery {
	sapSystems := []*spb.SapDiscovery{}

	instanceURI := fmt.Sprintf("projects/%s/zones/%s/instances/%s", cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName())
	log.CtxLogger(ctx).Info("Starting SAP Discovery")
	sapDetails := d.SapDiscoveryInterface.DiscoverSAPApps(ctx, cp)
	log.CtxLogger(ctx).Debugf("SAP Details: %v", sapDetails)
	log.CtxLogger(ctx).Info("Starting host discovery")
	hostResourceNames := d.HostDiscoveryInterface.DiscoverCurrentHost(ctx)
	log.CtxLogger(ctx).Debugf("Host Resource Names: %v", hostResourceNames)
	log.CtxLogger(ctx).Info("Discovering cloud resources for host")
	hostResources := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceURI, hostResourceNames, cp)
	log.CtxLogger(ctx).Debugf("Host Resources: %v", hostResources)
	for _, s := range sapDetails {
		system := &spb.SapDiscovery{}
		if s.AppSID != "" {
			log.CtxLogger(ctx).Info("Discovering cloud resources for app")
			appRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceURI, s.AppHosts, cp)
			log.CtxLogger(ctx).Debugf("App Resources: %v", appRes)
			if s.AppOnHost {
				appRes = append(appRes, hostResources...)
				log.CtxLogger(ctx).Debugf("App On Host Resources: %v", appRes)
			}
			if s.AppProperties.GetNfsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for app NFS")
				nfsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, s.AppProperties.GetNfsUri(), nil, cp)
				appRes = append(appRes, nfsRes...)
				s.AppProperties.NfsUri = nfsRes[0].GetResourceUri()
			}
			if s.AppProperties.GetAscsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for app ASCS")
				ascsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, s.AppProperties.GetAscsUri(), nil, cp)
				appRes = append(appRes, ascsRes...)
				s.AppProperties.AscsUri = ascsRes[0].GetResourceUri()
			}
			system.ApplicationLayer = &spb.SapDiscovery_Component{
				Sid:         s.AppSID,
				Resources:   removeDuplicates(appRes),
				Properties:  &spb.SapDiscovery_Component_ApplicationProperties_{ApplicationProperties: s.AppProperties},
				HostProject: cp.GetNumericProjectId(),
			}
		}
		if s.DBSID != "" {
			log.CtxLogger(ctx).Info("Discovering cloud resources for database")
			dbRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceURI, s.DBHosts, cp)
			if s.DBOnHost {
				dbRes = append(dbRes, hostResources...)
			}
			if s.DBProperties.GetSharedNfsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for database NFS")
				nfsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, s.DBProperties.GetSharedNfsUri(), nil, cp)
				if len(nfsRes) > 0 {
					dbRes = append(dbRes, nfsRes...)
					s.DBProperties.SharedNfsUri = nfsRes[0].GetResourceUri()
				}
			}
			system.DatabaseLayer = &spb.SapDiscovery_Component{
				Sid:         s.DBSID,
				Resources:   removeDuplicates(dbRes),
				Properties:  &spb.SapDiscovery_Component_DatabaseProperties_{DatabaseProperties: s.DBProperties},
				HostProject: cp.GetNumericProjectId(),
			}
		}
		system.ProjectNumber = cp.GetNumericProjectId()
		system.UpdateTime = timestamppb.Now()
		sapSystems = append(sapSystems, system)
	}
	return sapSystems
}

func (d *Discovery) writeToCloudLogging(sys *spb.SapDiscovery) error {
	s, err := protojson.Marshal(sys)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	json.Indent(&buf, s, "", "  ")

	payload := make(map[string]string)
	payload["type"] = "SapDiscovery"
	payload["discovery"] = buf.String()

	d.CloudLogInterface.Log(logging.Entry{
		Timestamp: time.Now(),
		Severity:  logging.Info,
		Payload:   payload,
	})

	return nil
}
