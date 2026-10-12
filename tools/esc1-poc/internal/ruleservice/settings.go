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
	"fmt"
	"sort"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func copyOwners(owners map[string]string) map[string]string {
	var result = map[string]string{}
	for kind, owner := range owners {
		result[kind] = owner
	}
	return result
}

// Keep a user's disable choice separate from its consequences. A rule waiting
// on a disabled rule can resume automatically once all its inputs are available.
func applyRuleSettings(bundle Bundle, plan *extension.Plan) error {
	var (
		known   = map[string]bool{}
		reasons = map[string]string{}
	)
	for _, query := range plan.Queries {
		known[query.Name] = true
	}
	if len(bundle.Disabled) > MaxFiles || !sort.StringsAreSorted(bundle.Disabled) {
		return fmt.Errorf("disabled rules must be a sorted, bounded list")
	}
	for _, name := range bundle.Disabled {
		if !known[name] || reasons[name] != "" {
			return fmt.Errorf("unknown or repeated disabled rule %q", name)
		}
		reasons[name] = "Disabled by you. Existing connections are kept."
	}
	for name, added := range bundle.AddedAt {
		if !known[name] || added.IsZero() {
			return fmt.Errorf("invalid saved date for %s", name)
		}
	}
	// Query order already puts explicit prerequisites first. Repeat because a
	// declared graph input can also refer to an output from another rule.
	for changed := true; changed; {
		changed = false
		blockedKinds := map[string]string{}
		for _, binding := range plan.Schema.Nodes {
			if reasons[binding.Query] != "" {
				blockedKinds["node:"+binding.Kind] = binding.Query
			}
		}
		for _, binding := range plan.Schema.Relationships {
			if reasons[binding.Query] != "" {
				blockedKinds["relationship:"+binding.Kind] = binding.Query
			}
		}
		pause := func(name, prerequisite string) {
			if prerequisite != "" && reasons[name] == "" {
				reasons[name] = "Waiting for " + prerequisite + " to be enabled. Existing connections are kept."
				changed = true
			}
		}
		for _, query := range plan.Queries {
			for _, input := range query.Dependencies {
				if input.Type == "query" && reasons[input.Name] != "" {
					pause(query.Name, input.Name)
				}
				pause(query.Name, blockedKinds[input.Type+":"+input.Name])
			}
		}
		for _, binding := range plan.Schema.Relationships {
			for _, endpoint := range binding.Endpoints {
				pause(binding.Query, blockedKinds["node:"+endpoint.SourceKind])
				pause(binding.Query, blockedKinds["node:"+endpoint.TargetKind])
			}
		}
	}
	for index := range plan.Queries {
		plan.Queries[index].DisabledReason = reasons[plan.Queries[index].Name]
	}
	return nil
}

func changeEnabledRules(candidate *Bundle, change Change) error {
	var disabled = map[string]bool{}
	for _, name := range candidate.Disabled {
		if _, exists := candidate.Files[name]; exists {
			disabled[name] = true
		}
	}
	for _, name := range append(append([]string{}, change.Disable...), change.Enable...) {
		if _, exists := candidate.Files[name]; !exists || !queryName.MatchString(name) {
			return fmt.Errorf("cannot enable or disable unknown rule %q", name)
		}
	}
	for _, name := range change.Disable {
		disabled[name] = true
	}
	for _, name := range change.Enable {
		for _, other := range change.Disable {
			if name == other {
				return fmt.Errorf("choose either enable or disable for %s", name)
			}
		}
		delete(disabled, name)
	}
	candidate.Disabled = nil
	for name := range disabled {
		candidate.Disabled = append(candidate.Disabled, name)
	}
	sort.Strings(candidate.Disabled)
	return nil
}

// Ownership belongs to the individual extension, not to the combined table.
// Adding a second schema must never relabel the first one's existing results.
func retainOwnership(previous extension.Plan, next *extension.Plan, candidate *Bundle) error {
	var (
		known               = map[string]string{}
		retired             = copyOwners(candidate.RetiredOwners)
		previousDefinitions = map[string]extension.Definition{}
	)
	for _, definition := range previous.Definitions {
		previousDefinitions[definition.Schema.Name] = definition
	}
	for _, definition := range next.Definitions {
		owner := definition.PostProcessing.Owner
		if old, exists := previousDefinitions[definition.Schema.Name]; exists {
			if (definition.LegacyProcessing && old.PostProcessing.Owner != owner) || old.Schema.Namespace != definition.Schema.Namespace {
				return fmt.Errorf("keep the owner and namespace of extension %s", definition.Schema.Name)
			}
			owner = old.PostProcessing.Owner
		}
		if !definition.LegacyProcessing {
			if candidate.Owners == nil {
				candidate.Owners = map[string]string{}
			}
			candidate.Owners[definition.Schema.Name] = owner
		}
	}
	if err := applyOwners(candidate.Owners, next); err != nil {
		return err
	}
	// Schema files remain registered even after their final rule is removed.
	// Refuse renaming one by upload: that could also abandon its display types.
	for name, filename := range previous.SchemaFiles {
		if next.SchemaFiles[name] != filename {
			return fmt.Errorf("keep the schema name and filename for %s", name)
		}
	}
	for _, kind := range candidate.RetiredRelationships {
		if retired[kind] == "" {
			retired[kind] = previous.Schema.Owner
		}
		known[kind] = retired[kind]
	}
	for _, binding := range previous.Schema.Nodes {
		known[binding.Kind] = binding.EffectiveOwner(previous.Schema.Owner)
	}
	for _, binding := range previous.Schema.Relationships {
		known[binding.Kind] = binding.EffectiveOwner(previous.Schema.Owner)
		retired[binding.Kind] = known[binding.Kind]
	}
	for _, binding := range next.Schema.Nodes {
		if old, exists := known[binding.Kind]; exists && old != binding.EffectiveOwner(next.Schema.Owner) {
			return fmt.Errorf("%s is already owned by another extension", binding.Kind)
		}
	}
	for _, binding := range next.Schema.Relationships {
		if old, exists := known[binding.Kind]; exists && old != binding.EffectiveOwner(next.Schema.Owner) {
			return fmt.Errorf("%s is already owned by another extension", binding.Kind)
		}
		delete(retired, binding.Kind)
	}
	candidate.RetiredRelationships = nil
	candidate.RetiredOwners = retired
	for kind := range retired {
		candidate.RetiredRelationships = append(candidate.RetiredRelationships, kind)
	}
	sort.Strings(candidate.RetiredRelationships)
	return nil
}

// Saved identities belong to the registry, not uploaded schema files. Applying
// them after loading also preserves the single-extension run-state identity.
func applyOwners(owners map[string]string, plan *extension.Plan) error {
	var (
		known      = map[string]bool{}
		kindOwners = map[string]string{}
	)
	for index := range plan.Definitions {
		definition := &plan.Definitions[index]
		known[definition.Schema.Name] = true
		if owner, exists := owners[definition.Schema.Name]; exists {
			if strings.TrimSpace(owner) == "" || len(owner) > 256 {
				return fmt.Errorf("invalid saved owner for extension %s", definition.Schema.Name)
			}
			if definition.LegacyProcessing && definition.PostProcessing.Owner != owner {
				return fmt.Errorf("keep the owner of extension %s", definition.Schema.Name)
			}
			definition.PostProcessing.Owner = owner
		}
		for _, kind := range definition.NodeKinds {
			kindOwners[kind.Name] = definition.PostProcessing.Owner
		}
		for _, kind := range definition.RelationshipKinds {
			kindOwners[kind.Name] = definition.PostProcessing.Owner
		}
	}
	for name := range owners {
		if !known[name] {
			return fmt.Errorf("saved owner refers to unknown extension %s", name)
		}
	}
	for index := range plan.Schema.Nodes {
		binding := &plan.Schema.Nodes[index]
		binding.Owner = kindOwners[binding.Kind]
	}
	for index := range plan.Schema.Relationships {
		binding := &plan.Schema.Relationships[index]
		binding.Owner = kindOwners[binding.Kind]
	}
	if len(plan.Definitions) > 0 {
		plan.Definition = plan.Definitions[0]
		if len(plan.Definitions) == 1 {
			plan.Schema.Owner = plan.Definition.PostProcessing.Owner
		}
	}
	return nil
}
