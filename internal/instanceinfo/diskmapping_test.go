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

package instanceinfo

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestForLinux(t *testing.T) {
	inputs := []struct {
		Command func(string) (string, error)
		Want    string
	}{
		{
			Command: func(path string) (string, error) {
				return path, nil
			},
			Want: "google-sda1",
		},
		{
			Command: func(path string) (string, error) {
				return "", nil
			},
			Want: "",
		},
		{
			Command: func(path string) (string, error) {
				return path + "\n", nil
			},
			Want: "google-sda1",
		},
	}
	defer func(f func(path string) (string, error)) { symLinkCommand = f }(symLinkCommand)
	for i := range inputs {
		d := PhysicalPathReader{OS: "linux"}

		symLinkCommand = inputs[i].Command

		want := inputs[i].Want
		got, err := d.ForDeviceName("sda1")

		if err != nil {
			t.Errorf(err.Error())
			continue
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%#v.ForDeviceName(\"sda1\") returned unexpected diff (-want +got):\n%s", d, diff)
		}
	}
}

func TestForLinuxError(t *testing.T) {
	d := PhysicalPathReader{OS: "linux"}
	defer func(f func(path string) (string, error)) { symLinkCommand = f }(symLinkCommand)
	symLinkCommand = func(path string) (string, error) {
		return "", errors.New("test error")
	}

	if _, err := d.ForDeviceName("sda1"); err == nil {
		t.Errorf("%#v.ForDeviceName(\"sda1\") did not return an error", d)
	}
}

func TestForWindows(t *testing.T) {
	inputs := []struct {
		Command func(string, ...string) (string, string, error)
		Want    string
	}{
		{
			Command: func(executable string, args ...string) (string, string, error) {
				return "\nsomemapping\r", "", nil
			},
			Want: "somemapping",
		},
		{
			Command: func(executable string, args ...string) (string, string, error) {
				return "", "", nil
			},
			Want: "",
		},
	}
	defer func(f func(executable string, args ...string) (string, string, error)) { executeCommand = f }(executeCommand)
	for i := range inputs {
		executeCommand = inputs[i].Command

		d := PhysicalPathReader{OS: "windows"}

		want := inputs[i].Want
		got, err := d.ForDeviceName("C:")

		if err != nil {
			t.Errorf(err.Error())
			continue
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%#v.ForDeviceName(\"C:\") returned unexpected diff (-want +got):\n%s", d, diff)
		}
	}
}

func TestForWindowsError(t *testing.T) {
	d := PhysicalPathReader{OS: "windows"}
	defer func(f func(executable string, args ...string) (string, string, error)) { executeCommand = f }(executeCommand)
	executeCommand = func(executable string, args ...string) (string, string, error) {
		return "", "", errors.New("test error")
	}

	if _, err := d.ForDeviceName("C:"); err == nil {
		t.Errorf("%#v.ForDeviceName(\"C:\") did not return an error", d)
	}
}
