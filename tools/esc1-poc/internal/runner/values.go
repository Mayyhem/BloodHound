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
	"fmt"
	"math"
	"sort"
	"strings"
)

type Pair struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

func readPairs(rows Rows) ([]Pair, error) {
	var (
		seen  = map[Pair]bool{}
		pairs = []Pair{}
	)
	for _, row := range rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("each relationship row must contain only source and target")
		}
		source, sourceOK := row["source"].(string)
		target, targetOK := row["target"].(string)
		if !sourceOK || !targetOK || strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" {
			return nil, fmt.Errorf("source and target must be nonempty objectid strings")
		}
		pair := Pair{Source: source, Target: target}
		if !seen[pair] {
			seen[pair] = true
			pairs = append(pairs, pair)
		}
	}
	sortPairs(pairs)
	return pairs, nil
}

func sortPairs(pairs []Pair) {
	sort.Slice(pairs, func(left, right int) bool {
		if pairs[left].Source == pairs[right].Source {
			return pairs[left].Target < pairs[right].Target
		}
		return pairs[left].Source < pairs[right].Source
	})
}

func stringsFrom(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return values, true
	case []any:
		var result = make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}

func onlyKind(value any, kind string) bool {
	var kinds, ok = stringsFrom(value)
	return ok && len(kinds) == 1 && kinds[0] == kind
}

func hasAllowedKind(value any, allowed []string) bool {
	var kinds, ok = stringsFrom(value)
	if !ok {
		return false
	}
	for _, kind := range kinds {
		for _, candidate := range allowed {
			if kind == candidate {
				return true
			}
		}
	}
	return false
}

// Query answers may contain ordinary values and nested tables. A graph node or
// path is deliberately rejected: authors must return its objectid or explicit
// properties instead. That makes the handoff between files easy to inspect.
func validateRows(rows Rows) error {
	for index, row := range rows {
		for column, value := range row {
			if err := validateValue(value); err != nil {
				return fmt.Errorf("row %d, column %s: %w", index+1, column, err)
			}
		}
	}
	return nil
}

func validateValue(value any) error {
	switch value := value.(type) {
	case nil, bool, string, int, int64:
		return nil
	case float64:
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			return nil
		}
	case []any:
		for _, item := range value {
			if err := validateValue(item); err != nil {
				return err
			}
		}
		return nil
	case []string:
		return nil
	case map[string]any:
		// Gob preserves map contents but does not distinguish a nil map
		// from an empty one. Reject that ambiguity before saving a row.
		if value == nil {
			return fmt.Errorf("a map value must not be nil; return null or an empty map explicitly")
		}
		for _, item := range value {
			if err := validateValue(item); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported answer value %T; return plain values instead of graph nodes or paths", value)
}

// Neo4j properties are simpler than query answers: a stored property cannot be
// a nested map or a list containing different kinds of values.
func validateProperty(value any) error {
	switch value := value.(type) {
	case string, bool, int, int64, float64:
		return validateValue(value)
	case []string:
		return nil
	case []any:
		var previousKind string
		for _, item := range value {
			kind := fmt.Sprintf("%T", item)
			switch item.(type) {
			case string, bool, int, int64, float64:
				if err := validateValue(item); err != nil {
					return err
				}
			default:
				return fmt.Errorf("a property list must contain only simple values")
			}
			if previousKind != "" && previousKind != kind {
				return fmt.Errorf("all values in a property list must have the same type")
			}
			previousKind = kind
		}
		return nil
	default:
		return fmt.Errorf("a property must be text, a number, true/false, or a list of those")
	}
}
