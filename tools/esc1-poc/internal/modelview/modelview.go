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

// Package modelview renders a proposed OpenGraph type model as an offline viewer.
package modelview

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed viewer.html
var viewer string

// Render returns a standalone HTML page. JSON is decoded and marshaled again so
// labels containing HTML cannot terminate the embedded application/json script.
// Every relationship needs explicit endpoints; no endpoint pairs are inferred.
func Render(definition []byte) ([]byte, error) {
	var (
		model   map[string]any
		encoded []byte
		err     error
	)
	if err = json.Unmarshal(definition, &model); err != nil {
		return nil, fmt.Errorf("decode model definition: %w", err)
	}
	if err = validate(model); err != nil {
		return nil, err
	}
	if encoded, err = json.Marshal(model); err != nil {
		return nil, fmt.Errorf("encode model definition: %w", err)
	}
	return []byte(strings.Replace(viewer, "__MODEL_JSON__", string(encoded), 1)), nil
}

func validate(model map[string]any) error {
	var (
		kinds                          = make(map[string]bool)
		relationshipNames              = make(map[string]bool)
		nodes, nodesOK                 = model["node_kinds"].([]any)
		relationships, relationshipsOK = model["relationship_kinds"].([]any)
		schema, schemaOK               = model["schema"].(map[string]any)
		schemaName, schemaNameOK       = schema["name"].(string)
	)
	if !schemaOK || !schemaNameOK || schemaName == "" {
		return fmt.Errorf("model definition requires schema metadata with a name")
	}
	if !nodesOK || !relationshipsOK {
		return fmt.Errorf("model definition requires node_kinds and relationship_kinds arrays")
	}
	for _, value := range nodes {
		var (
			kind, objectOK = value.(map[string]any)
			name, nameOK   = kind["name"].(string)
		)
		if !objectOK || !nameOK || name == "" || kinds[name] {
			return fmt.Errorf("node kinds must have unique, nonempty names")
		}
		kinds[name] = true
	}
	if externalValue, exists := model["external_node_kinds"]; exists {
		var external, externalOK = externalValue.([]any)
		if !externalOK {
			return fmt.Errorf("external_node_kinds must be an array")
		}
		for _, value := range external {
			var name, nameOK = value.(string)
			if !nameOK || name == "" || kinds[name] {
				return fmt.Errorf("external node kinds must have unique, nonempty names distinct from local types")
			}
			kinds[name] = true
		}
	}
	for _, value := range relationships {
		var (
			relationship, objectOK               = value.(map[string]any)
			name, nameOK                         = relationship["name"].(string)
			endpoints, endpointsOK               = relationship["endpoints"].([]any)
			traversableValue, traversablePresent = relationship["is_traversable"]
			_, traversableOK                     = traversableValue.(bool)
			endpointPairs                        = make(map[[2]string]bool)
		)
		if !objectOK || !nameOK || name == "" || (traversablePresent && !traversableOK) {
			return fmt.Errorf("relationship kinds require a name; is_traversable must be boolean when present")
		}
		if relationshipNames[name] {
			return fmt.Errorf("duplicate relationship kind %q", name)
		}
		relationshipNames[name] = true
		if value, present := relationship["existing_edge_policy"]; present {
			policy, valid := value.(string)
			if !valid || (policy != "replace-existing" && policy != "merge-properties" && policy != "keep-existing") {
				return fmt.Errorf("relationship %q existing_edge_policy must be replace-existing, merge-properties, or keep-existing", name)
			}
		}
		if !endpointsOK || len(endpoints) == 0 {
			return fmt.Errorf("relationship %q requires explicit nonempty endpoints", name)
		}
		for _, value := range endpoints {
			var (
				endpoint, endpointOK = value.(map[string]any)
				source, sourceOK     = endpoint["source_kind"].(string)
				target, targetOK     = endpoint["target_kind"].(string)
			)
			if !endpointOK || !sourceOK || !targetOK || !kinds[source] || !kinds[target] {
				return fmt.Errorf("relationship %q has an invalid or undeclared endpoint", name)
			}
			if endpointPairs[[2]string{source, target}] {
				return fmt.Errorf("relationship %q repeats endpoint pair %q to %q", name, source, target)
			}
			endpointPairs[[2]string{source, target}] = true
		}
	}
	return nil
}
