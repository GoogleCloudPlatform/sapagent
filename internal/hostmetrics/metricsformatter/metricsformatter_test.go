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

package metricsformatter

import (
	"fmt"
	"os"
	"testing"

	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func ExamplePerMinuteToPerSec() {
	fmt.Println(PerMinuteToPerSec(90))
	// Output: 2
}

func TestPerMinuteToPerSec(t *testing.T) {
	tests := map[float64]int64{
		50:     1,
		70:     1,
		89.999: 1,
		90:     2,
	}
	for v, want := range tests {
		got := PerMinuteToPerSec(v)
		if got != want {
			t.Errorf("PerMinuteToPerSec(%g) = %d want %d", v, got, want)
		}
	}
}

func ExampleToPercentage() {
	fmt.Println(ToPercentage(0.12345, 4))
	// Output: 12.35
}

func TestToPercentage(t *testing.T) {
	for _, v := range []struct {
		value     float64
		precision int
		want      float64
	}{
		{0.01, 1, 0},
		{0.01, 2, 1},
		{0.01, 3, 1},
		{0.12345, 1, 10},
		{0.12345, 2, 12},
		{0.12345, 3, 12.3},
		{0.12345, 4, 12.35},
		{0.555, 0, 100},
		{0.555, 1, 60},
		{0.555, 2, 56},
		{0.555, 3, 55.5},
	} {
		got := ToPercentage(v.value, v.precision)
		if got != v.want {
			t.Errorf("ToPercentage(%g, %d) = %g want %g", v.value, v.precision, got, v.want)
		}
	}
}
