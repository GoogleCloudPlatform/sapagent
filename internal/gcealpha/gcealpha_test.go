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

package gcealpha

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"google.golang.org/api/compute/v0.alpha"
	"google.golang.org/api/googleapi"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fakehttp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

// cmpCodeOnly ignores the error message string, allowing comparisons on the error code only.
var cmpCodeOnly = cmpopts.IgnoreFields(
	googleapi.Error{},
	"Body",
	"Header",
	"err",
)

// newTestService initializes a GCE client and plumbs it into a fake HTTP server.
// Remember to close the *httptest.Server after use.
func newTestService(ctx context.Context, responses []fakehttp.HardCodedResponse) (*GCEAlpha, *httptest.Server, error) {
	c, err := compute.New(&http.Client{})
	if err != nil {
		return nil, nil, errors.Wrap(err, "error creating GCE client")
	}

	s := fakehttp.NewServer(responses)
	c.BasePath = s.URL
	return &GCEAlpha{c}, s, nil
}

func TestGetInstance(t *testing.T) {
	tests := []struct {
		name             string
		project          string
		zone             string
		instance         string
		overrideBasePath string
		responsePath     string
		wantErr          error
		want             *compute.Instance
	}{{
		name:         "Success",
		project:      "testProject",
		zone:         "testZone",
		instance:     "testInstance",
		responsePath: "/projects/testProject/zones/testZone/instances/testInstance",
		want: &compute.Instance{
			Name: "instance1",
			Zone: "testZone",
		},
	}, {
		name:             "OverrideBasePath",
		project:          "testProject",
		zone:             "testZone",
		instance:         "testInstance",
		overrideBasePath: "/testPathComponent/",
		responsePath:     "/testPathComponent/projects/testProject/zones/testZone/instances/testInstance",
		want: &compute.Instance{
			Name: "instance1",
			Zone: "testZone",
		},
	}, {
		name:         "NonexistentInstance",
		project:      "testProject",
		zone:         "testZone",
		instance:     "nonexistent",
		responsePath: "/projects/testProject/zones/testZone/instances/testInstance",
		wantErr:      &googleapi.Error{Code: http.StatusNotFound},
	}}

	for _, tc := range tests {
		responses := []fakehttp.HardCodedResponse{}
		if tc.want != nil {
			response, err := (tc.want).MarshalJSON()
			if err != nil {
				t.Errorf("Could not parse JSON for %+v: %v", tc.want, err)
				continue
			}
			responses = []fakehttp.HardCodedResponse{
				fakehttp.HardCodedResponse{
					RequestMethod:      "GET",
					RequestEscapedPath: tc.responsePath,
					Response:           response,
				},
			}
		}
		g, s, err := newTestService(context.Background(), responses)
		defer s.Close()
		if err != nil {
			t.Fatalf("error constructing a GCEAlpha client: %v", err)
		}
		g.OverrideComputeBasePath(s.URL + tc.overrideBasePath)
		got, err := g.GetInstance(tc.project, tc.zone, tc.instance)
		if diff := cmp.Diff(tc.wantErr, err, cmpCodeOnly); diff != "" {
			t.Errorf("GetInstance(%s, %s, %s) returned an unexpected error (-want +got): %v", tc.project, tc.zone, tc.instance, diff)
		}
		if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreTypes(googleapi.ServerResponse{})); diff != "" {
			t.Errorf("GetInstance(%s, %s, %s) returned an unexpected diff (-want +got): %v", tc.project, tc.zone, tc.instance, diff)
		}
	}
}

func TestInitialized(t *testing.T) {
	tests := []struct {
		name    string
		gce     *GCEAlpha
		setup   func(context.Context) (*GCEAlpha, func(), error)
		want    bool
		wantErr bool
	}{
		{
			name: "ServiceNotInitialized",
			gce:  &GCEAlpha{service: nil},
			want: false,
		},
		{
			name: "ServiceInitialized",
			setup: func(ctx context.Context) (*GCEAlpha, func(), error) {
				g, s, err := newTestService(ctx, []fakehttp.HardCodedResponse{})
				if err != nil {
					return nil, nil, err
				}
				return g, func() { s.Close() }, nil
			},
			want: true,
		},
		{
			name: "NewGCEClientError",
			setup: func(ctx context.Context) (*GCEAlpha, func(), error) {
				g, err := NewGCEClient(context.TODO())
				return g, func() {}, err
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var g *GCEAlpha
			var err error
			if tc.setup != nil {
				var cleanup func()
				g, cleanup, err = tc.setup(context.Background())
				if cleanup != nil {
					defer cleanup()
				}
				if tc.wantErr {
					if err == nil {
						t.Errorf("NewGCEClient() did not return an error as expected.")
					}
					if g != nil && g.Initialized() != tc.want {
						t.Errorf("Initialized() got = %v, want = %v for a client that failed to initialize", g.Initialized(), tc.want)
					} else if g == nil && tc.want {
						t.Errorf("Initialized() expected %v for nil client, but this scenario implies false", tc.want)
					}
					return
				}
				if err != nil {
					t.Fatalf("setup() failed: %v", err)
				}
			} else {
				g = tc.gce
			}

			got := g.Initialized()
			if got != tc.want {
				t.Errorf("Initialized() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListNodeGroupNodes(t *testing.T) {
	tests := []struct {
		project      string
		zone         string
		nodeGroup    string
		responsePath string
		want         *compute.NodeGroupsListNodes
	}{{
		project:      "testProject",
		zone:         "testZone",
		nodeGroup:    "testNodeGroup",
		responsePath: "/projects/testProject/zones/testZone/nodeGroups/testNodeGroup/listNodes",
		want: &compute.NodeGroupsListNodes{
			Items: []*compute.NodeGroupNode{{
				Name:      "nodeGroup1",
				Instances: []string{"instance1", "instance2"},
			},
			},
		}}}

	for _, tc := range tests {
		response, err := (tc.want).MarshalJSON()
		if err != nil {
			t.Errorf("Could not parse JSON for %+v: %v", tc.want, err)
			continue
		}
		responses := []fakehttp.HardCodedResponse{
			fakehttp.HardCodedResponse{
				RequestMethod:      "POST",
				RequestEscapedPath: tc.responsePath,
				Response:           response,
			},
		}
		g, s, err := newTestService(context.Background(), responses)
		defer s.Close()
		if err != nil {
			t.Fatalf("error constructing a GCEAlpha client: %v", err)
		}
		got, err := g.ListNodeGroupNodes(tc.project, tc.zone, tc.nodeGroup)
		if err != nil {
			t.Errorf("ListNodeGroupNodes(%v, %v, %v) returned an unexpected error: %v", tc.project, tc.zone, tc.nodeGroup, err)
			continue
		}

		if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreTypes(googleapi.ServerResponse{})); diff != "" {
			t.Errorf("ListNodeGroupNodes(%v, %v, %v) returned an unexpected diff (-want +got): %v", tc.project, tc.zone, tc.nodeGroup, diff)
		}
	}
}

func TestListNodeGroups(t *testing.T) {
	tests := []struct {
		project      string
		zone         string
		responsePath string
		want         *compute.NodeGroupList
	}{{
		project:      "testProject",
		zone:         "testZone",
		responsePath: "/projects/testProject/zones/testZone/nodeGroups",
		want: &compute.NodeGroupList{Items: []*compute.NodeGroup{{
			Name: "nodeGroup1",
		}}},
	}}

	for _, tc := range tests {
		response, err := (tc.want).MarshalJSON()
		if err != nil {
			t.Errorf("Could not parse JSON for %+v: %v", tc.want, err)
			continue
		}
		responses := []fakehttp.HardCodedResponse{
			fakehttp.HardCodedResponse{
				RequestMethod:      "GET",
				RequestEscapedPath: tc.responsePath,
				Response:           response,
			},
		}
		g, s, err := newTestService(context.Background(), responses)
		defer s.Close()
		if err != nil {
			t.Fatalf("error constructing a GCEAlpha client: %v", err)
		}
		got, err := g.ListNodeGroups(tc.project, tc.zone)
		if err != nil {
			t.Errorf("ListNodeGroups(%v, %v) returned an unexpected error: %v", tc.project, tc.zone, err)
			continue
		}

		if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreTypes(googleapi.ServerResponse{})); diff != "" {
			t.Errorf("ListNodeGroups(%v, %v) returned an unexpected diff (-want +got): %v", tc.project, tc.zone, diff)
		}
	}
}
