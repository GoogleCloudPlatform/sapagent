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
	"time"

	"github.com/cenkalti/backoff/v4"
	"google.golang.org/api/googleapi"
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

// CreateSG creates a snapshot group.
func (s *SGService) CreateSG(ctx context.Context, project string, data []byte) error {
	// TODO: Implement this function.
	return nil
}

// GetSG gets a snapshot group.
func (s *SGService) GetSG(ctx context.Context, project string, sgName string) (*SGItem, error) {
	// TODO: Implement this function.
	return nil, nil
}

// ListSGs lists snapshot groups.
func (s *SGService) ListSGs(ctx context.Context, project string) ([]SGItem, error) {
	// TODO: Implement this function.
	return nil, nil
}
