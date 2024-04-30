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
	"strings"
	"sync"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	logging "cloud.google.com/go/logging"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/recovery"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	dwpb "github.com/GoogleCloudPlatform/sapagent/protos/datawarehouse"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
)

// Discovery is a type used to perform SAP System discovery operations.
type Discovery struct {
	WlmService              wlmInterface
	CloudLogInterface       cloudLogInterface
	CloudDiscoveryInterface cloudDiscoveryInterface
	HostDiscoveryInterface  hostDiscoveryInterface
	SapDiscoveryInterface   sapDiscoveryInterface
	AppsDiscovery           func(context.Context) *sappb.SAPInstances
	systems                 []*spb.SapDiscovery
	systemMu                sync.Mutex
	sapInstances            *sappb.SAPInstances
	sapMu                   sync.Mutex
	sapInstancesRoutine     *recovery.RecoverableRoutine
	systemDiscoveryRoutine  *recovery.RecoverableRoutine
}

// GetSAPSystems returns the current list of SAP Systems discovered on the current host.
func (d *Discovery) GetSAPSystems() []*spb.SapDiscovery {
	d.systemMu.Lock()
	defer d.systemMu.Unlock()
	return d.systems
}

// GetSAPInstances returns the current list of SAP Instances discovered on the current host.
func (d *Discovery) GetSAPInstances() *sappb.SAPInstances {
	d.sapMu.Lock()
	defer d.sapMu.Unlock()
	return d.sapInstances
}

// StartSAPSystemDiscovery Initializes the discovery object and starts the discovery subroutine.
// Returns true if the discovery goroutine is started, and false otherwise.
func StartSAPSystemDiscovery(ctx context.Context, config *cpb.Configuration, d *Discovery) bool {

	d.sapInstancesRoutine = &recovery.RecoverableRoutine{
		Routine:             updateSAPInstances,
		RoutineArg:          updateSapInstancesArgs{config, d},
		ErrorCode:           usagemetrics.DiscoverSapInstanceFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 5 * time.Second,
	}
	d.sapInstancesRoutine.StartRoutine(ctx)

	// Ensure SAP instances is populated before starting system discovery
	backoff.Retry(func() error {
		if d.GetSAPInstances() != nil {
			return nil
		}
		return fmt.Errorf("SAP Instances not ready yet")
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 120))

	d.systemDiscoveryRoutine = &recovery.RecoverableRoutine{
		Routine:             runDiscovery,
		RoutineArg:          runDiscoveryArgs{config, d},
		ErrorCode:           usagemetrics.DiscoverSapSystemFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 10 * time.Second,
	}

	d.systemDiscoveryRoutine.StartRoutine(ctx)

	// Ensure systems are populated before returning
	backoff.Retry(func() error {
		if d.GetSAPSystems() != nil {
			return nil
		}
		return fmt.Errorf("SAP Systems not ready yet")
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 120))
	return true
}

type cloudLogInterface interface {
	Log(e logging.Entry)
	Flush() error
}

type wlmInterface interface {
	WriteInsight(project, location string, writeInsightRequest *dwpb.WriteInsightRequest) error
}

type cloudDiscoveryInterface interface {
	DiscoverComputeResources(context.Context, *spb.SapDiscovery_Resource, []string, *ipb.CloudProperties) []*spb.SapDiscovery_Resource
}

type hostDiscoveryInterface interface {
	DiscoverCurrentHost(context.Context) []string
}

type sapDiscoveryInterface interface {
	DiscoverSAPApps(ctx context.Context, sapApps *sappb.SAPInstances, conf *cpb.DiscoveryConfiguration) []appsdiscovery.SapSystemDetails
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
			if r.GetInstanceProperties() != nil {
				log.Logger.Debugw("Stored instance properties", "properties", outRes.GetInstanceProperties().String())
				log.Logger.Debugw("Duplicate instance properties", "properties", r.InstanceProperties.String())
				if outRes.InstanceProperties == nil {
					outRes.InstanceProperties = r.InstanceProperties
				} else {
					outRes.InstanceProperties.InstanceRole |= r.InstanceProperties.InstanceRole
					if r.InstanceProperties.GetVirtualHostname() != "" {
						outRes.InstanceProperties.VirtualHostname = r.InstanceProperties.VirtualHostname
					}
					apps := make(map[string]string, len(outRes.InstanceProperties.AppInstances))
					for _, app := range outRes.InstanceProperties.AppInstances {
						apps[app.Name] = app.Number
						log.Logger.Debugw("App instance", "app", app.String())
					}
					for _, app := range r.InstanceProperties.AppInstances {
						if _, o := apps[app.Name]; !o {
							outRes.InstanceProperties.AppInstances = append(outRes.InstanceProperties.AppInstances, app)
							apps[app.Name] = app.Number
							log.Logger.Debugw("Adding app instance", "app", app.String())
						} else {
							log.Logger.Debugw("Duplicate app instance", "app", app.String())
						}
					}
				}
				log.Logger.Debugw("Merged properties", "properties", outRes.InstanceProperties.String())
			}
		}
	}
	return out
}

type updateSapInstancesArgs struct {
	config *cpb.Configuration
	d      *Discovery
}

type runDiscoveryArgs struct {
	config *cpb.Configuration
	d      *Discovery
}

func updateSAPInstances(ctx context.Context, a any) {
	var args updateSapInstancesArgs
	var ok bool
	if args, ok = a.(updateSapInstancesArgs); !ok {
		log.CtxLogger(ctx).Warn("args is not of type updateSapInstancesArgs")
		return
	}

	log.CtxLogger(ctx).Info("Starting SAP Instances update")
	updateTicker := time.NewTicker(args.config.GetDiscoveryConfiguration().GetSapInstancesUpdateFrequency().AsDuration())
	for {
		log.CtxLogger(ctx).Info("Updating SAP Instances")
		sapInst := args.d.AppsDiscovery(ctx)
		args.d.sapMu.Lock()
		args.d.sapInstances = sapInst
		args.d.sapMu.Unlock()

		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("SAP Discovery cancellation requested")
			return
		case <-updateTicker.C:
			continue
		}
	}
}

func runDiscovery(ctx context.Context, a any) {
	log.CtxLogger(ctx).Info("Starting SAP System Discovery")
	var args runDiscoveryArgs
	var ok bool
	if args, ok = a.(runDiscoveryArgs); !ok {
		log.CtxLogger(ctx).Warn("args is not of type runDiscoveryArgs")
		return
	}
	cp := args.config.GetCloudProperties()
	if cp == nil {
		log.CtxLogger(ctx).Warn("No Metadata Cloud Properties found, cannot collect resource information from the Compute API")
		return
	}

	updateTicker := time.NewTicker(args.config.GetDiscoveryConfiguration().GetSystemDiscoveryUpdateFrequency().AsDuration())
	for {
		sapSystems := args.d.discoverSAPSystems(ctx, cp, args.config)

		locationParts := strings.Split(cp.GetZone(), "-")
		region := strings.Join([]string{locationParts[0], locationParts[1]}, "-")

		// Write SAP system discovery data only if sap_system_discovery is enabled.
		if args.config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
			log.CtxLogger(ctx).Info("Sending systems to WLM API")
			for _, sys := range sapSystems {
				// Send System to DW API
				insightRequest := &dwpb.WriteInsightRequest{
					Insight: &dwpb.Insight{
						SapDiscovery: sys,
						InstanceId:   cp.GetInstanceId(),
					},
				}
				insightRequest.AgentVersion = configuration.AgentVersion

				err := args.d.WlmService.WriteInsight(cp.ProjectId, region, insightRequest)
				if err != nil {
					log.CtxLogger(ctx).Warnw("Encountered error writing to WLM", "error", err)
				}

				if args.d.CloudLogInterface == nil {
					continue
				}
				err = args.d.writeToCloudLogging(sys)
				if err != nil {
					log.CtxLogger(ctx).Warnw("Encountered error writing to cloud logging", "error", err)
				}
			}
		}

		log.CtxLogger(ctx).Info("Done SAP System Discovery")

		args.d.systemMu.Lock()
		args.d.systems = sapSystems
		args.d.systemMu.Unlock()

		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("SAP Discovery cancellation requested")
			return
		case <-updateTicker.C:
			continue
		}
	}
}

func (d *Discovery) discoverSAPSystems(ctx context.Context, cp *ipb.CloudProperties, config *cpb.Configuration) []*spb.SapDiscovery {
	sapSystems := []*spb.SapDiscovery{}

	instanceURI := fmt.Sprintf("projects/%s/zones/%s/instances/%s", cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName())
	log.CtxLogger(ctx).Info("Starting SAP Discovery")
	sapDetails := d.SapDiscoveryInterface.DiscoverSAPApps(ctx, d.GetSAPInstances(), config.GetDiscoveryConfiguration())
	log.CtxLogger(ctx).Debugw("SAP Details", "details", sapDetails)
	log.CtxLogger(ctx).Info("Starting host discovery")
	hostResourceNames := d.HostDiscoveryInterface.DiscoverCurrentHost(ctx)
	log.CtxLogger(ctx).Debugw("Host Resource Names", "names", hostResourceNames)
	log.CtxLogger(ctx).Debug("Discovering current host")
	hostResources := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, nil, append([]string{instanceURI}, hostResourceNames...), cp)
	log.CtxLogger(ctx).Debugw("Host Resources", "hostResources", hostResources)
	var instanceResource *spb.SapDiscovery_Resource
	// Find the instance resource
	for _, r := range hostResources {
		if strings.Contains(r.ResourceUri, cp.GetInstanceName()) {
			log.CtxLogger(ctx).Debugf("Instance Resource: %v", r)
			instanceResource = r
			break
		}
	}
	if instanceResource == nil {
		log.CtxLogger(ctx).Debug("No instance resource found")
	}
	for _, s := range sapDetails {
		system := &spb.SapDiscovery{}
		if s.AppComponent != nil {
			log.CtxLogger(ctx).Info("Discovering cloud resources for app")
			appRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, s.AppHosts, cp)
			log.CtxLogger(ctx).Debugf("App Resources: %v", appRes)
			if s.AppOnHost {
				appRes = append(appRes, hostResources...)
				log.CtxLogger(ctx).Debugf("App On Host Resources: %v", appRes)
			}
			if s.AppComponent.GetApplicationProperties().GetNfsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for app NFS")
				nfsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, []string{s.AppComponent.GetApplicationProperties().GetNfsUri()}, cp)
				if len(nfsRes) > 0 {
					appRes = append(appRes, nfsRes...)
					s.AppComponent.GetApplicationProperties().NfsUri = nfsRes[0].GetResourceUri()
				}
			}
			if s.AppComponent.GetApplicationProperties().GetAscsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for app ASCS")
				ascsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, []string{s.AppComponent.GetApplicationProperties().GetAscsUri()}, cp)
				if len(ascsRes) > 0 {
					log.CtxLogger(ctx).Debugw("ASCS Resources", "res", ascsRes)
					appRes = append(appRes, ascsRes...)
					s.AppComponent.GetApplicationProperties().AscsUri = ascsRes[0].GetResourceUri()
				}
			}
			if len(s.AppComponent.GetHaHosts()) > 0 {
				haRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, s.AppComponent.GetHaHosts(), cp)
				// Find the instances
				var haURIs []string
				for _, res := range haRes {
					if res.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
						haURIs = append(haURIs, res.GetResourceUri())
					}
				}
				appRes = append(appRes, haRes...)
				s.AppComponent.HaHosts = haURIs
			}
			s.AppComponent.HostProject = cp.GetNumericProjectId()
			s.AppComponent.Resources = removeDuplicates(appRes)
			system.ApplicationLayer = s.AppComponent
		}
		if s.DBComponent != nil {
			log.CtxLogger(ctx).Info("Discovering cloud resources for database")
			dbRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, s.DBHosts, cp)
			log.CtxLogger(ctx).Debugw("Database Resources", "res", dbRes)
			if s.DBOnHost {
				dbRes = append(dbRes, hostResources...)
			}
			if s.DBComponent.GetDatabaseProperties().GetSharedNfsUri() != "" {
				log.CtxLogger(ctx).Debug("Discovering cloud resources for database NFS")
				nfsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, []string{s.DBComponent.GetDatabaseProperties().GetSharedNfsUri()}, cp)
				if len(nfsRes) > 0 {
					dbRes = append(dbRes, nfsRes...)
					s.DBComponent.GetDatabaseProperties().SharedNfsUri = nfsRes[0].GetResourceUri()
				}
			}
			if len(s.DBComponent.GetHaHosts()) > 0 {
				log.CtxLogger(ctx).Debug("Discovering cloud resources for database HA")
				haRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, s.DBComponent.GetHaHosts(), cp)
				// Find the instances
				var haURIs []string
				for _, res := range haRes {
					if res.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
						haURIs = append(haURIs, res.GetResourceUri())
					}
				}
				dbRes = append(dbRes, haRes...)
				s.DBComponent.HaHosts = haURIs
			}
			for _, r := range dbRes {
				if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
					if r.InstanceProperties == nil {
						r.InstanceProperties = &spb.SapDiscovery_Resource_InstanceProperties{}
					}
					r.InstanceProperties.InstanceRole |= spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE
				}
			}
			log.CtxLogger(ctx).Debug("Done discovering DB")
			s.DBComponent.HostProject = cp.GetNumericProjectId()
			s.DBComponent.Resources = removeDuplicates(dbRes)
			system.DatabaseLayer = s.DBComponent
		}
		if len(s.InstanceProperties) > 0 {
			for _, iProp := range s.InstanceProperties {
				log.CtxLogger(ctx).Debugw("Discovering instance properties", "props", iProp)
				res := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, []string{iProp.VirtualHostname}, cp)
				log.CtxLogger(ctx).Debugf("Discovered instance properties: %s", res)
				if res == nil {
					continue
				}
				for _, r := range res {
					if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
						if r.InstanceProperties == nil {
							r.InstanceProperties = &spb.SapDiscovery_Resource_InstanceProperties{}
						}
						if iProp.VirtualHostname != "" {
							r.InstanceProperties.VirtualHostname = iProp.VirtualHostname
						}
						r.InstanceProperties.InstanceRole |= iProp.InstanceRole
						r.InstanceProperties.AppInstances = append(r.InstanceProperties.AppInstances, iProp.AppInstances...)
						log.CtxLogger(ctx).Debugf("Adding instance properties to resource: %s", r.ResourceUri)
					}
				}
				switch iProp.InstanceRole {
				case spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE:
					log.CtxLogger(ctx).Debug("Instance properties are for a database instance")
					if system.GetDatabaseLayer() != nil {
						system.DatabaseLayer.Resources = removeDuplicates(append(system.GetDatabaseLayer().Resources, res...))
					}
				case spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER:
					fallthrough
				case spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS:
					fallthrough
				case spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS:
					log.CtxLogger(ctx).Debug("Instance properties are for a application instance")
					if system.GetApplicationLayer() != nil {
						system.ApplicationLayer.Resources = removeDuplicates(append(system.GetApplicationLayer().Resources, res...))
					}
				}
			}
		}
		system.WorkloadProperties = s.WorkloadProperties
		system.ProjectNumber = cp.GetNumericProjectId()
		system.UpdateTime = timestamppb.Now()
		sapSystems = append(sapSystems, system)
	}
	log.CtxLogger(ctx).Debug("Done discovering systems")
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
