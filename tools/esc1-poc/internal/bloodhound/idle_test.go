// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
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
//
// SPDX-License-Identifier: Apache-2.0

package bloodhound

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitUntilIdleWaitsForAnalysis(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		queue      []string
		processing []string
	}{
		{
			name:       "queued startup analysis then running then complete",
			queue:      []string{"analysis", "", ""},
			processing: []string{"idle", "analyzing", "idle"},
		},
		{
			name:       "empty queue with analysis already running",
			queue:      []string{"", ""},
			processing: []string{"analyzing", "idle"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				calls  atomic.Int64
				client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
					var (
						callIndex = int(calls.Add(1) - 1)
						pollIndex = callIndex / 2
						completed = "2026-10-07T11:00:00Z"
					)
					if request.Method != http.MethodGet || pollIndex >= len(testCase.queue) {
						t.Errorf("unexpected background request: %s %s, call %d", request.Method, request.URL.Path, callIndex)
						response.WriteHeader(http.StatusBadRequest)
						return
					}
					if callIndex%2 == 0 {
						if request.URL.Path != "/api/v2/analysis/status" {
							t.Error("background queue must be checked before processing status")
						}
						fmt.Fprintf(response, `{"data":{"request_type":%q}}`, testCase.queue[pollIndex])
					} else {
						if request.URL.Path != "/api/v2/datapipe/status" {
							t.Errorf("unexpected processing path %s", request.URL.Path)
						}
						// The initial idle state still has an old successful analysis.
						if pollIndex == 0 && testCase.queue[pollIndex] != "" {
							fmt.Fprint(response, `{"data":{"status":"idle","last_analysis_run_at":"2026-10-07T10:00:00Z","last_complete_analysis_at":"2026-10-07T11:00:00Z"}}`)
						} else {
							if pollIndex == len(testCase.queue)-1 {
								completed = "2026-10-07T13:00:00Z"
							}
							fmt.Fprintf(response, `{"data":{"status":%q,"last_analysis_run_at":"2026-10-07T12:00:00Z","last_complete_analysis_at":%q}}`, testCase.processing[pollIndex], completed)
						}
					}
				})
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			)
			defer cancel()
			if err := client.WaitUntilIdle(ctx); err != nil {
				t.Fatal(err)
			}
			if actual := calls.Load(); actual != int64(2*len(testCase.queue)) {
				t.Fatalf("returned before the current analysis completed: got %d calls, want %d", actual, 2*len(testCase.queue))
			}
		})
	}
}

func TestWaitUntilIdleRejectsUncertainOrFailedAnalysis(t *testing.T) {
	for _, testCase := range []struct {
		name, queue, processing string
	}{
		{"latest run unfinished", `{"data":{"request_type":""}}`, `{"data":{"status":"idle","last_analysis_run_at":"2026-10-07T12:00:00Z","last_complete_analysis_at":"2026-10-07T11:00:00Z"}}`},
		{"missing start", `{"data":{"request_type":""}}`, `{"data":{"status":"idle","last_complete_analysis_at":"2026-10-07T13:00:00Z"}}`},
		{"missing completion", `{"data":{"request_type":""}}`, `{"data":{"status":"idle","last_analysis_run_at":"2026-10-07T12:00:00Z"}}`},
		{"zero start", `{"data":{"request_type":""}}`, `{"data":{"status":"idle","last_analysis_run_at":"0001-01-01T00:00:00Z","last_complete_analysis_at":"2026-10-07T13:00:00Z"}}`},
		{"zero completion", `{"data":{"request_type":""}}`, `{"data":{"status":"idle","last_analysis_run_at":"2026-10-07T12:00:00Z","last_complete_analysis_at":"0001-01-01T00:00:00Z"}}`},
		{"missing queue", `{"data":{}}`, `{"data":{"status":"idle"}}`},
		{"null queue", `{"data":{"request_type":null}}`, `{"data":{"status":"idle"}}`},
		{"missing status", `{"data":{"request_type":""}}`, `{"data":{"last_analysis_run_at":"2026-10-07T12:00:00Z","last_complete_analysis_at":"2026-10-07T13:00:00Z"}}`},
		{"unknown status", `{"data":{"request_type":""}}`, `{"data":{"status":"failed"}}`},
		{"pending deletion", `{"data":{"request_type":"deletion"}}`, `{"data":{"status":"idle"}}`},
		{"unknown request", `{"data":{"request_type":"unexpected"}}`, `{"data":{"status":"idle"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
					if request.Method != http.MethodGet {
						t.Error("background checks must not write")
					}
					switch request.URL.Path {
					case "/api/v2/analysis/status":
						fmt.Fprint(response, testCase.queue)
					case "/api/v2/datapipe/status":
						fmt.Fprint(response, testCase.processing)
					default:
						t.Errorf("unexpected request %s", request.URL.Path)
						response.WriteHeader(http.StatusNotFound)
					}
				})
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			)
			defer cancel()
			if err := client.WaitUntilIdle(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected an immediate rejection, got %v", err)
			}
		})
	}
}

func TestWaitUntilIdleCancellation(t *testing.T) {
	for _, testCase := range []struct {
		name, queue, status string
		alreadyCanceled     bool
	}{
		{"already canceled", "", "idle", true},
		{"persistent queued analysis", "analysis", "idle", false},
		{"persistent running analysis", "", "analyzing", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
					if testCase.alreadyCanceled {
						t.Error("canceled context reached the server")
					}
					if request.URL.Path == "/api/v2/analysis/status" {
						fmt.Fprintf(response, `{"data":{"request_type":%q}}`, testCase.queue)
					} else {
						fmt.Fprintf(response, `{"data":{"status":%q,"last_analysis_run_at":"2026-10-07T12:00:00Z","last_complete_analysis_at":"2026-10-07T13:00:00Z"}}`, testCase.status)
					}
				})
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
				expectedErr = context.DeadlineExceeded
				started     = time.Now()
			)
			defer cancel()
			client.pollInterval = time.Hour
			if testCase.alreadyCanceled {
				cancel()
				expectedErr = context.Canceled
			}
			if err := client.WaitUntilIdle(ctx); !errors.Is(err, expectedErr) {
				t.Fatalf("expected %v, got %v", expectedErr, err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("cancellation did not interrupt polling promptly: %v", elapsed)
			}
		})
	}
}
