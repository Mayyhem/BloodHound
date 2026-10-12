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

package ruleservice_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func TestBrowserHandlerOriginValidation(t *testing.T) {
	var authenticated = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, origin := range []string{"http://remote.example:18081", "http://127.0.0.1:18081/", "http://user@127.0.0.1:18081", "http://127.0.0.1:18081?", "http://127.0.0.1:18081?x=y", "http://127.0.0.1:18081#fragment", "file://127.0.0.1"} {
		if _, err := ruleservice.NewBrowserHandler(authenticated, strings.Repeat("a", 32), origin); err == nil {
			t.Fatalf("accepted invalid local origin %q", origin)
		}
	}
	for _, origin := range []string{"", "http://127.0.0.1:18081", "http://localhost:18081", "https://[::1]:18081"} {
		if _, err := ruleservice.NewBrowserHandler(authenticated, strings.Repeat("a", 32), origin); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ruleservice.NewBrowserHandler(nil, strings.Repeat("a", 32), ""); err == nil {
		t.Fatal("accepted nil handler")
	}
}

func TestBrowserHandlerLocalRequests(t *testing.T) {
	var (
		token         = strings.Repeat("a", 32)
		origin        = "http://127.0.0.1:18081"
		forwardedPath string
		authenticated = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != "Bearer "+token {
				t.Error("missing internal bearer token")
			}
			forwardedPath = request.URL.Path
			writer.WriteHeader(http.StatusNoContent)
		})
	)
	handler, err := ruleservice.NewBrowserHandler(authenticated, token, origin)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, method, path, host, origin, fetchSite, contentType string
		code                                                     int
		machinePath                                              string
	}{
		{name: "read plan", method: "GET", path: "/ui/plan", code: 204, machinePath: "/v1/plan"},
		{name: "read rules", method: "GET", path: "/ui/rules", code: 204, machinePath: "/v1/rules"},
		{name: "read status", method: "GET", path: "/ui/status", code: 204, machinePath: "/v1/status"},
		{name: "analysis status", method: "GET", path: "/ui/analysis", code: 204, machinePath: "/v1/analysis"},
		{name: "run analysis", method: "POST", path: "/ui/analysis", origin: origin, contentType: "application/json", code: 204, machinePath: "/v1/analysis"},
		{name: "analysis missing origin", method: "POST", path: "/ui/analysis", contentType: "application/json", code: 403},
		{name: "analysis cross site", method: "POST", path: "/ui/analysis", origin: "http://evil.example", contentType: "application/json", code: 403},
		{name: "analysis form rejected", method: "POST", path: "/ui/analysis", origin: origin, contentType: "text/plain", code: 415},
		{name: "analysis wrong method", method: "DELETE", path: "/ui/analysis", origin: origin, contentType: "application/json", code: 405},
		{name: "put rules", method: "PUT", path: "/ui/rules", origin: origin, contentType: "application/json; charset=utf-8", code: 204, machinePath: "/v1/rules"},
		{name: "preview", method: "POST", path: "/ui/preview", origin: origin, contentType: "application/json", code: 204, machinePath: "/v1/preview"},
		{name: "wrong host", method: "GET", path: "/ui/rules", host: "localhost:18081", code: 403},
		{name: "wrong origin", method: "GET", path: "/ui/rules", origin: "http://evil.example", code: 403},
		{name: "cross site", method: "GET", path: "/ui/rules", fetchSite: "cross-site", code: 403},
		{name: "write missing origin", method: "PUT", path: "/ui/rules", contentType: "application/json", code: 403},
		{name: "write wrong origin", method: "PUT", path: "/ui/rules", origin: "http://localhost:18081", contentType: "application/json", code: 403},
		{name: "write no content type", method: "PUT", path: "/ui/rules", origin: origin, code: 415},
		{name: "write form body", method: "PUT", path: "/ui/rules", origin: origin, contentType: "text/plain", code: 415},
		{name: "status cannot report", method: "POST", path: "/ui/status", origin: origin, contentType: "application/json", code: 405},
		{name: "plan cannot write", method: "PUT", path: "/ui/plan", origin: origin, contentType: "application/json", code: 405},
		{name: "unknown ui", method: "GET", path: "/ui/other", code: 404},
		{name: "query rejected", method: "GET", path: "/ui/rules?token=value", code: 403},
		{name: "encoded path rejected", method: "GET", path: "/ui/%72ules", code: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			var (
				request  = httptest.NewRequest(test.method, origin+test.path, strings.NewReader(`{}`))
				response = httptest.NewRecorder()
			)
			forwardedPath = ""
			if test.host != "" {
				request.Host = test.host
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			request.Header.Set("Content-Type", test.contentType)
			handler.ServeHTTP(response, request)
			if response.Code != test.code || forwardedPath != test.machinePath {
				t.Fatalf("got code %d path %q, want %d %q", response.Code, forwardedPath, test.code, test.machinePath)
			}
			if strings.Contains(response.Body.String(), token) {
				t.Fatal("response exposed bearer token")
			}
		})
	}
}

func TestBrowserHandlerKeepsMachineAuthenticationAndManualMode(t *testing.T) {
	var (
		token  = strings.Repeat("a", 32)
		origin = "http://127.0.0.1:18081"
	)
	authenticated, err := ruleservice.NewHandler(open(t), token)
	if err != nil {
		t.Fatal(err)
	}
	for _, localOrigin := range []string{"", origin} {
		handler, err := ruleservice.NewBrowserHandler(authenticated, token, localOrigin)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/v1/rules", "/v1/status"} {
			for _, suppliedToken := range []string{"", "wrong"} {
				if response := exchange(handler, "GET", path, suppliedToken, nil); response.Code != 401 {
					t.Fatal("machine request without valid token accepted", response.Code)
				}
			}
		}
		if response := exchange(handler, "GET", "/v1/status", token, nil); response.Code != 200 {
			t.Fatal("manual bearer request failed", response.Code)
		}
		var (
			request  = httptest.NewRequest("GET", origin+"/v1/browser-mode", nil)
			response = httptest.NewRecorder()
		)
		handler.ServeHTTP(response, request)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"local_ui":`+strconv.FormatBool(localOrigin != "")) || strings.Contains(response.Body.String(), token) {
			t.Fatal("incorrect browser mode", response.Body.String())
		}
		if localOrigin != "" {
			if response := exchange(handler, "GET", origin+"/ui/status", "", nil); response.Code != 200 {
				t.Fatal("local browser status request failed", response.Code)
			}
			request.Host = "registry:8081"
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if !strings.Contains(response.Body.String(), `"local_ui":false`) {
				t.Fatal("enabled browser mode on another host")
			}
		}
		if localOrigin == "" {
			if response := exchange(handler, "GET", origin+"/ui/status", "", nil); response.Code != 404 {
				t.Fatal("local routes enabled without explicit origin", response.Code)
			}
		}
	}
}
