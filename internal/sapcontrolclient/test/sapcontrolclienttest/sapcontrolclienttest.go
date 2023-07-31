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

// Package sapcontrolclienttest implements test fakes for the ClientInterface
package sapcontrolclienttest

import "github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"

// Fake is a fake SAPControl client that implements the ClientInterface.
type Fake struct {
	Processes     []sapcontrolclient.OSProcess
	WorkProcesses []sapcontrolclient.WorkProcess
	TaskQueues    []sapcontrolclient.TaskHandlerQueue
	EnqLocks      []sapcontrolclient.EnqLock

	ErrGetProcessList    error
	ErrABAPGetWPTable    error
	ErrGetQueueStatistic error
	ErrEnqGetLockTable   error
}

// GetProcessList a mock that returns map describing the statuses of SAP processes.
func (c Fake) GetProcessList() ([]sapcontrolclient.OSProcess, error) {
	return c.Processes, c.ErrGetProcessList
}

// ABAPGetWPTable is a fake implementation of sapcontrol package ABAPGetWPTable method.
func (c Fake) ABAPGetWPTable() ([]sapcontrolclient.WorkProcess, error) {
	return c.WorkProcesses, c.ErrABAPGetWPTable
}

// GetQueueStatistic is a fake implementation of sapcontrol package GetQueueStatistic method.
func (c Fake) GetQueueStatistic() ([]sapcontrolclient.TaskHandlerQueue, error) {
	return c.TaskQueues, c.ErrGetQueueStatistic
}

// GetEnqLockTable is a fake implementation of sapcontrol package EnqGetLockTable method.
func (c Fake) GetEnqLockTable() ([]sapcontrolclient.EnqLock, error) {
	return c.EnqLocks, c.ErrEnqGetLockTable
}
