/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/
package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDockerAPIErrorsDoNotRevealResponseSecrets(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP error", http.StatusForbidden, `{"message":"NEO4J_PASSWORD=private-password"}`},
		{"malformed inspect response", http.StatusOK, `{"Config":{"Env":["NEO4J_PASSWORD=private-password"]},`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var api = &dockerAPI{client: &http.Client{Transport: readinessTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.URL.Path != "/containers/selected-app/json" {
					t.Fatalf("unexpected inspect request: %s %s", request.Method, request.URL.Path)
				}
				return &http.Response{StatusCode: testCase.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(testCase.body))}, nil
			})}}
			if _, err := api.inspect(context.Background(), "selected-app"); err == nil || strings.Contains(err.Error(), "private-password") || strings.Contains(err.Error(), "NEO4J_PASSWORD") {
				t.Fatalf("inspect failure missing or disclosed secrets: %v", err)
			}
		})
	}
}

func TestDockerAlreadyStartedOrStoppedIsAccepted(t *testing.T) {
	var api = &dockerAPI{client: &http.Client{Transport: readinessTransport(func(*http.Request) (*http.Response, error) {
		return dockerTestResponse(http.StatusNotModified, nil), nil
	})}}
	for _, action := range []string{"start", "stop"} {
		if err := api.request(context.Background(), http.MethodPost, "/containers/immutable-id/"+action, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalDockerTransportIgnoresProxyAndRefusesRedirects(t *testing.T) {
	var (
		api       = localDockerAPI()
		transport = api.client.Transport.(*http.Transport)
	)
	if transport.Proxy != nil || transport.DialContext == nil {
		t.Fatal("Docker API must use the local socket without environment proxies")
	}
	if api.client.CheckRedirect == nil || api.client.CheckRedirect(&http.Request{}, nil) == nil {
		t.Fatal("Docker API allows redirects away from the local socket")
	}
}
