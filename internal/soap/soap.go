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

// Package soap is responsible for SOAP requests over HTTP.
// The package implements functions to create a soap client, send a http request
// and get responses using SOAP messages over HTTP.
package soap

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Client provides an interface for making SOAP requests over HTTP.
type Client struct {
	httpClient *http.Client
	url        string
}

// NewUDSClient returns a Client for making HTTP requests via unix domain sockets.
// socket is the path of socket the recipient is listening on.
func NewUDSClient(socket string) *Client {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
	return &Client{
		httpClient: client,
		url:        "http://unix",
	}
}

// HTTPError is returned whenever an error HTTP response code is returned.
type HTTPError struct {
	Code int
	Body []byte
}

// Error implements the error interface for HTTPError.
func (e HTTPError) Error() string {
	return fmt.Sprintf("HTTP error response returned. HTTP Status %d: %s", e.Code, string(e.Body))
}

// Envelope struct for marshaling/unmarshaling the XML SOAP Message.
type Envelope struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    Body
}

// Body struct for marshaling/unmarshaling the SOAP Body section of the SOAP Message.
type Body struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
	Content []byte   `xml:",innerxml"`
}

// Fault struct for marshaling/unmarshaling SOAP Fault XML.
type Fault struct {
	Code   string `xml:"faultcode,omitempty"`
	String string `xml:"faultstring,omitempty"`
	Actor  string `xml:"faultactor,omitempty"`
	Detail string `xml:"detail,omitempty"`
}

// Error implements the error interface for Fault.
func (e Fault) Error() string {
	return fmt.Sprintf("SOAP Fault received. Code: %s, String: %s", e.Code, e.String)
}

func marshalEnvelope(buffer *bytes.Buffer, envelope any, encoder *xml.Encoder) (*bytes.Buffer, error) {
	if err := encoder.Encode(envelope); err != nil {
		return nil, fmt.Errorf("failed to marshal SOAP Envelope: %v", err)
	}
	if err := encoder.Flush(); err != nil {
		return nil, fmt.Errorf("error flushing buffered XML: %v", err)
	}
	return buffer, nil
}

func parseSoapBody(respEnvelope *Envelope, resBody any) error {
	// SOAP Body either contains a Fault element or the expected resBody.
	// Check for Fault element returned. If so, return the error.
	fault := Fault{}
	if err := xml.Unmarshal(respEnvelope.Body.Content, &fault); err != nil {
		return fmt.Errorf("failed to unmarshal fault element for response type(%T): %v", resBody, err)
	}
	if fault.Code != "" {
		return fault
	}

	// No SOAP Fault element received; unmarshal into the expected response struct.
	if err := xml.Unmarshal(respEnvelope.Body.Content, resBody); err != nil {
		return fmt.Errorf("failed to unmarshal SOAP Body for response type(%T): %v", resBody, err)
	}
	return nil
}

// Call performs the SOAP action specified by reqBody and returns the result in resBody.
// reqBody is a pointer to a structure for the content for the SOAP message body for the request.
// resBody is a pointer to a structure to read the response SOAP message body content into.
func (client *Client) Call(reqBody, resBody any) error {

	// Marshal the reqBody struct into XML.
	reqContent, err := xml.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal SOAP Body for request type(%T): %v", reqBody, err)
	}

	// Construct the SOAP envelope.
	envelope := Envelope{
		Body: Body{
			Content: reqContent,
		},
	}

	// Marshal the envelope into XML.
	newBuffer := new(bytes.Buffer)
	encoder := xml.NewEncoder(newBuffer)
	buffer, err := marshalEnvelope(newBuffer, envelope, encoder)
	if err != nil {
		return err
	}

	// Execute the SOAP call.
	res, err := client.post(buffer)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Unmarshal the SOAP response.
	respEnvelope := &Envelope{
		Body: Body{},
	}
	decoder := xml.NewDecoder(res.Body)
	if err := decoder.Decode(respEnvelope); err != nil {
		return fmt.Errorf("failed to unmarshal SOAP Envelope for response type(%T): %v", resBody, err)
	}

	return parseSoapBody(respEnvelope, resBody)
}

// Post function wraps the SOAP XML body in an HTTP Post request & performs the request.
// body is the XML body for the SOAP envelope.
func (client *Client) post(body *bytes.Buffer) (*http.Response, error) {
	req, err := http.NewRequest("POST", client.url, body)
	if err != nil {
		return nil, fmt.Errorf("error creating new http request: %v", err)
	}

	req.Header.Add("Content-Type", "text/xml; charset=\"utf-8\"")
	req.Close = true

	res, err := client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing http request: %v", err)
	}
	if res.StatusCode >= 400 {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %v", err)
		}
		return res, HTTPError{
			Code: res.StatusCode,
			Body: body,
		}
	}

	return res, nil
}
