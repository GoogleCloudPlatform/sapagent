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

// Package wlm provides a simple service interface for using the Data Warehouse WriteInsight API.
package wlm

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"runtime"

	"google.golang.org/api/googleapi"
	"google3/third_party/golang/google_api/internal/gensupport"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	dwpb "github.com/GoogleCloudPlatform/sapagent/protos/datawarehouse"
)

const (
	basePathTemplate      = "https://workloadmanager-datawarehouse.UNIVERSE_DOMAIN/"
	mtlsBasePath          = "https://workloadmanager-datawarehouse.mtls.googleapis.com/"
	defaultUniverseDomain = "googleapis.com"
)

// DataWarehouseService implements the Data Warehouse service.
type DataWarehouseService struct {
	c        *http.Client
	BasePath string
}

// WriteInsightResponse is a wrapper for the response from a WriteInsight request.
type WriteInsightResponse struct {
	// ServerResponse contains the HTTP response code and headers from the server.
	googleapi.ServerResponse `json:"-"`
}

// NewService creates a new Data Warehouse service.
func NewService(ctx context.Context, opts ...option.ClientOption) (*DataWarehouseService, error) {
	log.CtxLogger(ctx).Debug("NewService")
	scopesOption := internaloption.WithDefaultScopes(
		"https://www.googleapis.com/auth/cloud-platform",
	)
	log.CtxLogger(ctx).Debug("Adding default options")
	// NOTE: prepend, so we don't override user-specified scopes.
	opts = append([]option.ClientOption{scopesOption}, opts...)
	opts = append(opts, internaloption.WithDefaultEndpointTemplate(basePathTemplate))
	opts = append(opts, internaloption.WithDefaultMTLSEndpoint(mtlsBasePath))
	opts = append(opts, internaloption.WithDefaultUniverseDomain(defaultUniverseDomain))
	log.CtxLogger(ctx).Debugw("Creating client with opts", "opts", opts)
	client, endpoint, err := htransport.NewClient(ctx, opts...)
	log.CtxLogger(ctx).Debug("Created client")
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client is nil")

	}
	log.CtxLogger(ctx).Debug("Creating service")
	s := &DataWarehouseService{c: client, BasePath: endpoint}
	log.CtxLogger(ctx).Debug("Created service")
	return s, nil
}

// WriteInsight sends the WriteInsightRequest to the Data Warehouse.
func (d *DataWarehouseService) WriteInsight(project, location string, req *dwpb.WriteInsightRequest) (*WriteInsightResponse, error) {
	reqHeaders := make(http.Header)
	reqHeaders.Set("x-goog-api-client", "gl-go/"+runtime.Version())
	reqHeaders.Set("User-Agent", googleapi.UserAgent)
	b, err := protojson.Marshal(req)
	if err != nil {
		return nil, err
	}
	log.Logger.Debugw("Body bytes", "bytes", b, "byteStr", string(b))
	body := bytes.NewReader(b)
	reqHeaders.Set("Content-Type", "application/json")
	url := googleapi.ResolveRelative(d.BasePath, "v1/projects/{+project}/locations/{+location}/insights:writeInsight")
	url += "?alt=json&prettyPrint=false"
	httpReq, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header = reqHeaders
	googleapi.Expand(httpReq.URL, map[string]string{
		"project":  project,
		"location": location,
	})
	buf := new(bytes.Buffer)
	bCopy, _ := httpReq.GetBody()
	buf.ReadFrom(bCopy)
	bodyBytes := buf.String()
	bodyStr := string(bodyBytes)
	log.Logger.Debugw("Sending request", "url", httpReq.URL, "body", bodyStr)
	httpRes, err := d.c.Do(httpReq)
	if httpRes != nil && httpRes.StatusCode == http.StatusNotModified {
		if httpRes.Body != nil {
			httpRes.Body.Close()
		}
		return nil, gensupport.WrapError(&googleapi.Error{
			Code:   httpRes.StatusCode,
			Header: httpRes.Header,
		})
	}
	if err != nil {
		return nil, err
	}
	defer googleapi.CloseBody(httpRes)
	if err := googleapi.CheckResponse(httpRes); err != nil {
		return nil, gensupport.WrapError(err)
	}
	target := &WriteInsightResponse{
		ServerResponse: googleapi.ServerResponse{
			Header:         httpRes.Header,
			HTTPStatusCode: httpRes.StatusCode,
		},
	}
	if err := gensupport.DecodeResponse(target, httpRes); err != nil {
		return nil, err
	}
	return target, nil
}
