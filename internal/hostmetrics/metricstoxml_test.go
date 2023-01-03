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

package hostmetrics

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	pb "github.com/GoogleCloudPlatform/sap-agent/protos/metrics"
)

func TestGenerateXML_ConfigWithRestart(t *testing.T) {
	nowMillis := time.Now().UnixMilli()
	metricsCollection := &pb.MetricsCollection{
		Metrics: []*pb.Metric{
			&pb.Metric{
				Category:        pb.Category_CATEGORY_CONFIG,
				Context:         pb.Context_CONTEXT_VM,
				RefreshInterval: pb.RefreshInterval_REFRESHINTERVAL_RESTART,
				Name:            "somemetric",
				Value:           "somevalue",
				LastRefresh:     nowMillis,
			},
		},
	}

	got := GenerateXML(metricsCollection)

	want := "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n" +
		"<metrics>\n" +
		"<metric category=\"config\" context=\"vm\" last-refresh=\"" +
		strconv.FormatInt(nowMillis, 10) +
		"\" refresh-interval=\"0\">\n" +
		"<name>somemetric</name>\n" +
		"<value>somevalue</value>\n" +
		"</metric>\n" +
		"</metrics>\n"
	diff := cmp.Diff(want, got)
	if diff != "" {
		t.Errorf("GenerateXML returned unexpected diff (-want +got):\n%s\n  want:\n%s\n  got:\n%s", diff, want, got)
	}
}

func TestGenerateXML_CPUWithRefresh(t *testing.T) {
	nowMillis := time.Now().UnixMilli()
	metricsCollection := &pb.MetricsCollection{
		Metrics: []*pb.Metric{
			&pb.Metric{
				Category:        pb.Category_CATEGORY_CPU,
				Context:         pb.Context_CONTEXT_VM,
				RefreshInterval: pb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				Name:            "somemetric",
				Value:           "somevalue",
				LastRefresh:     nowMillis,
			},
		},
	}

	got := GenerateXML(metricsCollection)

	want := "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n" +
		"<metrics>\n" +
		"<metric category=\"cpu\" context=\"vm\" last-refresh=\"" +
		strconv.FormatInt(nowMillis, 10) +
		"\" refresh-interval=\"60\">\n" +
		"<name>somemetric</name>\n" +
		"<value>somevalue</value>\n" +
		"</metric>\n" +
		"</metrics>\n"
	diff := cmp.Diff(want, got)
	if diff != "" {
		t.Errorf("GenerateXML returned unexpected diff (-want +got):\n%s\n  want:\n%s\n  got:\n%s", diff, want, got)
	}
}

func TestGenerateXML_WithDeviceId(t *testing.T) {
	nowMillis := time.Now().UnixMilli()
	deviceID := "somedeviceid"
	metricsCollection := &pb.MetricsCollection{
		Metrics: []*pb.Metric{
			&pb.Metric{
				Category:        pb.Category_CATEGORY_CPU,
				Context:         pb.Context_CONTEXT_VM,
				RefreshInterval: pb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				Name:            "somemetric",
				Value:           "somevalue",
				DeviceId:        deviceID,
				LastRefresh:     nowMillis,
			},
		},
	}

	got := GenerateXML(metricsCollection)

	want := "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n" +
		"<metrics>\n" +
		"<metric category=\"cpu\" context=\"vm\" " +
		"device-id=\"somedeviceid\" last-refresh=\"" +
		strconv.FormatInt(nowMillis, 10) +
		"\" refresh-interval=\"60\">\n" +
		"<name>somemetric</name>\n" +
		"<value>somevalue</value>\n" +
		"</metric>\n" +
		"</metrics>\n"
	diff := cmp.Diff(want, got)
	if diff != "" {
		t.Errorf("GenerateXML returned unexpected diff (-want +got):\n%s\n  want:\n%s\n  got:\n%s", diff, want, got)
	}
}

func TestGenerateXML_MemoryDiskNetwork(t *testing.T) {
	nowMillis := time.Now().UnixMilli()
	metricsCollection := &pb.MetricsCollection{
		Metrics: []*pb.Metric{
			&pb.Metric{
				Category:        pb.Category_CATEGORY_MEMORY,
				Context:         pb.Context_CONTEXT_VM,
				Unit:            pb.Unit_UNIT_MB,
				Type:            pb.Type_TYPE_INT32,
				RefreshInterval: pb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				Name:            "memmetric",
				Value:           "memvalue",
				LastRefresh:     nowMillis,
			},
			&pb.Metric{
				Category:        pb.Category_CATEGORY_DISK,
				Context:         pb.Context_CONTEXT_VM,
				Unit:            pb.Unit_UNIT_PERCENT,
				Type:            pb.Type_TYPE_STRING,
				RefreshInterval: pb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				Name:            "diskmetric",
				Value:           "diskvalue",
				LastRefresh:     nowMillis,
			},
			&pb.Metric{
				Category:        pb.Category_CATEGORY_NETWORK,
				Context:         pb.Context_CONTEXT_VM,
				Unit:            pb.Unit_UNIT_OPS_PER_SEC,
				Type:            pb.Type_TYPE_INT64,
				RefreshInterval: pb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				Name:            "netmetric",
				Value:           "netvalue",
				LastRefresh:     nowMillis,
			},
		},
	}

	got := GenerateXML(metricsCollection)

	want := "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n" +
		"<metrics>\n" +
		"<metric category=\"memory\" context=\"vm\" type=\"int32\" unit=\"MB\" last-refresh=\"" +
		strconv.FormatInt(nowMillis, 10) +
		"\" refresh-interval=\"60\">\n" +
		"<name>memmetric</name>\n" +
		"<value>memvalue</value>\n" +
		"</metric>\n" +
		"<metric category=\"disk\" context=\"vm\" type=\"string\" unit=\"Percent\" last-refresh=\"" +
		strconv.FormatInt(nowMillis, 10) +
		"\" refresh-interval=\"60\">\n" +
		"<name>diskmetric</name>\n" +
		"<value>diskvalue</value>\n" +
		"</metric>\n" +
		"<metric category=\"network\" context=\"vm\" type=\"int64\" unit=\"OpsPerSec\" last-refresh=\"" +
		strconv.FormatInt(nowMillis, 10) +
		"\" refresh-interval=\"60\">\n" +
		"<name>netmetric</name>\n" +
		"<value>netvalue</value>\n" +
		"</metric>\n" +
		"</metrics>\n"
	diff := cmp.Diff(want, got)
	if diff != "" {
		t.Errorf("GenerateXML returned unexpected diff (-want +got):\n%s\n  want:\n%s\n  got:\n%s", diff, want, got)
	}
}
