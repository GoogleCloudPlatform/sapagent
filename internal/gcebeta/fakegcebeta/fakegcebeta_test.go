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

// Package fakegcebeta has unit tests for the gcebeta fake.
package fakegcebeta

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmpopts/cmpopts"
	compute "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/googleapi"
)

// cmpCodeOnly ignores the error message string, allowing comparisons on the error code only.
var cmpCodeOnly = cmpopts.IgnoreFields(
	googleapi.Error{},
	"Body",
	"Header",
	"err",
)
var defaultInstance = TestGCE{
	Instances: []*compute.Instance{{
		Name: "testInstance",
		Zone: "testZone",
	}},
	NodeGroups: []*compute.NodeGroup{{
		Name: "testNodeGroup",
		Zone: "testZone",
	}, {
		Name: "otherNodeGroup",
		Zone: "testZone",
	}},
	NodeGroupNodes: &compute.NodeGroupsListNodes{
		Items: []*compute.NodeGroupNode{{
			Name:      "testNodeGroupNode",
			Instances: []string{"testInstance", "otherInstance"},
		}}},
	Project: "testProject",
	Zone:    "testZone",
}

func TestGetInstance(t *testing.T) {
	tests := []struct {
		name     string
		project  string
		zone     string
		instance string
		fake     *TestGCE
		wantErr  error
		want     *compute.Instance
	}{{
		name:     "Success",
		project:  "testProject",
		zone:     "testZone",
		instance: "testInstance",
		fake:     &defaultInstance,
		want: &compute.Instance{
			Name: "testInstance",
			Zone: "testZone",
		},
	}, {
		name:    "errProjectNotFound",
		project: "non-matching-project",
		fake:    &defaultInstance,
		wantErr: &googleapi.Error{Code: http.StatusNotFound},
	}, {
		name:    "errZoneNotFound",
		project: "testProject",
		zone:    "non-matching-zone",
		fake:    &defaultInstance,
		wantErr: &googleapi.Error{Code: http.StatusNotFound},
	}, {
		name:     "errInstanceNotFound",
		project:  "testProject",
		zone:     "testZone",
		instance: "non-matching-instance",
		fake:     &defaultInstance,
		wantErr:  &googleapi.Error{Code: http.StatusNotFound},
	},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fake.GetInstance(tc.project, tc.zone, tc.instance)
			if diff := cmp.Diff(tc.wantErr, err, cmpCodeOnly); diff != "" {
				t.Errorf("GetInstance(%s) returned an unexpected error (-want +got): %v", tc.name, diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("GetInstance(%s) returned an unexpected diff (-want +got): %v", tc.name, diff)
			}
		})
	}
}

func TestListNodeGroups(t *testing.T) {
	tests := []struct {
		name    string
		project string
		zone    string
		fake    *TestGCE
		wantErr error
		want    *compute.NodeGroupList
	}{{
		name:    "Success",
		project: "testProject",
		zone:    "testZone",
		fake:    &defaultInstance,
		want: &compute.NodeGroupList{
			Items: []*compute.NodeGroup{{
				Name: "testNodeGroup",
				Zone: "testZone",
			}, {
				Name: "otherNodeGroup",
				Zone: "testZone",
			}},
		},
	}, {
		name:    "errProjectNotFound",
		project: "non-matching-project",
		zone:    "non-matching-zone",
		fake:    &defaultInstance,
		wantErr: &googleapi.Error{Code: http.StatusNotFound},
	}, {
		name:    "errZoneNotFound",
		project: "testProject",
		zone:    "non-matching-zone",
		fake:    &defaultInstance,
		wantErr: &googleapi.Error{Code: http.StatusNotFound},
	},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fake.ListNodeGroups(tc.project, tc.zone)
			if diff := cmp.Diff(tc.wantErr, err, cmpCodeOnly); diff != "" {
				t.Errorf("ListNodeGroups(%s) returned an unexpected error (-want +got): %v", tc.name, diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ListNodeGroups(%s) returned an unexpected diff (-want +got): %v", tc.name, diff)
			}
		})
	}
}

func TestListNodeGroupNodes(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		zone      string
		nodeGroup string
		fake      *TestGCE
		wantErr   error
		want      *compute.NodeGroupsListNodes
	}{{
		name:      "Success",
		project:   "testProject",
		zone:      "testZone",
		nodeGroup: "testNodeGroup",
		fake:      &defaultInstance,
		want: &compute.NodeGroupsListNodes{
			Items: []*compute.NodeGroupNode{{
				Name:      "testNodeGroupNode",
				Instances: []string{"testInstance", "otherInstance"},
			}}}},
		{
			name:    "errProjectNotFound",
			project: "non-matching-project",
			zone:    "non-matching-zone",
			fake:    &defaultInstance,
			wantErr: &googleapi.Error{Code: http.StatusNotFound},
		}, {
			name:    "errZoneNotFound",
			project: "testProject",
			zone:    "non-matching-zone",
			fake:    &defaultInstance,
			wantErr: &googleapi.Error{Code: http.StatusNotFound},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fake.ListNodeGroupNodes(tc.project, tc.zone, tc.nodeGroup)
			if diff := cmp.Diff(tc.wantErr, err, cmpCodeOnly); diff != "" {
				t.Errorf("ListNodeGroups(%s) returned an unexpected error (-want +got): %v", tc.name, diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ListNodeGroups(%s) returned an unexpected diff (-want +got): %v", tc.name, diff)
			}
		})
	}
}
