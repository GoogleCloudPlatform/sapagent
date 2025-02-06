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

	"github.com/GoogleCloudPlatform/sapagent/internal/soap"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
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
		XMLName   xml.Name    `xml:"SAPControl GetProcessListResponse"`
		Processes []OSProcess `xml:"process>item,omitempty"`
	}

	// OSProcess struct for GetProcessListResponse soap response body content.
	// More fields can be added according to requirement.
	OSProcess struct {
		Name       string `xml:"name,omitempty"`
		Dispstatus string `xml:"dispstatus,omitempty"`
		Pid        int64  `xml:"pid,omitempty"`
	}

	// ABAPGetWPTableRequest struct for ABAPGetWPTable soap request body.
	ABAPGetWPTableRequest struct {
		XMLName xml.Name `xml:"urn:SAPControl ABAPGetWPTable"`
	}

	// ABAPGetWPTableResponse struct for ABAPGetWPTable soap response body.
	ABAPGetWPTableResponse struct {
		XMLName       xml.Name      `xml:"SAPControl ABAPGetWPTableResponse"`
		WorkProcesses []WorkProcess `xml:"workprocess>item"`
	}

	// WorkProcess struct for ABAPGetWPTable soap response body content.
	WorkProcess struct {
		No     int64  `xml:"No,omitempty"`
		Type   string `xml:"Typ,omitempty"`
		Pid    int64  `xml:"Pid,omitempty"`
		Status string `xml:"Status,omitempty"`
		Time   string `xml:"Time,omitempty"`
		User   string `xml:"User,omitempty"`
	}

	// GetQueueStatisticRequest struct for GetQueueStatistic soap request body.
	GetQueueStatisticRequest struct {
		XMLName xml.Name `xml:"urn:SAPControl GetQueueStatistic"`
	}

	// GetQueueStatisticResponse struct for GetQueueStatistic soap response body.
	GetQueueStatisticResponse struct {
		XMLName xml.Name           `xml:"SAPControl GetQueueStatisticResponse"`
		Queues  []TaskHandlerQueue `xml:"queue>item"`
	}

	// TaskHandlerQueue struct for GetQueueStatistic response with valid Now and High entries.
	TaskHandlerQueue struct {
		Type string `xml:"Typ,omitempty"`
		Now  int64  `xml:"Now,omitempty"`
		High int64  `xml:"High,omitempty"`
	}

	// GetEnqLockTableRequest struct for EnqGetLockTable soap request body.
	GetEnqLockTableRequest struct {
		XMLName xml.Name `xml:"urn:SAPControl EnqGetLockTable"`
	}

	// GetEnqLockTableResponse struct for EnqGetLockTable soap response body.
	GetEnqLockTableResponse struct {
		XMLName  xml.Name  `xml:"SAPControl EnqGetLockTableResponse"`
		EnqLocks []EnqLock `xml:"lock>item"`
	}

	// EnqLock struct holds the data about lock details.
	EnqLock struct {
		LockName        string `xml:"lock-name,omitempty"`
		LockArg         string `xml:"lock-arg,omitempty"`
		LockMode        string `xml:"lock-mode,omitempty"`
		Owner           string `xml:"owner,omitempty"`
		OwnerVB         string `xml:"owner-vb,omitempty"`
		UseCountOwner   int64  `xml:"use-count-owner,omitempty"`
		UseCountOwnerVB int64  `xml:"use-count-owner-vb,omitempty"`
		Client          string `xml:"client,omitempty"`
		User            string `xml:"user,omitempty"`
		Transaction     string `xml:"transaction,omitempty"`
		Object          string `xml:"object,omitempty"`
		Backup          string `xml:"backup,omitempty"`
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
// Returns:
//   - A list of OSProcess structs which is the response from the GetProcessList API call.
//   - Error if Client.call fails, nil otherwise.
func (c Client) GetProcessList() ([]OSProcess, error) {
	res := &GetProcessListResponse{}
	if err := c.call(&GetProcessListRequest{}, res); err != nil {
		return nil, err
	}
	log.Logger.Debugw("Sapcontrol GetProcessList", "apiResponse", res.Processes)
	return res.Processes, nil
}

// ABAPGetWPTable performs ABAPGetWPTable soap request.
// Returns:
//   - ABAPGetWPTable API call response as a list of WorkProcess structs.
//   - Error if Client.call fails, nil otherwise.
func (c Client) ABAPGetWPTable() ([]WorkProcess, error) {
	res := &ABAPGetWPTableResponse{}
	if err := c.call(&ABAPGetWPTableRequest{}, res); err != nil {
		return nil, err
	}

	log.Logger.Debugw("Sapcontrol ABAPGetWPTable", "apiResponse", res.WorkProcesses)
	return res.WorkProcesses, nil
}

// GetQueueStatistic performs GetQueueStatistic soap request.
// Returns:
//   - GetQueueStatistic API call response as a list of TaskHandlerQueue structs.
//   - Error if Client.call fails, nil otherwise.
func (c Client) GetQueueStatistic() ([]TaskHandlerQueue, error) {
	res := &GetQueueStatisticResponse{}
	if err := c.call(&GetQueueStatisticRequest{}, res); err != nil {
		return nil, err
	}
	log.Logger.Debugw("Sapcontrol GetQueueStatistic", "apiResponse", res.Queues)
	return res.Queues, nil
}

// GetEnqLockTable  performs EnqGetLockTable soap request.
// Returns:
//   - GetEqnLockTable API call response as a list of EnqLock struct.
//   - Error if Client.call fails, nil otherwise.
func (c Client) GetEnqLockTable() ([]EnqLock, error) {
	res := &GetEnqLockTableResponse{}
	if err := c.call(&GetEnqLockTableRequest{}, res); err != nil {
		return nil, err
	}
	log.Logger.Infow("Sapcontrol GetEnqLockTable", "apiResponse", res.EnqLocks)
	return res.EnqLocks, nil
}
