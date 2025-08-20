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
	"context"
	_ "embed"
	"errors"
	"net"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	cmpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/configurablemetrics"
	statuspb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/status"
	systempb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

const (
	testBaseInstanceName = "test-instance-name"
	testInstanceName1    = "test-instance-name-1"
	testInstanceName2    = "test-instance-name-2"
	testInstanceName3    = "test-instance-name-3"
	testInstanceID       = "test-instance-id"
	testBaseZone         = "test-region-zone"
	testZone1            = "test-region-zone-1"
	testZone2            = "test-region-zone-2"
	testProjectID        = "test-project-id"
)

var (
	cnf = &cnfpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			InstanceName: testBaseInstanceName,
			InstanceId:   testInstanceID,
			Zone:         testBaseZone,
			ProjectId:    testProjectID,
		},
		AgentProperties: &cnfpb.AgentProperties{Name: "sapagent", Version: "1.0"},
	}

	collectionConfigVersion = "34"
)

func wantSystemMetrics(ts *timestamppb.Timestamp, labels map[string]string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/system",
				Labels: labels,
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": testInstanceID,
					"zone":        testBaseZone,
					"project_id":  testProjectID,
				},
			},
			Points: []*mrpb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: 1,
					},
				},
			}},
		}},
	}
}

func createParameters(config *cnfpb.Configuration, workloadConfig *wlmpb.WorkloadValidation, discovery *fakeDiscoveryInterface) Parameters {
	return Parameters{
		Config:         config,
		WorkloadConfig: workloadConfig,
		Discovery:      discovery,
	}
}

func createWorkloadValidation(label string, value wlmpb.SystemVariable) *wlmpb.WorkloadValidation {
	return &wlmpb.WorkloadValidation{
		ValidationSystem: &wlmpb.ValidationSystem{
			SystemMetrics: []*wlmpb.SystemMetric{
				&wlmpb.SystemMetric{
					MetricInfo: &cmpb.MetricInfo{
						Type:  "workload.googleapis.com/sap/validation/system",
						Label: label,
					},
					Value: value,
				},
			},
		},
	}
}

func createFakeDiscovery(resourceType systempb.SapDiscovery_Resource_ResourceType, instanceRoles []systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole, zones []string, instanceNames []string) *fakeDiscoveryInterface {
	var resources []*systempb.SapDiscovery_Resource
	for i := 0; i < len(instanceRoles); i++ {
		resource := &systempb.SapDiscovery_Resource{
			ResourceType: resourceType,
			ResourceKind: systempb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "//compute.googleapis.com/projects/test-project/zones/" + zones[i] + "/instances/" + instanceNames[i],
			InstanceProperties: &systempb.SapDiscovery_Resource_InstanceProperties{
				InstanceRole: instanceRoles[i],
			},
		}
		resources = append(resources, resource)
	}
	return &fakeDiscoveryInterface{
		systems: []*systempb.SapDiscovery{
			{
				ApplicationLayer: &systempb.SapDiscovery_Component{
					Resources: resources,
				},
			},
		},
	}
}

func TestCollectSystemMetricsFromConfig(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	marshal := func(m proto.Message) string {
		bytes, err := protojson.Marshal(m)
		if err != nil {
			t.Fatalf("Failed to marshal proto: %v", err)
		}
		return string(bytes)
	}

	systemMetricOSNameVersion := &wlmpb.WorkloadValidation{
		ValidationSystem: &wlmpb.ValidationSystem{
			SystemMetrics: []*wlmpb.SystemMetric{
				&wlmpb.SystemMetric{
					MetricInfo: &cmpb.MetricInfo{
						Type:  "workload.googleapis.com/sap/validation/system",
						Label: "os",
					},
					Value: wlmpb.SystemVariable_OS_NAME_VERSION,
				},
			},
		},
	}

	tests := []struct {
		name       string
		params     Parameters
		wantLabels map[string]string
	}{
		{
			name: "DefaultCollectionDefinition",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: collectionDefinition.GetWorkloadValidation(),
				OSType:         "linux",
				osVendorID:     "debian",
				osVersion:      "11",
				InterfaceAddrsGetter: func() ([]net.Addr, error) {
					ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
					ip2, _ := net.ResolveIPAddr("ip", "192.168.0.2")
					return []net.Addr{ip1, ip2}, nil
				},
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					if params.Executable == "gcloud" {
						return commandlineexecutor.Result{
							StdOut: "Google Cloud SDK 393.0.0",
						}
					}
					if params.Executable == "gsutil" {
						return commandlineexecutor.Result{
							StdOut: "gsutil version 5.10",
						}
					}
					if params.Executable == "systemctl" {
						return commandlineexecutor.Result{
							StdOut: "active",
						}
					}
					if params.Executable == "sh" {
						return commandlineexecutor.Result{
							StdOut: "true",
						}
					}
					if params.Executable == "free" {
						return commandlineexecutor.Result{
							StdOut: "Mem: 2000000000\nSwap: 1000000000",
						}
					}
					if params.Executable == "uname" {
						return commandlineexecutor.Result{
							StdOut: "5.14.21-150500.55.73-default",
						}
					}
					if params.Executable == "grep" && params.Args[0] == "^SELINUX=" {
						return commandlineexecutor.Result{
							StdOut: "SELINUX=disabled",
						}
					}
					if params.Executable == "getenforce" {
						return commandlineexecutor.Result{
							StdOut: "disabled",
						}
					}
					return commandlineexecutor.Result{}
				},
			},
			wantLabels: map[string]string{
				"instance_name":               testBaseInstanceName,
				"os":                          "debian-11",
				"agent":                       "sapagent",
				"agent_version":               "1.0",
				"network_ips":                 "192.168.0.1,192.168.0.2",
				"gcloud":                      "true",
				"gsutil":                      "true",
				"agent_state":                 "running",
				"os_settings":                 "",
				"uefi_enabled":                "true",
				"total_ram":                   "2000000000",
				"total_swap":                  "1000000000",
				"sapconf":                     "active",
				"saptune":                     "active",
				"has_app_server":              "false",
				"app_server_zonal_separation": "false",
				"has_ascs":                    "false",
				"has_ers":                     "false",
				"tuned":                       "active",
				"selinux_config_configured":   "disabled",
				"selinux_config_active":       "disabled",
				"kernel_version": marshal(&statuspb.KernelVersion{
					RawString: "5.14.21-150500.55.73-default",
					OsKernel: &statuspb.KernelVersion_Version{
						Major: 5,
						Minor: 14,
						Build: 21,
					},
					DistroKernel: &statuspb.KernelVersion_Version{
						Major:     150500,
						Minor:     55,
						Build:     73,
						Remainder: "default",
					},
				}),
				"collection_config_version": collectionConfigVersion,
			},
		},
		{
			name: "SystemValidationMetricsEmpty",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "SystemVariableUnknown",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							&wlmpb.SystemMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "foo",
								},
							},
						},
					},
				},
			},
			wantLabels: map[string]string{
				"foo": "",
			},
		},
		{
			name: "OSNameVersionEmpty",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: systemMetricOSNameVersion,
				osVendorID:     "",
				osVersion:      "",
			},
			wantLabels: map[string]string{
				"os": "-",
			},
		},
		{
			name: "InterfaceAddrsError",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							&wlmpb.SystemMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "network_ips",
								},
								Value: wlmpb.SystemVariable_NETWORK_IPS,
							},
						},
					},
				},
				InterfaceAddrsGetter: func() ([]net.Addr, error) {
					return nil, errors.New("Interface Addrs Error")
				},
			},
			wantLabels: map[string]string{
				"network_ips": "",
			},
		},
		{
			name: "OSCommandMetrics_EmptyLabel",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_RHEL,
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputEquals{OutputEquals: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
										},
										IfFalse: &cmpb.EvalResult{
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
										},
									},
								},
							},
						},
					},
				},
				osVendorID: "sles",
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "foobar",
						StdErr: "",
					}
				},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "AppServerZonalSeparation_Different_Zones",
			params: createParameters(
				cnf,
				createWorkloadValidation("app_server_zonal_separation", wlmpb.SystemVariable_APP_SERVER_ZONAL_SEPARATION),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
					},
					[]string{testBaseZone, testZone1, testZone2},
					[]string{testInstanceName1, testInstanceName2, testInstanceName3},
				),
			),
			wantLabels: map[string]string{
				"app_server_zonal_separation": "true",
			},
		},
		{
			name: "AppServerZonalSeparation_Different_Zones_Multiple_App_Servers",
			params: createParameters(
				cnf,
				createWorkloadValidation("app_server_zonal_separation", wlmpb.SystemVariable_APP_SERVER_ZONAL_SEPARATION),
				createFakeDiscovery(
					systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
					},
					[]string{testBaseZone, testZone1, testZone2},
					[]string{testInstanceName1, testInstanceName2, testInstanceName3},
				),
			),
			wantLabels: map[string]string{
				"app_server_zonal_separation": "true",
			},
		},
		{
			name: "AppServerZonalSeparation_Same_Zone",
			params: createParameters(
				cnf,
				createWorkloadValidation("app_server_zonal_separation", wlmpb.SystemVariable_APP_SERVER_ZONAL_SEPARATION),
				createFakeDiscovery(
					systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
					},
					[]string{testBaseZone, testBaseZone, testBaseZone},
					[]string{testInstanceName1, testInstanceName2, testInstanceName3},
				),
			),
			wantLabels: map[string]string{
				"app_server_zonal_separation": "false",
			},
		},
		{
			name: "AppServerZonalSeparation_Single_Instance",
			params: createParameters(
				cnf,
				createWorkloadValidation("app_server_zonal_separation", wlmpb.SystemVariable_APP_SERVER_ZONAL_SEPARATION),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
					},
					[]string{testBaseZone},
					[]string{testInstanceName1},
				),
			),
			wantLabels: map[string]string{
				"app_server_zonal_separation": "false",
			},
		},
		{
			name: "HasAppServer_True",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_app_server", wlmpb.SystemVariable_HAS_APP_SERVER),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testInstanceName1},
				),
			),
			wantLabels: map[string]string{
				"has_app_server": "true",
			},
		},
		{
			name: "HasAppServer_False_No_App_Server",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_app_server", wlmpb.SystemVariable_HAS_APP_SERVER),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testZone1},
					[]string{testBaseInstanceName, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_app_server": "false",
			},
		},
		{
			name: "HasAppServer_False_Non_Compute_Resource",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_app_server", wlmpb.SystemVariable_HAS_APP_SERVER),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_UNSPECIFIED,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_app_server": "false",
			},
		},
		{
			name: "HasAppServer_False_Different_Instance",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_app_server", wlmpb.SystemVariable_HAS_APP_SERVER),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testInstanceName1, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_app_server": "false",
			},
		},
		{
			name: "HasAscs_True",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ascs", wlmpb.SystemVariable_HAS_ASCS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testInstanceName1, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_ascs": "true",
			},
		},
		{
			name: "HasAscs_False_No_Ascs",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ascs", wlmpb.SystemVariable_HAS_ASCS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_ascs": "false",
			},
		},
		{
			name: "HasAscs_False_Non_Compute_Resource",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ascs", wlmpb.SystemVariable_HAS_ASCS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_UNSPECIFIED,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_ascs": "false",
			},
		},
		{
			name: "HasAscs_False_Different_Instance",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ascs", wlmpb.SystemVariable_HAS_ASCS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testInstanceName1},
				),
			),
			wantLabels: map[string]string{
				"has_ascs": "false",
			},
		},
		{
			name: "HasErs_True",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ers", wlmpb.SystemVariable_HAS_ERS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_ers": "true",
			},
		},
		{
			name: "HasErs_False_No_Ers",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ers", wlmpb.SystemVariable_HAS_ERS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_ers": "false",
			},
		},
		{
			name: "HasErs_False_Non_Compute_Resource",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ers", wlmpb.SystemVariable_HAS_ERS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_UNSPECIFIED,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS_APP_SERVER,
					},
					[]string{testBaseZone},
					[]string{testBaseInstanceName},
				),
			),
			wantLabels: map[string]string{
				"has_ers": "false",
			},
		},
		{
			name: "HasErs_False_Different_Instance",
			params: createParameters(
				cnf,
				createWorkloadValidation("has_ers", wlmpb.SystemVariable_HAS_ERS),
				createFakeDiscovery(systempb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					[]systempb.SapDiscovery_Resource_InstanceProperties_InstanceRole{
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_UNSPECIFIED,
						systempb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
					},
					[]string{testBaseZone, testBaseZone},
					[]string{testBaseInstanceName, testInstanceName1},
				),
			),
			wantLabels: map[string]string{
				"has_ers": "false",
			},
		},
		{
			name: "KernelVersion_Error",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							&wlmpb.SystemMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "kernel_version",
								},
								Value: wlmpb.SystemVariable_KERNEL_VERSION,
							},
						},
					},
				},
				OSType: "linux",
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						Error: errors.New("Error in executing command"),
					}
				},
			},
			wantLabels: map[string]string{
				"kernel_version": "",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := wantSystemMetrics(&timestamppb.Timestamp{Seconds: time.Now().Unix()}, test.wantLabels)
			got := CollectSystemMetricsFromConfig(context.Background(), test.params)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectSystemMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}
