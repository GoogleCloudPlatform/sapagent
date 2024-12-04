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

package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
)

// Fake implementation of SecretManager interface
type fakeSecretManager struct {
	fakeTestPermissions *iampb.TestIamPermissionsResponse
	fakeError           error
}

func (f *fakeSecretManager) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest, opts ...gax.CallOption) (*iampb.TestIamPermissionsResponse, error) {
	return f.fakeTestPermissions, f.fakeError
}

// resourceType represents the type of resource being tested.
type resourceType int

const (
	resourceTypeProject resourceType = iota
	resourceTypeBucket
	resourceTypeInstance
	resourceTypeDisk
)

// createTestServer creates an HTTP test server with the specified response,
// status code, and the resource type
func createTestServer(t *testing.T, serverResp any, serverCode int, resourceType resourceType) (*httptest.Server, *[]string, *string) {
	t.Helper()
	var gotReqPerms []string
	var gotPath string
	var req any
	switch resourceType {
	case resourceTypeProject:
		req = &cloudresourcemanager.TestIamPermissionsRequest{}
	case resourceTypeInstance, resourceTypeDisk:
		req = &compute.TestPermissionsRequest{}
	case resourceTypeBucket:
		req = &iampb.TestIamPermissionsRequest{}
	default:
		t.Fatalf("Unsupported resource type: %v", resourceType)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resourceType == resourceTypeBucket {
			// For bucket tests, the request is in the query parameters
			gotPath = r.URL.Path
			gotReqPerms = r.URL.Query()["permissions"] // Extract permissions from query parameters
		} else {
			content, err := io.ReadAll(r.Body)
			gotPath = r.URL.Path
			r.Body.Close()
			if err != nil {
				t.Fatalf("Reading body: %v", err)
			}
			if err := json.Unmarshal(content, &req); err != nil {
				t.Fatalf("Unmarshalling request: %v", err)
			}
			switch req.(type) {
			case *cloudresourcemanager.TestIamPermissionsRequest:
				gotReqPerms = req.(*cloudresourcemanager.TestIamPermissionsRequest).Permissions
			case *compute.TestPermissionsRequest:
				gotReqPerms = req.(*compute.TestPermissionsRequest).Permissions
			case *iampb.TestIamPermissionsRequest:
				gotReqPerms = req.(*iampb.TestIamPermissionsRequest).Permissions
			default:
				t.Fatalf("Unsupported request type: %v", req)
			}
		}

		code := http.StatusOK
		if serverCode > 0 {
			code = serverCode
		}
		w.WriteHeader(code)
		w.Header().Set("Content-Type", "application/json")

		respContent, err := json.Marshal(serverResp)
		if err != nil {
			t.Fatalf("Error marshalling response: %v", err)
		}
		w.Write(respContent)
	}))

	return server, &gotReqPerms, &gotPath
}

func TestCheckIAMPermissionsOnSecret(t *testing.T) {
	ctx := context.Background()
	projectID := "test-project"
	secretID := "test-secret"

	tests := []struct {
		name                string
		permissions         []string
		fakeTestPermissions *iampb.TestIamPermissionsResponse
		fakeError           error
		wantGranted         []string
		wantErr             error
	}{
		{
			name:        "Success",
			permissions: []string{"secretmanager.secrets.get", "secretmanager.secrets.access"},
			fakeTestPermissions: &iampb.TestIamPermissionsResponse{
				Permissions: []string{"secretmanager.secrets.get"},
			},
			wantGranted: []string{"secretmanager.secrets.get"},
		},
		{
			name:        "Error",
			permissions: []string{"secretmanager.secrets.get", "secretmanager.secrets.access"},
			fakeError:   fmt.Errorf("test error"),
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeSMClient := &fakeSecretManager{
				fakeTestPermissions: test.fakeTestPermissions,
				fakeError:           test.fakeError,
			}

			iamClient := &IAM{
				smClient: fakeSMClient,
			}

			gotGranted, err := iamClient.CheckIAMPermissionsOnSecret(ctx, projectID, secretID, test.permissions)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("CheckIAMPermissionsOnSecret(%s) returned err: %v, wantErr: %v", test.name, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantGranted, gotGranted); diff != "" {
				t.Errorf("CheckIAMPermissionsOnSecret(%s) returned unexpected granted permissions diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestCheckIAMPermissionsOnProject(t *testing.T) {
	cases := []struct {
		name       string
		resource   string
		perms      []string
		serverResp *cloudresourcemanager.TestIamPermissionsResponse
		serverCode int
		wantPath   string
		wantReq    *cloudresourcemanager.TestIamPermissionsRequest
		want       []string
		wantErr    error
	}{
		{
			name:     "nil_list",
			resource: "testproject",
		},
		{
			name:     "AllPermissionsGranted",
			resource: "testproject",
			perms:    []string{"compute.instances.list"},
			serverResp: &cloudresourcemanager.TestIamPermissionsResponse{
				Permissions: []string{"compute.instances.list"},
			},
			wantPath: "/v1/projects/testproject:testIamPermissions",
			wantReq: &cloudresourcemanager.TestIamPermissionsRequest{
				Permissions: []string{"compute.instances.list"},
			},
			want: []string{"compute.instances.list"},
		},
		{
			name:     "SomePermissionsNotGranted",
			resource: "testproject",
			perms:    []string{"compute.instances.list", "iam.serviceAccounts.actAs"},
			serverResp: &cloudresourcemanager.TestIamPermissionsResponse{
				Permissions: []string{"compute.instances.list"},
			},
			wantPath: "/v1/projects/testproject:testIamPermissions",
			wantReq: &cloudresourcemanager.TestIamPermissionsRequest{
				Permissions: []string{"compute.instances.list", "iam.serviceAccounts.actAs"},
			},
			want: []string{"compute.instances.list"},
		},
		{
			name:       "ServerError",
			resource:   "testproject",
			perms:      []string{"compute.instances.list"},
			wantPath:   "/v1/projects/testproject:testIamPermissions",
			serverResp: nil,
			serverCode: http.StatusInternalServerError,
			want:       nil,
			wantReq: &cloudresourcemanager.TestIamPermissionsRequest{
				Permissions: []string{"compute.instances.list"},
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			// Use createTestServer with resourceTypeProject
			server, reqPerms, reqPath := createTestServer(t, tc.serverResp, tc.serverCode, resourceTypeProject)
			defer server.Close()

			iamClient, err := NewIAMClient(ctx, option.WithEndpoint(server.URL), option.WithoutAuthentication())
			if err != nil {
				t.Fatalf("NewIAMClient() failed: %v", err)
			}

			got, err := iamClient.CheckIAMPermissionsOnProject(ctx, tc.resource, tc.perms)
			gotPath := *reqPath
			gotPerms := *reqPerms

			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("CheckIAMPermissionsOnProject(%s) returned err: %v, wantErr: %v", tc.name, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Permissions mismatch (-want +got):\n%s", diff)
			}
			if len(tc.perms) > 0 {
				if gotPath != tc.wantPath {
					t.Errorf("Request resource mismatch got=%q want=%q", gotPath, tc.wantPath)
				}
				if diff := cmp.Diff(tc.wantReq.Permissions, gotPerms, protocmp.Transform()); diff != "" {
					t.Errorf("Request diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestCheckIAMPermissionsOnDisk(t *testing.T) {
	cases := []struct {
		name       string
		project    string
		zone       string
		diskName   string
		perms      []string
		serverResp *compute.TestPermissionsResponse
		serverCode int
		wantPath   string
		wantReq    *compute.TestPermissionsRequest
		want       []string
		wantErr    error
	}{
		{
			name:     "nil_list",
			project:  "testproject",
			zone:     "testzone",
			diskName: "testdisk",
		},
		{
			name:     "AllPermissionsGranted",
			project:  "testproject",
			zone:     "testzone",
			diskName: "testdisk",
			perms:    []string{"compute.disks.create"},
			serverResp: &compute.TestPermissionsResponse{
				Permissions: []string{"compute.disks.create"},
			},
			wantPath: "/projects/testproject/zones/testzone/disks/testdisk/testIamPermissions",
			wantReq: &compute.TestPermissionsRequest{
				Permissions: []string{"compute.disks.create"},
			},
			want: []string{"compute.disks.create"},
		},
		{
			name:     "SomePermissionsNotGranted",
			project:  "testproject",
			zone:     "testzone",
			diskName: "testdisk",
			perms:    []string{"compute.disks.create", "compute.disks.use"},
			serverResp: &compute.TestPermissionsResponse{
				Permissions: []string{"compute.disks.create"},
			},
			wantPath: "/projects/testproject/zones/testzone/disks/testdisk/testIamPermissions",
			wantReq: &compute.TestPermissionsRequest{
				Permissions: []string{"compute.disks.create", "compute.disks.use"},
			},
			want: []string{"compute.disks.create"},
		},
		{
			name:       "ServerError",
			project:    "testproject",
			zone:       "testzone",
			diskName:   "testdisk",
			perms:      []string{"compute.disks.create"},
			wantPath:   "/projects/testproject/zones/testzone/disks/testdisk/testIamPermissions",
			serverResp: nil,
			serverCode: http.StatusInternalServerError,
			want:       nil,
			wantReq: &compute.TestPermissionsRequest{
				Permissions: []string{"compute.disks.create"},
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			server, reqPerms, reqPath := createTestServer(t, tc.serverResp, tc.serverCode, resourceTypeDisk)
			defer server.Close()

			iamClient, err := NewIAMClient(ctx, option.WithEndpoint(server.URL), option.WithoutAuthentication())
			if err != nil {
				t.Fatalf("NewIAMClient() failed: %v", err)
			}

			got, err := iamClient.CheckIAMPermissionsOnDisk(ctx, tc.project, tc.zone, tc.diskName, tc.perms)
			gotPath := *reqPath
			gotPerms := *reqPerms

			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("CheckIAMPermissionsOnDisk(%s) returned err: %v, wantErr: %v", tc.name, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Permissions mismatch (-want +got):\n%s", diff)
			}
			if len(tc.perms) > 0 {
				if got, want := gotPath, tc.wantPath; got != want {
					t.Errorf("Request resource mismatch got=%q want=%q", got, want)
				}
				if diff := cmp.Diff(tc.wantReq.Permissions, gotPerms, protocmp.Transform()); diff != "" {
					t.Errorf("Request diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestCheckIAMPermissionsOnInstance(t *testing.T) {
	cases := []struct {
		name       string
		project    string
		zone       string
		instance   string
		perms      []string
		serverResp *compute.TestPermissionsResponse
		serverCode int
		wantPath   string
		wantReq    *compute.TestPermissionsRequest
		want       []string
		wantErr    error
	}{
		{
			name:     "nil_list",
			project:  "testproject",
			zone:     "testzone",
			instance: "testinstance",
		},
		{
			name:     "AllPermissionsGranted",
			project:  "testproject",
			zone:     "testzone",
			instance: "testinstance",
			perms:    []string{"compute.instances.get"},
			serverResp: &compute.TestPermissionsResponse{
				Permissions: []string{"compute.instances.get"},
			},
			wantPath: "/projects/testproject/zones/testzone/instances/testinstance/testIamPermissions",
			wantReq: &compute.TestPermissionsRequest{
				Permissions: []string{"compute.instances.get"},
			},
			want: []string{"compute.instances.get"},
		},
		{
			name:     "SomePermissionsNotGranted",
			project:  "testproject",
			zone:     "testzone",
			instance: "testinstance",
			perms:    []string{"compute.instances.get", "compute.instances.stop"},
			serverResp: &compute.TestPermissionsResponse{
				Permissions: []string{"compute.instances.get"},
			},
			wantPath: "/projects/testproject/zones/testzone/instances/testinstance/testIamPermissions",
			wantReq: &compute.TestPermissionsRequest{
				Permissions: []string{"compute.instances.get", "compute.instances.stop"},
			},
			want: []string{"compute.instances.get"},
		},
		{
			name:       "ServerError",
			project:    "testproject",
			zone:       "testzone",
			instance:   "testinstance",
			perms:      []string{"compute.instances.get"},
			wantPath:   "/projects/testproject/zones/testzone/instances/testinstance/testIamPermissions",
			serverResp: nil,
			serverCode: http.StatusInternalServerError,
			want:       nil,
			wantReq: &compute.TestPermissionsRequest{
				Permissions: []string{"compute.instances.get"},
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			server, reqPerms, reqPath := createTestServer(t, tc.serverResp, tc.serverCode, resourceTypeInstance)
			defer server.Close()

			iamClient, err := NewIAMClient(ctx, option.WithEndpoint(server.URL), option.WithoutAuthentication())
			if err != nil {
				t.Fatalf("NewIAMClient() failed: %v", err)
			}

			got, err := iamClient.CheckIAMPermissionsOnInstance(ctx, tc.project, tc.zone, tc.instance, tc.perms)
			gotPath := *reqPath
			gotPerms := *reqPerms

			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("CheckIAMPermissionsOnInstance(%s) returned err: %v, wantErr: %v", tc.name, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Permissions mismatch (-want +got):\n%s", diff)
			}
			if len(tc.perms) > 0 {
				if got, want := gotPath, tc.wantPath; got != want {
					t.Errorf("Request resource mismatch got=%q want=%q", got, want)
				}
				if diff := cmp.Diff(tc.wantReq.Permissions, gotPerms, protocmp.Transform()); diff != "" {
					t.Errorf("Request diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestCheckIAMPermissionsOnBucket(t *testing.T) {
	cases := []struct {
		name       string
		bucketName string
		perms      []string
		serverResp *iampb.TestIamPermissionsResponse
		serverCode int
		wantPath   string
		want       []string
		wantErr    error
	}{
		{
			name:       "nil_list",
			bucketName: "test-bucket",
		},
		{
			name:       "AllPermissionsGranted",
			bucketName: "test-bucket",
			perms:      []string{"storage.buckets.get, storage.buckets.update"},
			serverResp: &iampb.TestIamPermissionsResponse{Permissions: []string{"storage.buckets.get, storage.buckets.update"}},
			wantPath:   "/b/test-bucket/iam/testPermissions",
			want:       []string{"storage.buckets.get, storage.buckets.update"},
		},
		{
			name:       "SomePermissionsNotGranted",
			bucketName: "test-bucket",
			perms:      []string{"storage.buckets.get", "storage.objects.list"},
			serverResp: &iampb.TestIamPermissionsResponse{Permissions: []string{"storage.buckets.get"}},
			wantPath:   "/b/test-bucket/iam/testPermissions",
			want:       []string{"storage.buckets.get"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// t.Parallel()
			ctx := context.Background()

			server, reqPerms, reqPath := createTestServer(t, tc.serverResp, tc.serverCode, resourceTypeBucket)
			defer server.Close()

			iamClient, err := NewIAMClient(ctx, option.WithEndpoint(server.URL), option.WithoutAuthentication())
			if err != nil {
				t.Fatalf("NewIAMClient() failed: %v", err)
			}

			got, err := iamClient.CheckIAMPermissionsOnBucket(ctx, tc.bucketName, tc.perms)
			gotPath := *reqPath
			gotPerms := *reqPerms

			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("CheckIAMPermissionsOnBucket(%s) returned err: %v, wantErr: %v", tc.name, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Permissions mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.perms, gotPerms); diff != "" {
				t.Errorf("Permissions mismatch (-want +got):\n%s", diff)
			}
			if len(tc.perms) > 0 {
				if got, want := gotPath, tc.wantPath; got != want {
					t.Errorf("Request resource mismatch got=%q want=%q", got, want)
				}
			}
		})
	}
}
