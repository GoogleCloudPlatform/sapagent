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

// Package systemdiscovery implements the system discovery
// as an OTE to discover SAP systems running on the host.
package systemdiscovery

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"flag"
	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	dpb "google.golang.org/protobuf/types/known/durationpb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	appsdiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery/fake"
	clouddiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery/fake"
	hostdiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	logfake "github.com/GoogleCloudPlatform/sapagent/shared/log/fake"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultCloudProperties = &iipb.CloudProperties{
		ProjectId:        "default-project",
		InstanceId:       "default-instance-id",
		InstanceName:     "default-instance",
		Zone:             "default-zone",
		NumericProjectId: "13102003",
	}

	defaultAgentProperties = &cpb.AgentProperties{
		Name:    configuration.AgentName,
		Version: configuration.AgentVersion,
	}

	defaultDiscoveryConfig = &cpb.DiscoveryConfiguration{
		EnableDiscovery:                &wpb.BoolValue{Value: false},
		SapInstancesUpdateFrequency:    dpb.New(time.Duration(1 * time.Minute)),
		SystemDiscoveryUpdateFrequency: dpb.New(time.Duration(4 * time.Hour)),
		EnableWorkloadDiscovery:        &wpb.BoolValue{Value: true},
	}

	testDiscoveryConfig = &cpb.DiscoveryConfiguration{
		EnableDiscovery:                &wpb.BoolValue{Value: false},
		SapInstancesUpdateFrequency:    dpb.New(time.Duration(3 * time.Second)),
		SystemDiscoveryUpdateFrequency: dpb.New(time.Duration(4 * time.Hour)),
		EnableWorkloadDiscovery:        &wpb.BoolValue{Value: true},
	}

	testConfigFileJSON = `
	{
		"discovery_configuration": {
			"enable_discovery": false,
			"sap_instances_update_frequency": "3s",
			"system_discovery_update_frequency": "14400s",
			"enable_workload_discovery": true
		}
	}`

	testInvalidConfigFileJSON = `
	{
		"discovery_configuration": {
			"enable_discovery": tr,
			"sap_instances_update_frequency": "3s",
			"system_discovery_update_frequency": "14400s",
			"enable_workload_discovery": true
		}
	}`

	defaultIIOTEParams = &onetime.InternallyInvokedOTE{
		InvokedBy: "test",
		Lp:        log.Parameters{},
		Cp:        defaultCloudProperties,
	}

	defaultSAPInstances = &sappb.SAPInstances{
		Instances: []*sappb.SAPInstance{
			{
				Sapsid:         "test-hana-1",
				InstanceNumber: "001",
				Type:           sappb.InstanceType_HANA,
			},
			{
				Sapsid:         "test-hana-2",
				InstanceNumber: "002",
				Type:           sappb.InstanceType_HANA,
			},
		},
	}

	defaultAppsDiscovery = func(context.Context) *sappb.SAPInstances {
		return defaultSAPInstances
	}

	defaultCloudLoggingClient = &logging.Client{}
)

func createTestConfigFile(t *testing.T, configJSON string) *os.File {
	filePath := t.TempDir() + "/configuration.json"
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("os.Create(%v) failed: %v", filePath, err)
	}
	f.WriteString(configJSON)
	return f
}

func createTestIIOTESystemDiscovery(t *testing.T, configPath string) *SystemDiscovery {
	return &SystemDiscovery{
		CloudLogInterface: &logfake.TestCloudLogging{FlushErr: []error{nil}},
		CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
		},
		HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		SapDiscoveryInterface: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
		},
		AppsDiscovery: defaultAppsDiscovery,
		IIOTEParams:   defaultIIOTEParams,
		ConfigPath:    configPath,
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		sd   *SystemDiscovery
		args []any
		want subcommands.ExitStatus
	}{
		{
			name: "FailLengthArgs",
			sd:   &SystemDiscovery{},
			want: subcommands.ExitFailure,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			sd:   &SystemDiscovery{},
			want: subcommands.ExitFailure,
			args: []any{
				"arg_one",
				"arg_two",
				"arg_three",
			},
		},
		{
			name: "SuccessForHelp",
			sd: &SystemDiscovery{
				help: true,
			},
			args: []any{},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessWithConfig",
			sd: &SystemDiscovery{
				CloudLogInterface: &logfake.TestCloudLogging{FlushErr: []error{nil}},
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
					DiscoverCurrentHostResp: [][]string{{}},
				},
				SapDiscoveryInterface: &appsdiscoveryfake.SapDiscovery{
					DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
				},
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
				ConfigPath:    createTestConfigFile(t, testConfigFileJSON).Name(),
			},
			args: []any{
				"anything",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessWithoutConfig",
			sd: &SystemDiscovery{
				CloudLogInterface: &logfake.TestCloudLogging{FlushErr: []error{nil}},
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
					DiscoverCurrentHostResp: [][]string{{}},
				},
				SapDiscoveryInterface: &appsdiscoveryfake.SapDiscovery{
					DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
				},
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			},
			args: []any{
				"anything",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sd.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Execute() returned an unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSystemDiscoveryHandler(t *testing.T) {
	tests := []struct {
		name                string
		sd                  *SystemDiscovery
		wantErr             bool
		wantDiscoveryObject bool
		wantSAPInstances    *sappb.SAPInstances
	}{
		{
			name:                "SuccessIIOTEModeWithoutConfigFile",
			sd:                  createTestIIOTESystemDiscovery(t, ""),
			wantDiscoveryObject: true,
			wantSAPInstances:    defaultSAPInstances,
		},
		{
			name:                "SuccessIIOTEModeWithConfigFile",
			sd:                  createTestIIOTESystemDiscovery(t, createTestConfigFile(t, testConfigFileJSON).Name()),
			wantDiscoveryObject: true,
			wantSAPInstances:    defaultSAPInstances,
		},
		{
			name:    "FailConfigFileNotFound",
			sd:      createTestIIOTESystemDiscovery(t, createTestConfigFile(t, testConfigFileJSON).Name()+"sap"),
			wantErr: true,
		},
		{
			name: "SuccessApplyDefaultParamsIfMissing",
			sd: &SystemDiscovery{
				IIOTEParams: defaultIIOTEParams,
				ConfigPath:  createTestConfigFile(t, testConfigFileJSON).Name(),
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
					DiscoverCurrentHostResp: [][]string{{}},
				},
			},
			wantDiscoveryObject: true,
			wantSAPInstances:    &sappb.SAPInstances{},
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			discovery, err := test.sd.systemDiscoveryHandler(ctx, defaultCloudLoggingClient, defaultCloudProperties)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("SystemDiscoveryHandler() returned an unexpected error: %v, want error presence = %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if gotDiscoveryObject := discovery != nil; gotDiscoveryObject != test.wantDiscoveryObject {
				t.Errorf("SystemDiscoveryHandler() returned an unexpected discovery object: %v, want discovery object presence = %v", discovery, test.wantDiscoveryObject)
			}
			if diff := cmp.Diff(test.wantSAPInstances, discovery.GetSAPInstances(), protocmp.Transform()); diff != "" {
				t.Errorf("SystemDiscoveryHandler() returned an unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInitDefaults(t *testing.T) {
	tests := []struct {
		name       string
		sd         *SystemDiscovery
		lp         *log.Parameters
		fakeNewGCE onetime.GCEServiceFunc
		wantErr    bool
	}{
		{
			name: "HostDiscoveryInterfaceMissing",
			sd: &SystemDiscovery{
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			},
			lp: &log.Parameters{},
		},
		{
			name: "SAPDiscoveryInterfaceMissing",
			sd: &SystemDiscovery{
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
					DiscoverCurrentHostResp: [][]string{{}},
				},
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			},
			lp: &log.Parameters{},
		},
		{
			name: "CloudDiscoveryInterfaceMissing",
			sd: &SystemDiscovery{
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			},
			fakeNewGCE: func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			lp:         &log.Parameters{},
		},
		{
			name: "ErrorCreatingGCE",
			sd: &SystemDiscovery{
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			},
			fakeNewGCE: func(context.Context) (*gce.GCE, error) { return nil, fmt.Errorf("error creating GCE") },
			lp:         &log.Parameters{},
			wantErr:    true,
		},
		{
			name: "SetupCloudLogInterface",
			sd: &SystemDiscovery{
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
					DiscoverCurrentHostResp: [][]string{{}},
				},
				SapDiscoveryInterface: &appsdiscoveryfake.SapDiscovery{
					DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
				},
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			},
			lp: &log.Parameters{
				CloudLoggingClient: &logging.Client{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.sd.initDefaults(context.Background(), defaultCloudLoggingClient, test.fakeNewGCE)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("initDefaults() returned an unexpected error: %v, want error presence = %v", err, test.wantErr)
			}
		})
	}
}

func TestPrepareConfig(t *testing.T) {
	tests := []struct {
		name                string
		sd                  *SystemDiscovery
		cp                  *iipb.CloudProperties
		wantErr             bool
		wantCloudProperties *iipb.CloudProperties
		wantAgentProperties *cpb.AgentProperties
		wantDiscoveryConfig *cpb.DiscoveryConfiguration
	}{
		{
			name:                "SuccessNoConfigFile",
			sd:                  &SystemDiscovery{},
			cp:                  defaultCloudProperties,
			wantCloudProperties: defaultCloudProperties,
			wantAgentProperties: defaultAgentProperties,
			wantDiscoveryConfig: defaultDiscoveryConfig,
		},
		{
			name: "SuccessWithConfigFile",
			sd: &SystemDiscovery{
				ConfigPath: createTestConfigFile(t, testConfigFileJSON).Name(),
			},
			cp:                  defaultCloudProperties,
			wantCloudProperties: defaultCloudProperties,
			wantAgentProperties: defaultAgentProperties,
			wantDiscoveryConfig: testDiscoveryConfig,
		},
		{
			name: "FailConfigFileNotFound",
			sd: &SystemDiscovery{
				ConfigPath: createTestConfigFile(t, testConfigFileJSON).Name() + "sap",
			},
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "SuccessApplyDefaultParamsIfInvalid",
			sd: &SystemDiscovery{
				ConfigPath: createTestConfigFile(t, testInvalidConfigFileJSON).Name(),
			},
			cp:                  defaultCloudProperties,
			wantCloudProperties: defaultCloudProperties,
			wantAgentProperties: defaultAgentProperties,
			wantDiscoveryConfig: defaultDiscoveryConfig,
		},
		{
			name: "FailInvalidCloudProperties",
			sd: &SystemDiscovery{
				ConfigPath: createTestConfigFile(t, testConfigFileJSON).Name(),
			},
			cp: &iipb.CloudProperties{
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				NumericProjectId: "13102003",
			},
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotConfig, err := test.sd.prepareConfig(ctx, test.cp)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("prepareConfig() = %v, want error presence = %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.wantCloudProperties, gotConfig.GetCloudProperties(), protocmp.Transform()); diff != "" {
				t.Errorf("prepareConfig() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantAgentProperties, gotConfig.GetAgentProperties(), protocmp.Transform()); diff != "" {
				t.Errorf("prepareConfig() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantDiscoveryConfig, gotConfig.GetDiscoveryConfiguration(), protocmp.Transform()); diff != "" {
				t.Errorf("prepareConfig() returned an unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidateCloudProperties(t *testing.T) {
	tests := []struct {
		name string
		cp   *iipb.CloudProperties
		want bool
	}{
		{
			name: "SuccessAllFieldsPresent",
			cp:   defaultCloudProperties,
			want: true,
		},
		{
			name: "FailMissingProjectId",
			cp: &iipb.CloudProperties{
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
		},
		{
			name: "FailMissingInstanceId",
			cp: &iipb.CloudProperties{
				ProjectId:        "default-project",
				InstanceName:     "default-instance",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
		},
		{
			name: "FailMissingInstanceName",
			cp: &iipb.CloudProperties{
				ProjectId:        "default-project",
				InstanceId:       "default-instance-id",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
		},
		{
			name: "FailMissingZone",
			cp: &iipb.CloudProperties{
				ProjectId:        "default-project",
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				NumericProjectId: "13102003",
			},
		},
		{
			name: "FailMissingNumericProjectId",
			cp: &iipb.CloudProperties{
				ProjectId:    "default-project",
				InstanceId:   "default-instance-id",
				InstanceName: "default-instance",
				Zone:         "default-zone",
			},
		},
		{
			name: "FailEmptyData",
			cp: &iipb.CloudProperties{
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validateCloudProperties(test.cp)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("validateCloudProperties() returned an unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestName(t *testing.T) {
	sd := &SystemDiscovery{}
	if diff := cmp.Diff("systemdiscovery", sd.Name()); diff != "" {
		t.Errorf("Name() returned an unexpected diff (-want +got):\n%s", diff)
	}
}

func TestUsage(t *testing.T) {
	sd := &SystemDiscovery{}
	if diff := cmp.Diff(`Usage: systemdiscovery [-config=<path to config file>]
	[-loglevel=<debug|error|info|warn>] [-log-path=<log-path>] [-help]`+"\n", sd.Usage()); diff != "" {
		t.Errorf("Usage() returned an unexpected diff (-want +got):\n%s", diff)
	}
}

func TestSynopsis(t *testing.T) {
	sd := &SystemDiscovery{}
	if diff := cmp.Diff("discover SAP systems that are running on the host.", sd.Synopsis()); diff != "" {
		t.Errorf("Synopsis() returned an unexpected diff (-want +got):\n%s", diff)
	}
}

func TestSetFlags(t *testing.T) {
	sd := &SystemDiscovery{}
	flagSet := flag.NewFlagSet("flags", flag.ExitOnError)
	sd.SetFlags(flagSet)

	flags := []string{"c", "config", "h", "help", "loglevel", "log-path"}

	for _, flag := range flags {
		got := flagSet.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", flagSet, flag)
		}
	}
}
