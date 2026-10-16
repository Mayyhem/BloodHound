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

package ruleservice

import (
	"fmt"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// Revision identifies the installed schema records, using the same value the
// workspace displays. The editor checks it while graph writers are paused.
func (s SchemaSnapshot) Revision() string { return s.revision() }

// EditorPlan validates a candidate against current installed definitions, then
// keeps only the candidate and its complete prerequisite chain. It never adds
// the candidate to active rules or includes downstream consumers.
func EditorPlan(bundle Bundle, snapshot SchemaSnapshot, name, text string) (extension.Plan, error) {
	var (
		files    = map[string]string{}
		selected = map[string]bool{}
	)
	if err := snapshot.validate(); err != nil {
		return extension.Plan{}, err
	}
	for key, value := range bundle.Files {
		files[key] = value
	}
	files[name] = text
	definitions, err := schemaDefinitions(snapshot.Definitions)
	if err != nil {
		return extension.Plan{}, err
	}
	plan, err := extension.LoadEditorFilesWithDisabled(definitions, files, bundle.Disabled, name)
	if err != nil {
		return extension.Plan{}, err
	}
	names, err := plan.SelectPrerequisites([]string{name})
	if err != nil {
		return extension.Plan{}, err
	}
	for _, key := range names {
		selected[key] = true
	}
	queries := make([]extension.Query, 0, len(names))
	for _, query := range plan.Queries {
		if selected[query.Name] {
			if query.DisabledReason != "" {
				return extension.Plan{}, fmt.Errorf("a needed prerequisite is disabled or blocked")
			}
			queries = append(queries, query)
		}
	}
	nodes := []extension.NodeBinding{}
	relationships := []extension.RelationshipBinding{}
	for _, binding := range plan.Schema.Nodes {
		if selected[binding.Query] {
			nodes = append(nodes, binding)
		}
	}
	for _, binding := range plan.Schema.Relationships {
		if selected[binding.Query] {
			relationships = append(relationships, binding)
		}
	}
	plan.Queries, plan.Schema.Nodes, plan.Schema.Relationships = queries, nodes, relationships
	plan.SchemaRevision = snapshot.revision()
	return plan, nil
}
