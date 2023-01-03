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

package workloadmanager

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zieckey/goini"
	cfgpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
)

func TestCollectMetricsToJSON(t *testing.T) {
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}

	c := &cfgpb.Configuration{
		CollectionConfiguration: &cfgpb.CollectionConfiguration{
			CollectWorkloadValidationMetrics: false,
		},
	}
	p := Parameters{
		Config:               c,
		CommandRunner:        func(cmd string, args string) (string, string, error) { return "", "", nil },
		CommandRunnerNoSpace: func(cmd string, args ...string) (string, string, error) { return "", "", nil },
		ConfigFileReader:     func(data string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil },
		OSStatReader:         func(data string) (os.FileInfo, error) { return nil, nil },
	}
	got := CollectMetricsToJSON(context.Background(), p)
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Errorf("CollectMetricsToJSON returned incorrect JSON, does not start with [ or end with ] got: %s", got)
	}
}
