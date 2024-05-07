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

// Package appsdiscovery contains a set of functionality to discover SAP application details running on the current host, and their related components.
package appsdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

var (
	fsMountRegex              = regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+):(/[a-zA-Z0-9]+)`)
	headerLineRegex           = regexp.MustCompile(`[^-]+`)
	hanaVersionRegex          = regexp.MustCompile(`version:\s+(([0-9]+\.?)+)`)
	netweaverKernelRegex      = regexp.MustCompile(`kernel release\s+([0-9]+)`)
	netweaverPatchNumberRegex = regexp.MustCompile(`patch number\s+([0-9]+)`)
)

const (
	haNodes              = "HANodes:"
	r3transSuccessResult = "R3trans finished (0000)"
	r3transTmpFolder     = "/tmp/r3trans/"
	tmpControlFilePath   = r3transTmpFolder + "export_products.ctl"
	r3transOutputPath    = r3transTmpFolder + "output.txt"
)

type fileReader func(filename string) ([]byte, error)

// SapDiscovery contains variables and methods to discover SAP applications running on the current host.
type SapDiscovery struct {
	Execute    commandlineexecutor.Execute
	FileSystem filesystem.FileSystem
}

// SapSystemDetails contains information about an ASP system running on the current host.
type SapSystemDetails struct {
	AppComponent        *spb.SapDiscovery_Component
	DBComponent         *spb.SapDiscovery_Component
	AppHosts, DBHosts   []string
	AppOnHost, DBOnHost bool
	WorkloadProperties  *spb.SapDiscovery_WorkloadProperties
	InstanceProperties  []*spb.SapDiscovery_Resource_InstanceProperties
}

func removeDuplicates[T comparable](s []T) []T {
	m := make(map[string]bool)
	var o []T
	for _, s := range s {
		sStr := fmt.Sprintf("%v", s)
		if _, ok := m[sStr]; ok {
			continue
		}
		m[sStr] = true
		o = append(o, s)
	}
	return o
}

func mergeAppProperties(old, new *spb.SapDiscovery_Component_ApplicationProperties) *spb.SapDiscovery_Component_ApplicationProperties {
	if old == nil {
		return new
	}
	merged := proto.Clone(old).(*spb.SapDiscovery_Component_ApplicationProperties)
	if old.GetApplicationType() == spb.SapDiscovery_Component_ApplicationProperties_APPLICATION_TYPE_UNSPECIFIED {
		merged.ApplicationType = new.ApplicationType
	}
	if merged.AscsUri == "" {
		merged.AscsUri = new.AscsUri
	}
	if merged.NfsUri == "" {
		merged.NfsUri = new.NfsUri
	}
	return merged
}

func mergeDBProperties(old, new *spb.SapDiscovery_Component_DatabaseProperties) *spb.SapDiscovery_Component_DatabaseProperties {
	if old == nil {
		return new
	}
	merged := proto.Clone(old).(*spb.SapDiscovery_Component_DatabaseProperties)
	if old.GetDatabaseType() == spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED {
		merged.DatabaseType = new.DatabaseType
	}
	if merged.SharedNfsUri == "" {
		merged.SharedNfsUri = new.SharedNfsUri
	}
	if merged.DatabaseSid == "" {
		merged.DatabaseSid = new.DatabaseSid
	}
	if merged.InstanceNumber == "" {
		merged.InstanceNumber = new.InstanceNumber
	}
	return merged
}

func mergeComponent(old, new *spb.SapDiscovery_Component) *spb.SapDiscovery_Component {
	if old == nil {
		return new
	}

	merged := proto.Clone(old).(*spb.SapDiscovery_Component)

	if old.GetProperties() == nil {
		merged.Properties = new.Properties
	} else if new.GetProperties() != nil {
		switch x := old.Properties.(type) {
		case *spb.SapDiscovery_Component_ApplicationProperties_:
			merged.Properties = &spb.SapDiscovery_Component_ApplicationProperties_{
				ApplicationProperties: mergeAppProperties(x.ApplicationProperties, new.GetApplicationProperties()),
			}
		case *spb.SapDiscovery_Component_DatabaseProperties_:
			merged.Properties = &spb.SapDiscovery_Component_DatabaseProperties_{
				DatabaseProperties: mergeDBProperties(x.DatabaseProperties, new.GetDatabaseProperties()),
			}
		}
	}
	if old.GetTopologyType() == spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT ||
		new.GetTopologyType() == spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT {
		merged.TopologyType = spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT
	}

	merged.HaHosts = removeDuplicates(append(merged.GetHaHosts(), new.GetHaHosts()...))

	return merged
}

func mergeWorkloadProperties(old, new *spb.SapDiscovery_WorkloadProperties) *spb.SapDiscovery_WorkloadProperties {
	if new == nil {
		return old
	}
	return new
}

func mergeSystemDetails(old, new SapSystemDetails) SapSystemDetails {
	merged := old
	merged.AppOnHost = old.AppOnHost || new.AppOnHost
	merged.DBOnHost = old.DBOnHost || new.DBOnHost
	merged.AppComponent = mergeComponent(old.AppComponent, new.AppComponent)
	merged.DBComponent = mergeComponent(old.DBComponent, new.DBComponent)
	merged.AppHosts = removeDuplicates(append(merged.AppHosts, new.AppHosts...))
	merged.DBHosts = removeDuplicates(append(merged.DBHosts, new.DBHosts...))
	merged.WorkloadProperties = mergeWorkloadProperties(old.WorkloadProperties, new.WorkloadProperties)
	for _, iProp := range new.InstanceProperties {
		merged.InstanceProperties = append(merged.InstanceProperties, &spb.SapDiscovery_Resource_InstanceProperties{
			VirtualHostname: iProp.VirtualHostname,
			InstanceRole:    iProp.InstanceRole,
		})
	}

	return merged
}

// DiscoverSAPApps attempts to identify the different SAP Applications running on the current host.
func (d *SapDiscovery) DiscoverSAPApps(ctx context.Context, sapApps *sappb.SAPInstances, conf *cpb.DiscoveryConfiguration) []SapSystemDetails {
	sapSystems := []SapSystemDetails{}
	if sapApps == nil {
		log.CtxLogger(ctx).Debugw("No SAP applications found")
		return sapSystems
	}
	log.CtxLogger(ctx).Debugw("SAP Apps found", "apps", sapApps)
	for _, app := range sapApps.Instances {
		switch app.Type {
		case sappb.InstanceType_NETWEAVER:
			log.CtxLogger(ctx).Infow("discovering netweaver", "sid", app.Sapsid)
			sys := d.discoverNetweaver(ctx, app, conf)
			// See if a system with the same SID already exists
			found := false
			for i, s := range sapSystems {
				log.CtxLogger(ctx).Infow("Comparing to system", "dbSid", s.DBComponent.GetSid(), "appSID", s.AppComponent.GetSid())
				if (s.AppComponent.GetSid() == "" || s.AppComponent.GetSid() == sys.AppComponent.GetSid()) &&
					(s.DBComponent.GetSid() == "" || s.DBComponent.GetSid() == sys.DBComponent.GetSid()) {

					log.CtxLogger(ctx).Infow("Found existing system", "sid", sys.AppComponent.GetSid())
					sapSystems[i] = mergeSystemDetails(s, sys)
					sapSystems[i].AppOnHost = true
					found = true
					break
				}
			}
			if !found {
				log.CtxLogger(ctx).Infow("No existing system", "sid", app.Sapsid)
				sys.AppOnHost = true
				sapSystems = append(sapSystems, sys)
			}
		case sappb.InstanceType_HANA:
			log.CtxLogger(ctx).Infow("discovering hana", "sid", app.Sapsid)
			for _, sys := range d.discoverHANA(ctx, app) {
				// See if a system with the same SID already exists
				found := false
				for i, s := range sapSystems {
					if s.DBComponent.GetSid() == sys.DBComponent.GetSid() {
						log.CtxLogger(ctx).Infow("Found existing system", "sid", sys.DBComponent.GetSid())
						sapSystems[i] = mergeSystemDetails(s, sys)
						sapSystems[i].DBOnHost = true
						found = true
						break
					}
				}

				if !found {
					log.CtxLogger(ctx).Infow("No existing system", "sid", sys.DBComponent.GetSid())
					sys.DBOnHost = true
					sapSystems = append(sapSystems, sys)
				}
			}
		}
	}
	return sapSystems
}

func (d *SapDiscovery) discoverNetweaver(ctx context.Context, app *sappb.SAPInstance, conf *cpb.DiscoveryConfiguration) SapSystemDetails {
	appProps := &spb.SapDiscovery_Component_ApplicationProperties{
		ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
	}
	ascsHost, err := d.discoverASCS(ctx, app.Sapsid)
	if err != nil {
		log.CtxLogger(ctx).Infow("Encountered error during call to discoverASCS.", "error", err)
	} else {
		appProps.AscsUri = ascsHost
	}
	nfsHost, err := d.discoverAppNFS(ctx, app.Sapsid)
	if err != nil {
		log.CtxLogger(ctx).Infow("Encountered error during call to discoverAppNFS.", "error", err)
	} else {
		appProps.NfsUri = nfsHost
	}
	kernelVersion, err := d.discoverNetweaverKernelVersion(ctx, app.Sapsid)
	if err != nil {
		log.CtxLogger(ctx).Infow("Encountered error during call to discoverNetweaverKernelVersion.", "error", err)
	} else {
		appProps.KernelVersion = kernelVersion
	}
	ha, haNodes := d.discoverNetweaverHA(ctx, app)
	if !ha {
		haNodes = nil
	}

	ascsHosts, ersHosts, appHosts := d.discoverNetweaverHosts(ctx, app)
	log.CtxLogger(ctx).Debugw("ascsHosts", "ascsHosts", ascsHosts)
	log.CtxLogger(ctx).Debugw("ersHosts", "ersHosts", ersHosts)
	log.CtxLogger(ctx).Debugw("appHosts", "appHosts", appHosts)
	var iProps []*spb.SapDiscovery_Resource_InstanceProperties
	for _, a := range ascsHosts {
		iProps = append(iProps, &spb.SapDiscovery_Resource_InstanceProperties{
			VirtualHostname: a.Name,
			InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
		})
		appProps.AscsInstanceNumber = a.Number
	}

	for _, e := range ersHosts {
		iProps = append(iProps, &spb.SapDiscovery_Resource_InstanceProperties{
			VirtualHostname: e.Name,
			InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
		})
		appProps.ErsInstanceNumber = e.Number
	}

	for _, a := range appHosts {
		iProps = append(iProps, &spb.SapDiscovery_Resource_InstanceProperties{
			VirtualHostname: a.Name,
			InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
			AppInstances:    []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{a},
		})
	}

	details := SapSystemDetails{
		AppComponent: &spb.SapDiscovery_Component{
			Sid: app.Sapsid,
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
				ApplicationProperties: appProps,
			},
			HaHosts: haNodes,
		},
		AppHosts:           haNodes,
		InstanceProperties: iProps,
	}

	log.CtxLogger(ctx).Debugw("Checking config", "config", conf)
	if conf.GetEnableWorkloadDiscovery().GetValue() {
		isABAP, wlProps, err := d.discoverNetweaverABAP(ctx, app)
		if err != nil {
			log.CtxLogger(ctx).Infow("Encountered error during call to discoverNetweaverABAP.", "error", err)
		}
		if isABAP {
			appProps.ApplicationType = spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP
		} else {
			isJava, javaProps, err := d.discoverNetweaverJava(ctx, app)
			if err != nil {
				log.CtxLogger(ctx).Infow("Encountered error during call to discoverNetweaverJava.", "error", err)
			}
			if isJava {
				wlProps = javaProps
				appProps.ApplicationType = spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_JAVA
			}
		}
		details.WorkloadProperties = wlProps
	}

	dbSID, err := d.discoverDatabaseSID(ctx, app.Sapsid)
	if err != nil {
		return details
	}
	details.DBComponent = &spb.SapDiscovery_Component{
		Sid:          dbSID,
		TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
	}
	dbHosts, err := d.discoverAppToDBConnection(ctx, app.Sapsid)
	if err != nil {
		return details
	}

	details.DBHosts = dbHosts
	if len(details.DBHosts) > 1 {
		details.DBComponent.TopologyType = spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT
	}
	return details
}

func (d *SapDiscovery) discoverNetweaverHosts(ctx context.Context, app *sappb.SAPInstance) ([]*spb.SapDiscovery_Resource_InstanceProperties_AppInstance, []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance, []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance) {
	sidLower := strings.ToLower(app.Sapsid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	cmd := commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "sapcontrol", "-nr", app.InstanceNumber, "-function", "GetSystemInstanceList"},
	}
	res := d.Execute(ctx, cmd)
	if res.Error != nil {
		return nil, nil, nil
	}
	log.CtxLogger(ctx).Debugw("GetSystemInstanceList", "stdout", res.StdOut)
	var ascsHosts, ersHosts, appHosts []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance

	lines := strings.Split(res.StdOut, "\n")
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "hostname" {
			continue
		}
		instanceNumber := strings.TrimSpace(parts[1])
		features := strings.TrimSpace(parts[5])
		log.CtxLogger(ctx).Debugw("features", "name", name, "features", features)
		inst := &spb.SapDiscovery_Resource_InstanceProperties_AppInstance{
			Name:   name,
			Number: instanceNumber,
		}
		switch {
		case strings.Contains(features, "MESSAGESERVER"):
			ascsHosts = append(ascsHosts, inst)
		case strings.Contains(features, "ENQREP"):
			ersHosts = append(ersHosts, inst)
		case strings.Contains(features, "ABAP"):
			appHosts = append(appHosts, inst)
		}
	}

	return ascsHosts, ersHosts, appHosts
}

func hanaSystemDetails(app *sappb.SAPInstance, dbProps *spb.SapDiscovery_Component_DatabaseProperties, dbHosts []string, sid, dbProductVersion string) SapSystemDetails {
	t := spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP
	if len(dbHosts) > 1 {
		t = spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT
	}
	return SapSystemDetails{
		DBComponent: &spb.SapDiscovery_Component{
			Sid: sid,
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
				DatabaseProperties: dbProps,
			},
			HaHosts:      app.HanaHaMembers,
			TopologyType: t,
		},
		DBHosts: removeDuplicates(append(dbHosts, app.HanaHaMembers...)),
		WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
				Name:    "SAP HANA",
				Version: dbProductVersion,
			}},
		},
	}
}

func (d *SapDiscovery) discoverHANA(ctx context.Context, app *sappb.SAPInstance) []SapSystemDetails {
	dbHosts, err := d.discoverDBNodes(ctx, app.Sapsid, app.InstanceNumber)
	if err != nil || len(dbHosts) == 0 {
		return nil
	}
	dbNFS, _ := d.discoverDatabaseNFS(ctx)
	version, dbProductVersion, _ := d.discoverHANAVersion(ctx, app)
	dbProps := &spb.SapDiscovery_Component_DatabaseProperties{
		DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
		SharedNfsUri:    dbNFS,
		DatabaseVersion: version,
		DatabaseSid:     app.Sapsid,
		InstanceNumber:  app.InstanceNumber,
	}

	dbSIDs, err := d.discoverHANATenantDBs(ctx, app, dbHosts[0])
	if err != nil {
		log.CtxLogger(ctx).Infow("Encountered error during call to discoverHANATenantDBs. Only discovering primary HANA system.", "error", err)
		return []SapSystemDetails{hanaSystemDetails(app, dbProps, dbHosts, app.Sapsid, dbProductVersion)}
	}

	systems := []SapSystemDetails{}
	for _, s := range dbSIDs {
		systems = append(systems, hanaSystemDetails(app, dbProps, dbHosts, s, dbProductVersion))
	}
	return systems
}

func (d *SapDiscovery) discoverNetweaverHA(ctx context.Context, app *sappb.SAPInstance) (bool, []string) {
	log.CtxLogger(ctx).Debugw("Checking HA nodes", "app", app)
	sidLower := strings.ToLower(app.Sapsid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	params := commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "sapcontrol", "-nr", app.InstanceNumber, "-function", "HAGetFailoverConfig"},
	}
	res := d.Execute(ctx, params)
	if res.Error != nil {
		return false, nil
	}

	ha := strings.Contains(res.StdOut, "HAActive: TRUE")
	if !ha {
		return false, nil
	}

	i := strings.Index(res.StdOut, haNodes)
	i += len(haNodes)
	lines := strings.Split(res.StdOut[i:], "\n")
	var nodes []string
	if len(lines) > 0 {
		for _, n := range strings.Split(lines[0], ",") {
			n = strings.TrimSpace(n)
			if len(n) > 0 {
				nodes = append(nodes, n)
			}
		}
	}
	log.CtxLogger(ctx).Debugw("HA nodes", "nodes", nodes)
	if len(nodes) == 0 {
		log.CtxLogger(ctx).Debug("No HA nodes found in failover config, checking PCS")
		params = commandlineexecutor.Params{
			Executable: "pcs",
			Args:       []string{"config", "show"},
		}
		res = d.Execute(ctx, params)

		lines := strings.Split(res.StdOut, "\n")
		for i, line := range lines {
			if strings.Contains(line, "Pacemaker Nodes:") && i < len(lines)-1 {
				nodes = strings.Split(strings.TrimSpace(lines[i+1]), " ")
				break
			}
		}
	}

	return ha, nodes
}

func (d *SapDiscovery) discoverAppToDBConnection(ctx context.Context, sid string) ([]string, error) {
	sidLower := strings.ToLower(sid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "hdbuserstore", "list", "DEFAULT"},
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Infow("Error retrieving hdbuserstore info", "sid", sid, "error", result.Error, "stdout", result.StdOut, "stderr", result.StdErr)
		return nil, result.Error
	}

	dbHosts := parseDBHosts(result.StdOut)
	if len(dbHosts) == 0 {
		log.CtxLogger(ctx).Infow("Unable to find DB hostname and port in hdbuserstore output", "sid", sid)
		return nil, errors.New("Unable to find DB hostname and port in hdbuserstore output")
	}

	return dbHosts, nil
}

func (d *SapDiscovery) discoverNetweaverJava(ctx context.Context, app *sappb.SAPInstance) (bool, *spb.SapDiscovery_WorkloadProperties, error) {
	sidLower := strings.ToLower(app.Sapsid)
	sidUpper := strings.ToUpper(app.Sapsid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	cmdPath := fmt.Sprintf("/usr/sap/%s/J%s/j2ee/configtool/batchconfig.csh", sidUpper, app.InstanceNumber)
	log.CtxLogger(ctx).Debugw("cmdPath", "cmdPath", cmdPath)
	params := commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, cmdPath, "-task", "get.versions.of.deployed.units"},
	}
	result := d.Execute(ctx, params)
	log.CtxLogger(ctx).Debugw("batchconfig.csh result", "result", result)
	if result.Error != nil {
		return false, nil, result.Error
	}

	return true, parseBatchConfigOutput(ctx, result.StdOut), nil
}

func parseBatchConfigOutput(ctx context.Context, s string) *spb.SapDiscovery_WorkloadProperties {
	scvs := []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{}
	pv := &spb.SapDiscovery_WorkloadProperties_ProductVersion{}
	lines := strings.Split(s, "\n")
	scaLines := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "Listing the SCA versions:") {
			scaLines = true
			continue
		}
		if scaLines && strings.Contains(l, "Listing the versions of SDAs/EARs per SCA:") {
			break
		}
		if !scaLines || len(l) == 0 {
			continue
		}
		// At this point we have actual SCA Lines to parse.
		scv := parseSCALine(l)
		scvs = append(scvs, scv)

		if scv.GetName() == "SERVERCORE" {
			// we can use this for the product version
			pv = &spb.SapDiscovery_WorkloadProperties_ProductVersion{
				Name:    "SAP Netweaver",
				Version: scv.GetVersion(),
			}
		}
	}
	wlProps := &spb.SapDiscovery_WorkloadProperties{
		ProductVersions:           []*spb.SapDiscovery_WorkloadProperties_ProductVersion{pv},
		SoftwareComponentVersions: scvs,
	}
	log.CtxLogger(ctx).Debugw("NW Java Workload Properties", "wlProps", prototext.Format(wlProps))
	return wlProps
}

func parseSCALine(l string) *spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties {
	// Example SCA Line - "ESCONF_BUILDT : 1000.7.50.25.0.20220803154300"
	words := strings.Split(l, " ")
	name := words[0]
	versions := strings.Split(words[len(words)-1], ".")
	// Example Version - "1000.7.50.25.0.20220803154300"
	// Format is - AAAA.B.CC.DD.E.FFFFFFFFFFFFF
	// We utilize B, CC, DD, and E.
	var version, extVersion, typeVal string
	if len(versions) > 1 {
		version = versions[1]
	}
	if len(versions) > 2 {
		version += "." + versions[2]
	}
	if len(versions) > 3 {
		extVersion = versions[3]
	}
	if len(versions) > 4 {
		typeVal = versions[4]
	}
	return &spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
		Name:       name,
		Version:    version,
		ExtVersion: extVersion,
		Type:       typeVal,
	}
}

func (d *SapDiscovery) discoverNetweaverABAP(ctx context.Context, app *sappb.SAPInstance) (bool, *spb.SapDiscovery_WorkloadProperties, error) {
	if err := d.FileSystem.MkdirAll(r3transTmpFolder, 0777); err != nil {
		return false, nil, fmt.Errorf("error creating r3trans tmp folder: %v", err)
	}
	defer d.FileSystem.RemoveAll(r3transTmpFolder)
	if err := d.FileSystem.Chmod(r3transTmpFolder, 0777); err != nil {
		return false, nil, fmt.Errorf("error changing r3trans tmp folder permissions: %v", err)
	}
	sidLower := strings.ToLower(app.Sapsid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)

	// First check if the db is responding
	params := commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "R3trans", "-d", "-w", r3transTmpFolder + "tmp.log"},
	}
	result := d.Execute(ctx, params)
	log.CtxLogger(ctx).Debugw("R3trans result", "result", result)
	if result.Error != nil {
		return false, nil, result.Error
	}

	if !strings.Contains(result.StdOut, r3transSuccessResult) {
		return false, nil, fmt.Errorf("R3trans returned unexpected result, database may not be connected and working:\n%s", result.StdOut)
	}
	log.CtxLogger(ctx).Debugw("DB appears good", "stdOut", result.StdOut)

	// Now create the control file in /tmp
	contents := `EXPORT
	file='/tmp/r3trans/export_products.dat'
	CLIENT=all
	SELECT * FROM PRDVERS
	SELECT * FROM CVERS`
	file, err := d.FileSystem.Create(tmpControlFilePath)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error creating control file", "error", err)
		return false, nil, err
	}
	defer file.Close()
	if _, err = d.FileSystem.WriteStringToFile(file, contents); err != nil {
		log.CtxLogger(ctx).Infow("Error writing control file", "error", err)
		return false, nil, err
	}

	log.CtxLogger(ctx).Debugw("Control file created")

	// Run R3trans with the control file
	params.Args = []string{"-i", "-u", sidAdm, "R3trans", "-w", r3transOutputPath, tmpControlFilePath}
	if result = d.Execute(ctx, params); result.Error != nil {
		log.CtxLogger(ctx).Infow("Error running R3trans with control file", "error", result.Error)
		return false, nil, result.Error
	}

	// Export the data
	params.Args = []string{"-i", "-u", sidAdm, "R3trans", "-w", r3transOutputPath, "-v", "-l", r3transTmpFolder + "export_products.dat"}
	if result = d.Execute(ctx, params); result.Error != nil {
		log.CtxLogger(ctx).Infow("Error exporting data", "error", result.Error)
		return false, nil, result.Error
	}
	log.CtxLogger(ctx).Debugw("R3trans exported data", "stdOut", result.StdOut)

	// Read output.txt
	fileBytes, err := d.FileSystem.ReadFile(r3transOutputPath)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error reading r3trans output file", "r3transOutputPath", r3transOutputPath, "error", err)
		return false, nil, err
	}
	fileString := string(fileBytes[:])
	wlProps := parseR3transOutput(ctx, fileString)
	log.CtxLogger(ctx).Infow("Workload Properties", "wlProps", prototext.Format(wlProps))

	// Command success indicates system is ABAP.
	return true, wlProps, nil
}

func parseR3transOutput(ctx context.Context, s string) (wlProps *spb.SapDiscovery_WorkloadProperties) {
	log.CtxLogger(ctx).Debugw("R3trans exported data", "fileString", s)
	lines := strings.Split(s, "\n")
	cversLines := false
	prdversLines := false
	cversEntries := []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{}
	prdversEntries := []*spb.SapDiscovery_WorkloadProperties_ProductVersion{}
	for _, l := range lines {
		if strings.Contains(l, "CVERS") && strings.Contains(l, "REP") {
			cversLines, prdversLines = true, false
		} else if strings.Contains(l, "PRDVERS") && strings.Contains(l, "REP") {
			prdversLines, cversLines = true, false
		} else if !strings.Contains(l, "**") {
			cversLines, prdversLines = false, false
		} else {
			if cversLines {
				// Example line : "4 ETW000 ** 102 ** SAP_ABA                       750       0025      S"
				cversSplit := strings.Split(l, "**")
				re := regexp.MustCompile("\\s+")
				if len(cversSplit) < 1 {
					log.CtxLogger(ctx).Infow("cvers entry does not have enough fields", "fields", cversSplit, "len(fields)", len(cversSplit))
					continue
				}
				// Taking everything after the "**" which is "SAP_ABA                       750       0025      S"
				// And splitting that on any number of spaces.
				fields := re.Split(strings.TrimSpace(cversSplit[len(cversSplit)-1]), -1)
				cversEntry := map[string]string{}
				if len(fields) > 0 {
					cversEntry["name"] = fields[0]
				}
				if len(fields) > 1 {
					cversEntry["version"] = fields[1]
				}
				if len(fields) > 2 {
					cversEntry["ext_version"] = fields[2]
				}
				if len(fields) == 3 {
					// The last two fields are combined.
					if len(fields[2]) < 1 {
						log.CtxLogger(ctx).Infow("Parsing component encountered ext_version that is too short.", "fields[2]", fields[2], "len(fields[2])", len(fields[2]))
						continue
					}
					// This looks like "0000000000S" where "0000000000" is the ext_version and "S" is the type.
					cversEntry["ext_version"] = string(fields[2][0 : len(fields[2])-1])
					cversEntry["type"] = string(fields[2][len(fields[2])-1])
				}
				if len(fields) > 3 {
					cversEntry["type"] = fields[3]
				}
				cversObj := &spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
					Name:       cversEntry["name"],
					Version:    cversEntry["version"],
					ExtVersion: cversEntry["ext_version"],
					Type:       cversEntry["type"],
				}
				cversEntries = append(cversEntries, cversObj)
			}
			if prdversLines {
				re := regexp.MustCompile("\\s\\s+")
				// Example of what this looks like:
				// 4 ETW000 ** 394 ** 73554900100900000414SAP NETWEAVER   7.5   sap.com    SAP NETWEAVER 7.5       +20220927121631
				// We split on multiple consecutive spaces so that we don't split in the middle of a given field.
				prdversSplit := re.Split(l, -1)
				if len(prdversSplit) < 2 {
					log.CtxLogger(ctx).Infow("prdvers entry does not have enough fields", "fields", prdversSplit, "len(fields)", len(prdversSplit))
					continue
				}
				// Extracting the second to last element here gives us "SAP NETWEAVER 7.5"
				fields := prdversSplit[len(prdversSplit)-2]
				// Find the last space in the product description. This separates the name from the version.
				lastIndex := strings.LastIndex(fields, " ")
				if lastIndex < 0 {
					log.CtxLogger(ctx).Infow("Failed to distinguish name from version for prdvers entry", "fields", fields, "len(fields)", len(fields))
					prdversEntries = append(prdversEntries, &spb.SapDiscovery_WorkloadProperties_ProductVersion{Name: fields})
					continue
				}
				prvdersObj := &spb.SapDiscovery_WorkloadProperties_ProductVersion{
					Name:    fields[:lastIndex],
					Version: fields[lastIndex+1:],
				}
				prdversEntries = append(prdversEntries, prvdersObj)
			}
		}
	}
	return &spb.SapDiscovery_WorkloadProperties{
		ProductVersions:           prdversEntries,
		SoftwareComponentVersions: cversEntries,
	}
}

func parseDBHosts(s string) (dbHosts []string) {
	lines := strings.Split(s, "\n")
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.Index(t, "ENV") < 0 {
			continue
		}

		// Trim up to the first colon
		_, hosts, _ := strings.Cut(t, ":")
		p := strings.Split(hosts, ";")
		// Each semicolon part contains the pattern <host>:<port>
		// The first part will contain "ENV : <host>:port; <host2>:<port2>"
		for _, h := range p {
			c := strings.Split(h, ":")
			if len(c) < 2 {
				continue
			}
			dbHosts = append(dbHosts, strings.TrimSpace(c[0]))
		}
	}
	return dbHosts
}

func (d *SapDiscovery) discoverDatabaseSID(ctx context.Context, appSID string) (string, error) {
	sidLower := strings.ToLower(appSID)
	sidUpper := strings.ToUpper(appSID)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "hdbuserstore", "list"},
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Infow("Error retrieving hdbuserstore info", "sid", appSID, "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr)
		return "", result.Error
	}

	re, err := regexp.Compile(`DATABASE\s*:\s*([a-zA-Z][a-zA-Z0-9]{2})`)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error compiling regex", "error", err)
		return "", err
	}
	sid := re.FindStringSubmatch(result.StdOut)
	if len(sid) > 1 {
		return sid[1], nil
	}

	// No DB SID in userstore, check profiles
	profilePath := fmt.Sprintf("/usr/sap/%s/SYS/profile/*", sidUpper)
	result = d.Execute(ctx, commandlineexecutor.Params{
		Executable:  "sh",
		ArgsToSplit: `-c 'grep "dbid\|dbms/name" ` + profilePath + `'`,
	})

	if result.Error != nil {
		log.CtxLogger(ctx).Infow("Error retrieving sap profile info", "sid", appSID, "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr)
		return "", result.Error
	}

	re, err = regexp.Compile(`(dbid|dbms\/name)\s*=\s*([a-zA-Z][a-zA-Z0-9]{2})`)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error compiling regex", "error", err)
		return "", err
	}
	sid = re.FindStringSubmatch(result.StdOut)
	if len(sid) > 2 {
		log.CtxLogger(ctx).Infow("Found DB SID", "sid", sid[2])
		return sid[2], nil
	}

	return "", errors.New("No database SID found")
}

func (d *SapDiscovery) discoverDBNodes(ctx context.Context, sid, instanceNumber string) ([]string, error) {
	if sid == "" || instanceNumber == "" {
		return nil, errors.New("To discover additional HANA nodes, SID and instance number must be provided")
	}
	sidLower := strings.ToLower(sid)
	sidUpper := strings.ToUpper(sid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	scriptPath := fmt.Sprintf("/usr/sap/%s/HDB%s/exe/python_support/landscapeHostConfiguration.py", sidUpper, instanceNumber)
	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "python", scriptPath},
	})
	// The commandlineexecutor interface returns an error any time the command
	// has an exit status != 0. However, only 0 and 1 are considered true
	// error exit codes for this script.
	if result.Error != nil && result.ExitCode < 2 {
		log.CtxLogger(ctx).Infow("Error running landscapeHostConfiguration.py", "sid", sid, "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr, "exitcode", result.ExitCode)
		return nil, result.Error
	}

	// Example output:
	// | Host        | Host   | Host   | Failover | Remove | Storage   | Storage   | Failover | Failover | NameServer | NameServer | IndexServer | IndexServer | Host    | Host    | Worker  | Worker  |
	// |             | Active | Status | Status   | Status | Config    | Actual    | Config   | Actual   | Config     | Actual     | Config      | Actual      | Config  | Actual  | Config  | Actual  |
	// |             |        |        |          |        | Partition | Partition | Group    | Group    | Role       | Role       | Role        | Role        | Roles   | Roles   | Groups  | Groups  |
	// | ----------- | ------ | ------ | -------- | ------ | --------- | --------- | -------- | -------- | ---------- | ---------- | ----------- | ----------- | ------- | ------- | ------- | ------- |
	// | dru-s4dan   | yes    | ok     |          |        |         1 |         1 | default  | default  | master 1   | master     | worker      | master      | worker  | worker  | default | default |
	// | dru-s4danw1 | yes    | ok     |          |        |         2 |         2 | default  | default  | master 2   | slave      | worker      | slave       | worker  | worker  | default | default |
	// | dru-s4danw2 | yes    | ok     |          |        |         3 |         3 | default  | default  | slave      | slave      | worker      | slave       | worker  | worker  | default | default |
	// | dru-s4danw3 | yes    | ignore |          |        |         0 |         0 | default  | default  | master 3   | slave      | standby     | standby     | standby | standby | default | -       |
	var hosts []string
	lines := strings.Split(result.StdOut, "\n")
	pastHeaders := false
	for _, line := range lines {
		cols := strings.Split(line, "|")
		if len(cols) < 2 {
			continue
		}
		trimmed := strings.TrimSpace(cols[1])
		if trimmed == "" {
			continue
		}
		if !pastHeaders {
			pastHeaders = !headerLineRegex.MatchString(trimmed)
			continue
		}

		hosts = append(hosts, trimmed)
	}
	return hosts, nil
}

func (d *SapDiscovery) discoverASCS(ctx context.Context, sid string) (string, error) {
	// The ASCS of a Netweaver server is identified by the entry "rdisp/mshost" in the DEFAULT.PFL
	profilePath := fmt.Sprintf("/sapmnt/%s/profile/DEFAULT.PFL", sid)
	p := commandlineexecutor.Params{
		Executable: "grep",
		Args:       []string{"rdisp/mshost", profilePath},
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Infow("Error executing grep", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
		return "", res.Error
	}

	lines := strings.Split(res.StdOut, "\n")
	for _, line := range lines {
		parts := strings.Split(line, "=")
		if len(parts) < 2 {
			continue
		}

		return strings.TrimSpace(parts[1]), nil
	}

	return "", errors.New("no ASCS found in default profile")
}

func (d *SapDiscovery) discoverAppNFS(ctx context.Context, sid string) (string, error) {
	// The primary NFS of a Netweaver server is identified as the one that is mounted to the /sapmnt/<SID> directory.
	p := commandlineexecutor.Params{
		Executable: "df",
		Args:       []string{"-h"},
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Infow("Error executing df -h", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
		return "", res.Error
	}

	mntPath := filepath.Join("/sapmnt", sid)
	lines := strings.Split(res.StdOut, "\n")
	for _, line := range lines {
		if strings.Contains(line, mntPath) {
			matches := fsMountRegex.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}

			return matches[1], nil
		}
	}

	return "", errors.New("no NFS found")
}

func (d *SapDiscovery) discoverNetweaverKernelVersion(ctx context.Context, sid string) (string, error) {
	sidLower := strings.ToLower(sid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	p := commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"-i", "-u", sidAdm, "disp+work"},
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Infow("Error executing disp+work command", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
		return "", res.Error
	}

	kernelMatches := netweaverKernelRegex.FindStringSubmatch(res.StdOut)
	if len(kernelMatches) < 2 {
		return "", errors.New("unable to identify Netweaver kernel version")
	}

	kernelNumber, _ := strconv.Atoi(kernelMatches[1])

	patchMatches := netweaverPatchNumberRegex.FindStringSubmatch(res.StdOut)

	if len(patchMatches) < 2 {
		return "", errors.New("unable to identify Netweaver kernel version")
	}
	patchNumber, _ := strconv.Atoi(patchMatches[1])

	version := fmt.Sprintf("SAP Kernel %d Patch %d", kernelNumber, patchNumber)
	return version, nil
}

func (d *SapDiscovery) discoverDatabaseNFS(ctx context.Context) (string, error) {
	// The primary NFS of a Netweaver server is identified as the one that is mounted to the /sapmnt/<SID> directory.
	p := commandlineexecutor.Params{
		Executable: "df",
		Args:       []string{"-h"},
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Infow("Error executing df -h", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
		return "", res.Error
	}

	mntPath := "/hana/shared"
	lines := strings.Split(res.StdOut, "\n")
	for _, line := range lines {
		if strings.Contains(line, mntPath) {
			matches := fsMountRegex.FindStringSubmatch(line)
			if len(matches) < 2 {
				continue
			}

			return matches[1], nil
		}
	}
	return "", errors.New("unable to identify main database NFS")
}

func (d *SapDiscovery) discoverHANAVersion(ctx context.Context, app *sappb.SAPInstance) (string, string, error) {
	log.CtxLogger(ctx).Debug("Entered discoverHANAVersion")
	sidLower := strings.ToLower(app.Sapsid)
	sidUpper := strings.ToUpper(app.Sapsid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	path := fmt.Sprintf("/usr/sap/%s/HDB%s/HDB", sidUpper, app.GetInstanceNumber())
	p := commandlineexecutor.Params{
		Executable: path,
		Args:       []string{"version"},
		User:       sidAdm,
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Infow("Error executing HDB version command", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
		return "", "", res.Error
	}

	match := hanaVersionRegex.FindStringSubmatch(res.StdOut)
	if len(match) < 2 {
		return "", "", errors.New("unable to identify HANA version")
	}

	parts := strings.Split(match[1], ".")
	// Ignore atoi errors since the regex enforces these parts to be numeric.
	majorVersion, _ := strconv.Atoi(parts[0])
	minorVersion, _ := strconv.Atoi(parts[1])
	revision, _ := strconv.Atoi(parts[2])
	revisionMinor, _ := strconv.Atoi(parts[3])

	version := fmt.Sprintf("HANA %d.%d Rev %d", majorVersion, minorVersion, revision)
	s := fmt.Sprintf("%d.%d SPS%02d Rev%d.%02d", majorVersion, minorVersion, int(revision/10), revision, revisionMinor)
	return version, s, nil
}

func (d *SapDiscovery) readAndUnmarshalJson(ctx context.Context, filepath string) (map[string]any, error) {
	file, err := d.FileSystem.ReadFile(filepath)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error reading file", "filepath", filepath, "error", err)
		return nil, err
	}

	data := map[string]any{}
	err = json.Unmarshal(file, &data)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error unmarshalling file", "filepath", filepath, "error", err, "contents", string(file))
		return nil, err
	}

	return data, nil
}

func (d *SapDiscovery) discoverHANATenantDBs(ctx context.Context, app *sappb.SAPInstance, dbHost string) ([]string, error) {
	// The hdb topology containing sid info is contained in the nameserver_topology_HDBHostname.json
	// Abstract path: /hana/shared/SID/HDBXX/HDBHostname/trace/nameserver_topology_HDBHostname.json
	// Concrete example: /hana/shared/DEH/HDB00/dnwh75rdbci/trace/nameserver_topology_dnwh75rdbci.json
	log.CtxLogger(ctx).Debugw("Entered discoverHANATenantDBs")
	instanceID := app.GetInstanceId()
	sidUpper := strings.ToUpper(app.Sapsid)
	topologyPath := fmt.Sprintf("/hana/shared/%s/%s/%s/trace/nameserver_topology_%s.json", sidUpper, instanceID, dbHost, dbHost)
	log.CtxLogger(ctx).Debugw("hdb topology file", "filepath", topologyPath)
	data, err := d.readAndUnmarshalJson(ctx, topologyPath)
	if err != nil {
		return nil, err
	}
	databasesData := map[string]any{}
	if topology, ok := data["topology"]; ok {
		if topologyMap, ok := topology.(map[string]any); ok {
			if databases, ok := topologyMap["databases"]; ok {
				if databasesMap, ok := databases.(map[string]any); ok {
					databasesData = databasesMap
				}
			}
		}
	}
	log.CtxLogger(ctx).Debugw("databasesData", "databasesData", databasesData)
	var dbSids []string
	for _, db := range databasesData {
		if dbMap, ok := db.(map[string]any); ok {
			if dbSid, ok := dbMap["name"]; ok {
				dbSidString, ok := dbSid.(string)
				if ok {
					// Ignore the SYSTEMDB
					if len(dbSidString) == 3 {
						dbSids = append(dbSids, dbSidString)
					}
				}

			}
		}
	}

	log.CtxLogger(ctx).Debugw("End of discoverHANATenantDBs", "dbSids", dbSids)
	return dbSids, nil
}
