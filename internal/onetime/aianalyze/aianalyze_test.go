/*
Copyright 2024 Google LLC

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

package aianalyze

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem/fake"

	cpb "cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	defaultCloudProps = &ipb.CloudProperties{
		ProjectId:    "test-project",
		Zone:         "test-zone-a",
		InstanceName: "test-instance",
	}

	samplePacemakerData = `Jul 18 20:42:08.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ + digit_re='^[0-9]+$' ]
Jul 18 20:42:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ + digit_re='^[0-9]+$' ]
Jul 18 20:42:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ + [[ 4 =~ ^[0-9]+$ ]] ]
Jul 18 20:42:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ + image= ]
Jul 18 20:42:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ + get_project ]
Jul 18 20:42:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ ++ /usr/bin/gcloud config get project ]
Jul 18 20:42:47.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ ++ /usr/bin/gcloud config get project ]
Jul 18 20:43:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ ++ /usr/bin/gcloud config get project ]
	`

	sampleNameserverData = `[31641]{-1}[-1/-1] 2024-07-18 20:30:01.601382 i ha_dr_SAPHanaSR  SAPHanaSR.py(00074) : SAPHanaSR.srConnectionChanged() CALLING CRM: <sudo /usr/sbin/crm_attribute -n hana_tst_site_srHook_vm-ha2 -v SOK -t crm_config -s SAPHanaSR> rc=0
[31641]{-1}[-1/-1] 2024-07-18 20:30:21.601382 i ha_dr_SAPHanaSR  SAPHanaSR.py(00074) : SAPHanaSR.srConnectionChanged() CALLING CRM: <sudo /usr/sbin/crm_attribute -n hana_tst_site_srHook_vm-ha2 -v SOK -t crm_config -s SAPHanaSR> rc=0
[31641]{-1}[-1/-1] 2024-07-18 20:30:31.601382 i ha_dr_SAPHanaSR  SAPHanaSR.py(00074) : SAPHanaSR.srConnectionChanged() CALLING CRM: <sudo /usr/sbin/crm_attribute -n hana_tst_site_srHook_vm-ha2 -v SOK -t crm_config -s SAPHanaSR> rc=0
[31641]{-1}[-1/-1] 2024-07-18 20:30:31.601454 i ha_dr_provider   PythonProxyImpl.cpp(01113) : calling HA/DR provider susChkSrv.hookDRConnectionChanged(hostname=vm-ha1, port=30003, volume=3, service_name=indexserver, database=TST, status=15, database_status=15, system_status=15, timestamp=2024-07-18T20:30:31.545490+00:00, is_in_sync=1, system_is_in_sync=1, reason=, siteName=vm-ha2)
[31892]{-1}[-1/-1] 2024-07-18 20:30:31.601567 i sr_nameserver    DRRequestHandler.cpp(01659) : drConnectionChanged: 3:3 2(vm-ha2): isInSync=1, HADRProvider hook executed rc=0
[31892]{-1}[-1/-1] 2024-07-18 20:32:31.601567 i sr_nameserver    DRRequestHandler.cpp(01659) : drConnectionChanged: 3:3 2(vm-ha2): isInSync=1, HADRProvider hook executed rc=0
`

	errorHTTPGet = func(url string) (*http.Response, error) {
		return nil, cmpopts.AnyError
	}
	successHTTPGet = func(url string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &MockReadCloser{data: []byte("Test Data")},
		}, nil
	}
)

// MockReadCloser is a mock implementation of io.ReadCloser for http.Response.
type (
	MockReadCloser struct {
		data   []byte // The data to return on Read
		err    error  // The error to return on Read or Close
		closed bool   // Flag to track if Close has been called
	}

	mockRestService struct {
		getResponseResp []byte
		getResponseErr  error
	}
)

func (mrc *MockReadCloser) Read(p []byte) (n int, err error) {
	if mrc.err != nil {
		return 0, mrc.err
	}
	n = copy(p, mrc.data)
	return n, nil
}

func (mrc *MockReadCloser) Close() error {
	if mrc.err != nil {
		return mrc.err
	}
	mrc.closed = true
	return nil
}

func (m *mockRestService) NewRest() {}

func (m *mockRestService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error) {
	return m.getResponseResp, m.getResponseErr
}

func TestSupportAnalyzerHandler(t *testing.T) {
	tests := []struct {
		name       string
		a          *AiAnalyzer
		wantResult string
		wantStatus subcommands.ExitStatus
	}{
		{
			name: "FailureValidation",
			a: &AiAnalyzer{
				Sid:     "",
				Project: "test-project",
				Region:  "test-region",
			},
			wantResult: "Error while validating parameters",
			wantStatus: subcommands.ExitUsageError,
		},
		{
			name: "FailureOverview",
			a: &AiAnalyzer{
				Sid:               "test-sid",
				InstanceNumber:    "00",
				SupportBundlePath: "/test-support-bundle-path",
				Project:           "test-project",
				Region:            "test-region",
				rest: &mockRestService{
					getResponseErr: cmpopts.AnyError,
				},
			},
			wantResult: "Error while getting overview",
			wantStatus: subcommands.ExitFailure,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.a.oteLogger = onetime.CreateOTELogger(false)
			gotResult, gotStatus := tc.a.supportAnalyzerHandler(ctx, &onetime.RunOptions{CloudProperties: defaultCloudProps})
			if gotResult != tc.wantResult {
				t.Errorf("supportAnalyzerHandler() returned unexpected result: %v, want: %v", gotResult, tc.wantResult)
			}
			if gotStatus != tc.wantStatus {
				t.Errorf("supportAnalyzerHandler() returned unexpected status: %v, want: %v", gotStatus, tc.wantStatus)
			}
		})
	}
}

func TestProtosToJSONList(t *testing.T) {
	tests := []struct {
		name        string
		contentList []*cpb.Content
		want        []byte
		wantErr     error
	}{
		{
			name: "Failure",
			contentList: []*cpb.Content{
				&cpb.Content{
					Role:  "user",
					Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: string([]byte{0xff, 0xfe, 0xfd})}}},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			contentList: []*cpb.Content{
				&cpb.Content{
					Role:  "user",
					Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: "Test Prompt"}}},
				},
			},
			want: []byte(`{"contents":[{"role":"user","parts":[{"text":"Test Prompt"}]}]}`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := protosToJSONList(context.Background(), test.contentList)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("protosToJSONList(%v) returned unexpected error: %v, want: %v", test.contentList, err, test.wantErr)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("protosToJSONList(%v) returned unexpected diff (-want +got):\n%s", test.contentList, diff)
			}
		})
	}
}

func TestGetOverview(t *testing.T) {
	tests := []struct {
		name    string
		a       *AiAnalyzer
		wantErr error
	}{
		{
			name: "Success",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseResp: []byte(`[{"candidates": [{"content": {"role": "user", "parts": [{"text": "Test prompt"}]}}]}]`),
					getResponseErr:  nil,
				},
			},
			wantErr: nil,
		},
		{
			name: "Failure",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseErr: cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			err := test.a.getOverview(context.Background())
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getOverview() returned unexpected error: %v, want: %v", err, test.wantErr)
			}
		})
	}
}

func TestGenerateContentREST(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "success") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `[{"candidates": [{"content": {"role": "user", "parts": [{"text": "Test prompt"}]}}]}]`)
		} else if strings.Contains(r.URL.Path, "error") {
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
		} else if strings.Contains(r.URL.Path, "unmarshal_error") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"Candidate": [{"content": "incorrect_field"}]`)
		}
	}))
	defer ts.Close()

	tests := []struct {
		name    string
		a       *AiAnalyzer
		data    []byte
		want    []generateContentResponse
		wantErr error
	}{
		{
			name: "GetResponseError",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseErr: cmpopts.AnyError,
				},
			},
			data:    []byte(`{"contents":[{"role":"user","parts":[{"text":"Test prompt?"}]}]}`),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UnmarshalError",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseResp: []byte(`{"Candidate": [{"content": "incorrect_field"}]`),
					getResponseErr:  nil,
				},
			},
			data:    []byte(`{"contents":[{"role":"user","parts":[{"text":"Test Prompt?"}]}]}`),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseResp: []byte(`[{"candidates": [{"content": {"role": "user", "parts": [{"text": "Test prompt answer"}]}}]}]`),
					getResponseErr:  nil,
				},
			},
			data: []byte(`{"contents":[{"role":"user","parts":[{"text":"Test prompt?"}]}]}`),
			want: []generateContentResponse{
				generateContentResponse{
					Candidates: []candidate{
						candidate{
							Content: content{
								Role:  "user",
								Parts: []part{part{Text: "Test prompt answer"}},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			got, err := test.a.generateContentREST(context.Background(), test.data)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("generateContentREST(%v) returned unexpected error: %v, want: %v", test.data, err, test.wantErr)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("generateContentREST(%v) returned unexpected diff (-want +got):\n%s", test.data, diff)
			}
		})
	}
}

func TestValidateParametersErrors(t *testing.T) {
	tests := []struct {
		name    string
		a       *AiAnalyzer
		cp      *ipb.CloudProperties
		wantErr error
	}{
		{
			name: "NoSID",
			a: &AiAnalyzer{
				Sid:     "",
				Project: "test-project",
				Region:  "test-region",
			},
			cp:      defaultCloudProps,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoInstanceNumber",
			a: &AiAnalyzer{
				Sid:     "test-sid",
				Project: "test-project",
				Region:  "test-region",
			},
			cp:      defaultCloudProps,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoSupportBundlePath",
			a: &AiAnalyzer{
				Sid:            "test-sid",
				InstanceNumber: "00",
				Project:        "test-project",
				Region:         "test-region",
			},
			cp:      defaultCloudProps,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoProject",
			a: &AiAnalyzer{
				Sid:               "test-sid",
				InstanceNumber:    "00",
				SupportBundlePath: "/test-support-bundle-path",
				Project:           "",
			},
			cp:      defaultCloudProps,
			wantErr: nil,
		},
		{
			name: "NoRegionInvalid",
			a: &AiAnalyzer{
				Sid:               "test-sid",
				InstanceNumber:    "00",
				SupportBundlePath: "/test-support-bundle-path",
				Project:           "test-project",
			},
			cp: &ipb.CloudProperties{
				ProjectId:  "test-project",
				Zone:       "invalid-zone",
				InstanceId: "test-instance",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoRegionValid",
			a: &AiAnalyzer{
				Sid:               "test-sid",
				InstanceNumber:    "00",
				SupportBundlePath: "/test-support-bundle-path",
				Project:           "test-project",
			},
			cp:      defaultCloudProps,
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.a.validateParameters(ctx, tc.cp)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("validateParameters(%v, %v) returned unexpected diff (-want +got):\n%s", tc.a, tc.cp, diff)
			}
		})
	}
}

func TestExtractTimeWindowFromText(t *testing.T) {
	tests := []struct {
		name        string
		a           *AiAnalyzer
		text        string
		isPacemaker bool
		wantResult  string
		wantErr     error
	}{
		{
			name: "InvalidTimestamp",
			a: &AiAnalyzer{
				Timestamp: "Jan 02 15:04:05.000",
			},
			text:        samplePacemakerData,
			isPacemaker: true,
			wantResult:  "",
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "PacemakerTimeParseErr",
			a: &AiAnalyzer{
				Timestamp:         "2024-07-18 20:42:31",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			text:        samplePacemakerData + "\nJul 38 20:42:38.044 vm1-ha1 pacemaker-fenced    [3442] (log_action) 	warning: fence_legacy[4764] stderr: [ + digit_re='^[0-9]+$' ]",
			isPacemaker: true,
			wantResult:  samplePacemakerData,
			wantErr:     nil,
		},
		{
			name: "SuccessPacemaker1",
			a: &AiAnalyzer{
				Timestamp:         "2024-07-18 20:42:38",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			text:        samplePacemakerData,
			isPacemaker: true,
			wantResult:  samplePacemakerData,
			wantErr:     nil,
		},
		{
			name: "SuccessPacemaker2",
			a: &AiAnalyzer{
				Timestamp:         "2024-07-18 20:42:38.044",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			text:        samplePacemakerData + "\nSAPHanaTopology(rsc_SAPHanaTopology_TST_HDB00)[5863]:	2024/07/18_20:42:46 INFO: RA ==== begin action monitor_clone (0.162.3) (2s)====",
			isPacemaker: true,
			wantResult:  samplePacemakerData + "\nSAPHanaTopology(rsc_SAPHanaTopology_TST_HDB00)[5863]:	2024/07/18_20:42:46 INFO: RA ==== begin action monitor_clone (0.162.3) (2s)====",
			wantErr:     nil,
		},
		{
			name: "NameserverTimeParseErr",
			a: &AiAnalyzer{
				Timestamp:         "2024-07-18 20:30:31",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			text:        sampleNameserverData + "\n[31641]{-1}[-1/-1] 2024-07-38 20:30:31.601382 i ha_dr_SAPHanaSR  SAPHanaSR.py(00074) : SAPHanaSR.srConnectionChanged() CALLING CRM: <sudo /usr/sbin/crm_attribute -n hana_tst_site_srHook_vm-ha2 -v SOK -t crm_config -s SAPHanaSR> rc=0",
			isPacemaker: false,
			wantResult:  sampleNameserverData,
			wantErr:     nil,
		},
		{
			name: "SuccessNameserver",
			a: &AiAnalyzer{
				Timestamp:         "2024-07-18 20:30:31",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			text:        sampleNameserverData,
			isPacemaker: false,
			wantResult:  sampleNameserverData,
			wantErr:     nil,
		},
		{
			name: "NoWindow",
			a: &AiAnalyzer{
				Timestamp:         "2024-07-17 20:30:31",
				BeforeEventWindow: 10,
				AfterEventWindow:  10,
			},
			text:        sampleNameserverData,
			isPacemaker: false,
			wantResult:  "",
			wantErr:     nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.a.extractTimeWindowFromText(ctx, tc.text, tc.isPacemaker)
			fmt.Println("got", got)
			fmt.Println("err", err)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("extractTimeWindowFromText() returned unexpected diff (-want +got):\n%s", diff)
			}
			if got != tc.wantResult {
				t.Errorf("extractTimeWindowFromText() = %v, want: %v", got, tc.wantResult)
			}
		})
	}
}

func TestFilterNameserverTraces(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     bool
	}{
		{
			name:     "InvalidPrefix",
			fileName: "fake_nameserver_00000.trc",
			want:     false,
		},
		{
			name:     "InvalidExt",
			fileName: "nameserver_00000.log",
			want:     false,
		},
		{
			name:     "InvalidParts",
			fileName: "nameserver_00000.trc",
			want:     false,
		},
		{
			name:     "Contains00000",
			fileName: "nameserver_00000.trc",
			want:     false,
		},
		{
			name:     "Valid",
			fileName: "nameserver_check-vm.30001.000.trc",
			want:     true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterNameserverTraces(ctx, tc.fileName)
			if got != tc.want {
				t.Errorf("filterNameserverTraces(%q) = %v, want: %v", tc.fileName, got, tc.want)
			}
		})
	}
}

func TestFetchNameserverTraces(t *testing.T) {
	tests := []struct {
		name       string
		a          *AiAnalyzer
		wantTraces string
		wantErr    error
	}{
		{
			name: "ReadDirError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadDirResp: [][]fs.FileInfo{[]os.FileInfo{}},
					ReadDirErr:  []error{cmpopts.AnyError},
				},
			},
			wantTraces: "",
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "ReadFileError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadDirResp: [][]fs.FileInfo{[]os.FileInfo{
						fake.FileInfo{FakeName: "nameserver_check-vm.30001.000.trc", FakeIsDir: true},
						fake.FileInfo{FakeName: "nameserver_check-vm.30001.001.trc", FakeIsDir: false},
					}},
					ReadDirErr:   []error{nil},
					ReadFileResp: [][]byte{nil},
					ReadFileErr:  []error{cmpopts.AnyError},
				},
			},
			wantTraces: "",
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "ExtractTimeWindowFromTextError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadDirResp: [][]fs.FileInfo{[]os.FileInfo{
						fake.FileInfo{FakeName: "nameserver_check-vm.30001.000.trc", FakeIsDir: true},
						fake.FileInfo{FakeName: "nameserver_check-vm.30001.001.trc", FakeIsDir: false},
					}},
					ReadDirErr:   []error{nil},
					ReadFileResp: [][]byte{[]byte(sampleNameserverData)},
					ReadFileErr:  []error{nil},
				},
				Timestamp:         "2024-07-37 20:30:31",
				BeforeEventWindow: 10,
				AfterEventWindow:  10,
			},
			wantTraces: "",
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "Success",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadDirResp: [][]fs.FileInfo{[]os.FileInfo{
						fake.FileInfo{FakeName: "nameserver_check-vm.30001.000.trc", FakeIsDir: true},
						fake.FileInfo{FakeName: "nameserver_check-vm.30001.001.trc", FakeIsDir: false},
					}},
					ReadDirErr:   []error{nil},
					ReadFileResp: [][]byte{[]byte(sampleNameserverData)},
					ReadFileErr:  []error{nil},
				},
				Timestamp:         "2024-07-18 20:30:31",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			wantTraces: sampleNameserverData + "\n",
			wantErr:    nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			got, err := test.a.fetchNameserverTraces(context.Background())

			if got != test.wantTraces {
				t.Errorf("fetchNameserverTraces() = %v, want: %v", got, test.wantTraces)
			}
			if diff := cmp.Diff(err, test.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("fetchNameserverTraces() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFetchPacemakerLogs(t *testing.T) {
	tests := []struct {
		name     string
		a        *AiAnalyzer
		wantLogs string
		wantErr  error
	}{
		{
			name: "ReadFileError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadFileResp: [][]byte{nil},
					ReadFileErr:  []error{cmpopts.AnyError},
				},
			},
			wantLogs: "",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "ExtractTimeWindowFromTextError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadFileResp: [][]byte{[]byte(samplePacemakerData)},
					ReadFileErr:  []error{nil},
				},
				Timestamp:         "2024-07-37 20:42:31",
				BeforeEventWindow: 10,
				AfterEventWindow:  10,
			},
			wantLogs: "",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "Success",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					ReadFileResp: [][]byte{[]byte(samplePacemakerData)},
					ReadFileErr:  []error{nil},
				},
				Timestamp:         "2024-07-18 20:42:31",
				BeforeEventWindow: 4500,
				AfterEventWindow:  1800,
			},
			wantLogs: samplePacemakerData,
			wantErr:  nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			got, err := test.a.fetchPacemakerLogs(context.Background())

			if got != test.wantLogs {
				t.Errorf("fetchPacemakerLogs() = %v, want: %v", got, test.wantLogs)
			}
			if diff := cmp.Diff(err, test.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("fetchPacemakerLogs() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDownloadLogparser(t *testing.T) {
	tests := []struct {
		name    string
		a       *AiAnalyzer
		httpGet httpGet
		wantErr error
	}{
		{
			name: "StatError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					StatResp: []os.FileInfo{nil},
					StatErr:  []error{cmpopts.AnyError},
				},
			},
			httpGet: errorHTTPGet,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AlreadyExists",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					StatResp: []os.FileInfo{nil},
					StatErr:  []error{os.ErrExist},
				},
			},
			httpGet: errorHTTPGet,
			wantErr: nil,
		},
		{
			name: "OpenFileError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					StatResp:     []os.FileInfo{nil},
					StatErr:      []error{nil},
					OpenFileResp: []*os.File{nil},
					OpenFileErr:  []error{cmpopts.AnyError},
				},
			},
			httpGet: errorHTTPGet,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DownloadError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					StatResp:     []os.FileInfo{nil},
					StatErr:      []error{nil},
					OpenFileResp: []*os.File{nil},
					OpenFileErr:  []error{nil},
				},
			},
			httpGet: errorHTTPGet,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CopyError",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					StatResp:     []os.FileInfo{nil},
					StatErr:      []error{nil},
					OpenFileResp: []*os.File{nil},
					OpenFileErr:  []error{nil},
					CopyResp:     []int64{0},
					CopyErr:      []error{cmpopts.AnyError},
				},
			},
			httpGet: successHTTPGet,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			a: &AiAnalyzer{
				fs: &fake.FileSystem{
					StatResp:     []os.FileInfo{nil},
					StatErr:      []error{nil},
					OpenFileResp: []*os.File{nil},
					OpenFileErr:  []error{nil},
					CopyResp:     []int64{0},
					CopyErr:      []error{nil},
				},
			},
			httpGet: successHTTPGet,
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			err := test.a.downloadLogparser(context.Background(), test.httpGet)
			if diff := cmp.Diff(err, test.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("downloadLogparser() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
