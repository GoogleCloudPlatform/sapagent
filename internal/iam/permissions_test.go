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

package permissions

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type fakeIAMService struct {
	projectPermissions    map[string][]string
	bucketPermissions     map[string][]string
	diskPermissions       map[string][]string
	instancePermissions   map[string][]string
	secretPermissions     map[string][]string
	projectPermissionErr  error
	bucketPermissionErr   error
	diskPermissionErr     error
	instancePermissionErr error
	secretPermissionErr   error
}

func (f *fakeIAMService) CheckIAMPermissionsOnProject(ctx context.Context, projectID string, permissions []string) ([]string, error) {
	if f.projectPermissionErr != nil {
		return nil, f.projectPermissionErr
	}
	return f.projectPermissions[projectID], nil
}

func (f *fakeIAMService) CheckIAMPermissionsOnBucket(ctx context.Context, bucketName string, permissions []string) ([]string, error) {
	if f.bucketPermissionErr != nil {
		return nil, f.bucketPermissionErr
	}
	return f.bucketPermissions[bucketName], nil
}

func (f *fakeIAMService) CheckIAMPermissionsOnDisk(ctx context.Context, projectID, zone, diskName string, permissions []string) ([]string, error) {
	if f.diskPermissionErr != nil {
		return nil, f.diskPermissionErr
	}
	key := fmt.Sprintf("%s/%s/%s", projectID, zone, diskName)
	return f.diskPermissions[key], nil
}

func (f *fakeIAMService) CheckIAMPermissionsOnInstance(ctx context.Context, projectID, zone, instanceName string, permissions []string) ([]string, error) {
	if f.instancePermissionErr != nil {
		return nil, f.instancePermissionErr
	}
	key := fmt.Sprintf("%s/%s/%s", projectID, zone, instanceName)
	return f.instancePermissions[key], nil
}

func (f *fakeIAMService) CheckIAMPermissionsOnSecret(ctx context.Context, projectID, secretName string, permissions []string) ([]string, error) {
	if f.secretPermissionErr != nil {
		return nil, f.secretPermissionErr
	}
	key := fmt.Sprintf("%s/%s", projectID, secretName)
	return f.secretPermissions[key], nil
}

func TestGetServicePermissionsStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name            string
		serviceName     string
		resourceDetails *ResourceDetails
		iamService      *fakeIAMService
		wantPermissions map[string]bool
		wantErr         error
	}{
		{
			name:        "serviceNotFound",
			serviceName: "invalid-service",
			wantErr:     cmpopts.AnyError,
		},
		{
			name:        "missingProjectID",
			serviceName: "HOST_METRICS",
			resourceDetails: &ResourceDetails{
				Zone:       "us-central1-a",
				BucketName: "test-bucket",
				DiskName:   "test-disk",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "missingBucketName",
			serviceName: "BACKINT",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
				Zone:      "us-central1-a",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.nodeGroups.list", "compute.nodeGroups.get", "compute.instances.get", "monitoring.timeSeries.create"},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "missingProjectIDZoneOrDiskName",
			serviceName: "DISKBACKUP",
			resourceDetails: &ResourceDetails{
				Zone:       "us-central1-a",
				BucketName: "test-bucket",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "missingProjectIDZoneOrInstanceName",
			serviceName: "DISKBACKUP",
			resourceDetails: &ResourceDetails{
				Zone:       "us-central1-a",
				BucketName: "test-bucket",
				DiskName:   "test-disk",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "missingProjectIDOrSecretName",
			serviceName: "SECRET_MANAGER",
			resourceDetails: &ResourceDetails{
				Zone: "us-central1-a",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "checkPermissionsFailed",
			serviceName: "HOST_METRICS",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				BucketName:   "test-bucket",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissionErr: fmt.Errorf("failed to check permissions"),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "hanaMonitoring_allPermissionsGranted",
			serviceName: "HANA_MONITORING",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"monitoring.timeSeries.create"},
				},
			},
			wantPermissions: map[string]bool{
				"monitoring.timeSeries.create": true,
			},
		},
		{
			name:        "hanaMonitoring_somePermissionsNotGranted",
			serviceName: "HANA_MONITORING",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{},
				},
			},
			wantPermissions: map[string]bool{
				"monitoring.timeSeries.create": false,
			},
		},
		{
			name:        "processMetrics_allPermissionsGranted",
			serviceName: "PROCESS_METRICS",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.nodeGroups.list", "compute.nodeGroups.get", "compute.instances.get", "monitoring.timeSeries.create"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.nodeGroups.list":      true,
				"compute.nodeGroups.get":       true,
				"compute.instances.get":        true,
				"monitoring.timeSeries.create": true,
			},
		},
		{
			name:        "processMetrics_somePermissionsNotGranted",
			serviceName: "PROCESS_METRICS",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.nodeGroups.list", "compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.nodeGroups.list":      true,
				"compute.nodeGroups.get":       false,
				"compute.instances.get":        true,
				"monitoring.timeSeries.create": false,
			},
		},
		{
			name:        "cloudLogging_allPermissionsGranted",
			serviceName: "CLOUD_LOGGING",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"logging.logEntries.create"},
				},
			},
			wantPermissions: map[string]bool{
				"logging.logEntries.create": true,
			},
		},
		{
			name:        "cloudLogging_somePermissionsNotGranted",
			serviceName: "CLOUD_LOGGING",
			resourceDetails: &ResourceDetails{
				ProjectID: "test-project",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{},
				},
			},
			wantPermissions: map[string]bool{
				"logging.logEntries.create": false,
			},
		},
		{
			name:        "hostMetrics_allPermissionsGranted",
			serviceName: "HOST_METRICS",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				BucketName:   "test-bucket",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.instances.list", "monitoring.metricDescriptors.get", "monitoring.metricDescriptors.list"},
				},
				instancePermissions: map[string][]string{
					"test-project/us-central1-a/test-instance": []string{"compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.instances.list":            true,
				"compute.instances.get":             true,
				"monitoring.metricDescriptors.get":  true,
				"monitoring.metricDescriptors.list": true,
			},
		},
		{
			name:        "hostMetrics_somePermissionsNotGranted",
			serviceName: "HOST_METRICS",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				BucketName:   "test-bucket",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.instances.list"},
				},
				instancePermissions: map[string][]string{
					"test-project/us-central1-a/test-instance": []string{"compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.instances.list":            true,
				"monitoring.metricDescriptors.get":  false,
				"monitoring.metricDescriptors.list": false,
				"compute.instances.get":             true,
			},
		},
		{
			name:        "backint_allPermissionsGranted",
			serviceName: "BACKINT",
			resourceDetails: &ResourceDetails{
				ProjectID:  "test-project",
				BucketName: "test-bucket",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"storage.objects.list", "storage.objects.create"},
				},
				bucketPermissions: map[string][]string{
					"test-bucket": []string{"storage.objects.get", "storage.objects.update", "storage.objects.delete"},
				},
			},
			wantPermissions: map[string]bool{
				"storage.objects.list":   true,
				"storage.objects.create": true,
				"storage.objects.get":    true,
				"storage.objects.update": true,
				"storage.objects.delete": true,
			},
		},
		{
			name:        "backint_somePermissionsNotGranted",
			serviceName: "BACKINT",
			resourceDetails: &ResourceDetails{
				ProjectID:  "test-project",
				BucketName: "test-bucket",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"storage.objects.list"},
				},
				bucketPermissions: map[string][]string{
					"test-bucket": []string{"storage.objects.get", "storage.objects.update"},
				},
			},
			wantPermissions: map[string]bool{
				"storage.objects.list":   true,
				"storage.objects.create": false,
				"storage.objects.get":    true,
				"storage.objects.update": true,
				"storage.objects.delete": false,
			},
		},
		{
			name:        "backintMultipart_allPermissionsGranted",
			serviceName: "BACKINT_MULTIPART",
			resourceDetails: &ResourceDetails{
				BucketName: "test-bucket",
			},
			iamService: &fakeIAMService{
				bucketPermissions: map[string][]string{
					"test-bucket": []string{"storage.multipartUploads.create", "storage.multipartUploads.abort"},
				},
			},
			wantPermissions: map[string]bool{
				"storage.multipartUploads.create": true,
				"storage.multipartUploads.abort":  true,
			},
		},
		{
			name:        "backintMultipart_somePermissionsNotGranted",
			serviceName: "BACKINT_MULTIPART",
			resourceDetails: &ResourceDetails{
				BucketName: "test-bucket",
			},
			iamService: &fakeIAMService{
				bucketPermissions: map[string][]string{
					"test-bucket": []string{"storage.multipartUploads.create"},
				},
			},
			wantPermissions: map[string]bool{
				"storage.multipartUploads.create": true,
				"storage.multipartUploads.abort":  false,
			},
		},
		{
			name:        "diskbackup_allPermissionsGranted",
			serviceName: "DISKBACKUP",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.disks.create", "compute.disks.createSnapshot", "compute.disks.get", "compute.disks.setLabels", "compute.disks.use", "compute.globalOperations.get", "compute.instances.attachDisk", "compute.instances.detachDisk", "compute.instances.get", "compute.snapshots.create", "compute.snapshots.get", "compute.snapshots.setLabels", "compute.snapshots.useReadOnly", "compute.zoneOperations.get"},
				},
				diskPermissions: map[string][]string{
					"test-project/us-central1-a/test-disk": []string{"compute.disks.get"},
				},
				instancePermissions: map[string][]string{
					"test-project/us-central1-a/test-instance": []string{"compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.disks.create":          true,
				"compute.disks.createSnapshot":  true,
				"compute.disks.get":             true,
				"compute.disks.setLabels":       true,
				"compute.disks.use":             true,
				"compute.globalOperations.get":  true,
				"compute.instances.attachDisk":  true,
				"compute.instances.detachDisk":  true,
				"compute.instances.get":         true,
				"compute.snapshots.create":      true,
				"compute.snapshots.get":         true,
				"compute.snapshots.setLabels":   true,
				"compute.snapshots.useReadOnly": true,
				"compute.zoneOperations.get":    true,
			},
		},
		{
			name:        "diskbackup_somePermissionsNotGranted",
			serviceName: "DISKBACKUP",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.disks.create", "compute.disks.createSnapshot", "compute.disks.get", "compute.disks.setLabels", "compute.disks.use", "compute.globalOperations.get", "compute.instances.attachDisk", "compute.instances.detachDisk", "compute.instances.get", "compute.snapshots.create", "compute.snapshots.get", "compute.snapshots.setLabels", "compute.snapshots.useReadOnly", "compute.zoneOperations.get"},
				},
				diskPermissions: map[string][]string{
					"test-project/us-central1-a/test-disk": []string{},
				},
				instancePermissions: map[string][]string{
					"test-project/us-central1-a/test-instance": []string{"compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.disks.create":          true,
				"compute.disks.createSnapshot":  true,
				"compute.disks.get":             true,
				"compute.disks.setLabels":       true,
				"compute.disks.use":             true,
				"compute.globalOperations.get":  true,
				"compute.instances.attachDisk":  true,
				"compute.instances.detachDisk":  true,
				"compute.instances.get":         true,
				"compute.snapshots.create":      true,
				"compute.snapshots.get":         true,
				"compute.snapshots.setLabels":   true,
				"compute.snapshots.useReadOnly": true,
				"compute.zoneOperations.get":    true,
			},
		},
		{
			name:        "diskbackupStriped_allPermissionsGranted",
			serviceName: "DISKBACKUP_STRIPED",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.disks.addResourcePolicies", "compute.disks.create", "compute.disks.get", "compute.disks.list", "compute.disks.removeResourcePolicies", "compute.disks.use", "compute.disks.useReadOnly", "compute.globalOperations.get", "compute.instances.attachDisk", "compute.instances.detachDisk", "compute.instances.get", "compute.instantSnapshotGroups.create", "compute.instantSnapshotGroups.delete", "compute.instantSnapshotGroups.get", "compute.instantSnapshotGroups.list", "compute.instantSnapshots.list", "compute.instantSnapshots.useReadOnly", "compute.resourcePolicies.create", "compute.resourcePolicies.use", "compute.resourcePolicies.useReadOnly", "compute.snapshots.create", "compute.snapshots.get", "compute.snapshots.list", "compute.snapshots.setLabels", "compute.snapshots.useReadOnly", "compute.zoneOperations.get"},
				},
				diskPermissions: map[string][]string{
					"test-project/us-central1-a/test-disk": []string{"compute.disks.get"},
				},
				instancePermissions: map[string][]string{
					"test-project/us-central1-a/test-instance": []string{"compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.disks.addResourcePolicies":    true,
				"compute.disks.create":                 true,
				"compute.disks.get":                    true,
				"compute.disks.list":                   true,
				"compute.disks.removeResourcePolicies": true,
				"compute.disks.use":                    true,
				"compute.disks.useReadOnly":            true,
				"compute.globalOperations.get":         true,
				"compute.instances.attachDisk":         true,
				"compute.instances.detachDisk":         true,
				"compute.instances.get":                true,
				"compute.instantSnapshotGroups.create": true,
				"compute.instantSnapshotGroups.delete": true,
				"compute.instantSnapshotGroups.get":    true,
				"compute.instantSnapshotGroups.list":   true,
				"compute.instantSnapshots.list":        true,
				"compute.instantSnapshots.useReadOnly": true,
				"compute.resourcePolicies.create":      true,
				"compute.resourcePolicies.use":         true,
				"compute.resourcePolicies.useReadOnly": true,
				"compute.snapshots.create":             true,
				"compute.snapshots.get":                true,
				"compute.snapshots.list":               true,
				"compute.snapshots.setLabels":          true,
				"compute.snapshots.useReadOnly":        true,
				"compute.zoneOperations.get":           true,
			},
		},
		{
			name:        "diskbackupStriped_somePermissionsNotGranted",
			serviceName: "DISKBACKUP_STRIPED",
			resourceDetails: &ResourceDetails{
				ProjectID:    "test-project",
				Zone:         "us-central1-a",
				DiskName:     "test-disk",
				InstanceName: "test-instance",
			},
			iamService: &fakeIAMService{
				projectPermissions: map[string][]string{
					"test-project": []string{"compute.disks.addResourcePolicies", "compute.disks.create", "compute.disks.get", "compute.disks.list", "compute.disks.removeResourcePolicies", "compute.disks.use", "compute.disks.useReadOnly", "compute.globalOperations.get", "compute.instances.attachDisk", "compute.instances.detachDisk", "compute.instances.get", "compute.instantSnapshotGroups.create", "compute.instantSnapshotGroups.delete", "compute.instantSnapshotGroups.get", "compute.instantSnapshotGroups.list", "compute.instantSnapshots.list", "compute.instantSnapshots.useReadOnly", "compute.resourcePolicies.create", "compute.resourcePolicies.use", "compute.resourcePolicies.useReadOnly", "compute.snapshots.create", "compute.snapshots.get", "compute.snapshots.list", "compute.snapshots.setLabels", "compute.snapshots.useReadOnly", "compute.zoneOperations.get"},
				},
				diskPermissions: map[string][]string{
					"test-project/us-central1-a/test-disk": []string{},
				},
				instancePermissions: map[string][]string{
					"test-project/us-central1-a/test-instance": []string{"compute.instances.get"},
				},
			},
			wantPermissions: map[string]bool{
				"compute.disks.addResourcePolicies":    true,
				"compute.disks.create":                 true,
				"compute.disks.get":                    true,
				"compute.disks.list":                   true,
				"compute.disks.removeResourcePolicies": true,
				"compute.disks.use":                    true,
				"compute.disks.useReadOnly":            true,
				"compute.globalOperations.get":         true,
				"compute.instances.attachDisk":         true,
				"compute.instances.detachDisk":         true,
				"compute.instances.get":                true,
				"compute.instantSnapshotGroups.create": true,
				"compute.instantSnapshotGroups.delete": true,
				"compute.instantSnapshotGroups.get":    true,
				"compute.instantSnapshotGroups.list":   true,
				"compute.instantSnapshots.list":        true,
				"compute.instantSnapshots.useReadOnly": true,
				"compute.resourcePolicies.create":      true,
				"compute.resourcePolicies.use":         true,
				"compute.resourcePolicies.useReadOnly": true,
				"compute.snapshots.create":             true,
				"compute.snapshots.get":                true,
				"compute.snapshots.list":               true,
				"compute.snapshots.setLabels":          true,
				"compute.snapshots.useReadOnly":        true,
				"compute.zoneOperations.get":           true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotPermissions, gotErr := GetServicePermissionsStatus(ctx, tc.iamService, tc.serviceName, tc.resourceDetails)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("GetServicePermissionsStatus(%v, %v, %v) got error: %v, want error: %v", tc.iamService, tc.serviceName, tc.resourceDetails, gotErr, tc.wantErr)
			}
			if tc.wantErr == nil {
				if diff := cmp.Diff(tc.wantPermissions, gotPermissions); diff != "" {
					t.Errorf("GetServicePermissionsStatus(%v, %v, %v) returned unexpected diff (-want +got):\n%s", tc.iamService, tc.serviceName, tc.resourceDetails, diff)
				}
			}
		})
	}
}
