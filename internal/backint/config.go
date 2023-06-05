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

package backint

import (
	"errors"
	"runtime"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

// ReadConfigFile abstracts os.ReadFile function for testability.
type ReadConfigFile func(string) ([]byte, error)

// ParseArgsAndValidateConfig reads the backint args and params and validates them.
// If valid, the proto will be populated and defaults will be applied.
func (b *Backint) ParseArgsAndValidateConfig(read ReadConfigFile) bool {
	if err := b.parseCommandLineArgs(); err != nil {
		log.Logger.Errorw("Incorrect command line arguments", "error", err)
		usagemetrics.Error(usagemetrics.BackintIncorrectArguments)
		return false
	}

	if code, err := b.readParametersFile(b.paramFile, read); err != nil {
		log.Logger.Errorf("Parameters JSON at '%s' has error: %v. Please fix the JSON and restart backint", b.paramFile, err)
		usagemetrics.Error(code)
		return false
	}

	b.applyDefaults(int64(runtime.NumCPU()))
	return true
}

// parseCommandLineArgs checks that the necessary CLI arguments are provided.
func (b *Backint) parseCommandLineArgs() error {
	if b.user == "" {
		return errors.New("user ID must be provided")
	}
	if b.paramFile == "" {
		return errors.New("parameters file must be provided")
	}
	if b.function == "" {
		return errors.New("function must be provided")
	}
	function := bpb.Function(bpb.Function_value[strings.ToUpper(b.function)])
	if function == bpb.Function_FUNCTION_UNSPECIFIED {
		return errors.New("function must be one of: [backup, restore, inquire, delete]")
	}

	b.config = &bpb.BackintConfiguration{
		UserId:              b.user,
		InputFile:           b.inFile,
		OutputFile:          b.outFile,
		ParamFile:           b.paramFile,
		BackupId:            b.backupID,
		DatabaseObjectCount: b.count,
		BackupLevel:         b.backupLevel,
		Function:            function,
	}
	return nil
}

// readParametersFile reads backint configuration from the given file into proto.
func (b *Backint) readParametersFile(p string, read ReadConfigFile) (int, error) {
	content, err := read(p)
	if err != nil {
		return usagemetrics.BackintConfigReadFailure, err
	}
	if len(content) == 0 {
		return usagemetrics.BackintConfigReadFailure, errors.New("empty parameters file")
	}

	config := &bpb.BackintConfiguration{}
	err = protojson.Unmarshal(content, config)
	if err != nil {
		log.Logger.Errorw("Invalid content in the parameters file", "file", p, "content", string(content))
		return usagemetrics.BackintMalformedConfigFile, err
	}
	proto.Merge(b.config, config)

	if err := b.validateParameters(); err != nil {
		return usagemetrics.BackintMalformedConfigFile, err
	}

	return 0, nil
}

// validateParameters ensures parameters from the params file provide a valid configuration.
func (b *Backint) validateParameters() error {
	if b.config.GetBucket() == "" {
		return errors.New("bucket must be provided")
	}
	if b.config.GetEncryptionKey() != "" && b.config.GetKmsKey() != "" {
		return errors.New("only one of encryption_key or kms_key can be provided")
	}
	if b.config.GetFunction() == bpb.Function_BACKUP && b.config.GetParallelStreams() > 1 {
		if b.config.GetCompress() {
			return errors.New("compressed parallel backups are not supported - 'parallel_streams' must be set to 1 in order to compress data")
		}
		if b.config.GetEncryptionKey() != "" || b.config.GetKmsKey() != "" {
			return errors.New("encrypted parallel backups are not supported - 'parallel_streams' must be set to 1 in order to encrypt data")
		}
	}

	return nil
}

// applyDefaults will apply the default configuration settings to the Backint configuration.
// The defaults are set only if the values passed are undefined or invalid.
func (b *Backint) applyDefaults(numCPU int64) {
	if b.config.GetRetries() <= 0 {
		log.Logger.Warn("retries defaulted to 5")
		b.config.Retries = 5
	}
	if b.config.GetParallelStreams() <= 0 {
		log.Logger.Warn("parallel_streams defaulted to 1")
		b.config.ParallelStreams = 1
	}
	if b.config.GetParallelSizeMb() <= 0 {
		log.Logger.Warn("parallel_size_mb defaulted to 128")
		b.config.ParallelSizeMb = 128
	}
	if b.config.GetThreads() <= 0 {
		if numCPU > 64 {
			numCPU = 64
		}
		log.Logger.Warnf("threads defaulted to %d", numCPU)
		b.config.Threads = numCPU
	}
	if b.config.GetBufferSizeMb() <= 0 {
		log.Logger.Warn("buffer_size_mb defaulted to 100")
		b.config.BufferSizeMb = 100
	}
	if b.config.GetRateLimitMb() <= 0 {
		log.Logger.Warn("rate_limit_mb defaulted to 0")
		b.config.RateLimitMb = 0
	}
	if b.config.FileReadTimeoutMs <= 0 {
		log.Logger.Warn("file_read_timeout_ms defaulted to 1000")
		b.config.FileReadTimeoutMs = 1000
	}
}
