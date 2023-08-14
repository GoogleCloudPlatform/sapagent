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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	compute "google.golang.org/api/compute/v0.alpha"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fakehttp"
)

// newService intializes a GCE client and plumbs it into a fake HTTP server.
// Remember to close the *httptest.Server after use.
func newService(ctx context.Context, responses []fakehttp.HardCodedResponse) (*GCEAlpha, *httptest.Server, error) {
	c, err := compute.New(&http.Client{})
	if err != nil {
		return nil, nil, errors.Wrap(err, "error creating GCE client")
	}

	s := fakehttp.NewServer(responses)
	c.BasePath = s.URL
	return &GCEAlpha{c}, s, nil
}

// responseJSON takes a compute.Instance struct and formats as JSON, ignoring errors.
func responseJSON(c *compute.Instance) []byte {
	JSONString, _ := (c).MarshalJSON()
	return JSONString
}

func TestGetInstance(t *testing.T) {
	ctx := context.Background()
	wantInstance := &compute.Instance{
		Name: "instance1",
		Zone: "testZone",
	}
	g, s, err := newService(ctx, []fakehttp.HardCodedResponse{
		fakehttp.HardCodedResponse{
			RequestMethod:      "GET",
			RequestEscapedPath: "/projects/testProject/zones/testZone/instances/testInstance",
			Response:           responseJSON(wantInstance),
		},
	})
	defer s.Close()
	if err != nil {
		t.Fatalf("error constructing a GCEAlpha client: %v", err)
	}
	r, err := g.GetInstance("testProject", "testZone", "testInstance")
	if err != nil {
		t.Errorf("GetInstance() returned error: %v", err)
	}
	cmpOpts := []cmp.Option{
		cmp.FilterPath(func(p cmp.Path) bool {
			return p.Last().String() == ".ServerResponse"
		}, cmp.Ignore()), // Ignore the raw HTTP response
	}
	if diff := cmp.Diff(wantInstance, r, cmpOpts...); diff != "" {
		t.Errorf("GetInstance() diff (-want +got):\n%s", diff)
	}
}
