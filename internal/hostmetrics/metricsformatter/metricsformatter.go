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

// Package metricsformatter provides functionality for manipulating data to be used for metrics collection.
package metricsformatter

import (
	"math"
)

const (
	// Unavailable is the default reported value for an unavailable metric.
	Unavailable = -1.0
)

// PerMinuteToPerSec converts a per-minute value to per-second value, rounding to the nearest integer.
func PerMinuteToPerSec(perMinute float64) int64 {
	return int64(math.Round(perMinute / 60))
}

// ToPercentage converts a value into a percentage (out of one hundred) with the given precision.
func ToPercentage(value float64, precision int) float64 {
	return math.Round(value*math.Pow10(precision)) * 100 / math.Pow10(precision)
}
