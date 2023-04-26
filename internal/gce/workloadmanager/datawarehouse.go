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

// TODO: b/277773712 - Replace this with the public version on github when the API is GA

// Package workloadmanager contains types and functions to interact with the various WorkloadManager cloud APIs.
package workloadmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"runtime"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

const basePath = "https://autopush-workloadmanager-datawarehouse.sandbox.googleapis.com/v1/"
const mtlsBasePath = "https://workloadmanager.mtls.googleapis.com/v1alpha1/"

type (
	// SendRequest is used to send HTTP requests and return the response.
	SendRequest func(context.Context, *http.Client, *http.Request) (*http.Response, error)
)

func sendRequest(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	// Disallow Accept-Encoding because it interferes with the automatic gzip handling
	// done by the default http.Transport. See https://github.com/google/google-api-go-client/issues/219.
	if _, ok := req.Header["Accept-Encoding"]; ok {
		return nil, errors.New("google api: custom Accept-Encoding headers not allowed")
	}
	if ctx == nil {
		return client.Do(req)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req.WithContext(ctx))
	// If we got an error, and the context has been canceled,
	// the context's error is probably more useful.
	if err != nil && ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return resp, err
}

func setOptions(u map[string][]string, opts ...googleapi.CallOption) {
	for _, o := range opts {
		m, ok := o.(googleapi.MultiCallOption)
		if ok {
			k, v := m.GetMulti()
			u[k] = v
			continue
		}
		k, v := o.Get()
		u[k] = []string{v}
	}
}

// NewService creates a new WorkloadManager Service.
func NewService(ctx context.Context, opts ...option.ClientOption) (*Service, error) {
	scopesOption := internaloption.WithDefaultScopes(
		"https://www.googleapis.com/auth/cloud-platform",
	)
	// NOTE: prepend, so we don't override user-specified scopes.
	opts = append([]option.ClientOption{scopesOption}, opts...)
	opts = append(opts, internaloption.WithDefaultEndpoint(basePath))
	opts = append(opts, internaloption.WithDefaultMTLSEndpoint(mtlsBasePath))
	client, endpoint, err := htransport.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	s := &Service{client: client, BasePath: basePath, SendRequest: sendRequest}
	if endpoint != "" {
		s.BasePath = endpoint
	}
	return s, nil
}

// Service handles connecting to a version of the Data Warehouse API defined by BasePath.
type Service struct {
	client      *http.Client
	BasePath    string // API endpoint base URL
	UserAgent   string // optional additional User-Agent fragment
	SendRequest SendRequest
}

func (s *Service) userAgent() string {
	if s.UserAgent == "" {
		return googleapi.UserAgent
	}
	return googleapi.UserAgent + " " + s.UserAgent
}

// Insight is a presentation of host resource usage where the workload
// runs.

type Insight struct {
	// SapDiscovery: The insights data for sap system discovery. This is a
	// copy of SAP System proto and should get updated whenever that one
	// changes.
	SapDiscovery *SapDiscovery `json:"sapDiscovery,omitempty"`
	// SentTime: Output only. [Output only] Create time stamp
	SentTime string `json:"sentTime,omitempty"`
	// ForceSendFields is a list of field names (e.g. "SapDiscovery") to
	// unconditionally include in API requests. By default, fields with
	// empty or default values are omitted from API requests. However, any
	// non-pointer, non-interface field appearing in ForceSendFields will be
	// sent to the server regardless of whether the field is empty or not.
	// This may be used to include empty fields in Patch requests.
	ForceSendFields []string `json:"-"`
	// NullFields is a list of field names (e.g. "SapDiscovery") to include
	// in API requests with the JSON null value. By default, fields with
	// empty values are omitted from API requests. However, any field with
	// an empty value appearing in NullFields will be sent to the server as
	// null. It is an error if a field in this list has a non-empty value.
	// This may be used to include null fields in Patch requests.
	NullFields []string `json:"-"`
}

// SapDiscovery is the schema of SAP system discovery data.
type SapDiscovery struct {
	// ApplicationLayer: An SAP system may run without an application layer.
	ApplicationLayer *SapDiscoveryComponent `json:"applicationLayer,omitempty"`
	// DatabaseLayer: An SAP System must have a database.
	DatabaseLayer *SapDiscoveryComponent `json:"databaseLayer,omitempty"`
	// Metadata: The metadata for SAP system discovery data.
	Metadata *SapDiscoveryMetadata `json:"metadata,omitempty"`
	// SystemID: A combination of database SID, database instance URI and
	// tenant DB name to make a unique identifier per-system.
	SystemID string `json:"systemId,omitempty"`
	// UpdateTime: Unix timestamp this system has been updated last.
	UpdateTime string `json:"updateTime,omitempty"`
	// ForceSendFields is a list of field names (e.g. "ApplicationLayer") to
	// unconditionally include in API requests. By default, fields with
	// empty or default values are omitted from API requests. However, any
	// non-pointer, non-interface field appearing in ForceSendFields will be
	// sent to the server regardless of whether the field is empty or not.
	// This may be used to include empty fields in Patch requests.
	ForceSendFields []string `json:"-"`
	// NullFields is a list of field names (e.g. "ApplicationLayer") to
	// include in API requests with the JSON null value. By default, fields
	// with empty values are omitted from API requests. However, any field
	// with an empty value appearing in NullFields will be sent to the
	// server as null. It is an error if a field in this list has a
	// non-empty value. This may be used to include null fields in Patch
	// requests.
	NullFields []string `json:"-"`
}

// SapDiscoveryComponent is an organized set of SAP System resources.
type SapDiscoveryComponent struct {
	// ApplicationType: The component is a SAP application.
	ApplicationType string `json:"applicationType,omitempty"`
	// DatabaseType: The component is a SAP database.
	DatabaseType string `json:"databaseType,omitempty"`
	// HostProject: Pantheon Project in which the resources reside.
	HostProject string `json:"hostProject,omitempty"`
	// Resources: The resources in a component.
	Resources []*SapDiscoveryResource `json:"resources,omitempty"`
	// Sid: The sap identifier, used by the SAP software and helps
	// differentiate systems for customers.
	Sid string `json:"sid,omitempty"`
	// ForceSendFields is a list of field names (e.g. "ApplicationType") to
	// unconditionally include in API requests. By default, fields with
	// empty or default values are omitted from API requests. However, any
	// non-pointer, non-interface field appearing in ForceSendFields will be
	// sent to the server regardless of whether the field is empty or not.
	// This may be used to include empty fields in Patch requests.
	ForceSendFields []string `json:"-"`
	// NullFields is a list of field names (e.g. "ApplicationType") to
	// include in API requests with the JSON null value. By default, fields
	// with empty values are omitted from API requests. However, any field
	// with an empty value appearing in NullFields will be sent to the
	// server as null. It is an error if a field in this list has a
	// non-empty value. This may be used to include null fields in Patch
	// requests.
	NullFields []string `json:"-"`
}

// SapDiscoveryMetadata is the metadata for an SAP System
type SapDiscoveryMetadata struct {
	// CustomerRegion: Customer region string for customer's use. Does not
	// represent the GCP region.
	CustomerRegion string `json:"customerRegion,omitempty"`
	// DefinedSystem: Customer defined, something like "E-commerce pre prod"
	DefinedSystem string `json:"definedSystem,omitempty"`
	// EnvironmentType: Should be "prod", "QA", "dev", "staging", etc.
	EnvironmentType string `json:"environmentType,omitempty"`
	// SapProduct: This sap product name
	SapProduct string `json:"sapProduct,omitempty"`
	// ForceSendFields is a list of field names (e.g. "CustomerRegion") to
	// unconditionally include in API requests. By default, fields with
	// empty or default values are omitted from API requests. However, any
	// non-pointer, non-interface field appearing in ForceSendFields will be
	// sent to the server regardless of whether the field is empty or not.
	// This may be used to include empty fields in Patch requests.
	ForceSendFields []string `json:"-"`
	// NullFields is a list of field names (e.g. "CustomerRegion") to
	// include in API requests with the JSON null value. By default, fields
	// with empty values are omitted from API requests. However, any field
	// with an empty value appearing in NullFields will be sent to the
	// server as null. It is an error if a field in this list has a
	// non-empty value. This may be used to include null fields in Patch
	// requests.
	NullFields []string `json:"-"`
}

// SapDiscoveryResource describes a resource within an SAP System.
type SapDiscoveryResource struct {
	// RelatedResources: A list of resource URIs related to this resource.
	RelatedResources []string `json:"relatedResources,omitempty"`
	// ResourceKind: ComputeInstance, ComputeDisk, VPC, Bare Metal server,
	// etc.
	ResourceKind string `json:"resourceKind,omitempty"`
	// ResourceType: The type of this resource.
	//
	// Possible values:
	//   "RESOURCE_TYPE_UNSPECIFIED" - Undefined resource type.
	//   "COMPUTE" - This is a compute resource.
	//   "STORAGE" - This a storage resource.
	//   "NETWORK" - This is a network resource.
	ResourceType string `json:"resourceType,omitempty"`
	// ResourceURI: URI of the resource, includes project, location, and
	// name.
	ResourceURI string `json:"resourceUri,omitempty"`
	// UpdateTime: Unix timestamp of when this resource last had its
	// discovery data updated.
	UpdateTime string `json:"updateTime,omitempty"`
	// ForceSendFields is a list of field names (e.g. "RelatedResources") to
	// unconditionally include in API requests. By default, fields with
	// empty or default values are omitted from API requests. However, any
	// non-pointer, non-interface field appearing in ForceSendFields will be
	// sent to the server regardless of whether the field is empty or not.
	// This may be used to include empty fields in Patch requests.
	ForceSendFields []string `json:"-"`
	// NullFields is a list of field names (e.g. "RelatedResources") to
	// include in API requests with the JSON null value. By default, fields
	// with empty values are omitted from API requests. However, any field
	// with an empty value appearing in NullFields will be sent to the
	// server as null. It is an error if a field in this list has a
	// non-empty value. This may be used to include null fields in Patch
	// requests.
	NullFields []string `json:"-"`
}

// WriteInsightRequest is a request for sending the data insights.
type WriteInsightRequest struct {
	// Insight: Required. The metrics data details.
	Insight *Insight `json:"insight,omitempty"`
	// RequestID: Optional. An optional request ID to identify requests.
	// Specify a unique request ID so that if you must retry your request,
	// the server will know to ignore the request if it has already been
	// completed. The server will guarantee that for at least 60 minutes
	// since the first request. For example, consider a situation where you
	// make an initial request and t he request times out. If you make the
	// request again with the same request ID, the server can check if
	// original operation with the same request ID was received, and if so,
	// will ignore the second request. This prevents clients from
	// accidentally creating duplicate commitments. The request ID must be a
	// valid UUID with the exception that zero UUID is not supported
	// (00000000-0000-0000-0000-000000000000).
	RequestID string `json:"requestId,omitempty"`
	// ForceSendFields is a list of field names (e.g. "Insight") to
	// unconditionally include in API requests. By default, fields with
	// empty or default values are omitted from API requests. However, any
	// non-pointer, non-interface field appearing in ForceSendFields will be
	// sent to the server regardless of whether the field is empty or not.
	// This may be used to include empty fields in Patch requests.
	ForceSendFields []string `json:"-"`
	// NullFields is a list of field names (e.g. "Insight") to include in
	// API requests with the JSON null value. By default, fields with empty
	// values are omitted from API requests. However, any field with an
	// empty value appearing in NullFields will be sent to the server as
	// null. It is an error if a field in this list has a non-empty value.
	// This may be used to include null fields in Patch requests.
	NullFields []string `json:"-"`
}

// WriteInsightResponse is the response for write insights request.
type WriteInsightResponse struct {
	// ServerResponse contains the HTTP response code and headers from the
	// server.
	googleapi.ServerResponse `json:"-"`
}

// LINT.ThenChange (//depot/google3/google/cloud/workloadmanager/datawarehouse/v1/data_collect_service.proto)

// WriteInsightCall represents a call to send insight data to the WLM API.
type WriteInsightCall struct {
	s                   *Service
	name                string
	writeinsightrequest *WriteInsightRequest
	urlParams           map[string][]string
	ctx                 context.Context
	header              http.Header
}

// WriteInsight creates a WriteInsightCall to send the data insights to workload manager data
// warehouse.
//
//   - location: The GCP location. The format is:
//     projects/{project}/locations/{location}.
func (s *Service) WriteInsight(name string, writeinsightrequest *WriteInsightRequest) *WriteInsightCall {
	return &WriteInsightCall{
		s:                   s,
		urlParams:           make(map[string][]string),
		name:                name,
		writeinsightrequest: writeinsightrequest,
	}
}

func (c *WriteInsightCall) doRequest(alt string) (*http.Response, error) {
	reqHeaders := make(http.Header)
	reqHeaders.Set("x-goog-api-client", "gl-go/"+runtime.Version()+" gdcl/0.116.0")
	for k, v := range c.header {
		reqHeaders[k] = v
	}
	reqHeaders.Set("User-Agent", c.s.userAgent())
	var body io.Reader = nil
	body, err := googleapi.WithoutDataWrapper.JSONReader(c.writeinsightrequest)
	if err != nil {
		return nil, err
	}
	reqHeaders.Set("Content-Type", "application/json")
	c.urlParams["alt"] = []string{alt}
	c.urlParams["prettyPrint"] = []string{"false"}
	urls := googleapi.ResolveRelative(c.s.BasePath, "{+name}/insights:writeInsight")
	urls += "?" + url.Values(c.urlParams).Encode()
	req, err := http.NewRequest("POST", urls, body)
	if err != nil {
		return nil, err
	}
	req.Header = reqHeaders
	googleapi.Expand(req.URL, map[string]string{
		"name": c.name,
	})
	return c.s.SendRequest(c.ctx, c.s.client, req)
}

// Do executes the "workloadmanager.projects.locations.insights.writeInsight" call.
// Exactly one of *WriteInsightResponse or error will be non-nil. Any
// non-2xx status code is an error. Response headers are in either
// *WriteInsightResponse.ServerResponse.Header or (if a response was
// returned at all) in error.(*googleapi.Error).Header. Use
// googleapi.IsNotModified to check whether the returned error was
// because http.StatusNotModified was returned.
func (c *WriteInsightCall) Do(opts ...googleapi.CallOption) (*WriteInsightResponse, error) {
	setOptions(c.urlParams, opts...)
	res, err := c.doRequest("json")
	if res != nil && res.StatusCode == http.StatusNotModified {
		if res.Body != nil {
			res.Body.Close()
		}
		return nil, &googleapi.Error{
			Code:   res.StatusCode,
			Header: res.Header,
		}
	}
	if err != nil {
		return nil, err
	}
	defer googleapi.CloseBody(res)
	if err := googleapi.CheckResponse(res); err != nil {
		return nil, err
	}
	ret := &WriteInsightResponse{
		ServerResponse: googleapi.ServerResponse{
			Header:         res.Header,
			HTTPStatusCode: res.StatusCode,
		},
	}
	if res.StatusCode == http.StatusNoContent {
		return ret, nil
	}
	target := &ret
	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		return nil, err
	}
	return ret, nil
}
