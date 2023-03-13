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

package onetime

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		os       string
		want     subcommands.ExitStatus
	}{
		{
			name: "WindowsUnSupported",
			os:   "windows",
			want: subcommands.ExitUsageError,
		},
		{
			name:     "EmptyHost",
			snapshot: Snapshot{host: ""},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "EmptyPort",
			snapshot: Snapshot{host: "localhost", port: ""},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "EmptySID",
			snapshot: Snapshot{host: "localhost", port: "123", sid: ""},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "EmptyUser",
			snapshot: Snapshot{host: "localhost", port: "123", sid: "HDB", user: ""},
			want:     subcommands.ExitUsageError,
		},
		{
			name: "EmptyDisk",
			snapshot: Snapshot{
				host: "localhost",
				port: "123",
				sid:  "HDB",
				user: "system",
				disk: "",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "EmptyDiskZone",
			snapshot: Snapshot{
				host:     "localhost",
				port:     "123",
				sid:      "HDB",
				user:     "system",
				disk:     "pd-1",
				diskZone: "",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "EmptyPasswordAndSecret",
			snapshot: Snapshot{
				host:           "localhost",
				port:           "123",
				sid:            "HDB",
				user:           "system",
				disk:           "pd-1",
				diskZone:       "us-east1-a",
				password:       "",
				passwordSecret: "",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "PasswordSecret",
			snapshot: Snapshot{
				host:           "localhost",
				port:           "123",
				sid:            "HDB",
				user:           "system",
				disk:           "pd-1",
				diskZone:       "us-east1-a",
				passwordSecret: "my-secret",
			},
			want: subcommands.ExitFailure, // Cannot access secrets in unit tests.
		},
		{
			name: "Password",
			snapshot: Snapshot{
				host:     "localhost",
				port:     "123",
				sid:      "HDB",
				user:     "system",
				disk:     "pd-1",
				diskZone: "us-east1-a",
				password: "myPass",
			},
			want: subcommands.ExitFailure, // Cannot connect to HANA DB in unit tests.
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.handler(context.Background(), test.os)
			if got != test.want {
				t.Errorf("handler(snapshot=%v, os=%v)=%v, want=%v", test.snapshot, test.os, got, test.want)
			}
		})
	}
}

func TestCreateNewHANASnapshot(t *testing.T) {
	tests := []struct {
		name string
		qf   queryFunc
		want error
	}{
		{
			name: "ReadSnapshotIDError",
			qf: func(*sql.DB, string) (string, error) {
				return "", cmpopts.AnyError
			},
			want: cmpopts.AnyError,
		},
		{
			name: "AbandonSnapshotFailure",
			qf: func(h *sql.DB, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CLOSE") {
					return "", cmpopts.AnyError
				}
				return "stale-snapshot", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateSnapshotFailure",
			qf: func(h *sql.DB, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			qf: func(*sql.DB, string) (string, error) {
				return "", nil
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := createNewHANASnapshot(&sql.DB{}, test.qf, "backup-1")
			if got != test.want {
				t.Errorf("createNewHANASnapshot()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestExecute(t *testing.T) {
	s := &Snapshot{}
	s.SetFlags(&flag.FlagSet{})
	got := s.Execute(context.Background(), nil)
	// Execute fails in unit tests as there is no DB.
	if got != subcommands.ExitUsageError {
		t.Errorf("Execute()=%v, want=%v", got, subcommands.ExitFailure)
	}
}
