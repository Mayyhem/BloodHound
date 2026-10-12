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
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStackRequiresMatchingRunningImageAndBuildRevision(t *testing.T) {
	var (
		reference = strings.Repeat("a", 40)
		cases     = []struct {
			name, image, revision string
			running               bool
			accepted              bool
		}{
			{"matching build", "esc1-poc-bloodhound:" + reference, reference, true, true},
			{"tag without matching build", "esc1-poc-bloodhound:" + reference, strings.Repeat("b", 40), true, false},
			{"different image", "bloodhound:latest", reference, true, false},
			{"stopped container", "esc1-poc-bloodhound:" + reference, reference, false, false},
		}
	)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var calls [][]string
			application := stack{composePath: "test-compose.yaml", command: func(_ context.Context, arguments ...string) ([]byte, error) {
				calls = append(calls, arguments)
				if reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "ps", "-q", "bloodhound"}) {
					return []byte("container-id\n"), nil
				}
				if len(arguments) == 4 && arguments[0] == "inspect" && arguments[1] == "--format" && arguments[3] == "container-id" {
					return []byte(fmt.Sprintf(`{"image":%q,"revision":%q,"running":%t}`, testCase.image, testCase.revision, testCase.running)), nil
				}
				if reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "port", "bloodhound", "8080"}) {
					return []byte("127.0.0.1:18080\n"), nil
				}
				if reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "port", "graph-db", "7687"}) {
					return []byte("127.0.0.1:17687\n"), nil
				}
				return nil, fmt.Errorf("unexpected Docker command %v", arguments)
			}}
			err := application.verify(context.Background(), reference)
			expectedCalls := 2
			if testCase.accepted {
				expectedCalls = 4
			}
			if (err == nil) != testCase.accepted || len(calls) != expectedCalls {
				t.Fatalf("verification error=%v calls=%v", err, calls)
			}
		})
	}
}

func TestStackRejectsAmbiguousContainerBeforeInspecting(t *testing.T) {
	for _, output := range []string{"", "first\nsecond\n"} {
		var calls int
		application := stack{command: func(_ context.Context, _ ...string) ([]byte, error) { calls++; return []byte(output), nil }}
		if err := application.verify(context.Background(), strings.Repeat("a", 40)); err == nil || calls != 1 {
			t.Fatalf("ambiguous containers inspected: error=%v calls=%d", err, calls)
		}
	}
}

func TestStackRestartsAfterFailuresUsingFreshContext(t *testing.T) {
	var (
		stopFailure    = errors.New("stop failed")
		actionFailure  = errors.New("calculation failed")
		restartFailure = errors.New("restart failed")
		cases          = []struct {
			name                                 string
			stopError, actionError, restartError error
			cancelDuringAction                   bool
		}{
			{"successful run", nil, nil, nil, false},
			{"stop failure", stopFailure, nil, nil, false},
			{"calculation failure", nil, actionFailure, nil, false},
			{"canceled calculation", nil, context.Canceled, nil, true},
			{"restart failure after successful publication", nil, nil, restartFailure, false},
			{"both calculation and recovery fail", nil, actionFailure, restartFailure, false},
		}
	)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				operationContext, cancel = context.WithCancel(context.Background())
				calls                    [][]string
				actionCalled             bool
			)
			defer cancel()
			application := stack{composePath: "test-compose.yaml", command: func(ctx context.Context, arguments ...string) ([]byte, error) {
				calls = append(calls, arguments)
				switch {
				case reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "stop", "-t", "30", "bloodhound"}):
					return nil, testCase.stopError
				case reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "start", "bloodhound"}):
					if ctx.Err() != nil {
						t.Fatal("recovery inherited canceled calculation context")
					}
					if _, bounded := ctx.Deadline(); !bounded {
						t.Fatal("recovery has no deadline")
					}
					return nil, testCase.restartError
				default:
					return nil, fmt.Errorf("unexpected Docker command %v", arguments)
				}
			}}
			err := application.withBloodHoundStopped(operationContext, func(context.Context) error {
				actionCalled = true
				if testCase.cancelDuringAction {
					cancel()
				}
				return testCase.actionError
			})
			if actionCalled != (testCase.stopError == nil) || len(calls) != 2 {
				t.Fatalf("unsafe control sequence: action=%t calls=%v", actionCalled, calls)
			}
			for _, expected := range []error{testCase.stopError, testCase.actionError, testCase.restartError} {
				if expected != nil && !errors.Is(err, expected) {
					t.Fatalf("lost failure %v in result %v", expected, err)
				}
			}
			if testCase.stopError == nil && testCase.actionError == nil && testCase.restartError == nil && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkLockExcludesAnotherCommandAndReleasesOnClose(t *testing.T) {
	var (
		directory  = t.TempDir()
		first, err = lockWork(directory)
	)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockWork(directory); err == nil {
		second.Close()
		t.Fatal("another command acquired the active lock")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := lockWork(directory)
	if err != nil {
		t.Fatalf("closed command retained lock: %v", err)
	}
	defer third.Close()
}

func TestStackRejectsPortsThatPointOutsidePreparedStack(t *testing.T) {
	var cases = []struct {
		name, appPort, graphPort string
		accepted                 bool
	}{
		{"prepared ports", "127.0.0.1:18080", "127.0.0.1:17687", true},
		{"different application", "127.0.0.1:80", "127.0.0.1:17687", false},
		{"different database", "127.0.0.1:18080", "127.0.0.1:7687", false},
		{"exposed database", "127.0.0.1:18080", "0.0.0.0:17687", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			application := stack{composePath: "test-compose.yaml", command: func(_ context.Context, arguments ...string) ([]byte, error) {
				if reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "port", "bloodhound", "8080"}) {
					return []byte(testCase.appPort), nil
				}
				if reflect.DeepEqual(arguments, []string{"compose", "-f", "test-compose.yaml", "port", "graph-db", "7687"}) {
					return []byte(testCase.graphPort), nil
				}
				return nil, fmt.Errorf("unexpected Docker command %v", arguments)
			}}
			if err := application.verifyPorts(context.Background()); (err == nil) != testCase.accepted {
				t.Fatalf("port verification result: %v", err)
			}
		})
	}
}

type readinessTransport func(*http.Request) (*http.Response, error)

func (s readinessTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return s(request)
}

func TestAPIReadinessWaitsThroughStartupAndAuthenticates(t *testing.T) {
	var (
		calls  int
		client = &http.Client{Transport: readinessTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.URL.String() != "http://127.0.0.1:18080/api/version" || request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatal("readiness request did not use the authenticated local version route")
			}
			if calls == 1 {
				return nil, fmt.Errorf("listener is not ready")
			}
			status := http.StatusServiceUnavailable
			if calls == 3 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
		})}
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	)
	defer cancel()
	if err := waitAPI(ctx, client, "test-token", time.Millisecond); err != nil || calls != 3 {
		t.Fatalf("readiness error=%v calls=%d", err, calls)
	}
}

func TestAPIReadinessRejectsBadCredentialsWithoutRetrying(t *testing.T) {
	var (
		calls  int
		client = &http.Client{Transport: readinessTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
	)
	if err := waitAPI(context.Background(), client, "invalid-token", time.Hour); err == nil || calls != 1 {
		t.Fatalf("invalid credentials retried: error=%v calls=%d", err, calls)
	}
}

func TestAPIReadinessHonorsCancellationDuringRetry(t *testing.T) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		client      = &http.Client{Transport: readinessTransport(func(*http.Request) (*http.Response, error) {
			cancel()
			return nil, fmt.Errorf("listener is not ready")
		})}
	)
	defer cancel()
	if err := waitAPI(ctx, client, "test-token", time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("readiness did not honor cancellation: %v", err)
	}
}

func TestCompareRejectsVaultBeforeEndpointsAndCredentials(t *testing.T) {
	var configuration = configuration{
		extensionDirectory: "examples/vault/extension",
		workDirectory:      t.TempDir(),
		composePath:        "nonexistent-compose.yaml",
	}
	// Invalid endpoint values and an empty Docker search path prove this local
	// extension check completes before any prepared-stack access is needed.
	t.Setenv("BH_URL", "http://example.invalid")
	t.Setenv("NEO4J_URI", "bolt://example.invalid")
	t.Setenv("BH_TOKEN", "")
	t.Setenv("NEO4J_PASSWORD", "")
	t.Setenv("PATH", t.TempDir())
	if err := comparePublished(context.Background(), configuration); err == nil || !strings.Contains(err.Error(), "compare_native_esc1 enabled") || !strings.Contains(err.Error(), "written expectations") {
		t.Fatalf("expected local explanation for Vault comparison, got %v", err)
	}
}
