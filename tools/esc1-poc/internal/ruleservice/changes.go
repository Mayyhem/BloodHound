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
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

var ErrConflict = errors.New("the rules changed or the replacement has not been confirmed; refresh and review again")

// Change describes a user's choice. The two revisions prevent an old browser
// tab from replacing newer work or activating something different from its preview.
type Change struct {
	SchemaRevision  string            `json:"schema_revision,omitempty"`
	BaseRevision    string            `json:"base_revision"`
	DraftRevision   string            `json:"draft_revision,omitempty"`
	ConfirmRevision string            `json:"confirm_revision,omitempty"`
	Files           map[string]string `json:"files,omitempty"`
	Overwrite       []string          `json:"overwrite,omitempty"`
	Remove          []string          `json:"remove,omitempty"`
	Disable         []string          `json:"disable,omitempty"`
	Enable          []string          `json:"enable,omitempty"`
}

type Preview struct {
	Dependencies              []extension.ExecutionDependency `json:"dependencies"`
	BaseRevision              string                          `json:"base_revision"`
	Revision                  string                          `json:"revision"`
	Removed                   []string                        `json:"removed"`
	Overwritten               []string                        `json:"overwritten"`
	RetiredRelationships      []string                        `json:"retired_relationships"`
	RecalculatedRelationships []string                        `json:"recalculated_relationships"`
	Paused                    []string                        `json:"paused"`
	Plan                      PlanView                        `json:"plan"`
	Disabled                  []string                        `json:"disabled"`
	Enabled                   []string                        `json:"enabled"`
}

func (s *Store) View() PlanView {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return BuildView(s.active, s.plan)
}

func (s *Store) Preview(change Change) (Preview, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_, _, preview, err := s.candidate(change)
	return preview, err
}

func (s *Store) Apply(change Change) (Bundle, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	candidate, plan, _, err := s.candidate(change)
	if err != nil {
		return Bundle{}, err
	}
	if change.ConfirmRevision == "" || change.ConfirmRevision != candidate.Revision {
		return Bundle{}, ErrConflict
	}
	return s.activate(candidate, plan)
}

// Build everything privately while holding the store lock. Preview makes no
// disk changes. Apply repeats the checks before atomically saving the result.
func (s *Store) candidate(change Change) (Bundle, extension.Plan, Preview, error) {
	var (
		candidate = cloneBundle(s.active)
		preview   = Preview{BaseRevision: s.active.Revision, Removed: []string{}, Overwritten: []string{}}
		allowed   = map[string]bool{}
	)
	change, normalizeError := normalizeChange(change)
	if normalizeError != nil {
		return Bundle{}, extension.Plan{}, preview, normalizeError
	}
	if change.DraftRevision != "" {
		if s.draft.Revision != change.DraftRevision || s.draft.BaseRevision != s.active.Revision {
			return Bundle{}, extension.Plan{}, preview, ErrConflict
		}
		if len(change.Files)+len(change.Remove)+len(change.Enable)+len(change.Disable)+len(change.Overwrite) != 0 {
			return Bundle{}, extension.Plan{}, preview, fmt.Errorf("review the saved draft on its own; save any further edits first")
		}
		change.Files = s.draft.Files
		change.Overwrite = s.draft.Overwrite
	}
	if change.BaseRevision != s.active.Revision || change.SchemaRevision != "" && change.SchemaRevision != s.schemas.revision() {
		return Bundle{}, extension.Plan{}, preview, ErrConflict
	}
	if err := s.checkUploadedAssociations(change.Files, candidate.Schemas); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	schemas, err := s.reviewedSchemas(candidate.Schemas, change)
	if err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	candidate.Schemas, candidate.SchemaRevision = schemas, s.schemas.revision()
	if len(change.Files)+len(change.Remove)+len(change.Disable)+len(change.Enable) == 0 || len(change.Files) > MaxFiles || len(change.Remove) > MaxFiles || len(change.Overwrite) > MaxFiles || len(change.Disable) > MaxFiles || len(change.Enable) > MaxFiles {
		return Bundle{}, extension.Plan{}, preview, fmt.Errorf("choose files to upload or rules to remove, disable, or enable; at most %d of each", MaxFiles)
	}
	for _, name := range change.Overwrite {
		allowed[name] = true
	}
	for name, contents := range change.Files {
		if previous, exists := candidate.Files[name]; exists {
			oldKind, _, oldError := extension.QueryOutputKind(previous)
			newKind, _, newError := extension.QueryOutputKind(contents)
			if oldError == nil && newError == nil && oldKind != "" && newKind == "" {
				// Allow new standalone intermediates, while keeping the existing
				// protection against accidentally dropping a publisher's RETURN
				// kind and silently retiring all of its calculated connections.
				return Bundle{}, extension.Plan{}, preview, fmt.Errorf("the existing output declaration is missing; keep its kind or explicitly remove this producer before adding an intermediate file")
			}
		}
		if previous, exists := candidate.Files[name]; exists && previous != contents {
			if !allowed[name] {
				return Bundle{}, extension.Plan{}, preview, fmt.Errorf("%w: choose whether to overwrite %s, including its input declarations", ErrConflict, name)
			}
			preview.Overwritten = append(preview.Overwritten, name)
		}
		candidate.Files[name] = contents
	}
	// Check paths and byte limits before writing temporary validation files.
	if err := checkFiles(candidate.Files); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	removed, err := removeUnusedRules(candidate.Files, change.Remove, candidate.Schemas)
	if err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	preview.Removed = removed
	if err := checkFiles(candidate.Files); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	if err := changeEnabledRules(&candidate, change); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	for name := range candidate.Suspended {
		if _, exists := candidate.Files[name]; !exists {
			delete(candidate.Suspended, name)
		}
	}
	plan, err := loadBundle(candidate)
	if err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	if err := retainRetirements(s.plan, plan, s.active.Schemas, &candidate); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	for name := range candidate.AddedAt {
		if _, exists := candidate.Files[name]; !exists {
			delete(candidate.AddedAt, name)
		}
	}
	candidate.Revision = bundleRevision(candidate)
	if err := checkRetirements(candidate, plan); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	if err := applyRuleSettings(candidate, &plan); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	plan, err = checkInstalledPlan(candidate, plan, s.schemas)
	if err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	for _, query := range plan.Queries {
		if containsName(change.Enable, query.Name) && query.SchemaPaused {
			return Bundle{}, extension.Plan{}, preview, fmt.Errorf("cannot enable %s: %s", query.Name, query.DisabledReason)
		}
	}
	plan.RetiredEndpoints = copyEndpoints(candidate.RetiredEndpoints)
	preview.Disabled = append([]string{}, change.Disable...)
	preview.Enabled = append([]string{}, change.Enable...)
	preview.Revision = candidate.Revision
	preview.RetiredRelationships = append([]string(nil), candidate.RetiredRelationships...)
	preview.Plan = BuildView(candidate, plan)
	preview.Dependencies = []extension.ExecutionDependency{}
	for index := range preview.Plan.Queries {
		preview.Plan.Queries[index].Text = candidate.Files[preview.Plan.Queries[index].Name]
	}
	for _, query := range plan.Queries {
		preview.Dependencies = append(preview.Dependencies, plan.ExecutionDependencies(query)...)
	}
	preview.Paused = []string{}
	for _, query := range plan.Queries {
		if query.DisabledReason != "" {
			preview.Paused = append(preview.Paused, query.Name)
		}
	}
	sort.Strings(preview.Paused)
	preview.RecalculatedRelationships = changedRelationshipTypes(s.plan, plan, preview.Removed)
	sort.Strings(preview.Overwritten)
	return candidate, plan, preview, nil
}

// Removing a query also removes rules that need its returned rows. Graph
// readers stay and recalculate using the remaining producers. Keep the kind's
// display information in the schema, but detach its calculation. Finally walk
// backwards from the remaining outputs and discard unneeded scratch queries.
// The preview lists every removed file before the user activates this change.
func removeUnusedRules(files map[string]string, requested []string, schemas []InstalledSchema) ([]string, error) {
	var (
		dependencies = map[string][]extension.Dependency{}
		outputs      = map[string]bool{}
		removed      = map[string]bool{}
		needed       = map[string]bool{}
		independent  = map[string]bool{}
		names        = []string{}
	)
	for _, schema := range schemas {
		if len(schema.Definition) == 0 || string(schema.Definition) == "null" {
			continue
		}
		current, err := extension.ParseDefinition(schema.Definition)
		if err != nil {
			return nil, err
		}
		for _, kind := range current.NodeKinds {
			if kind.PostProcessing != nil {
				outputs[savedQueryKey(schema.Namespace, kind.PostProcessing.Query)] = true
			}
		}
		for _, kind := range current.RelationshipKinds {
			if kind.PostProcessing != nil {
				outputs[savedQueryKey(schema.Namespace, kind.PostProcessing.Query)] = true
			}
		}
	}
	for name, contents := range files {
		if extension.IsSchemaFile(name) {
			continue
		}
		inputs, err := extension.QueryDependencies(contents)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		dependencies[name] = inputs
		kind, _, err := extension.QueryOutputKind(contents)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if kind != "" {
			outputs[name] = true
		}
	}
	// A developer may activate row-only intermediate files before they have a
	// consumer. Preserve those independent files when deleting another branch.
	// Scratch prerequisites that belonged to a removed output still prune as
	// before, so the review does not leave a broken calculation behind.
	originalInputs := map[string]bool{}
	for name := range outputs {
		originalInputs[name] = true
	}
	for changed := true; changed; {
		changed = false
		for name := range originalInputs {
			for _, input := range dependencies[name] {
				if input.Type == "query" && !originalInputs[input.Name] {
					originalInputs[input.Name] = true
					changed = true
				}
			}
		}
	}
	for name := range dependencies {
		if !originalInputs[name] {
			independent[name] = true
		}
	}
	for _, name := range requested {
		if _, exists := dependencies[name]; !exists {
			return nil, fmt.Errorf("cannot remove unknown rule %q", name)
		}
		removed[name] = true
	}
	// Uploading a malformed replacement must fail validation, rather than
	// silently pruning it and retiring the previous producer's output.
	if len(requested) == 0 {
		return names, nil
	}
	for changed := true; changed; {
		changed = false
		for name, inputs := range dependencies {
			if removed[name] {
				continue
			}
			for _, input := range inputs {
				if input.Type == "query" && removed[input.Name] {
					removed[name], changed = true, true
				}
			}
		}
	}
	for name := range independent {
		if !removed[name] {
			needed[name] = true
		}
	}
	for name := range outputs {
		if !removed[name] {
			needed[name] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for name := range needed {
			for _, input := range dependencies[name] {
				if input.Type == "query" && !needed[input.Name] {
					needed[input.Name], changed = true, true
				}
			}
		}
	}
	for name := range dependencies {
		if !needed[name] {
			delete(files, name)
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Deleting a producer refreshes its shared connections and the results of
// graph readers downstream. Those readers keep their files. This list keeps
// that graph cleanup separate from the files shown in the deletion preview.
func changedRelationshipTypes(previous, next extension.Plan, removed []string) []string {
	var (
		affectedQueries = map[string]bool{}
		affectedKinds   = map[string]bool{}
		result          = []string{}
	)
	for _, name := range removed {
		affectedQueries[name] = true
	}
	for changed := true; changed; {
		changed = false
		for _, plan := range []extension.Plan{previous, next} {
			for _, binding := range plan.Schema.Nodes {
				if affectedQueries[binding.Query] && !affectedKinds["node:"+binding.Kind] {
					affectedKinds["node:"+binding.Kind], changed = true, true
				}
			}
			for _, binding := range plan.Schema.Relationships {
				if affectedQueries[binding.Query] && !affectedKinds["relationship:"+binding.Kind] {
					affectedKinds["relationship:"+binding.Kind], changed = true, true
				}
			}
		}
		for _, query := range next.Queries {
			if affectedQueries[query.Name] {
				continue
			}
			for _, input := range query.Dependencies {
				if input.Type == "query" && affectedQueries[input.Name] || affectedKinds[input.Type+":"+input.Name] {
					affectedQueries[query.Name], changed = true, true
				}
			}
		}
		for _, binding := range next.Schema.Relationships {
			if affectedQueries[binding.Query] {
				continue
			}
			if affectedKinds["relationship:"+binding.Kind] {
				affectedQueries[binding.Query], changed = true, true
			}
			for _, endpoint := range binding.Endpoints {
				if affectedKinds["node:"+endpoint.SourceKind] || affectedKinds["node:"+endpoint.TargetKind] {
					affectedQueries[binding.Query], changed = true, true
				}
			}
		}
	}
	for kind := range affectedKinds {
		if strings.HasPrefix(kind, "relationship:") {
			result = append(result, strings.TrimPrefix(kind, "relationship:"))
		}
	}
	sort.Strings(result)
	return result
}
