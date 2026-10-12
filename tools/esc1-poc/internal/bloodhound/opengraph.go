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

package bloodhound

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var edgeKindPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// These checks follow the pinned upload JSON schemas and reserved kind policy.
// References may resolve to nodes from earlier or later collection uploads.
func validateOpenGraph(graphPayload, metadataPayload json.RawMessage) error {
	var (
		graph    map[string]any
		metadata map[string]any
		found    bool
	)
	if err := json.Unmarshal(graphPayload, &graph); err != nil || graph == nil {
		return fmt.Errorf("graph must be an object")
	}
	if len(metadataPayload) > 0 {
		if err := json.Unmarshal(metadataPayload, &metadata); err != nil || metadata == nil {
			return fmt.Errorf("metadata must be an object")
		}
		for key, value := range metadata {
			if key != "source_kind" {
				return fmt.Errorf("unsupported metadata field %q", key)
			}
			if _, valid := value.(string); value != nil && !valid {
				return fmt.Errorf("metadata.source_kind must be a string or null")
			}
		}
	}
	for _, arrayName := range []string{"nodes", "edges"} {
		var (
			value, exists = graph[arrayName]
			items, valid  = value.([]any)
		)
		if !exists {
			continue
		}
		found = true
		if !valid {
			return fmt.Errorf("graph.%s must be an array", arrayName)
		}
		for index, value := range items {
			var item, valid = value.(map[string]any)
			if !valid {
				return fmt.Errorf("graph.%s[%d] must be an object", arrayName, index)
			}
			if err := validateGraphItem(arrayName, item); err != nil {
				return fmt.Errorf("graph.%s[%d]: %w", arrayName, index, err)
			}
		}
	}
	if !found {
		return fmt.Errorf("graph requires nodes or edges")
	}
	return nil
}

func validateGraphItem(arrayName string, item map[string]any) error {
	if arrayName == "nodes" {
		var kinds, valid = item["kinds"].([]any)
		if _, valid := item["id"].(string); !valid {
			return fmt.Errorf("node id must be a string")
		}
		if !valid || len(kinds) > 3 {
			return fmt.Errorf("node kinds must be an array of at most three strings")
		}
		for _, value := range kinds {
			var kind, valid = value.(string)
			if !valid {
				return fmt.Errorf("node kinds must contain strings")
			}
			if err := validateGraphKind(kind); err != nil {
				return err
			}
		}
	} else {
		var kind, valid = item["kind"].(string)
		if !valid || !edgeKindPattern.MatchString(kind) {
			return fmt.Errorf("edge kind must contain alphanumeric characters or underscores")
		}
		if err := validateGraphKind(kind); err != nil {
			return err
		}
		for _, endpoint := range []string{"start", "end"} {
			if err := validateGraphEndpoint(item[endpoint]); err != nil {
				return fmt.Errorf("%s: %w", endpoint, err)
			}
		}
	}
	if properties, exists := item["properties"]; exists {
		return validateGraphProperties(properties, arrayName == "nodes")
	}
	return nil
}

func validateGraphKind(kind string) error {
	var normalized = strings.ToLower(kind)
	if normalized == "tag" || strings.HasPrefix(normalized, "tag_") {
		return fmt.Errorf("kind %q uses the reserved tag namespace", kind)
	}
	return nil
}

func validateGraphEndpoint(value any) error {
	var (
		endpoint, valid = value.(map[string]any)
		strategy        = "id"
	)
	if !valid {
		return fmt.Errorf("endpoint must be an object")
	}
	if value, exists := endpoint["match_by"]; exists {
		if strategy, valid = value.(string); !valid {
			return fmt.Errorf("match_by must be a string")
		}
	}
	if value, exists := endpoint["kind"]; exists {
		if _, valid := value.(string); !valid {
			return fmt.Errorf("endpoint kind must be a string")
		}
	}
	switch strategy {
	case "id", "name":
		if _, exists := endpoint["property_matchers"]; exists {
			return fmt.Errorf("id or name endpoints cannot have property_matchers")
		}
		if _, valid := endpoint["value"].(string); !valid {
			return fmt.Errorf("id or name endpoints require a string value")
		}
	case "property":
		var matchers, valid = endpoint["property_matchers"].([]any)
		if _, exists := endpoint["value"]; exists {
			return fmt.Errorf("property endpoints cannot have value")
		}
		if !valid || len(matchers) == 0 {
			return fmt.Errorf("property endpoints require nonempty property_matchers")
		}
		for _, value := range matchers {
			var matcher, valid = value.(map[string]any)
			if !valid {
				return fmt.Errorf("property matcher must be an object")
			}
			if _, valid := matcher["key"].(string); !valid {
				return fmt.Errorf("property matcher key must be a string")
			}
			if matcher["operator"] != "equals" {
				return fmt.Errorf("property matcher operator must be equals")
			}
			if graphPrimitiveType(matcher["value"]) == "" {
				return fmt.Errorf("property matcher value must be a string, number, or boolean")
			}
		}
	default:
		return fmt.Errorf("unsupported endpoint match_by %q", strategy)
	}
	return nil
}

func validateGraphProperties(value any, node bool) error {
	var properties, valid = value.(map[string]any)
	if value == nil {
		return nil
	}
	if !valid {
		return fmt.Errorf("properties must be an object or null")
	}
	if _, exists := properties["objectid"]; node && exists {
		return fmt.Errorf("node properties cannot include objectid")
	}
	for name, value := range properties {
		if graphPrimitiveType(value) != "" {
			continue
		}
		var (
			items, valid = value.([]any)
			itemType     string
		)
		if !valid {
			return fmt.Errorf("property %q must be text, a number, true/false, or a list containing only one of those types", name)
		}
		for _, item := range items {
			var primitiveType = graphPrimitiveType(item)
			if primitiveType == "" || (itemType != "" && primitiveType != itemType) {
				return fmt.Errorf("property %q must contain only text, only numbers, or only true/false values", name)
			}
			itemType = primitiveType
		}
	}
	return nil
}

func graphPrimitiveType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return ""
	}
}
