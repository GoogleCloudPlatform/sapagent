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

package sapguestactions

import (
	"context"
	"testing"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func TestStartACSCommunication(t *testing.T) {
	tests := []struct {
		name   string
		config *cpb.Configuration
		want   bool
	}{
		{
			name:   "Default",
			config: &cpb.Configuration{},
			want:   false,
		},
		{
			name: "Disabled",
			config: &cpb.Configuration{
				UapConfiguration: &cpb.UAPConfiguration{
					Enabled: &wpb.BoolValue{Value: false},
				},
			},
			want: false,
		},
		{
			name: "Enabled",
			config: &cpb.Configuration{
				UapConfiguration: &cpb.UAPConfiguration{
					Enabled: &wpb.BoolValue{Value: true},
				},
			},
			want: true,
		},
		{
			name: "TestChannelEnabled",
			config: &cpb.Configuration{
				UapConfiguration: &cpb.UAPConfiguration{
					Enabled:            &wpb.BoolValue{Value: true},
					TestChannelEnabled: &wpb.BoolValue{Value: true},
				},
			},
			want: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		if got := StartACSCommunication(ctx, tc.config); got != tc.want {
			t.Errorf("StartACSCommunication(%v) = %v, want: %v", tc.config, got, tc.want)
		}
	}
}
