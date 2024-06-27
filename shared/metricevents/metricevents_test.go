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

package metricevents

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestAddEvent makes 2 calls to AddEvent with p1 then p2.
func TestAddEvent(t *testing.T) {
	tests := []struct {
		name       string
		p1         Parameters
		p2         Parameters
		wantUpdate bool
		wantEvents map[string]eventData
	}{
		{
			name: "SameEventNoChange",
			p1: Parameters{
				Path:   "event1",
				Value:  "value1",
				Labels: map[string]string{"label1": "value1"},
			},
			p2: Parameters{
				Path:   "event1",
				Value:  "value1",
				Labels: map[string]string{"label1": "value1"},
			},
			wantUpdate: false,
			wantEvents: map[string]eventData{
				"event1": eventData{
					labels:    map[string]string{"label1": "value1"},
					lastValue: "value1",
				},
			},
		},
		{
			name: "SameEventValueChange",
			p1: Parameters{
				Path:   "event1",
				Value:  "value1",
				Labels: map[string]string{"label1": "value1"},
			},
			p2: Parameters{
				Path:   "event1",
				Value:  "value2",
				Labels: map[string]string{"label1": "value1"},
			},
			wantUpdate: true,
			wantEvents: map[string]eventData{
				"event1": eventData{
					labels:    map[string]string{"label1": "value1"},
					lastValue: "value2",
				},
			},
		},
		{
			name: "SameEventLabelChange",
			p1: Parameters{
				Path:   "event1",
				Value:  "value1",
				Labels: map[string]string{"label1": "value1"},
			},
			p2: Parameters{
				Path:   "event1",
				Value:  "value1",
				Labels: map[string]string{"label2": "value2"},
			},
			wantUpdate: false,
			wantEvents: map[string]eventData{
				"event1": eventData{
					labels:    map[string]string{"label2": "value2"},
					lastValue: "value1",
				},
			},
		},
		{
			name: "DifferentEventPaths",
			p1: Parameters{
				Path:   "event1",
				Value:  "value1",
				Labels: map[string]string{"label1": "value1"},
			},
			p2: Parameters{
				Path:   "event2",
				Value:  "value2",
				Labels: map[string]string{"label2": "value2"},
			},
			wantUpdate: false,
			wantEvents: map[string]eventData{
				"event1": eventData{
					labels:    map[string]string{"label1": "value1"},
					lastValue: "value1",
				},
				"event2": eventData{
					labels:    map[string]string{"label2": "value2"},
					lastValue: "value2",
				},
			},
		},
		{
			name: "DifferentEventsSamePath",
			p1: Parameters{
				Path:       "event1",
				Value:      "value1",
				Labels:     map[string]string{"label1": "value1"},
				Identifier: "1",
			},
			p2: Parameters{
				Path:       "event1",
				Value:      "value2",
				Labels:     map[string]string{"label2": "value2"},
				Identifier: "2",
			},
			wantUpdate: false,
			wantEvents: map[string]eventData{
				"event11": eventData{
					labels:    map[string]string{"label1": "value1"},
					lastValue: "value1",
				},
				"event12": eventData{
					labels:    map[string]string{"label2": "value2"},
					lastValue: "value2",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Clear the events map before each test.
			for k := range events {
				delete(events, k)
			}
			AddEvent(context.Background(), test.p1)
			got := AddEvent(context.Background(), test.p2)
			if got != test.wantUpdate {
				t.Errorf("AddEvent(%v, %v)=%v, want %v", test.p1, test.p2, got, test.wantUpdate)
			}
			if diff := cmp.Diff(test.wantEvents, events, cmpopts.IgnoreFields(eventData{}, "lastUpdated"), cmp.AllowUnexported(eventData{})); diff != "" {
				t.Errorf("AddEvent(%v, %v) returned diff (-want +got):\n%s", test.p1, test.p2, diff)
			}
		})
	}
}
