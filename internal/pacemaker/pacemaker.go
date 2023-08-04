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
// The package implements functions to detect of Pacemaker HA cluster on the
// machine running sap-agent. Package supports both SUSE and RHEL based images.
// The goal of the package is to interact with pacemaker via commands and
// enable sap-agent with this data for WLM metrics and linux cluster related
// process metrics.
package pacemaker

import (
	"context"
	"encoding/xml"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"google3/third_party/sapagent/shared/log"
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

// Data gets the crm_mon data and parses it into the CRMMon struct.
func Data(ctx context.Context) (*CRMMon, error) {
	return data(ctx, commandlineexecutor.ExecuteCommand)
}

func data(ctx context.Context, runner commandlineexecutor.Execute) (*CRMMon, error) {
	result := runner(ctx, commandlineexecutor.Params{
		Executable: "crm_mon",
		Args:       []string{"--as-xml"},
	})
	if result.Error != nil && result.ExecutableFound {
		log.Logger.Errorw("Command 'crm_mon --as-xml' failed", "stdout", result.StdOut, "stderr", result.StdErr, "error", result.Error)
		return nil, result.Error
	}
	return parseCRMMon([]byte(result.StdOut))
}

// ParseCRMMon parses the XML returned by crm_mon command in to
// go structs. Accepts the XML content as byte array.
func parseCRMMon(byteVal []byte) (*CRMMon, error) {
	crm := &CRMMon{}

	if err := xml.Unmarshal(byteVal, crm); err != nil {
		return nil, err
	}
	log.Logger.Debugw("XML parsed to 'crmMon' struct", "crm", crm)
	return crm, nil
}

// Enabled checks if the current instance is part of a Linux pacemaker cluster.
// Returns a bool indicating whether pacemaker cluster is enabled.
func Enabled(crm *CRMMon) bool {
	if crm != nil && len(crm.Nodes) > 0 {
		log.Logger.Info("Pacemaker cluster is configured on current machine.")
		return true
	}
	return false
}

// NodeState returns a map with key as node name in the pacemaker cluster,
// and value as the state string. Returns an error in case of failures.
func NodeState(crm *CRMMon) (map[string]string, error) {
	if crm == nil {
		return nil, nil
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
func ResourceState(crm *CRMMon) ([]Resource, error) {
	if crm == nil {
		return nil, nil
	}
	var rs []Resource
	// Concatenate the three separate resource arrays into one array.
	resources := append(crm.Resources.General, crm.Resources.Group...)
	resources = append(resources, crm.Resources.Clone...)

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
func FailCount(crm *CRMMon) ([]ResourceFailCount, error) {
	if crm == nil {
		return nil, nil
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
func XMLString(ctx context.Context, exec commandlineexecutor.Execute, crmAvailable bool) *string {
	if crmAvailable {
		result := exec(ctx, commandlineexecutor.Params{
			Executable: "cibadmin",
			Args:       []string{"--query"},
		})
		return &result.StdOut
	}
	result := exec(ctx, commandlineexecutor.Params{
		Executable: "pcs",
		Args:       []string{"cluster", "cib"},
	})
	if result.Error != nil {
		return nil
	}
	return &result.StdOut
}
