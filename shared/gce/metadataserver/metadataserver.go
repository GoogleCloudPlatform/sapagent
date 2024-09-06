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
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// Default values if information cannot be obtained from the metadata server.
const (
	ImageUnknown       = "unknown"
	MachineTypeUnknown = "unknown"
)

var (
	zonePattern        = regexp.MustCompile("zones/([^/]*)")
	machineTypePattern = regexp.MustCompile("machineTypes/([^/]*)")

	// not a const so we can override in test suite.
	metadataServerURL                     = "http://metadata.google.internal/computeMetadata/v1"
	metadataNoUpcomingMaintenanceResponse = `{ "error": "no notifications have been received yet, try again later" }`
)

const (
	cloudPropertiesURI     = "/"
	maintenanceEventURI    = "/instance/maintenance-event"
	upcomingMaintenanceURI = "/instance/upcoming-maintenance"
	diskType               = "/instance/disks/"

	helpString = `For information on permissions needed to access metadata refer: https://cloud.google.com/compute/docs/metadata/querying-metadata#permissions. Restart the agent after adding necessary permissions.`
)

type (
	metadataServerResponse struct {
		Project  projectInfo  `json:"project"`
		Instance instanceInfo `json:"instance"`
	}

	projectInfo struct {
		ProjectID        string `json:"projectId"`
		NumericProjectID int64  `json:"numericProjectId"`
	}

	instanceInfo struct {
		ID          int64  `json:"id"`
		Zone        string `json:"zone"`
		Name        string `json:"name"`
		Image       string `json:"image"`
		MachineType string `json:"machineType"`
	}

	// CloudProperties contains the cloud properties of the instance.
	CloudProperties struct {
		ProjectID, NumericProjectID, InstanceID, Zone, InstanceName, Image, MachineType string
	}
)

// CloudPropertiesWithRetry fetches information from the GCE metadata server with a retry mechanism.
//
// If there are any persistent errors in fetching this information, then the error will be logged
// and the return value will be nil.
// This API will be deprecated and replaced by ReadCloudPropertiesWithRetry once the consumers have been migrated.
func CloudPropertiesWithRetry(bo backoff.BackOff) *instancepb.CloudProperties {
	var (
		attempt = 1
		cp      *instancepb.CloudProperties
	)
	err := backoff.Retry(func() error {
		var err error
		cp, err = requestCloudProperties()
		if err != nil {
			log.Logger.Warnw("Error in requestCloudProperties", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, bo)
	if err != nil {
		log.Logger.Errorw("CloudProperties request retry limit exceeded", log.Error(err))
	}
	return cp
}

// ReadCloudPropertiesWithRetry fetches information from the GCE metadata server with a retry mechanism.
//
// If there are any persistent errors in fetching this information, then the error will be logged
// and the return value will be nil.
func ReadCloudPropertiesWithRetry(bo backoff.BackOff) *CloudProperties {
	var (
		attempt = 1
		cp      *CloudProperties
	)
	err := backoff.Retry(func() error {
		var err error
		cp, err = requestProperties()
		if err != nil {
			log.Logger.Warnw("Error in requestCloudProperties", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, bo)
	if err != nil {
		log.Logger.Errorw("CloudProperties request retry limit exceeded", log.Error(err))
	}
	return cp
}

// DiskTypeWithRetry fetches disk information from the GCE metadata server with a retry mechanism.
//
// If there are any persistent errors in fetching this information, then the error will be logged
// and the return value will be "".
func DiskTypeWithRetry(bo backoff.BackOff, disk string) string {
	var (
		attempt  = 1
		diskType string
	)
	err := backoff.Retry(func() error {
		var err error
		diskType, err = requestDiskType(disk)
		if err != nil {
			log.Logger.Warnw("Error in requestDiskType", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, bo)
	if err != nil {
		log.Logger.Errorw("DiskType request retry limit exceeded", log.Error(err))
	}
	return diskType
}

// get performs a get request to the metadata server and returns the response body.
func get(uri, queryString string) ([]byte, error) {
	metadataURL, err := url.Parse(metadataServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata server url: %v, %s", err, helpString)
	}
	metadataURL.RawQuery = queryString
	reqURL := metadataURL.JoinPath(uri).String()
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to metadata server: %v, %s", err, helpString)
	}
	req.Header.Add("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response from metadata server: %v, %s", err, helpString)
	}
	defer res.Body.Close()
	if !isStatusSuccess(res.StatusCode) {
		if uri == upcomingMaintenanceURI && res.StatusCode == 503 {
			body, errIO := io.ReadAll(res.Body)
			if errIO != nil {
				return nil, fmt.Errorf("failed to read response body from metadata server: %v", err)
			}
			return body, nil
		}
		return nil, fmt.Errorf("unsuccessful response from metadata server: %s, %s", res.Status, helpString)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from metadata server: %v", err)
	}
	return body, nil
}

// requestCloudProperties attempts to fetch information from the GCE metadata server.
// This function will be deprecated and replaced by requestProperties once the consumers have been migrated.
func requestCloudProperties() (*instancepb.CloudProperties, error) {
	body, err := get(cloudPropertiesURI, "recursive=true")
	if err != nil {
		return nil, err
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
	machineType := parseMachineType(instance.MachineType)
	instanceName := instance.Name
	image := instance.Image

	if image == "" {
		image = ImageUnknown
	}
	if machineType == "" {
		machineType = MachineTypeUnknown
	}

	log.Logger.Debugw("Default Cloud Properties from metadata server",
		"projectid", projectID, "projectnumber", numericProjectID, "instanceid", instanceID, "zone", zone, "instancename", instanceName, "image", image, "machinetype", machineType)

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
		MachineType:      machineType,
	}, nil
}

// requestProperties attempts to fetch information from the GCE metadata server.
func requestProperties() (*CloudProperties, error) {
	body, err := get(cloudPropertiesURI, "recursive=true")
	if err != nil {
		return nil, err
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
	machineType := parseMachineType(instance.MachineType)
	instanceName := instance.Name
	image := instance.Image

	if image == "" {
		image = ImageUnknown
	}
	if machineType == "" {
		machineType = MachineTypeUnknown
	}

	log.Logger.Debugw("Default Cloud Properties from metadata server",
		"projectid", projectID, "projectnumber", numericProjectID, "instanceid", instanceID, "zone", zone, "instancename", instanceName, "image", image, "machinetype", machineType)

	if projectID == "" || numericProjectID == "0" || instanceID == "0" || zone == "" || instanceName == "" {
		return nil, fmt.Errorf("metadata server responded with incomplete information")
	}

	return &CloudProperties{
		ProjectID:        projectID,
		NumericProjectID: numericProjectID,
		InstanceID:       instanceID,
		Zone:             zone,
		InstanceName:     instanceName,
		Image:            image,
		MachineType:      machineType,
	}, nil
}

// requestDiskType attempts to fetch information from the GCE metadata server.
func requestDiskType(disk string) (string, error) {
	body, err := get(fmt.Sprintf("%s%s/type", diskType, disk), "recursive=true")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func isStatusSuccess(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode <= 299
}

// parseZone retrieves the zone name from the metadata server response.
//
// The metadata server returns the zone as "projects/PROJECT_NUM/zones/ZONE_NAME" but we only need ZONE_NAME.
func parseZone(raw string) string {
	var zone string
	match := zonePattern.FindStringSubmatch(raw)
	if len(match) >= 2 {
		zone = match[1]
	}
	return zone
}

// parseMachineType retrieves the machine type from the response.
// The metadata server returns the machine type as
// "projects/PROJECT_NUM/machineTypes/MACHINE_TYPE", we only need MACHINE_TYPE.
func parseMachineType(raw string) string {
	match := machineTypePattern.FindStringSubmatch(raw)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// FetchCloudProperties retrieves the cloud properties using a backoff policy.
func FetchCloudProperties() *CloudProperties {
	exp := backoff.NewExponentialBackOff()
	return ReadCloudPropertiesWithRetry(backoff.WithMaxRetries(exp, 1)) // 1 retry (2 total attempts)
}

// FetchGCEMaintenanceEvent retrieves information about pending host maintenance events.
func FetchGCEMaintenanceEvent() (string, error) {
	body, err := get(maintenanceEventURI, "")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// FetchGCEUpcomingMaintenance retrieves information about upcoming host maintenance events.
func FetchGCEUpcomingMaintenance() (string, error) {
	body, err := get(upcomingMaintenanceURI, "")
	if err != nil {
		return "", err
	}
	return string(body), nil
}
