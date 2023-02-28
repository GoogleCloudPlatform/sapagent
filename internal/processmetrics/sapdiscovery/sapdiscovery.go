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

// Package sapdiscovery discovers the SAP Applications and instances running on a given instance.
package sapdiscovery

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"

	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var (
	// haPattern captures the site and site name e.g.
	// site/1/SITE_NAME=gce-1\nlocal_site_id=1\n -> site/1/SITE_NAME=gce-1
	haPattern = regexp.MustCompile(`site/([0-9])/SITE_NAME=(.*)`)

	// sitePattern captures local_site_id e.g.
	// site/1/SITE_NAME=gce-1\nlocal_site_id=1\n -> local_site_id=1
	sitePattern = regexp.MustCompile(`local_site_id=([0-9])`)

	primaryMastersPattern = regexp.MustCompile(`PRIMARY_MASTERS=(.*)`)
	sapInitRunningPattern = regexp.MustCompile(`running$`)

	// netweaverProtocolPortPattern captures protocol and port number.
	// Example: "ParameterValue\nOK\nPROT=HTTP,PORT=8100" is parsed as "PROT=HTTP,PORT=8100".
	netweaverProtocolPortPattern = regexp.MustCompile(`PROT=([a-z|A-Z]+),PORT=([0-9]+)`)

	// sapServicesStartsrvPattern captures the sapstartsrv path in /usr/sap/sapservices.
	// Example: "/usr/sap/DEV/ASCS01/exe/sapstartsrv pf=/usr/sap/DEV/SYS/profile/DEV_ASCS01_dnwh75ldbci -D -u devadm"
	// is parsed as "sapstartsrv pf=/usr/sap/DEV/SYS/profile/DEV_ASCS01".

	sapServicesStartsrvPattern = regexp.MustCompile(`startsrv pf=/usr/sap/([A-Z][A-Z|0-9][A-Z|0-9])[/|a-z|A-Z|0-9]+/profile/([A-Z][A-Z|0-9][A-Z|0-9])_([a-z|A-Z]+)([0-9]+)`)

	// sapServicesProfilePattern captures the sap profile path in /usr/sap/sapservices.
	// Example: "/usr/sap/DEV/ASCS01/exe/sapstartsrv pf=/usr/sap/DEV/SYS/profile/DEV_ASCS01_dnwh75ldbci -D -u devadm"
	// is parsed as "/usr/sap/DEV/SYS/profile/DEV_ASCS01_dnwh75ldbci".
	sapServicesProfilePattern = regexp.MustCompile(`pf=(\S*)?`)

	// libraryPathPattern captures the LD_LIBRARY_PATH from /usr/sap/sapservices.
	// Example: "LD_LIBRARY_PATH=/usr/sap/DEV/ASCS01/exe:$LD_LIBRARY_PATH;export LD_LIBRARY_PATH" is parsed as
	// "/usr/sap/DEV/ASCS01/exe".
	libraryPathPattern = regexp.MustCompile("LD_LIBRARY_PATH=(/usr/sap/[A-Z][A-Z|0-9][A-Z|0-9]/[a-z|A-Z]+[0-9]+/exe)")

	// systemReplicationStatus contains valid return codes for systemReplicationStatus.py.
	// Return codes reference can be found in "SAP HANA System Replication" section in SAP docs.
	// Any code from 10-15 is a valid return code. Anything else needs to be treated as failure.
	// Reference: https://help.sap.com/docs/SAP_HANA_PLATFORM/4e9b18c116aa42fc84c7dbfd02111aba/f6b1bd1020984ee69e902b21b702c096.html?version=2.0.04
	systemReplicationStatus = map[int64]string{
		10: "No System Replication.",
		11: "Error: Error occurred on the connection. Additional details on the error can be found in REPLICATION_STATUS_DETAILS.",
		12: "Unknown: The secondary system did not connect to primary since last restart of the primary system.",
		13: "Initializing: Initial data transfer is in progress. In this state, the secondary is not usable at all.",
		14: "Syncing: The secondary system is syncing again (for example, after a temporary connection loss or restart of the secondary).",
		15: "Active: Initialization or sync with the primary is complete and the secondary is continuously replicating. No data loss will occur in SYNC mode.",
	}
)

type (
	// CommandRunner is a function to execute command. Production callers
	// to pass commandlineexecutor.ExpandAndExecuteCommand while calling
	// this package's APIs.
	CommandRunner     func(string, string) (string, string, error)
	listInstances     func(CommandRunner) ([]*instanceInfo, error)
	cmdExitCode       func(string, string, string) (string, string, int64, error)
	replicationConfig func(string, string, string) (int, []string, int64, error)
	instanceInfo      struct {
		Sid, InstanceName, Snr, ProfilePath, LDLibraryPath string
	}

	// GCEInterface provides an easily testable translation to the secret manager API.
	GCEInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
	}
)

// Metrics contains the TimeSeries values for process metrics.
type Metrics struct {
	TimeSeries *monitoringresourcespb.TimeSeries
}

// SAPApplications Discovers the SAP Application instances.
//
//	Returns a sapb.SAPInstances which is an array of SAP instances running on the given machine.
func SAPApplications() *sapb.SAPInstances {
	return instances(HANAReplicationConfig, listSAPInstances)
}

// instances is a testable version of SAPApplications.
func instances(hrc replicationConfig, list listInstances) *sapb.SAPInstances {
	log.Logger.Info("Discovering SAP Applications.")
	var sapInstances []*sapb.SAPInstance

	hana, err := hanaInstances(hrc, list)
	if err != nil {
		log.Logger.Errorw("Unable to discover HANA instances", log.Error(err))
	} else {
		sapInstances = hana
	}

	netweaver, err := netweaverInstances(list)
	if err != nil {
		log.Logger.Errorw("Unable to discover Netweaver instances", log.Error(err))
	} else {
		sapInstances = append(sapInstances, netweaver...)
	}
	return &sapb.SAPInstances{
		Instances:          sapInstances,
		LinuxClusterMember: pacemaker.Enabled(),
	}
}

// hanaInstances returns list of SAP HANA Instances present on the machine.
// Returns error in case of failures.
func hanaInstances(hrc replicationConfig, list listInstances) ([]*sapb.SAPInstance, error) {
	log.Logger.Info("Discovering SAP HANA instances.")
	sapServicesEntries, err := list(commandlineexecutor.ExpandAndExecuteCommand)
	if err != nil {
		return nil, err
	}

	var instances []*sapb.SAPInstance

	for _, entry := range sapServicesEntries {
		log.Logger.Infow("Processing SAP Instance", "instance", entry)
		if entry.InstanceName != "HDB" {
			log.Logger.Debugw("Instance is not SAP HANA", "instance", entry)
			continue
		}

		instanceID := entry.InstanceName + entry.Snr
		user := strings.ToLower(entry.Sid) + "adm"
		siteID, HAMembers, _, err := hrc(user, entry.Sid, instanceID)
		if err != nil {
			log.Logger.Debugw("Failed to get HANA HA configuration for instance", "instanceid", instanceID, "error", err)
			siteID = -1 // INSTANCE_SITE_UNDEFINED
		}

		instance := &sapb.SAPInstance{
			Sapsid:         entry.Sid,
			InstanceNumber: entry.Snr,
			Type:           sapb.InstanceType_HANA,
			Site:           HANASite(siteID),
			HanaHaMembers:  HAMembers,
			User:           user,
			InstanceId:     instanceID,
			ProfilePath:    entry.ProfilePath,
			LdLibraryPath:  entry.LDLibraryPath,
			SapcontrolPath: fmt.Sprintf("%s/sapcontrol", entry.LDLibraryPath),
		}

		instances = append(instances, instance)
	}
	log.Logger.Infow("Found SAP HANA instances", "count", len(instances), "instances", instances)
	return instances, nil
}

// HANAReplicationConfig discovers the HANA High Availability configuration.
// Returns:
// HANA HA site as int.
//
//	0 == STANDALONE MODE
//	1 == HANA PRIMARY
//	2 == HANA SECONDARY
//
// HANA HA member nodes as array of strings {PRIMARY_NODE, SECONDARY_NODE}.
// Exit status of systemReplicationStatus.py as int64.
func HANAReplicationConfig(user, sid, instID string) (site int, HAMembers []string, exitStatus int64, err error) {
	return readReplicationConfig(user, sid, instID, commandlineexecutor.ExpandAndExecuteCommandAsUserExitCode)
}

// readReplicationConfig is a testable version of HANAReplicationConfig.
func readReplicationConfig(user, sid, instID string, runCmd cmdExitCode) (site int, HAMembers []string, exitStatus int64, err error) {
	cmd := fmt.Sprintf("/usr/sap/%s/%s/HDBSettings.sh", sid, instID)
	args := "systemReplicationStatus.py --sapcontrol=1"
	out, _, exitStatus, err := runCmd(user, cmd, args)
	if err != nil {
		log.Logger.Debugw("Failed to get SAP HANA Replication Status", log.Error(err))
		return 0, nil, 0, err
	}

	message, ok := systemReplicationStatus[exitStatus]
	if !ok {
		return 0, nil, exitStatus, fmt.Errorf("invalid return code: %d from systemReplicationStatus.py", exitStatus)
	}
	log.Logger.Debugw("Tool systemReplicationStatus.py returned", "exitstatus", exitStatus, "message", message)

	log.Logger.Debugw("SAP HANA Replication Config result", "out", out)
	match := sitePattern.FindStringSubmatch(out)
	if len(match) != 2 {
		log.Logger.Errorw("Error determining SAP HANA Site for instance", "instanceid", instID)
		return 0, nil, 0, fmt.Errorf("error determining SAP HANA Site for instance: %s", instID)
	}
	site, err = strconv.Atoi(match[1])
	if err != nil {
		log.Logger.Debugw("Failed to get the site info for SAP HANA", log.Error(err))
		return 0, nil, 0, err
	}

	if site == 0 {
		log.Logger.Debugw("HANA instance is in standalone mode for instance", "instanceid", instID)
		return site, nil, exitStatus, nil
	}

	haSites := haPattern.FindAllStringSubmatch(out, -1)
	if len(haSites) < 1 {
		log.Logger.Debugw("Error determining SAP HANA HA members for instance", "instanceid", instID)
		return site, nil, exitStatus, fmt.Errorf("error determining SAP HANA HA members for instance: %s", instID)
	}

	if len(haSites) > 1 {
		site1, site2 := haSites[0][2], haSites[1][2]
		if haSites[0][1] == "1" {
			HAMembers = []string{site1, site2}
		} else {
			HAMembers = []string{site2, site1}
		}
		log.Logger.Debugw("Current node is primary. SAP HANA HA Members are", "hamembers", HAMembers)
	} else {
		match := primaryMastersPattern.FindStringSubmatch(out)
		if len(match) < 2 {
			log.Logger.Debugw("Error determining SAP HANA HA - Primary for instance", "instanceid", instID)
			return 0, nil, exitStatus, fmt.Errorf("error determining SAP HANA HA - Primary for instance: %s", instID)
		}
		HAMembers = []string{match[1], haSites[0][2]}
		log.Logger.Debugw("Current node is secondary. SAP HANA HA Members are", "hamembers", HAMembers)
	}

	return site, HAMembers, exitStatus, nil
}

// HANASite maps an integer to SAP Instance Site Type.
func HANASite(mode int) sapb.InstanceSite {
	sites := map[int]sapb.InstanceSite{
		0: sapb.InstanceSite_HANA_STANDALONE,
		1: sapb.InstanceSite_HANA_PRIMARY,
		2: sapb.InstanceSite_HANA_SECONDARY,
	}
	if site, ok := sites[mode]; ok {
		return site
	}
	return sapb.InstanceSite_INSTANCE_SITE_UNDEFINED
}

// listSAPInstances returns list of SAP Instances present on the machine.
// The list is derived from '/usr/sap/sapservices' file.
func listSAPInstances(runner CommandRunner) ([]*instanceInfo, error) {
	var sapServicesEntries []*instanceInfo
	stdOut, stdErr, err := runner("grep", "'pf=' /usr/sap/sapservices")
	log.Logger.Debugw("`grep 'pf=' /usr/sap/sapservices` returned", "stdout", stdOut, "stderr", stdErr, "error", err)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSuffix(stdOut, "\n"), "\n")
	for _, line := range lines {
		path := sapServicesStartsrvPattern.FindStringSubmatch(line)
		if len(path) != 5 {
			log.Logger.Debugw("No SAP instance found", "line", line, "match", path)
			continue
		}

		profile := sapServicesProfilePattern.FindStringSubmatch(line)
		if len(profile) != 2 {
			log.Logger.Debugw("No SAP instance profile found", "line", line, "match", profile)
			continue
		}

		entry := &instanceInfo{
			Sid:          path[1],
			InstanceName: path[3],
			Snr:          path[4],
			ProfilePath:  profile[1],
		}

		entry.LDLibraryPath = fmt.Sprintf("/usr/sap/%s/%s%s/exe", entry.Sid, entry.InstanceName, entry.Snr)
		libraryPath := libraryPathPattern.FindStringSubmatch(line)
		if len(libraryPath) == 2 {
			log.Logger.Debugw("Overriding SAP LD_LIBRARY_PATH with value found", "line", line)
			entry.LDLibraryPath = libraryPath[1]
		}
		log.Logger.Infow("Found SAP Instance", "entry", entry)
		sapServicesEntries = append(sapServicesEntries, entry)
	}
	return sapServicesEntries, nil
}

// netweaverInstances returns list of SAP Netweaver instances present on the machine.
func netweaverInstances(list listInstances) ([]*sapb.SAPInstance, error) {
	var instances []*sapb.SAPInstance
	log.Logger.Info("Discovering SAP NetWeaver instances.")

	sapServicesEntries, err := list(commandlineexecutor.ExpandAndExecuteCommand)
	if err != nil {
		return nil, err
	}
	for _, entry := range sapServicesEntries {
		log.Logger.Debugw("Processing SAP Instance", "entry", entry)

		instanceID := entry.InstanceName + entry.Snr
		user := strings.ToLower(entry.Sid) + "adm"

		instance := &sapb.SAPInstance{
			Sapsid:         entry.Sid,
			InstanceNumber: entry.Snr,
			User:           user,
			InstanceId:     instanceID,
			ProfilePath:    entry.ProfilePath,
			LdLibraryPath:  entry.LDLibraryPath,
			SapcontrolPath: fmt.Sprintf("%s/sapcontrol", entry.LDLibraryPath),
		}

		instance.NetweaverHttpPort, instance.Type, instance.Kind = findPort(instance, entry.InstanceName)
		if instance.GetType() == sapb.InstanceType_NETWEAVER {
			instance.NetweaverHealthCheckUrl, instance.ServiceName, err = buildURLAndServiceName(entry.InstanceName, instance.NetweaverHttpPort)
			if err != nil {
				log.Logger.Debugw("Could not build Netweaver URL for health check", log.Error(err))
			}
			instances = append(instances, instance)
		}
	}
	log.Logger.Infow("Found SAP NetWeaver instances", "count", len(instances), "instances", instances)
	return instances, nil
}

// findPort uses the SAP instanceName to find the server HTTP port.
func findPort(instance *sapb.SAPInstance, instanceName string) (string, sapb.InstanceType, sapb.InstanceKind) {
	var (
		httpPort     string
		instanceType sapb.InstanceType = sapb.InstanceType_INSTANCE_TYPE_UNDEFINED
		instanceKind sapb.InstanceKind = sapb.InstanceKind_INSTANCE_KIND_UNDEFINED
		err          error
	)

	switch instanceName {
	case "ASCS", "SCS":
		instanceType = sapb.InstanceType_NETWEAVER
		instanceKind = sapb.InstanceKind_CS
		httpPort, err = serverPortFromSAPProfile(instance, "ms")
		if err != nil {
			log.Logger.Debugw("The ms HTTP port not found, set to default: '81<snr>.'", "instancename", instanceName)
			httpPort = "81" + instance.GetInstanceNumber()
		}
	case "J", "JC", "D", "DVEBMGS":
		instanceType = sapb.InstanceType_NETWEAVER
		instanceKind = sapb.InstanceKind_APP
		httpPort, err = serverPortFromSAPProfile(instance, "icm")
		if err != nil {
			log.Logger.Debugw("The icm HTTP port not found, set to default: '5<snr>00.'", "instancename", instanceName)
			httpPort = "5" + instance.GetInstanceNumber() + "00"
		}
	case "ERS":
		log.Logger.Debugw("This is an Enqueue Replication System.", "instancename", instanceName)
		instanceType = sapb.InstanceType_NETWEAVER
		instanceKind = sapb.InstanceKind_ERS
	case "HDB":
		log.Logger.Debugw("This is a HANA instance.", "instancename", instanceName)
		instanceType = sapb.InstanceType_HANA
	default:
		log.Logger.Debugw("Unknown instance", "instancename", instanceName)
	}
	return httpPort, instanceType, instanceKind
}

// serverPortFromSAPProfile returns the HTTP port using `sapcontrol -function ParameterValue`.
func serverPortFromSAPProfile(instance *sapb.SAPInstance, prefix string) (string, error) {
	// Check if any of server_port_0 thru server_port_9 are configured for HTTP.
	// Reference: "Generic Profile Parameters with Ending _<xx>" section in SAP NetWeaver documentation.
	// (link: https://help.sap.com/doc/saphelp_nw74/7.4.16/en-us/c4/1839a549b24fef92860134ce6af271/frameset.htm)

	for i := 0; i < 10; i++ {
		readPortRunner := &commandlineexecutor.Runner{
			User:       instance.GetUser(),
			Executable: instance.GetSapcontrolPath(),
			Args:       fmt.Sprintf("%s -nr %s -function ParameterValue %s/server_port_%d", instance.GetSapcontrolPath(), instance.GetInstanceNumber(), prefix, i),
			Env:        []string{"LD_LIBRARY_PATH=" + instance.GetLdLibraryPath()},
		}
		port, err := parseHTTPPort(readPortRunner)
		if err != nil {
			log.Logger.Debugw("Server port is not configured for HTTP", "port", fmt.Sprintf("%s/server_port_%d", prefix, i), "error", err)
			continue
		}
		return port, nil
	}
	return "", fmt.Errorf("the HTTP port is not configured for instance : %s", instance.GetInstanceId())
}

// parseHTTPPort parses the output of sapcontrol command for HTTP port.
// Returns HTTP port on success, error if current parameter is not coonfigured for HTTP.
func parseHTTPPort(r sapcontrol.RunnerWithEnv) (port string, err error) {
	stdOut, stdErr, _, err := r.RunWithEnv()
	log.Logger.Debugw("Sapcontrol returned", "stdout", stdOut, "stderr", stdErr, "error", err)
	if err != nil {
		return "", err
	}

	match := netweaverProtocolPortPattern.FindStringSubmatch(stdOut)
	if len(match) != 3 {
		return "", fmt.Errorf("the port is not configured for HTTP")
	}

	protocol, port := match[1], match[2]
	log.Logger.Debugw("Found protocol on port", "protocol", protocol, "port", port)
	if protocol == "HTTP" && port != "0" {
		return port, nil
	}
	return "", fmt.Errorf("the port is not configured for HTTP")
}

// buildURLAndServiceName builds the healthcheck URLs bases on SAP Instance type.
func buildURLAndServiceName(instanceName, HTTPPort string) (url, serviceName string, err error) {
	if HTTPPort == "" {
		return "", "", fmt.Errorf("empty value for HTTP port")
	}

	switch instanceName {
	case "ASCS", "SCS":
		url = fmt.Sprintf("http://localhost:%s/msgserver/text/logon", HTTPPort)
		serviceName = "SAP-CS" // Central Services
	case "D", "DVEBMGS":
		url = fmt.Sprintf("http://localhost:%s/sap/public/icman/ping", HTTPPort)
		serviceName = "SAP-ICM-ABAP"
	case "J", "JC":
		url = fmt.Sprintf("http://localhost:%s/sap/admin/public/images/sap.png", HTTPPort)
		serviceName = "SAP-ICM-Java"
	default:
		return "", "", fmt.Errorf("unknown SAP instance type")
	}
	return url, serviceName, nil
}

// sapInitRunning returns a bool indicating if sapinit is running.
// Returns an error in case of failures.
func sapInitRunning(runner CommandRunner) (bool, error) {
	stdOut, stdErr, err := runner("/usr/sap/hostctrl/exe/sapinit", "status")
	log.Logger.Debugw("`/usr/sap/hostctrl/exe/sapinit status` returned", "stdout", stdOut, "stderr", stdErr, "error", err)
	if err != nil {
		return false, err
	}

	match := sapInitRunningPattern.FindStringSubmatch(stdOut)
	if match == nil {
		return false, nil
	}
	return true, nil
}

// ReadHANACredentials returns the HANA DB user and password key from configuration.
func ReadHANACredentials(ctx context.Context, projectID string, hanaConfig *cpb.HANAMetricsConfig, gceService GCEInterface) (user, password string, err error) {
	// Value hana_db_user must be set to collect HANA DB query metrics.
	user = hanaConfig.GetHanaDbUser()
	if user == "" {
		log.Logger.Info("Using default value for hana_db_user.")
		user = "SYSTEM"
	}

	// Either hana_db_password or hana_db_password_secret_name must be set.
	if hanaConfig.GetHanaDbPassword() == "" && hanaConfig.GetHanaDbPasswordSecretName() == "" {
		return "", "", fmt.Errorf("both hana_db_password and hana_db_password_secret_name are empty")
	}

	if hanaConfig.GetHanaDbPassword() != "" {
		return user, hanaConfig.GetHanaDbPassword(), nil
	}

	password, err = gceService.GetSecret(ctx, projectID, hanaConfig.GetHanaDbPasswordSecretName())
	if err != nil {
		return "", "", err
	}
	return user, password, nil
}
