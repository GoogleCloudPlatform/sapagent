/*
Copyright 2023 Google LLC

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

package remotevalidation

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	dpb "google.golang.org/protobuf/types/known/durationpb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

type fakeDiscoveryInterface struct {
	systems   []*spb.SapDiscovery
	instances *sapb.SAPInstances
}

func (d *fakeDiscoveryInterface) GetSAPSystems() []*spb.SapDiscovery  { return d.systems }
func (d *fakeDiscoveryInterface) GetSAPInstances() *sapb.SAPInstances { return d.instances }

func TestExecute(t *testing.T) {
	defaultLoadOptions := collectiondefinition.LoadOptions{
		CollectionConfig: &cpb.CollectionConfiguration{
			WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
				FetchLatestConfig: wpb.Bool(false),
			},
		},
		ReadFile: func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
		OSType:   "linux",
		Version:  "1.0",
	}

	tests := []struct {
		name        string
		remote      *RemoteValidation
		loadOptions collectiondefinition.LoadOptions
		want        subcommands.ExitStatus
	}{
		{
			name: "SuccessForHelp",
			remote: &RemoteValidation{
				help: true,
			},
			loadOptions: defaultLoadOptions,
			want:        subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.remote.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, instanceinfo.New(nil, nil), test.loadOptions)
			if got != test.want {
				t.Errorf("Execute(%v) = %v, want %v", test.remote, got, test.want)
			}
		})
	}
}

func TestRemoteValidationHandler(t *testing.T) {
	defaultLoadOptions := collectiondefinition.LoadOptions{
		CollectionConfig: &cpb.CollectionConfiguration{
			WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
				FetchLatestConfig: wpb.Bool(false),
			},
		},
		ReadFile: func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
		OSType:   "linux",
		Version:  configuration.AgentVersion,
	}

	tests := []struct {
		name        string
		remote      *RemoteValidation
		loadOptions collectiondefinition.LoadOptions
		discovery   *fakeDiscoveryInterface
		want        subcommands.ExitStatus
	}{
		{
			name: "EmptyProject",
			remote: &RemoteValidation{
				instanceid: "instance-1",
				zone:       "zone-1",
			},
			loadOptions: defaultLoadOptions,
			discovery:   &fakeDiscoveryInterface{},
			want:        subcommands.ExitUsageError,
		},
		{
			name:        "EmptyInstanceID",
			remote:      &RemoteValidation{},
			loadOptions: defaultLoadOptions,
			discovery:   &fakeDiscoveryInterface{},
			want:        subcommands.ExitUsageError,
		},
		{
			name: "EmptyZone",
			remote: &RemoteValidation{
				project: "project-1",
			},
			loadOptions: defaultLoadOptions,
			discovery:   &fakeDiscoveryInterface{},
			want:        subcommands.ExitUsageError,
		},
		{
			name: "ConfigErrNotExist",
			remote: &RemoteValidation{
				project:    "project-1",
				instanceid: "instance-1",
				zone:       "zone-1",
				config:     "/path/does/not/exist",
			},
			loadOptions: defaultLoadOptions,
			discovery:   &fakeDiscoveryInterface{},
			want:        subcommands.ExitFailure,
		},
		{
			name: "ConfigReadFileError",
			remote: &RemoteValidation{
				project:    "project-1",
				instanceid: "instance-1",
				zone:       "zone-1",
				config:     "/tmp/workload-validation.json",
			},
			loadOptions: collectiondefinition.LoadOptions{
				ReadFile: func(s string) ([]byte, error) { return nil, errors.New("ReadFile Error") },
				OSType:   "linux",
				Version:  configuration.AgentVersion,
			},
			discovery: &fakeDiscoveryInterface{},
			want:      subcommands.ExitFailure,
		},
		{
			name: "ConfigUnmarshalError",
			remote: &RemoteValidation{
				project:    "project-1",
				instanceid: "instance-1",
				zone:       "zone-1",
				config:     "/tmp/workload-validation.json",
			},
			loadOptions: collectiondefinition.LoadOptions{
				ReadFile: func(s string) ([]byte, error) { return []byte("invalid json"), nil },
				OSType:   "linux",
				Version:  configuration.AgentVersion,
			},
			discovery: &fakeDiscoveryInterface{},
			want:      subcommands.ExitFailure,
		},
		{
			name: "ConfigSuccess",
			remote: &RemoteValidation{
				project:    "project-1",
				instanceid: "instance-1",
				zone:       "zone-1",
				config:     "/tmp/workload-validation.json",
			},
			loadOptions: collectiondefinition.LoadOptions{
				ReadFile: func(s string) ([]byte, error) { return []byte("{}"), nil },
				OSType:   "linux",
				Version:  configuration.AgentVersion,
			},
			discovery: &fakeDiscoveryInterface{},
			want:      subcommands.ExitSuccess,
		},
		{
			name: "CollectionDefinitionLoadError",
			remote: &RemoteValidation{
				project:    "project-1",
				instanceid: "instance-1",
				zone:       "zone-1",
			},
			loadOptions: collectiondefinition.LoadOptions{
				CollectionConfig: &cpb.CollectionConfiguration{
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						FetchLatestConfig: wpb.Bool(false),
					},
				},
				ReadFile: func(s string) ([]byte, error) { return nil, errors.New("ReadFile Error") },
				OSType:   "linux",
				Version:  configuration.AgentVersion,
			},
			discovery: &fakeDiscoveryInterface{},
			want:      subcommands.ExitFailure,
		},
		{
			name: "CollectionDefinitionLoadSuccess",
			remote: &RemoteValidation{
				project:    "project-1",
				instanceid: "instance-1",
				zone:       "zone-1",
			},
			loadOptions: defaultLoadOptions,
			discovery:   &fakeDiscoveryInterface{},
			want:        subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.remote.remoteValidationHandler(context.Background(), handlerOptions{
				config:    test.remote.createConfiguration(),
				iir:       instanceinfo.New(nil, nil),
				loadOpts:  test.loadOptions,
				discovery: test.discovery,
			})
			if got != test.want {
				t.Errorf("remoteValidationHandler(%v) = %v, want %v", test.remote, got, test.want)
			}
		})
	}
}

func TestUsageForRemoteValidation(t *testing.T) {
	want := "Usage: remote -project=<project-id> -instance=<instance-id> -name=<instance-name> -zone=<instance-zone> [-h]\n"
	rv := RemoteValidation{}
	got := rv.Usage()
	if got != want {
		t.Errorf("Usage() = %v, want %v", got, want)
	}
}

func TestSetFlagsForRemoteValidation(t *testing.T) {
	flags := []string{"project", "instance", "zone", "name"}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	remoteValidation := RemoteValidation{}
	remoteValidation.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestCreateConfiguration(t *testing.T) {
	project := "test-project"
	instanceID := "test-instanceid"
	instanceName := "test-instancename"
	zone := "test-zone"

	want := &cpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			ProjectId:    project,
			InstanceId:   instanceID,
			InstanceName: instanceName,
			Zone:         zone,
		},
		AgentProperties: &cpb.AgentProperties{
			Name:    configuration.AgentName,
			Version: configuration.AgentVersion,
		},
		DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
			EnableDiscovery:                wpb.Bool(true),
			SapInstancesUpdateFrequency:    dpb.New(time.Duration(1 * time.Hour)),
			SystemDiscoveryUpdateFrequency: dpb.New(time.Duration(1 * time.Hour)),
		},
	}

	r := &RemoteValidation{project: project, instanceid: instanceID, instancename: instanceName, zone: zone}
	got := r.createConfiguration()
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("createConfiguration() returned unexpected diff (-want +got):\n%s", diff)
	}
}
