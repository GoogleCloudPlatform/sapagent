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

package metricoverrides

import (
	"bytes"
	"context"
	"embed"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

//go:embed test_data/metrics-fake.yaml
var testFS embed.FS

const testDemoMetricYaml = `metric: cluster/failcounts
	metric_value: 1
# Test Comment
metric: cluster/testmetric
	metric_value: 1.1
	test_label: "test_value"
#metric: cluster/testcommentedmetric
#	metric_value: 1.5
metric: cluster/testboolmetric
	metric_value: TRUE
	test_label: "test_value2"
`

func TestCollectDemoMetrics(t *testing.T) {
	tests := []struct {
		name            string
		inputYAML       string
		wantMetricCount int
		wantErr         error
	}{
		{
			name:            "ValidYAML",
			inputYAML:       testDemoMetricYaml,
			wantMetricCount: 3,
		},
		{
			name:            "EmptyYAML",
			inputYAML:       "",
			wantMetricCount: 0, // No metrics should be created
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := DemoMetricsReaderFunc(func(string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader([]byte(tt.inputYAML))), nil
			})
			ip := &DemoInstanceProperties{
				Config:           &cpb.Configuration{},
				Reader:           reader,
				DemoMetricPath:   "test_data/metrics-fake.yaml",
				MetricTypePrefix: "test.com/testmetric",
			}
			got, err := ip.CollectWithRetry(context.Background())
			if !cmp.Equal(err, tt.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Collect() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != tt.wantMetricCount {
				t.Errorf("Collect() = %v, wantMetricCount %v", got, tt.wantMetricCount)
			}
		})
	}
}
