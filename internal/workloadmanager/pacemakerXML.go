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

package workloadmanager

import (
	"encoding/xml"

	"google3/third_party/sapagent/shared/log"
)

/*
CIB (Cluster Information Base) stores metadata defining a pacemaker XML CIB object cluster.
*/
type CIB struct {
	CRMFeatureSet  string         `xml:"crm_feature_set,attr"`
	ValidateWith   string         `xml:"validate-with,attr"`
	Epoch          int            `xml:"epoch,attr"`
	NumUpdates     int            `xml:"num_updates,attr"`
	AdminEpoch     int            `xml:"admin_epoch,attr"`
	CIBLastWritten string         `xml:"cib-last-written,attr"`
	UpdateOrigin   string         `xml:"update-origin,attr"`
	UpdateClient   string         `xml:"update-client,attr"`
	UpdateUser     string         `xml:"update-user,attr"`
	HaveQuorum     int            `xml:"have-quorum,attr"`
	DCUUID         int            `xml:"dc-uuid,attr"`
	Configuration  Configuration  `xml:"configuration"`
	Status         []CIBNodeState `xml:"status>node_state"`
}

/*
Configuration store CIB object configuration metadata.
*/
type Configuration struct {
	Nodes       []CIBNode          `xml:"nodes>node"`
	CRMConfig   CRMConfig          `xml:"crm_config"`
	Resources   Resources          `xml:"resources"`
	Constraints Constraints        `xml:"constraints"`
	RSCDefaults ClusterPropertySet `xml:"rsc_defaults>meta_attributes"`
	OPDefaults  ClusterPropertySet `xml:"op_defaults>meta_attributes"`
}

/*
Resources stores metadata defining the node structure of the current CIB object (nodes, groups, etc).
*/
type Resources struct {
	Primitives []PrimitiveClass `xml:"primitive"`
	Group      Group            `xml:"group"`
	Clone      Clone            `xml:"clone"`
	Master     Clone            `xml:"master"`
}

/*
Clone stores overall data defining a group of cloned nodes and their metadata (also used to define
a node master/clone relation).
*/
type Clone struct {
	ID         string             `xml:"id,attr"`
	Attributes ClusterPropertySet `xml:"meta_attributes"`
	Primitives []PrimitiveClass   `xml:"primitive"`
}

/*
Group defines a specific and identifiable collection of CIB primitive objects.
*/
type Group struct {
	ID         string           `xml:"id,attr"`
	Primitives []PrimitiveClass `xml:"primitive"`
}

/*
Constraints store data defining resource constraints applicable to the current CIB node.
*/
type Constraints struct {
	RSCLocations  []RSCLocation `xml:"rsc_location"`
	RSCColocation RSCColocation `xml:"rsc_colocation"`
	RSCOrder      RSCOrder      `xml:"rsc_order"`
}

/*
RSCLocation defines a constraint's resource node location.
*/
type RSCLocation struct {
	ID    string `xml:"id,attr"`
	RSC   string `xml:"rsc,attr"`
	Score string `xml:"score,attr"`
	Node  string `xml:"node,attr"`
}

/*
RSCColocation defines the co-locations (two or more nodes located together) of resources (typically
used for master/clone nodes).
*/
type RSCColocation struct {
	ID          string `xml:"id,attr"`
	Score       string `xml:"score,attr"`
	RSC         string `xml:"rsc,attr"`
	RSCRole     string `xml:"rsc-role,attr"`
	WithRSC     string `xml:"with-rsc,attr"`
	WithRSCRole string `xml:"with-rsc-role,attr"`
}

/*
RSCOrder defines the order in which resource constraints apply
*/
type RSCOrder struct {
	ID    string `xml:"id,attr"`
	Kind  string `xml:"kind,attr"`
	First string `xml:"first,attr"`
	Then  string `xml:"then,attr"`
}

/*
CRMConfig stores the cluster properties that apply to the current CRM node.
*/
type CRMConfig struct {
	ClusterPropertySets []ClusterPropertySet `xml:"cluster_property_set"`
}

/*
ClusterPropertySet defines a set of identifiable name/value pairs
*/
type ClusterPropertySet struct {
	ID      string   `xml:"id,attr"`
	NVPairs []NVPair `xml:"nvpair"`
}

/*
PrimitiveClass defines a primitive node instance, its individual properties, and a list of
operations it can perform.
*/
type PrimitiveClass struct {
	Class              string             `xml:"class,attr"`
	ID                 string             `xml:"id,attr"`
	ClassType          string             `xml:"type,attr"`
	Provider           string             `xml:"provider,attr"`
	InstanceAttributes ClusterPropertySet `xml:"instance_attributes"`
	Operations         []Op               `xml:"operations>op"`
}

/*
Op defines an operation entry.
*/
type Op struct {
	ID       string `xml:"id,attr"`
	Interval string `xml:"interval,attr"`
	Name     string `xml:"name,attr"`
	Timeout  string `xml:"timeout,attr"`
}

/*
NVPair stores a generic, individual mapping of name/value pairs with an additional ID value.
*/
type NVPair struct {
	ID    string `xml:"id,attr"`
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

/*
CIBNode stores the properties and attributes of an individual CIB configuration node (see
Configuration struct).
*/
type CIBNode struct {
	ID                 string             `xml:"id,attr"`
	Uname              string             `xml:"uname,attr"`
	InstanceAttributes ClusterPropertySet `xml:"instance_attributes"`
}

/*
CIBNodeState stores the statuses of individual CIB Nodes (see Configuration struct).
*/
type CIBNodeState struct {
	ID                  string              `xml:"id,attr"`
	Uname               string              `xml:"uname,attr"`
	InCCM               bool                `xml:"in_ccm,attr"`
	CRMD                string              `xml:"crmd,attr"`
	CRMDebugOrigin      string              `xml:"crm-debug-origin,attr"`
	Join                string              `xml:"join,attr"`
	Expected            string              `xml:"expected,attr"`
	TransientAttributes TransientAttributes `xml:"transient_attributes"`
	LRM                 LRM                 `xml:"lrm"`
}

/*
TransientAttributes store an additional layer of ID's cluster attributes used by a CIB node state
struct
*/
type TransientAttributes struct {
	ID                 string             `xml:"id,attr"`
	InstanceAttributes ClusterPropertySet `xml:"instance_attributes"`
}

/*
LRM (Local Resource Manager) stores the collective LRM status information with an additional ID value.
*/
type LRM struct {
	ID           string        `xml:"id,attr"`
	LRMResources []LRMResource `xml:"lrm_resources>lrm_resource"`
}

/*
LRMResource stores the status information of an individual LRM CIB resource.
*/
type LRMResource struct {
	ID            string     `xml:"id,attr"`
	ResourceType  string     `xml:"type,attr"`
	ResrouceClass string     `xml:"class,attr"`
	Provider      string     `xml:"provider,attr"`
	LRMRscOps     []LRMRSCOp `xml:"lrm_rsc_op"`
}

/*
LRMRSCOp stores the state of an individual LRM resource operation.
*/
type LRMRSCOp struct {
	ID              string `xml:"id,attr"`
	OperationKey    string `xml:"operation_key,attr"`
	Operation       string `xml:"operation,attr"`
	CRMDebugOrigin  string `xml:"crm-debug-origin,attr"`
	CRMFeatureSet   string `xml:"crm_feature_set,attr"`
	TransitionKey   string `xml:"transition-key,attr"`
	TransitionMagic string `xml:"transition-magic,attr"`
	ExitReason      string `xml:"exit-reason,attr"`
	OnNode          string `xml:"on_node,attr"`
	CallID          string `xml:"call-id,attr"`
	RCCode          int    `xml:"rc-code,attr"`
	OpStatus        int    `xml:"op-status,attr"`
	Interval        int    `xml:"interval,attr"`
	LastRun         int64  `xml:"last-run,attr"`
	LastRCChange    int64  `xml:"last-rc-change,attr"`
	ExecTime        int    `xml:"exec-time,attr"`
	QueueTime       int    `xml:"queue-time,attr"`
	OpDigest        string `xml:"op-digest,attr"`
	OpForceRestart  string `xml:"op-force-restart,attr"`
	OpRestartDigest string `xml:"op-restart-digest,attr"`
}

/*
ParseXML uses XML unmarshal to parse an ASCII byte array into a CIB structure for pacemaker metric
processing.
*/
func ParseXML(xmlVal []byte) (*CIB, error) {
	cib := new(CIB)

	if err := xml.Unmarshal(xmlVal, cib); err != nil {
		return nil, err
	}
	log.Logger.Debugw("XML parsed to 'crmMon' struct", "crmmon", cib)
	return cib, nil

}
