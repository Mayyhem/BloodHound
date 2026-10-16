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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// WorkspaceView is one consistent read of the active rules and received reports.
// It contains no collected objects or answer rows. Connections exist only here,
// never in the database that stores BloodHound's collected graph.
type InstalledExtensionView struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
}

type WorkspaceView struct {
	InstalledExtensions []InstalledExtensionView        `json:"installed_extensions"`
	Plan                PlanView                        `json:"plan"`
	Dependencies        []extension.ExecutionDependency `json:"dependencies"`
	Status              Status                          `json:"status"`
	ObservedAt          time.Time                       `json:"observed_at"`
}

// Allow room for rule properties and dependency explanations as well as files.
// This bound applies to both the sender and receiver.
const MaxWorkspaceBytes = 4*MaxBundleBytes + maxStoredStatusBytes

func (s *Store) Workspace() WorkspaceView {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var view = WorkspaceView{
		Plan: BuildView(s.active, s.plan), Status: s.latestStatus(),
		Dependencies: []extension.ExecutionDependency{}, ObservedAt: time.Now().UTC(), InstalledExtensions: []InstalledExtensionView{},
	}
	// With no active rules, authors still need the installed schema revision to
	// review their first upload against the same definitions they inspected.
	if s.schemas.Definitions != nil {
		view.Plan.SchemaRevision = s.schemas.revision()
	}
	// Creation can choose any currently installed extension, even when no file
	// is active yet. Retained/deleted schema associations are not choices here.
	for _, schema := range s.schemas.Definitions {
		definition, err := extension.ParseDefinition(schema.Definition)
		if err != nil {
			continue
		}
		view.InstalledExtensions = append(view.InstalledExtensions, InstalledExtensionView{Namespace: schema.Namespace, Name: schema.Name, DisplayName: definition.Schema.DisplayName, Version: schema.Version})
	}
	sort.Slice(view.InstalledExtensions, func(i, j int) bool {
		return view.InstalledExtensions[i].Namespace < view.InstalledExtensions[j].Namespace
	})
	// The workspace uses actual dependencies, not the old broad connected groups.
	// Omit their duplicate query text from this response.
	view.Plan.Threads = []ThreadView{}
	for index := range view.Plan.Queries {
		// The runner may rewrite parameter names. Authors inspect their original file.
		view.Plan.Queries[index].Text = s.active.Files[view.Plan.Queries[index].Name]
		// These optional settings contain pointers; copy them before releasing the lock.
		if settings := view.Plan.Queries[index].Processing.Reachability; settings != nil {
			copied := *settings
			copied.Relationships = append([]string(nil), settings.Relationships...)
			copied.NodeKinds = append([]string(nil), settings.NodeKinds...)
			view.Plan.Queries[index].Processing.Reachability = &copied
		}
		if settings := view.Plan.Queries[index].Processing.GroupOverlap; settings != nil {
			copied := *settings
			view.Plan.Queries[index].Processing.GroupOverlap = &copied
		}
	}
	for _, query := range s.plan.Queries {
		view.Dependencies = append(view.Dependencies, s.plan.ExecutionDependencies(query)...)
	}
	return view
}

// Workspace reads one fixed destination. The shared client already refuses
// redirects, bypasses environment proxies, and bounds the request duration.
func (s Client) Workspace(ctx context.Context) (WorkspaceView, error) {
	var (
		view          WorkspaceView
		response, err = s.request(ctx, http.MethodGet, "/v1/workspace", nil)
	)
	if err != nil {
		return view, fmt.Errorf("cannot contact the rule service")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return view, fmt.Errorf("workspace is unavailable from the rule service")
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, MaxWorkspaceBytes+1))
	if err != nil || len(contents) > MaxWorkspaceBytes {
		return view, fmt.Errorf("workspace response is incomplete or exceeds its limit")
	}
	if err := json.Unmarshal(contents, &view); err != nil {
		return WorkspaceView{}, fmt.Errorf("invalid workspace response")
	}
	if view.ObservedAt.IsZero() || len(view.Plan.Queries) > MaxFiles || validateStatus(view.Status) != nil {
		return WorkspaceView{}, fmt.Errorf("invalid workspace response")
	}
	return view, nil
}
