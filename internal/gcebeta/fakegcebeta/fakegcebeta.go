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

// Package fakegcebeta provides a fake version of the GCE struct to return canned responses in unit tests.
package fakegcebeta

import (
	"net/http"

	"google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/googleapi"
)

// TestGCE implements GCE interfaces. A new TestGCE instance should be used per iteration of the test.
type TestGCE struct {
	Instances      []*compute.Instance
	NodeGroups     []*compute.NodeGroup
	NodeGroupNodes *compute.NodeGroupsListNodes
	Project        string
	Zone           string
}

// Initialized fakes a check if the fake has been initialized.
func (g *TestGCE) Initialized() bool { return true }

// GetInstance fakes a call to the compute API to retrieve a GCE Instance.
func (g *TestGCE) GetInstance(project, zone, instance string) (*compute.Instance, error) {
	if project != g.Project || zone != g.Zone || g.Instances == nil {
		return nil, &googleapi.Error{Code: http.StatusNotFound}
	}
	for _, i := range g.Instances {
		if instance == i.Name {
			return i, nil
		}
	}
	return nil, &googleapi.Error{Code: http.StatusNotFound}
}

// ListNodeGroups fakely retrieves the node groups for a given project and zone.
func (g *TestGCE) ListNodeGroups(project, zone string) (*compute.NodeGroupList, error) {
	if project != g.Project || zone != g.Zone || g.NodeGroups == nil {
		return nil, &googleapi.Error{Code: http.StatusNotFound}
	}
	builder := []*compute.NodeGroup{}
	for _, ng := range g.NodeGroups {
		if zone == ng.Zone {
			builder = append(builder, ng)
		}
	}
	return &compute.NodeGroupList{Items: builder}, nil
}

// ListNodeGroupNodes fakely lists the nodes in a given node group.
func (g *TestGCE) ListNodeGroupNodes(project, zone, nodeGroup string) (*compute.NodeGroupsListNodes, error) {
	if project != g.Project || zone != g.Zone || g.NodeGroupNodes == nil {
		return nil, &googleapi.Error{Code: http.StatusNotFound}
	}
	return g.NodeGroupNodes, nil
}
