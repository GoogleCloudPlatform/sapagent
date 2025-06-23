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
	// TODO: Implement this function.
	return nil, nil
}
