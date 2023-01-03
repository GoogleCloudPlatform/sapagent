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

package pacemaker

import (
	"encoding/xml"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
)

var (
	exampleXMLData = `
<?xml version="1.0"?>
<crm_mon version="2.0.1">
    <summary>
        <stack type="corosync" />
        <current_dc present="true" version="2.0.1+20190417.13d370ca9-3.24.1-2.0.1+20190417.13d370ca9" name="test-instance-1" id="1" with_quorum="true" />
        <last_update time="Sun Sep 25 15:28:40 2022" />
        <last_change time="Sun Sep 25 15:28:16 2022" user="root" client="crm_attribute" origin="test-instance-1" />
        <nodes_configured number="2" />
        <resources_configured number="8" disabled="0" blocked="0" />
        <cluster_options stonith-enabled="true" symmetric-cluster="true" no-quorum-policy="stop" maintenance-mode="false" />
    </summary>
    <nodes>
        <node name="test-instance-1" id="1" online="true" standby="false" standby_onfail="false" maintenance="false" pending="false" unclean="false" shutdown="false" expected_up="true" is_dc="true" resources_running="5" type="member" />
        <node name="test-instance-2" id="2" online="true" standby="false" standby_onfail="false" maintenance="false" pending="false" unclean="false" shutdown="false" expected_up="true" is_dc="false" resources_running="3" type="member" />
    </nodes>
    <resources>
        <resource id="STONITH-test-instance-1" resource_agent="stonith:external/gcpstonith" role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
            <node name="test-instance-2" id="2" cached="false"/>
        </resource>
        <resource id="STONITH-test-instance-2" resource_agent="stonith:external/gcpstonith" role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
            <node name="test-instance-1" id="1" cached="false"/>
        </resource>
        <group id="g-primary" number_resources="2" >
             <resource id="rsc_vip_int-primary" resource_agent="ocf::heartbeat:IPaddr2" role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
                 <node name="test-instance-1" id="1" cached="false"/>
             </resource>
             <resource id="rsc_vip_hc-primary" resource_agent="ocf::heartbeat:anything" role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
                 <node name="test-instance-1" id="1" cached="false"/>
             </resource>
        </group>
        <clone id="cln_SAPHanaTopology_HDB_HDB00" multi_state="false" unique="false" managed="true" failed="false" failure_ignored="false" target_role="Started" >
            <resource id="rsc_SAPHanaTopology_HDB_HDB00" resource_agent="ocf::suse:SAPHanaTopology" role="Started" target_role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
                <node name="test-instance-1" id="1" cached="false"/>
            </resource>
            <resource id="rsc_SAPHanaTopology_HDB_HDB00" resource_agent="ocf::suse:SAPHanaTopology" role="Started" target_role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
                <node name="test-instance-2" id="2" cached="false"/>
            </resource>
        </clone>
        <clone id="msl_SAPHana_HDB_HDB00" multi_state="true" unique="false" managed="true" failed="false" failure_ignored="false" target_role="Started" >
            <resource id="rsc_SAPHana_HDB_HDB00" resource_agent="ocf::suse:SAPHana" role="Master" target_role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
                <node name="test-instance-1" id="1" cached="false"/>
            </resource>
            <resource id="rsc_SAPHana_HDB_HDB00" resource_agent="ocf::suse:SAPHana" role="Slave" target_role="Started" active="true" orphaned="false" blocked="false" managed="true" failed="false" failure_ignored="false" nodes_running_on="1" >
                <node name="test-instance-2" id="2" cached="false"/>
            </resource>
        </clone>
    </resources>
		<node_history>
        <node name="test-instance-1">
            <resource_history id="STONITH-test-instance-2" orphan="false" migration-threshold="5000">
                <operation_history call="28" task="start" last-rc-change="Sat Oct  8 15:07:00 2022" last-run="Sat Oct  8 15:07:00 2022" exec-time="14079ms" queue-time="0ms" rc="0" rc_text="ok" />
                <operation_history call="29" task="monitor" interval="300000ms" last-rc-change="Sat Oct  8 15:07:14 2022" exec-time="6446ms" queue-time="0ms" rc="0" rc_text="ok" />
            </resource_history>
				</node>
				<node name="test-instance-2">
            <resource_history id="STONITH-test-instance-1" orphan="false" migration-threshold="5000">
                <operation_history call="28" task="start" last-rc-change="Sat Oct  8 14:55:03 2022" last-run="Sat Oct  8 14:55:03 2022" exec-time="6992ms" queue-time="0ms" rc="0" rc_text="ok" />
                <operation_history call="32" task="monitor" interval="300000ms" last-rc-change="Sat Oct  8 14:56:22 2022" exec-time="6411ms" queue-time="0ms" rc="0" rc_text="ok" />
            </resource_history>
            <resource_history id="rsc_vip_hc-primary" orphan="false" migration-threshold="5000" fail-count="1" last-failure="Sat Oct  8 15:26:30 2022">
                <operation_history call="47" task="monitor" interval="10000ms" last-rc-change="Sat Oct  8 15:26:30 2022" exec-time="0ms" queue-time="0ms" rc="1" rc_text="unknown error" />
                <operation_history call="57" task="start" last-rc-change="Sat Oct  8 15:26:30 2022" last-run="Sat Oct  8 15:26:30 2022" exec-time="124ms" queue-time="0ms" rc="0" rc_text="ok" />
                <operation_history call="58" task="monitor" interval="10000ms" last-rc-change="Sat Oct  8 15:26:30 2022" exec-time="14ms" queue-time="0ms" rc="0" rc_text="ok" />
            </resource_history>
				</node>
		</node_history>
</crm_mon>
`
	xmlNoResourceWithFailCount = `
<?xml version="1.0"?>
<crm_mon version="2.0.1">
		<node_history>
        <node name="test-instance-1">
            <resource_history id="STONITH-test-instance-2" orphan="false" migration-threshold="5000">
                <operation_history call="28" task="start" last-rc-change="Sat Oct  8 15:07:00 2022" last-run="Sat Oct  8 15:07:00 2022" exec-time="14079ms" queue-time="0ms" rc="0" rc_text="ok" />
                <operation_history call="29" task="monitor" interval="300000ms" last-rc-change="Sat Oct  8 15:07:14 2022" exec-time="6446ms" queue-time="0ms" rc="0" rc_text="ok" />
            </resource_history>
				</node>
				<node name="test-instance-2">
            <resource_history id="STONITH-test-instance-1" orphan="false" migration-threshold="5000">
                <operation_history call="28" task="start" last-rc-change="Sat Oct  8 14:55:03 2022" last-run="Sat Oct  8 14:55:03 2022" exec-time="6992ms" queue-time="0ms" rc="0" rc_text="ok" />
                <operation_history call="32" task="monitor" interval="300000ms" last-rc-change="Sat Oct  8 14:56:22 2022" exec-time="6411ms" queue-time="0ms" rc="0" rc_text="ok" />
            </resource_history>
				</node>
		</node_history>
</crm_mon>
`
	xmlZeroNodeCRM = `
	<?xml version="1.0"?>
<crm_mon version="2.0.1">
	<nodes>
	</nodes>
</crm_mon>
`
	defaultResources = CRMResources{
		General: []CRMResource{
			{
				ID:    "STONITH-test-instance-1",
				Agent: "stonith:external/gcpstonith",
				Role:  "Started",
				Node:  CRMResourceNode{Name: "test-instance-2"},
			},
			{
				ID:    "STONITH-test-instance-2",
				Agent: "stonith:external/gcpstonith",
				Role:  "Started",
				Node:  CRMResourceNode{Name: "test-instance-1"},
			},
		},
		Group: []CRMResource{
			{
				ID:    "rsc_vip_int-primary",
				Agent: "ocf::heartbeat:IPaddr2",
				Role:  "Started",
				Node:  CRMResourceNode{Name: "test-instance-1"},
			},
			{
				ID:    "rsc_vip_hc-primary",
				Agent: "ocf::heartbeat:anything",
				Role:  "Started",
				Node:  CRMResourceNode{Name: "test-instance-1"},
			},
		},
		Clone: []CRMResource{
			{
				ID:    "rsc_SAPHanaTopology_HDB_HDB00",
				Agent: "ocf::suse:SAPHanaTopology",
				Role:  "Started",
				Node:  CRMResourceNode{Name: "test-instance-1"},
			},
			{
				ID:    "rsc_SAPHanaTopology_HDB_HDB00",
				Agent: "ocf::suse:SAPHanaTopology",
				Role:  "Started",
				Node:  CRMResourceNode{Name: "test-instance-2"},
			},
			{
				ID:    "rsc_SAPHana_HDB_HDB00",
				Agent: "ocf::suse:SAPHana",
				Role:  "Master",
				Node:  CRMResourceNode{Name: "test-instance-1"},
			},
			{
				ID:    "rsc_SAPHana_HDB_HDB00",
				Agent: "ocf::suse:SAPHana",
				Role:  "Slave",
				Node:  CRMResourceNode{Name: "test-instance-2"},
			},
		},
	}

	defaultNodes = []CRMNode{
		{
			Name:             "test-instance-1",
			ID:               1,
			Online:           true,
			ExpectedUp:       true,
			IsDC:             true,
			ResourcesRunning: 5,
			NodeType:         "member",
		},
		{
			Name:             "test-instance-2",
			ID:               2,
			Online:           true,
			ExpectedUp:       true,
			ResourcesRunning: 3,
			NodeType:         "member",
		},
	}

	defaultNodeHistory = []CRMNodeHistory{
		{
			Name: "test-instance-1",
			ResourceHistory: []CRMResourceHistory{
				{
					ID:                 "STONITH-test-instance-2",
					MigrationThreshold: "5000",
				},
			},
		},
		{
			Name: "test-instance-2",
			ResourceHistory: []CRMResourceHistory{
				{
					ID:                 "STONITH-test-instance-1",
					MigrationThreshold: "5000",
				},
				{
					ID:                 "rsc_vip_hc-primary",
					MigrationThreshold: "5000",
					FailCount:          1,
				},
			},
		},
	}
)

func TestParseCRMMon(t *testing.T) {
	tests := []struct {
		name       string
		xmlInput   []byte
		wantCRMMon *CRMMon
		wantErr    error
	}{
		{
			name:     "Success",
			xmlInput: []byte(exampleXMLData),
			wantCRMMon: &CRMMon{
				XMLName:     xml.Name{Local: "crm_mon"},
				Nodes:       defaultNodes,
				Resources:   defaultResources,
				NodeHistory: defaultNodeHistory,
			},
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
			gotCRMMon, gotErr := ParseCRMMon(test.xmlInput)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("Failure in parseCRMMon(), gotErr: %v, wantErr: %v.", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantCRMMon, gotCRMMon); diff != "" {
				t.Fatalf("Failure in parseCRMMon() returned diff (-want +got):\n%s.", diff)
			}
		})
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		fakeExists commandlineexecutor.CommandExistsRunner
		fakeRun    commandlineexecutor.CommandRunner
		want       bool
	}{
		{
			name:       "Success",
			fakeExists: func(string) bool { return true },
			fakeRun: func(string, string) (string, string, error) {
				return exampleXMLData, "", nil
			},
			want: true,
		},
		{
			name:       "CommandDoesNotExist",
			fakeExists: func(string) bool { return false },
		},
		{
			name:       "CRMMonCommandFailure",
			fakeExists: func(string) bool { return true },
			fakeRun: func(string, string) (string, string, error) {
				return "", "", cmpopts.AnyError
			},
		},
		{
			name:       "InvalidXML",
			fakeExists: func(string) bool { return true },
			fakeRun: func(string, string) (string, string, error) {
				return "<i>Not XML</q>", "", nil
			},
		},
		{
			name:       "ZeroNodeCRM",
			fakeExists: func(string) bool { return true },
			fakeRun: func(string, string) (string, string, error) {
				return xmlZeroNodeCRM, "", nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsEnabled(test.fakeExists, test.fakeRun)
			if got != test.want {
				t.Fatalf("Failure in isEnabled(), got: %v, want: %v.", got, test.want)
			}
		})
	}
}

func TestNState(t *testing.T) {
	tests := []struct {
		name      string
		fakeRun   commandlineexecutor.CommandRunner
		want      map[string]string
		wantError error
	}{
		{
			name: "Success",
			fakeRun: func(string, string) (string, string, error) {
				return exampleXMLData, "", nil
			},
			want: map[string]string{
				"test-instance-1": "online",
				"test-instance-2": "online",
			},
		},
		{
			name: "ReadFailure",
			fakeRun: func(string, string) (string, string, error) {
				return "", "", cmpopts.AnyError
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "InvalidXML",
			fakeRun: func(string, string) (string, string, error) {
				return "<>Still Not XML</q>", "", nil
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "StandByAndShutdown",
			fakeRun: func(string, string) (string, string, error) {
				xml := `<?xml version="1.0"?>
				<crm_mon version="2.0.1">
					<nodes>
        		<node name="test-instance-1" id="1" standby="true" />
       	 		<node name="test-instance-2" id="2" shutdown="true" />
    			</nodes>
				</crm_mon>`
				return xml, "", nil
			},
			want: map[string]string{
				"test-instance-1": "standby",
				"test-instance-2": "shutdown",
			},
		},
		{
			name: "UncleanAndUnknown",
			fakeRun: func(string, string) (string, string, error) {
				xml := `<?xml version="1.0"?>
				<crm_mon version="2.0.1">
					<nodes>
        		<node name="test-instance-1" id="1" unclean="true" />
       	 		<node name="test-instance-2" id="2" />
    			</nodes>
				</crm_mon>`
				return xml, "", nil
			},
			want: map[string]string{
				"test-instance-1": "unclean",
				"test-instance-2": "unknown",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotError := nState(test.fakeRun)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Fatalf("Failure in nState() returned diff (-want +got):\n%s.", diff)
			}
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Fatalf("Failure in nState(), gotError: %v, wantError: %v.", gotError, test.wantError)
			}
		})
	}
}

func TestRState(t *testing.T) {
	tests := []struct {
		name          string
		fakeRun       commandlineexecutor.CommandRunner
		wantResources []Resource
		wantError     error
	}{
		{
			name: "Success",
			fakeRun: func(string, string) (string, string, error) {
				return exampleXMLData, "", nil
			},
			wantResources: []Resource{
				{
					Name: "stonith:external/gcpstonith",
					Role: "Started",
					Node: "test-instance-2",
				},
				{
					Name: "stonith:external/gcpstonith",
					Role: "Started",
					Node: "test-instance-1",
				},
				{
					Name: "ocf::heartbeat:IPaddr2",
					Role: "Started",
					Node: "test-instance-1",
				},
				{
					Name: "ocf::heartbeat:anything",
					Role: "Started",
					Node: "test-instance-1",
				},
				{
					Name: "ocf::suse:SAPHanaTopology",
					Role: "Started",
					Node: "test-instance-1",
				},
				{
					Name: "ocf::suse:SAPHanaTopology",
					Role: "Started",
					Node: "test-instance-2",
				},
				{
					Name: "ocf::suse:SAPHana",
					Role: "Master",
					Node: "test-instance-1",
				},
				{
					Name: "ocf::suse:SAPHana",
					Role: "Slave",
					Node: "test-instance-2",
				},
			},
		},
		{
			name: "ReadFailure",
			fakeRun: func(string, string) (string, string, error) {
				return "", "", cmpopts.AnyError
			},
			wantError: cmpopts.AnyError,
		},
		{
			name: "InvalidXML",
			fakeRun: func(string, string) (string, string, error) {
				return "<not xml>", "", nil
			},
			wantError: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotResources, gotError := rState(test.fakeRun)

			if diff := cmp.Diff(test.wantResources, gotResources); diff != "" {
				t.Fatalf("Failure in rState() returned diff (-want +got):\n%s.", diff)
			}
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Fatalf("Failure in rState(), gotError: %v, wantError: %v.", gotError, test.wantError)
			}
		})
	}
}

func TestFailCount(t *testing.T) {
	tests := []struct {
		name    string
		fakeRun commandlineexecutor.CommandRunner
		want    []ResourceFailCount
		wantErr error
	}{
		{
			name: "ResourceWithFailCount",
			fakeRun: func(string, string) (string, string, error) {
				return exampleXMLData, "", nil
			},
			want: []ResourceFailCount{
				{
					ResourceName: "rsc_vip_hc-primary",
					Node:         "test-instance-2",
					FailCount:    1,
				},
			},
		},
		{
			name: "NoResourceWithFailCount",
			fakeRun: func(string, string) (string, string, error) {
				return xmlNoResourceWithFailCount, "", nil
			},
		},
		{
			name: "XMLParseFailure",
			fakeRun: func(string, string) (string, string, error) {
				return "<not xml>", "", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CRMMonFailure",
			fakeRun: func(string, string) (string, string, error) {
				return "", "", cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := FailCount(test.fakeRun)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("Failure in fCount(), gotErr: %v, wantErr: %v.", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Fatalf("Failure in fCount() returned diff (-want +got):\n%s.", diff)
			}

		})
	}
}

func TestPaceMakerXMLString(t *testing.T) {
	tests := []struct {
		name       string
		cmdExists  func(string) bool
		cmdExecute func(string, string) (string, string, error)
		crmAvail   bool
		want       *string
	}{
		{
			name:       "NilReturn",
			cmdExists:  func(string) bool { return false },
			cmdExecute: func(string, string) (string, string, error) { return "", "", nil },
			crmAvail:   false,
			want:       nil,
		},
		{
			name:       "CRMAvailable",
			cmdExists:  func(string) bool { return false },
			cmdExecute: func(string, string) (string, string, error) { return exampleXMLData, "", nil },
			crmAvail:   true,
			want:       &exampleXMLData,
		},
		{
			name:       "PCSExistsCRMUnavailable",
			cmdExists:  func(string) bool { return true },
			cmdExecute: func(string, string) (string, string, error) { return xmlNoResourceWithFailCount, "", nil },
			crmAvail:   false,
			want:       &xmlNoResourceWithFailCount,
		},
		{
			name:       "PCSExistsCRMAvailable",
			cmdExists:  func(string) bool { return true },
			cmdExecute: func(string, string) (string, string, error) { return exampleXMLData, "", nil },
			crmAvail:   true,
			want:       &exampleXMLData,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := XMLString(test.cmdExecute, test.cmdExists, test.crmAvail)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Fatalf("Failure in XMLString() returned diff (-want +got):\n%s.", diff)
			}
		})
	}
}
