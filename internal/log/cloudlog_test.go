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
	"fmt"
	"sync"
	"testing"
	"time"

	logging "cloud.google.com/go/logging"
	"go.uber.org/zap/zapcore"
)

func TestLogLevels(t *testing.T) {
	tests := []struct {
		name       string
		setlevel   zapcore.Level
		checklevel zapcore.Level
		want       bool
	}{
		{
			name:       "DebugDisabled",
			setlevel:   zapcore.InfoLevel,
			checklevel: zapcore.DebugLevel,
			want:       false,
		},
		{
			name:       "InfoDisabled",
			setlevel:   zapcore.WarnLevel,
			checklevel: zapcore.InfoLevel,
			want:       false,
		},
		{
			name:       "WarnDisabled",
			setlevel:   zapcore.ErrorLevel,
			checklevel: zapcore.WarnLevel,
			want:       false,
		},
		{
			name:       "DebugEnabled",
			setlevel:   zapcore.DebugLevel,
			checklevel: zapcore.DebugLevel,
			want:       true,
		},
		{
			name:       "InfoEnabled",
			setlevel:   zapcore.DebugLevel,
			checklevel: zapcore.InfoLevel,
			want:       true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cc := &CloudCore{LogLevel: test.setlevel}
			got := cc.Enabled(test.checklevel)
			if got != test.want {
				t.Errorf("Log level check failed (%v), setlevel: %v, checklevel: %v, got: %v, want :%v",
					test.name, test.setlevel, test.checklevel, got, test.want)
			}
		})
	}
}

func TestSync(t *testing.T) {
	ml := &mockLogger{}
	cc := &CloudCore{GoogleCloudLogger: ml}

	cc.Sync()
	want := true
	got := ml.flushed

	if got != want {
		t.Errorf("Sync call did not call Flush, got: %v, want: %v", got, want)
	}
}

func TestFlushCloudLog(t *testing.T) {
	ml := &mockLogger{}
	cloudCore = &CloudCore{GoogleCloudLogger: ml}

	FlushCloudLog()
	want := true
	got := ml.flushed

	if got != want {
		t.Errorf("FlushCloudLog call did not call Flush, got: %v, want: %v", got, want)
	}
}

func TestSyncFailure(t *testing.T) {
	mfl := &mockFailureLogger{}
	cc := &CloudCore{GoogleCloudLogger: mfl}

	err := cc.Sync()
	want := "Error flushing the Google Cloud Logger: Failed to flush"
	got := err.Error()

	if got != want {
		t.Errorf("Sync call did not fail as expected, got: %v, want: %v", got, want)
	}
}

func getIntZapcoreField(key string, value int64, t zapcore.FieldType) zapcore.Field {
	return zapcore.Field{
		Key:     key,
		Type:    t,
		Integer: value,
	}
}

func getInterfaceZapcoreField(key string, value any, t zapcore.FieldType) zapcore.Field {
	return zapcore.Field{
		Key:       key,
		Type:      t,
		Interface: value,
	}
}

func getStringZapcoreField(key string, value string, t zapcore.FieldType) zapcore.Field {
	return zapcore.Field{
		Key:    key,
		Type:   t,
		String: value,
	}
}

func getTimeTypeZapcoreField(key string, value int64, location any) zapcore.Field {
	return zapcore.Field{
		Key:       key,
		Type:      zapcore.TimeType,
		Integer:   value,
		Interface: location,
	}
}

func TestWrite(t *testing.T) {
	ml := &mockLogger{}
	cc := &CloudCore{GoogleCloudLogger: ml}
	ts := time.Now()

	tests := []struct {
		name             string
		zapentry         zapcore.Entry
		additionalfields []zapcore.Field
		wantfields       map[string]any
		wantseverity     logging.Severity
	}{
		{
			name: "LogInfoMessage",
			zapentry: zapcore.Entry{
				Message:    "message",
				LoggerName: "loggername",
				Level:      zapcore.InfoLevel,
				Time:       ts,
				Caller: zapcore.EntryCaller{
					Defined: true,
					File:    "file",
					Line:    10,
				},
				Stack: "stack",
			},
			additionalfields: []zapcore.Field{
				zapcore.Field{
					Key:       "defaultkey",
					Interface: "defaultinterface",
				},
				getStringZapcoreField("stringkey", "string", zapcore.StringType),
				getStringZapcoreField("bytstringkey", "bytestring", zapcore.ByteStringType),
				getInterfaceZapcoreField("complex128key", 128, zapcore.Complex128Type),
				getInterfaceZapcoreField("complex64key", 64, zapcore.Complex64Type),
				getInterfaceZapcoreField("timefullkey", ts, zapcore.TimeFullType),
				getInterfaceZapcoreField("skipkey", nil, zapcore.SkipType),
				getInterfaceZapcoreField("stringerkey", ml, zapcore.StringerType),
				getInterfaceZapcoreField("errorkey", fmt.Errorf("some error"), zapcore.ErrorType),
				getIntZapcoreField("float64key", 64, zapcore.Float64Type),
				getIntZapcoreField("float32key", 32, zapcore.Float32Type),
				getIntZapcoreField("int64key", 64, zapcore.Int64Type),
				getIntZapcoreField("int32key", 32, zapcore.Int32Type),
				getIntZapcoreField("int16key", 16, zapcore.Int16Type),
				getIntZapcoreField("int8key", 8, zapcore.Int8Type),
				getIntZapcoreField("uint64key", 64, zapcore.Uint64Type),
				getIntZapcoreField("uint32key", 32, zapcore.Uint32Type),
				getIntZapcoreField("uint16key", 16, zapcore.Uint16Type),
				getIntZapcoreField("uint8key", 8, zapcore.Uint8Type),
				getIntZapcoreField("uintptrkey", 1, zapcore.UintptrType),
				getIntZapcoreField("durationkey", 999, zapcore.DurationType),
				getIntZapcoreField("boolkey", 1, zapcore.BoolType),
				getTimeTypeZapcoreField("timekey", ts.UnixNano(), ts.Location()),
				getTimeTypeZapcoreField("timekeyNilInterface", 123, nil),
			},
			wantfields: map[string]any{
				"message":       "message",
				"caller":        "file:10",
				"stack":         "stack",
				"stringkey":     "string",
				"bytstringkey":  "bytestring",
				"int64key":      64,
				"int32key":      32,
				"int16key":      16,
				"int8key":       8,
				"uint64key":     64,
				"uint32key":     32,
				"uint16key":     16,
				"uint8key":      8,
				"uintptrkey":    1,
				"boolkey":       true,
				"complex128key": 128,
				"complex64key":  64,
				"durationkey":   "999ns",
				"defaultkey":    "defaultinterface",
				"timefullkey":   ts,
				"errorkey":      "some error",
				"stringerkey":   "mockLogger",
				// Strip monotonic clock reading
				"timekey":             ts.Round(0),
				"timekeyNilInterface": time.Unix(0, 123),
			},
			wantseverity: logging.Info,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cc.Write(test.zapentry, test.additionalfields)
			for k, want := range test.wantfields {
				got := ml.entries[0].Payload.(map[string]any)[k]
				if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
					t.Errorf("Log write failed (%v), got: %v, want: %v", test.name, got, want)
				}
			}
			gotseverity := ml.entries[0].Severity
			if gotseverity != test.wantseverity {
				t.Errorf("Log write failed (%v), gotseverity: %v, wantseverity :%v", test.name, gotseverity, test.wantseverity)
			}
			cc.Sync()
		})
	}
}

type mockLogger struct {
	entries []logging.Entry
	mutex   sync.Mutex
	flushed bool
}

func (t *mockLogger) Flush() error {
	t.flushed = true
	t.entries = nil
	return nil
}

func (t *mockLogger) String() string {
	return "mockLogger"
}

func (t *mockLogger) Log(e logging.Entry) {
	t.mutex.Lock()
	t.entries = append(t.entries, e)
	t.mutex.Unlock()
}

type mockFailureLogger struct {
	entries []logging.Entry
	mutex   sync.Mutex
	flushed bool
}

func (t *mockFailureLogger) Flush() error {
	return fmt.Errorf("Failed to flush")
}

func (t *mockFailureLogger) Log(e logging.Entry) {
	// do nothing
}
