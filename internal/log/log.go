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

/*
Package log provides a wrapper to the underlying logging library.

Logging should be done by accessing the log.Logger object to issue log statements.

Using any of the base loggers (Debug, Info, Warn, Error) any additional fields will be concatenated
onto into the "msg" field.  This may cause serialization issues.

It's suggested to use the "with" log methods (Debugw, Infow, Warnw, Errorw) to include additional
fields in the log line.

Examples:

	  Log with additional fields in JSON:
		  log.Logger.Infow("My message to log", "key1", "value1", "key2", 9999, "key3", errors.New("an error"))
			or
			log.Logger.Infow("My message to log", log.String("key1", "value1"))

		Log data directly into the message:
		  log.Logger.Info("My message to log with data", " key1: ", SOMEVAR, " key2: ", 9999)

NOT RECOMMENDED

	Log data in the message and additional fields:
		log.Logger.Infow(fmt.Sprtingf("My message to log with data 5v"), "error", error)

	Log data directly into the message without serialization:
		log.Logger.Infof("My message to log with data %v", 77.33232)
		log.Logger.Infof("My message to log with data %d", 77.33232)

Note:

	    When using the logger if you use the "f" methods such as Infof the data in the message will
			be serialized unless you use Sprintf style in the message and the correect type on the data
			such as %v, %s, %d.  The suggested approach is to separate any data into separate fields for
			logging.
*/
package log

import (
	"io/ioutil"
	"log"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
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

// SetupLoggingToFile provides the configuration of the Logger to file based on configuration of the agent.
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
		MaxSize:    25, // megabytes
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
	// we use the sugared logger to allow for simpler field and message additions to logs
	Logger = coreLogger.Sugar()
}

// SetupLoggingForTest creates the Logger to log to the console during unit tests.
func SetupLoggingForTest() {
	logger, _ := zap.NewDevelopment()
	level = zapcore.DebugLevel.String()
	logfile = ""
	defer logger.Sync() // flushes buffer, if any
	Logger = logger.Sugar()
}

/*
SetupLoggingToDiscard provides the configuration of the Logger to discard all logs.
Discarding logs is only used when the agent is run remotely during workload manager metrics remote collection.
*/
func SetupLoggingToDiscard() {
	level = ""
	logfile = ""
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

// GetLevel will return the current logging level as a string.
func GetLevel() string {
	return level
}

// GetLogFile will return the current logfile as a string.
func GetLogFile() string {
	return logfile
}

// Error creates a zap Field for an error.
func Error(err error) zap.Field {
	return zap.Error(err)
}

// Float64 creates a zap Field for a float64.
func Float64(key string, value float64) zap.Field {
	return zap.Float64(key, value)
}

// Print will use the go default log to print messages to the console, should only be used by main.
func Print(msg string) {
	log.SetFlags(0)
	log.Print(msg)
}

// String creates a zap Field for a string.
func String(key string, value string) zap.Field {
	return zap.String(key, value)
}
