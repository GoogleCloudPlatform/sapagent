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


// Agent wide error code mappings - Only append the error codes at the end of the list.
// Existing codes should not be modified. New codes should tested in the unit tests.
// Make sure to update the id mapping in this sheet: go/sap-core-eng-tool-mapping.
const (
	UnknownError                                   = 0  //	Unknown error
	CloudPropertiesNotSet                          = 1  // 	Cloud properties are not set, cannot start services.
	GCEServiceCreateFailure                        = 2  //	Failed to create GCE service
	MetricClientCreateFailure                      = 3  //	Failed to create Cloud Monitoring metric client
	QueryClientCreateFailure                       = 4  //	Failed to create Cloud Monitoring query client
	BareMetalCloudPropertiesNotSet                 = 5  //	Bare metal instance detected without cloud properties set.
	LocalHTTPListenerCreateFailure                 = 6  //	Could not start HTTP server on localhost:18181
	ConfigFileReadFailure                          = 7  //	Could not read from configuration file
	MalformedConfigFile                            = 8  //	Invalid content in the configuration file
	WLMMetricCollectionFailure                     = 9  //	Workload metrics collection failure
	ProcessMetricsMetricClientCreateFailure        = 10 //	Process metrics metric client creation failed
	NoSAPInstancesFound                            = 11 //	NO SAP instances found
	HANAMonitoringCollectionFailure                = 12 //	HANA Monitoring collection failure
	HANAMonitoringConfigReadFailure                = 13 //	HANA Monitoring Config File Read Failure
	MalformedHANAMonitoringConfigFile              = 14 //	Malformed HANA Monitoring Config File
	MalformedDefaultHANAMonitoringQueriesFile      = 15 //	Malformed Default HANA Monitoring Queries File
	AgentMetricsServiceCreateFailure               = 16 //	Failed to create agent metrics service
	HeartbeatMonitorCreateFailure                  = 17 //	Failed to create heartbeat monitor
	HeartbeatMonitorRegistrationFailure            = 18 //	Failed to register service with the heartbeat monitor
	SnapshotDBNotReadyFailure                      = 19 //	SnapshotDBNotReadyFailure
	DiskSnapshotCreateFailure                      = 20 //	DiskSnapshotCreateFailure
	DiskSnapshotFailedDBNotComplete                = 21 //	DiskSnapshotFailedDBNotComplete
	DiskSnapshotDoneDBNotComplete                  = 22 //	DiskSnapshotDoneDBNotComplete
	CollectionDefinitionLoadFailure                = 23 //	Failed to load collection definition
	CollectionDefinitionValidateFailure            = 24 //	Failed to validate collection definition
	WLMServiceCreateFailure                        = 25 //	Failed to create WLM service
	BackintIncorrectArguments                      = 26 //	Incorrect CLI arguments for Backint
	BackintMalformedConfigFile                     = 27 //	Malformed Backint configuration file
	BackintConfigReadFailure                       = 28 //	Backint configuration file read error
	BackintBackupFailure                           = 29 //	Backint error during Backup function
	BackintRestoreFailure                          = 30 //	Backint error during Restore function
	BackintInquireFailure                          = 31 //	Backint error during Inquire function
	BackintDeleteFailure                           = 32 //	Backint error during Delete function
	SOSReportCollectionUsageError                  = 33 //	SOS Report collection usage error - invalid params
	SOSReportCollectionExitFailure                 = 34 //	SOS Report collection exit failure
	BackintDiagnoseFailure                         = 35 //	Backint error during Diagnose function
	ReadMetricsQueryFailure                        = 36 //	ReadMetrics error during querying Cloud Monitoring
	ReadMetricsWriteFileFailure                    = 37 //	ReadMetrics error during writing results to local filesystem
	ReadMetricsBucketUploadFailure                 = 38 //	ReadMetrics error during uploading results to GCS bucket
	InstallBackintFailure                          = 39 //	InstallBackint error during installation/migration
	ReliabilityQueryFailure                        = 40 //	Reliability error during querying Cloud Monitoring
	ReliabilityWriteFileFailure                    = 41 //	Reliability error during writing results to local filesystem
	ReliabilityBucketUploadFailure                 = 42 //	Reliability error during uploading results to GCS bucket
	DiscoverSapInstanceFailure                     = 43 //	Panic during Discover SAP Instances routine
	DiscoverSapSystemFailure                       = 44 //	Panic during discover SAP Systems routine
	UsageMetricsDailyLogError                      = 45 //	Panic during report daily usage routine
	CollectMetricsRoutineFailure                   = 46 //	Panic during collect metrics routine
	SlowMetricsCollectionFailure                   = 47 //	Panic during SlowMetricsCollection routine
	CollectFastMetrcsRoutineFailure                = 48 //	Panic during CollectFastMetrcs routine
	HeartbeatRoutineFailure                        = 49 //	panic during Heartbeat routine
	WLMCollectionRoutineFailure                    = 50 //	Panic during WLM metrics Collection routine
	HostMetricsHTTPServerRoutineFailure            = 51 //	Panic during HostMetricsHTTPServer routine
	HostMetricsCollectionRoutineFailure            = 52 //	Panic during HostMetricsCollection routine
	WLMCollectionSystemRoutineFailure              = 53 //	Panic during WLMCollectionSystem routine
	WLMCollectionHANARoutineFailure                = 54 //	Panic during WLMCollectionHANA routine
	WLMCollectionNetweaverRoutineFailure           = 55 //	Panic during WLMCollectionNetweaver routine
	WLMCollectionPacemakerRoutineFailure           = 56 //	Panic during WLMCollectionPacemaker routine
	WLMCollectionCustomRoutineFailure              = 57 //	Panic during WLMCollectionCustom routine
	AgentMetricsCollectAndSubmitFailure            = 58 //	AgentMetricsCollectAndSubmitFailure
	RemoteCollectSSHFailure                        = 59 //	RemoteCollectSSHFailure
	RemoteCollectGcloudFailure                     = 60 //	RemoteCollectGcloudFailure
	ConfigureBackintFailure                        = 61 //	ConfigureBackint error during configuration/migration
	CollectionDefinitionUpdateRoutineFailure       = 62 //	CollectionDefinitionUpdateRoutineFailure
	HANAMonitoringCreateWorkerPoolFailure          = 63 //	HANAMonitoringCreateWorkerPoolFailure
	CollectReliabilityMetricsRoutineFailure        = 64 //	CollectReliabilityMetricsRoutineFailure
	ConfigureInstanceFailure                       = 65 //	ConfigureInstance Failure
	EncryptedDiskSnapshotFailure                   = 66 //	EncryptedDiskSnapshotFailure
	EncryptedSnapshotRestoreFailure                = 67 //	EncryptedSnapshotRestoreFailure
	GuestActionsFailure                            = 68 //	GuestActionsFailure
	PerformanceDiagnosticsFailure                  = 69 //	PerformanceDiagnosticsFailure
	PerformanceDiagnosticsConfigureInstanceFailure = 70 //	PerformanceDiagnosticsConfigureInstanceFailure
	PerformanceDiagnosticsBackupFailure            = 71 //	PerformanceDiagnosticsBackupFailure
	PerformanceDiagnosticsFIOFailure               = 72 //	PerformanceDiagnosticsFIOFailure
	BalanceIRQFailure                              = 73 //	BalanceIRQFailure
	HDBUserstoreKeyFailure                         = 74 //	HDBUserstoreKeyQueryFailure
	ServiceDisableFailure                          = 75 //	ServiceDisableFailure
	ServiceEnableFailure                           = 76 //	ServiceEnableFailure
	GCBDRBackupFailure                             = 77 //	GCBDRBackupFailure
)

// Agent wide action mappings - Only append the action codes at the end of the list.
// Existing codes should not be modified. New codes should be tested in the unit tests.
// Make sure to update the id mapping in this sheet: go/sap-core-eng-tool-mapping.
const (
	UnknownAction                           = 0  //	Unknown
	CollectWLMMetrics                       = 1  //	Collecting WLM metrics
	CollectHostMetrics                      = 2  //	Collecting SAP host metrics
	CollectProcessMetrics                   = 3  //	Collecting process metrics
	CollectHANAMonitoringMetrics            = 4  //	Collect HANA Monitoring Metrics
	HANADiskSnapshot                        = 5  //	Run HANA Disk Snapshot OTE
	SSLModeOnHANAMonitoring                 = 6  //	SSL Mode On HANA Monitoring
	BackintRunning                          = 7  //	Backint running
	BackintBackupStarted                    = 8  //	Backint Backup started
	BackintRestoreStarted                   = 9  //	Backint Restore started
	BackintInquireStarted                   = 10 //	Backint Inquire started
	BackintDeleteStarted                    = 11 //	Backint Delete started
	BackintBackupFinished                   = 12 //	Backint Backup finished
	BackintRestoreFinished                  = 13 //	Backint Restore finished
	BackintInquireFinished                  = 14 //	Backint Inquire finished
	BackintDeleteFinished                   = 15 //	Backint Delete finished
	BackintDiagnoseStarted                  = 16 //	Backint Diagnose started
	BackintDiagnoseFinished                 = 17 //	Backint Diagnose finished
	HANADiskRestore                         = 18 //	Run HANA Disk Restore OTE
	ReadMetricsStarted                      = 19 //	ReadMetrics started
	ReadMetricsFinished                     = 20 //	ReadMetrics finished
	InstallBackintStarted                   = 21 //	InstallBackint started
	InstallBackintFinished                  = 22 //	InstallBackint finished
	RemoteWLMMetricsCollection              = 23 //	Remote WLM Metrics Collection
	ReliabilityStarted                      = 24 //	Reliability Started
	ReliabilityFinished                     = 25 //	Reliability Finished
	ConfigureBackintStarted                 = 26 //	ConfigureBackintStarted
	ConfigureBackintFinished                = 27 //	ConfigureBackintFinished
	CollectReliabilityMetrics               = 28 //	CollectReliabilityMetrics
	ConfigureInstanceStarted                = 29 //	ConfigureInstanceStarted
	ConfigureInstanceFinished               = 30 //	ConfigureInstanceFinished
	EncryptedDiskSnapshot                   = 31 //	EncryptedDiskSnapshot
	EncryptedSnapshotRestore                = 32 //	EncryptedSnapshotRestore
	GuestActionsStarted                     = 33 //	GuestActionsStarted
	PerformanceDiagnostics                  = 34 //	PerformanceDiagnostics
	PerformanceDiagnosticsConfigureInstance = 35 //	PerformanceDiagnosticsConfigureInstance
	PerformanceDiagnosticsBackup            = 36 //	PerformanceDiagnosticsBackup
	PerformanceDiagnosticsFIO               = 37 //	PerformanceDiagnosticsFIO
	ConfigureInstanceCheckFinished          = 38 //	ConfigureInstanceCheckFinished
	ConfigureInstanceApplyFinished          = 39 //	ConfigureInstanceApplyFinished
	ReliabilityHANAAvailable                = 40 //	ReliabilityHANAAvailable
	ReliabilityHANANotAvailable             = 41 //	ReliabilityHANANotAvailable
	ReliabilityHANAHAAvailable              = 42 //	ReliabilityHANAHAAvailable
	ReliabilityHANAHANotAvailable           = 43 //	ReliabilityHANAHANotAvailable
	ReliabilitySAPNWAvailable               = 44 //	ReliabilityHANANWAvailable
	ReliabilitySAPNWNotAvailable            = 45 //	ReliabilitySAPNWNotAvailable
	BalanceIRQStarted                       = 46 //	BalanceIRQStarted
	BalanceIRQFinished                      = 47 //	BalanceIRQFinished
	BalanceIRQInstallStarted                = 48 //	BalanceIRQInstallStarted
	BalanceIRQInstallFinished               = 49 //	BalanceIRQInstallFinished
	HDBUserstoreKeyConfigured               = 50 //	HDBUserstoreKeyConfigured
	HANADiskSnapshotUserstoreKey            = 51 //	HANADiskSnapshotUserstoreKey
	HANAInsightsOTEUserstoreKey             = 52 //	HANAInsightsOTEUserstoreKey
	BackintRecoveryParameterEnabled         = 53 //	BackintRecoveryParameterEnabled
	ServiceDisableStarted                   = 54 //	ServiceDisableStarted
	ServiceDisableFinished                  = 55 //	ServiceDisableFinished
	ServiceEnableStarted                    = 56 //	ServiceEnableStarted
	ServiceEnableFinished                   = 57 //	ServiceEnableFinished
	UAPShellCommand                         = 58 //	UAPShellCommand
	UAPBackintCommand                       = 59 //	UAPBackintCommand
	UAPConfigureCommand                     = 60 //	UAPConfigureCommand
	UAPConfigureInstanceCommand             = 61 //	UAPConfigureInstanceCommand
	UAPGCBDRBackupCommand                   = 62 //	UAPGCBDRBackupCommand
	UAPGCBDRDiscoveryCommand                = 63 //	UAPGCBDRDiscoveryCommand
	UAPHANADiskBackupCommand                = 64 //	UAPHANADiskBackupCommand
	UAPPerformanceDiagnosticsCommand        = 65 //	UAPPerformanceDiagnosticsCommand
	UAPSupportBundleCommand                 = 66 //	UAPSupportBundleCommand
	UAPVersionCommand                       = 67 //	UAPVersionCommand
	GCBDRBackupStarted                      = 68 //	GCBDRBackupRunning
	GCBDRBackupFinished                     = 69 //	GCBDRBackupFinished
)

// LINT.ThenChange("//depot/github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics/usagemetrics_test.go")
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
	"895652539525",
	"365534300350",
}
