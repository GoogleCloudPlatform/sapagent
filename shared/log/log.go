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
onto the "msg" field.  This may cause serialization issues.

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
		log.Logger.Infow(fmt.Sprintf("My message to log with data 5v"), "error", error)

	Log data directly into the message without serialization:
		log.Logger.Infof("My message to log with data %v", 77.33232)
		log.Logger.Infof("My message to log with data %d", 77.33232)

Note:

	    When using the logger if you use the "f" methods such as Infof the data in the message will
			be serialized unless you use Sprintf style in the message and the correct type on the data
			such as %v, %s, %d.  The suggested approach is to separate any data into separate fields for
			logging.
*/
package log

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	logging "cloud.google.com/go/logging"
	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Logger used for logging structured messages
	Logger           *zap.SugaredLogger
	level            string
	logfile          string
	cloudCore        *CloudCore
	constructionLock sync.Mutex

	// loggerMap maps services identified by their serviceNames to respective SugaredLoggers.
	loggerMap map[string]*zap.SugaredLogger
)

type (
	// Parameters for setting up logging
	Parameters struct {
		LogToCloud         bool
		CloudLoggingClient *logging.Client
		OSType             string
		Level              zapcore.Level
		LogFileName        string
		LogFilePath        string
		CloudLogName       string
	}
	cloudWriter struct {
		w io.Writer
	}
)

// contextKeyType represents context key Service
type contextKeyType string

// CtxKey is a key of the type contextKeyType for context logging.
const CtxKey contextKeyType = "context"

// init returns default logger with no context.
func init() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	Logger = logger.Sugar()
	loggerMap = make(map[string]*zap.SugaredLogger)
}

// OTEFilePath returns the log file path for the OTE invoked depending if it is invoked internally
// or via command line.
func OTEFilePath(agentName string, oteName string, osType string, logFilePath string) string {
	logName := agentName
	if oteName != "" {
		logName = fmt.Sprintf("%s-%s", agentName, oteName)
	}
	LogFileName := fmt.Sprintf("/var/log/%s.log", logName)
	if osType != "windows" && logFilePath != "" {
		if !strings.HasSuffix(logFilePath, `/`) {
			logFilePath = logFilePath + `/`
		}
		LogFileName = fmt.Sprintf("%s%s.log", logFilePath, logName)
	}
	if osType == "windows" {
		LogFileName = fmt.Sprintf(`C:\Program Files\Google\%s\logs\%s.log`, agentName, logName)
		if logFilePath != "" {
			if !strings.HasSuffix(logFilePath, `\`) {
				logFilePath = logFilePath + `\`
			}
			LogFileName = fmt.Sprintf(`%s%s.log`, logFilePath, logName)
		}
	}
	return LogFileName
}

// DefaultOTEPath returns the default OTE path for the agent/command. {COMMAND} is a placeholder for
// the command name.
func DefaultOTEPath(agentName string, osType string, logFilePath string) string {
	return OTEFilePath(agentName, "{COMMAND}", osType, logFilePath)
}

// SetupLogging uses the agent configuration to set up the file Logger.
func SetupLogging(params Parameters) {
	level = params.Level.String()
	logfile = params.LogFileName
	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	config.TimeKey = "timestamp"
	logEncoder := zapcore.NewJSONEncoder(config)
	fileOrPrintLogger := &lumberjack.Logger{
		Filename:   params.LogFileName,
		MaxSize:    25, // megabytes
		MaxBackups: 3,
	}
	_, err := fileOrPrintLogger.Write(make([]byte, 0))
	fileOrPrintLogWriter := zapcore.AddSync(fileOrPrintLogger)
	if err != nil {
		// Could not write to the log file, write to console instead
		logEncoder = zapcore.NewConsoleEncoder(config)
		fileOrPrintLogWriter = zapcore.AddSync(os.Stdout)
	}

	var core zapcore.Core
	// if logging to Cloud Logging then add the file based logging + cloud logging, else just file logging
	if params.LogToCloud && params.CloudLoggingClient != nil {
		cloudCore = &CloudCore{
			GoogleCloudLogger: params.CloudLoggingClient.Logger(params.CloudLogName),
			LogLevel:          params.Level,
		}
		core = zapcore.NewTee(
			zapcore.NewCore(logEncoder, fileOrPrintLogWriter, params.Level),
			cloudCore,
		)
	} else {
		core = zapcore.NewTee(
			zapcore.NewCore(logEncoder, fileOrPrintLogWriter, params.Level),
		)
	}
	coreLogger := zap.New(core, zap.AddCaller()).With(zap.Int("pid", os.Getpid()))
	defer coreLogger.Sync()
	// we use the sugared logger to allow for simpler field and message additions to logs
	Logger = coreLogger.Sugar()
}

// StringLevelToZapcore returns the equivalent of the string log level. It defaults to info level
// in case unknown log level is identified.
func StringLevelToZapcore(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		Logger.Warnw("Unsupported log level, defaulting to info", "level", level)
		return zapcore.InfoLevel
	}
}

// SetupLoggingForTest creates the Logger to log to the console during unit tests.
func SetupLoggingForTest() {
	constructionLock.Lock()
	defer constructionLock.Unlock()
	logger, _ := zap.NewDevelopment()
	level = zapcore.DebugLevel.String()
	logfile = ""
	defer logger.Sync() // flushes buffer, if any
	Logger = logger.Sugar()
}

// SetupLoggingForOTE creates logging config for the agent's one time execution.
func SetupLoggingForOTE(agentName, command string, params Parameters) Parameters {
	params.CloudLogName = fmt.Sprintf("%s-%s", agentName, command)
	SetupLogging(params)
	return params
}

/*
SetupLoggingToDiscard provides the configuration of the Logger to discard all logs.
Discarding logs is only used when the agent is run remotely during workload manager metrics remote collection.
*/
func SetupLoggingToDiscard() {
	constructionLock.Lock()
	defer constructionLock.Unlock()
	level = ""
	logfile = ""
	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(config)
	writer := zapcore.AddSync(io.Discard)
	core := zapcore.NewTee(
		zapcore.NewCore(fileEncoder, writer, zapcore.ErrorLevel),
	)
	coreLogger := zap.New(core, zap.AddCaller())
	defer coreLogger.Sync()
	Logger = coreLogger.Sugar()
	log.SetFlags(0)
}

// SetCtx returns context objects with additional key value pairs.
func SetCtx(ctx context.Context, key, value string) context.Context {
	return context.WithValue(ctx, contextKeyType(key), value)
}

// CtxLogger  returns a zap logger with as much context as possible
func CtxLogger(ctx context.Context) *zap.SugaredLogger {
	constructionLock.Lock()
	defer constructionLock.Unlock()
	if serviceName, ok := ctx.Value(CtxKey).(string); ok {
		if logger, exists := loggerMap[serviceName]; exists {
			return logger
		}
		loggerMap[serviceName] = Logger.With(zap.String("context", serviceName))
		return loggerMap[serviceName]
	}
	return Logger
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

// Int64 creates a zap Field for an int64.
func Int64(key string, value int64) zap.Field {
	return zap.Int64(key, value)
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

// FlushCloudLog will flush any buffered log entries to cloud logging if it is enabled.
func FlushCloudLog() {
	if cloudCore != nil && cloudCore.GoogleCloudLogger != nil {
		cloudCore.GoogleCloudLogger.Flush()
	}
}
