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
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/bloodhound"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
)

var dockerName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

type containerStack struct {
	docker          *dockerAPI
	applicationName string
	graphName       string
	network         string
	applicationPort string
	graphPort       string
	graphDriver     string
	graphDatabase   string
	application     containerInfo
	graph           containerInfo
	baseURL         string
	graphURL        string
}

func newContainerStack() (*containerStack, error) {
	var result = &containerStack{
		docker:          localDockerAPI(),
		applicationName: environment("BH_CONTAINER", "esc1-poc-bloodhound-1"),
		graphName:       environment("NEO4J_CONTAINER", "esc1-poc-graph-db-1"),
		network:         environment("BH_DOCKER_NETWORK", "esc1-poc_default"),
		applicationPort: environment("BH_INTERNAL_PORT", "8080"),
		graphPort:       environment("NEO4J_INTERNAL_PORT", "7687"),
	}
	result.graphDriver = selectedGraphDriver()
	if result.graphDriver != "neo4j" && result.graphDriver != "pg" {
		return nil, fmt.Errorf("GRAPH_DRIVER must be neo4j or pg")
	}
	if result.graphDriver == "pg" {
		result.graphName = environment("POSTGRES_CONTAINER", "esc1-poc-pg-db-1")
		result.graphPort = environment("POSTGRES_INTERNAL_PORT", "5432")
	}
	for _, name := range []string{result.applicationName, result.graphName, result.network} {
		if !dockerName.MatchString(name) {
			return nil, fmt.Errorf("container and network names must contain only letters, numbers, dots, underscores, or hyphens")
		}
	}
	for _, port := range []string{result.applicationPort, result.graphPort} {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("container ports must be numbers from 1 through 65535")
		}
	}
	// Addresses come from verified Docker containers. An old host setting must
	// not silently send credentials or graph writes to a different installation.
	if os.Getenv("BH_URL") != "" || os.Getenv("NEO4J_URI") != "" || os.Getenv("NEO4J_DATABASE") != "" || os.Getenv("POSTGRES_URI") != "" {
		return nil, fmt.Errorf("container mode discovers its addresses; unset BH_URL, NEO4J_URI, and NEO4J_DATABASE")
	}
	return result, nil
}

func (s *containerStack) verify(ctx context.Context, _ string) error {
	// Container mode accepts an ordinary BloodHound image. Record its actual
	// image ID in the report; the query's research reference is not proof of
	// this image's source. ESC1 still checks the native answers before saving.
	var (
		application, applicationError = s.docker.inspect(ctx, s.applicationName)
		graph, graphError             = s.docker.inspect(ctx, s.graphName)
	)
	if err := errors.Join(applicationError, graphError); err != nil {
		return err
	}
	if application.ID == "" || graph.ID == "" || application.ID == graph.ID {
		return fmt.Errorf("BloodHound and Neo4j must be two different identified containers")
	}
	if !containerRunning(application) || !containerRunning(graph) {
		return fmt.Errorf("BloodHound and Neo4j must both be running and not paused or restarting")
	}
	if err := s.checkConnection(application, graph); err != nil {
		return err
	}
	if err := s.checkOtherContainers(ctx, application, graph); err != nil {
		return err
	}
	s.application, s.graph = application, graph
	// Resolve names afresh on every command, then use the verified addresses for
	// this run. A second check before stopping detects replacement or movement.
	s.baseURL = "http://" + net.JoinHostPort(application.NetworkSettings.Networks[s.network].IPAddress, s.applicationPort)
	if s.graphDriver == "pg" {
		s.graphURL = "postgresql://" + net.JoinHostPort(graph.NetworkSettings.Networks[s.network].IPAddress, s.graphPort) + "/" + s.graphDatabase
		if err := graphdb.VerifyPostgresGraphDriver(ctx, s.graphURL, environment("POSTGRES_USER", "bloodhound"), os.Getenv("POSTGRES_PASSWORD"), "pg"); err != nil {
			return err
		}
	} else {
		s.graphURL = "bolt://" + net.JoinHostPort(graph.NetworkSettings.Networks[s.network].IPAddress, s.graphPort)
	}
	return nil
}

func containerRunning(container containerInfo) bool {
	return container.State.Running && !container.State.Paused && !container.State.Restarting && !container.State.OOMKilled
}

func containerEnvironment(container containerInfo, name string) string {
	for _, setting := range container.Config.Env {
		key, value, found := strings.Cut(setting, "=")
		if found && strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func (s *containerStack) checkConnection(application, graph containerInfo) error {
	var (
		applicationNetwork, applicationFound = application.NetworkSettings.Networks[s.network]
		graphNetwork, graphFound             = graph.NetworkSettings.Networks[s.network]
		connection, err                      = applicationGraphConnection(application)
	)
	if !applicationFound || !graphFound || applicationNetwork.NetworkID == "" || applicationNetwork.NetworkID != graphNetwork.NetworkID {
		return fmt.Errorf("BloodHound and Neo4j must share BH_DOCKER_NETWORK")
	}
	for _, address := range []string{applicationNetwork.IPAddress, graphNetwork.IPAddress} {
		parsed := net.ParseIP(address)
		if parsed == nil || !parsed.IsPrivate() {
			return fmt.Errorf("Docker network must assign private IP addresses to both containers")
		}
	}
	if selectedGraphDriver() != containerEnvironment(application, "bhe_graph_driver") {
		return fmt.Errorf("GRAPH_DRIVER must match BloodHound's explicit bhe_graph_driver")
	}
	if selectedGraphDriver() == "pg" {
		if err != nil || connection.Scheme != "postgresql" || connection.Hostname() == "" || connection.User == nil || len(connection.Path) < 2 || strings.Contains(connection.Path[1:], "/") || connection.RawQuery != "" || connection.Fragment != "" {
			return fmt.Errorf("BloodHound must provide a single PostgreSQL connection through bhe_database_connection")
		}
		if connection.User.Username() != environment("POSTGRES_USER", "bloodhound") {
			return fmt.Errorf("POSTGRES_USER must match BloodHound's selected PostgreSQL role")
		}
		s.graphDatabase = strings.TrimPrefix(connection.Path, "/")
	} else if err != nil || connection.Hostname() == "" || (connection.Scheme != "neo4j" && connection.Scheme != "bolt") || connection.RawQuery != "" || connection.ForceQuery || connection.Fragment != "" || (connection.Path != "" && connection.Path != "/" && connection.Path != "/neo4j") {
		return fmt.Errorf("BloodHound must provide a single Neo4j connection through bhe_neo4j_connection")
	}
	if connection.Port() != s.graphPort || !containerHasHost(graph, s.network, connection.Hostname()) {
		return fmt.Errorf("the selected graph container and port do not match BloodHound's graph connection")
	}
	if len(application.HostConfig.ExtraHosts) != 0 {
		return fmt.Errorf("BloodHound extra_hosts overrides require review before automatic graph verification")
	}
	if binding := containerEnvironment(application, "bhe_bind_addr"); binding != "" {
		_, port, err := net.SplitHostPort(binding)
		if err != nil || port != s.applicationPort {
			return fmt.Errorf("BH_INTERNAL_PORT does not match BloodHound's bhe_bind_addr")
		}
	} else if s.applicationPort != "8080" {
		return fmt.Errorf("a nonstandard BH_INTERNAL_PORT requires an explicit matching bhe_bind_addr on BloodHound")
	}
	return nil
}

func containerHasHost(container containerInfo, network, host string) bool {
	var endpoint, found = container.NetworkSettings.Networks[network]
	if !found {
		return false
	}
	return host == endpoint.IPAddress || host == strings.TrimPrefix(container.Name, "/") || slices.Contains(endpoint.Aliases, host)
}

func (s *containerStack) checkOtherContainers(ctx context.Context, application, graph containerInfo) error {
	var (
		containers, err = s.docker.runningContainers(ctx)
		connection, _   = applicationGraphConnection(application)
	)
	if err != nil {
		return err
	}
	for _, other := range containers {
		// BloodHound may belong to more than one network. Its graph hostname
		// must not also name another container on any network it can use.
		for network := range application.NetworkSettings.Networks {
			if other.ID != graph.ID && containerHasHost(other, network, connection.Hostname()) {
				return fmt.Errorf("BloodHound's graph hostname identifies more than one container on its networks")
			}
		}
		if other.ID == application.ID || containerEnvironment(other, "bhe_graph_driver") != selectedGraphDriver() {
			continue
		}
		otherConnection, err := applicationGraphConnection(other)
		if err != nil {
			return fmt.Errorf("another BloodHound container has an unreadable graph connection")
		}
		for network := range other.NetworkSettings.Networks {
			if containerHasHost(graph, network, otherConnection.Hostname()) && otherConnection.Port() == s.graphPort {
				return fmt.Errorf("another running BloodHound container uses this Neo4j graph; use a maintenance arrangement that stops every writer")
			}
		}
	}
	return nil
}

func (s *containerStack) client() (*bloodhound.Client, error) {
	return bloodhound.NewContainer(s.baseURL, os.Getenv("BH_TOKEN"))
}

func (s *containerStack) database(ctx context.Context) (graphdb.Connection, error) {
	if selectedGraphDriver() == "pg" {
		if os.Getenv("POSTGRES_PASSWORD") == "" {
			return nil, fmt.Errorf("POSTGRES_PASSWORD is required for the existing graph")
		}
		return graphdb.OpenPostgresContainer(ctx, s.graphURL, environment("POSTGRES_USER", "bloodhound"), os.Getenv("POSTGRES_PASSWORD"))
	}
	if os.Getenv("NEO4J_PASSWORD") == "" {
		return nil, fmt.Errorf("NEO4J_PASSWORD is required for the existing graph")
	}
	return graphdb.OpenContainer(ctx, s.graphURL, environment("NEO4J_USER", "neo4j"), os.Getenv("NEO4J_PASSWORD"), "neo4j")
}

func (s *containerStack) ready(ctx context.Context) error {
	return waitAPIAt(ctx, readinessClient(), s.baseURL, os.Getenv("BH_TOKEN"), 250*time.Millisecond)
}

func (s *containerStack) images() map[string]string {
	return map[string]string{"bloodhound": s.application.Image, selectedGraphDriver(): s.graph.Image}
}

func (s *containerStack) withBloodHoundStopped(ctx context.Context, action func(context.Context) error) error {
	var (
		previousApplication = s.application
		previousGraph       = s.graph
		previousURL         = s.baseURL
		previousGraphURL    = s.graphURL
	)
	if err := s.verify(ctx, ""); err != nil {
		return err
	}
	if s.application.ID != previousApplication.ID || s.graph.ID != previousGraph.ID || s.baseURL != previousURL || s.graphURL != previousGraphURL {
		return fmt.Errorf("containers changed after the readiness check; retry without publishing changes")
	}
	return s.stopAndRun(ctx, action)
}

func (s *containerStack) stopAndRun(ctx context.Context, action func(context.Context) error) error {
	var result error
	func() {
		// Use the inspected immutable ID, not a name that might now refer to a
		// replacement container. Always try recovery, including after a lost
		// stop response or cancellation. A hard kill still needs manual start.
		defer func() {
			recovery, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := s.docker.request(recovery, http.MethodPost, "/containers/"+s.application.ID+"/start", nil); err != nil {
				result = errors.Join(result, fmt.Errorf("restart BloodHound manually; saved graph changes are not undone: %w", err))
			}
		}()
		if err := s.docker.request(ctx, http.MethodPost, "/containers/"+s.application.ID+"/stop?t=30", nil); err != nil {
			result = fmt.Errorf("stop BloodHound before calculating: %w", err)
			return
		}
		stopped, err := s.docker.inspect(ctx, s.application.ID)
		if err != nil {
			result = err
			return
		}
		if stopped.State.Running || stopped.State.Restarting || stopped.State.Paused || stopped.State.OOMKilled || (stopped.State.ExitCode != 0 && stopped.State.ExitCode != 143) {
			result = fmt.Errorf("BloodHound did not stop cleanly; existing results were kept")
			return
		}
		graph, err := s.docker.inspect(ctx, s.graph.ID)
		if err != nil {
			result = err
			return
		}
		if !containerRunning(graph) || graph.NetworkSettings.Networks[s.network].IPAddress != s.graph.NetworkSettings.Networks[s.network].IPAddress {
			result = fmt.Errorf("Neo4j changed or stopped before calculation; existing results were kept")
			return
		}
		result = action(ctx)
	}()
	return result
}
