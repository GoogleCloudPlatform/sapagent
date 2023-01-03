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

package main

import (
	"flag"
	"testing"
)

func TestSetupFlagsAndParse(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		setFlag     bool
		flagValue   string
		checkConfig bool
	}{
		{
			name:        "HasHelp",
			pattern:     "help",
			setFlag:     false,
			flagValue:   "",
			checkConfig: false,
		},
		{
			name:        "HasProject",
			pattern:     "project",
			setFlag:     true,
			flagValue:   "someproject",
			checkConfig: true,
		},
		{
			name:        "HasInstanceId",
			pattern:     "instance",
			setFlag:     true,
			flagValue:   "someinstance",
			checkConfig: true,
		},
		{
			name:        "HasInstanceName",
			pattern:     "name",
			setFlag:     true,
			flagValue:   "someinstancename",
			checkConfig: true,
		},
		{
			name:        "HasZone",
			pattern:     "zone",
			setFlag:     true,
			flagValue:   "somezone",
			checkConfig: true,
		},
	}

	setupFlags()
	// set flags
	for _, test := range tests {
		if test.setFlag {
			flag.Set(test.pattern, test.flagValue)
		}
	}
	parseFlags(false)

	for _, test := range tests {
		got := flag.Lookup(test.pattern)
		if got == nil {
			t.Errorf("failure in setupFlagsAndParse(). got-flag: %s  want-flag: nil", test.pattern)
		}
		var want, fgot string
		switch test.pattern {
		case "project":
			want = "someproject"
			fgot = config.CloudProperties.ProjectId
		case "instance":
			want = "someinstance"
			fgot = config.CloudProperties.InstanceId
		case "name":
			want = "someinstancename"
			fgot = config.CloudProperties.InstanceName
		case "zone":
			want = "somezone"
			fgot = config.CloudProperties.Zone
		}
		if test.checkConfig && fgot != want {
			t.Errorf("failure in setupFlagsAndParse(). got-config-value: %s  want-config-value: %s", fgot, want)
		}
	}
}
