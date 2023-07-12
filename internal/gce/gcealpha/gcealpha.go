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

// Package gcealpha is a lightweight testable wrapper around GCE compute alpha APIs.
package gcealpha

import (
	"context"

	"github.com/pkg/errors"
	compute "google.golang.org/api/compute/v0.alpha"
)

// GCEAlpha is a wrapper for Google Compute Engine services.
type GCEAlpha struct {
	service *compute.Service
}

// NewGCEClient creates a new GCE service wrapper.
func NewGCEClient(ctx context.Context) (*GCEAlpha, error) {
	s, err := compute.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error creating GCE alpha client")
	}

	return &GCEAlpha{s}, nil
}

// GetInstance retrieves a GCE Instance defined by the project, zone, and name provided.
func (g *GCEAlpha) GetInstance(project, zone, instance string) (*compute.Instance, error) {
	return g.service.Instances.Get(project, zone, instance).Do()
}

// ListNodes retrieves a node group's contents for a given project, zone, and node group name.
func (g *GCEAlpha) ListNodes(project, zone, nodeGroup string) (*compute.NodeGroupsListNodes, error) {
	return g.service.NodeGroups.ListNodes(project, zone, nodeGroup).Do()
}
