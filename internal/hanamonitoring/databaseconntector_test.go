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

package databaseconnector

import (
	"testing"
)

func TestConnectFailure(t *testing.T) {
	_, err := Connect("fakeUser", "fakePass", "fakeHost", "fakePort")
	if err == nil {
		t.Error("Connect('fakeUser', 'fakePass', 'fakeHost', 'fakePort') = nil, want any error")
	}
}

func TestConnectValidatesDriver(t *testing.T) {
	// Connect() with empty arguments will still be able to validate the hdb driver and create a *sql.DB.
	// A call to Query() with this returned *sql.DB would encounter a ping error.
	_, err := Connect("", "", "", "")
	if err != nil {
		t.Errorf("Connect('', '', '', '') = %v, want nil error", err)
	}
}
