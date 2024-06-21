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
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultCloudProperties = &iipb.CloudProperties{
		ProjectId:    "default-project",
		InstanceName: "default-instance",
		Zone:         "default-zone",
	}

	defaultDiscoveryConfig = &cpb.DiscoveryConfiguration{
		EnableDiscovery:                &wpb.BoolValue{Value: true},
		SapInstancesUpdateFrequency:    dpb.New(time.Duration(1 * time.Minute)),
		SystemDiscoveryUpdateFrequency: dpb.New(time.Duration(4 * time.Hour)),
		EnableWorkloadDiscovery:        &wpb.BoolValue{Value: true},
	}

	defaultAgentProperties = &cpb.AgentProperties{
		Name:    configuration.AgentName,
		Version: configuration.AgentVersion,
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

	testDiscoveryConfig = &cpb.DiscoveryConfiguration{
		EnableDiscovery:                &wpb.BoolValue{Value: false},
		SapInstancesUpdateFrequency:    dpb.New(time.Duration(3 * time.Second)),
		SystemDiscoveryUpdateFrequency: dpb.New(time.Duration(4 * time.Hour)),
		EnableWorkloadDiscovery:        &wpb.BoolValue{Value: true},
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
			name: "SuccessfullyParseArgs",
			sd:   &SystemDiscovery{},
			want: subcommands.ExitSuccess,
			args: []any{
				"arg_one",
				log.Parameters{},
				defaultCloudProperties,
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
			name: "SuccessForAgentVersion",
			sd: &SystemDiscovery{
				version: true,
			},
			args: []any{},
			want: subcommands.ExitSuccess,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sd.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Execute() returned an unexpected diff (-want +got): %s", diff)
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
			name: "SuccessIIOTEModeWithoutConfigFile",
			sd: &SystemDiscovery{
				IIOTEParams: &onetime.InternallyInvokedOTE{
					InvokedBy: "test",
					Lp:        log.Parameters{},
					Cp:        defaultCloudProperties,
				},
			},
			args:                []any{},
			wantDiscoveryObject: nil,
			wantErr:             nil,
		},
		{
			name: "SuccessIIOTEModeWithConfigFile",
			sd: &SystemDiscovery{
				configPath: createTestConfigFile(t, testConfigFileJSON).Name(),
				IIOTEParams: &onetime.InternallyInvokedOTE{
					InvokedBy: "test",
					Lp:        log.Parameters{},
					Cp:        defaultCloudProperties,
				},
			},
			args:                []any{},
			wantDiscoveryObject: nil,
			wantErr:             nil,
		},
		{
			name: "SuccessOTEModeWithoutConfigFile",
			sd: &SystemDiscovery{
				IIOTEParams: nil,
			},
			args: []any{
				"arg_one",
				log.Parameters{},
				defaultCloudProperties,
			},
			wantDiscoveryObject: nil,
			wantErr:             nil,
		},
		{
			name: "SuccessOTEModeWithConfigFile",
			sd: &SystemDiscovery{
				configPath:  createTestConfigFile(t, testConfigFileJSON).Name(),
				IIOTEParams: nil,
			},
			args: []any{
				"arg_one",
				log.Parameters{},
				defaultCloudProperties,
			},
			wantDiscoveryObject: nil,
			wantErr:             nil,
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
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotDiscoveryObject, gotErr := test.sd.SystemDiscoveryHandler(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
			if diff := cmp.Diff(test.wantDiscoveryObject, gotDiscoveryObject, protocmp.Transform()); diff != "" {
				t.Errorf("initialize() returned an unexpected diff (-want +got): %v, %v", test.wantDiscoveryObject, gotDiscoveryObject)
			}
			if diff := cmp.Diff(test.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("initialize() returned an unexpected diff (-want +got): %v, %v", test.wantErr, gotErr)
			}
		})
	}
}

func TestPrepareConfig(t *testing.T) {
	tests := []struct {
		name       string
		sd         *SystemDiscovery
		cp         *iipb.CloudProperties
		args       []any
		wantErr    error
		wantConfig *cpb.Configuration
	}{
		{
			name:    "SuccessNoConfigFile",
			sd:      &SystemDiscovery{},
			cp:      defaultCloudProperties,
			args:    []any{},
			wantErr: nil,
			wantConfig: &cpb.Configuration{
				CloudProperties:        defaultCloudProperties,
				AgentProperties:        defaultAgentProperties,
				DiscoveryConfiguration: defaultDiscoveryConfig,
			},
		},
		{
			name: "SuccessWithConfigFile",
			sd: &SystemDiscovery{
				configPath: createTestConfigFile(t, testConfigFileJSON).Name(),
			},
			cp:      defaultCloudProperties,
			args:    []any{},
			wantErr: nil,
			wantConfig: &cpb.Configuration{
				CloudProperties:        defaultCloudProperties,
				AgentProperties:        defaultAgentProperties,
				DiscoveryConfiguration: testDiscoveryConfig,
			},
		},
		{
			name: "FailConfigFileNotFound",
			sd: &SystemDiscovery{
				configPath: createTestConfigFile(t, testConfigFileJSON).Name() + "sap",
			},
			cp:         defaultCloudProperties,
			args:       []any{},
			wantErr:    cmpopts.AnyError,
			wantConfig: nil,
		},
		{
			name: "FailConfigInvalidParams",
			sd: &SystemDiscovery{
				configPath: createTestConfigFile(t, testInvalidConfigFileJSON).Name(),
			},
			cp:         defaultCloudProperties,
			args:       []any{},
			wantErr:    cmpopts.AnyError,
			wantConfig: nil,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotConfig, gotErr := test.sd.prepareConfig(ctx, test.cp, test.args...)
			if diff := cmp.Diff(test.wantConfig, gotConfig, protocmp.Transform()); diff != "" {
				t.Errorf("initialize() returned an unexpected diff (-want +got): %v, %v", test.wantConfig, gotConfig)
			}
			if diff := cmp.Diff(test.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("initialize() returned an unexpected diff (-want +got): %v, %v", test.wantErr, gotErr)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	sd := &SystemDiscovery{}
	flagSet := flag.NewFlagSet("flags", flag.ExitOnError)
	sd.SetFlags(flagSet)

	flags := []string{
		"c", "config", "h", "help", "loglevel", "v", "version",
	}

	for _, flag := range flags {
		got := flagSet.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", flagSet, flag)
		}
	}
}
