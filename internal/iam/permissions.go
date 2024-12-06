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

// Package permissions provides functions to check IAM permissions for sapagent
// services.
package permissions

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/go-yaml/yaml"
)

//go:embed iam-permissions.yaml
var iamPermissionsYAML []byte

type yamlFeatures struct {
	Features []servicePermissions `yaml:"features"`
}

// ServicePermissions is a struct to hold the permissions for a service.
type servicePermissions struct {
	Name            string              `yaml:"name"`
	PermissionsList []EntityPermissions `yaml:"permissionsList"`
}

// EntityPermissions is a struct to hold the permissions for an entity.
type EntityPermissions struct {
	Type        string   `yaml:"type"`
	Permissions []string `yaml:"permissions"`
}

// ResourceDetails is a struct to hold the details of the resources
// (project/disk etc) on which the permissions are checked.
type ResourceDetails struct {
	ProjectID    string
	Zone         string
	BucketName   string
	DiskName     string
	InstanceName string
	SecretName   string
}

// IAMService is an interface for an IAM service.
type IAMService interface {
	CheckIAMPermissionsOnProject(ctx context.Context, projectID string, permissions []string) ([]string, error)
	CheckIAMPermissionsOnBucket(ctx context.Context, bucketName string, permissions []string) ([]string, error)
	CheckIAMPermissionsOnDisk(ctx context.Context, projectID, zone, diskName string, permissions []string) ([]string, error)
	CheckIAMPermissionsOnInstance(ctx context.Context, projectID, zone, instanceName string, permissions []string) ([]string, error)
	CheckIAMPermissionsOnSecret(ctx context.Context, projectID, secretName string, permissions []string) ([]string, error)
}

// permissionsMap stores the permissions for each feature.
var permissionsMap map[string][]EntityPermissions

func init() {
	// Parse the YAML data at init time
	// TODO : Look into if the number of conversions
	// can be reduced by using JSON instead of YAML.
	var permissionsData yamlFeatures
	err := yaml.Unmarshal(iamPermissionsYAML, &permissionsData)
	if err != nil {
		panic(fmt.Errorf("failed to unmarshal YAML: %v", err))
	}

	// Create the permissionsMap
	permissionsMap = make(map[string][]EntityPermissions)
	for _, service := range permissionsData.Features {
		permissionsMap[service.Name] = service.PermissionsList
	}
}

// GetServicePermissionsStatus checks if the required IAM permissions for a
// service/functionality are granted on the specified resource, and returns
// a map of permissions to granted/not granted. Assumes that the permissions
// are unique across all resource types for a service.
func GetServicePermissionsStatus(ctx context.Context, iamService IAMService, serviceName string, r *ResourceDetails) (map[string]bool, error) {
	// Get the permissions for the service from the permissionsMap
	permissionsList, ok := permissionsMap[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	allPermissions := fetchPermissionsMap(permissionsList)
	// Check permissions for each resource type in the permissionsList
	var allGrantedPermissions []string
	for _, permList := range permissionsList {
		// Call the appropriate IAM check function based on resource type
		var grantedPermissions []string
		var err error
		switch permList.Type {
		case "Project":
			if r.ProjectID == "" {
				return nil, fmt.Errorf("missing ProjectID in entityDetails")
			}
			grantedPermissions, err = iamService.CheckIAMPermissionsOnProject(ctx, r.ProjectID, permList.Permissions)
		case "Bucket":
			if r.BucketName == "" {
				return nil, fmt.Errorf("missing BucketName in entityDetails")
			}
			grantedPermissions, err = iamService.CheckIAMPermissionsOnBucket(ctx, r.BucketName, permList.Permissions)
		case "Disk":
			if r.ProjectID == "" || r.Zone == "" || r.DiskName == "" {
				return nil, fmt.Errorf("missing ProjectID, Zone, or DiskName in entityDetails")
			}
			grantedPermissions, err = iamService.CheckIAMPermissionsOnDisk(ctx, r.ProjectID, r.Zone, r.DiskName, permList.Permissions)
		case "Instance":
			if r.ProjectID == "" || r.Zone == "" || r.InstanceName == "" {
				return nil, fmt.Errorf("missing ProjectID, Zone, or InstanceName in entityDetails")
			}
			grantedPermissions, err = iamService.CheckIAMPermissionsOnInstance(ctx, r.ProjectID, r.Zone, r.InstanceName, permList.Permissions)
		case "Secret":
			if r.ProjectID == "" || r.SecretName == "" {
				return nil, fmt.Errorf("missing ProjectID or SecretName in entityDetails")
			}
			grantedPermissions, err = iamService.CheckIAMPermissionsOnSecret(ctx, r.ProjectID, r.SecretName, permList.Permissions)
		default:
			return nil, fmt.Errorf("unsupported resource type: %s", permList.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to check permissions for service %s on entity %s: %v", serviceName, permList.Type, err)
		}

		allGrantedPermissions = append(allGrantedPermissions, grantedPermissions...)
	}

	// Sets the permissions map to true for all granted permissions.
	// Assumes that the permissions are unique across all resource types.
	for _, perm := range allGrantedPermissions {
		allPermissions[perm] = true
	}

	return allPermissions, nil
}

// fetchPermissionsMap creates a map of permissions defaulted to false (i.e.
// not granted).
func fetchPermissionsMap(permissionsList []EntityPermissions) map[string]bool {
	allPermissions := make(map[string]bool)
	for _, permList := range permissionsList {
		for _, perm := range permList.Permissions {
			allPermissions[perm] = false
		}
	}

	return allPermissions
}
