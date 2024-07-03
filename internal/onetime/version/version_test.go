/*
Copyright 2023 Google LLC

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

package version

import (
	"context"
	"testing"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestSynopsis(t *testing.T) {
	v := Version{}
	want := "print Agent for SAP version information"

	got := v.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want %v", got, want)
	}
}

func TestName(t *testing.T) {
	v := Version{}
	want := "version"

	got := v.Name()
	if got != want {
		t.Errorf("Name()=%v, want %v", got, want)
	}
}

func TestExecuteVersion(t *testing.T) {
	v := Version{}
	want := subcommands.ExitSuccess
	args := []any{
		"test",
		log.Parameters{},
		nil,
	}

	got := v.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, args...)
	if got != want {
		t.Errorf("Execute(%v, %v)=%v, want %v", v, args, got, want)
	}
}
