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

// Package configuration parses and validates the Backint configuration parameters.
package configuration

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"go.uber.org/zap/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

// ReadConfigFile abstracts os.ReadFile function for testability.
type ReadConfigFile func(string) ([]byte, error)

// Parameters holds the Backint configuration parameters to validate.
type Parameters struct {
	User, Function             string
	InFile, OutFile, ParamFile string
	BackupID, BackupLevel      string
	Count                      int64
	Config                     *bpb.BackintConfiguration
}

// GetLogParameters returns the log_to_cloud and log_level configuration.
func GetLogParameters(readConfig ReadConfigFile, ParamFile string) (bool, zapcore.Level) {
	p := &Parameters{ParamFile: ParamFile, Config: &bpb.BackintConfiguration{}}
	p.readParametersFile(readConfig)
	if p.Config.GetLogToCloud() == nil {
		p.Config.LogToCloud = wpb.Bool(true)
	}
	return p.Config.GetLogToCloud().GetValue(), LogLevelToZapcore(p.Config.GetLogLevel())
}

// ParseArgsAndValidateConfig reads the backint args and params and validates them.
// If valid, the proto will be populated and defaults will be applied.
func (p *Parameters) ParseArgsAndValidateConfig(readConfig ReadConfigFile, readEncryptionKey ReadConfigFile) (*bpb.BackintConfiguration, error) {
	if err := p.parseCommandLineArgs(); err != nil {
		log.Logger.Errorw("Incorrect command line arguments", "error", err)
		usagemetrics.Error(usagemetrics.BackintIncorrectArguments)
		return p.Config, err
	}

	if code, err := p.readParametersFile(readConfig); err != nil {
		log.Logger.Errorf("Parameters file at '%s' has error: %v. Please fix the error and restart backint", p.ParamFile, err)
		usagemetrics.Error(code)
		return p.Config, err
	}

	p.ApplyDefaults(int64(runtime.NumCPU()))

	if p.Config.GetEncryptionKey() != "" {
		if err := p.readEncryptionKeyFromFile(readEncryptionKey); err != nil {
			return p.Config, err
		}
	}
	if p.Config.GetRecoveryBucket() != "" || p.Config.GetRecoveryFolderPrefix() != "" {
		usagemetrics.Action(usagemetrics.BackintRecoveryParameterEnabled)
	}
	return p.Config, nil
}

// LogLevelToZapcore returns the zapcore equivalent of the configuration log level.
func LogLevelToZapcore(level bpb.LogLevel) zapcore.Level {
	switch level {
	case bpb.LogLevel_DEBUG:
		return zapcore.DebugLevel
	case bpb.LogLevel_INFO:
		return zapcore.InfoLevel
	case bpb.LogLevel_WARNING:
		return zapcore.WarnLevel
	case bpb.LogLevel_ERROR:
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// parseCommandLineArgs checks that the necessary CLI arguments are provided.
func (p *Parameters) parseCommandLineArgs() error {
	if p.User == "" {
		return errors.New("user ID must be provided")
	}
	if p.ParamFile == "" {
		return errors.New("parameters file must be provided")
	}
	if p.Function == "" {
		return errors.New("function must be provided")
	}
	function := BackintFunction(p.Function)
	if function == bpb.Function_FUNCTION_UNSPECIFIED {
		return errors.New("function must be one of: [backup, restore, inquire, delete, diagnose]")
	}

	p.Config = &bpb.BackintConfiguration{
		UserId:              p.User,
		InputFile:           p.InFile,
		OutputFile:          p.OutFile,
		ParamFile:           p.ParamFile,
		BackupId:            p.BackupID,
		DatabaseObjectCount: p.Count,
		BackupLevel:         p.BackupLevel,
		Function:            function,
	}
	return nil
}

// readParametersFile reads backint configuration from the params file into proto.
func (p *Parameters) readParametersFile(read ReadConfigFile) (int, error) {
	content, err := read(p.ParamFile)
	if err != nil {
		return usagemetrics.BackintConfigReadFailure, err
	}
	if len(content) == 0 {
		return usagemetrics.BackintConfigReadFailure, errors.New("empty parameters file")
	}

	config, err := Unmarshal(p.ParamFile, content)
	if err != nil {
		return usagemetrics.BackintMalformedConfigFile, err
	}
	proto.Merge(p.Config, config)

	// For RESTORE operations, several parameters can be overridden.
	if p.Config.GetFunction() == bpb.Function_RESTORE {
		if p.Config.GetRecoveryBucket() != "" {
			p.Config.Bucket = p.Config.GetRecoveryBucket()
			p.Config.FolderPrefix = p.Config.GetRecoveryFolderPrefix()
			log.Logger.Infow("bucket and folder_prefix overridden by recovery_bucket and recovery_folder_prefix for RESTORE operation", "bucket", p.Config.GetBucket(), "folder_prefix", p.Config.GetFolderPrefix())
		} else if p.Config.GetRecoveryFolderPrefix() != "" {
			p.Config.FolderPrefix = p.Config.GetRecoveryFolderPrefix()
			log.Logger.Infow("folder_prefix overridden by recovery_folder_prefix for RESTORE operation", "folder_prefix", p.Config.GetFolderPrefix())
		}
	}
	if err := p.validateParameters(); err != nil {
		return usagemetrics.BackintMalformedConfigFile, err
	}

	if strings.HasSuffix(p.ParamFile, ".txt") {
		marshalLegacyParameters(p.ParamFile, config)
	}
	return 0, nil
}

// validateParameters ensures parameters from the params file provide a valid configuration.
func (p *Parameters) validateParameters() error {
	if p.Config.GetBucket() == "" {
		return errors.New("bucket must be provided")
	}
	if strings.Contains(p.Config.GetBucket(), "/") || strings.HasPrefix(p.Config.GetBucket(), "gs:") {
		return fmt.Errorf("bucket (%s) must not contain any '/' or be prefixed with 'gs:', only include the name of the bucket in the parameter", p.Config.GetBucket())
	}
	if p.Config.GetRecoveryBucket() != "" && (strings.Contains(p.Config.GetRecoveryBucket(), "/") || strings.HasPrefix(p.Config.GetRecoveryBucket(), "gs:")) {
		return fmt.Errorf("recovery_bucket (%s) must not contain any '/' or be prefixed with 'gs:', only include the name of the bucket in the parameter", p.Config.GetRecoveryBucket())
	}
	if p.Config.GetEncryptionKey() != "" && p.Config.GetKmsKey() != "" {
		return errors.New("only one of encryption_key or kms_key can be provided")
	}
	if p.Config.GetFunction() == bpb.Function_BACKUP && (p.Config.GetParallelStreams() > 1 || p.Config.GetXmlMultipartUpload()) {
		if p.Config.GetCompress() {
			return errors.New("compressed parallel backups are not supported - 'parallel_streams' must be set to 1 in order to compress data")
		}
		if p.Config.GetEncryptionKey() != "" || p.Config.GetKmsKey() != "" {
			return errors.New("encrypted parallel backups are not supported - 'parallel_streams' must be set to 1 in order to encrypt data")
		}
	}
	if p.Config.GetFunction() == bpb.Function_RESTORE && p.Config.GetParallelRecoveryStreams() > 1 {
		if p.Config.GetCompress() {
			return errors.New("compressed parallel restores are not supported - 'parallel_recovery_streams' must be set to 0 or 1 in order to compress data")
		}
	}
	if p.Config.GetObjectRetentionMode() != "" && p.Config.GetObjectRetentionMode() != "Unlocked" && p.Config.GetObjectRetentionMode() != "Locked" {
		return errors.New("object_retention_mode must be either 'Unlocked' or 'Locked'")
	}
	if (p.Config.GetObjectRetentionTime() != "" && p.Config.GetObjectRetentionMode() == "") || (p.Config.GetObjectRetentionTime() == "" && p.Config.GetObjectRetentionMode() != "") {
		return errors.New("object_retention_time and object_retention_mode must be set together")
	}

	return nil
}

// ApplyDefaults will apply the default configuration settings to the Backint configuration.
// The defaults are set only if the values passed are undefined or invalid.
func (p *Parameters) ApplyDefaults(numCPU int64) {
	if p.Config.GetLogToCloud() == nil {
		p.Config.LogToCloud = wpb.Bool(true)
	}
	if p.Config.GetSendMetricsToMonitoring() == nil {
		p.Config.SendMetricsToMonitoring = wpb.Bool(true)
	}
	if p.Config.GetRetries() <= 0 {
		log.Logger.Info("retries defaulted to 5")
		p.Config.Retries = 5
	}
	if p.Config.GetParallelStreams() <= 0 && p.Config.GetXmlMultipartUpload() {
		log.Logger.Info("parallel_streams defaulted to 16 for XML multipart upload")
		p.Config.ParallelStreams = 16
	}
	if p.Config.GetParallelStreams() <= 0 {
		log.Logger.Info("parallel_streams defaulted to 1")
		p.Config.ParallelStreams = 1
	}
	if p.Config.GetParallelStreams() > 32 {
		log.Logger.Info("parallel_streams capped to 32")
		p.Config.ParallelStreams = 32
	}
	if p.Config.GetParallelRecoveryStreams() > 32 {
		log.Logger.Info("parallel_recovery_streams capped to 32")
		p.Config.ParallelRecoveryStreams = 32
	}
	if p.Config.GetThreads() <= 0 {
		if numCPU > 64 {
			numCPU = 64
		}
		log.Logger.Infof("threads defaulted to %d", numCPU)
		p.Config.Threads = numCPU
	}
	if p.Config.GetThreads() > 64 {
		log.Logger.Info("threads capped to 64")
		p.Config.Threads = 64
	}
	if p.Config.GetBufferSizeMb() <= 0 {
		log.Logger.Info("buffer_size_mb defaulted to 100")
		p.Config.BufferSizeMb = 100
	}
	if p.Config.GetBufferSizeMb() > 250 {
		log.Logger.Info("buffer_size_mb capped to 250")
		p.Config.BufferSizeMb = 250
	}
	if p.Config.GetRateLimitMb() <= 0 {
		log.Logger.Info("rate_limit_mb defaulted to 0")
		p.Config.RateLimitMb = 0
	}
	if p.Config.FileReadTimeoutMs <= 0 {
		log.Logger.Info("file_read_timeout_ms defaulted to 60000")
		p.Config.FileReadTimeoutMs = 60000
	}
	if p.Config.GetInputFile() == "" {
		log.Logger.Info("input_file defaulted to /dev/stdin")
		p.Config.InputFile = "/dev/stdin"
	}
	if p.Config.GetOutputFile() == "" {
		log.Logger.Info("output_file defaulted to /dev/stdout")
		p.Config.OutputFile = "/dev/stdout"
	}
	if p.Config.GetFolderPrefix() != "" {
		if !strings.HasSuffix(p.Config.GetFolderPrefix(), "/") {
			p.Config.FolderPrefix += "/"
		}
		log.Logger.Infof("folder_prefix is set. All objects in the GCS bucket will be prefixed with '%s'", p.Config.GetFolderPrefix())
	}
	if p.Config.GetLogLevel() == bpb.LogLevel_LOG_LEVEL_UNSPECIFIED {
		p.Config.LogLevel = bpb.LogLevel_INFO
	}
}

func (p *Parameters) readEncryptionKeyFromFile(read ReadConfigFile) error {
	keyFileName := p.Config.GetEncryptionKey()
	data, err := read(keyFileName)
	if err != nil {
		log.Logger.Errorw("Unable to read encryption_key file", "keyFileName", keyFileName, "err", err)
		return err
	}
	p.Config.EncryptionKey = strings.TrimSuffix(string(data), "\n")
	log.Logger.Debugw("Successfully read encryption_key file", "keyFileName", keyFileName)
	return nil
}

// Unmarshal reads the content into a BackintConfiguration proto.
// If a .json file is supplied, protojson handles the unmarshaling.
// If a .txt file is provided, a custom parse is used.
func Unmarshal(parameterFile string, content []byte) (*bpb.BackintConfiguration, error) {
	config := &bpb.BackintConfiguration{}
	var err error
	if strings.HasSuffix(parameterFile, ".json") {
		if err = protojson.Unmarshal(content, config); err != nil {
			return nil, err
		}
		return config, nil
	}

	log.Logger.Infow("Parsing legacy parameters file", "configPath", parameterFile)
	config.Compress = true
	config.LogToCloud = wpb.Bool(true)
	for _, line := range strings.Split(string(content), "\n") {
		if line == "" {
			continue
		}
		split := strings.SplitN(line, " ", 2)
		if len(split) < 2 && line != "#DISABLE_COMPRESSION" && line != "#DUMP_DATA" && line != "#DISABLE_CLOUD_LOGGING" {
			return nil, fmt.Errorf("empty value for parameter: %s", line)
		}
		switch split[0] {
		case "#DISABLE_COMPRESSION":
			config.Compress = false
		case "#DISABLE_CLOUD_LOGGING":
			config.LogToCloud = wpb.Bool(false)
		case "#DUMP_DATA":
			config.DumpData = true
		case "#BUCKET":
			config.Bucket = split[1]
		case "#SERVICE_ACCOUNT":
			config.ServiceAccountKey = split[1]
		case "#ENCRYPTION_KEY":
			config.EncryptionKey = split[1]
		case "#KMS_KEY_NAME":
			config.KmsKey = split[1]
		case "#LOG_LEVEL":
			config.LogLevel = bpb.LogLevel(bpb.LogLevel_value[split[1]])
		case "#READ_IDLE_TIMEOUT":
			if config.FileReadTimeoutMs, err = strconv.ParseInt(split[1], 10, 64); err != nil {
				return nil, fmt.Errorf("failed to parse #READ_IDLE_TIMEOUT as int64, err: %v", err)
			}
		case "#CHUNK_SIZE_MB":
			if config.BufferSizeMb, err = strconv.ParseInt(split[1], 10, 64); err != nil {
				return nil, fmt.Errorf("failed to parse #CHUNK_SIZE_MB as int64, err: %v", err)
			}
		case "#RATE_LIMIT_MB":
			if config.RateLimitMb, err = strconv.ParseInt(split[1], 10, 64); err != nil {
				return nil, fmt.Errorf("failed to parse #RATE_LIMIT_MB as int64, err: %v", err)
			}
		case "#MAX_GCS_RETRY":
			if config.Retries, err = strconv.ParseInt(split[1], 10, 64); err != nil {
				return nil, fmt.Errorf("failed to parse #MAX_GCS_RETRY as int64, err: %v", err)
			}
		case "#PARALLEL_FACTOR":
			if config.ParallelStreams, err = strconv.ParseInt(split[1], 10, 64); err != nil {
				return nil, fmt.Errorf("failed to parse #PARALLEL_FACTOR as int64, err: %v", err)
			}
		case "#THREADS":
			if config.Threads, err = strconv.ParseInt(split[1], 10, 64); err != nil {
				return nil, fmt.Errorf("failed to parse #THREADS as int64, err: %v", err)
			}
		case "#PARALLEL_PART_SIZE_MB":
			log.Logger.Infow("#PARALLEL_PART_SIZE_MB has been deprecated and can be removed from the configuration", "line", line)
		default:
			log.Logger.Warnw("Unexpected line in parameters file", "line", line)
		}
	}
	return config, nil
}

// marshalLegacyParameters attempts to save the legacy .txt parameters
// as a .json file. If an error occurs, allow Backint to continue execution.
func marshalLegacyParameters(parameterFile string, config *bpb.BackintConfiguration) {
	configPath := strings.TrimSuffix(parameterFile, ".txt") + ".json"
	log.Logger.Infow("Saving legacy parameters as JSON", "configPath", configPath)
	configData, err := protojson.MarshalOptions{Indent: "  ", UseProtoNames: true}.Marshal(config)
	if err != nil {
		log.Logger.Errorw("Unable to marshal config", "err", err)
	}
	if err := os.WriteFile(configPath, configData, 0640); err != nil {
		log.Logger.Errorw("Unable to write JSON parameters file", "configPath", configPath, "err", err)
	}
	if err := os.Chmod(configPath, 0640); err != nil {
		log.Logger.Errorw("Unable to change permissions on JSON parameters file", "configPath", configPath, "err", err)
	}
	log.Logger.Infow("Successfully translated text parameters file to JSON", "parameterFileText", parameterFile, "parameterFileJSON", configPath)
}

// ConfigToPrint returns a string representation of the configuration for logging.
// It ensures we do not log the encryption key, kms key and service account key.
func ConfigToPrint(config *bpb.BackintConfiguration) *bpb.BackintConfiguration {
	maskedconfig := proto.Clone(config).(*bpb.BackintConfiguration)
	if maskedconfig.GetEncryptionKey() != "" {
		maskedconfig.EncryptionKey = "***"
	}
	if maskedconfig.GetKmsKey() != "" {
		maskedconfig.KmsKey = "***"
	}
	if maskedconfig.GetServiceAccountKey() != "" {
		maskedconfig.ServiceAccountKey = "***"
	}
	return maskedconfig
}

// BackintFunction is a helper function to return BackintFunction enum for the given string.
func BackintFunction(function string) bpb.Function {
	return bpb.Function(bpb.Function_value[strings.ToUpper(function)])
}
