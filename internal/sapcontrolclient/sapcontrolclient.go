/*
Copyright 2023 Google LLC

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

// Package sapcontrolclient implements functions to perform SAPControl API calls
package sapcontrolclient

import (
	"encoding/xml"
	"fmt"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/soap"
)

type (
	// Client implements sapcontrol calls supported for all sap instance types.
	Client struct {
		soap *soap.Client
		sn   string
	}

	// GetProcessListRequest struct for GetProcessList soap request body.
	GetProcessListRequest struct {
		XMLName xml.Name `xml:"urn:SAPControl GetProcessList"`
	}

	// GetProcessListResponse struct for GetProcessList soap response body.
	GetProcessListResponse struct {
		XMLName xml.Name `xml:"SAPControl GetProcessListResponse"`

		Processes []OSProcess `xml:"process>item,omitempty"`
	}

	// OSProcess struct for GetProcessListResponse soap response body content.
	// More fields can be added according to requirement.
	OSProcess struct {
		Name       string `xml:"name,omitempty"`
		Dispstatus string `xml:"dispstatus,omitempty"`
		Pid        int64  `xml:"pid,omitempty"`
	}
)

// New returns a Client for soap calls supported by all types of sap instances.
// sn: system number for the sap instance.
func New(sn string) Client {
	// sapcontrol listens on the unix domain socket /tmp/.sapstream5#{sn}13 by default
	soap := soap.NewUDSClient(fmt.Sprintf("/tmp/.sapstream5%s13", sn))
	return Client{soap, sn}
}

// call is a syntactic encapsulation for invoking soap.Call function.
func (c Client) call(request, response any) error {
	return c.soap.Call(request, response)
}

// GetProcessList is a method defined on the Client struct. It uses the SAPControl web API
// to build a map describing the statuses of all SAP processes.
// Example Usage:
//
//	sc := New("02")
//	processes, err := sc.GetProcessList()
//
// Returns:
//   - A list of OSProcess structs which is the response from the GetProcessList API call.
//   - Error if Client.call fails, nil otherwise.
func (c Client) GetProcessList() ([]OSProcess, error) {
	res := &GetProcessListResponse{}
	if err := c.call(&GetProcessListRequest{}, res); err != nil {
		log.Logger.Debugw("Failed to get SAP Process Status via API call", log.Error(err))
		return nil, err
	}
	log.Logger.Debugw("Sapcontrol GetProcessList", "API response", res.Processes)

	return res.Processes, nil
}
