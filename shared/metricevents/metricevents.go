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

// Package metricevents provides a way to track state changes in specific cloud
// monitoring metrics. On state transitions, a cloud logging metric is written.
package metricevents

import (
	"context"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	events map[string]eventData
	mu     sync.Mutex
)

type eventData struct {
	labels      map[string]string
	lastValue   string
	lastUpdated time.Time
}

// Parameters for AddEvent.
type Parameters struct {
	Path    string
	Message string
	Value   string
	Labels  map[string]string

	// Identifier is used to distinguish between events with the same path.
	// The identifier is appended to the path to create a unique key for the
	// event in the event map.
	Identifier string
}

// AddEvent adds an event to the list of events. If the event already exists,
// a cloud logging message is written if the value has changed.
// Returns true if an event incurs a state change.
func AddEvent(ctx context.Context, p Parameters) bool {
	mu.Lock()
	defer mu.Unlock()

	if events == nil {
		events = make(map[string]eventData)
	}
	key := p.Path + p.Identifier
	stateChange := false
	if event, exists := events[key]; exists && event.lastValue != p.Value {
		stateChange = true
		// NOTE: This log message has specific keys used in querying Cloud Logging.
		// Never change these keys since it would have downstream effects.
		log.CtxLogger(ctx).Infow(p.Message, "metricEvent", true, "metric", p.Path, "previousValue", event.lastValue, "currentValue", p.Value, "previousLabels", event.labels, "currentLabels", p.Labels, "lastUpdated", event.lastUpdated)
	}
	events[key] = eventData{
		labels:      p.Labels,
		lastValue:   p.Value,
		lastUpdated: time.Now(),
	}
	return stateChange
}
