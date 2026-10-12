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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const validOpenGraph = `{"metadata":{"source_kind":"EXP"},"graph":{"nodes":[{"id":"helper","kinds":["EXP_Helper"],"properties":{"name":"Helper","active":true,"weight":1.5,"names":["a","b"],"weights":[1,2.5],"flags":[true,false],"empty":[]}}],"edges":[{"start":{"value":"helper"},"end":{"match_by":"id","value":"native-id","kind":"User"},"kind":"EXP_ESC1","properties":null}]}}`

func TestOpenGraphValidation(t *testing.T) {
	for _, testCase := range []struct {
		name, payload string
		valid         bool
	}{
		{"full graph", validOpenGraph, true},
		{"empty nodes", `{"graph":{"nodes":[]}}`, true},
		{"empty edges", `{"graph":{"edges":[]}}`, true},
		{"zero kinds follows schema", `{"graph":{"nodes":[{"id":"n","kinds":[]}]}}`, true},
		{"null property bag", `{"graph":{"nodes":[{"id":"n","kinds":["EXP_Node"],"properties":null}]}}`, true},
		{"name and property references", `{"graph":{"edges":[{"start":{"match_by":"name","value":"native user","kind":"User"},"end":{"match_by":"property","kind":"Computer","property_matchers":[{"key":"name","operator":"equals","value":"HOST"},{"key":"enabled","operator":"equals","value":true},{"key":"rating","operator":"equals","value":3}]},"kind":"EXP_Link"}]}}`, true},
		{"null source", `{"metadata":{"source_kind":null},"graph":{"edges":[]}}`, true},
		{"mixed classic first", `{"meta":{"type":"users","version":6},"data":[],"graph":{"nodes":[]}}`, false},
		{"mixed graph first", `{"graph":{"nodes":[]},"meta":{"type":"users","version":6},"data":[]}`, false},
		{"mixed data only", `{"graph":{"nodes":[]},"data":[]}`, false},
		{"mixed null meta", `{"graph":{"nodes":[]},"meta":null}`, false},
		{"null graph", `{"graph":null}`, false},
		{"array graph", `{"graph":[]}`, false},
		{"empty graph", `{"graph":{}}`, false},
		{"null nodes", `{"graph":{"nodes":null}}`, false},
		{"object edges", `{"graph":{"edges":{}}}`, false},
		{"primitive node", `{"graph":{"nodes":[1]}}`, false},
		{"null node", `{"graph":{"nodes":[null]}}`, false},
		{"missing id", `{"graph":{"nodes":[{"kinds":[]}]}}`, false},
		{"numeric id", `{"graph":{"nodes":[{"id":3,"kinds":[]}]}}`, false},
		{"missing kinds", `{"graph":{"nodes":[{"id":"n"}]}}`, false},
		{"null kinds", `{"graph":{"nodes":[{"id":"n","kinds":null}]}}`, false},
		{"numeric kinds", `{"graph":{"nodes":[{"id":"n","kinds":[1]}]}}`, false},
		{"four kinds", `{"graph":{"nodes":[{"id":"n","kinds":["A","B","C","D"]}]}}`, false},
		{"reserved node kind", `{"graph":{"nodes":[{"id":"n","kinds":["TAG_Private"]}]}}`, false},
		{"objectid property", `{"graph":{"nodes":[{"id":"n","kinds":[],"properties":{"objectid":"other"}}]}}`, false},
		{"nested property", `{"graph":{"nodes":[{"id":"n","kinds":[],"properties":{"x":{}}}]}}`, false},
		{"null property", `{"graph":{"nodes":[{"id":"n","kinds":[],"properties":{"x":null}}]}}`, false},
		{"mixed property array", `{"graph":{"nodes":[{"id":"n","kinds":[],"properties":{"x":[1,"a"]}}]}}`, false},
		{"nested property array", `{"graph":{"nodes":[{"id":"n","kinds":[],"properties":{"x":[[]]}}]}}`, false},
		{"null property element", `{"graph":{"nodes":[{"id":"n","kinds":[],"properties":{"x":[null]}}]}}`, false},
		{"invalid metadata", `{"metadata":{"other":"x"},"graph":{"nodes":[]}}`, false},
		{"null metadata", `{"metadata":null,"graph":{"nodes":[]}}`, false},
		{"numeric source", `{"metadata":{"source_kind":2},"graph":{"nodes":[]}}`, false},
		{"trailing JSON", validOpenGraph + `{}`, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateJSON([]byte(testCase.payload)); (err == nil) != testCase.valid {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}

func TestOpenGraphEdgeValidation(t *testing.T) {
	for _, testCase := range []struct{ name, edge string }{
		{"missing kind", `{"start":{"value":"a"},"end":{"value":"b"}}`},
		{"invalid kind", `{"start":{"value":"a"},"end":{"value":"b"},"kind":"EXP-Link"}`},
		{"reserved kind", `{"start":{"value":"a"},"end":{"value":"b"},"kind":"Tag"}`},
		{"scalar reference", `{"start":"a","end":{"value":"b"},"kind":"EXP_Link"}`},
		{"missing reference", `{"start":{"value":"a"},"kind":"EXP_Link"}`},
		{"missing value", `{"start":{},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"numeric value", `{"start":{"value":1},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"null strategy", `{"start":{"match_by":null,"value":"a"},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"unknown strategy", `{"start":{"match_by":"other","value":"a"},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"numeric kind filter", `{"start":{"kind":2,"value":"a"},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"id with matchers", `{"start":{"value":"a","property_matchers":[]},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"property with value", `{"start":{"match_by":"property","value":"a","property_matchers":[{"key":"name","operator":"equals","value":"a"}]},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"empty matchers", `{"start":{"match_by":"property","property_matchers":[]},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"missing matchers", `{"start":{"match_by":"property"},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"missing matcher key", `{"start":{"match_by":"property","property_matchers":[{"operator":"equals","value":"a"}]},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"wrong matcher operator", `{"start":{"match_by":"property","property_matchers":[{"key":"name","operator":"contains","value":"a"}]},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"null matcher value", `{"start":{"match_by":"property","property_matchers":[{"key":"name","operator":"equals","value":null}]},"end":{"value":"b"},"kind":"EXP_Link"}`},
		{"nested edge property", `{"start":{"value":"a"},"end":{"value":"b"},"kind":"EXP_Link","properties":{"x":{}}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateJSON([]byte(`{"graph":{"edges":[` + testCase.edge + `]}}`)); err == nil {
				t.Fatal("accepted malformed edge")
			}
		})
	}
}

func writeOpenGraphZIP(t *testing.T, path string, payloads ...string) {
	t.Helper()
	var (
		buffer bytes.Buffer
		writer = zip.NewWriter(&buffer)
	)
	for index, payload := range payloads {
		var member, err = writer.Create(fmt.Sprintf("collection%d.json", index))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(member, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenGraphMixedUpload(t *testing.T) {
	var (
		paths   = []string{filepath.Join(t.TempDir(), "native.json"), filepath.Join(t.TempDir(), "graph.json"), filepath.Join(t.TempDir(), "mixed.zip")}
		uploads int
		client  = testClient(t, func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/api/v2/file-upload/start":
				fmt.Fprint(response, `{"data":{"id":42}}`)
			case "/api/v2/file-upload/42":
				var (
					expected, err = os.ReadFile(paths[uploads])
					actual        []byte
					readErr       error
				)
				if err != nil {
					t.Error(err)
					return
				}
				actual, readErr = io.ReadAll(request.Body)
				if readErr != nil || !bytes.Equal(actual, expected) {
					t.Error("upload bytes changed")
				}
				if uploads == 2 && request.Header.Get("Content-Type") != "application/zip" {
					t.Error("wrong ZIP content type")
				}
				uploads++
			case "/api/v2/file-upload/42/end":
			case "/api/v2/file-upload":
				fmt.Fprint(response, `{"data":[`+completeJobJSON(42)+`]}`)
			default:
				t.Errorf("unexpected request %s", request.URL.Path)
			}
		})
	)
	for index, payload := range []string{validCollection, validOpenGraph} {
		if err := os.WriteFile(paths[index], []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeOpenGraphZIP(t, paths[2], validCollection, validOpenGraph)
	if _, err := client.UploadAndWait(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if uploads != len(paths) {
		t.Fatalf("uploaded %d files", uploads)
	}
}

func TestMalformedOpenGraphPreventsBatchUpload(t *testing.T) {
	for _, extension := range []string{"json", "zip"} {
		t.Run(extension, func(t *testing.T) {
			var (
				paths          = []string{filepath.Join(t.TempDir(), "valid.json"), filepath.Join(t.TempDir(), "invalid."+extension)}
				client         = testClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("malformed batch sent HTTP request") })
				invalidPayload = `{"graph":{"edges":[{"start":"wrong","end":{"value":"native-id"},"kind":"EXP_Link"}]}}`
			)
			if err := os.WriteFile(paths[0], []byte(validCollection), 0600); err != nil {
				t.Fatal(err)
			}
			if extension == "zip" {
				writeOpenGraphZIP(t, paths[1], validOpenGraph, invalidPayload)
			} else if err := os.WriteFile(paths[1], []byte(invalidPayload), 0600); err != nil {
				t.Fatal(err)
			}
			if receipt, err := client.UploadAndWait(context.Background(), paths); err == nil || receipt.JobID != 0 {
				t.Fatalf("malformed batch accepted: %+v %v", receipt, err)
			}
		})
	}
}

func TestOpenGraphUploadPreservesFailureReceipt(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		status     int
		failed     int
		partial    int
		statusName string
	}{
		{"failed", 5, 1, 0, "failed"},
		{"partial status", 8, 0, 1, "partiallycomplete"},
		{"partial file", 2, 0, 1, "complete"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				path   = filepath.Join(t.TempDir(), "graph.json")
				calls  int
				client = testClient(t, func(response http.ResponseWriter, request *http.Request) {
					calls++
					switch request.URL.Path {
					case "/api/v2/file-upload/start":
						fmt.Fprint(response, `{"data":{"id":42}}`)
					case "/api/v2/file-upload/42", "/api/v2/file-upload/42/end":
					case "/api/v2/file-upload":
						fmt.Fprintf(response, `{"data":[{"id":42,"status":%d,"total_files":1,"failed_files":%d,"partial_failed_files":%d,"end_time":"2026-10-07T12:00:00Z"}]}`, testCase.status, testCase.failed, testCase.partial)
					default:
						t.Errorf("unexpected request %s", request.URL.Path)
					}
				})
			)
			if err := os.WriteFile(path, []byte(validOpenGraph), 0600); err != nil {
				t.Fatal(err)
			}
			if receipt, err := client.UploadAndWait(context.Background(), []string{path}); err == nil || receipt.JobID != 42 || receipt.Status != testCase.statusName {
				t.Fatalf("unsuccessful job receipt lost: %+v %v", receipt, err)
			}
			if calls != 4 {
				t.Fatalf("unexpected failure sequence: %d requests", calls)
			}
		})
	}
}
