/*
Copyright 2022 Google LLC

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

// Package metadataserver performs requests to the metadata server of a GCE instance.
//
// Interfacing with the metadata server is necessary to obtain project-level and per-instance
// metadata for use by the gcagent. Requests to the metadata server will also be used as a
// logging mechanism for gcagent usage metrics.
package metadataserver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// ImageUnknown is the default value if image information cannot be obtained from the metadata server.
const ImageUnknown = "unknown"

var (
	zonePattern = regexp.MustCompile("zones/([^/]*)")

	// not a const so we can override in test suite.
	metadataServerURL = "http://metadata.google.internal/computeMetadata/v1/?recursive=true"
)

type metadataServerResponse struct {
	Project  projectInfo  `json:"project"`
	Instance instanceInfo `json:"instance"`
}

type projectInfo struct {
	ProjectID        string `json:"projectId"`
	NumericProjectID int64  `json:"numericProjectId"`
}

type instanceInfo struct {
	ID    int64  `json:"id"`
	Zone  string `json:"zone"`
	Name  string `json:"name"`
	Image string `json:"image"`
}

// CloudPropertiesWithRetry fetches information from the GCE metadata server with a retry mechanism.
//
// If there are any persistent errors in fetching this information, then the error will be logged
// and the return value will be nil.
func CloudPropertiesWithRetry(bo backoff.BackOff) *instancepb.CloudProperties {
	var (
		attempt = 1
		cp      *instancepb.CloudProperties
	)
	err := backoff.Retry(func() error {
		var err error
		cp, err = requestCloudProperties()
		if err != nil {
			log.Logger.Errorw("Error in requestCloudProperties", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, bo)
	if err != nil {
		log.Logger.Errorw("CloudProperties request retry limit exceeded", log.Error(err))
	}
	return cp
}

// requestCloudProperties attempts to fetch information from the GCE metadata server.
func requestCloudProperties() (*instancepb.CloudProperties, error) {
	helpString := `For information on permissions needed to access metadata refer: https://cloud.google.com/compute/docs/metadata/querying-metadata#permissions. Restart the agent after adding necessary permissions.`

	req, err := http.NewRequest("GET", metadataServerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to metadata server: %v, %s", err, helpString)
	}
	req.Header.Add("Metadata-Flavor", "Google")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response from metadata server: %v, %s", err, helpString)
	}
	defer res.Body.Close()
	if !isStatusSuccess(res.StatusCode) {
		return nil, fmt.Errorf("unsuccessful response from metadata server: %s, %s", res.Status, helpString)
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from metadata server: %v", err)
	}
	resBodyJSON := &metadataServerResponse{}
	if err = json.Unmarshal(body, resBodyJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body from metadata server: %v", err)
	}

	project := resBodyJSON.Project
	projectID := project.ProjectID
	numericProjectID := strconv.FormatInt(int64(project.NumericProjectID), 10)
	instance := resBodyJSON.Instance
	instanceID := strconv.FormatInt(int64(instance.ID), 10)
	zone := parseZone(instance.Zone)
	instanceName := instance.Name
	image := instance.Image
	if image == "" {
		image = ImageUnknown
	}

	log.Logger.Infow("Default Cloud Properties from metadata server",
		"projectid", projectID, "projectnumber", numericProjectID, "instanceid", instanceID, "zone", zone, "instancename", instanceName, "image", image)

	if projectID == "" || numericProjectID == "0" || instanceID == "0" || zone == "" || instanceName == "" {
		return nil, fmt.Errorf("metadata server responded with incomplete information")
	}

	return &instancepb.CloudProperties{
		ProjectId:        projectID,
		NumericProjectId: numericProjectID,
		InstanceId:       instanceID,
		Zone:             zone,
		InstanceName:     instanceName,
		Image:            image,
	}, nil
}

func isStatusSuccess(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode <= 299
}

// parseZone retrieves the zone name from the metadata server response.
//
// The metadata server returns zone as "projects/PROJECT_NUM/zones/ZONE_NAME" but we only need ZONE_NAME.
func parseZone(raw string) string {
	var zone string
	match := zonePattern.FindStringSubmatch(raw)
	if len(match) >= 2 {
		zone = match[1]
	}
	return zone
}
