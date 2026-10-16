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

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func validateEndpoints(ctx context.Context, transaction Transaction, binding extension.RelationshipBinding, pairs []Pair) error {
	var resolved map[string][]string
	if reader, ok := transaction.(EndpointReader); ok {
		identifiers := []string{}
		seen := map[string]bool{}
		for _, pair := range pairs {
			for _, identifier := range []string{pair.Source, pair.Target} {
				if !seen[identifier] {
					seen[identifier] = true
					identifiers = append(identifiers, identifier)
				}
			}
		}
		var err error
		resolved, err = reader.EndpointKinds(ctx, identifiers)
		if err != nil {
			return err
		}
		for _, identifier := range identifiers {
			if len(resolved[identifier]) == 0 {
				side := "target"
				for _, pair := range pairs {
					if pair.Source == identifier {
						side = "source"
						break
					}
				}
				return &EndpointError{Kind: binding.Kind, Side: side, Object: identifier}
			}
		}
	}
	for _, pair := range pairs {
		var kinds [2]any
		for index, identifier := range []string{pair.Source, pair.Target} {
			if resolved != nil {
				kinds[index] = resolved[identifier]
				continue
			}
			rows, err := transaction.Read(ctx, "MATCH (item {objectid: $id}) RETURN labels(item) AS kinds", map[string]any{"id": identifier})
			if err != nil {
				return err
			}
			if len(rows) != 1 {
				return &EndpointError{Kind: binding.Kind, Side: []string{"source", "target"}[index], Object: identifier, Found: len(rows)}
			}
			kinds[index] = rows[0]["kinds"]
		}

		allowed := false
		for _, endpoint := range binding.Endpoints {
			// Both ends must match the SAME declared pair. Taking the source
			// from one rule and the target from another would invent permission.
			if hasAllowedKind(kinds[0], []string{endpoint.SourceKind}) && hasAllowedKind(kinds[1], []string{endpoint.TargetKind}) {
				allowed = true
				break
			}
		}
		if !allowed {
			return &EndpointError{Kind: binding.Kind, Object: pair.Source, PairOther: pair.Target}
		}
	}
	return nil
}

// publishHelper receives the current node found by the shared helper scan.
// It never looks up an unlabelled object ID once per helper: on Neo4j that
// would repeatedly scan the whole graph. The caller checks duplicate IDs.
func publishHelper(ctx context.Context, transaction Transaction, binding extension.NodeBinding, row, existing map[string]any, parameterLimit int64, writeToGraph bool) (int, error) {
	var (
		identifier, valid = row["id"].(string)
		stored            = map[string]any{"objectid": identifier}
	)
	if !valid || !strings.HasPrefix(identifier, binding.IDPrefix) || len(identifier) == len(binding.IDPrefix) {
		return 0, fmt.Errorf("helper id must begin with %q and include an identifier", binding.IDPrefix)
	}
	if len(row) != 3 || !onlyKind(row["kinds"], binding.Kind) {
		return 0, fmt.Errorf("helper row must contain id, kinds, properties, with only the approved kind %s", binding.Kind)
	}
	properties, valid := row["properties"].(map[string]any)
	if !valid {
		return 0, fmt.Errorf("helper properties must be a map")
	}
	// Copy author properties so assigning the object ID cannot change
	// the intermediate answers that another query may still need.
	for key, value := range properties {
		if key == "objectid" || key == "" {
			return 0, fmt.Errorf("helper property %q is reserved or empty", key)
		}
		if err := validateProperty(value); err != nil {
			return 0, fmt.Errorf("helper property %s: %w", key, err)
		}
		stored[key] = value
	}
	parameters := map[string]any{"id": identifier, "properties": stored}
	if encodedSize(parameters) > parameterLimit {
		return 0, fmt.Errorf("helper properties exceed the parameter budget")
	}
	if existing != nil {
		previous, valid := existing["properties"].(map[string]any)
		if !valid || !onlyKind(existing["kinds"], binding.Kind) {
			return 0, fmt.Errorf("helper id %q already identifies a different node kind", identifier)
		}
		previousJSON, _ := json.Marshal(previous)
		storedJSON, _ := json.Marshal(stored)
		if bytes.Equal(previousJSON, storedJSON) {
			return 0, nil
		}
	}
	if !writeToGraph {
		return 0, nil
	}
	var err error
	if publisher, ok := transaction.(Publisher); ok {
		err = publisher.PutNode(ctx, binding.Kind, stored)
	} else {
		_, err = transaction.Write(ctx, "MERGE (item:`"+binding.Kind+"` {objectid: $id}) SET item = $properties", parameters)
	}
	if err != nil {
		return 0, err
	}
	return 1, nil
}
