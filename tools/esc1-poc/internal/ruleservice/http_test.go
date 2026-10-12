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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func exchange(handler http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func TestHTTPAuthenticationAndStrictUploads(t *testing.T) {
	store := open(t)
	handler, err := ruleservice.NewHandler(store, "private-token-012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/rules", "/v1/status", "/v1/preview"} {
		if response := exchange(handler, "GET", path, "", nil); response.Code != 401 {
			t.Fatal(response.Code)
		}
		if response := exchange(handler, "GET", path, "wrong", nil); response.Code != 401 {
			t.Fatal(response.Code)
		}
	}
	if response := exchange(handler, "GET", "/health", "", nil); response.Code != 200 || strings.Contains(response.Body.String(), "private-token-012345678901234567890123456789") {
		t.Fatal(response)
	}
	if response := exchange(handler, "GET", "/", "", nil); response.Code != http.StatusOK || strings.Count(response.Body.String(), `<table id="rules-table">`) != 1 || strings.Count(response.Body.String(), `id="extension-filter"`) != 1 {
		t.Fatal("upload page did not expose one filterable rules table", response.Code)
	}
	for _, endpoint := range [][2]string{{"PUT", "/v1/rules"}, {"POST", "/v1/preview"}} {
		for _, body := range []string{`{"files":{},"files":{}}`, `{"files":{},"unexpected":true}`, `{"files":null}`, `{"remove":null}`, `{"files":{},"overwrite":[1]}`, strings.Repeat("x", ruleservice.MaxBundleBytes+1)} {
			response := exchange(handler, endpoint[0], endpoint[1], "private-token-012345678901234567890123456789", []byte(body))
			if response.Code != 400 && response.Code != 413 {
				t.Fatal(endpoint, response.Code, response.Body.String())
			}
		}
	}
	contents := confirmedHTTPChange(t, handler, ruleservice.Change{Files: files(t)})
	var response *httptest.ResponseRecorder

	response = exchange(handler, "PUT", "/v1/rules", "private-token-012345678901234567890123456789", contents)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := exchange(handler, "GET", "/v1/rules", "private-token-012345678901234567890123456789", nil); response.Code != 200 {
		t.Fatal(response.Code)
	}
	response = exchange(handler, "POST", "/v1/status", "private-token-012345678901234567890123456789", []byte(`{"state":"success","event":"analysis","message":"Applied"}`))
	if response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	if store.LatestStatus().UpdatedAt.IsZero() {
		t.Fatal("missing server timestamp")
	}
	if response := exchange(handler, "POST", "/v1/status", "private-token-012345678901234567890123456789", []byte(`{"state":"unknown"}`)); response.Code != 400 {
		t.Fatal(response.Code)
	}
	if response := exchange(handler, "DELETE", "/v1/rules", "private-token-012345678901234567890123456789", nil); response.Code != 405 {
		t.Fatal(response.Code)
	}
}

type transport func(*http.Request) (*http.Response, error)

func (s transport) RoundTrip(request *http.Request) (*http.Response, error) { return s(request) }
func localClient(handler http.Handler) ruleservice.Client {
	return ruleservice.Client{BaseURL: "http://registry", Token: "private-token-012345678901234567890123456789", HTTP: &http.Client{Transport: transport(func(request *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Result(), nil
	})}}
}
func TestClientFetchAndReport(t *testing.T) {
	store := open(t)
	handler, err := ruleservice.NewHandler(store, "private-token-012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	client := localClient(handler)
	if _, err := client.Fetch(context.Background()); !errors.Is(err, ruleservice.ErrNoActive) {
		t.Fatal(err)
	}
	expected, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := client.Fetch(context.Background())
	if err != nil || actual.Revision != expected.Revision {
		t.Fatal(err)
	}
	if err := client.Report(context.Background(), ruleservice.Status{State: "running", Event: "analysis", Revision: actual.Revision}); err != nil {
		t.Fatal(err)
	}
	if store.LatestStatus().State != "running" {
		t.Fatal("report not saved")
	}
	client.Token = "wrong"
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("unauthenticated fetch")
	}
}
func TestClientRejectsUntrustedResponses(t *testing.T) {
	store := open(t)
	bundle, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	bundle.Revision = strings.Repeat("0", 64)
	contents, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"digest": string(contents), "oversize": strings.Repeat("x", ruleservice.MaxBundleBytes+1), "duplicate": `{"files":{},"files":{}}`} {
		t.Run(name, func(t *testing.T) {
			client := ruleservice.Client{BaseURL: "http://registry", Token: "private-token-012345678901234567890123456789", HTTP: &http.Client{Transport: transport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			if _, err := client.Fetch(context.Background()); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
	var requests int
	client := ruleservice.Client{BaseURL: "http://registry", Token: "private-token-012345678901234567890123456789", HTTP: &http.Client{Transport: transport(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://other-host/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}}
	if _, err := client.Fetch(context.Background()); err == nil || requests != 1 {
		t.Fatal("redirect followed", err, requests)
	}
}

func TestConcurrentUploadBodiesAreBounded(t *testing.T) {
	store := open(t)
	handler, err := ruleservice.NewHandler(store, "private-token-012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	var (
		entered  = make(chan struct{}, 4)
		release  = make(chan struct{})
		finished sync.WaitGroup
	)
	for index := 0; index < 4; index++ {
		finished.Add(1)
		go func() {
			defer finished.Done()
			request := httptest.NewRequest("PUT", "/v1/rules", nil)
			request.Header.Set("Authorization", "Bearer private-token-012345678901234567890123456789")
			request.Body = &blockedBody{entered: entered, release: release}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
		}()
	}
	for index := 0; index < 4; index++ {
		<-entered
	}
	response := exchange(handler, "PUT", "/v1/rules", "private-token-012345678901234567890123456789", []byte(`{"files":{}}`))
	close(release)
	finished.Wait()
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("unbounded uploads", response.Code)
	}
}

type blockedBody struct {
	entered chan struct{}
	release chan struct{}
}

func (s *blockedBody) Read(contents []byte) (int, error) {
	s.entered <- struct{}{}
	<-s.release
	return 0, io.EOF
}
func (s *blockedBody) Close() error { return nil }

func TestInterruptedUploadPreservesActiveRules(t *testing.T) {
	store := open(t)
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ruleservice.NewHandler(store, "private-token-012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PUT", "/v1/rules", nil)
	request.Header.Set("Authorization", "Bearer private-token-012345678901234567890123456789")
	request.Body = &interruptedBody{}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatal(response.Code)
	}
	active, err := store.Active()
	if err != nil || active.Revision != initial.Revision {
		t.Fatal("partial upload changed active rules", err)
	}
}

type interruptedBody struct{}

func (s *interruptedBody) Read(contents []byte) (int, error) {
	return copy(contents, `{"files":{"schema.json":`), io.ErrUnexpectedEOF
}
func (s *interruptedBody) Close() error { return nil }

func TestHandlerRequiresPrivateToken(t *testing.T) {
	store := open(t)
	for _, token := range []string{"", "short", strings.Repeat(" ", 32)} {
		if _, err := ruleservice.NewHandler(store, token); err == nil {
			t.Fatal("accepted invalid token")
		}
	}
	if _, err := ruleservice.NewHandler(nil, strings.Repeat("a", 32)); err == nil {
		t.Fatal("accepted nil store")
	}
}

func TestClientDisablesDefaultProxy(t *testing.T) {
	var (
		store       = open(t)
		original    = http.DefaultTransport
		transport   = original.(*http.Transport).Clone()
		connections = make(chan string, 1)
		finished    = make(chan error, 1)
	)
	bundle, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	contents, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	transport.Proxy = func(*http.Request) (*url.URL, error) {
		t.Error("default proxy received authenticated request")
		return url.Parse("http://untrusted-proxy")
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		connections <- address
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			request, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				finished <- err
				return
			}
			if request.Header.Get("Authorization") != "Bearer private-token-012345678901234567890123456789" {
				finished <- errors.New("missing direct authentication")
				return
			}
			response := http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Connection": []string{"close"}}, Body: io.NopCloser(bytes.NewReader(contents)), ContentLength: int64(len(contents)), Close: true}
			finished <- response.Write(server)
		}()
		return client, nil
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original; transport.CloseIdleConnections() }()
	client := ruleservice.Client{BaseURL: "http://registry.invalid", Token: "private-token-012345678901234567890123456789", HTTP: &http.Client{}}
	actual, err := client.Fetch(t.Context())
	if err != nil || actual.Revision != bundle.Revision {
		t.Fatal(err)
	}
	if address := <-connections; address != "registry.invalid:80" {
		t.Fatal("authentication routed through proxy", address)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
