/*
Copyright 2024 Google LLC

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

package pacemaker

import (
	_ "embed"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	//go:embed test_data/pacemaker.xml
	pacemakerXML string
	//go:embed test_data/pacemaker-serviceaccount.xml
	pacemakerServiceAccountXML string
	//go:embed test_data/pacemaker-notype.xml
	pacemakerNoTypeXML string
	//go:embed test_data/pacemaker-fencegce.xml
	pacemakerFenceGCEXML string
	//go:embed test_data/pacemaker-clone.xml
	pacemakerCloneXML string
	//go:embed test_data/pacemaker-cliprefer.xml
	pacemakerClipReferXML string

	parsedPacemakerCloneXML = CIB{
		CRMFeatureSet:  "3.7.1",
		ValidateWith:   "pacemaker-3.6",
		Epoch:          90336,
		NumUpdates:     0,
		AdminEpoch:     0,
		CIBLastWritten: "Thu May  5 17:53:21 2022",
		UpdateOrigin:   "rhel-ha2",
		UpdateClient:   "crm_attribute",
		UpdateUser:     "root",
		HaveQuorum:     1,
		DCUUID:         1,
		Configuration:  Configuration{},
		Status: []CIBNodeState{
			{
				ID:             "1",
				Uname:          "rhel-ha1",
				InCCM:          true,
				CRMD:           "online",
				CRMDebugOrigin: "do_update_resource",
				Join:           "member",
				Expected:       "member",
				TransientAttributes: TransientAttributes{
					ID: "1",
					InstanceAttributes: ClusterPropertySet{
						ID: "status-1",
						NVPairs: []NVPair{
							{ID: "status-1-master-SAPHana_HAR_00", Name: "master-SAPHana_HAR_00", Value: "100"},
							{ID: "status-1-hana_har_version", Name: "hana_har_version", Value: "2.00.057.00.1629894416"},
							{ID: "status-1-hana_har_sync_state", Name: "hana_har_sync_state", Value: "SOK"},
							{ID: "status-1-hana_har_clone_state", Name: "hana_har_clone_state", Value: "DEMOTED"},
							{ID: "status-1-hana_har_roles", Name: "hana_har_roles", Value: "4:S:master1:master:worker:master"},
							{ID: "status-1-fail-count-SAPHana_HAR_00.monitor_59000", Name: "fail-count-SAPHana_HAR_00#monitor_59000", Value: "2"},
							{ID: "status-1-last-failure-SAPHana_HAR_00.monitor_59000", Name: "last-failure-SAPHana_HAR_00#monitor_59000", Value: "1651696943"},
						},
					},
				},
				LRM: LRM{
					ID: "1",
					LRMResources: []LRMResource{
						{
							ID:            "SAPHanaTopology_HAR_00",
							ResourceType:  "SAPHanaTopology",
							ResrouceClass: "ocf",
							Provider:      "heartbeat",
							LRMRscOps: []LRMRSCOp{
								{
									ID:              "SAPHanaTopology_HAR_00_last_0",
									OperationKey:    "SAPHanaTopology_HAR_00_start_0",
									Operation:       "start",
									CRMDebugOrigin:  "build_active_RAs",
									CRMFeatureSet:   "3.7.1",
									TransitionKey:   "20:21:0:207d703b-956f-494c-b2bc-d07cf0a366bb",
									TransitionMagic: "0:0;20:21:0:207d703b-956f-494c-b2bc-d07cf0a366bb",
									ExitReason:      "",
									OnNode:          "rhel-ha1",
									CallID:          "29",
									RCCode:          0,
									OpStatus:        0,
									Interval:        0,
									LastRCChange:    1646251172,
									LastRun:         1646251172,
									ExecTime:        2719,
									QueueTime:       0,
									OpDigest:        "95c004370528dbb0f6836ddb16bb73ac",
									OpForceRestart:  "",
									OpRestartDigest: "f2317cad3d54cec5d7d7aa7d0bf35cf8",
								},
								{
									ID:              "SAPHanaTopology_HAR_00_monitor_10000",
									OperationKey:    "SAPHanaTopology_HAR_00_monitor_10000",
									Operation:       "monitor",
									CRMFeatureSet:   "3.7.1",
									CRMDebugOrigin:  "build_active_RAs",
									TransitionKey:   "21:21:0:207d703b-956f-494c-b2bc-d07cf0a366bb",
									TransitionMagic: "0:0;21:21:0:207d703b-956f-494c-b2bc-d07cf0a366bb",
									ExitReason:      "",
									OnNode:          "rhel-ha1",
									CallID:          "31",
									RCCode:          0,
									OpStatus:        0,
									Interval:        10000,
									LastRCChange:    1646251175,
									LastRun:         0,
									ExecTime:        3548,
									QueueTime:       0,
									OpDigest:        "0cbdbe857df192f74d644a9b362527d1",
								},
							},
						},
						LRMResource{
							ID:            "SAPHana_HAR_00",
							ResourceType:  "SAPHana",
							ResrouceClass: "ocf",
							Provider:      "heartbeat",
							LRMRscOps: []LRMRSCOp{
								{
									ID:              "SAPHana_HAR_00_last_0",
									OperationKey:    "SAPHana_HAR_00_start_0",
									Operation:       "start",
									CRMFeatureSet:   "3.7.1",
									CRMDebugOrigin:  "do_update_resource",
									TransitionKey:   "30:88437:0:30691068-39b0-4d9e-8342-ce26705d7a80",
									TransitionMagic: "0:0;30:88437:0:30691068-39b0-4d9e-8342-ce26705d7a80",
									ExitReason:      "",
									OnNode:          "rhel-ha1",
									CallID:          "60",
									RCCode:          0,
									OpStatus:        0,
									Interval:        0,
									LastRCChange:    1651697218,
									LastRun:         1651697218,
									ExecTime:        1986,
									QueueTime:       0,
									OpDigest:        "6531e8b20586c0f3cf25a58a6b3d6032",
									OpForceRestart:  "  INSTANCE_PROFILE  ",
									OpRestartDigest: "f2317cad3d54cec5d7d7aa7d0bf35cf8",
								},
							},
						},
					},
				},
			},
			CIBNodeState{
				ID:             "2",
				Uname:          "rhel-ha2",
				InCCM:          true,
				CRMD:           "online",
				CRMDebugOrigin: "do_update_resource",
				Join:           "member",
				Expected:       "member",
				TransientAttributes: TransientAttributes{
					ID: "2",
					InstanceAttributes: ClusterPropertySet{
						ID: "status-2",
						NVPairs: []NVPair{
							{ID: "status-2-master-SAPHana_HAR_00", Name: "master-SAPHana_HAR_00", Value: "150"},
							{ID: "status-2-hana_har_version", Name: "hana_har_version", Value: "2.00.057.00.1629894416"},
							{ID: "status-2-hana_har_sync_state", Name: "hana_har_sync_state", Value: "PRIM"},
							{ID: "status-2-hana_har_clone_state", Name: "hana_har_clone_state", Value: "PROMOTED"},
							{ID: "status-2-hana_har_roles", Name: "hana_har_roles", Value: "4:P:master1:master:worker:master"},
							{ID: "status-2-fail-count-SAPHana_HAR_00.monitor_59000", Name: "fail-count-SAPHana_HAR_00#monitor_59000", Value: "1"},
							{ID: "status-2-last-failure-SAPHana_HAR_00.monitor_59000", Name: "last-failure-SAPHana_HAR_00#monitor_59000", Value: "1651247387"},
						},
					},
				},
				LRM: LRM{
					ID: "2",
					LRMResources: []LRMResource{
						{
							ID:            "STONITH-rhel-ha1",
							ResourceType:  "fence_gce",
							ResrouceClass: "stonith",
							LRMRscOps: []LRMRSCOp{
								{
									ID:              "STONITH-rhel-ha1_last_0",
									OperationKey:    "STONITH-rhel-ha1_start_0",
									Operation:       "start",
									CRMFeatureSet:   "3.7.1",
									CRMDebugOrigin:  "do_update_resource",
									TransitionKey:   "6:9:0:30691068-39b0-4d9e-8342-ce26705d7a80",
									TransitionMagic: "0:0;6:9:0:30691068-39b0-4d9e-8342-ce26705d7a80",
									ExitReason:      "",
									OnNode:          "rhel-ha2",
									CallID:          "28",
									RCCode:          0,
									OpStatus:        0,
									Interval:        0,
									LastRCChange:    1646253318,
									LastRun:         1646253318,
									ExecTime:        914,
									QueueTime:       0,
									OpDigest:        "f451ebcf5bef089b57c909be03bb6504",
								},
								{
									ID:              "STONITH-rhel-ha1_monitor_300000",
									OperationKey:    "STONITH-rhel-ha1_monitor_300000",
									Operation:       "monitor",
									CRMFeatureSet:   "3.7.1",
									CRMDebugOrigin:  "do_update_resource",
									TransitionKey:   "7:9:0:30691068-39b0-4d9e-8342-ce26705d7a80",
									TransitionMagic: "0:0;7:9:0:30691068-39b0-4d9e-8342-ce26705d7a80",
									ExitReason:      "",
									OnNode:          "rhel-ha2",
									CallID:          "30",
									RCCode:          0,
									OpStatus:        0,
									Interval:        300000,
									LastRCChange:    1646253319,
									LastRun:         0,
									ExecTime:        998,
									QueueTime:       0,
									OpDigest:        "c0012f0aeca5d95f97a2dbcc8610cd9b",
								},
							},
						},
					},
				},
			},
		},
	}

	parsedPacemakerXML = CIB{
		CRMFeatureSet:  "3.6.1",
		ValidateWith:   "pacemaker-3.5",
		Epoch:          747,
		NumUpdates:     0,
		AdminEpoch:     0,
		CIBLastWritten: "Wed Mar  2 20:26:14 2022",
		UpdateOrigin:   "instanceId",
		UpdateClient:   "crm_attribute",
		UpdateUser:     "root",
		HaveQuorum:     1,
		DCUUID:         1,
		Configuration: Configuration{
			CRMConfig: CRMConfig{
				ClusterPropertySets: []ClusterPropertySet{
					{
						ID: "cib-bootstrap-options",
						NVPairs: []NVPair{
							{ID: "cib-bootstrap-options-have-watchdog", Name: "have-watchdog", Value: "false"},
							{ID: "cib-bootstrap-options-dc-version", Name: "dc-version", Value: "2.0.5+20201202.ba59be712-150300.4.16.1-2.0.5+20201202.ba59be712"},
							{ID: "cib-bootstrap-options-cluster-infrastructure", Name: "cluster-infrastructure", Value: "corosync"},
							{ID: "cib-bootstrap-options-cluster-name", Name: "cluster-name", Value: "hacluster"},
							{Name: "maintenance-mode", Value: "false", ID: "cib-bootstrap-options-maintenance-mode"},
							{Name: "stonith-timeout", Value: "300s", ID: "cib-bootstrap-options-stonith-timeout"},
							{Name: "stonith-enabled", Value: "true", ID: "cib-bootstrap-options-stonith-enabled"},
						},
					},
					{
						ID: "SAPHanaSR",
						NVPairs: []NVPair{
							{ID: "SAPHanaSR-hana_has_site_srHook_sles-ha2", Name: "hana_has_site_srHook_sles-ha2", Value: "SOK"},
							{ID: "SAPHanaSR-hana_has_site_srHook_instanceId", Name: "hana_has_site_srHook_instanceId", Value: "PRIM"},
						},
					},
				},
			},
			Resources: Resources{
				Primitives: []PrimitiveClass{
					{
						ID:        "STONITH-instanceName",
						Class:     "stonith",
						ClassType: "external/gcpstonith",
						Operations: []Op{
							{Name: "monitor", Interval: "300s", Timeout: "120s", ID: "STONITH-instanceName-monitor-300s"},
							{Name: "start", Interval: "0", Timeout: "60s", ID: "STONITH-instanceName-start-0"},
						},
						InstanceAttributes: ClusterPropertySet{
							ID: "STONITH-instanceName-instance_attributes",
							NVPairs: []NVPair{
								{Name: "instance_name", Value: "instanceId", ID: "STONITH-instanceName-instance_attributes-instance_name"},
								{Name: "gcloud_path", Value: "/usr/bin/gcloud", ID: "STONITH-instanceName-instance_attributes-gcloud_path"},
								{Name: "logging", Value: "yes", ID: "STONITH-instanceName-instance_attributes-logging"},
								{Name: "pcmk_reboot_timeout", Value: "300", ID: "STONITH-instanceName-instance_attributes-pcmk_reboot_timeout"},
								{Name: "pcmk_monitor_retries", Value: "4", ID: "STONITH-instanceName-instance_attributes-pcmk_monitor_retries"},
								{Name: "pcmk_delay_max", Value: "30", ID: "STONITH-instanceName-instance_attributes-pcmk_delay_max"},
							},
						},
					},
				},
				Group: Group{
					ID: "g-primary",
					Primitives: []PrimitiveClass{
						{
							ID:        "rsc_vip_int-primary",
							Class:     "ocf",
							Provider:  "heartbeat",
							ClassType: "IPaddr2",
							InstanceAttributes: ClusterPropertySet{
								ID:      "rsc_vip_int-primary-instance_attributes",
								NVPairs: []NVPair{{ID: "rsc_vip_int-primary-instance_attributes-ip", Value: "10.150.1.10", Name: "ip"}},
							},
							Operations: []Op{
								{
									Name: "monitor", Interval: "3600s", Timeout: "60s", ID: "rsc_vip_int-primary-monitor-3600s",
								},
							},
						},
					},
				},
				Clone: Clone{
					ID: "cln_SAPHanaTopology_HAS_HDB00",
					Attributes: ClusterPropertySet{
						ID:      "cln_SAPHanaTopology_HAS_HDB00-meta_attributes",
						NVPairs: []NVPair{{Name: "clone-node-max", Value: "1", ID: "cln_SAPHanaTopology_HAS_HDB00-meta_attributes-clone-node-max"}},
					},
					Primitives: []PrimitiveClass{
						{
							ID:         "rsc_SAPHanaTopology_HAS_HDB00",
							Class:      "ocf",
							Provider:   "suse",
							ClassType:  "SAPHanaTopology",
							Operations: []Op{{Name: "monitor", Interval: "10", Timeout: "600", ID: "rsc_sap2_HAS_HDB00-operations-monitor-10"}},
							InstanceAttributes: ClusterPropertySet{
								ID:      "rsc_SAPHanaTopology_HAS_HDB00-instance_attributes",
								NVPairs: []NVPair{{Name: "SID", Value: "HAS", ID: "rsc_SAPHanaTopology_HAS_HDB00-instance_attributes-SID"}},
							},
						},
					},
				},
				Master: Clone{
					ID: "msl_SAPHana_HAS_HDB00",
					Attributes: ClusterPropertySet{
						ID:      "msl_SAPHana_HAS_HDB00-meta_attributes",
						NVPairs: []NVPair{{Name: "notify", Value: "true", ID: "msl_SAPHana_HAS_HDB00-meta_attributes-notify"}},
					},
					Primitives: []PrimitiveClass{
						{
							ID:         "rsc_SAPHana_HAS_HDB00",
							Class:      "ocf",
							Provider:   "suse",
							ClassType:  "SAPHana",
							Operations: []Op{{Name: "start", Interval: "0", Timeout: "3600", ID: "rsc_sap_HAS_HDB00-operations-start-0"}},
							InstanceAttributes: ClusterPropertySet{
								ID:      "rsc_SAPHana_HAS_HDB00-instance_attributes",
								NVPairs: []NVPair{{Name: "SID", Value: "HAS", ID: "rsc_SAPHana_HAS_HDB00-instance_attributes-SID"}},
							},
						},
					},
				},
			},
			Nodes: []CIBNode{
				{
					ID:    "1",
					Uname: "instanceId",
					InstanceAttributes: ClusterPropertySet{
						ID: "nodes-1",
						NVPairs: []NVPair{
							{ID: "nodes-1-hana_has_op_mode", Name: "hana_has_op_mode", Value: "logreplay"},
							{ID: "nodes-1-lpa_has_lpt", Name: "lpa_has_lpt", Value: "1646252774"},
							{ID: "nodes-1-hana_has_vhost", Name: "hana_has_vhost", Value: "instanceId"},
							{ID: "nodes-1-hana_has_srmode", Name: "hana_has_srmode", Value: "syncmem"},
							{ID: "nodes-1-hana_has_remoteHost", Name: "hana_has_remoteHost", Value: "sles-ha2"},
							{ID: "nodes-1-hana_has_site", Name: "hana_has_site", Value: "instanceId"},
							{ID: "nodes-1-standby", Name: "standby", Value: "off"},
						},
					},
				},
				{
					ID:    "2",
					Uname: "sles-ha2",
					InstanceAttributes: ClusterPropertySet{
						ID: "nodes-2",
						NVPairs: []NVPair{
							{ID: "nodes-2-lpa_has_lpt", Name: "lpa_has_lpt", Value: "30"},
							{ID: "nodes-2-hana_has_op_mode", Name: "hana_has_op_mode", Value: "logreplay"},
							{ID: "nodes-2-hana_has_vhost", Name: "hana_has_vhost", Value: "sles-ha2"},
							{ID: "nodes-2-hana_has_site", Name: "hana_has_site", Value: "sles-ha2"},
							{ID: "nodes-2-hana_has_srmode", Name: "hana_has_srmode", Value: "syncmem"},
							{ID: "nodes-2-hana_has_remoteHost", Name: "hana_has_remoteHost", Value: "instanceId"},
							{ID: "nodes-2-standby", Name: "standby", Value: "off"},
						},
					},
				},
			},
			RSCDefaults: ClusterPropertySet{
				ID: "rsc-options",
				NVPairs: []NVPair{
					{ID: "rsc-options-resource-stickiness", Name: "resource-stickiness", Value: "1000"},
					{ID: "rsc-options-migration-threshold", Name: "migration-threshold", Value: "5000"},
				},
			},
			OPDefaults: ClusterPropertySet{
				ID: "op-options",
				NVPairs: []NVPair{
					{ID: "op-options-timeout", Name: "timeout", Value: "600"},
				},
			},
			Constraints: Constraints{
				RSCLocations: []RSCLocation{
					{
						ID:    "LOC_STONITH_instanceId",
						RSC:   "STONITH-instanceName",
						Score: "-INFINITY",
						Node:  "instanceId",
					},
					{
						ID:    "LOC_STONITH_sles-ha2",
						RSC:   "STONITH-sles-ha2",
						Score: "-INFINITY",
						Node:  "sles-ha2",
					},
				},
				RSCColocation: RSCColocation{
					ID:          "col_saphana_ip_HAS_HDB00",
					Score:       "4000",
					RSC:         "g-primary",
					RSCRole:     "Started",
					WithRSC:     "msl_SAPHana_HAS_HDB00",
					WithRSCRole: "Master",
				},
				RSCOrder: RSCOrder{
					ID:    "ord_SAPHana_HAS_HDB00",
					Kind:  "Optional",
					First: "cln_SAPHanaTopology_HAS_HDB00",
					Then:  "msl_SAPHana_HAS_HDB00",
				},
			},
		},
	}
)

func TestClonePacemakerXMLStatus(t *testing.T) {
	tests := []struct {
		name     string
		xmlInput []byte
		wantCIB  *CIB
		wantErr  error
	}{
		{
			name:     "Success",
			xmlInput: []byte(pacemakerCloneXML),
			wantCIB:  &parsedPacemakerCloneXML,
		},
		{
			name:     "EmptyXMLFailure",
			xmlInput: []byte{},
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "InvalidXML",
			xmlInput: []byte("<i>Not XML</q>"),
			wantErr:  cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			gotCIB, gotErr := ParseXML(test.xmlInput)

			if !cmp.Equal(test.wantErr, gotErr, cmpopts.EquateErrors()) {
				t.Errorf("ParseXML got error %v, want error %v", gotErr, test.wantErr)
			}

			/*
				Parsing and validating the Configuration struct at this step is not necessary since this is
				done in the following unit test.
			*/
			if gotErr == nil {
				gotCIB.Configuration = Configuration{}
			}

			if diff := cmp.Diff(test.wantCIB, gotCIB); diff != "" {
				t.Errorf("ParseXML() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParsePacemakerXML(t *testing.T) {
	tests := []struct {
		name     string
		xmlInput []byte
		wantCIB  *CIB
		wantErr  error
	}{
		{
			name:     "Success",
			xmlInput: []byte(pacemakerXML),
			wantCIB:  &parsedPacemakerXML,
		},
		{
			name:     "EmptyXMLFailure",
			xmlInput: []byte{},
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "InvalidXML",
			xmlInput: []byte("<i>Not XML</q>"),
			wantErr:  cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			gotCIB, gotErr := ParseXML(test.xmlInput)

			if diff := cmp.Diff(test.wantCIB, gotCIB); diff != "" {
				t.Errorf("ParseXML() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}

			if !cmp.Equal(test.wantErr, gotErr, cmpopts.EquateErrors()) {
				t.Errorf("ParseXML got error %v, want error %v", gotErr, test.wantErr)
			}
		})
	}
}
