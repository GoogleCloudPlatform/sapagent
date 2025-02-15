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
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"cloud.google.com/go/logging"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/recovery"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	dwpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/datawarehouse"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

const systemDiscoveryOverride = "/etc/google-cloud-sap-agent/system.json"

// SapSystemDiscoveryInterface is an interface for retrieving the list of SAP systems.
type SapSystemDiscoveryInterface interface {
	GetSAPSystems() []*spb.SapDiscovery
}

// SapAppDiscoveryInterface is an interface for retrieving the list of SAP instances.
type SapAppDiscoveryInterface interface {
	GetSAPInstances() *sappb.SAPInstances
}

// Discovery is a type used to perform SAP System discovery operations.
type Discovery struct {
	WlmService              WlmInterface
	CloudLogInterface       CloudLogInterface
	CloudDiscoveryInterface CloudDiscoveryInterface
	HostDiscoveryInterface  HostDiscoveryInterface
	SapDiscoveryInterface   SapDiscoveryInterface
	AppsDiscovery           func(context.Context, SapSystemDiscoveryInterface) *sappb.SAPInstances
	OSStatReader            workloadmanager.OSStatReader
	FileReader              workloadmanager.ConfigFileReader
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

// CloudLogInterface is exported to be used by the system discovery OTE.
type CloudLogInterface interface {
	Log(e logging.Entry)
	Flush() error
}

// WlmInterface is exported to be used by the system discovery OTE.
type WlmInterface interface {
	WriteInsight(project, location string, writeInsightRequest *dwpb.WriteInsightRequest) error
}

// CloudDiscoveryInterface is exported to be used by the system discovery OTE.
type CloudDiscoveryInterface interface {
	DiscoverComputeResources(context.Context, *spb.SapDiscovery_Resource, string, []string, *ipb.CloudProperties) []*spb.SapDiscovery_Resource
}

// HostDiscoveryInterface is exported to be used by the system discovery OTE.
type HostDiscoveryInterface interface {
	DiscoverCurrentHost(context.Context) []string
}

// SapDiscoveryInterface is exported to be used by the system discovery OTE.
type SapDiscoveryInterface interface {
	DiscoverSAPApps(ctx context.Context, sapApps *sappb.SAPInstances, conf *cpb.DiscoveryConfiguration) []appsdiscovery.SapSystemDetails
}

type loadBalancerGroup struct {
	lbRes        *spb.SapDiscovery_Resource
	instanceURIs []string
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

	if fileInfo, err := args.d.OSStatReader(systemDiscoveryOverride); fileInfo != nil && err == nil {
		args.d.sapInstances = &sappb.SAPInstances{}
		return
	}
	log.CtxLogger(ctx).Info("Starting SAP Instances update")
	updateTicker := time.NewTicker(args.config.GetDiscoveryConfiguration().GetSapInstancesUpdateFrequency().AsDuration())
	for {
		log.CtxLogger(ctx).Info("Updating SAP Instances")
		sapInst := args.d.AppsDiscovery(ctx, args.d)
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
		log.CtxLogger(ctx).Debugw("Discovered SAP Systems", "systems", sapSystems)

		locationParts := strings.Split(cp.GetZone(), "-")
		region := strings.Join([]string{locationParts[0], locationParts[1]}, "-")

		// Write SAP system discovery data only if sap_system_discovery is enabled.
		if args.config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
			log.CtxLogger(ctx).Info("Sending systems to WLM API")
			for _, sys := range sapSystems {
				sys.ProjectNumber = cp.GetNumericProjectId()
				sys.UpdateTime = timestamppb.Now()
				// Remove fields only used for local discovery:
				// resource.instanceProperties.diskDeviceNames
				var comps []*spb.SapDiscovery_Component
				if sys.ApplicationLayer != nil {
					comps = append(comps, sys.ApplicationLayer)
				}
				if sys.DatabaseLayer != nil {
					comps = append(comps, sys.DatabaseLayer)
				}
				for len(comps) > 0 {
					comp := comps[len(comps)-1]
					comps = comps[:len(comps)-1]
					if comp.ReplicationSites != nil {
						for _, site := range comp.ReplicationSites {
							comps = append(comps, site.Component)
						}
					}
					for _, res := range comp.GetResources() {
						if res.GetInstanceProperties() != nil {
							res.InstanceProperties.DiskDeviceNames = nil
						}
					}
				}
				log.CtxLogger(ctx).Debugw("System to send to WLM", "system", sys)
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
					log.CtxLogger(ctx).Infow("Encountered error writing to WLM", "error", err)
				}

				if args.d.CloudLogInterface == nil {
					continue
				}
				err = args.d.writeToCloudLogging(sys)
				if err != nil {
					log.CtxLogger(ctx).Infow("Encountered error writing to cloud logging", "error", err)
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

func (d *Discovery) discoverOverrideSystem(ctx context.Context, overrideFile string, instanceResource *spb.SapDiscovery_Resource) []*spb.SapDiscovery {
	file, err := d.FileReader(overrideFile)
	if err != nil {
		log.CtxLogger(ctx).Warnf("Failed to open override file: %v", err)
		return nil
	}
	defer file.Close()
	var fileBytes []byte
	if fileBytes, err = ioutil.ReadAll(file); err != nil {
		log.CtxLogger(ctx).Warnf("Failed to read override file: %v", err)
		return nil
	}
	log.CtxLogger(ctx).Debugf("File bytes: %s", fileBytes)
	var system spb.SapDiscovery
	if err := protojson.Unmarshal(fileBytes, &system); err != nil {
		log.CtxLogger(ctx).Warnf("Failed to decode override file: %v", err)
		return nil
	}
	// Make the instances without URI refer to this one.
	for _, r := range system.GetApplicationLayer().GetResources() {
		if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE &&
			r.GetResourceUri() == "" {
			r.ResourceUri = instanceResource.GetResourceUri()
			r.InstanceProperties = instanceResource.InstanceProperties
		}
	}
	for _, r := range system.GetDatabaseLayer().GetResources() {
		if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE &&
			r.GetResourceUri() == "" {
			r.ResourceUri = instanceResource.GetResourceUri()
			r.InstanceProperties = instanceResource.InstanceProperties
		}
	}
	return []*spb.SapDiscovery{&system}
}

func (d *Discovery) discoverSAPSystems(ctx context.Context, cp *ipb.CloudProperties, config *cpb.Configuration) []*spb.SapDiscovery {
	instanceURI := fmt.Sprintf("projects/%s/zones/%s/instances/%s", cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName())
	log.CtxLogger(ctx).Debug("Discovering current host")
	hostInstanceResources := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, nil, "", []string{instanceURI}, cp)
	log.CtxLogger(ctx).Debugw("Host Resources", "hostResources", hostInstanceResources)
	var instanceResource *spb.SapDiscovery_Resource
	var instanceNetwork string
	// Find the instance resource and its subnetwork
	for _, r := range hostInstanceResources {
		if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE && strings.Contains(r.ResourceUri, cp.GetInstanceName()) {
			log.CtxLogger(ctx).Debugf("Instance Resource: %v", r)
			instanceResource = r
		}
		if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK {
			instanceNetwork = r.ResourceUri
		}
		if instanceResource != nil && instanceNetwork != "" {
			break
		}
	}
	if instanceResource == nil {
		log.CtxLogger(ctx).Debug("No instance resource found")
	}
	if fileInfo, err := d.OSStatReader(systemDiscoveryOverride); fileInfo != nil && err == nil {
		log.CtxLogger(ctx).Info("Discovering system from override file")
		return d.discoverOverrideSystem(ctx, systemDiscoveryOverride, instanceResource)
	}

	log.CtxLogger(ctx).Info("Starting host discovery")
	hostResourceNames := d.HostDiscoveryInterface.DiscoverCurrentHost(ctx)
	log.CtxLogger(ctx).Debugw("Host Resource Names", "names", hostResourceNames)
	log.CtxLogger(ctx).Infow("Discovering other host resources")
	hostResources := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, hostResourceNames, cp)
	hostResources = removeDuplicates(append(hostResources, hostInstanceResources...))
	log.CtxLogger(ctx).Debugw("Host Resources", "hostResources", hostResources)

	sapSystems := []*spb.SapDiscovery{}

	log.CtxLogger(ctx).Info("Starting SAP Discovery")
	sapDetails := d.SapDiscoveryInterface.DiscoverSAPApps(ctx, d.GetSAPInstances(), config.GetDiscoveryConfiguration())
	log.CtxLogger(ctx).Debugw("SAP Details", "details", sapDetails)
	for _, s := range sapDetails {
		system := &spb.SapDiscovery{}
		if s.AppComponent != nil {
			log.CtxLogger(ctx).Info("Discovering cloud resources for app")
			appRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, s.AppHosts, cp)
			log.CtxLogger(ctx).Debugf("App Resources: %v", appRes)
			if s.AppOnHost {
				appRes = append(appRes, hostResources...)
				log.CtxLogger(ctx).Debugf("App On Host Resources: %v", appRes)
			}
			if s.AppComponent.GetApplicationProperties().GetNfsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for app NFS")
				nfsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, []string{s.AppComponent.GetApplicationProperties().GetNfsUri()}, cp)
				if len(nfsRes) > 0 {
					appRes = append(appRes, nfsRes...)
					s.AppComponent.GetApplicationProperties().NfsUri = nfsRes[0].GetResourceUri()
				}
			}
			if s.AppComponent.GetApplicationProperties().GetAscsUri() != "" {
				log.CtxLogger(ctx).Info("Discovering cloud resources for app ASCS")
				ascsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, []string{s.AppComponent.GetApplicationProperties().GetAscsUri()}, cp)
				if len(ascsRes) > 0 {
					log.CtxLogger(ctx).Debugw("ASCS Resources", "res", ascsRes)
					appRes = append(appRes, ascsRes...)
					s.AppComponent.GetApplicationProperties().AscsUri = ascsRes[0].GetResourceUri()
				}
			}
			if len(s.AppComponent.GetHaHosts()) > 0 {
				haRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, s.AppComponent.GetHaHosts(), cp)
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
			dbRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, s.DBHosts, cp)
			log.CtxLogger(ctx).Debugw("Database Resources", "res", dbRes)
			if s.DBOnHost {
				dbRes = removeDuplicates(append(dbRes, hostResources...))
			}

			if s.DBDiskMap != nil {
				log.CtxLogger(ctx).Debugw("DB Disk Map", "map", s.DBDiskMap)
				for mountPath, deviceName := range s.DBDiskMap {
					// instanceResource instance properties disk mounts is a list of the attached disks with
					// where name is the disk's source and mountPoint is the device name.
					for _, disk := range instanceResource.InstanceProperties.DiskDeviceNames {
						if strings.Contains(deviceName, disk.GetDeviceName()) {
							log.CtxLogger(ctx).Debugw("Disk for device name", "disk", disk, "mountPath", mountPath, "deviceName", deviceName)
							instanceResource.InstanceProperties.DiskMounts = append(instanceResource.InstanceProperties.DiskMounts,
								&spb.SapDiscovery_Resource_InstanceProperties_DiskMount{
									Name:       disk.GetSource(),
									MountPoint: mountPath,
								})
						}
					}
				}
			}
			// Make a resource map for quicker lookup
			resourcesByType := make(map[spb.SapDiscovery_Resource_ResourceKind][]*spb.SapDiscovery_Resource)
			resourceByURI := make(map[string]*spb.SapDiscovery_Resource)
			for _, r := range dbRes {
				resourcesByType[r.GetResourceKind()] = append(resourcesByType[r.GetResourceKind()], r)
				resourceByURI[r.GetResourceUri()] = r
			}
			log.CtxLogger(ctx).Debugw("Resources by type", "resources", resourcesByType)
			log.CtxLogger(ctx).Debugw("Resources by URI", "resources", resourceByURI)
			// Find the load balancer groups
			var lbGroups []loadBalancerGroup
			for _, lb := range resourcesByType[spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE] {
				log.CtxLogger(ctx).Debugw("LB", "lb", lb)
				lbGroup := loadBalancerGroup{lbRes: lb, instanceURIs: []string{}}
				for _, related := range lb.RelatedResources {
					rr := resourceByURI[related]
					if rr.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
						log.CtxLogger(ctx).Debugw("LB Instance", "instance", rr)
						lbGroup.instanceURIs = append(lbGroup.instanceURIs, related)
					} else if rr.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP {
						log.CtxLogger(ctx).Debugw("LB Instance Group", "instanceGroup", rr)
						for _, igr := range rr.RelatedResources {
							igres := resourceByURI[igr]
							if igres.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
								log.CtxLogger(ctx).Debugw("LB Instance in Group", "instance", igres)
								lbGroup.instanceURIs = append(lbGroup.instanceURIs, igr)
							}
						}
					}
				}
				log.CtxLogger(ctx).Debugw("LB Group", "group", lbGroup)
				lbGroups = append(lbGroups, lbGroup)
			}
			log.CtxLogger(ctx).Debugw("LB Groups", "groups", lbGroups)
			if s.DBComponent.GetDatabaseProperties().GetSharedNfsUri() != "" {
				log.CtxLogger(ctx).Debug("Discovering cloud resources for database NFS")
				nfsRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, []string{s.DBComponent.GetDatabaseProperties().GetSharedNfsUri()}, cp)
				if len(nfsRes) > 0 {
					dbRes = append(dbRes, nfsRes...)
					s.DBComponent.GetDatabaseProperties().SharedNfsUri = nfsRes[0].GetResourceUri()
				}
			}
			s.DBComponent.Resources = removeDuplicates(dbRes)
			if s.DBInstance != nil && s.DBInstance.HanaReplicationTree != nil {
				log.CtxLogger(ctx).Debug("Discovering cloud resources for database replication sites")
				primarySite := d.discoverReplicationSite(ctx, s.DBInstance.HanaReplicationTree, s.DBComponent.GetSid(), instanceResource, instanceNetwork, lbGroups, cp)
				primarySite.Component.Properties = s.DBComponent.Properties
				s.DBComponent = primarySite.Component

				// Find the site this instance belongs to.
				addHostResourcesforSite(ctx, s.DBComponent, dbRes, cp)
			}
			for _, r := range s.DBComponent.GetResources() {
				if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
					if r.InstanceProperties == nil {
						r.InstanceProperties = &spb.SapDiscovery_Resource_InstanceProperties{}
					}
					r.InstanceProperties.InstanceRole |= spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE
				}
			}

			log.CtxLogger(ctx).Debug("Done discovering DB")
			s.DBComponent.HostProject = cp.GetNumericProjectId()
			system.DatabaseLayer = s.DBComponent
		}
		if len(s.InstanceProperties) > 0 {
			for _, iProp := range s.InstanceProperties {
				log.CtxLogger(ctx).Debugw("Discovering instance properties", "props", iProp)
				res := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceNetwork, []string{iProp.VirtualHostname}, cp)
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

						appInstances := make(map[string]*spb.SapDiscovery_Resource_InstanceProperties_AppInstance)
						for _, app := range r.InstanceProperties.AppInstances {
							appInstances[app.GetName()] = app
						}
						for _, app := range iProp.AppInstances {
							if _, ok := appInstances[app.GetName()]; !ok {
								r.InstanceProperties.AppInstances = append(r.InstanceProperties.AppInstances, app)
								appInstances[app.GetName()] = app
							}
						}
						log.CtxLogger(ctx).Debugw("Adding instance properties to resource", "resource_uri", r.ResourceUri, "instance_properties", r.InstanceProperties)
					}
				}
				if (iProp.InstanceRole & spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE) != 0 {
					log.CtxLogger(ctx).Debug("Instance properties are for a database instance")
					if system.GetDatabaseLayer() != nil {
						system.DatabaseLayer.Resources = removeDuplicates(append(system.GetDatabaseLayer().Resources, res...))
					}
				}
				if (iProp.InstanceRole & spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS_ERS_APP_SERVER) != 0 {
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

func (d *Discovery) discoverReplicationSite(ctx context.Context, site *sappb.HANAReplicaSite, sid string, instanceResource *spb.SapDiscovery_Resource, instanceSubnetwork string, lbGroups []loadBalancerGroup, cp *ipb.CloudProperties) *spb.SapDiscovery_Component_ReplicationSite {
	siteRes := d.CloudDiscoveryInterface.DiscoverComputeResources(ctx, instanceResource, instanceSubnetwork, []string{site.Name}, cp)
	log.CtxLogger(ctx).Debugw("Site Resources", "site", site.Name, "resources", siteRes)
	repComp := &spb.SapDiscovery_Component{
		Resources: removeDuplicates(siteRes),
		Sid:       sid,
	}

	// Find the region of the instance that the site name corresponds to.
	for _, r := range repComp.Resources {
		if r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
			if r.InstanceProperties == nil {
				r.InstanceProperties = &spb.SapDiscovery_Resource_InstanceProperties{}
			}
			r.InstanceProperties.InstanceRole |= spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE
			if r.GetInstanceProperties().GetVirtualHostname() == site.Name {
				instanceZone := clouddiscovery.ExtractFromURI(r.GetResourceUri(), "zones")
				regionParts := strings.Split(instanceZone, "-")
				instanceRegion := strings.Join([]string{regionParts[0], regionParts[1]}, "-")
				repComp.Region = instanceRegion
			}
		}
	}

	// Check if this site is part of a known load balancer group
	var lbGroup *loadBalancerGroup
resLoop:
	for _, r := range siteRes {
		if r.ResourceKind != spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
			continue
		}
		for _, lb := range lbGroups {
			if slices.Contains(lb.instanceURIs, r.GetResourceUri()) {
				log.CtxLogger(ctx).Debugw("Site is part of a load balancer group", "site", site.Name, "lbGroup", lb.lbRes.GetResourceUri())
				lbGroup = &lb
				break resLoop
			}
		}
	}
	if lbGroup == nil {
		log.CtxLogger(ctx).Debugw("Site not part of existing LB group", "site", site.Name)
	}
	for _, childSite := range site.Targets {
		childRepSite := d.discoverReplicationSite(ctx, childSite, sid, instanceResource, instanceSubnetwork, lbGroups, cp)
		inLbGroup := false
		if lbGroup != nil {
			// Check if the child site is part of the same load balancer group
			for _, r := range childRepSite.Component.Resources {
				if r.ResourceKind != spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
					continue
				}
				if slices.Contains(lbGroup.instanceURIs, r.GetResourceUri()) {
					log.CtxLogger(ctx).Debugw("Child site is part of the same LB group", "site", childSite.Name, "lbGroup", lbGroup.lbRes.GetResourceUri())
					inLbGroup = true
					// Child is part of the same load balancer group, therefore belongs in the same component.
					repComp.Resources = removeDuplicates(append(repComp.Resources, childRepSite.Component.Resources...))
					// Add the child site's replication sites to this component.
					repComp.ReplicationSites = append(repComp.ReplicationSites, childRepSite.Component.ReplicationSites...)
					repComp.HaHosts = append(repComp.HaHosts, lbGroup.instanceURIs...)
					break
				}
			}
		}
		if !inLbGroup {
			log.CtxLogger(ctx).Debugw("Child site not part of the same LB group", "site", childSite.Name)
			// Child is not part of the same load balancer group, therefore belongs in a separate component.
			childRepSite.SourceSite = site.Name

			repComp.ReplicationSites = append(repComp.ReplicationSites, childRepSite)
		}
	}
	log.CtxLogger(ctx).Debugw("Site", "site", site.Name, "component", repComp)
	return &spb.SapDiscovery_Component_ReplicationSite{
		Component: repComp,
	}
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

func addHostResourcesforSite(ctx context.Context, site *spb.SapDiscovery_Component, hostResources []*spb.SapDiscovery_Resource, cp *ipb.CloudProperties) {
	for _, r := range site.Resources {
		instanceName := clouddiscovery.ExtractFromURI(r.GetResourceUri(), "instances")
		if instanceName == cp.GetInstanceName() {
			// Add the host resources to this site.
			log.CtxLogger(ctx).Debugw("Adding host resources to site", "resources", hostResources)
			site.Resources = removeDuplicates(append(site.Resources, hostResources...))
			return
		}
	}
	// Host is not in the site, check children
	for _, rep := range site.ReplicationSites {
		addHostResourcesforSite(ctx, rep.Component, hostResources, cp)
	}
}
