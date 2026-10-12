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

package ruleservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const analysisTestID = "12345678-1234-1234-1234-123456789abc"
const analysisTestKey = "private-analysis-key"

func TestNewAnalysisClientConfiguration(t *testing.T) {
	var client, err = NewAnalysisClient("", "", "")
	if client != nil || err != nil {
		t.Fatalf("disabled client: %v, %v", client, err)
	}
	for _, origin := range []string{"", "ftp://bloodhound:8080", "http://user:pass@bloodhound:8080", "http://bloodhound:8080/", "http://bloodhound:8080/path", "http://bloodhound:8080?", "http://bloodhound:8080?secret", "http://bloodhound:8080#fragment", "http://bloodhound:8080#"} {
		if _, err := NewAnalysisClient(origin, analysisTestID, analysisTestKey); err == nil {
			t.Errorf("accepted invalid origin %q", origin)
		}
	}
	for _, credentials := range [][2]string{{"", analysisTestKey}, {analysisTestID, ""}, {"not-a-uuid", analysisTestKey}} {
		if _, err := NewAnalysisClient("http://bloodhound:8080", credentials[0], credentials[1]); err == nil {
			t.Error("accepted invalid credentials")
		}
	}
	client, err = NewAnalysisClient("http://bloodhound:8080", analysisTestID, analysisTestKey)
	if err != nil {
		t.Fatal(err)
	}
	native := client.(*analysisClient)
	if native.http.Timeout > 6*time.Second || native.http.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("unsafe transport configuration")
	}
}

// Recompute the documented chain independently, including the body, as the native authenticator does.
func checkAnalysisSignature(t *testing.T, request *http.Request) {
	t.Helper()
	var (
		requestDate    = request.Header.Get("RequestDate")
		operation      = hmac.New(sha256.New, []byte(analysisTestKey))
		signature, err = base64.StdEncoding.DecodeString(request.Header.Get("Signature"))
	)
	if err != nil {
		t.Error("invalid signature encoding")
		return
	}
	if _, err := time.Parse(time.RFC3339, requestDate); err != nil {
		t.Error("invalid request date")
		return
	}
	if request.Header.Get("Authorization") != "bhesignature "+analysisTestID {
		t.Error("wrong token ID")
	}
	operation.Write([]byte(request.Method + request.RequestURI))
	date := hmac.New(sha256.New, operation.Sum(nil))
	date.Write([]byte(requestDate[:13]))
	body := hmac.New(sha256.New, date.Sum(nil))
	if _, err := io.Copy(body, request.Body); err != nil {
		t.Error(err)
	}
	if !hmac.Equal(signature, body.Sum(nil)) {
		t.Error("signature does not match native HMAC chain")
	}
}

func TestAnalysisClientStatusAndRun(t *testing.T) {
	for _, test := range []struct {
		name, queue, status    string
		pending, running, busy bool
	}{
		{name: "idle", status: "idle"},
		{name: "pending", queue: "analysis", status: "idle", pending: true, busy: true},
		{name: "running", status: "analyzing", running: true, busy: true},
		{name: "deletion", queue: "deletion", status: "idle", busy: true},
		{name: "ingest", status: "ingesting"},
		{name: "purge", status: "purging"},
		{name: "prune", status: "pruning"},
		{name: "starting", status: "starting"},
		{name: "optimize", status: "optimizing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var puts int
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				checkAnalysisSignature(t, request)
				switch request.URL.Path {
				case "/api/v2/analysis/status":
					io.WriteString(writer, `{"data":{"request_type":"`+test.queue+`","requested_by":"user"}}`)
				case "/api/v2/datapipe/status":
					io.WriteString(writer, `{"data":{"status":"`+test.status+`","last_analysis_run_at":"2026-10-11T12:00:00Z"}}`)
				case "/api/v2/analysis":
					if request.Method != http.MethodPut {
						t.Error("wrong trigger method")
					}
					puts++
					writer.WriteHeader(http.StatusAccepted)
				default:
					t.Error("unexpected path")
				}
			}))
			defer server.Close()
			client, err := NewAnalysisClient(server.URL, analysisTestID, analysisTestKey)
			if err != nil {
				t.Fatal(err)
			}
			state, err := client.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if state.Pending != test.pending || state.Running != test.running || state.NativeStatus != test.status || state.Available != (test.queue != "deletion") {
				t.Fatalf("unexpected state: %+v", state)
			}
			err = client.Run(context.Background())
			if test.busy {
				if !errors.Is(err, ErrAnalysisBusy) || puts != 0 {
					t.Fatalf("busy run: %v puts=%d", err, puts)
				}
			} else if err != nil || puts != 1 {
				t.Fatalf("run: %v puts=%d", err, puts)
			}
		})
	}
}

func TestAnalysisClientRejectsUnsafeResponses(t *testing.T) {
	for _, test := range []struct {
		name              string
		code              int
		queue, processing string
	}{
		{name: "unauthorized", code: 401}, {name: "forbidden", code: 403}, {name: "server failure", code: 500},
		{name: "missing data", queue: `{}`}, {name: "missing queue", queue: `{"data":{}}`},
		{name: "unknown queue", queue: `{"data":{"request_type":"unexpected"}}`},
		{name: "malformed", queue: `not json`},
		{name: "unknown processing", queue: `{"data":{"request_type":""}}`, processing: `{"data":{"status":"unexpected"}}`},
		{name: "missing processing", queue: `{"data":{"request_type":""}}`, processing: `{}`},
		{name: "oversized", queue: strings.Repeat("x", 64*1024+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if test.code != 0 {
					writer.WriteHeader(test.code)
					io.WriteString(writer, "private-server-body")
					return
				}
				if request.URL.Path == "/api/v2/analysis/status" {
					io.WriteString(writer, test.queue)
				} else {
					io.WriteString(writer, test.processing)
				}
			}))
			defer server.Close()
			client, err := NewAnalysisClient(server.URL, analysisTestID, analysisTestKey)
			if err != nil {
				t.Fatal(err)
			}
			state, err := client.Status(context.Background())
			if err == nil || state.Available {
				t.Fatalf("unsafe state: %+v %v", state, err)
			}
			for _, secret := range []string{server.URL, analysisTestKey, analysisTestID, "private-server-body"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("error exposes private information")
				}
			}
		})
	}
}

func TestAnalysisClientRejectsRedirectsAndCanceledRequests(t *testing.T) {
	var forwarded bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewAnalysisClient(server.URL, analysisTestID, analysisTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(context.Background()); err == nil || forwarded {
		t.Fatal("redirect accepted or followed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Status(ctx); err == nil || strings.Contains(err.Error(), server.URL) {
		t.Fatal("canceled request accepted or origin exposed")
	}
}

func TestAnalysisClientRunRequiresAcceptedResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v2/analysis/status":
					io.WriteString(writer, `{"data":{"request_type":""}}`)
				case "/api/v2/datapipe/status":
					io.WriteString(writer, `{"data":{"status":"idle"}}`)
				case "/api/v2/analysis":
					writer.WriteHeader(status)
					io.WriteString(writer, "private-server-body")
				}
			}))
			defer server.Close()
			client, err := NewAnalysisClient(server.URL, analysisTestID, analysisTestKey)
			if err != nil {
				t.Fatal(err)
			}
			if err = client.Run(context.Background()); err == nil || strings.Contains(err.Error(), "private-server-body") {
				t.Fatalf("unexpected trigger error: %v", err)
			}
		})
	}
}
