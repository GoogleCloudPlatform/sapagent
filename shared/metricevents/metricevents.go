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
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	events         map[string]eventData
	mu             sync.Mutex
	logDelayEvents map[string]eventData
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
	if logDelayEvents == nil {
		logDelayEvents = make(map[string]eventData)
	}

	key := p.Path + p.Identifier
	stateChange := false
	if event, exists := events[key]; exists && event.lastValue != p.Value {
		stateChange = true
		// Some metric paths are sent multiple times - one for each service.
		// To avoid logging the same event multiple times, we will group labels
		// that share the same path and value and log after a short delay.
		logDelayKey := p.Path + p.Value
		if logEvent, exists := logDelayEvents[logDelayKey]; exists {
			for k, v := range event.labels {
				if logEvent.labels[k] != v {
					logEvent.labels[k] += ", " + v
				}
			}
		} else {
			logDelayEvents[logDelayKey] = event
			time.AfterFunc(time.Minute, func() {
				// Lock the mutex as this runs in a separate goroutine.
				mu.Lock()
				defer mu.Unlock()
				logEvent := logDelayEvents[logDelayKey]
				// Sort and remove duplicate labels.
				for k, labels := range logEvent.labels {
					labelSlice := strings.Split(labels, ", ")
					sort.Strings(labelSlice)
					logEvent.labels[k] = strings.Join(slices.Compact(labelSlice), ", ")
				}
				// NOTE: This log message has specific keys used in querying Cloud Logging.
				// Never change these keys since it would have downstream effects.
				log.CtxLogger(ctx).Infow(p.Message, "metricEvent", true, "metric", p.Path, "previousValue", logEvent.lastValue, "currentValue", p.Value, "previousLabels", p.Labels, "currentLabels", logEvent.labels, "lastUpdated", logEvent.lastUpdated)
				delete(logDelayEvents, logDelayKey)
			})
		}
	}

	events[key] = eventData{
		labels:      p.Labels,
		lastValue:   p.Value,
		lastUpdated: time.Now(),
	}
	return stateChange
}
