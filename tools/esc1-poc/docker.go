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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Only Docker's local socket is used. The container needs no Docker executable,
// shell, or copy of the user's BloodHound Compose file.
type dockerAPI struct{ client *http.Client }

type containerInfo struct {
	ID    string `json:"Id"`
	Name  string
	Image string
	State struct {
		Running    bool
		Paused     bool
		Restarting bool
		OOMKilled  bool
		ExitCode   int
	}
	Config          struct{ Env []string }
	HostConfig      struct{ ExtraHosts []string }
	NetworkSettings struct{ Networks map[string]containerNetwork }
}

type containerNetwork struct {
	NetworkID string
	IPAddress string
	Aliases   []string
}

func localDockerAPI() *dockerAPI {
	var transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", "/var/run/docker.sock")
		},
	}
	return &dockerAPI{client: &http.Client{
		Transport: transport,
		Timeout:   45 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("Docker redirects are not allowed")
		},
	}}
}

func (s *dockerAPI) request(ctx context.Context, method, path string, result any) error {
	var (
		request, err = http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
		response     *http.Response
	)
	if err != nil {
		return err
	}
	response, err = s.client.Do(request)
	if err != nil {
		return fmt.Errorf("contact the local Docker socket: %w", err)
	}
	defer response.Body.Close()
	// Docker returns 304 when a requested start/stop is already satisfied.
	if response.StatusCode != http.StatusNotModified && (response.StatusCode < 200 || response.StatusCode >= 300) {
		// Inspect responses include secrets. Never print their contents.
		return fmt.Errorf("Docker %s %s: HTTP %d", method, path, response.StatusCode)
	}
	if result != nil {
		if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(result); err != nil {
			return fmt.Errorf("read Docker response: %w", err)
		}
	}
	return nil
}

func (s *dockerAPI) inspect(ctx context.Context, name string) (containerInfo, error) {
	var result containerInfo
	var err = s.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", &result)
	return result, err
}

func (s *dockerAPI) runningContainers(ctx context.Context) ([]containerInfo, error) {
	var (
		summaries []struct {
			ID string `json:"Id"`
		}
		result []containerInfo
	)
	if err := s.request(ctx, http.MethodGet, "/containers/json", &summaries); err != nil {
		return nil, err
	}
	for _, summary := range summaries {
		container, err := s.inspect(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, container)
	}
	return result, nil
}
