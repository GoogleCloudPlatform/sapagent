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
	"runtime"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"go.uber.org/zap/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
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

// ParseArgsAndValidateConfig reads the backint args and params and validates them.
// If valid, the proto will be populated and defaults will be applied.
func (p *Parameters) ParseArgsAndValidateConfig(read ReadConfigFile) (*bpb.BackintConfiguration, bool) {
	if err := p.parseCommandLineArgs(); err != nil {
		log.Logger.Errorw("Incorrect command line arguments", "error", err)
		usagemetrics.Error(usagemetrics.BackintIncorrectArguments)
		return p.Config, false
	}

	if code, err := p.readParametersFile(read); err != nil {
		log.Logger.Errorf("Parameters JSON at '%s' has error: %v. Please fix the JSON and restart backint", p.ParamFile, err)
		usagemetrics.Error(code)
		return p.Config, false
	}

	p.applyDefaults(int64(runtime.NumCPU()))
	return p.Config, true
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
		log.Logger.Warnw("Unsupported log level, defaulting to INFO", "level", level.String())
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
	function := bpb.Function(bpb.Function_value[strings.ToUpper(p.Function)])
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

	config := &bpb.BackintConfiguration{}
	err = protojson.Unmarshal(content, config)
	if err != nil {
		log.Logger.Errorw("Invalid content in the parameters file", "file", p.ParamFile, "content", string(content))
		return usagemetrics.BackintMalformedConfigFile, err
	}
	proto.Merge(p.Config, config)

	if err := p.validateParameters(); err != nil {
		return usagemetrics.BackintMalformedConfigFile, err
	}

	return 0, nil
}

// validateParameters ensures parameters from the params file provide a valid configuration.
func (p *Parameters) validateParameters() error {
	if p.Config.GetBucket() == "" {
		return errors.New("bucket must be provided")
	}
	if p.Config.GetEncryptionKey() != "" && p.Config.GetKmsKey() != "" {
		return errors.New("only one of encryption_key or kms_key can be provided")
	}
	if p.Config.GetFunction() == bpb.Function_BACKUP && p.Config.GetParallelStreams() > 1 {
		if p.Config.GetCompress() {
			return errors.New("compressed parallel backups are not supported - 'parallel_streams' must be set to 1 in order to compress data")
		}
		if p.Config.GetEncryptionKey() != "" || p.Config.GetKmsKey() != "" {
			return errors.New("encrypted parallel backups are not supported - 'parallel_streams' must be set to 1 in order to encrypt data")
		}
	}

	return nil
}

// applyDefaults will apply the default configuration settings to the Backint configuration.
// The defaults are set only if the values passed are undefined or invalid.
func (p *Parameters) applyDefaults(numCPU int64) {
	if p.Config.GetRetries() <= 0 {
		log.Logger.Warn("retries defaulted to 5")
		p.Config.Retries = 5
	}
	if p.Config.GetParallelStreams() <= 0 {
		log.Logger.Warn("parallel_streams defaulted to 1")
		p.Config.ParallelStreams = 1
	}
	if p.Config.GetParallelStreams() > 32 {
		log.Logger.Warn("parallel_streams capped to 32")
		p.Config.ParallelStreams = 32
	}
	if p.Config.GetParallelSizeMb() <= 0 {
		log.Logger.Warn("parallel_size_mb defaulted to 128")
		p.Config.ParallelSizeMb = 128
	}
	if p.Config.GetThreads() <= 0 {
		if numCPU > 64 {
			numCPU = 64
		}
		log.Logger.Warnf("threads defaulted to %d", numCPU)
		p.Config.Threads = numCPU
	}
	if p.Config.GetBufferSizeMb() <= 0 {
		log.Logger.Warn("buffer_size_mb defaulted to 100")
		p.Config.BufferSizeMb = 100
	}
	if p.Config.GetBufferSizeMb() > 250 {
		log.Logger.Warn("buffer_size_mb capped to 250")
		p.Config.BufferSizeMb = 250
	}
	if p.Config.GetRateLimitMb() <= 0 {
		log.Logger.Warn("rate_limit_mb defaulted to 0")
		p.Config.RateLimitMb = 0
	}
	if p.Config.FileReadTimeoutMs <= 0 {
		log.Logger.Warn("file_read_timeout_ms defaulted to 1000")
		p.Config.FileReadTimeoutMs = 1000
	}
	if p.Config.GetInputFile() == "" {
		log.Logger.Warn("input_file defaulted to /dev/stdin")
		p.Config.InputFile = "/dev/stdin"
	}
	if p.Config.GetOutputFile() == "" {
		log.Logger.Warn("output_file defaulted to /dev/stdout")
		p.Config.OutputFile = "/dev/stdout"
	}
}
