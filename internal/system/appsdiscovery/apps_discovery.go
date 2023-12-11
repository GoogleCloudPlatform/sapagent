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

	"google.golang.org/protobuf/proto"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	fsMountRegex     = regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+):(/[a-zA-Z0-9]+)`)
	headerLineRegex  = regexp.MustCompile(`[^-]+`)
	hanaVersionRegex = regexp.MustCompile(`version:\s+(([0-9]+\.?)+)`)
)

const (
	haNodes = "HANodes:"
)

type fileReader func(filename string) ([]byte, error)

// SapDiscovery contains variables and methods to discover SAP applications running on the current host.
type SapDiscovery struct {
	Execute    commandlineexecutor.Execute
	FileReader fileReader
}

// SapSystemDetails contains information about an ASP system running on the current host.
type SapSystemDetails struct {
	AppComponent        *spb.SapDiscovery_Component
	DBComponent         *spb.SapDiscovery_Component
	AppHosts, DBHosts   []string
	AppOnHost, DBOnHost bool
}

func removeDuplicates(s []string) []string {
	m := make(map[string]bool)
	o := []string{}
	for _, s := range s {
		if _, ok := m[s]; ok {
			continue
		}
		m[s] = true
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

	merged.HaHosts = removeDuplicates(append(merged.GetHaHosts(), new.GetHaHosts()...))

	return merged
}

func mergeSystemDetails(old, new SapSystemDetails) SapSystemDetails {
	merged := old
	merged.AppOnHost = old.AppOnHost || new.AppOnHost
	merged.DBOnHost = old.DBOnHost || new.DBOnHost
	merged.AppComponent = mergeComponent(old.AppComponent, new.AppComponent)
	merged.DBComponent = mergeComponent(old.DBComponent, new.DBComponent)
	merged.AppHosts = removeDuplicates(append(merged.AppHosts, new.AppHosts...))
	merged.DBHosts = removeDuplicates(append(merged.DBHosts, new.DBHosts...))

	return merged
}

// DiscoverSAPApps attempts to identify the different SAP Applications running on the current host.
func (d *SapDiscovery) DiscoverSAPApps(ctx context.Context, sapApps *sappb.SAPInstances) []SapSystemDetails {
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
			sys := d.discoverNetweaver(ctx, app)
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
			sys := d.discoverHANA(ctx, app)
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
				log.CtxLogger(ctx).Infow("No existing system", "sid", app.Sapsid)
				sys.DBOnHost = true
				sapSystems = append(sapSystems, sys)
			}
		}
	}
	return sapSystems
}

func (d *SapDiscovery) discoverNetweaver(ctx context.Context, app *sappb.SAPInstance) SapSystemDetails {
	dbSID, err := d.discoverDatabaseSID(ctx, app.Sapsid)
	if err != nil {
		return SapSystemDetails{}
	}
	dbHosts, err := d.discoverAppToDBConnection(ctx, app.Sapsid)
	ascsHost, err := d.discoverASCS(ctx, app.Sapsid)
	nfsHost, err := d.discoverAppNFS(ctx, app.Sapsid)
	appProps := &spb.SapDiscovery_Component_ApplicationProperties{
		ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
		AscsUri:         ascsHost,
		NfsUri:          nfsHost,
	}
	ha, nodes := d.discoverNetweaverHA(ctx, app)
	haNodes := nodes
	if !ha {
		haNodes = nil
	}
	return SapSystemDetails{
		AppComponent: &spb.SapDiscovery_Component{
			Sid: app.Sapsid,
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
				ApplicationProperties: appProps,
			},
			HaHosts: haNodes,
		},
		AppHosts: nodes,
		DBComponent: &spb.SapDiscovery_Component{
			Sid: dbSID,
		},
		DBHosts: dbHosts,
	}
}

func (d *SapDiscovery) discoverHANA(ctx context.Context, app *sappb.SAPInstance) SapSystemDetails {
	dbHosts, err := d.discoverDBNodes(ctx, app.Sapsid, app.InstanceNumber)
	if err != nil {
		return SapSystemDetails{}
	}
	dbNFS, _ := d.discoverDatabaseNFS(ctx)
	version, _ := d.discoverHANAVersion(ctx, app.Sapsid)
	dbProps := &spb.SapDiscovery_Component_DatabaseProperties{
		DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
		SharedNfsUri:    dbNFS,
		DatabaseVersion: version,
	}
	return SapSystemDetails{
		DBComponent: &spb.SapDiscovery_Component{
			Sid: app.Sapsid,
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
				DatabaseProperties: dbProps,
			},
			HaHosts: app.HanaHaMembers,
		},
		DBHosts: removeDuplicates(append(dbHosts, app.HanaHaMembers...)),
	}
}

func (d *SapDiscovery) discoverNetweaverHA(ctx context.Context, app *sappb.SAPInstance) (bool, []string) {
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

	i := strings.Index(res.StdOut, haNodes)
	i += len(haNodes)
	lines := strings.Split(res.StdOut[i:], "\n")
	var nodes []string
	if len(lines) > 0 {
		for _, n := range strings.Split(lines[0], ",") {
			nodes = append(nodes, strings.TrimSpace(n))
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
		log.CtxLogger(ctx).Warnw("Error retrieving hdbuserstore info", "sid", sid, "error", result.Error, "stdout", result.StdOut, "stderr", result.StdErr)
		return nil, result.Error
	}

	dbHosts := parseDBHosts(result.StdOut)
	if len(dbHosts) == 0 {
		log.CtxLogger(ctx).Warnw("Unable to find DB hostname and port in hdbuserstore output", "sid", sid)
		return nil, errors.New("Unable to find DB hostname and port in hdbuserstore output")
	}

	return dbHosts, nil
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
		log.CtxLogger(ctx).Warnw("Error retrieving hdbuserstore info", "sid", appSID, "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr)
		return "", result.Error
	}

	re, err := regexp.Compile(`DATABASE\s*:\s*([a-zA-Z][a-zA-Z0-9]{2})`)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error compiling regex", "error", err)
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
		log.CtxLogger(ctx).Warnw("Error retrieving sap profile info", "sid", appSID, "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr)
		return "", result.Error
	}

	re, err = regexp.Compile(`(dbid|dbms\/name)\s*=\s*([a-zA-Z][a-zA-Z0-9]{2})`)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error compiling regex", "error", err)
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
	command := fmt.Sprintf("-i -u %s python %s", sidAdm, scriptPath)
	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: command,
	})
	// The commandlineexecutor interface returns an error any time the command
	// has an exit status != 0. However, only 0 and 1 are considered true
	// error exit codes for this script.
	if result.Error != nil && result.ExitCode < 2 {
		log.CtxLogger(ctx).Warnw("Error running landscapeHostConfiguration.py", "sid", sid, "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr, "exitcode", result.ExitCode)
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
		log.CtxLogger(ctx).Warnw("Error executing grep", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
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
		log.CtxLogger(ctx).Warnw("Error executing df -h", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
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

func (d *SapDiscovery) discoverDatabaseNFS(ctx context.Context) (string, error) {
	// The primary NFS of a Netweaver server is identified as the one that is mounted to the /sapmnt/<SID> directory.
	p := commandlineexecutor.Params{
		Executable: "df",
		Args:       []string{"-h"},
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Warnw("Error executing df -h", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
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

func (d *SapDiscovery) discoverHANAVersion(ctx context.Context, sid string) (string, error) {
	var version string
	sidLower := strings.ToLower(sid)
	sidAdm := fmt.Sprintf("%sadm", sidLower)
	p := commandlineexecutor.Params{
		Executable: "HDB",
		Args:       []string{"version"},
		User:       sidAdm,
	}
	res := d.Execute(ctx, p)
	if res.Error != nil {
		log.CtxLogger(ctx).Warnw("Error executing HDB version command", "error", res.Error, "stdOut", res.StdOut, "stdErr", res.StdErr, "exitcode", res.ExitCode)
		return "", res.Error
	}

	match := hanaVersionRegex.FindStringSubmatch(res.StdOut)
	if len(match) < 2 {
		return "", errors.New("unable to identify HANA version")
	}

	parts := strings.Split(match[1], ".")
	// Ignore atoi errors since the regex enforces these parts to be numeric.
	majorVersion, _ := strconv.Atoi(parts[0])
	minorVersion, _ := strconv.Atoi(parts[1])
	revision, _ := strconv.Atoi(parts[2])
	version = fmt.Sprintf("HANA %d.%d Rev %d", majorVersion, minorVersion, revision)
	return version, nil
}

func (d *SapDiscovery) readAndUnmarshalJson(ctx context.Context, filepath string) (map[string]any, error) {
	file, err := d.FileReader(filepath)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error reading file", "filepath", filepath, "error", err)
		return nil, err
	}

	data := map[string]any{}
	err = json.Unmarshal(file, &data)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error unmarshalling file", "filepath", filepath, "error", err, "contents", string(file))
		return nil, err
	}

	return data, nil
}

func (d *SapDiscovery) discoverHANATenantDBs(ctx context.Context, app *sappb.SAPInstance, dbHost string, runid string) ([]string, error) {
	// The hdb topology containing sid info is contained in the nameserver_topology_HDBHostname.json
	// Abstract path: /hana/shared/SID/HDBXX/HDBHostname/trace/nameserver_topology_HDBHostname.json
	// Concrete example: /hana/shared/DEH/HDB00/dnwh75rdbci/trace/nameserver_topology_dnwh75rdbci.json
	log.CtxLogger(ctx).Infow("Entered discoverHANATenantDBs", "runid", runid)
	instanceID := app.GetInstanceId()
	sidUpper := strings.ToUpper(app.Sapsid)
	topologyPath := fmt.Sprintf("/hana/shared/%s/%s/%s/trace", sidUpper, instanceID, dbHost, dbHost)
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
	log.CtxLogger(ctx).Infow("databasesData", "databasesData", databasesData)
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

	log.CtxLogger(ctx).Infow("End of discoverHANATenantDBs", "dbSids", dbSids, "runid", runid)
	return dbSids, nil
}
