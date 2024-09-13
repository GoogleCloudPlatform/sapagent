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

package usagemetrics

import "testing"

func TestErrorConstants(t *testing.T) {
	if UnknownError != 0 {
		t.Errorf("UnknownError = %v, want 0", UnknownError)
	}
	if CloudPropertiesNotSet != 1 {
		t.Errorf("CloudPropertiesNotSet = %v, want 1", CloudPropertiesNotSet)
	}
	if GCEServiceCreateFailure != 2 {
		t.Errorf("GCEServiceCreateFailure = %v, want 2", GCEServiceCreateFailure)
	}
	if MetricClientCreateFailure != 3 {
		t.Errorf("MetricClientCreateFailure = %v, want 3", MetricClientCreateFailure)
	}
	if QueryClientCreateFailure != 4 {
		t.Errorf("QueryClientCreateFailure = %v, want 4", QueryClientCreateFailure)
	}
	if BareMetalCloudPropertiesNotSet != 5 {
		t.Errorf("BareMetalCloudPropertiesNotSet = %v, want 5", BareMetalCloudPropertiesNotSet)
	}
	if LocalHTTPListenerCreateFailure != 6 {
		t.Errorf("LocalHTTPListenerCreateFailure = %v, want 6", LocalHTTPListenerCreateFailure)
	}
	if ConfigFileReadFailure != 7 {
		t.Errorf("ConfigFileReadFailure = %v, want 7", ConfigFileReadFailure)
	}
	if MalformedConfigFile != 8 {
		t.Errorf("MalformedConfigFile = %v, want 8", MalformedConfigFile)
	}
	if WLMMetricCollectionFailure != 9 {
		t.Errorf("WLMMetricCollectionFailure = %v, want 9", WLMMetricCollectionFailure)
	}
	if ProcessMetricsMetricClientCreateFailure != 10 {
		t.Errorf("ProcessMetricsMetricClientCreateFailure = %v, want 10", ProcessMetricsMetricClientCreateFailure)
	}
	if NoSAPInstancesFound != 11 {
		t.Errorf("NoSAPInstancesFound = %v, want 11", NoSAPInstancesFound)
	}
	if HANAMonitoringCollectionFailure != 12 {
		t.Errorf("HANAMonitoringCollectionFailure = %v, want 12", HANAMonitoringCollectionFailure)
	}
	if HANAMonitoringConfigReadFailure != 13 {
		t.Errorf("HANAMonitoringConfigReadFailure = %v, want 13", HANAMonitoringConfigReadFailure)
	}
	if MalformedHANAMonitoringConfigFile != 14 {
		t.Errorf("MalformedHANAMonitoringConfigFile = %v, want 14", MalformedHANAMonitoringConfigFile)
	}
	if MalformedDefaultHANAMonitoringQueriesFile != 15 {
		t.Errorf("MalformedDefaultHANAMonitoringQueriesFile = %v, want 15", MalformedDefaultHANAMonitoringQueriesFile)
	}
	if AgentMetricsServiceCreateFailure != 16 {
		t.Errorf("AgentMetricsServiceCreateFailure = %v, want 16", AgentMetricsServiceCreateFailure)
	}
	if HeartbeatMonitorCreateFailure != 17 {
		t.Errorf("HeartbeatMonitorCreateFailure = %v, want 17", HeartbeatMonitorCreateFailure)
	}
	if HeartbeatMonitorRegistrationFailure != 18 {
		t.Errorf("HeartbeatMonitorRegistrationFailure = %v, want 18", HeartbeatMonitorRegistrationFailure)
	}
	if SnapshotDBNotReadyFailure != 19 {
		t.Errorf("SnapshotDBNotReadyFailure = %v, want 19", SnapshotDBNotReadyFailure)
	}
	if DiskSnapshotCreateFailure != 20 {
		t.Errorf("DiskSnapshotCreateFailure = %v, want 20", DiskSnapshotCreateFailure)
	}
	if DiskSnapshotFailedDBNotComplete != 21 {
		t.Errorf("DiskSnapshotFailedDBNotComplete = %v, want 21", DiskSnapshotFailedDBNotComplete)
	}
	if DiskSnapshotDoneDBNotComplete != 22 {
		t.Errorf("DiskSnapshotDoneDBNotComplete = %v, want 22", DiskSnapshotDoneDBNotComplete)
	}
	if CollectionDefinitionLoadFailure != 23 {
		t.Errorf("CollectionDefinitionLoadFailure = %v, want 23", CollectionDefinitionLoadFailure)
	}
	if CollectionDefinitionValidateFailure != 24 {
		t.Errorf("CollectionDefinitionValidateFailure = %v, want 24", CollectionDefinitionValidateFailure)
	}
	if WLMServiceCreateFailure != 25 {
		t.Errorf("WLMServiceCreateFailure = %v, want 25", WLMServiceCreateFailure)
	}
	if BackintIncorrectArguments != 26 {
		t.Errorf("BackintIncorrectArguments = %v, want 26", BackintIncorrectArguments)
	}
	if BackintMalformedConfigFile != 27 {
		t.Errorf("BackintMalformedConfigFile = %v, want 27", BackintMalformedConfigFile)
	}
	if BackintConfigReadFailure != 28 {
		t.Errorf("BackintConfigReadFailure = %v, want 28", BackintConfigReadFailure)
	}
	if BackintBackupFailure != 29 {
		t.Errorf("BackintBackupFailure = %v, want 29", BackintBackupFailure)
	}
	if BackintRestoreFailure != 30 {
		t.Errorf("BackintRestoreFailure = %v, want 30", BackintRestoreFailure)
	}
	if BackintInquireFailure != 31 {
		t.Errorf("BackintInquireFailure = %v, want 31", BackintInquireFailure)
	}
	if BackintDeleteFailure != 32 {
		t.Errorf("BackintDeleteFailure = %v, want 32", BackintDeleteFailure)
	}
	if SOSReportCollectionUsageError != 33 {
		t.Errorf("SOSReportCollectionUsageError = %v, want 33", SOSReportCollectionUsageError)
	}
	if SOSReportCollectionExitFailure != 34 {
		t.Errorf("SOSReportCollectionExitFailure = %v, want 33", SOSReportCollectionExitFailure)
	}
	if BackintDiagnoseFailure != 35 {
		t.Errorf("BackintDiagnoseFailure = %v, want 35", BackintDiagnoseFailure)
	}
	if ReadMetricsQueryFailure != 36 {
		t.Errorf("ReadMetricsQueryFailure = %v, want 36", ReadMetricsQueryFailure)
	}
	if ReadMetricsWriteFileFailure != 37 {
		t.Errorf("ReadMetricsWriteFileFailure = %v, want 37", ReadMetricsWriteFileFailure)
	}
	if ReadMetricsBucketUploadFailure != 38 {
		t.Errorf("ReadMetricsBucketUploadFailure = %v, want 38", ReadMetricsBucketUploadFailure)
	}
	if InstallBackintFailure != 39 {
		t.Errorf("InstallBackintFailure = %v, want 39", InstallBackintFailure)
	}
	if ReliabilityQueryFailure != 40 {
		t.Errorf("ReliabilityQueryFailure = %v, want 40", ReliabilityQueryFailure)
	}
	if ReliabilityWriteFileFailure != 41 {
		t.Errorf("ReliabilityWriteFileFailure = %v, want 41", ReliabilityWriteFileFailure)
	}
	if ReliabilityBucketUploadFailure != 42 {
		t.Errorf("ReliabilityBucketUploadFailure = %v, want 42", ReliabilityBucketUploadFailure)
	}
	if DiscoverSapInstanceFailure != 43 {
		t.Errorf("DiscoverSapInstanceFailure = %v, want 43", DiscoverSapInstanceFailure)
	}
	if DiscoverSapSystemFailure != 44 {
		t.Errorf("DiscoverSapSystemFailure = %v, want 44", DiscoverSapSystemFailure)
	}
	if UsageMetricsDailyLogError != 45 {
		t.Errorf("UsageMetricsDailyLogError = %v, want 45", UsageMetricsDailyLogError)
	}
	if CollectMetricsRoutineFailure != 46 {
		t.Errorf("CollectMetricsRoutineFailure = %v, want 46", CollectMetricsRoutineFailure)
	}
	if SlowMetricsCollectionFailure != 47 {
		t.Errorf("SlowMetricsCollectionFailure = %v, want 47", SlowMetricsCollectionFailure)
	}
	if CollectFastMetrcsRoutineFailure != 48 {
		t.Errorf("CollectFastMetrcsRoutineFailure = %v, want 48", CollectFastMetrcsRoutineFailure)
	}
	if HeartbeatRoutineFailure != 49 {
		t.Errorf("HeartbeatRoutineFailure = %v, want 49", HeartbeatRoutineFailure)
	}
	if WLMCollectionRoutineFailure != 50 {
		t.Errorf("WLMCollectionRoutineFailure = %v, want 50", WLMCollectionRoutineFailure)
	}
	if HostMetricsHTTPServerRoutineFailure != 51 {
		t.Errorf("HostMetricsHTTPServerRoutineFailure = %v, want 51", HostMetricsHTTPServerRoutineFailure)
	}
	if HostMetricsCollectionRoutineFailure != 52 {
		t.Errorf("HostMetricsCollectionRoutineFailure = %v, want 52", HostMetricsCollectionRoutineFailure)
	}
	if WLMCollectionSystemRoutineFailure != 53 {
		t.Errorf("WLMCollectionSystemRoutineFailure = %v, want 53", WLMCollectionSystemRoutineFailure)
	}
	if WLMCollectionHANARoutineFailure != 54 {
		t.Errorf("WLMCollectionHANARoutineFailure = %v, want 54", WLMCollectionHANARoutineFailure)
	}
	if WLMCollectionNetweaverRoutineFailure != 55 {
		t.Errorf("WLMCollectionNetweaverRoutineFailure = %v, want 55", WLMCollectionNetweaverRoutineFailure)
	}
	if WLMCollectionPacemakerRoutineFailure != 56 {
		t.Errorf("WLMCollectionPacemakerRoutineFailure = %v, want 56", WLMCollectionPacemakerRoutineFailure)
	}
	if WLMCollectionCustomRoutineFailure != 57 {
		t.Errorf("WLMCollectionCustomRoutineFailure = %v, want 57", WLMCollectionCustomRoutineFailure)
	}
	if AgentMetricsCollectAndSubmitFailure != 58 {
		t.Errorf("AgentMetricsCollectAndSubmitFailure = %v, want 58", AgentMetricsCollectAndSubmitFailure)
	}
	if RemoteCollectSSHFailure != 59 {
		t.Errorf("RemoteCollectSSHFailure = %v, want 59", RemoteCollectSSHFailure)
	}
	if RemoteCollectGcloudFailure != 60 {
		t.Errorf("RemoteCollectGcloudFailure = %v, want 60", RemoteCollectGcloudFailure)
	}
	if ConfigureBackintFailure != 61 {
		t.Errorf("ConfigureBackintFailure = %v, want 61", ConfigureBackintFailure)
	}
	if CollectionDefinitionUpdateRoutineFailure != 62 {
		t.Errorf("CollectionDefinitionUpdateRoutineFailure = %v, want 62", CollectionDefinitionUpdateRoutineFailure)
	}
	if HANAMonitoringCreateWorkerPoolFailure != 63 {
		t.Errorf("HANAMonitoringCreateWorkerPoolFailure = %v, want 63", HANAMonitoringCreateWorkerPoolFailure)
	}
	if CollectReliabilityMetricsRoutineFailure != 64 {
		t.Errorf("CollectReliabilityMetricsRoutineFailure = %v, want 64", CollectReliabilityMetricsRoutineFailure)
	}
	if ConfigureInstanceFailure != 65 {
		t.Errorf("ConfigureInstanceFailure = %v, want 65", ConfigureInstanceFailure)
	}
	if EncryptedDiskSnapshotFailure != 66 {
		t.Errorf("EncryptedDiskSnapshotFailure = %v, want 66", EncryptedDiskSnapshotFailure)
	}
	if EncryptedSnapshotRestoreFailure != 67 {
		t.Errorf("EncryptedSnapshotRestoreFailure = %v, want 67", EncryptedSnapshotRestoreFailure)
	}
	if GuestActionsFailure != 68 {
		t.Errorf("GuestActionsFailure = %v, want 68", GuestActionsFailure)
	}
	if PerformanceDiagnosticsFailure != 69 {
		t.Errorf("PerformanceDiagnosticsFailure = %v, want 69", PerformanceDiagnosticsFailure)
	}
	if PerformanceDiagnosticsConfigureInstanceFailure != 70 {
		t.Errorf("PerformanceDiagnosticsConfigureInstanceFailure = %v, want 70", PerformanceDiagnosticsConfigureInstanceFailure)
	}
	if PerformanceDiagnosticsBackupFailure != 71 {
		t.Errorf("PerformanceDiagnosticsBackupFailure = %v, want 71", PerformanceDiagnosticsBackupFailure)
	}
	if PerformanceDiagnosticsFIOFailure != 72 {
		t.Errorf("PerformanceDiagnosticsFIOFailure = %v, want 72", PerformanceDiagnosticsFIOFailure)
	}
	if BalanceIRQFailure != 73 {
		t.Errorf("BalanceIRQFailure = %v, want 73", BalanceIRQFailure)
	}
	if HDBUserstoreKeyFailure != 74 {
		t.Errorf("HDBUserstoreKeyFailure = %v, want 74", HDBUserstoreKeyFailure)
	}
	if ServiceDisableFailure != 75 {
		t.Errorf("ServiceDisableFailure = %v, want 75", ServiceDisableFailure)
	}
	if ServiceEnableFailure != 76 {
		t.Errorf("ServiceEnableFailure = %v, want 76", ServiceEnableFailure)
	}
	if GCBDRBackupFailure != 77 {
		t.Errorf("GCBDRBackupFailure = %v, want 77", GCBDRBackupFailure)
	}
	if GCBDRDiscoveryFailure != 78 {
		t.Errorf("GCBDRDiscoveryFailure = %v, want 78", GCBDRDiscoveryFailure)
	}
	if HANAInsightsOTEFailure != 79 {
		t.Errorf("HANAInsightsOTEFailure = %v, want 79", HANAInsightsOTEFailure)
	}
}

func TestActionConstants(t *testing.T) {
	if UnknownAction != 0 {
		t.Errorf("UnknownAction = %v, want 0", UnknownAction)
	}
	if CollectWLMMetrics != 1 {
		t.Errorf("CollectWLMMetrics = %v, want 1", CollectWLMMetrics)
	}
	if CollectHostMetrics != 2 {
		t.Errorf("CollectHostMetrics = %v, want 2", CollectHostMetrics)
	}
	if CollectProcessMetrics != 3 {
		t.Errorf("CollectProcessMetrics = %v, want 3", CollectProcessMetrics)
	}
	if CollectHANAMonitoringMetrics != 4 {
		t.Errorf("CollectHANAMonitoringMetrics = %v, want 4", CollectHANAMonitoringMetrics)
	}
	if HANADiskSnapshot != 5 {
		t.Errorf("HANADiskSnapshot = %v, want 5", HANADiskSnapshot)
	}
	if SSLModeOnHANAMonitoring != 6 {
		t.Errorf("SSLModeOnHANAMonitoring = %v, want 6", SSLModeOnHANAMonitoring)
	}
	if BackintRunning != 7 {
		t.Errorf("BackintRunning = %v, want 7", BackintRunning)
	}
	if BackintBackupStarted != 8 {
		t.Errorf("BackintBackupStarted = %v, want 8", BackintBackupStarted)
	}
	if BackintRestoreStarted != 9 {
		t.Errorf("BackintRestoreStarted = %v, want 9", BackintRestoreStarted)
	}
	if BackintInquireStarted != 10 {
		t.Errorf("BackintInquireStarted = %v, want 10", BackintInquireStarted)
	}
	if BackintDeleteStarted != 11 {
		t.Errorf("BackintDeleteStarted = %v, want 11", BackintDeleteStarted)
	}
	if BackintBackupFinished != 12 {
		t.Errorf("	BackintBackupFinished = %v, want 12", BackintBackupFinished)
	}
	if BackintRestoreFinished != 13 {
		t.Errorf("BackintRestoreFinished = %v, want 13", BackintRestoreFinished)
	}
	if BackintInquireFinished != 14 {
		t.Errorf("BackintInquireFinished = %v, want 14", BackintInquireFinished)
	}
	if BackintDeleteFinished != 15 {
		t.Errorf("BackintDeleteFinished = %v, want 15", BackintDeleteFinished)
	}
	if BackintDiagnoseStarted != 16 {
		t.Errorf("BackintDiagnoseStarted = %v, want 16", BackintDiagnoseStarted)
	}
	if BackintDiagnoseFinished != 17 {
		t.Errorf("BackintDiagnoseFinished = %v, want 17", BackintDiagnoseFinished)
	}
	if HANADiskRestore != 18 {
		t.Errorf("HANADiskRestore = %v, want 18", HANADiskRestore)
	}
	if ReadMetricsStarted != 19 {
		t.Errorf("ReadMetricsStarted = %v, want 19", ReadMetricsStarted)
	}
	if ReadMetricsFinished != 20 {
		t.Errorf("ReadMetricsFinished = %v, want 20", ReadMetricsFinished)
	}
	if InstallBackintStarted != 21 {
		t.Errorf("InstallBackintStarted = %v, want 21", InstallBackintStarted)
	}
	if InstallBackintFinished != 22 {
		t.Errorf("InstallBackintFinished = %v, want 22", InstallBackintFinished)
	}
	if RemoteWLMMetricsCollection != 23 {
		t.Errorf("RemoteWLMMetricsCollection = %v, want 23", RemoteWLMMetricsCollection)
	}
	if ReliabilityStarted != 24 {
		t.Errorf("ReliabilityStarted = %v, want 24", ReliabilityStarted)
	}
	if ReliabilityFinished != 25 {
		t.Errorf("ReliabilityFinished = %v, want 25", ReliabilityFinished)
	}
	if ConfigureBackintStarted != 26 {
		t.Errorf("ConfigureBackintStarted = %v, want 26", ConfigureBackintStarted)
	}
	if ConfigureBackintFinished != 27 {
		t.Errorf("ConfigureBackintFinished = %v, want 27", ConfigureBackintFinished)
	}
	if CollectReliabilityMetrics != 28 {
		t.Errorf("CollectReliabilityMetrics = %v, want 28", CollectReliabilityMetrics)
	}
	if ConfigureInstanceStarted != 29 {
		t.Errorf("ConfigureInstanceStarted = %v, want 29", ConfigureInstanceStarted)
	}
	if ConfigureInstanceFinished != 30 {
		t.Errorf("ConfigureInstanceFinished = %v, want 30", ConfigureInstanceFinished)
	}
	if EncryptedDiskSnapshot != 31 {
		t.Errorf("EncryptedDiskSnapshot = %v, want 31", EncryptedDiskSnapshot)
	}
	if EncryptedSnapshotRestore != 32 {
		t.Errorf("EncryptedSnapshotRestore = %v, want 32", EncryptedSnapshotRestore)
	}
	if GuestActionsStarted != 33 {
		t.Errorf("GuestActionsStarted = %v, want 33", GuestActionsStarted)
	}
	if PerformanceDiagnostics != 34 {
		t.Errorf("PerformanceDiagnostics = %v, want 34", PerformanceDiagnostics)
	}
	if PerformanceDiagnosticsConfigureInstance != 35 {
		t.Errorf("PerformanceDiagnosticsConfigureInstance = %v, want 35", PerformanceDiagnosticsConfigureInstance)
	}
	if PerformanceDiagnosticsBackup != 36 {
		t.Errorf("PerformanceDiagnosticsBackup = %v, want 36", PerformanceDiagnosticsBackup)
	}
	if PerformanceDiagnosticsFIO != 37 {
		t.Errorf("PerformanceDiagnosticsFIO = %v, want 37", PerformanceDiagnosticsFIO)
	}
	if ConfigureInstanceCheckFinished != 38 {
		t.Errorf("ConfigureInstanceCheckFinished = %v, want 38", ConfigureInstanceCheckFinished)
	}
	if ConfigureInstanceApplyFinished != 39 {
		t.Errorf("ConfigureInstanceApplyFinished = %v, want 39", ConfigureInstanceApplyFinished)
	}
	if ReliabilityHANAAvailable != 40 {
		t.Errorf("ReliabilityHANAAvailable = %v, want 40", ReliabilityHANAAvailable)
	}
	if ReliabilityHANANotAvailable != 41 {
		t.Errorf("ReliabilityHANANotAvailable = %v, want 41", ReliabilityHANANotAvailable)
	}
	if ReliabilityHANAHAAvailable != 42 {
		t.Errorf("ReliabilityHANAHAAvailable = %v, want 42", ReliabilityHANAHAAvailable)
	}
	if ReliabilityHANAHANotAvailable != 43 {
		t.Errorf("ReliabilityHANAHANotAvailable = %v, want 43", ReliabilityHANAHANotAvailable)
	}
	if ReliabilitySAPNWAvailable != 44 {
		t.Errorf("ReliabilitySAPNWAvailable = %v, want 44", ReliabilitySAPNWAvailable)
	}
	if ReliabilitySAPNWNotAvailable != 45 {
		t.Errorf("ReliabilitySAPNWNotAvailable = %v, want 45", ReliabilitySAPNWNotAvailable)
	}
	if BalanceIRQStarted != 46 {
		t.Errorf("BalanceIRQStarted = %v, want 46", BalanceIRQStarted)
	}
	if BalanceIRQFinished != 47 {
		t.Errorf("BalanceIRQFinished = %v, want 47", BalanceIRQFinished)
	}
	if BalanceIRQInstallStarted != 48 {
		t.Errorf("BalanceIRQInstallStarted = %v, want 48", BalanceIRQInstallStarted)
	}
	if BalanceIRQInstallFinished != 49 {
		t.Errorf("BalanceIRQInstallFinished = %v, want 49", BalanceIRQInstallFinished)
	}
	if HDBUserstoreKeyConfigured != 50 {
		t.Errorf("HDBUserstoreKeyConfigured = %v, want 50", HDBUserstoreKeyConfigured)
	}
	if HANADiskSnapshotUserstoreKey != 51 {
		t.Errorf("HANADiskSnapshotUserstoreKey = %v, want 51", HANADiskSnapshotUserstoreKey)
	}
	if HANAInsightsOTEUserstoreKey != 52 {
		t.Errorf("HANAInsightsOTEUserstoreKey = %v, want 52", HANAInsightsOTEUserstoreKey)
	}
	if BackintRecoveryParameterEnabled != 53 {
		t.Errorf("BackintRecoveryParameterEnabled = %v, want 53", BackintRecoveryParameterEnabled)
	}
	if ServiceDisableStarted != 54 {
		t.Errorf("ServiceDisableStarted = %v, want 54", ServiceDisableStarted)
	}
	if ServiceDisableFinished != 55 {
		t.Errorf("ServiceDisableFinished = %v, want 55", ServiceDisableFinished)
	}
	if ServiceEnableStarted != 56 {
		t.Errorf("ServiceEnableStarted = %v, want 56", ServiceEnableStarted)
	}
	if ServiceEnableFinished != 57 {
		t.Errorf("ServiceEnableFinished = %v, want 57", ServiceEnableFinished)
	}
	if UAPShellCommand != 58 {
		t.Errorf("UAPShellCommand = %v, want 58", UAPShellCommand)
	}
	if UAPBackintCommand != 59 {
		t.Errorf("UAPBackintCommand = %v, want 59", UAPBackintCommand)
	}
	if UAPConfigureCommand != 60 {
		t.Errorf("UAPConfigureCommand = %v, want 60", UAPConfigureCommand)
	}
	if UAPConfigureInstanceCommand != 61 {
		t.Errorf("UAPConfigureInstanceCommand = %v, want 61", UAPConfigureInstanceCommand)
	}
	if UAPGCBDRBackupCommand != 62 {
		t.Errorf("UAPGCBDRBackupCommand = %v, want 62", UAPGCBDRBackupCommand)
	}
	if UAPGCBDRDiscoveryCommand != 63 {
		t.Errorf("UAPGCBDRDiscoveryCommand = %v, want 63", UAPGCBDRDiscoveryCommand)
	}
	if UAPHANADiskBackupCommand != 64 {
		t.Errorf("UAPHANADiskBackupCommand = %v, want 64", UAPHANADiskBackupCommand)
	}
	if UAPPerformanceDiagnosticsCommand != 65 {
		t.Errorf("UAPPerformanceDiagnosticsCommand = %v, want 65", UAPPerformanceDiagnosticsCommand)
	}
	if UAPSupportBundleCommand != 66 {
		t.Errorf("UAPSupportBundleCommand = %v, want 66", UAPSupportBundleCommand)
	}
	if UAPVersionCommand != 67 {
		t.Errorf("UAPVersionCommand = %v, want 67", UAPVersionCommand)
	}
	if GCBDRBackupStarted != 68 {
		t.Errorf("GCBDRBackupStarted = %v, want 68", GCBDRBackupStarted)
	}
	if GCBDRBackupFinished != 69 {
		t.Errorf("GCBDRBackupFinished = %v, want 69", GCBDRBackupFinished)
	}
	if HANADiskGroupBackupStarted != 70 {
		t.Errorf("HANADiskGroupBackupStarted = %v, want 70", HANADiskGroupBackupStarted)
	}
	if HANADiskGroupBackupSucceeded != 71 {
		t.Errorf("HANADiskGroupBackupSucceeded = %v, want 71", HANADiskGroupBackupSucceeded)
	}
	if HANADiskBackupSucceeded != 72 {
		t.Errorf("HANADiskBackupSucceeded = %v, want 72", HANADiskBackupSucceeded)
	}
	if HANADiskGroupRestoreStarted != 73 {
		t.Errorf("HANADiskGroupRestoreStarted = %v, want 73", HANADiskGroupRestoreStarted)
	}
	if HANADiskGroupRestoreSucceeded != 74 {
		t.Errorf("HANADiskGroupRestoreSucceeded = %v, want 74", HANADiskGroupRestoreSucceeded)
	}
	if HANADiskRestoreSucceeded != 75 {
		t.Errorf("HANADiskRestoreSucceeded = %v, want 75", HANADiskRestoreSucceeded)
	}
	if ConfigPollerStarted != 76 {
		t.Errorf("ConfigPollerStarted = %v, want 76", ConfigPollerStarted)
	}
	if GCBDRDiscoveryStarted != 77 {
		t.Errorf("GCBDRDiscoveryStarted = %v, want 77", GCBDRDiscoveryStarted)
	}
	if GCBDRDiscoveryFinished != 78 {
		t.Errorf("GCBDRDiscoveryFinished = %v, want 78", GCBDRDiscoveryFinished)
	}
	if HANAInsightsOTEStarted != 79 {
		t.Errorf("HANAInsightsOTEStarted = %v, want 79", HANAInsightsOTEStarted)
	}
	if HANAInsightsOTEFinished != 80 {
		t.Errorf("HANAInsightsOTEFinished = %v, want 80", HANAInsightsOTEFinished)
	}
}
