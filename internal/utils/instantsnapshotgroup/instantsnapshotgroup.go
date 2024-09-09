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

// Package instantsnapshotgroup is used to provide instant snapshot group related functions.
package instantsnapshotgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// EnvKey is the context key for environment.
	EnvKey string

	// defaultTokenGetter abstracts getting default oauth2 access token.
	defaultTokenGetter func(context.Context, ...string) (oauth2.TokenSource, error)

	// JSONCredentialsGetter abstracts obtaining JSON oauth2 google credentials.
	JSONCredentialsGetter func(context.Context, []byte, ...string) (*google.Credentials, error)

	// ISGService is a temporary interface representing the expected behavior of Instant Snapshot Groups.
	// It will be replaced by a concrete struct when the full implementation is available.
	ISGService struct {
		httpClient  httpClient
		tokenGetter defaultTokenGetter
		baseURL     string
		maxRetries  uint64
	}

	// ISResponse is the response for IS.
	ISResponse struct {
		Kind     string   `json:"kind"`
		ID       string   `json:"id"`
		Items    []ISItem `json:"items"`
		SelfLink string   `json:"selfLink"`
	}

	// ISItem is the item for IS.
	ISItem struct {
		Kind              string `json:"kind"`
		ID                string `json:"id"`
		CreationTimestamp string `json:"creationTimestamp"`
		Name              string `json:"name"`
		Description       string `json:"description"`

		Status                       string            `json:"status"`
		SourceDisk                   string            `json:"sourceDisk"`
		SourceDiskID                 string            `json:"sourceDiskId"`
		DiskSizeGb                   string            `json:"diskSizeGb"`
		SelfLink                     string            `json:"selfLink"`
		SelfLinkWithID               string            `json:"selfLinkWithId"`
		LabelFingerprint             string            `json:"labelFingerprint"`
		Zone                         string            `json:"zone"`
		ResourceStatus               map[string]string `json:"resourceStatus"`
		SatisfiesPzi                 bool              `json:"satisfiesPzi"`
		SourceInstantSnapshotGroup   string            `json:"sourceInstantSnapshotGroup"`
		SourceInstantSnapshotGroupID string            `json:"sourceInstantSnapshotGroupId"`
	}

	httpClient interface {
		Do(req *http.Request) (*http.Response, error)
	}
)

var (
	defaultNewClient = func(timeout time.Duration, trans *http.Transport) httpClient {
		return &http.Client{Timeout: timeout, Transport: trans}
	}
	defaultTransport = func() *http.Transport {
		return &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxConnsPerHost:       100,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       10 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
)

// NewService initializes the ISGService with a new http client.
func (s *ISGService) NewService() error {
	s.httpClient = defaultNewClient(10*time.Minute, defaultTransport())
	s.tokenGetter = google.DefaultTokenSource
	s.maxRetries = 300
	return nil
}

func token(ctx context.Context, tokenGetter defaultTokenGetter) (*oauth2.Token, error) {
	tokenSource, err := tokenGetter(ctx)
	if err != nil {
		return nil, err
	}
	return tokenSource.Token()
}

// GetResponse creates a new request with given method, url and data and returns the response.
func (s *ISGService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error) {
	log.CtxLogger(ctx).Infow("GetResponse", "method", method, "baseURL", baseURL, "data", string(data))
	req, err := http.NewRequest(method, baseURL, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request, err: %w", err)
	}

	token, err := token(ctx, s.tokenGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to get token, err: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+token.AccessToken)
	req.Header.Add("Content-Type", "application/json")
	token.SetAuthHeader(req)

	resp, err := s.httpClient.Do(req)
	defer googleapi.CloseBody(resp)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return bodyBytes, err
}

// getProcessStatus  returns the status of the given process.
func (s *ISGService) getProcessStatus(ctx context.Context, baseURL string) (string, error) {
	bodyBytes, err := s.GetResponse(ctx, "GET", baseURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get process status, err: %w", err)
	}
	log.CtxLogger(ctx).Debugw("getProcessStatus Response", "response", string(bodyBytes))

	var response map[string]any
	if err := json.Unmarshal([]byte(bodyBytes), &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	if response["status"] == nil {
		return "", fmt.Errorf("no status found in response")
	}
	return response["status"].(string), nil
}

// CreateISG creates an instant snapshot group.
func (s *ISGService) CreateISG(ctx context.Context, project, zone string, data []byte) error {
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/instantSnapshotGroups", project, zone)
	}
	bodyBytes, err := s.GetResponse(ctx, "POST", s.baseURL, data)
	log.CtxLogger(ctx).Debugw("CreateISG", "baseURL", s.baseURL, "data", string(data), "response", string(bodyBytes))
	if err != nil {
		s.baseURL = ""
		return fmt.Errorf("failed to create Instant Snapshot Group, err: %w", err)
	}
	s.baseURL = ""
	bodyString := string(bodyBytes) // Convert to string
	log.CtxLogger(ctx).Debugw("CreateISG Response", "response", bodyString)

	return nil
}

// parseInstantSnapshotGroupURL parses the URL of instant snapshot group and returns the zone and
// instant snapshot group name.
func parseInstantSnapshotGroupURL(cgURL string) (string, string, error) {
	parts := strings.Split(cgURL, "/")
	if len(parts) < 11 {
		return "", "", fmt.Errorf("invalid URL format")
	}

	zone := parts[8]
	cg := parts[10]
	return zone, cg, nil
}

func (s *ISGService) isgExists(ctx context.Context, project, zone, opName string) error {
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/operations/%s", project, zone, opName)
	}
	bodyBytes, err := s.GetResponse(ctx, "GET", s.baseURL, nil)
	log.CtxLogger(ctx).Debugw("isgExists", "baseURL", s.baseURL, "bodyBytes", string(bodyBytes))
	if err != nil {
		s.baseURL = ""
		return fmt.Errorf("failed to get Instant Snapshot Group, err: %w", err)
	}
	s.baseURL = ""

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	log.CtxLogger(ctx).Debugw("Operation", "status", op.Status, "op", op)
	if op.Status == "DELETING" {
		return fmt.Errorf("Instant Snapshot Group deletion in progress")
	}
	if op.Status == "READY" {
		return nil
	}
	return nil
}

// DescribeInstantSnapshots returns the list of instant snapshots for a given group snapshot.
func (s *ISGService) DescribeInstantSnapshots(ctx context.Context, project, zone, isg string) ([]ISItem, error) {
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/instantSnapshots", project, zone)
	}
	bodyBytes, err := s.GetResponse(ctx, "GET", s.baseURL, nil)
	log.CtxLogger(ctx).Debugw("DescribeInstantSnapshots", "baseURL", s.baseURL, "bodyBytes", string(bodyBytes))
	if err != nil {
		s.baseURL = ""
		return nil, fmt.Errorf("failed to list instant snapshots for given group snapshot, err: %w", err)
	}
	s.baseURL = ""

	var isItems []ISItem
	var isResp ISResponse
	if err := json.Unmarshal(bodyBytes, &isResp); err != nil {
		return nil, err
	}
	log.CtxLogger(ctx).Debugw("Instant Snapshot Response", "instantSnapshots", isResp.Items)
	for _, is := range isResp.Items {
		isZone, isISG, err := parseInstantSnapshotGroupURL(is.SourceInstantSnapshotGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Instant Snapshot Group URL, err: %w", err)
		}
		if isZone == zone && isISG == isg {
			isItems = append(isItems, is)
		}
	}

	log.CtxLogger(ctx).Debugw("ISResp", "isResp.Items", isItems)
	return isItems, nil
}

// DeleteISG deletes the given instant snapshot group.
func (s *ISGService) DeleteISG(ctx context.Context, project, zone, isgName string) error {
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/instantSnapshotGroups/%s", project, zone, isgName)
	}
	bodyBytes, err := s.GetResponse(ctx, "DELETE", s.baseURL, nil)
	log.CtxLogger(ctx).Debugw("DeleteISG", "baseURL", s.baseURL, "bodyBytes", string(bodyBytes))
	if err != nil {
		s.baseURL = ""
		return fmt.Errorf("failed to initiate deletion of Instant Snapshot Group, err: %w", err)
	}
	s.baseURL = ""
	log.CtxLogger(ctx).Debugw("DeleteISG Response", "response", string(bodyBytes))

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}

	constantBackoff := backoff.NewConstantBackOff(1 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, s.maxRetries), ctx)
	return backoff.Retry(func() error {
		return s.isgExists(ctx, project, zone, op.Name)
	}, bo)
}

func (s *ISGService) waitForISGUploadCompletion(ctx context.Context, baseURL string) error {
	status, err := s.getProcessStatus(ctx, baseURL)
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Debug("Snapshot status:", status)
	if status != "READY" {
		return fmt.Errorf("Instant Snapshot Group upload is still in progress, status: %s", status)
	}
	return nil
}

// WaitForISGUploadCompletionWithRetry waits for the given snapshot creation operation
// to complete with constant backoff retries.
func (s *ISGService) WaitForISGUploadCompletionWithRetry(ctx context.Context, baseURL string) error {
	constantBackoff := backoff.NewConstantBackOff(1 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, s.maxRetries), ctx)
	return backoff.Retry(func() error {
		return s.waitForISGUploadCompletion(ctx, baseURL)
	}, bo)
}
