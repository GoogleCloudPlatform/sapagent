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

// Package iam provides a wrapper for IAM APIs offered by google cloud libraries.
package iam

import (
	"context"
	"fmt"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/storage"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// SecretManager is an interface for the Secret Manager client.
type SecretManager interface {
	TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest, opts ...gax.CallOption) (*iampb.TestIamPermissionsResponse, error)
}

// IAM is a wrapper for IAM APIs offered by google cloud libraries.
type IAM struct {
	disksService     *compute.DisksService
	instancesService *compute.InstancesService
	projectsService  *cloudresourcemanager.ProjectsService
	smClient         SecretManager
	storageClient    *storage.Client
}

// NewIAMClient creates a new IAM API wrapper.
func NewIAMClient(ctx context.Context, opts ...option.ClientOption) (*IAM, error) {
	computeService, err := compute.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error creating GCE client: %w", err)
	}
	smClient, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error creating secret manager client: %w", err)
	}
	crmService, err := cloudresourcemanager.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error creating cloud resource manager client: %w", err)
	}
	storageClient, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("error creating storage client: %w", err)
	}
	return &IAM{
		instancesService: computeService.Instances,
		disksService:     computeService.Disks,
		projectsService:  crmService.Projects,
		smClient:         smClient,
		storageClient:    storageClient,
	}, nil
}

// CheckIAMPermissionsOnDisk checks if the required IAM permissions are present
// for the caller's identity (service account, etc) on a given disk.
//
// Returns the list of input permissions that are granted.
func (i *IAM) CheckIAMPermissionsOnDisk(ctx context.Context, project, zone, diskName string, permissions []string) (grantedPermissions []string, err error) {
	if i.disksService == nil {
		return nil, fmt.Errorf("disksService is not initialized")
	}
	if permissions == nil {
		return nil, nil
	}
	req := &compute.TestPermissionsRequest{
		Permissions: permissions,
	}

	resp, err := i.disksService.TestIamPermissions(project, zone, diskName, req).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to test IAM permissions for disk: %w", err)
	}

	return resp.Permissions, nil
}

// CheckIAMPermissionsOnProject checks if the required IAM permissions are
// present for the caller's identity (service account, etc) on a given project.
//
// Returns the list of input permissions that are granted.
func (i *IAM) CheckIAMPermissionsOnProject(ctx context.Context, projectID string, permissions []string) (grantedPermissions []string, err error) {
	if i.projectsService == nil {
		return nil, fmt.Errorf("projectsService is not initialized")
	}
	if permissions == nil {
		return nil, nil
	}
	req := &cloudresourcemanager.TestIamPermissionsRequest{
		Permissions: permissions,
	}
	resp, err := i.projectsService.TestIamPermissions(projectID, req).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to test IAM permissions for project: %w", err)
	}

	return resp.Permissions, nil
}

// CheckIAMPermissionsOnBucket checks if the required IAM permissions are
// present for the caller's identity (service account, etc) on a given bucket,
// based on the connect params.
//
// Returns the list of input permissions that are granted.
func (i *IAM) CheckIAMPermissionsOnBucket(ctx context.Context, bucketName string, permissions []string) (grantedPermissions []string, err error) {
	if i.storageClient == nil {
		return nil, fmt.Errorf("storageClient is not initialized")
	}
	if permissions == nil {
		return nil, nil
	}
	grantedPermissions, err = i.storageClient.Bucket(bucketName).IAM().TestPermissions(ctx, permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to test IAM permissions for bucket: %w", err)
	}

	return grantedPermissions, nil
}

// CheckIAMPermissionsOnSecret checks if the required IAM permissions are
// present for the caller's identity (service account, etc) on a given secret.
//
// Returns the list of input permissions that are granted.
func (i *IAM) CheckIAMPermissionsOnSecret(ctx context.Context, projectID, secretID string, permissions []string) (grantedPermissions []string, err error) {
	if i.smClient == nil {
		return nil, fmt.Errorf("smClient is not initialized")
	}
	if permissions == nil {
		return nil, nil
	}
	req := &iampb.TestIamPermissionsRequest{
		Resource:    fmt.Sprintf("projects/%s/secrets/%s", projectID, secretID),
		Permissions: permissions,
	}
	resp, err := i.smClient.TestIamPermissions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to test IAM permissions for secret: %w", err)
	}
	return resp.GetPermissions(), nil
}

// CheckIAMPermissionsOnInstance checks if the required IAM permissions are
// present for the caller's identity (service account, etc) on a given instance.
//
// Returns the list of input permissions that are granted.
func (i *IAM) CheckIAMPermissionsOnInstance(ctx context.Context, project, zone, instanceName string, permissions []string) (grantedPermissions []string, err error) {
	if i.instancesService == nil {
		return nil, fmt.Errorf("instancesService is not initialized")
	}
	if permissions == nil {
		return nil, nil
	}
	req := &compute.TestPermissionsRequest{
		Permissions: permissions,
	}

	resp, err := i.instancesService.TestIamPermissions(project, zone, instanceName, req).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to test IAM permissions for instance: %w", err)
	}
	return resp.Permissions, nil
}
