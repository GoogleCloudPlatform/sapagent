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
	"os"
	"testing"
	"time"

	"flag"
	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
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
		AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
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
				IIOTEParams:   nil,
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
				IIOTEParams:   nil,
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
		args                []any
		wantDiscoveryObject *system.Discovery
		wantErr             error
	}{
		{
			name:                "SuccessIIOTEModeWithoutConfigFile",
			sd:                  createTestIIOTESystemDiscovery(t, ""),
			args:                []any{},
			wantDiscoveryObject: &system.Discovery{},
			wantErr:             nil,
		},
		{
			name:                "SuccessIIOTEModeWithConfigFile",
			sd:                  createTestIIOTESystemDiscovery(t, createTestConfigFile(t, testConfigFileJSON).Name()),
			args:                []any{},
			wantDiscoveryObject: &system.Discovery{},
			wantErr:             nil,
		},
		{
			name:                "FailConfigFileNotFound",
			sd:                  createTestIIOTESystemDiscovery(t, createTestConfigFile(t, testConfigFileJSON).Name()+"sap"),
			args:                []any{},
			wantDiscoveryObject: nil,
			wantErr:             cmpopts.AnyError,
		},
		{
			name: "FailIIOTEParamsAndArgsNotPassed",
			sd: &SystemDiscovery{
				IIOTEParams: nil,
			},
			args:                []any{},
			wantDiscoveryObject: nil,
			wantErr:             cmpopts.AnyError,
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
			args:                []any{},
			wantDiscoveryObject: &system.Discovery{},
			wantErr:             nil,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotDiscoveryObject, gotErr := test.sd.SystemDiscoveryHandler(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
			if test.wantDiscoveryObject != nil && gotDiscoveryObject != nil {
				return
			}
			if diff := cmp.Diff(test.wantDiscoveryObject, gotDiscoveryObject, protocmp.Transform()); diff != "" {
				t.Errorf("SystemDiscoveryHandler() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("SystemDiscoveryHandler() returned an unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInitDefaults(t *testing.T) {
	tests := []struct {
		name    string
		sd      *SystemDiscovery
		lp      *log.Parameters
		wantErr bool
	}{
		{
			name: "HostDiscoveryInterfaceMissing",
			sd: &SystemDiscovery{
				CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
					DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
				},
				AppsDiscovery: func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
				IIOTEParams:   nil,
			},
			lp:      &log.Parameters{},
			wantErr: false,
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
				IIOTEParams:   nil,
			},
			lp:      &log.Parameters{},
			wantErr: false,
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
				IIOTEParams:   nil,
			},
			lp: &log.Parameters{
				CloudLoggingClient: &logging.Client{},
			},
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotErr := test.sd.initDefaults(context.Background(), test.lp) != nil
			if gotErr != test.wantErr {
				t.Errorf("initDefaults(%v) = %v, want error presence = %v", test.sd, gotErr, test.wantErr)
			}
		})
	}
}

func TestPrepareConfig(t *testing.T) {
	tests := []struct {
		name                string
		sd                  *SystemDiscovery
		cp                  *iipb.CloudProperties
		args                []any
		wantErr             error
		wantCloudProperties *iipb.CloudProperties
		wantAgentProperties *cpb.AgentProperties
		wantDiscoveryConfig *cpb.DiscoveryConfiguration
	}{
		{
			name:                "SuccessNoConfigFile",
			sd:                  &SystemDiscovery{},
			cp:                  defaultCloudProperties,
			args:                []any{},
			wantErr:             nil,
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
			args:                []any{},
			wantErr:             nil,
			wantCloudProperties: defaultCloudProperties,
			wantAgentProperties: defaultAgentProperties,
			wantDiscoveryConfig: testDiscoveryConfig,
		},
		{
			name: "FailConfigFileNotFound",
			sd: &SystemDiscovery{
				ConfigPath: createTestConfigFile(t, testConfigFileJSON).Name() + "sap",
			},
			cp:                  defaultCloudProperties,
			args:                []any{},
			wantErr:             cmpopts.AnyError,
			wantCloudProperties: nil,
			wantAgentProperties: nil,
			wantDiscoveryConfig: nil,
		},
		{
			name: "SuccessApplyDefaultParamsIfInvalid",
			sd: &SystemDiscovery{
				ConfigPath: createTestConfigFile(t, testInvalidConfigFileJSON).Name(),
			},
			cp:                  defaultCloudProperties,
			args:                []any{},
			wantErr:             nil,
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
				ProjectId:        "",
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				NumericProjectId: "13102003",
			},
			args:                []any{},
			wantErr:             cmpopts.AnyError,
			wantCloudProperties: nil,
			wantAgentProperties: nil,
			wantDiscoveryConfig: nil,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotConfig, gotErr := test.sd.prepareConfig(ctx, test.cp, test.args...)
			if diff := cmp.Diff(test.wantCloudProperties, gotConfig.GetCloudProperties(), protocmp.Transform()); diff != "" {
				t.Errorf("prepareConfig() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantAgentProperties, gotConfig.GetAgentProperties(), protocmp.Transform()); diff != "" {
				t.Errorf("prepareConfig() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantDiscoveryConfig, gotConfig.GetDiscoveryConfiguration(), protocmp.Transform()); diff != "" {
				t.Errorf("prepareConfig() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
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
			want: false,
		},
		{
			name: "FailMissingInstanceId",
			cp: &iipb.CloudProperties{
				ProjectId:        "default-project",
				InstanceName:     "default-instance",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
			want: false,
		},
		{
			name: "FailMissingInstanceName",
			cp: &iipb.CloudProperties{
				ProjectId:        "default-project",
				InstanceId:       "default-instance-id",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
			want: false,
		},
		{
			name: "FailMissingZone",
			cp: &iipb.CloudProperties{
				ProjectId:        "default-project",
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				NumericProjectId: "13102003",
			},
			want: false,
		},
		{
			name: "FailMissingNumericProjectId",
			cp: &iipb.CloudProperties{
				ProjectId:    "default-project",
				InstanceId:   "default-instance-id",
				InstanceName: "default-instance",
				Zone:         "default-zone",
			},
			want: false,
		},
		{
			name: "FailEmptyData",
			cp: &iipb.CloudProperties{
				ProjectId:        "",
				InstanceId:       "default-instance-id",
				InstanceName:     "default-instance",
				Zone:             "default-zone",
				NumericProjectId: "13102003",
			},
			want: false,
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
