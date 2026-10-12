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
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// checkedRelationship accepts the same small property vocabulary on both
// databases. Validation also runs for keep-existing: a bad answer must never
// become permission to remove other, previously published connections.
func checkedRelationship(row map[string]any) (map[string]any, error) {
	var properties = map[string]any{}
	if len(row) != 2 && len(row) != 3 {
		return nil, fmt.Errorf("each relationship row must contain source, target, and optionally properties")
	}
	if _, err := readPairs(Rows{{"source": row["source"], "target": row["target"]}}); err != nil {
		return nil, err
	}
	if raw, supplied := row["properties"]; supplied {
		values, ok := raw.(map[string]any)
		if !ok || values == nil {
			return nil, fmt.Errorf("relationship properties must be a map, not null")
		}
		for key, value := range values {
			if strings.TrimSpace(key) == "" || key == "poc_owner" {
				return nil, fmt.Errorf("relationship properties cannot use an empty name or the reserved poc_owner name")
			}
			if value == nil {
				// For merging, null explicitly removes a key. For a new or
				// replacement connection it simply leaves that key absent.
				properties[key] = nil
				continue
			}
			normalized, err := normalizeRelationshipProperty(value)
			if err != nil {
				return nil, fmt.Errorf("relationship property %q: %w", key, err)
			}
			properties[key] = normalized
		}
	} else if len(row) != 2 {
		return nil, fmt.Errorf("each relationship row must contain source, target, and optionally properties")
	}
	return map[string]any{"source": row["source"], "target": row["target"], "properties": properties}, nil
}

func normalizeRelationshipProperty(value any) (any, error) {
	var items []any
	switch value := value.(type) {
	case []string:
		items = make([]any, len(value))
		for index, item := range value {
			items[index] = item
		}
	case []any:
		items = value
	default:
		return normalizeRelationshipScalar(value)
	}
	var result = make([]any, len(items))
	var previousType string
	for index, item := range items {
		normalized, err := normalizeRelationshipScalar(item)
		if err != nil {
			return nil, fmt.Errorf("property lists require simple values without nulls: %w", err)
		}
		valueType := fmt.Sprintf("%T", normalized)
		if previousType != "" && valueType != previousType {
			return nil, fmt.Errorf("all values in a property list must have the same type")
		}
		previousType = valueType
		result[index] = normalized
	}
	return result, nil
}

func normalizeRelationshipScalar(value any) (any, error) {
	switch value := value.(type) {
	case string, bool, int64:
		return value, nil
	case int:
		return int64(value), nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("numbers must be finite")
		}
		if math.Trunc(value) == value {
			// PostgreSQL may return 2.0 as the integer 2. Normalize whole
			// numbers once, without converting exact integers into floats.
			if value < math.MinInt64 || value >= -float64(math.MinInt64) {
				return nil, fmt.Errorf("whole numbers must fit in a signed 64-bit integer")
			}
			return int64(value), nil
		}
		return value, nil
	default:
		return nil, fmt.Errorf("a property must be text, a number, true/false, or a list of those")
	}
}

// JSON gives simple values a stable comparison across database drivers: map
// order and int-versus-int64 do not matter, while large integers stay exact.
// Unsupported existing values simply compare unequal and can be overwritten.
func samePropertyValue(left, right any) bool {
	var leftJSON, leftError = json.Marshal(left)
	var rightJSON, rightError = json.Marshal(right)
	return leftError == nil && rightError == nil && bytes.Equal(leftJSON, rightJSON)
}

// Keep the requested patch small. Do not copy the whole existing property map
// into the update, or erase properties that the query did not mention.
func changedProperties(existing, desired map[string]any) map[string]any {
	var patch = map[string]any{}
	for key, value := range desired {
		old, present := existing[key]
		if value == nil {
			if present {
				patch[key] = nil
			}
		} else if !present || !samePropertyValue(old, value) {
			patch[key] = value
		}
	}
	return patch
}

func creationProperties(desired map[string]any) map[string]any {
	var properties = map[string]any{}
	for key, value := range desired {
		if value != nil {
			properties[key] = value
		}
	}
	return properties
}
