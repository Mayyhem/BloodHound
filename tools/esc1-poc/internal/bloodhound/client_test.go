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
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const validCollection = `{"meta":{"type":"users","version":6,"count":1},"data":[{"ObjectIdentifier":"S-1"}]}`

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	var (
		listener, listenErr = net.Listen("tcp4", "127.0.0.1:0")
		server              = &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
		client              *Client
		err                 error
	)
	if listenErr != nil {
		t.Fatal(listenErr)
	}
	server.Start()
	client, err = New(server.URL, "test-token")
	t.Cleanup(server.Close)
	if err != nil {
		t.Fatal(err)
	}
	client.pollInterval = time.Millisecond
	return client
}

func TestUploadAndWait(t *testing.T) {
	var (
		calls    []string
		statuses = []int{6, 7, 2}
		client   = testClient(t, func(response http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing bearer token")
			}
			calls = append(calls, request.Method+" "+request.URL.Path)
			switch request.URL.Path {
			case "/api/v2/file-upload/start":
				response.WriteHeader(http.StatusCreated)
				fmt.Fprint(response, `{"data":{"id":42}}`)
			case "/api/v2/file-upload/42":
				payload, err := io.ReadAll(request.Body)
				if err != nil {
					t.Error(err)
				}
				if string(payload) != validCollection {
					t.Error("upload bytes changed")
				}
				if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("X-File-Upload-Name") != "users.json" {
					t.Error("upload headers missing")
				}
				response.WriteHeader(http.StatusAccepted)
			case "/api/v2/file-upload/42/end":
				response.WriteHeader(http.StatusOK)
			case "/api/v2/file-upload":
				if request.URL.Query().Get("id") != "eq:42" {
					t.Error("missing ID filter")
				}
				fmt.Fprintf(response, `{"data":[{"id":42,"status":%d,"total_files":1,"failed_files":0,"partial_failed_files":0,"end_time":"2026-10-07T12:00:00Z"}]}`, statuses[0])
				statuses = statuses[1:]
			default:
				t.Errorf("unexpected request %s", request.URL.Path)
			}
		})
		path = filepath.Join(t.TempDir(), "users.json")
	)
	if err := os.WriteFile(path, []byte(validCollection), 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := client.UploadAndWait(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.JobID != 42 || receipt.Status != "complete" || receipt.CompletedAt != "2026-10-07T12:00:00Z" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if len(calls) != 6 || len(statuses) != 0 {
		t.Fatalf("unexpected upload sequence: %v", calls)
	}
}

func TestWaitRejectsUnsuccessfulJobs(t *testing.T) {
	for _, testCase := range []struct {
		name                   string
		status                 string
		total, failed, partial int
	}{
		{"canceled", "3", 1, 0, 0}, {"timeout", "4", 1, 0, 0}, {"failed", "5", 1, 1, 0},
		{"partial status", "8", 2, 1, 0}, {"partial files", "2", 2, 0, 1}, {"failed files", "2", 2, 1, 0},
		{"empty job", "2", 0, 0, 0}, {"unknown", "90", 1, 0, 0}, {"missing status", "null", 1, 0, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet {
					t.Error("failed job must not trigger writes")
				}
				fmt.Fprintf(response, `{"data":[{"id":42,"status":%s,"total_files":%d,"failed_files":%d,"partial_failed_files":%d,"end_time":"2026-10-07T12:00:00Z"}]}`, testCase.status, testCase.total, testCase.failed, testCase.partial)
			})
			receipt, err := client.Wait(context.Background(), 42)
			if err == nil || receipt.JobID != 42 {
				t.Fatalf("failure not preserved: %+v %v", receipt, err)
			}
		})
	}
}

func TestWaitCancellation(t *testing.T) {
	var (
		client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
			fmt.Fprint(response, `{"data":[{"id":42,"status":7}]}`)
		})
		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	)
	defer cancel()
	client.pollInterval = time.Hour
	if _, err := client.Wait(ctx, 42); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}

func TestLocalOriginAndRedirect(t *testing.T) {
	for _, origin := range []string{"https://example.com", "http://localhost", "http://127.0.0.1/path", "http://user:pass@127.0.0.1", "http://127.0.0.1?other=1"} {
		if _, err := New(origin, "token"); err == nil {
			t.Errorf("accepted origin %s", origin)
		}
	}
	var client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Location", "http://example.com/")
		response.WriteHeader(http.StatusFound)
	})
	if _, err := client.Status(context.Background(), 42); err == nil {
		t.Fatal("followed redirect")
	}
}

func TestValidationBeforeUpload(t *testing.T) {
	for _, payload := range []string{"", `{`, validCollection + `{}`, `{"meta":{"type":"unsupported","version":6},"data":[]}`, `{"meta":{"type":"users","version":6,"count":2},"data":[]}`} {
		var (
			client = testClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("invalid local file sent a request") })
			path   = filepath.Join(t.TempDir(), "users.json")
		)
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := client.UploadAndWait(context.Background(), []string{path}); err == nil {
			t.Errorf("accepted invalid payload %q", payload)
		}
	}
}

func TestZIPValidation(t *testing.T) {
	for _, testCase := range []struct {
		name, member, payload string
		valid                 bool
	}{
		{"valid", "users.json", validCollection, true}, {"truncated JSON", "users.json", "{", false},
		{"unexpected member", "notes.txt", "notes", false}, {"empty archive", "", "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				buffer bytes.Buffer
				writer = zip.NewWriter(&buffer)
				path   = filepath.Join(t.TempDir(), "collection.zip")
			)
			if testCase.member != "" {
				member, err := writer.Create(testCase.member)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = member.Write([]byte(testCase.payload)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			_, contentType, err := readCollection(path)
			if (err == nil) != testCase.valid {
				t.Fatalf("unexpected validation result %v", err)
			}
			if testCase.valid && contentType != "application/zip" {
				t.Fatal("wrong content type")
			}
		})
	}
	var path = filepath.Join(t.TempDir(), "broken.zip")
	if err := os.WriteFile(path, []byte("not a ZIP"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCollection(path); err == nil {
		t.Fatal("accepted malformed archive")
	}
}

func TestUploadSchema(t *testing.T) {
	var (
		payload = []byte(`{"schema":{"name":"ESC1 experiment","namespace":"EXP","version":"v1.0.0"},"node_kinds":[{"name":"EXP_Helper"}],"relationship_kinds":[{"name":"EXP_ESC1","is_traversable":true}]}`)
		calls   int
		client  = testClient(t, func(response http.ResponseWriter, request *http.Request) {
			calls++
			if request.Method != http.MethodPut || request.URL.Path != "/api/v2/extensions" || request.Header.Get("Content-Type") != "application/json" {
				t.Error("wrong schema request")
			}
			response.WriteHeader(http.StatusCreated)
		})
	)
	if err := client.UploadSchema(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if err := client.UploadSchema(context.Background(), []byte(`{"queries":[]}`)); err == nil {
		t.Fatal("accepted unsupported schema field")
	}
	if calls != 1 {
		t.Fatal("invalid schema was uploaded")
	}
}

func completeJobJSON(identifier int) string {
	return fmt.Sprintf(`{"id":%d,"status":2,"total_files":1,"failed_files":0,"partial_failed_files":0,"end_time":"2026-10-07T12:00:00Z"}`, identifier)
}

func TestCheckAllJobsComplete(t *testing.T) {
	for _, testCase := range []struct {
		name, payload string
		valid         bool
	}{
		{"completed job", `{"data":[` + completeJobJSON(1) + `],"count":1}`, true},
		{"old success and new failure", `{"data":[` + completeJobJSON(1) + `,{"id":2,"status":5,"total_files":1,"failed_files":1,"partial_failed_files":0,"end_time":"2026-10-07T13:00:00Z"}],"count":2}`, false},
		{"old success and unfinished job", `{"data":[` + completeJobJSON(1) + `,{"id":2,"status":7}],"count":2}`, false},
		{"missing failure count", `{"data":[{"id":1,"status":2,"total_files":1,"partial_failed_files":0,"end_time":"2026-10-07T12:00:00Z"}]}`, false},
		{"missing partial failure count", `{"data":[{"id":1,"status":2,"total_files":1,"failed_files":0,"end_time":"2026-10-07T12:00:00Z"}]}`, false},
		{"partial failure", `{"data":[{"id":1,"status":2,"total_files":1,"failed_files":0,"partial_failed_files":1,"end_time":"2026-10-07T12:00:00Z"}]}`, false},
		{"missing completion time", `{"data":[{"id":1,"status":2,"total_files":1,"failed_files":0,"partial_failed_files":0}]}`, false},
		{"invalid completion time", `{"data":[{"id":1,"status":2,"total_files":1,"failed_files":0,"partial_failed_files":0,"end_time":"yesterday"}]}`, false},
		{"no jobs", `{"data":[],"count":0}`, false},
		{"incomplete page", `{"data":[` + completeJobJSON(1) + `],"count":2}`, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != "/api/v2/file-upload" || request.URL.Query().Get("sort_by") != "id" || request.URL.Query().Get("limit") != "100" {
					t.Error("incorrect job enumeration request")
				}
				fmt.Fprint(response, testCase.payload)
			})
			if err := client.CheckAllJobsComplete(context.Background()); (err == nil) != testCase.valid {
				t.Fatalf("unexpected check result: %v", err)
			}
		})
	}
}

func TestCheckAllJobsCompletePagination(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		secondPage string
		valid      bool
	}{
		{"complete second page", completeJobJSON(101), true},
		{"second page failed", `{"id":101,"status":5}`, false},
		{"duplicate ID", completeJobJSON(100), false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				calls  int
				client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
					calls++
					if calls == 1 {
						if request.URL.Query().Get("skip") != "0" {
							t.Error("first page skip")
						}
						fmt.Fprint(response, `{"data":[`)
						for identifier := 1; identifier <= 100; identifier++ {
							if identifier > 1 {
								fmt.Fprint(response, ",")
							}
							fmt.Fprint(response, completeJobJSON(identifier))
						}
						fmt.Fprint(response, `],"count":101,"skip":0,"limit":100}`)
					} else {
						if calls != 2 || request.URL.Query().Get("skip") != "100" {
							t.Error("second page skip")
						}
						fmt.Fprintf(response, `{"data":[%s],"count":101,"skip":100,"limit":100}`, testCase.secondPage)
					}
				})
			)
			if err := client.CheckAllJobsComplete(context.Background()); (err == nil) != testCase.valid {
				t.Fatalf("unexpected pagination result: %v", err)
			}
			if calls != 2 {
				t.Fatalf("expected both pages, got %d", calls)
			}
		})
	}
}
