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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type fakeContainerDocker struct {
	containers map[string]containerInfo
	names      map[string]string
	running    []string
	calls      []string
	afterStop  func()
	beforeStop func()
	stopError  bool
	startError bool
	startFresh bool
}

func dockerTestResponse(status int, result any) *http.Response {
	var payload, _ = json.Marshal(result)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(payload)))}
}

func (s *fakeContainerDocker) request(request *http.Request) (*http.Response, error) {
	var (
		path = request.URL.Path
		name string
	)
	s.calls = append(s.calls, request.Method+" "+request.URL.RequestURI())
	if request.Method == http.MethodGet && path == "/containers/json" {
		var summaries []map[string]string
		for _, identifier := range s.running {
			summaries = append(summaries, map[string]string{"Id": identifier})
		}
		return dockerTestResponse(http.StatusOK, summaries), nil
	}
	if !strings.HasPrefix(path, "/containers/") {
		return nil, fmt.Errorf("unexpected Docker path %s", path)
	}
	name = strings.TrimPrefix(path, "/containers/")
	if request.Method == http.MethodGet && strings.HasSuffix(name, "/json") {
		name = strings.TrimSuffix(name, "/json")
		if identifier, found := s.names[name]; found {
			name = identifier
		}
		if container, found := s.containers[name]; found {
			return dockerTestResponse(http.StatusOK, container), nil
		}
		return dockerTestResponse(http.StatusNotFound, "missing container"), nil
	}
	if request.Method == http.MethodPost && strings.HasSuffix(name, "/stop") {
		name = strings.TrimSuffix(name, "/stop")
		if s.beforeStop != nil {
			s.beforeStop()
		}
		if s.stopError {
			return dockerTestResponse(http.StatusInternalServerError, "secret credentials in Docker error body"), nil
		}
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		var container, found = s.containers[name]
		if !found {
			return dockerTestResponse(http.StatusNotFound, "missing container"), nil
		}
		container.State.Running = false
		container.State.ExitCode = 0
		s.containers[name] = container
		if s.afterStop != nil {
			s.afterStop()
		}
		return dockerTestResponse(http.StatusNoContent, nil), nil
	}
	if request.Method == http.MethodPost && strings.HasSuffix(name, "/start") {
		s.startFresh = request.Context().Err() == nil
		if _, hasDeadline := request.Context().Deadline(); !hasDeadline {
			s.startFresh = false
		}
		if s.startError {
			return dockerTestResponse(http.StatusInternalServerError, "secret credentials in Docker error body"), nil
		}
		return dockerTestResponse(http.StatusNoContent, nil), nil
	}
	return nil, fmt.Errorf("unexpected Docker request %s %s", request.Method, path)
}

func containerTestStack() (*containerStack, *fakeContainerDocker) {
	var (
		application = containerInfo{ID: "application-immutable-id", Name: "/ordinary-ce-app", Image: "sha256:ordinary-ce-image"}
		graph       = containerInfo{ID: "graph-immutable-id", Name: "/ordinary-ce-graph", Image: "sha256:ordinary-neo4j-image"}
		fakeDocker  = &fakeContainerDocker{names: map[string]string{"selected-app": application.ID, "selected-graph": graph.ID}}
	)
	application.State.Running, graph.State.Running = true, true
	application.Config.Env = []string{"bhe_graph_driver=neo4j", "bhe_neo4j_connection=neo4j://neo4j:password@graph-db:7687", "bhe_bind_addr=0.0.0.0:8080"}
	application.NetworkSettings.Networks = map[string]containerNetwork{"ordinary-network": {NetworkID: "network-immutable-id", IPAddress: "172.18.0.3", Aliases: []string{"bloodhound"}}}
	graph.NetworkSettings.Networks = map[string]containerNetwork{"ordinary-network": {NetworkID: "network-immutable-id", IPAddress: "172.18.0.2", Aliases: []string{"graph-db"}}}
	fakeDocker.containers = map[string]containerInfo{application.ID: application, graph.ID: graph}
	fakeDocker.running = []string{application.ID, graph.ID}
	return &containerStack{
		docker:          &dockerAPI{client: &http.Client{Transport: readinessTransport(fakeDocker.request)}},
		applicationName: "selected-app", graphName: "selected-graph", network: "ordinary-network",
		applicationPort: "8080", graphPort: "7687",
	}, fakeDocker
}

func (s *fakeContainerDocker) changeApplication(change func(*containerInfo)) {
	var application = s.containers["application-immutable-id"]
	change(&application)
	s.containers[application.ID] = application
}

func setContainerSetting(container *containerInfo, name, value string) {
	for index, setting := range container.Config.Env {
		if strings.HasPrefix(setting, name+"=") {
			container.Config.Env[index] = name + "=" + value
			return
		}
	}
	container.Config.Env = append(container.Config.Env, name+"="+value)
}

func containerMutationCalls(calls []string) []string {
	var result []string
	for _, call := range calls {
		if strings.HasPrefix(call, "POST ") {
			result = append(result, call)
		}
	}
	return result
}

func TestContainerStackAcceptsOrdinaryCEAndUsesImmutableIDs(t *testing.T) {
	var (
		application, fakeDocker = containerTestStack()
		actionCalled            bool
	)
	if err := application.verify(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if application.baseURL != "http://172.18.0.3:8080" || application.graphURL != "bolt://172.18.0.2:7687" {
		t.Fatalf("connections did not use inspected Docker addresses: %s %s", application.baseURL, application.graphURL)
	}
	if !reflect.DeepEqual(application.images(), map[string]string{"bloodhound": "sha256:ordinary-ce-image", "neo4j": "sha256:ordinary-neo4j-image"}) {
		t.Fatal("ordinary CE image identities were not recorded")
	}
	if err := application.withBloodHoundStopped(context.Background(), func(context.Context) error {
		actionCalled = true
		if fakeDocker.containers[application.application.ID].State.Running || !fakeDocker.containers[application.graph.ID].State.Running {
			t.Fatal("calculation ran without a stopped application and running graph")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !actionCalled || !fakeDocker.startFresh {
		t.Fatal("calculation or recovery did not run")
	}
	if expected := []string{"POST /containers/application-immutable-id/stop?t=30", "POST /containers/application-immutable-id/start"}; !reflect.DeepEqual(containerMutationCalls(fakeDocker.calls), expected) {
		t.Fatalf("container mutations used names or touched the graph: %v", fakeDocker.calls)
	}
}

func TestContainerVerificationRejectsUnsafeTargetsBeforeStopping(t *testing.T) {
	var cases = []struct {
		name   string
		change func(*containerStack, *fakeContainerDocker)
	}{
		{"wrong graph hostname", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) {
				setContainerSetting(container, "bhe_neo4j_connection", "neo4j://other-graph:7687")
			})
		}},
		{"wrong graph port", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) {
				setContainerSetting(container, "bhe_neo4j_connection", "neo4j://graph-db:17687")
			})
		}},
		{"PostgreSQL graph", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { setContainerSetting(container, "bhe_graph_driver", "pg") })
		}},
		{"missing graph driver", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { setContainerSetting(container, "bhe_graph_driver", "") })
		}},
		{"missing explicit connection", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { setContainerSetting(container, "bhe_neo4j_connection", "") })
		}},
		{"connection query", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) {
				setContainerSetting(container, "bhe_neo4j_connection", "neo4j://graph-db:7687?database=other")
			})
		}},
		{"wrong application port", func(application *containerStack, _ *fakeContainerDocker) { application.applicationPort = "18080" }},
		{"missing network", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { container.NetworkSettings.Networks = nil })
		}},
		{"different network identity", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) {
				var endpoint = container.NetworkSettings.Networks["ordinary-network"]
				endpoint.NetworkID = "other-network-id"
				container.NetworkSettings.Networks["ordinary-network"] = endpoint
			})
		}},
		{"public IP", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) {
				var endpoint = container.NetworkSettings.Networks["ordinary-network"]
				endpoint.IPAddress = "8.8.8.8"
				container.NetworkSettings.Networks["ordinary-network"] = endpoint
			})
		}},
		{"extra_hosts override", func(_ *containerStack, docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { container.HostConfig.ExtraHosts = []string{"graph-db:8.8.8.8"} })
		}},
		{"other BloodHound writer", func(_ *containerStack, docker *fakeContainerDocker) {
			var other = containerInfo{ID: "other-writer", Name: "/other-bloodhound"}
			other.Config.Env = []string{"bhe_graph_driver=neo4j", "bhe_neo4j_connection=bolt://graph-db:7687"}
			other.NetworkSettings.Networks = map[string]containerNetwork{"ordinary-network": {NetworkID: "network-immutable-id", IPAddress: "172.18.0.4"}}
			docker.containers[other.ID] = other
			docker.running = append(docker.running, other.ID)
		}},
		{"graph alias collision", func(_ *containerStack, docker *fakeContainerDocker) {
			var other = containerInfo{ID: "alias-collision", Name: "/other-service"}
			other.NetworkSettings.Networks = map[string]containerNetwork{"ordinary-network": {NetworkID: "network-immutable-id", IPAddress: "172.18.0.4", Aliases: []string{"graph-db"}}}
			docker.containers[other.ID] = other
			docker.running = append(docker.running, other.ID)
		}},
		{"graph alias collision on another application network", func(_ *containerStack, docker *fakeContainerDocker) {
			var other = containerInfo{ID: "alias-collision", Name: "/other-service"}
			docker.changeApplication(func(container *containerInfo) {
				container.NetworkSettings.Networks["second-network"] = containerNetwork{NetworkID: "second-network-id", IPAddress: "172.19.0.3"}
			})
			other.NetworkSettings.Networks = map[string]containerNetwork{"second-network": {NetworkID: "second-network-id", IPAddress: "172.19.0.4", Aliases: []string{"graph-db"}}}
			docker.containers[other.ID] = other
			docker.running = append(docker.running, other.ID)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				application, fakeDocker = containerTestStack()
				actionCalled            bool
			)
			if err := application.verify(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			testCase.change(application, fakeDocker)
			if err := application.verify(context.Background(), ""); err == nil {
				t.Fatal("unsafe target passed verification")
			}
			if err := application.withBloodHoundStopped(context.Background(), func(context.Context) error { actionCalled = true; return nil }); err == nil {
				t.Fatal("unsafe target was accepted")
			}
			if actionCalled || len(containerMutationCalls(fakeDocker.calls)) != 0 {
				t.Fatalf("unsafe target triggered calculation or container mutation: %v", fakeDocker.calls)
			}
		})
	}
}

func TestContainerReplacementAfterVerificationAborts(t *testing.T) {
	var (
		application, fakeDocker = containerTestStack()
		actionCalled            bool
	)
	if err := application.verify(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	var replacement = fakeDocker.containers[application.application.ID]
	replacement.ID = "replacement-application-id"
	fakeDocker.containers[replacement.ID] = replacement
	fakeDocker.names[application.applicationName] = replacement.ID
	fakeDocker.running = []string{replacement.ID, application.graph.ID}
	if err := application.withBloodHoundStopped(context.Background(), func(context.Context) error { actionCalled = true; return nil }); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("replacement did not abort: %v", err)
	}
	if actionCalled || len(containerMutationCalls(fakeDocker.calls)) != 0 {
		t.Fatalf("replacement triggered calculation or container mutation: %v", fakeDocker.calls)
	}
}

func TestContainerStopFailuresAlwaysRestart(t *testing.T) {
	var queryError = errors.New("query failed")
	var cases = []struct {
		name       string
		stopError  bool
		startError bool
		cancel     bool
		afterStop  func(*fakeContainerDocker)
		queryError error
		wantAction bool
	}{
		{name: "stop error", stopError: true},
		{name: "query error", queryError: queryError, wantAction: true},
		{name: "canceled calculation", cancel: true, wantAction: true},
		{name: "forced stop exit 137", afterStop: func(docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { container.State.ExitCode = 137 })
		}},
		{name: "application still running", afterStop: func(docker *fakeContainerDocker) {
			docker.changeApplication(func(container *containerInfo) { container.State.Running = true })
		}},
		{name: "graph stopped", afterStop: func(docker *fakeContainerDocker) {
			var graph = docker.containers["graph-immutable-id"]
			graph.State.Running = false
			docker.containers[graph.ID] = graph
		}},
		{name: "restart error preserves query error", queryError: queryError, startError: true, wantAction: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				application, fakeDocker = containerTestStack()
				ctx, cancel             = context.WithCancel(context.Background())
				actionCalled            bool
			)
			defer cancel()
			if err := application.verify(ctx, ""); err != nil {
				t.Fatal(err)
			}
			fakeDocker.stopError, fakeDocker.startError = testCase.stopError, testCase.startError
			fakeDocker.afterStop = func() {
				if testCase.afterStop != nil {
					testCase.afterStop(fakeDocker)
				}
			}
			var err = application.withBloodHoundStopped(ctx, func(actionContext context.Context) error {
				actionCalled = true
				if testCase.cancel {
					cancel()
					return actionContext.Err()
				}
				return testCase.queryError
			})
			if err == nil || actionCalled != testCase.wantAction {
				t.Fatalf("failure handling: error=%v calculation=%v", err, actionCalled)
			}
			if testCase.queryError != nil && !errors.Is(err, testCase.queryError) {
				t.Fatalf("query error lost: %v", err)
			}
			if testCase.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if testCase.startError && !strings.Contains(err.Error(), "restart BloodHound manually") {
				t.Fatalf("missing recovery instructions: %v", err)
			}
			if !fakeDocker.startFresh {
				t.Fatal("restart did not use a fresh bounded context")
			}
			if expected := []string{"POST /containers/application-immutable-id/stop?t=30", "POST /containers/application-immutable-id/start"}; !reflect.DeepEqual(containerMutationCalls(fakeDocker.calls), expected) {
				t.Fatalf("failure skipped recovery or mutated another container: %v", fakeDocker.calls)
			}
		})
	}
}

func TestDeploymentConfigurationRejectsUnknownModesAndOverrides(t *testing.T) {
	for _, key := range []string{"POSTPROCESSOR_MODE", "BH_URL", "NEO4J_URI", "NEO4J_DATABASE", "BH_CONTAINER", "NEO4J_CONTAINER", "BH_DOCKER_NETWORK", "BH_INTERNAL_PORT", "NEO4J_INTERNAL_PORT"} {
		t.Setenv(key, "")
	}
	t.Setenv("POSTPROCESSOR_MODE", "unknown")
	if _, err := configuredDeployment(configuration{command: "run"}); err == nil {
		t.Fatal("unknown deployment mode accepted")
	}
	t.Setenv("POSTPROCESSOR_MODE", "container")
	t.Setenv("NEO4J_PASSWORD", "")
	if _, err := configuredDeployment(configuration{command: "run"}); err == nil {
		t.Fatal("container run accepted without graph credentials")
	}
	t.Setenv("NEO4J_PASSWORD", "test-password")
	for _, testCase := range []struct{ key, value string }{
		{"BH_URL", "http://other-server"}, {"NEO4J_URI", "bolt://other-graph:7687"}, {"NEO4J_DATABASE", "other"},
		{"BH_CONTAINER", "bad/name"}, {"BH_DOCKER_NETWORK", "bad network"},
		{"BH_INTERNAL_PORT", "0"}, {"NEO4J_INTERNAL_PORT", "65536"},
	} {
		t.Run(testCase.key, func(t *testing.T) {
			t.Setenv(testCase.key, testCase.value)
			if _, err := configuredDeployment(configuration{command: "run"}); err == nil {
				t.Fatal("unsafe container configuration accepted")
			}
		})
	}
	if _, err := configuredDeployment(configuration{command: "run"}); err != nil {
		t.Fatalf("valid container settings rejected: %v", err)
	}
}

func TestCancellationDuringContainerStopStillRestarts(t *testing.T) {
	var (
		application, fakeDocker = containerTestStack()
		ctx, cancel             = context.WithCancel(context.Background())
		actionCalled            bool
	)
	defer cancel()
	if err := application.verify(ctx, ""); err != nil {
		t.Fatal(err)
	}
	fakeDocker.beforeStop = cancel
	if err := application.withBloodHoundStopped(ctx, func(context.Context) error { actionCalled = true; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop cancellation was not reported: %v", err)
	}
	if actionCalled || !fakeDocker.startFresh {
		t.Fatal("canceled stop ran calculation or skipped fresh-context recovery")
	}
	if expected := []string{"POST /containers/application-immutable-id/stop?t=30", "POST /containers/application-immutable-id/start"}; !reflect.DeepEqual(containerMutationCalls(fakeDocker.calls), expected) {
		t.Fatalf("canceled stop skipped recovery: %v", fakeDocker.calls)
	}
}

func TestPostgresContainerConnectionChecksSelectedHostAndDriver(t *testing.T) {
	t.Setenv("GRAPH_DRIVER", "pg")
	t.Setenv("POSTGRES_USER", "bloodhound")
	var (
		deployment, docker = containerTestStack()
		application        = docker.containers["application-immutable-id"]
		graph              = docker.containers["graph-immutable-id"]
	)
	deployment.graphPort = "5432"
	setContainerSetting(&application, "bhe_graph_driver", "pg")
	setContainerSetting(&application, "bhe_database_connection", "user=bloodhound password=test dbname=bloodhound host=graph-db port=5432")
	if err := deployment.checkConnection(application, graph); err != nil {
		t.Fatal(err)
	}
	if deployment.graphDatabase != "bloodhound" {
		t.Fatalf("database %q", deployment.graphDatabase)
	}
	for _, connection := range []string{
		"user=other-role dbname=bloodhound host=graph-db port=5432",
		"user=bloodhound dbname=bloodhound host=app-db port=5432",
		"user=bloodhound dbname=bloodhound host=graph-db port=25432",
		"postgresql://bloodhound:test@graph-db:5432/",
		"host=/var/run/postgresql dbname=bloodhound",
		"host=graph-db,unverified-db port=5432,5432 user=bloodhound dbname=bloodhound",
		"postgresql://bloodhound:test@graph-db:5432/bloodhound?search_path=alternate",
	} {
		setContainerSetting(&application, "bhe_database_connection", connection)
		if err := deployment.checkConnection(application, graph); err == nil {
			t.Fatalf("accepted mismatched connection %q", connection)
		}
	}
	setContainerSetting(&application, "bhe_database_connection", "user=bloodhound dbname=bloodhound host=graph-db port=5432")
	setContainerSetting(&application, "bhe_graph_driver", "neo4j")
	if err := deployment.checkConnection(application, graph); err == nil {
		t.Fatal("accepted wrong graph driver")
	}
}
