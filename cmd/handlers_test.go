// Copyright 2020-2022 JackTrip Labs, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestPayload struct {
	Name string
}

type TestResponseHeaders struct {
	header string
	result string
}

func TestRunHTTPServer(t *testing.T) {
	t.Skip("TODO")
}

func TestHandlePingRequestNoWS(t *testing.T) {
	assert := assert.New(t)
	// Instantiate mock response writer and request to exercise method
	mockResp := httptest.NewRecorder()
	mockReq := httptest.NewRequest("GET", "http://example.com/foo", nil)
	mockReq.Header.Set("Connection", "nothing")
	mockReq.Header.Set("Upgrade", "nada")
	handlePingRequest(mockResp, mockReq)
	resp := mockResp.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	tests := []TestResponseHeaders{
		// Check Content-Type header
		{
			header: "Content-Type",
			result: "application/json",
		},
		// Check Access-Control-Allow-Origin header
		{
			header: "Access-Control-Allow-Origin",
			result: "*",
		},
	}

	assert.Equal(200, resp.StatusCode)
	assert.Equal(`{"status":"OK"}`, string(body))
	for _, param := range tests {
		assert.Equal(param.result, resp.Header.Get(param.header))
	}
}

func TestOptionsGetOnly(t *testing.T) {
	assert := assert.New(t)
	// Instantiate mock response writer and request to exercise method
	mockResp := httptest.NewRecorder()
	mockReq := httptest.NewRequest("GET", "http://example.com/foo", nil)
	OptionsGetOnly(mockResp, mockReq)
	resp := mockResp.Result()

	tests := []TestResponseHeaders{
		// Check Allow header
		{
			header: "Allow",
			result: "GET, OPTIONS",
		},
		// Check Access-Control-Allow-Methods header
		{
			header: "Access-Control-Allow-Methods",
			result: "GET, OPTIONS",
		},
		// Check Access-Control-Allow-Origin header
		{
			header: "Access-Control-Allow-Origin",
			result: "*",
		},
	}

	assert.Equal(200, resp.StatusCode)
	for _, param := range tests {
		assert.Equal(param.result, resp.Header.Get(param.header))
	}
}

func TestRespondJSONValid(t *testing.T) {
	assert := assert.New(t)
	// Instantiate mock response writer to exercise method
	mockResp := httptest.NewRecorder()
	payload := TestPayload{"mr-worldwide"}
	RespondJSON(mockResp, 299, payload)
	resp := mockResp.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	tests := []TestResponseHeaders{
		// Check Content-Type header
		{
			header: "Content-Type",
			result: "application/json",
		},
		// Check Access-Control-Allow-Origin header
		{
			header: "Access-Control-Allow-Origin",
			result: "*",
		},
	}

	assert.Equal(299, resp.StatusCode)
	assert.Equal(`{"Name":"mr-worldwide"}`, string(body))
	for _, param := range tests {
		assert.Equal(param.result, resp.Header.Get(param.header))
	}
}

func TestRespondJSONInvalid(t *testing.T) {
	assert := assert.New(t)
	// Instantiate mock response writer to exercise method
	mockResp := httptest.NewRecorder()
	// Bad payload should result in an error
	payload := make(chan int)
	RespondJSON(mockResp, 298, payload)
	resp := mockResp.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	tests := []TestResponseHeaders{
		// Check Content-Type header
		{
			header: "Content-Type",
			result: "",
		},
		// Check Access-Control-Allow-Origin header
		{
			header: "Access-Control-Allow-Origin",
			result: "",
		},
	}

	assert.Equal(500, resp.StatusCode)
	assert.Equal("json: unsupported type: chan int", string(body))
	for _, param := range tests {
		assert.Equal(param.result, resp.Header.Get(param.header))
	}
}
