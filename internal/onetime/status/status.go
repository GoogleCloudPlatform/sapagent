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

// Package status implements the status subcommand to provide information
// on the agent, configuration, IAM and functional statuses.
package status

import (
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"flag"
	store "cloud.google.com/go/storage"
	"github.com/google/subcommands"
	backintconfiguration "github.com/GoogleCloudPlatform/sapagent/internal/backint/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/iam"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/iam"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/statushelper"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/storage"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/status"
)

const (
	agentPackageName        = "google-cloud-sap-agent"
	agentRepoName           = "google-cloud-sap-agent"
	fetchLatestVersionError = "Error: could not fetch latest version"
	// TODO: Implement status OTE check for WIF based authentications access to scopes.
	requiredScope       = "https://www.googleapis.com/auth/cloud-platform"
	hostMetricsEndpoint = "http://localhost:18181"
	// Below outline the names of the services as defined in the iam permissions file.
	hanaMonitoringLabel       = "HANA_MONITORING"
	processMetricsLabel       = "PROCESS_METRICS"
	cloudLoggingLabel         = "CLOUD_LOGGING"
	hostMetricsLabel          = "HOST_METRICS"
	backintLabel              = "BACKINT"
	backintMultipartLabel     = "BACKINT_MULTIPART"
	diskBackupLabel           = "DISKBACKUP"
	diskBackupStripedLabel    = "DISKBACKUP_STRIPED"
	systemDiscoveryLabel      = "SAP_SYSTEM_DISCOVERY"
	workloadEvaluationMELabel = "WORKLOAD_EVALUATION_METRICS"
	secretManagerLabel        = "SECRET_MANAGER"
	agentMetricsLabel         = "AGENT_HEALTH_METRICS"
)

type (
	// IAMService is an interface for the IAM service.
	IAMService interface {
		CheckIAMPermissionsOnBucket(ctx context.Context, bucketName string, permissions []string) ([]string, error)
		CheckIAMPermissionsOnDisk(ctx context.Context, project, zone, diskName string, permissions []string) ([]string, error)
		CheckIAMPermissionsOnInstance(ctx context.Context, project, zone, instanceName string, permissions []string) ([]string, error)
		CheckIAMPermissionsOnProject(ctx context.Context, projectID string, permissions []string) ([]string, error)
		CheckIAMPermissionsOnSecret(ctx context.Context, projectID, secretID string, permissions []string) ([]string, error)
	}

	httpGetter  func(url string) (resp *http.Response, err error)
	statFunc    func(name string) (os.FileInfo, error)
	readDirFunc func(dirname string) ([]fs.FileInfo, error)
)

// Status stores the status subcommand parameters.
type Status struct {
	ConfigFilePath        string
	BackintParametersPath string

	compact           bool
	help              bool
	logLevel, logPath string
	config            *cpb.Configuration
	gceService        onetime.GCEInterface
	iamService        IAMService
	oteLogger         *onetime.OTELogger
	cloudProps        *iipb.CloudProperties
	readFile          configuration.ReadConfigFile
	backintReadFile   backintconfiguration.ReadConfigFile
	exec              commandlineexecutor.Execute
	exists            commandlineexecutor.Exists
	backintClient     storage.Client
	permissionsStatus permissions.FetchStatusFunc
	httpGet           httpGetter
	createDBHandle    databaseconnector.DBHandleFunc
	stat              statFunc
	readDir           readDirFunc
}

// Name implements the subcommand interface for status.
func (*Status) Name() string { return "status" }

// Synopsis implements the subcommand interface for status.
func (*Status) Synopsis() string { return "get the status of the agent and its services" }

// Usage implements the subcommand interface for status.
func (*Status) Usage() string {
	return `status [-config <path-to-agent-config-file>]
       [-backint <path-to-backint-parameters-file>] [-compact]

  Get the status of the agent and its services.
`
}

// SetFlags implements the subcommand interface for status.
func (s *Status) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.ConfigFilePath, "config", "", "Configuration path override")
	fs.StringVar(&s.ConfigFilePath, "c", "", "Configuration path override")
	fs.StringVar(&s.BackintParametersPath, "backint", "", "Backint parameters path")
	fs.StringVar(&s.BackintParametersPath, "b", "", "Backint parameters path")
	fs.BoolVar(&s.compact, "compact", false, "Display a compact status (no configuration or IAM)")
	fs.BoolVar(&s.help, "h", false, "Display help")
}

// Execute implements the subcommand interface for status.
func (s *Status) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// Initialize logging and cloud properties.
	lp, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     s.Name(),
		Help:     s.help,
		LogLevel: s.logLevel,
		LogPath:  s.logPath,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}
	s.cloudProps = cp
	// Run the status checks.
	agentStatus, exitStatus := s.Run(ctx, onetime.CreateRunOptions(cp, false))
	if exitStatus == subcommands.ExitFailure {
		// Collect support bundle if there's an error.
		supportbundle.CollectAgentSupport(ctx, f, lp, cp, s.Name())
	}
	log.CtxLogger(ctx).Infow("Agent Status", "status", agentStatus)
	statushelper.PrintStatus(ctx, agentStatus, s.compact)
	log.CtxLogger(ctx).Info("Status finished")
	return exitStatus
}

// Run executes the command and returns the status.
func (s *Status) Run(ctx context.Context, opts *onetime.RunOptions) (*spb.AgentStatus, subcommands.ExitStatus) {
	var err error
	s.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	s.readFile = os.ReadFile
	s.backintReadFile = os.ReadFile
	s.exec = commandlineexecutor.ExecuteCommand
	s.exists = commandlineexecutor.CommandExists
	s.backintClient = store.NewClient
	s.iamService, err = iam.NewIAMClient(ctx)
	s.stat = os.Stat
	s.readDir = ioutil.ReadDir
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not create IAM client", "error", err)
		return nil, subcommands.ExitFailure
	}
	s.permissionsStatus = permissions.GetServicePermissionsStatus
	s.httpGet = http.Get
	s.createDBHandle = databaseconnector.CreateDBHandle
	status, err := s.statusHandler(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not get agent status", "error", err)
		return nil, subcommands.ExitFailure
	}
	return status, subcommands.ExitSuccess
}

// statusHandler executes the status checks and returns the results as the AgentStatus proto.
func (s *Status) statusHandler(ctx context.Context) (*spb.AgentStatus, error) {
	if s.permissionsStatus == nil || s.httpGet == nil || s.createDBHandle == nil {
		return nil, fmt.Errorf("status struct has not been initialized")
	}
	log.CtxLogger(ctx).Info("Status starting")
	agentStatus, config := s.agentStatus(ctx)
	agentStatus.Services = append(agentStatus.Services, s.hostMetricsStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.processMetricsStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.hanaMonitoringMetricsStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.systemDiscoveryStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.backintStatus(ctx))
	agentStatus.Services = append(agentStatus.Services, s.diskSnapshotStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.workloadManagerStatus(ctx, config))
	agentStatus.References = append(agentStatus.References, &spb.Reference{
		Name: "Release notes",
		Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new",
	})
	agentStatus.References = append(agentStatus.References, &spb.Reference{
		Name: "Guides",
		Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/all-guides",
	})

	return agentStatus, nil
}

// agentStatus returns the agent version, enabled/running, config path, and the
// configuration as parsed by the agent.
func (s *Status) agentStatus(ctx context.Context) (*spb.AgentStatus, *cpb.Configuration) {
	agentStatus := &spb.AgentStatus{
		AgentName:        agentPackageName,
		InstalledVersion: fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
	}

	var err error
	agentStatus.AvailableVersion, err = statushelper.FetchLatestVersion(ctx, agentPackageName, agentPackageName, runtime.GOOS, s.exec, s.exists)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not fetch latest version", "error", err)
		agentStatus.AvailableVersion = fetchLatestVersionError
	}

	switch {
	case s.cloudProps == nil:
		log.CtxLogger(ctx).Errorw("Could not fetch scopes", "error", err)
		agentStatus.CloudApiAccessFullScopesGranted = spb.State_ERROR_STATE
	case slices.Contains(s.cloudProps.GetScopes(), requiredScope):
		agentStatus.CloudApiAccessFullScopesGranted = spb.State_SUCCESS_STATE
	default:
		agentStatus.CloudApiAccessFullScopesGranted = spb.State_FAILURE_STATE
	}

	enabled, running, err := statushelper.CheckAgentEnabledAndRunning(ctx, agentPackageName, runtime.GOOS, s.exec)
	agentStatus.SystemdServiceEnabled = spb.State_FAILURE_STATE
	agentStatus.SystemdServiceRunning = spb.State_FAILURE_STATE
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not check agent enabled and running", "error", err)
		agentStatus.SystemdServiceEnabled = spb.State_ERROR_STATE
		agentStatus.SystemdServiceRunning = spb.State_ERROR_STATE
	} else {
		if enabled {
			agentStatus.SystemdServiceEnabled = spb.State_SUCCESS_STATE
		}
		if running {
			agentStatus.SystemdServiceRunning = spb.State_SUCCESS_STATE
		}
	}

	path := s.ConfigFilePath
	if len(path) == 0 {
		switch runtime.GOOS {
		case "linux":
			path = configuration.LinuxConfigPath
		case "windows":
			path = configuration.WindowsConfigPath
		}
	}
	agentStatus.ConfigurationFilePath = path
	config, err := configuration.Read(path, s.readFile)
	agentStatus.ConfigurationValid = spb.State_SUCCESS_STATE
	if err != nil {
		agentStatus.ConfigurationValid = spb.State_FAILURE_STATE
		agentStatus.ConfigurationErrorMessage = err.Error()
	}
	config = configuration.ApplyDefaults(config, s.cloudProps)

	return agentStatus, config
}

func (s *Status) hostMetricsStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:  "Host Metrics",
		State: spb.State_UNSPECIFIED_STATE,
		ConfigValues: []*spb.ConfigValue{
			configValue("provide_sap_host_agent_metrics", config.GetProvideSapHostAgentMetrics().GetValue(), true),
		},
	}
	if !config.GetProvideSapHostAgentMetrics().GetValue() {
		status.State = spb.State_FAILURE_STATE
		return status
	}

	status.State = spb.State_SUCCESS_STATE
	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, hostMetricsLabel, &permissions.ResourceDetails{
		ProjectID:    s.cloudProps.GetProjectId(),
		InstanceName: s.cloudProps.GetInstanceId(),
		Zone:         s.cloudProps.GetZone(),
	})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}
	resp, err := s.httpGet(hostMetricsEndpoint)
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error verifying endpoint", spb.State_ERROR_STATE)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return logCheckFailureAndReturnStatus(ctx, status, fmt.Sprintf("Endpoint verification failed with code: %d", resp.StatusCode), spb.State_FAILURE_STATE)
	}

	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}

func (s *Status) processMetricsStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:  "Process Metrics",
		State: spb.State_UNSPECIFIED_STATE,
		ConfigValues: []*spb.ConfigValue{
			configValue("collect_process_metrics", config.GetCollectionConfiguration().GetCollectProcessMetrics(), false),
			configValue("process_metrics_frequency", config.GetCollectionConfiguration().GetProcessMetricsFrequency(), 5),
			configValue("process_metrics_to_skip", config.GetCollectionConfiguration().GetProcessMetricsToSkip(), []string{}),
			configValue("slow_process_metrics_frequency", config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency(), 30),
		},
	}
	if !config.GetCollectionConfiguration().GetCollectProcessMetrics() {
		status.State = spb.State_FAILURE_STATE
		return status
	}

	status.State = spb.State_SUCCESS_STATE
	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, processMetricsLabel, &permissions.ResourceDetails{ProjectID: s.cloudProps.GetProjectId()})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}
	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}
func (s *Status) hanaMonitoringMetricsStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:  "HANA Monitoring Metrics",
		State: spb.State_UNSPECIFIED_STATE,
		ConfigValues: []*spb.ConfigValue{
			configValue("connection_timeout", config.GetHanaMonitoringConfiguration().GetConnectionTimeout().GetSeconds(), 120),
			configValue("enabled", config.GetHanaMonitoringConfiguration().GetEnabled(), false),
			configValue("execution_threads", config.GetHanaMonitoringConfiguration().GetExecutionThreads(), 10),
			configValue("max_connect_retries", config.GetHanaMonitoringConfiguration().GetMaxConnectRetries().GetValue(), 1),
			configValue("query_timeout_sec", config.GetHanaMonitoringConfiguration().GetQueryTimeoutSec(), 300),
			configValue("sample_interval_sec", config.GetHanaMonitoringConfiguration().GetSampleIntervalSec(), 300),
			configValue("send_query_response_time", config.GetHanaMonitoringConfiguration().GetSendQueryResponseTime(), false),
		},
	}
	if !config.GetHanaMonitoringConfiguration().GetEnabled() {
		status.State = spb.State_FAILURE_STATE
		return status
	}

	status.State = spb.State_SUCCESS_STATE
	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, processMetricsLabel, &permissions.ResourceDetails{ProjectID: s.cloudProps.GetProjectId()})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}
	failedInstances := []string{}
	for _, i := range s.config.GetHanaMonitoringConfiguration().GetHanaInstances() {
		// Note: We ignore timeout params here.
		dbp := databaseconnector.Params{
			Username:       i.GetUser(),
			Host:           i.GetHost(),
			Password:       i.GetPassword(),
			PasswordSecret: i.GetSecretName(),
			Port:           i.GetPort(),
			EnableSSL:      i.GetEnableSsl(),
			HostNameInCert: i.GetHostNameInCertificate(),
			RootCAFile:     i.GetTlsRootCaFile(),
			HDBUserKey:     i.GetHdbuserstoreKey(),
			SID:            i.GetSid(),
			GCEService:     s.gceService,
			Project:        s.config.GetCloudProperties().GetProjectId(),
			PingSpec: &databaseconnector.PingSpec{
				Timeout:    1 * time.Second,
				MaxRetries: 0,
			},
		}
		if _, err := s.createDBHandle(ctx, dbp); err != nil {
			log.CtxLogger(ctx).Errorw("Error connecting to database", "name", i.GetName(), "error", err.Error())
			failedInstances = append(failedInstances, i.GetName())
			continue
		}
	}
	if len(failedInstances) > 0 {
		return logCheckFailureAndReturnStatus(ctx, status, fmt.Sprintf("Failed to connect to HANA instances: %s", strings.Join(failedInstances, ", ")), spb.State_FAILURE_STATE)
	}
	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}

func (s *Status) systemDiscoveryStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name: "System Discovery",
		ConfigValues: []*spb.ConfigValue{
			configValue("enable_discovery", config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue(), true),
			configValue("enable_workload_discovery", config.GetDiscoveryConfiguration().GetEnableWorkloadDiscovery().GetValue(), true),
			configValue("sap_instances_update_frequency", config.GetDiscoveryConfiguration().GetSapInstancesUpdateFrequency().GetSeconds(), 60),
			configValue("system_discovery_update_frequency", config.GetDiscoveryConfiguration().GetSystemDiscoveryUpdateFrequency().GetSeconds(), 4*60*60),
		},
	}
	if !config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
		status.State = spb.State_FAILURE_STATE
		return status
	}
	status.State = spb.State_SUCCESS_STATE
	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, systemDiscoveryLabel, &permissions.ResourceDetails{ProjectID: s.cloudProps.GetProjectId()})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}

	// Ensure sapservices and anything in /hana/shared have readable permissions.
	if err := checkFilePermissions("/usr/sap/sapservices", 0400, s.stat); err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, err.Error(), spb.State_FAILURE_STATE)
	}
	files, err := s.readDir("/hana/shared")
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, err.Error(), spb.State_FAILURE_STATE)
	}
	for _, f := range files {
		if err := checkFilePermissions("/hana/shared/"+f.Name(), 0400, s.stat); err != nil {
			return logCheckFailureAndReturnStatus(ctx, status, err.Error(), spb.State_FAILURE_STATE)
		}
	}
	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}

func (s *Status) backintStatus(ctx context.Context) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:  "Backint",
		State: spb.State_UNSPECIFIED_STATE,
	}
	if s.BackintParametersPath == "" {
		status.UnspecifiedStateMessage = "Backint parameters file not specified / Disabled"
		return status
	}
	p := backintconfiguration.Parameters{
		User:      "Status OTE",
		Function:  "diagnose",
		ParamFile: s.BackintParametersPath,
	}
	config, err := p.ParseArgsAndValidateConfig(s.backintReadFile, s.backintReadFile)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not parse backint parameters", "error", err)
		status.State = spb.State_ERROR_STATE
		status.ErrorMessage = err.Error()
		return status
	}
	printConfig := backintconfiguration.ConfigToPrint(config)
	status.State = spb.State_SUCCESS_STATE
	log.CtxLogger(ctx).Infof("Backint parameters: %v", printConfig)

	// Due to the large number of config values, print the important ones always
	// and the others only if the user has overridden the default.
	status.ConfigValues = []*spb.ConfigValue{
		configValue("bucket", printConfig.Bucket, ""),
		configValue("log_to_cloud", printConfig.LogToCloud.GetValue(), true),
		configValue("param_file", printConfig.ParamFile, ""),
	}
	overrideOnlyConfigValues := []*spb.ConfigValue{
		configValue("buffer_size_mb", printConfig.BufferSizeMb, 100),
		configValue("client_endpoint", printConfig.ClientEndpoint, ""),
		configValue("compress", printConfig.Compress, false),
		configValue("custom_time", printConfig.CustomTime, ""),
		configValue("encryption_key", printConfig.EncryptionKey, ""),
		configValue("folder_prefix", printConfig.FolderPrefix, ""),
		configValue("file_read_timeout_ms", printConfig.FileReadTimeoutMs, 60000),
		configValue("kms_key", printConfig.KmsKey, ""),
		configValue("metadata", printConfig.Metadata, map[string]string{}),
		configValue("parallel_streams", printConfig.ParallelStreams, 1),
		configValue("parallel_recovery_streams", printConfig.ParallelRecoveryStreams, 0),
		configValue("rate_limit_mb", printConfig.RateLimitMb, 0),
		configValue("recovery_bucket", printConfig.RecoveryBucket, ""),
		configValue("recovery_folder_prefix", printConfig.RecoveryFolderPrefix, ""),
		configValue("retries", printConfig.Retries, 5),
		configValue("send_metrics_to_monitoring", printConfig.SendMetricsToMonitoring.GetValue(), true),
		configValue("service_account_key", printConfig.ServiceAccountKey, ""),
		configValue("shorten_folder_path", printConfig.ShortenFolderPath, false),
		configValue("storage_class", printConfig.StorageClass, "STANDARD"),
		configValue("threads", printConfig.Threads, 64),
		configValue("xml_multipart_upload", printConfig.XmlMultipartUpload, false),
		configValue("object_retention_mode", printConfig.ObjectRetentionMode, ""),
		configValue("object_retention_time", printConfig.ObjectRetentionTime, ""),
	}
	for _, configValue := range overrideOnlyConfigValues {
		if !configValue.GetIsDefault() {
			status.ConfigValues = append(status.ConfigValues, configValue)
		}
	}

	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, backintLabel, &permissions.ResourceDetails{
		ProjectID:  s.cloudProps.GetProjectId(),
		BucketName: printConfig.Bucket,
	})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	if printConfig.XmlMultipartUpload {
		multipartUploadPermissionsStatus, multipartAllGranted, err := s.fetchPermissionsStatus(ctx, backintMultipartLabel, &permissions.ResourceDetails{
			ProjectID:  s.cloudProps.GetProjectId(),
			BucketName: printConfig.Bucket,
		})
		if err != nil {
			return logCheckFailureAndReturnStatus(ctx, status, "Error checking multipart upload IAM permissions", spb.State_ERROR_STATE)
		}
		permissionsStatus = append(permissionsStatus, multipartUploadPermissionsStatus...)
		allGranted = allGranted && multipartAllGranted
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}
	connectParams := &storage.ConnectParameters{
		StorageClient:    s.backintClient,
		ServiceAccount:   config.GetServiceAccountKey(),
		BucketName:       config.GetBucket(),
		UserAgentSuffix:  "Backint for GCS",
		VerifyConnection: true,
		MaxRetries:       config.GetRetries(),
		Endpoint:         config.GetClientEndpoint(),
		UserAgent:        configuration.StorageAgentName(),
	}
	_, ok := storage.ConnectToBucket(ctx, connectParams)
	if !ok {
		return logCheckFailureAndReturnStatus(ctx, status, "Failed to connect to bucket", spb.State_FAILURE_STATE)
	}
	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}

func (s *Status) diskSnapshotStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:  "Disk Snapshot",
		State: spb.State_SUCCESS_STATE, // Disk snapshot is an OTE so there's no enabled state to check.
	}
	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, diskBackupLabel, &permissions.ResourceDetails{
		ProjectID: s.cloudProps.GetProjectId(),
	})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}
	// TODO: Add striped disk checks once that is GA.
	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}

func (s *Status) workloadManagerStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:  "Workload Manager Evaluation",
		State: spb.State_UNSPECIFIED_STATE,
		ConfigValues: []*spb.ConfigValue{
			configValue("collect_workload_validation_metrics", config.GetCollectionConfiguration().GetCollectWorkloadValidationMetrics().GetValue(), true),
			configValue("config_target_environment", config.GetCollectionConfiguration().GetWorkloadValidationCollectionDefinition().GetConfigTargetEnvironment(), cpb.TargetEnvironment_PRODUCTION),
			configValue("fetch_latest_config", config.GetCollectionConfiguration().GetWorkloadValidationCollectionDefinition().GetFetchLatestConfig().GetValue(), true),
			configValue("workload_validation_db_metrics_frequency", config.GetCollectionConfiguration().GetWorkloadValidationDbMetricsFrequency(), 3600),
			configValue("workload_validation_metrics_frequency", config.GetCollectionConfiguration().GetWorkloadValidationMetricsFrequency(), 300),
		},
	}
	if !config.GetCollectionConfiguration().GetCollectWorkloadValidationMetrics().GetValue() {
		status.State = spb.State_FAILURE_STATE
		return status
	}
	status.State = spb.State_SUCCESS_STATE

	permissionsStatus, allGranted, err := s.fetchPermissionsStatus(ctx, workloadEvaluationMELabel, &permissions.ResourceDetails{ProjectID: s.cloudProps.GetProjectId()})
	if err != nil {
		return logCheckFailureAndReturnStatus(ctx, status, "Error checking IAM permissions", spb.State_ERROR_STATE)
	}
	status.IamPermissions = permissionsStatus
	if !allGranted {
		return logCheckFailureAndReturnStatus(ctx, status, "IAM permissions not granted", spb.State_FAILURE_STATE)
	}

	status.FullyFunctional = spb.State_SUCCESS_STATE
	return status
}

func configValue(name string, value any, defaultValue any) *spb.ConfigValue {
	return &spb.ConfigValue{
		Name:      name,
		Value:     fmt.Sprint(value),
		IsDefault: fmt.Sprint(value) == fmt.Sprint(defaultValue),
	}
}

func (s *Status) fetchPermissionsStatus(ctx context.Context, functionalityName string, resourceProps *permissions.ResourceDetails) ([]*spb.IAMPermission, bool, error) {
	permissions, err := s.permissionsStatus(ctx, s.iamService, functionalityName, resourceProps)
	if err != nil {
		return nil, false, err
	}
	allGranted := true
	var permissionsStatus []*spb.IAMPermission
	for permission, granted := range permissions {
		var permissionState spb.State
		if granted {
			permissionState = spb.State_SUCCESS_STATE
		} else {
			allGranted = false
			permissionState = spb.State_FAILURE_STATE
		}
		permissionsStatus = append(permissionsStatus, &spb.IAMPermission{
			Name:    permission,
			Granted: permissionState,
		})

		// Sort the permissions by name for consistency.
		sort.Slice(permissionsStatus, func(i, j int) bool {
			return permissionsStatus[i].GetName() < permissionsStatus[j].GetName()
		})
	}
	return permissionsStatus, allGranted, nil
}

func logCheckFailureAndReturnStatus(ctx context.Context, status *spb.ServiceStatus, msg string, fullyFunctional spb.State) *spb.ServiceStatus {
	log.CtxLogger(ctx).Errorw(msg)
	status.FullyFunctional = fullyFunctional
	status.ErrorMessage = msg
	return status
}

func checkFilePermissions(path string, wantPermissions fs.FileMode, stat statFunc) error {
	fileInfo, err := stat(path)
	if err != nil {
		return err
	}
	if fileInfo.Mode().Perm()&wantPermissions != wantPermissions {
		return fmt.Errorf("%s has incorrect permissions. Got: %#o, want: %#o", path, fileInfo.Mode().Perm(), wantPermissions)
	}
	return nil
}
