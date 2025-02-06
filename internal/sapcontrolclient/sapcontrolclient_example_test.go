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

package sapcontrolclient_test

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"

	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

var (
	//go:embed testdata/getprocesslist/no_name.xml
	processListResponse string

	//go:embed testdata/abapgetwptable/one_work_process.xml
	workProcessResponse string

	//go:embed testdata/getqueuestatistic/one_queue.xml
	taskQueueResponse string
)

// Sapcontrol is a mock server implementation for sapcontrol webservices.
type Sapcontrol struct {
	server   *httptest.Server
	response string
}

// Stop http listener for the mock server.
func (m Sapcontrol) Stop() {
	m.server.Close()
}

// NewSapControl returns a new mock for sapcontrol.
// socket is the socket path the mock sapcontrol is supposed to listen on.
func NewSapControl(socket, response string) (*Sapcontrol, error) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(response))
	})

	// Create test server.
	s := httptest.NewUnstartedServer(handler)

	// Create a listener on a unix socket.
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}

	// Replace default listener with socket listener.
	s.Listener.Close()
	s.Listener = l
	s.Start()
	return &Sapcontrol{s, response}, nil
}

func setupSAPMocks(response string) *Sapcontrol {
	mock, err := NewSapControl("/tmp/.sapstream51013", response)
	if err != nil {
		log.Logger.Fatalf("setupSAPMocks(): %v", err)
	}
	return mock
}

// Example to get response from GetProcessList SAPControl webmethod.
func ExampleClient_GetProcessList() {
	// Setting up mock SAP server to return fake API response.
	mock := setupSAPMocks(processListResponse)

	sc := sapcontrolclient.New("10")
	// This is a testable example, so fmt.Println is used instead of log.Logger.
	fmt.Println(sc.GetProcessList())

	// Stopping the mock SAP server.
	mock.Stop()
	// Output:
	// [{hdbdaemon SAPControl-GREEN 9609} { SAPControl-GREEN 9972}] <nil>
}

// Example to get response from ABAPGetWPTable SAPControl webmethod.
func ExampleClient_ABAPGetWPTable() {
	// Setting up mock SAP server to return fake API response.
	mock := setupSAPMocks(workProcessResponse)

	sc := sapcontrolclient.New("10")
	// This is a testable example, so fmt.Println is used instead of log.Logger.
	fmt.Println(sc.ABAPGetWPTable())

	// Stopping the mock SAP server.
	mock.Stop()
	// Output:
	//[{0 DIA 12723 Run 4 }] <nil>
}

// Example to get response from GetQueueStatistic SAPControl webmethod.
func ExampleClient_GetQueueStatistic() {
	// Setting up mock SAP server to return fake API response.
	mock := setupSAPMocks(taskQueueResponse)

	sc := sapcontrolclient.New("10")
	// This is a testable example, so fmt.Println is used instead of log.Logger.
	fmt.Println(sc.GetQueueStatistic())

	// Stopping the mock SAP server.
	mock.Stop()
	// Output:
	// [{ABAP/NOWP 7 2}] <nil>
}
