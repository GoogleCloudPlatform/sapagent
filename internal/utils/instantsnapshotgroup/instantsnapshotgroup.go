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
		backoff     *backoff.ExponentialBackOff
		maxRetries  int
	}

	// ISResponse is the response for IS.
	ISResponse struct {
		Kind          string   `json:"kind"`
		ID            string   `json:"id"`
		NextPageToken string   `json:"nextPageToken"`
		Items         []ISItem `json:"items"`
		SelfLink      string   `json:"selfLink"`
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

	errorResponse struct {
		Err googleapi.Error `json:"error"`
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

// NewService initializes the ISGService with a new http client.
func (s *ISGService) NewService() error {
	s.httpClient = defaultNewClient(10*time.Minute, defaultTransport())
	s.tokenGetter = google.DefaultTokenSource
	s.backoff = setupBackoff()
	s.maxRetries = 3
	return nil
}

// token fetches a token with default or workload identity federation credentials.
func token(ctx context.Context, tokenGetter defaultTokenGetter) (*oauth2.Token, error) {
	tokenScope := "https://www.googleapis.com/auth/cloud-platform"
	tokenSource, err := tokenGetter(ctx, tokenScope)
	if err != nil {
		return nil, err
	}
	return tokenSource.Token()
}

// GetResponse creates a new request with given method, url and data and returns the response.
func (s *ISGService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error) {
	log.CtxLogger(ctx).Debugw("GetResponse", "method", method, "baseURL", baseURL, "data", string(data))
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

	var genericResponse map[string]any
	if err := json.Unmarshal(bodyBytes, &genericResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	if genericResponse["error"] != nil {
		var googleapiErr errorResponse
		if err := json.Unmarshal([]byte(bodyBytes), &googleapiErr); err != nil {
			return nil, fmt.Errorf("failed to unmarshal googleapi error, err: %w", err)
		}

		log.CtxLogger(ctx).Errorw("getresponse error", "error", googleapiErr)
		if googleapiErr.Err.Code != http.StatusOK {
			return nil, fmt.Errorf("%s", googleapiErr.Err.Message)
		}
	}
	return bodyBytes, nil
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
		s.baseURL = ""
		return fmt.Errorf("failed to initiate creation of group snapshot, err: %w", err)
	}

	log.CtxLogger(ctx).Debugw("CreateISG", "baseURL", s.baseURL, "data", string(data), "response", string(bodyBytes))
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
		return fmt.Errorf("failed to delete Instant Snapshot Group, err: %w", err)
	}
	s.baseURL = ""

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	if op.Error != nil {
		log.CtxLogger(ctx).Errorw("isgExists Error", "op", op)
		return fmt.Errorf("failed to delete Instant Snapshot Group, err: %s", op.Error.Errors[0].Message)
	}

	log.CtxLogger(ctx).Debugw("Operation", "status", op.Status, "op", op)
	if op.Status != "DONE" {
		return fmt.Errorf("Instant Snapshot Group deletion in progress")
	}
	return nil
}

// DescribeInstantSnapshots returns the list of instant snapshots for a given group snapshot.
func (s *ISGService) DescribeInstantSnapshots(ctx context.Context, project, zone, isg string) ([]ISItem, error) {
	if s.baseURL == "" {
		s.baseURL = fmt.Sprintf("https://compute.googleapis.com/compute/alpha/projects/%s/zones/%s/instantSnapshots", project, zone)
	}
	baseURL := s.baseURL
	bodyBytes, err := s.GetResponse(ctx, "GET", baseURL, nil)
	log.CtxLogger(ctx).Debugw("DescribeInstantSnapshots", "baseURL", baseURL, "bodyBytes", string(bodyBytes))
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

	pageToken := ""
	for {
		for _, is := range isResp.Items {
			isZone, isISG, err := parseInstantSnapshotGroupURL(is.SourceInstantSnapshotGroup)
			if err != nil {
				return nil, fmt.Errorf("failed to parse Instant Snapshot Group URL, err: %w", err)
			}
			if isZone == zone && isISG == isg {
				isItems = append(isItems, is)
			}
		}

		if isResp.NextPageToken != "" {
			pageToken = isResp.NextPageToken
		} else {
			break
		}
		isResp = ISResponse{}

		bodyBytes, err := s.GetResponse(ctx, "GET", baseURL+"?pageToken="+pageToken, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list instant snapshots for given group snapshot, err: %w", err)
		}
		if err := json.Unmarshal(bodyBytes, &isResp); err != nil {
			return nil, err
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
		return fmt.Errorf("failed to initiate deletion of Instant Snapshot Group, err: %w", err)
	}
	s.baseURL = ""
	log.CtxLogger(ctx).Debugw("DeleteISG Response", "response", string(bodyBytes))

	op := compute.Operation{}
	if err := json.Unmarshal(bodyBytes, &op); err != nil {
		return fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}

	if op.Error != nil {
		log.CtxLogger(ctx).Errorw("DeleteISG Error", "op.Error", op.Error)
		return fmt.Errorf("failed to delete Instant Snapshot Group, err: %s", op.Error.Errors[0].Message)
	}

	bo.Reset()
	i = 0
	return backoff.Retry(func() error {
		if err := s.isgExists(ctx, project, zone, op.Name); err != nil {
			i++
			if i == s.maxRetries {
				return backoff.Permanent(s.isgExists(ctx, project, zone, op.Name))
			}
			return err
		}

		return nil
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
	return backoff.Retry(func() error {
		if err := s.waitForISGUploadCompletion(ctx, baseURL); err != nil {
			i++
			if i == s.maxRetries {
				return backoff.Permanent(err)
			}
			return err
		}

		return nil
	}, bo)
}
