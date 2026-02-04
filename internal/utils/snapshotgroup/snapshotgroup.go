/*
Copyright 2025 Google LLC

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

// Package snapshotgroup is used to provide snapshot group related functions.
package snapshotgroup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/cenkalti/backoff/v4"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/rest"
)

const defaultMaxRetries = 8

type (
	// SGService is a struct that provides operations for managing snapshot groups.
	// It currently serves as a placeholder and will be updated with a more complete implementation.
	SGService struct {
		rest       *rest.Rest
		baseURL    string
		backoff    *backoff.ExponentialBackOff
		maxRetries int
	}

	errorResponse struct {
		Err googleapi.Error `json:"error"`
	}

	// SGItem represents a snapshot group item.
	SGItem struct {
		Kind                           string                           `json:"kind"`
		Name                           string                           `json:"name"`
		ID                             string                           `json:"id"`
		Status                         string                           `json:"status"`
		CreationTimestamp              string                           `json:"creationTimestamp"`
		SourceInfo                     SGSourceInfo                     `json:"sourceInfo"`
		SourceInstantSnapshotGroupInfo SGSourceInstantSnapshotGroupInfo `json:"sourceInstantSnapshotGroupInfo"`
		SelfLink                       string                           `json:"selfLink"`
		SelfLinkWithId                 string                           `json:"selfLinkWithId"`
	}

	// SGListResponse represents the response of a list snapshot groups request.
	SGListResponse struct {
		Items         []SGItem `json:"items"`
		NextPageToken string   `json:"nextPageToken"`
	}

	// SGSourceInfo represents the source info of a snapshot group.
	SGSourceInfo struct {
		ConsistencyGroup   string `json:"consistencyGroup"`
		ConsistencyGroupId string `json:"consistencyGroupId"`
	}

	// SGSourceInstantSnapshotGroupInfo represents the source instant snapshot group info of a snapshot group.
	SGSourceInstantSnapshotGroupInfo struct {
		InstantSnapshotGroup   string `json:"instantSnapshotGroup"`
		InstantSnapshotGroupId string `json:"instantSnapshotGroupId"`
	}

	// SnapshotItem represents a single snapshot item in the list.
	SnapshotItem struct {
		Kind                      string            `json:"kind"`
		ID                        string            `json:"id"`
		CreationTimestamp         string            `json:"creationTimestamp"`
		Name                      string            `json:"name"`
		Status                    string            `json:"status"`
		SourceDisk                string            `json:"sourceDisk"`
		SourceDiskID              string            `json:"sourceDiskID"`
		DiskSizeGB                string            `json:"diskSizeGb"`
		StorageBytes              string            `json:"storageBytes"`
		StorageBytesStatus        string            `json:"storageBytesStatus"`
		SelfLink                  string            `json:"selfLink"`
		SelfLinkWithID            string            `json:"selfLinkWithID"`
		LabelFingerprint          string            `json:"labelFingerprint"`
		Labels                    map[string]string `json:"labels"`
		StorageLocations          []string          `json:"storageLocations"`
		DownloadBytes             string            `json:"downloadBytes"`
		SourceInstantSnapshot     string            `json:"sourceInstantSnapshot"`
		SourceInstantSnapshotID   string            `json:"sourceInstantSnapshotID"`
		CreationSizeBytes         string            `json:"creationSizeBytes"`
		EnableConfidentialCompute bool              `json:"enableConfidentialCompute"`
		SnapshotGroupName         string            `json:"snapshotGroupName"`
		SnapshotGroupID           string            `json:"snapshotGroupID"`
	}

	// SnapshotListResponse represents the response of a list snapshots request.
	SnapshotListResponse struct {
		Kind          string         `json:"kind"`
		ID            string         `json:"id"`
		Items         []SnapshotItem `json:"items"`
		SelfLink      string         `json:"selfLink"`
		NextPageToken string         `json:"nextPageToken"`
	}

	// DiskItem represents a single disk item in the list.
	DiskItem struct {
		Kind              string `json:"kind"`
		ID                string `json:"id"`
		CreationTimestamp string `json:"creationTimestamp"`
		Name              string `json:"name"`
		Status            string `json:"status"`
		Zone              string `json:"zone"`
		SizeGb            string `json:"sizeGb"`
		SourceSnapshot    string `json:"sourceSnapshot"`
		SourceSnapshotID  string `json:"sourceSnapshotId"`
		SelfLink          string `json:"selfLink"`
		SelfLinkWithID    string `json:"selfLinkWithId"`
	}

	// DiskListResponse represents the response of a list disks request.
	DiskListResponse struct {
		Kind          string     `json:"kind"`
		ID            string     `json:"id"`
		Items         []DiskItem `json:"items"`
		SelfLink      string     `json:"selfLink"`
		NextPageToken string     `json:"nextPageToken"`
	}
)

func setupBackoff() *backoff.ExponentialBackOff {
	b := &backoff.ExponentialBackOff{
		InitialInterval:     2 * time.Second,
		RandomizationFactor: 0,
		Multiplier:          2,
		MaxInterval:         1 * time.Hour,
		MaxElapsedTime:      30 * time.Minute,
		Clock:               backoff.SystemClock,
	}
	b.Reset()
	return b
}

// NewService initializes the SGService with a new http client.
func (s *SGService) NewService() error {
	s.backoff = setupBackoff()
	s.maxRetries = defaultMaxRetries

	s.rest = &rest.Rest{}
	s.rest.NewRest()

	return nil
}

// GetResponse is a wrapper around rest.GetResponse.
func (s *SGService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) (bodyBytes []byte, err error) {
	bodyBytes, err = s.rest.GetResponse(ctx, method, baseURL, data)
	if err != nil {
		return nil, fmt.Errorf("failed to get response, err: %w", err)
	}

	op := &compute.Operation{}
	if err := json.Unmarshal(bodyBytes, op); err == nil && op.Error != nil {
		return nil, fmt.Errorf("Failed to unmarshal compute.Operation: %w", err)
	}

	var genericResponse map[string]any
	if err = json.Unmarshal(bodyBytes, &genericResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	if genericResponse["error"] != nil {
		var googleapiErr errorResponse
		if err = json.Unmarshal(bodyBytes, &googleapiErr); err != nil {
			return nil, fmt.Errorf("error from server: %v", genericResponse["error"])
		}

		log.CtxLogger(ctx).Errorw("getresponse error", "error", googleapiErr)
		return nil, fmt.Errorf("%s", googleapiErr.Err.Message)
	}
	return bodyBytes, nil
}

// CreateSG creates a snapshot group.
func (s *SGService) CreateSG(ctx context.Context, project string, data []byte) error {
	s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/global/snapshotGroups", project)

	bo := &backoff.ExponentialBackOff{
		InitialInterval:     s.backoff.InitialInterval,
		RandomizationFactor: s.backoff.RandomizationFactor,
		Multiplier:          s.backoff.Multiplier,
		MaxInterval:         s.backoff.MaxInterval,
		MaxElapsedTime:      s.backoff.MaxElapsedTime,
		Clock:               backoff.SystemClock,
	}

	var i int
	var bodyBytes []byte
	var err error
	if err = backoff.Retry(func() error {
		var getResponseErr error
		bodyBytes, getResponseErr = s.GetResponse(ctx, "POST", s.baseURL, data)
		if getResponseErr != nil {
			i++
			if i == s.maxRetries {
				return backoff.Permanent(getResponseErr)
			}
			return getResponseErr
		}

		return nil
	}, bo); err != nil {
		return fmt.Errorf("failed to initiate creation of group snapshot, err: %w", err)
	}

	log.CtxLogger(ctx).Debugw("CreateSG", "baseURL", s.baseURL, "data", string(data), "response", string(bodyBytes))

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}

	if op.Error != nil {
		log.CtxLogger(ctx).Errorw("CreateSG Error", "op.Error", op.Error)
		if len(op.Error.Errors) > 0 && op.Error.Errors[0].Message != "" {
			return fmt.Errorf("error creating snapshot group: %s", op.Error.Errors[0].Message)
		}
		if op.HttpErrorStatusCode > 0 {
			return fmt.Errorf("error creating snapshot group with status code: %d and message: %s", op.HttpErrorStatusCode, op.HttpErrorMessage)
		}
		return errors.New("failed to create snapshot group")
	}

	log.CtxLogger(ctx).Debugw("CreateSG Operation", "operation", op)

	return nil
}

// BulkInsertFromSG creates disks from a snapshot group.
func (s *SGService) BulkInsertFromSG(ctx context.Context, project, zone string, data []byte) (*compute.Operation, error) {
	s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/disks/bulkInsert", project, zone)

	bo := &backoff.ExponentialBackOff{
		InitialInterval:     s.backoff.InitialInterval,
		RandomizationFactor: s.backoff.RandomizationFactor,
		Multiplier:          s.backoff.Multiplier,
		MaxInterval:         s.backoff.MaxInterval,
		MaxElapsedTime:      s.backoff.MaxElapsedTime,
		Clock:               backoff.SystemClock,
	}

	var i int
	var bodyBytes []byte
	var err error
	if err = backoff.Retry(func() error {
		var getResponseErr error
		bodyBytes, getResponseErr = s.GetResponse(ctx, "POST", s.baseURL, data)
		if getResponseErr != nil {
			i++
			if i == s.maxRetries {
				return backoff.Permanent(getResponseErr)
			}
			return getResponseErr
		}

		return nil
	}, bo); err != nil {
		return nil, fmt.Errorf("failed to initiate bulk insert from group snapshot, err: %w", err)
	}

	log.CtxLogger(ctx).Debugw("BulkInsertFromSG", "baseURL", s.baseURL, "data", string(data), "response", string(bodyBytes))

	op := &compute.Operation{}
	if err := json.Unmarshal(bodyBytes, op); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}

	if op.Error != nil {
		log.CtxLogger(ctx).Errorw("BulkInsertFromSG Error", "op.Error", op.Error)
		if len(op.Error.Errors) > 0 && op.Error.Errors[0].Message != "" {
			return nil, fmt.Errorf("error bulk inserting from snapshot group: %s", op.Error.Errors[0].Message)
		}
		if op.HttpErrorStatusCode > 0 {
			return nil, fmt.Errorf("error bulk inserting from snapshot group with status code: %d and message: %s", op.HttpErrorStatusCode, op.HttpErrorMessage)
		}
		return nil, errors.New("failed to bulk insert from snapshot group")
	}

	log.CtxLogger(ctx).Debugw("BulkInsertFromSG Operation", "operation", op)

	return op, nil
}

// GetSG gets a snapshot group.
func (s *SGService) GetSG(ctx context.Context, project string, sgName string) (*SGItem, error) {
	s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/global/snapshotGroups/%s", project, sgName)

	var bodyBytes []byte
	var err error
	bodyBytes, err = s.GetResponse(ctx, "GET", s.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot group, err: %w", err)
	}

	log.CtxLogger(ctx).Debugw("GetSG", "baseURL", s.baseURL, "response", string(bodyBytes))

	sg := SGItem{}
	if err := json.Unmarshal(bodyBytes, &sg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}

	return &sg, nil
}

// ListSGs lists snapshot groups.
func (s *SGService) ListSGs(ctx context.Context, project string) ([]SGItem, error) {
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/global/snapshotGroups", project)
	}

	var sgs []SGItem
	var nextPageToken string

	for {
		url := s.baseURL
		if nextPageToken != "" {
			url = fmt.Sprintf("%s?pageToken=%s", url, nextPageToken)
		}

		var bodyBytes []byte
		var err error
		// s.GetResponse returns the response body as a byte slice and handles closing the underlying http.Response.Body.
		bodyBytes, err = s.GetResponse(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list snapshot groups, err: %w", err)
		}
		log.CtxLogger(ctx).Debugw("ListSGs", "url", url, "response", string(bodyBytes))

		var listResponse SGListResponse
		if err := json.Unmarshal(bodyBytes, &listResponse); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
		}

		sgs = append(sgs, listResponse.Items...)
		nextPageToken = listResponse.NextPageToken

		if nextPageToken == "" {
			break
		}
	}

	return sgs, nil
}

// WaitForSGUploadCompletion waits for the given snapshot group upload to complete.
func (s *SGService) WaitForSGUploadCompletion(ctx context.Context, project, sgName string) error {
	sg, err := s.GetSG(ctx, project, sgName)
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Debugf("Snapshot group status: %s", sg.Status)
	if sg.Status != "READY" {
		return fmt.Errorf("snapshot group upload is still in progress, status: %s", sg.Status)
	}
	return nil
}

// WaitForSGUploadCompletionWithRetry waits for the given snapshot group creation operation
// to complete with constant backoff retries.
func (s *SGService) WaitForSGUploadCompletionWithRetry(ctx context.Context, project, sgName string) error {
	bo := &backoff.ExponentialBackOff{
		InitialInterval:     s.backoff.InitialInterval,
		RandomizationFactor: s.backoff.RandomizationFactor,
		Multiplier:          s.backoff.Multiplier,
		MaxInterval:         s.backoff.MaxInterval,
		MaxElapsedTime:      s.backoff.MaxElapsedTime,
		Clock:               backoff.SystemClock,
	}

	operation := func() error {
		return s.WaitForSGUploadCompletion(ctx, project, sgName)
	}
	backoffWithMaxRetries := backoff.WithMaxRetries(bo, uint64(s.maxRetries-1))
	return backoff.Retry(operation, backoffWithMaxRetries)
}

// WaitForSGCreation waits for the given snapshot group to be created.
func (s *SGService) WaitForSGCreation(ctx context.Context, project, sgName string) error {
	sg, err := s.GetSG(ctx, project, sgName)
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Debugf("Snapshot group status: %s", sg.Status)
	if sg.Status == "CREATING" {
		return fmt.Errorf("snapshot group creation is still in progress, status: %s", sg.Status)
	}
	return nil
}

// WaitForSGCreationWithRetry waits for the given snapshot group creation to complete with
// constant backoff retries. The retry will stop once the status is not "CREATING".
func (s *SGService) WaitForSGCreationWithRetry(ctx context.Context, project, sgName string) error {
	bo := &backoff.ExponentialBackOff{
		InitialInterval:     s.backoff.InitialInterval,
		RandomizationFactor: s.backoff.RandomizationFactor,
		Multiplier:          s.backoff.Multiplier,
		MaxInterval:         s.backoff.MaxInterval,
		MaxElapsedTime:      s.backoff.MaxElapsedTime,
		Clock:               backoff.SystemClock,
	}

	operation := func() error {
		return s.WaitForSGCreation(ctx, project, sgName)
	}
	backoffWithMaxRetries := backoff.WithMaxRetries(bo, uint64(s.maxRetries-1))
	return backoff.Retry(operation, backoffWithMaxRetries)
}

// ListSnapshotsFromSG lists snapshots for a given snapshot group.
func (s *SGService) ListSnapshotsFromSG(ctx context.Context, project, sgName string) ([]SnapshotItem, error) {
	filterValue := fmt.Sprintf(`snapshotGroupName="https://www.googleapis.com/compute/alpha/projects/%s/global/snapshotGroups/%s"`, project, sgName)
	s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/global/snapshots?filter=%s", project, url.QueryEscape(filterValue))

	var snaps []SnapshotItem
	var nextPageToken string

	for {
		url := s.baseURL
		if nextPageToken != "" {
			url = fmt.Sprintf("%s&pageToken=%s", url, nextPageToken)
		}

		var bodyBytes []byte
		var err error
		bodyBytes, err = s.GetResponse(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list snapshots for snapshot group %s, err: %w", sgName, err)
		}
		log.CtxLogger(ctx).Debugw("ListSnapshotsFromSG", "url", url, "response", string(bodyBytes))

		var listResponse SnapshotListResponse
		if err := json.Unmarshal(bodyBytes, &listResponse); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
		}

		snaps = append(snaps, listResponse.Items...)
		nextPageToken = listResponse.NextPageToken

		if nextPageToken == "" {
			break
		}
	}

	return snaps, nil
}

// ListDisksFromSnapshot lists disks restored from a given standard snapshot.
func (s *SGService) ListDisksFromSnapshot(ctx context.Context, project, zone, snapshotName string) ([]DiskItem, error) {
	filterValue := fmt.Sprintf(`sourceSnapshot="https://www.googleapis.com/compute/alpha/projects/%s/global/snapshots/%s"`, project, snapshotName)
	s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/disks?filter=%s", project, zone, url.QueryEscape(filterValue))

	var disks []DiskItem
	var nextPageToken string

	for {
		url := s.baseURL
		if nextPageToken != "" {
			url = fmt.Sprintf("%s&pageToken=%s", url, nextPageToken)
		}

		var bodyBytes []byte
		var err error
		bodyBytes, err = s.GetResponse(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list disks for snapshot %s, err: %w", snapshotName, err)
		}
		log.CtxLogger(ctx).Debugw("ListDisksFromSnapshot", "url", url, "response", string(bodyBytes))

		var listResponse DiskListResponse
		if err := json.Unmarshal(bodyBytes, &listResponse); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
		}

		disks = append(disks, listResponse.Items...)
		nextPageToken = listResponse.NextPageToken

		if nextPageToken == "" {
			break
		}
	}

	return disks, nil
}

// sgExists checks if the given snapshot group exists.
func (s *SGService) sgExists(ctx context.Context, opLink string) error {
	bodyBytes, err := s.GetResponse(ctx, "GET", opLink, nil)
	log.CtxLogger(ctx).Debugw("sgExists", "baseURL", s.baseURL, "bodyBytes", string(bodyBytes))
	if err != nil {
		s.baseURL = ""
		return fmt.Errorf("failed to delete Snapshot Group, err: %w", err)
	}
	s.baseURL = ""

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	if op.Error != nil {
		log.CtxLogger(ctx).Errorw("isgExists Error", "op", op)
		return fmt.Errorf("failed to delete Snapshot Group, err: %s", op.Error.Errors[0].Message)
	}

	log.CtxLogger(ctx).Debugw("Operation", "status", op.Status, "op", op)
	if op.Status != "DONE" {
		return fmt.Errorf("Snapshot Group deletion in progress")
	}
	return nil
}

// DeleteSG deletes a snapshot group.
func (s *SGService) DeleteSG(ctx context.Context, project, sgName string) error {
	// Wait for the snapshot group upload to complete before deletion.
	// Deletion while upload is in progress results in a failed operation.
	if err := s.WaitForSGUploadCompletionWithRetry(ctx, project, sgName); err != nil {
		return err
	}
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/global/snapshotGroups/%s", project, sgName)
	}

	bo := &backoff.ExponentialBackOff{
		InitialInterval:     s.backoff.InitialInterval,
		RandomizationFactor: s.backoff.RandomizationFactor,
		Multiplier:          s.backoff.Multiplier,
		MaxInterval:         s.backoff.MaxInterval,
		MaxElapsedTime:      s.backoff.MaxElapsedTime,
		Clock:               backoff.SystemClock,
	}
	bo.Reset()

	var i int
	var bodyBytes []byte
	if err := backoff.Retry(func() error {
		var getResponseErr error
		bodyBytes, getResponseErr = s.GetResponse(ctx, "DELETE", s.baseURL, nil)
		if getResponseErr != nil {
			i++
			if i == s.maxRetries {
				return backoff.Permanent(getResponseErr)
			}
			return getResponseErr
		}

		return nil
	}, bo); err != nil {
		s.baseURL = ""
		return fmt.Errorf("failed to initiate deletion of Snapshot Group, err: %w", err)
	}
	s.baseURL = ""
	log.CtxLogger(ctx).Debugw("DeleteSG Response", "response", string(bodyBytes))

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}

	if op.Error != nil {
		log.CtxLogger(ctx).Errorw("DeleteSG Error", "op.Error", op.Error)
		return fmt.Errorf("failed to delete Snapshot Group, err: %s", op.Error.Errors[0].Message)
	}

	bo.Reset()
	i = 0
	return backoff.Retry(func() error {
		if err := s.sgExists(ctx, op.SelfLink); err != nil {
			i++
			if i == s.maxRetries {
				return backoff.Permanent(s.sgExists(ctx, op.SelfLink))
			}
			return err
		}

		return nil
	}, bo)
}
