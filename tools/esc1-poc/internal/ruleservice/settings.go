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

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

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
			// Refreshing just one producer would erase the paused producer's
			// connections. Pause the shared result until all can run again.
			pause(binding.Query, blockedKinds["relationship:"+binding.Kind])
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

// Remember which connection types no longer have any producing rules. The
// saved endpoint scope lets the runner clear old results even if a later schema
// changes the type's name. A shared type is refreshed by its remaining producers.
func retainRetirements(previous extension.Plan, next extension.Plan, candidate *Bundle) error {
	var retired = map[string]bool{}
	// Schema files also hold display information. Keep their identity stable
	// even when their last rule is deleted.
	for name, filename := range previous.SchemaFiles {
		if next.SchemaFiles[name] != filename {
			return fmt.Errorf("keep the schema name and filename for %s", name)
		}
	}
	for _, kind := range candidate.RetiredRelationships {
		retired[kind] = true
	}
	if candidate.RetiredEndpoints == nil {
		candidate.RetiredEndpoints = map[string][]extension.Endpoint{}
	}
	for _, binding := range previous.Schema.Relationships {
		retired[binding.Kind] = true
		candidate.RetiredEndpoints[binding.Kind] = append([]extension.Endpoint(nil), binding.Endpoints...)
	}
	for _, binding := range next.Schema.Relationships {
		delete(retired, binding.Kind)
		delete(candidate.RetiredEndpoints, binding.Kind)
	}
	candidate.RetiredRelationships = nil
	for kind := range retired {
		candidate.RetiredRelationships = append(candidate.RetiredRelationships, kind)
	}
	sort.Strings(candidate.RetiredRelationships)
	return nil
}

// Snapshots are independent: a caller changing an endpoint must not change the
// cleanup scope used by a running worker or saved in the registry.
func copyEndpoints(scopes map[string][]extension.Endpoint) map[string][]extension.Endpoint {
	var result = map[string][]extension.Endpoint{}
	for kind, endpoints := range scopes {
		result[kind] = append([]extension.Endpoint(nil), endpoints...)
	}
	return result
}
