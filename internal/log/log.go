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

// Package log provides a wrapper to the underlying logging library
package log

import (
	"io/ioutil"
	"log"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	cpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
)

// Logger used for logging structured messages
var Logger *zap.SugaredLogger
var level string
var logfile string

func init() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	Logger = logger.Sugar()
}

// SetupLoggingToFile provides the configuration of the Logger to file based on configuration of the agent
func SetupLoggingToFile(goos string, l cpb.Configuration_LogLevel) {
	logfile = "/var/log/google-cloud-sap-agent.log"
	if goos == "windows" {
		logfile = "C:\\Program Files\\Google\\google-cloud-sap-agent\\logs\\google-cloud-sap-agent.log"
	}
	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	config.TimeKey = "timestamp"
	fileEncoder := zapcore.NewJSONEncoder(config)
	writer := zapcore.AddSync(&lumberjack.Logger{
		Filename:   logfile,
		MaxSize:    50, // megabytes
		MaxBackups: 3,
	})
	defaultLogLevel := zapcore.InfoLevel
	switch l {
	case cpb.Configuration_DEBUG:
		defaultLogLevel = zapcore.DebugLevel
	case cpb.Configuration_INFO:
		defaultLogLevel = zapcore.InfoLevel
	case cpb.Configuration_WARNING:
		defaultLogLevel = zapcore.WarnLevel
	case cpb.Configuration_ERROR:
		defaultLogLevel = zapcore.ErrorLevel
	}
	level = defaultLogLevel.String()
	core := zapcore.NewTee(
		zapcore.NewCore(fileEncoder, writer, defaultLogLevel),
	)
	coreLogger := zap.New(core, zap.AddCaller())
	defer coreLogger.Sync()
	// we use the sugared logger to allow for unstructured data in the logs
	Logger = coreLogger.Sugar()
}

/*
SetupLoggingToDiscard provides the configuration of the Logger to discard all logs.
Discarding logs is only used when the agent is run remotely during workload manager metrics remote collection.
*/
func SetupLoggingToDiscard() {
	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(config)
	writer := zapcore.AddSync(ioutil.Discard)
	core := zapcore.NewTee(
		zapcore.NewCore(fileEncoder, writer, zapcore.ErrorLevel),
	)
	coreLogger := zap.New(core, zap.AddCaller())
	defer coreLogger.Sync()
	Logger = coreLogger.Sugar()
	log.SetFlags(0)
}

// GetLevel will return the current logging level as a string
func GetLevel() string {
	return level
}

// GetLogFile will return the current logfile as a string
func GetLogFile() string {
	return logfile
}

// Error creates a zap Field for an error
func Error(err error) zap.Field {
	return zap.Error(err)
}

// Float64 creates a zap Field for a float64.
func Float64(key string, value float64) zap.Field {
	return zap.Float64(key, value)
}

// Print will use the go default log to print messages to the console, should only be used by main's
func Print(msg string) {
	log.SetFlags(0)
	log.Print(msg)
}

// String creates a zap Field for a string.
func String(key string, value string) zap.Field {
	return zap.String(key, value)
}
