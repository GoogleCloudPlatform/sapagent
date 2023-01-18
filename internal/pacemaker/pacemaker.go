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

// Package pacemaker implements Linux Pacemaker cluster related functions.
// The package implements functions to detect of Pacemeaker HA cluster on the
// machine running sap-agent. Package supports both SUSE and RHEL based images.
// The goal of the package is to interact with pacemaker via commands and
// enable sap-agent with this data for WLM metrics and linux cluster related
// process metrics.
package pacemaker

import (
	"encoding/xml"
	"fmt"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

type (
	// CRMNode stores unmarshalled CRMNode metadata and properties.
	CRMNode struct {
		Name             string `xml:"name,attr"`
		ID               int    `xml:"id,attr"`
		Online           bool   `xml:"online,attr"`
		Standby          bool   `xml:"standby,attr"`
		StandbyOnfail    bool   `xml:"standby_onfail,attr"`
		Maintenance      bool   `xml:"maintenance,attr"`
		Pending          bool   `xml:"pending,attr"`
		Unclean          bool   `xml:"unclean,attr"`
		Shutdown         bool   `xml:"shutdown,attr"`
		ExpectedUp       bool   `xml:"expected_up,attr"`
		IsDC             bool   `xml:"is_dc,attr"`
		ResourcesRunning int    `xml:"resources_running,attr"`
		NodeType         string `xml:"type,attr"`
	}

	// CRMResourceNode stores a basic, unmarshalled crm resource node.
	CRMResourceNode struct {
		Name string `xml:"name,attr"`
	}

	// CRMResource stores an unmarshalled crm resource metadata and a corresponding crm resource node.
	CRMResource struct {
		ID    string          `xml:"id,attr"`
		Agent string          `xml:"resource_agent,attr"`
		Role  string          `xml:"role,attr"`
		Node  CRMResourceNode `xml:"node"`
	}

	// CRMResources stores unmarshalled general, group, and clone CRMResource slices.
	CRMResources struct {
		General []CRMResource `xml:"resource"`
		Group   []CRMResource `xml:"group>resource"`
		Clone   []CRMResource `xml:"clone>resource"`
	}

	// CRMResourceHistory stores unmarshalled crm_node cluster resource history.
	CRMResourceHistory struct {
		ID                 string `xml:"id,attr"`
		Orphan             bool   `xml:"orphan,attr"`
		MigrationThreshold string `xml:"migration-threshold,attr"`
		FailCount          int    `xml:"fail-count,attr"`
	}

	// CRMNodeHistory stores unmarshalled crm_node cluster node history.
	CRMNodeHistory struct {
		Name            string               `xml:"name,attr"`
		ResourceHistory []CRMResourceHistory `xml:"resource_history"`
	}

	// CRMMon stores unmarshalled XML output from the crm_mon command.
	CRMMon struct {
		XMLName     xml.Name         `xml:"crm_mon"`
		Nodes       []CRMNode        `xml:"nodes>node"`
		Resources   CRMResources     `xml:"resources"`
		NodeHistory []CRMNodeHistory `xml:"node_history>node"`
	}

	// Resource struct has pacemaker resource details.
	Resource struct {
		Name, Role, Node string
	}

	// ResourceFailCount has pacemaker resource fail-count details.
	ResourceFailCount struct {
		ResourceName, Node string
		FailCount          int
	}
)

// ParseCRMMon parses the XML returned by crm_mon command in to
// go structs. Accepts the XML content as byte array.
func ParseCRMMon(byteVal []byte) (*CRMMon, error) {
	crm := &CRMMon{}

	if err := xml.Unmarshal(byteVal, crm); err != nil {
		return nil, err
	}
	log.Logger.Debugf("XML parsed to 'crmMon' struct as: %v.", crm)
	return crm, nil
}

// Enabled checks if the current instance is part of a Linux pacemaker cluster.
// Returns a bool indicating whether pacemaker cluster is enabled.
func Enabled() bool {
	return IsEnabled(commandlineexecutor.CommandExists, commandlineexecutor.ExpandAndExecuteCommand)
}

// IsEnabled is testable version of Enabled.
// TODO(b/252819120): Test exported functions using test stubs.
func IsEnabled(exists commandlineexecutor.CommandExistsRunner, runner commandlineexecutor.CommandRunner) bool {
	if !exists("crm_mon") {
		log.Logger.Debug("Command 'crm_mon' not found.")
		return false
	}
	stdOut, stdErr, err := runner("crm_mon", "--as-xml")
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Command 'crm_mon --as-xml' failed, stdOut: %s, stdErr: %s.", stdOut, stdErr), log.Error(err))
		return false
	}
	crm, err := ParseCRMMon([]byte(stdOut))
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Failed to parse crm_mon's XML Output: %s", stdOut), log.Error(err))
		return false
	}
	if len(crm.Nodes) > 0 {
		log.Logger.Info("Pacemaker cluster is configured on current machine.")
		return true
	}
	return false
}

// NodeState returns a map with key as node name in the pacemaker cluster,
// and value as the state string. Returns an error in case of failures.
func NodeState() (map[string]string, error) {
	return nState(commandlineexecutor.ExpandAndExecuteCommand)
}

// TODO(b/252819120): Test exported functions using test stubs.
// nState is a testable version of NodeState.
func nState(runner commandlineexecutor.CommandRunner) (map[string]string, error) {
	stdOut, stdErr, err := runner("crm_mon", "--as-xml")
	log.Logger.Debugf("Command 'crm_mon --as-xml' returned stdOut: %s, stdErr: %s.", stdOut, stdErr)
	if err != nil {
		return nil, err
	}
	crm, err := ParseCRMMon([]byte(stdOut))
	if err != nil {
		return nil, err
	}

	ns := make(map[string]string)
	for _, n := range crm.Nodes {
		switch {
		case n.Online:
			ns[n.Name] = "online"
		case n.Standby:
			ns[n.Name] = "standby"
		case n.Shutdown:
			ns[n.Name] = "shutdown"
		case n.Unclean:
			ns[n.Name] = "unclean"
		default:
			ns[n.Name] = "unknown"
		}
	}
	return ns, nil
}

// ResourceState returns a list of Resource structs with one entry per
// pacemaker resource. Returns an error in case of failures.
func ResourceState() ([]Resource, error) {
	return rState(commandlineexecutor.ExpandAndExecuteCommand)
}

// TODO(b/252819120): Test exported functions using test stubs.
// rState is a testable version of ResourceState.
func rState(runner commandlineexecutor.CommandRunner) ([]Resource, error) {
	stdOut, stdErr, err := runner("crm_mon", "--as-xml")
	log.Logger.Debugf("Command 'crm_mon --as-xml' returned stdOut: %s, stdErr: %s.", stdOut, stdErr)
	if err != nil {
		return nil, err
	}
	crm, err := ParseCRMMon([]byte(stdOut))
	if err != nil {
		return nil, err
	}
	// Concatenate the three separate resource arrays into one array.
	resources := append(crm.Resources.General, crm.Resources.Group...)
	resources = append(resources, crm.Resources.Clone...)

	var rs []Resource
	for _, r := range resources {
		rInfo := Resource{
			Name: r.Agent,
			Role: r.Role,
			Node: r.Node.Name,
		}
		rs = append(rs, rInfo)
	}

	return rs, nil
}

// FailCount returns a list of ResourceFailCount structs with one entry per
// pacemaker resource that has a non-zero fail-count.
// Returns (nil, nil) if none of the resources have fail-count field set.
// Returns a (nil, error) in case of failures.
func FailCount(runner commandlineexecutor.CommandRunner) ([]ResourceFailCount, error) {
	stdOut, stdErr, err := runner("crm_mon", "--as-xml")
	log.Logger.Debugf("Command 'crm_mon --as-xml' returned stdOut: %s, stdErr: %s.", stdOut, stdErr)
	if err != nil {
		return nil, err
	}
	crm, err := ParseCRMMon([]byte(stdOut))
	if err != nil {
		return nil, err
	}

	var fc []ResourceFailCount
	for _, n := range crm.NodeHistory {
		for _, r := range n.ResourceHistory {
			if r.FailCount > 0 {
				rInfo := ResourceFailCount{
					ResourceName: r.ID,
					Node:         n.Name,
					FailCount:    r.FailCount,
				}
				fc = append(fc, rInfo)
			}
		}
	}
	return fc, nil
}

/*
XMLString obtains a string of encoded XML data describing the pacemaker metrics.
*/
func XMLString(runner commandlineexecutor.CommandRunner, exists commandlineexecutor.CommandExistsRunner, crmAvailable bool) *string {
	if crmAvailable {
		result, _, _ := runner("cibadmin", "--query")
		return &result
	}
	if exists("pcs") {
		result, _, _ := runner("pcs", "cluster cib")
		return &result
	}
	return nil
}
