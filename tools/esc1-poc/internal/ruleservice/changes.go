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
	"encoding/json"
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
	BaseRevision              string   `json:"base_revision"`
	Revision                  string   `json:"revision"`
	Removed                   []string `json:"removed"`
	Overwritten               []string `json:"overwritten"`
	RetiredRelationships      []string `json:"retired_relationships"`
	RecalculatedRelationships []string `json:"recalculated_relationships"`
	Paused                    []string `json:"paused"`
	Plan                      PlanView `json:"plan"`
	Disabled                  []string `json:"disabled"`
	Enabled                   []string `json:"enabled"`
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
	if change.BaseRevision != s.active.Revision {
		return Bundle{}, extension.Plan{}, preview, ErrConflict
	}
	if len(change.Files)+len(change.Remove)+len(change.Disable)+len(change.Enable) == 0 || len(change.Files) > MaxFiles || len(change.Remove) > MaxFiles || len(change.Overwrite) > MaxFiles || len(change.Disable) > MaxFiles || len(change.Enable) > MaxFiles {
		return Bundle{}, extension.Plan{}, preview, fmt.Errorf("choose files to upload or rules to remove, disable, or enable; at most %d of each", MaxFiles)
	}
	for _, name := range change.Overwrite {
		allowed[name] = true
	}
	for name, contents := range change.Files {
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
	removed, err := removeUnusedRules(candidate.Files, change.Remove)
	if err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	preview.Removed = removed
	if err := checkFiles(candidate.Files); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	plan, err := loadFiles(s.directory, candidate.Files)
	if err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	if err := retainRetirements(s.plan, plan, &candidate); err != nil {
		return Bundle{}, extension.Plan{}, preview, err
	}
	if err := changeEnabledRules(&candidate, change); err != nil {
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
	plan.RetiredRelationships = append([]string(nil), candidate.RetiredRelationships...)
	plan.RetiredEndpoints = copyEndpoints(candidate.RetiredEndpoints)
	preview.Disabled = append([]string{}, change.Disable...)
	preview.Enabled = append([]string{}, change.Enable...)
	preview.Revision = candidate.Revision
	preview.RetiredRelationships = append([]string(nil), candidate.RetiredRelationships...)
	preview.Plan = BuildView(candidate, plan)
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
func removeUnusedRules(files map[string]string, requested []string) ([]string, error) {
	var (
		definitions  = map[string]extension.Definition{}
		dependencies = map[string][]extension.Dependency{}
		outputs      = map[string]bool{}
		removed      = map[string]bool{}
		needed       = map[string]bool{}
		names        = []string{}
	)
	for name, contents := range files {
		if !extension.IsSchemaFile(name) {
			continue
		}
		current, err := extension.ParseDefinition([]byte(contents))
		if err != nil {
			return nil, err
		}
		definitions[name] = current
		for _, kind := range current.NodeKinds {
			if kind.PostProcessing != nil {
				outputs[kind.PostProcessing.Query] = true
			}
		}
		for _, kind := range current.RelationshipKinds {
			if kind.PostProcessing != nil {
				outputs[kind.PostProcessing.Query] = true
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
	// Only legacy schemas contain calculation settings to detach. Modern
	// schema files, including formatting, survive rule removal unchanged.
	for filename, current := range definitions {
		var changed, keepsESC1 bool
		for index := range current.NodeKinds {
			kind := &current.NodeKinds[index]
			if kind.PostProcessing != nil && removed[kind.PostProcessing.Query] {
				kind.PostProcessing, changed = nil, true
			}
		}
		for index := range current.RelationshipKinds {
			kind := &current.RelationshipKinds[index]
			if kind.PostProcessing != nil && removed[kind.PostProcessing.Query] {
				kind.PostProcessing, changed = nil, true
			}
			if kind.Name == "PoC_ADCSESC1" && kind.PostProcessing != nil {
				keepsESC1 = true
			}
		}
		if changed {
			if current.PostProcessing != nil && !keepsESC1 {
				current.PostProcessing.CompareNativeESC1 = false
			}
			contents, err := json.MarshalIndent(current, "", "  ")
			if err != nil {
				return nil, err
			}
			files[filename] = string(contents) + "\n"
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
