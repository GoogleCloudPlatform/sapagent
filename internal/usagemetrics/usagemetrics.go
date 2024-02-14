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

// Package usagemetrics provides logging utility for the operational status of Google Cloud Agent for SAP.
package usagemetrics

import um "github.com/GoogleCloudPlatform/sapagent/shared/usagemetrics"

// The following status values are supported.
const (
	StatusRunning       um.Status = "RUNNING"
	StatusStarted       um.Status = "STARTED"
	StatusStopped       um.Status = "STOPPED"
	StatusConfigured    um.Status = "CONFIGURED"
	StatusMisconfigured um.Status = "MISCONFIGURED"
	StatusError         um.Status = "ERROR"
	StatusInstalled     um.Status = "INSTALLED"
	StatusUpdated       um.Status = "UPDATED"
	StatusUninstalled   um.Status = "UNINSTALLED"
	StatusAction        um.Status = "ACTION"
)

// Agent wide error code mappings.
const (
	UnknownError = iota
	CloudPropertiesNotSet
	GCEServiceCreateFailure
	MetricClientCreateFailure
	QueryClientCreateFailure
	BareMetalCloudPropertiesNotSet
	LocalHTTPListenerCreateFailure
	ConfigFileReadFailure
	MalformedConfigFile
	WLMMetricCollectionFailure
	ProcessMetricsMetricClientCreateFailure
	NoSAPInstancesFound
	HANAMonitoringCollectionFailure
	HANAMonitoringConfigReadFailure
	MalformedHANAMonitoringConfigFile
	MalformedDefaultHANAMonitoringQueriesFile
	AgentMetricsServiceCreateFailure
	HeartbeatMonitorCreateFailure
	HeartbeatMonitorRegistrationFailure
	SnapshotDBNotReadyFailure
	DiskSnapshotCreateFailure
	DiskSnapshotFailedDBNotComplete
	DiskSnapshotDoneDBNotComplete
	CollectionDefinitionLoadFailure
	CollectionDefinitionValidateFailure
	WLMServiceCreateFailure
	BackintIncorrectArguments
	BackintMalformedConfigFile
	BackintConfigReadFailure
	BackintBackupFailure
	BackintRestoreFailure
	BackintInquireFailure
	BackintDeleteFailure
	SOSReportCollectionUsageError
	SOSReportCollectionExitFailure
	BackintDiagnoseFailure
	ReadMetricsQueryFailure
	ReadMetricsWriteFileFailure
	ReadMetricsBucketUploadFailure
	InstallBackintFailure
	ReliabilityQueryFailure
	ReliabilityWriteFileFailure
	ReliabilityBucketUploadFailure
	DiscoverSapInstanceFailure
	DiscoverSapSystemFailure
	UsageMetricsDailyLogError
	CollectMetricsRoutineFailure
	SlowMetricsCollectionFailure
	CollectFastMetrcsRoutineFailure
	HeartbeatRoutineFailure
	WLMCollectionRoutineFailure
	HostMetricsHTTPServerRoutineFailure
	HostMetricsCollectionRoutineFailure
	WLMCollectionSystemRoutineFailure
	WLMCollectionHANARoutineFailure
	WLMCollectionNetweaverRoutineFailure
	WLMCollectionPacemakerRoutineFailure
	WLMCollectionCustomRoutineFailure
	AgentMetricsCollectAndSubmitFailure
	RemoteCollectSSHFailure
	RemoteCollectGcloudFailure
	ConfigureBackintFailure
	CollectionDefinitionUpdateRoutineFailure
	HANAMonitoringCreateWorkerPoolFailure
	CollectReliabilityMetricsRoutineFailure
)

// Agent wide action mappings.
const (
	UnknownAction = iota
	CollectWLMMetrics
	CollectHostMetrics
	CollectProcessMetrics
	CollectHANAMonitoringMetrics
	HANADiskSnapshot
	SSLModeOnHANAMonitoring
	BackintRunning
	BackintBackupStarted
	BackintRestoreStarted
	BackintInquireStarted
	BackintDeleteStarted
	BackintBackupFinished
	BackintRestoreFinished
	BackintInquireFinished
	BackintDeleteFinished
	BackintDiagnoseStarted
	BackintDiagnoseFinished
	HANADiskRestore
	ReadMetricsStarted
	ReadMetricsFinished
	InstallBackintStarted
	InstallBackintFinished
	RemoteWLMMetricsCollection
	ReliabilityStarted
	ReliabilityFinished
	ConfigureBackintStarted
	ConfigureBackintFinished
	CollectReliabilityMetrics
)

// projectNumbers contains known project numbers for test instances.
var projectExclusionList = []string{
	"922508251869",
	"155261204042",
	"114837167255",
	"161716815775",
	"607888266690",
	"863817768072",
	"39979408140",
	"510599941441",
	"1038306394601",
	"714149369409",
	"450711760461",
	"600915385160",
	"208472317671",
	"824757391322",
	"977154783768",
	"148036532291",
	"425380551487",
	"811811474621",
	"975534532604",
	"475132212764",
	"201338458013",
	"269972924358",
	"605897091243",
	"1008799658123",
	"916154365516",
	"843031526114",
	"562567328974",
	"894989386931",
	"108988699619",
	"221666662846",
	"625805418481",
	"211003240391",
	"1031874220405",
	"709743230039",
	"597454869213",
	"190577778578",
	"1098359830385",
	"245746275688",
	"776752604875",
	"743463339463",
	"771731819498",
	"124631552650",
	"966841751088",
	"624630633137",
	"624630633137",
}
