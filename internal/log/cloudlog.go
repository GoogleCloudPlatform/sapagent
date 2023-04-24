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

package log

import (
	"context"
	"fmt"
	"log"
	"time"

	logging "cloud.google.com/go/logging"
	"go.uber.org/zap/zapcore"
)

// GoogleCloudLogger interface
type GoogleCloudLogger interface {
	Flush() error
	Log(e logging.Entry)
}

// Mapping of zap severities to Cloud Logging severity.
var severityMapping = map[zapcore.Level]logging.Severity{
	zapcore.DebugLevel:  logging.Debug,
	zapcore.InfoLevel:   logging.Info,
	zapcore.WarnLevel:   logging.Warning,
	zapcore.ErrorLevel:  logging.Error,
	zapcore.DPanicLevel: logging.Critical,
	zapcore.PanicLevel:  logging.Critical,
	zapcore.FatalLevel:  logging.Critical,
}

// CloudLoggingClient create a logging.Client for writing logs to CloudLogging, will be nil if a ping fails
func CloudLoggingClient(ctx context.Context, projectID string) *logging.Client {
	client, err := logging.NewClient(ctx, projectID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return nil
	}
	err = client.Ping(ctx)
	if err != nil {
		log.Printf("Not logging to Cloud Logging: %v", err)
		return nil
	}
	return client
}

// CloudCore that will be used as a zapcore.Core
type CloudCore struct {
	GoogleCloudLogger GoogleCloudLogger
	LogLevel          zapcore.Level
}

// Enabled implements zapcore.Core.
func (c *CloudCore) Enabled(l zapcore.Level) bool {
	return l >= c.LogLevel
}

// With implements zapcore.Core.
func (c *CloudCore) With(additionalFields []zapcore.Field) zapcore.Core {
	return &CloudCore{
		GoogleCloudLogger: c.GoogleCloudLogger,
		LogLevel:          c.LogLevel,
	}
}

// Check implements zapcore.Core.
func (c *CloudCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

// Write implements zapcore.Core. It writes a log entry to Cloud Logging.
func (c *CloudCore) Write(ze zapcore.Entry, additionalFields []zapcore.Field) error {
	severity, specified := severityMapping[ze.Level]
	if !specified {
		severity = logging.Default
	}

	payload := createPayloadWithFields(additionalFields)
	payload["message"] = ze.Message
	payload["caller"] = ze.Caller.String()
	payload["stack"] = ze.Stack

	c.GoogleCloudLogger.Log(logging.Entry{
		Timestamp: ze.Time,
		Severity:  severity,
		Payload:   payload,
	})

	return nil
}

// Sync implements zapcore.Core, flushes the CloudCore's GoogleCloudLogger.
func (c *CloudCore) Sync() error {
	if err := c.GoogleCloudLogger.Flush(); err != nil {
		return fmt.Errorf("Error flushing the Google Cloud Logger: %v", err)
	}
	return nil
}

// createPayloadWithFields creates a map and adds the fields from additionalFields to it.
func createPayloadWithFields(additionalFields []zapcore.Field) map[string]any {
	payload := make(map[string]any)
	for _, f := range additionalFields {
		switch f.Type {
		case zapcore.BoolType:
			payload[f.Key] = (f.Integer == 1)
		case zapcore.ByteStringType, zapcore.StringType:
			payload[f.Key] = f.String
		case zapcore.Complex128Type, zapcore.Complex64Type:
			payload[f.Key] = fmt.Sprint(f.Interface)
		case zapcore.DurationType:
			payload[f.Key] = time.Duration(f.Integer).String()
		case zapcore.Float64Type:
			payload[f.Key] = float64(f.Integer)
		case zapcore.Float32Type:
			payload[f.Key] = float32(f.Integer)
		case zapcore.Int64Type:
			payload[f.Key] = int64(f.Integer)
		case zapcore.Int32Type:
			payload[f.Key] = int32(f.Integer)
		case zapcore.Int16Type:
			payload[f.Key] = int16(f.Integer)
		case zapcore.Int8Type:
			payload[f.Key] = int8(f.Integer)
		case zapcore.TimeType:
			if f.Interface != nil {
				payload[f.Key] = time.Unix(0, f.Integer).In(f.Interface.(*time.Location))
			} else {
				// Fall back to UTC if location is nil.
				payload[f.Key] = time.Unix(0, f.Integer)
			}
		case zapcore.TimeFullType:
			payload[f.Key] = f.Interface.(time.Time)
		case zapcore.Uint64Type:
			payload[f.Key] = uint64(f.Integer)
		case zapcore.Uint32Type:
			payload[f.Key] = uint32(f.Integer)
		case zapcore.Uint16Type:
			payload[f.Key] = uint16(f.Integer)
		case zapcore.Uint8Type:
			payload[f.Key] = uint8(f.Integer)
		case zapcore.UintptrType:
			payload[f.Key] = uintptr(f.Integer)
		case zapcore.StringerType:
			payload[f.Key] = f.Interface.(fmt.Stringer).String()
		case zapcore.ErrorType:
			payload[f.Key] = f.Interface.(error).Error()
		case zapcore.SkipType:
			continue
		default:
			payload[f.Key] = f.Interface
		}
	}
	return payload
}
