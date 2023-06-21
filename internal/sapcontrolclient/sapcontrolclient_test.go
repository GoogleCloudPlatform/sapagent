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

package sapcontrolclient

import (
	_ "embed"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// DefaultSapcontrolSocket defines a default socket for mock sapcontrol service.
// "10" is used as a default instance for mock sapcontrol service.
const DefaultSapcontrolSocket = "/tmp/.sapstream51013"

// Sapcontrol is a mock server implementation for sapcontrol webservices.
type Sapcontrol struct {
	server   *httptest.Server
	response string
}

// Stop http listener for the mock server.
func (m Sapcontrol) Stop() {
	m.server.Close()
}

var (
	//go:embed testdata/getprocesslist/all_processes.txt
	processListResponse string

	//go:embed testdata/getprocesslist/one_process.txt
	oneProcessListResponse string

	emptyProcessListResponse string = `<?xml version="1.0" encoding="UTF-8"?>
		</SOAP-ENV:Envelope>`

	//go:embed testdata/getprocesslist/no_pid.txt
	noPidProcessListResponse string

	//go:embed testdata/getprocesslist/no_name.txt
	noNameProcessListResponse string
)

// NewSapControl returns a new mock for sapcontrol.
// socket is the socket path the mock sapcontrol is supposed to listen on.
func NewSapControl(socket, response string) (*Sapcontrol, error) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(response))
	})
	// Create a listener on a unix socket.
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}

	// Create test server.
	s := httptest.NewUnstartedServer(handler)

	// Replace default listener with socket listener.
	s.Listener.Close()
	s.Listener = l
	s.Start()
	return &Sapcontrol{s, response}, nil
}

func setupSAPMocks(t *testing.T, response string) *Sapcontrol {
	t.Helper()
	mock, err := NewSapControl(DefaultSapcontrolSocket, response)
	if err != nil {
		t.Fatalf("setupSAPMocks(): %v", err)
	}
	t.Cleanup(mock.Stop)
	return mock
}

// setupClient() returns a Client listening to the specified fake SAP Instance number.
func setupClient(t *testing.T) Client {
	t.Helper()
	// "10" is used as a default instance for mock sapcontrol service.
	return New("10")
}

func TestGetProcessList(t *testing.T) {
	tests := []struct {
		name           string
		fakeResponse   string
		wantProcStatus []OSProcess
		wantErr        error
	}{
		{
			name:           "SucceedsOneProcess",
			fakeResponse:   oneProcessListResponse,
			wantProcStatus: []OSProcess{OSProcess{"hdbdaemon", "SAPControl-GREEN", 9609}},
			wantErr:        nil,
		},
		{
			name:         "SucceedsAllProcesses",
			fakeResponse: processListResponse,
			wantProcStatus: []OSProcess{
				{"hdbdaemon", "SAPControl-GREEN", 9609},
				{"hdbcompileserver", "SAPControl-GREEN", 9972},
				{"hdbindexserver", "SAPControl-GREEN", 10013},
				{"hdbnameserver", "SAPControl-GREEN", 9642},
				{"hdbpreprocessor", "SAPControl-GREEN", 9975},
				{"hdbwebdispatcher", "SAPControl-GREEN", 11322},
				{"hdbxsengine", "SAPControl-GREEN", 10016},
			},
			wantErr: nil,
		},
		{
			name:         "SapControlFails",
			fakeResponse: `Wrong Response`,
			wantErr:      cmpopts.AnyError,
		},
		{
			name:         "NoPIDForProcess",
			fakeResponse: noPidProcessListResponse,
			wantProcStatus: []OSProcess{
				OSProcess{"hdbdaemon", "SAPControl-GREEN", 9609},
				OSProcess{"hdbcompileserver", "SAPControl-GREEN", 9972},
				OSProcess{"hdbindexserver", "SAPControl-GREEN", 0},
			},
			wantErr: nil,
		},
		{
			name:           "NoNameForProcess",
			fakeResponse:   noNameProcessListResponse,
			wantProcStatus: []OSProcess{OSProcess{"hdbdaemon", "SAPControl-GREEN", 9609}, OSProcess{"", "SAPControl-GREEN", 9972}},
			wantErr:        nil,
		},
		{
			name:         "EmptyResponse",
			fakeResponse: emptyProcessListResponse,
			wantErr:      cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupSAPMocks(t, test.fakeResponse)
			c := setupClient(t)
			gotProcesses, gotErr := c.GetProcessList()

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("GetProcessList(), gotErr: %v wantErr: %v.", gotErr, test.wantErr)
			}

			if diff := cmp.Diff(test.wantProcStatus, gotProcesses); diff != "" {
				t.Errorf("GetProcessList() returned unexpected diff (-want +got):\n%v", diff)
			}
		})
	}
}
