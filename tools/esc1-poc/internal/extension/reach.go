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

package extension

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Reachability transforms the query's node metadata into reusable reachable sets.
// The query selects seeds; traversal visits each relevant node once, not paths.
type Reachability struct {
	Direction     string   `json:"direction"`
	Relationships []string `json:"relationships"`
	IncludeStart  bool     `json:"include_start"`
	ExpandResults bool     `json:"expand_results"`
	RejectCycles  bool     `json:"reject_cycles"`
	NodeKinds     []string `json:"node_kinds,omitempty"`
}

func parseReachability(text string, dependencies []Dependency) (*Reachability, error) {
	var result *Reachability
	for _, line := range leadingComments(text) {
		var comment = strings.TrimSpace(line)
		if !strings.HasPrefix(comment, "reachability:") {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("reachability directive may appear only once")
		}
		result = &Reachability{}
		var decoder = json.NewDecoder(strings.NewReader(strings.TrimSpace(strings.TrimPrefix(comment, "reachability:"))))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(result); err != nil {
			return nil, fmt.Errorf("invalid reachability directive: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("reachability directive must contain one JSON object")
		}
	}
	if result == nil {
		return nil, nil
	}
	if result.Direction != "incoming" && result.Direction != "outgoing" {
		return nil, fmt.Errorf("reachability direction must be incoming or outgoing")
	}
	if len(result.Relationships) == 0 {
		return nil, fmt.Errorf("reachability requires relationship kinds")
	}
	var declared = make(map[Dependency]bool, len(dependencies))
	for _, dependency := range dependencies {
		declared[dependency] = true
	}
	for kindType, kinds := range map[string][]string{"relationship": result.Relationships, "node": result.NodeKinds} {
		var seen = make(map[string]bool, len(kinds))
		for _, kind := range kinds {
			if !identifier.MatchString(kind) || seen[kind] {
				return nil, fmt.Errorf("invalid or repeated reachability %s kind %q", kindType, kind)
			}
			seen[kind] = true
			if !declared[Dependency{Type: kindType, Name: kind}] {
				return nil, fmt.Errorf("reachability %s kind %s needs a declared dependency", kindType, kind)
			}
		}
	}
	return result, nil
}

// A batch-on declaration is the author's promise that unioning answers from
// disjoint chunks produces the same relationship set as the complete input.
func parseBatchDependency(text string, dependencies []Dependency) (string, error) {
	var result string
	for _, line := range leadingComments(text) {
		var comment = strings.TrimSpace(line)
		if !strings.HasPrefix(comment, "batch-on:") {
			continue
		}
		if result != "" {
			return "", fmt.Errorf("batch-on directive may appear only once")
		}
		result = strings.TrimSpace(strings.TrimPrefix(comment, "batch-on:"))
		if !identifier.MatchString(result) {
			return "", fmt.Errorf("batch-on requires one prerequisite parameter name (without $)")
		}
	}
	if result != "" {
		var declared bool
		for _, dependency := range dependencies {
			if dependency.Type == "query" && dependency.AnswerParameter() == result {
				declared = true
			}
		}
		if !declared {
			return "", fmt.Errorf("batch-on requires a declared query prerequisite")
		}
	}
	return result, nil
}

// GroupOverlap applies ordered group-aware overlap to permission sides supplied
// by the seed query, using the declared membership prerequisite as evidence.
type GroupOverlap struct {
	Memberships string `json:"memberships"`
}

func parseGroupOverlap(text string, dependencies []Dependency) (*GroupOverlap, error) {
	var result *GroupOverlap
	for _, line := range leadingComments(text) {
		var comment = strings.TrimSpace(line)
		if !strings.HasPrefix(comment, "group-overlap:") {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("group-overlap directive may appear only once")
		}
		result = &GroupOverlap{}
		var decoder = json.NewDecoder(strings.NewReader(strings.TrimSpace(strings.TrimPrefix(comment, "group-overlap:"))))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(result); err != nil {
			return nil, fmt.Errorf("invalid group-overlap directive: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("group-overlap directive must contain one JSON object")
		}
	}
	if result == nil {
		return nil, nil
	}
	if !identifier.MatchString(result.Memberships) {
		return nil, fmt.Errorf("group-overlap requires the membership prerequisite parameter name (without $)")
	}
	var declared bool
	for _, dependency := range dependencies {
		if dependency.Type == "query" && dependency.AnswerParameter() == result.Memberships {
			declared = true
		}
	}
	if !declared {
		return nil, fmt.Errorf("group-overlap memberships needs a declared query prerequisite")
	}
	return result, nil
}
