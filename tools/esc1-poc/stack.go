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
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type dockerCommand func(context.Context, ...string) ([]byte, error)

type stack struct {
	composePath string
	command     dockerCommand
}

func runDocker(ctx context.Context, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", arguments...).Output()
}

func (s stack) compose(ctx context.Context, arguments ...string) ([]byte, error) {
	var commandArguments = append([]string{"compose", "-f", s.composePath}, arguments...)
	return s.command(ctx, commandArguments...)
}

// Both the local tag and the image's build label must identify our reference.
// A convenient tag alone does not tell us which source produced the container.
func (s stack) verify(ctx context.Context, reference string) error {
	var (
		containerIDs, err = s.compose(ctx, "ps", "-q", "bloodhound")
		inspected         []byte
		identity          struct {
			Image    string `json:"image"`
			Revision string `json:"revision"`
			Running  bool   `json:"running"`
		}
	)
	if err != nil {
		return fmt.Errorf("find dedicated BloodHound container: %w", err)
	}
	identifiers := strings.Fields(string(containerIDs))
	if len(identifiers) != 1 {
		return fmt.Errorf("expected exactly one running bloodhound container in the dedicated Compose stack")
	}
	inspected, err = s.command(ctx, "inspect", "--format", `{"image":{{json .Config.Image}},"revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}},"running":{{json .State.Running}}}`, identifiers[0])
	if err != nil {
		return fmt.Errorf("inspect dedicated BloodHound container: %w", err)
	}
	if err := json.Unmarshal(inspected, &identity); err != nil {
		return fmt.Errorf("decode BloodHound container identity: %w", err)
	}
	expectedImage := "esc1-poc-bloodhound:" + reference
	if selectedGraphDriver() == "pg" {
		// The PostgreSQL lab uses a separate native reference build. Its
		// development version avoids stamping old release tags onto newer code.
		expectedImage = "esc1-poc-bloodhound-reference:" + reference
	}
	if !identity.Running || identity.Image != expectedImage || identity.Revision != reference {
		return fmt.Errorf("BloodHound must be running image %s with matching org.opencontainers.image.revision", expectedImage)
	}
	if selectedGraphDriver() == "pg" {
		if err := s.verifyPostgres(ctx, identifiers[0]); err != nil {
			return err
		}
	}
	return s.verifyPorts(ctx)
}

// Stopping the application is useful only when it writes to the database we
// are about to connect to. Keep both published endpoints fixed for this stack.
func (s stack) verifyPorts(ctx context.Context) error {
	var endpoints = []struct{ service, internalPort, published string }{
		{"bloodhound", "8080", "127.0.0.1:18080"},
		{"graph-db", "7687", "127.0.0.1:17687"},
	}
	if selectedGraphDriver() == "pg" {
		endpoints[1] = struct{ service, internalPort, published string }{"pg-db", "5432", "127.0.0.1:15432"}
	}
	for _, endpoint := range endpoints {
		published, err := s.compose(ctx, "port", endpoint.service, endpoint.internalPort)
		if err != nil {
			return fmt.Errorf("check %s published port: %w", endpoint.service, err)
		}
		if strings.TrimSpace(string(published)) != endpoint.published {
			return fmt.Errorf("dedicated %s must publish %s", endpoint.service, endpoint.published)
		}
	}
	return nil
}

// Stopping closes the application's database transactions. Pausing a container
// would leave those transactions open and could block our publication.
// The dedicated stack must have no additional or manual graph writers.
func (s stack) withBloodHoundStopped(ctx context.Context, action func(context.Context) error) error {
	var actionError error
	func() {
		// Recovery uses a fresh context even when the upload/run timeout expired.
		// Try it even after stop fails, because the app may have partly stopped.
		defer func() {
			recoveryContext, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if _, err := s.compose(recoveryContext, "start", "bloodhound"); err != nil {
				actionError = errors.Join(actionError, fmt.Errorf("restart BloodHound; manually restore the dedicated service before another run: %w", err))
			}
		}()
		if _, err := s.compose(ctx, "stop", "-t", "30", "bloodhound"); err != nil {
			actionError = fmt.Errorf("stop BloodHound before publication: %w", err)
			return
		}
		actionError = action(ctx)
	}()
	return actionError
}

// A separate open file description cannot take this lock while another CLI
// command owns it. Closing the file releases the lock, including on errors.
func lockWork(directory string) (*os.File, error) {
	var (
		lock *os.File
		err  error
	)
	if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	lock, err = os.OpenFile(filepath.Join(directory, "runner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("cannot acquire experiment lock; another command may be running: %w", err)
	}
	return lock, nil
}

// Compose start returns before the HTTP listener is ready. Wait for the real
// authenticated version route before submitting uploads or updating the schema.
func waitForAPI(ctx context.Context) error {
	return waitAPI(ctx, readinessClient(), os.Getenv("BH_TOKEN"), 250*time.Millisecond)
}

func readinessClient() *http.Client {
	var (
		transport = http.DefaultTransport.(*http.Transport).Clone()
		client    *http.Client
	)
	transport.Proxy = nil
	client = &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return fmt.Errorf("BloodHound readiness redirects are not allowed")
	}}
	return client
}

func waitAPI(ctx context.Context, client *http.Client, token string, interval time.Duration) error {
	return waitAPIAt(ctx, client, "http://127.0.0.1:18080", token, interval)
}

func waitAPIAt(ctx context.Context, client *http.Client, baseURL, token string, interval time.Duration) error {
	defer client.CloseIdleConnections()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/version", nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err == nil {
			// We need the status only. Limit the discarded body so readiness cannot
			// turn into an unbounded download from an unexpected local service.
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
			if response.StatusCode >= 300 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
				return fmt.Errorf("BloodHound readiness GET /api/version: HTTP %d", response.StatusCode)
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for BloodHound API readiness: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (s stack) verifyPostgres(ctx context.Context, applicationID string) error {
	var (
		applicationJSON, err = s.command(ctx, "inspect", applicationID)
		applications         []containerInfo
		graphIDs             []byte
		graphJSON            []byte
		graphs               []containerInfo
	)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(applicationJSON, &applications); err != nil || len(applications) != 1 {
		return fmt.Errorf("decode BloodHound graph configuration")
	}
	graphIDs, err = s.compose(ctx, "ps", "-q", "pg-db")
	if err != nil {
		return err
	}
	identifiers := strings.Fields(string(graphIDs))
	if len(identifiers) != 1 {
		return fmt.Errorf("expected one dedicated PostgreSQL container")
	}
	graphJSON, err = s.command(ctx, "inspect", identifiers[0])
	if err != nil {
		return err
	}
	if err = json.Unmarshal(graphJSON, &graphs); err != nil || len(graphs) != 1 {
		return fmt.Errorf("decode dedicated PostgreSQL identity")
	}
	if applications[0].ID == graphs[0].ID || !containerRunning(graphs[0]) {
		return fmt.Errorf("dedicated PostgreSQL must be a separate running container")
	}
	for network := range applications[0].NetworkSettings.Networks {
		verification := &containerStack{network: network, graphPort: "5432", applicationPort: "8080", graphDriver: "pg"}
		if err = verification.checkConnection(applications[0], graphs[0]); err == nil {
			if verification.graphDatabase != "bloodhound" {
				return fmt.Errorf("dedicated PostgreSQL must use database bloodhound")
			}
			return graphdb.VerifyPostgresGraphDriver(ctx, "postgresql://127.0.0.1:15432/bloodhound", environment("POSTGRES_USER", "bloodhound"), environment("POSTGRES_PASSWORD", "experiment-only"), "pg")
		}
	}
	return fmt.Errorf("BloodHound's PostgreSQL connection does not match the dedicated graph container: %w", err)
}
