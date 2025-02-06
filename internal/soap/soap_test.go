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

package soap

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

type request struct {
	XMLName xml.Name `xml:"TestRequest"`
}

type simpleResponse struct {
	XMLName xml.Name `xml:"TestResponse"`
	Success bool     `xml:"Success"`
}

type integerResponse struct {
	XMLName xml.Name `xml:"TestResponse"`
	Pid     int64    `xml:"Pid"`
}

type complexResponse struct {
	XMLName xml.Name `xml:"TestResponse"`
	Content content  `xml:"Content"`
}

type content struct {
	Name  string   `xml:"Name"`
	Items []string `xml:"Item"`
}

type httpResponseXML struct {
	XMLResp        string
	httpStatusCode int
}

var httpResponses = map[string]*httpResponseXML{
	"simpleResponse": &httpResponseXML{
		XMLResp: `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
			<soap:Body>
				<TestResponse>
					<Success>true</Success>
				</TestResponse>
			</soap:Body>
		</soap:Envelope>`,
		httpStatusCode: http.StatusOK,
	},
	"complexResponse": &httpResponseXML{
		XMLResp: `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
			<soap:Body>
				<TestResponse>
					<Content>
						<Name>Test Name</Name>
						<Item>Item1</Item>
						<Item>Item2</Item>
					</Content>
				</TestResponse>
			</soap:Body>
		</soap:Envelope>`,
		httpStatusCode: http.StatusOK,
	},
	"faultResponse": &httpResponseXML{
		XMLResp: `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
			<soap:Body>
				<Fault>
					<faultcode>In message exchange</faultcode>
					<faultstring>Method not implemented</faultstring>
				</Fault>
			</soap:Body>
		</soap:Envelope>`,
		httpStatusCode: http.StatusOK,
	},
	"integerOverflowResponse": &httpResponseXML{
		XMLResp: `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
			<soap:Body>
				<TestResponse>
					<Name>Integer Overflow</Name>
					<Pid>1000000000000000000000</Pid>
			</TestResponse>
			</soap:Body>
		</soap:Envelope>`,
		httpStatusCode: http.StatusOK,
	},
	"invalidIntResponse": &httpResponseXML{
		XMLResp: `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
			<soap:Body>
				<TestResponse>
					<Name>Integer Overflow</Name>
					<Pid>This is not an integer.</Pid>
			</TestResponse>
			</soap:Body>
		</soap:Envelope>`,
		httpStatusCode: http.StatusOK,
	},
	"httpErrorResponse": &httpResponseXML{
		XMLResp:        `Bad Request`,
		httpStatusCode: http.StatusBadRequest,
	},
	"emptyResponse": &httpResponseXML{
		XMLResp: `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
		</soap:Envelope>`,
		httpStatusCode: http.StatusOK,
	},
}

type errWriter struct{}

func (errWriter) Write(p []byte) (n int, err error) { return 0, fmt.Errorf("unwritable") }

// Test marshaling the request envelope with fakeEncoder.
func TestMarshalEnvelope(t *testing.T) {
	fakeEncoder := xml.NewEncoder(errWriter{})
	wantErrMess := "failed to marshal SOAP Envelope: unwritable"

	env := Envelope{}
	buffer := new(bytes.Buffer)
	_, err := marshalEnvelope(buffer, env, fakeEncoder)
	if err == nil {
		t.Errorf("soap.Client.marshalEnvelope(%v, %v, %v) = %v, want %v", buffer, env, fakeEncoder, err, wantErrMess)
	}
}

// Test creating UDSClient and performing incorrect http post request.
func TestPostError(t *testing.T) {
	tests := []struct {
		desc      string
		clientURL string
	}{
		{
			desc:      "Error creating new HTTP request.",
			clientURL: "Invalid URL\n",
		},
		{
			desc:      "Error performing HTTP request.",
			clientURL: "Incorrect URL",
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			s := newUDSMockServer(writeHTTPResponse(httpResponses["simpleResponse"]), t)
			s.Start()
			defer s.Close()

			client := NewUDSClient("/tmp/test.sock")
			client.url = test.clientURL
			gotErr := client.Call(&request{}, &simpleResponse{})
			if gotErr == nil {
				t.Errorf("Wanted HTTP post request error. Got nil error.")
			}
		})
	}
}

// Test to check working of parseSoapBody() with incorrect parameters.
func TestParseSoapBody(t *testing.T) {
	tests := []struct {
		desc         string
		respEnvelope *Envelope
		wantErrMess  string
	}{
		{
			desc: "Fault message",
			respEnvelope: &Envelope{
				Body: Body{Content: []byte("unmarshal error")},
			},
			wantErrMess: "failed to unmarshal fault element for response type(*soap.complexResponse): Unmarshal error",
		},
		{
			desc: "Respbody error message",
			respEnvelope: &Envelope{
				Body: Body{Content: []byte(httpResponses["simpleResponse"].XMLResp)},
			},
			wantErrMess: "failed to unmarshal SOAP Body for response type(*soap.complexResponse): expected element type <TestResponse> but have <Envelope>",
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			err := parseSoapBody(test.respEnvelope, &complexResponse{})

			if err == nil {
				t.Errorf("soap.Client.parseSoapBody(%v) = %v, want %v", test.respEnvelope, err, test.wantErrMess)
			}
		})
	}
}

func TestCall(t *testing.T) {
	tests := []struct {
		desc         string
		httpHandler  http.HandlerFunc
		req          any
		respBody     any
		wantRespBody any
		wantErr      error
		xh           any
	}{
		{
			// Test creating UDSClient and performing simple client.Call
			desc:         "simpleResponse",
			httpHandler:  writeHTTPResponse(httpResponses["simpleResponse"]),
			req:          &request{},
			respBody:     &simpleResponse{},
			wantRespBody: &simpleResponse{Success: true},
			wantErr:      nil,
		},
		{
			// Test performing soap.call successful unmarshaling for response with complex content type.
			desc:        "complexResponse",
			httpHandler: writeHTTPResponse(httpResponses["complexResponse"]),
			req:         &request{},
			respBody:    &complexResponse{},
			wantRespBody: &complexResponse{Content: content{
				Name:  "Test Name",
				Items: []string{"Item1", "Item2"},
			},
			},
			wantErr: nil,
		},
		{
			// Test performing soap.call unsuccessful unmarshaling for response with integer overflow.
			desc:         "integerOverflowError",
			httpHandler:  writeHTTPResponse(httpResponses["integerOverflowResponse"]),
			req:          &request{},
			respBody:     &integerResponse{},
			wantRespBody: &integerResponse{},
			wantErr:      cmpopts.AnyError,
		},
		{
			// Test performing soap.call unsuccessful unmarshaling for response with wrong field type.
			desc:         "invalidFieldTypeError",
			httpHandler:  writeHTTPResponse(httpResponses["invalidIntResponse"]),
			req:          &request{},
			respBody:     &integerResponse{},
			wantRespBody: &integerResponse{},
			wantErr:      cmpopts.AnyError,
		},
		{
			// Test performing soap.call unsuccessful marshaling for invalid request.
			desc:         "marshalError",
			httpHandler:  writeHTTPResponse(httpResponses["complexResponse"]),
			req:          map[int]string{1: "Invalid request"},
			respBody:     &complexResponse{},
			wantRespBody: &complexResponse{},
			wantErr:      cmpopts.AnyError,
		},
		{
			// Test client.call successful unmarshaling for SOAP Fault response.
			desc:         "faultResponse",
			httpHandler:  writeHTTPResponse(httpResponses["faultResponse"]),
			req:          &request{},
			respBody:     &complexResponse{},
			wantRespBody: &complexResponse{},
			wantErr: Fault{
				Code:   "In message exchange",
				String: "Method not implemented",
			},
		},
		{
			// Test client.call successful parsing for HTTP error response.
			desc:         "httpErrorResponse",
			httpHandler:  writeHTTPResponse(httpResponses["httpErrorResponse"]),
			req:          &request{},
			respBody:     &complexResponse{},
			wantRespBody: &complexResponse{},
			wantErr:      cmpopts.AnyError,
		},
		{
			// Test client.call empty response.
			desc:         "emptyResponse",
			httpHandler:  writeHTTPResponse(httpResponses["emptyResponse"]),
			req:          &request{},
			respBody:     &complexResponse{},
			wantRespBody: &complexResponse{},
			wantErr:      cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			s := newUDSMockServer(test.httpHandler, t)
			s.Start()
			defer s.Close()

			client := NewUDSClient("/tmp/test.sock")
			req := test.req
			gotErr := client.Call(req, test.respBody)
			opts := []cmp.Option{
				cmpopts.IgnoreFields(simpleResponse{}, "XMLName"),
				cmpopts.IgnoreFields(integerResponse{}, "XMLName"),
				cmpopts.IgnoreFields(complexResponse{}, "XMLName"),
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Error mismatch client.Call(%v, %v) = %v, want %v.", req, test.respBody, gotErr, test.wantErr)
			}
			if !cmp.Equal(test.respBody, test.wantRespBody, opts...) {
				t.Errorf("Response mismatch client.Call(%v, %v) = %v, want %v.", req, test.respBody, test.respBody, test.wantRespBody)
			}
		})
	}
}

func newUDSMockServer(handler http.HandlerFunc, t *testing.T) *httptest.Server {
	t.Helper()
	// Create a listener on a unix socket.
	l, err := net.Listen("unix", "/tmp/test.sock")
	if err != nil {
		t.Fatal(err)
	}

	// Create test server.
	s := httptest.NewUnstartedServer(handler)

	// Replace default listener with socket listener.
	s.Listener.Close()
	s.Listener = l

	return s
}

func writeHTTPResponse(httpRespXML *httpResponseXML) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(httpRespXML.httpStatusCode)
		w.Write([]byte(httpRespXML.XMLResp))
	})
}
