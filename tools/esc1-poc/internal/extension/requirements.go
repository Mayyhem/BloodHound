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
	"fmt"
	"path"
	"strings"
)

// AnswerParameter returns the name used inside the consuming Cypher file.
// Plans constructed directly in tests can omit the alias just as authors can.
func (s Dependency) AnswerParameter() string {
	if s.Parameter != "" {
		return s.Parameter
	}
	return strings.TrimSuffix(path.Base(s.Name), ".cypher")
}

func parseQueryRequirement(text, namespace string) (Dependency, error) {
	var (
		result = Dependency{Type: "query"}
		name   string
	)
	if before, after, found := strings.Cut(text, " as "); found {
		text, result.Parameter = strings.TrimSpace(before), strings.TrimSpace(after)
		if !identifier.MatchString(result.Parameter) {
			return Dependency{}, fmt.Errorf("a query alias must be a valid Cypher parameter name")
		}
	}
	if position := strings.IndexAny(text, "<>=!~"); position >= 0 {
		name, result.Conditions = strings.TrimSpace(text[:position]), strings.TrimSpace(text[position:])
	} else {
		name = strings.TrimSpace(text)
	}
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		parts = []string{namespace, parts[0]}
	}
	if len(parts) != 2 || !identifier.MatchString(parts[0]) || !queryFilename.MatchString(parts[1]+".cypher") {
		return Dependency{}, fmt.Errorf("invalid query reference; use query:users or query:PoC_AD/users, without .cypher")
	}
	if _, err := parseVersionConditions(result.Conditions); err != nil {
		return Dependency{}, err
	}
	result.Name = parts[0] + "/" + parts[1] + ".cypher"
	if result.Parameter == "" {
		result.Parameter = parts[1]
	}
	return result, nil
}

func parseDependencies(contents string) ([]Dependency, error) {
	var (
		dependencies []Dependency
		seen         = map[string]bool{}
		parameters   = map[string]bool{}
		declared     bool
		continuation bool
		none         bool
	)
	metadata, err := ReadQueryMetadata(contents)
	if err != nil {
		return nil, err
	}
	for _, comment := range leadingComments(contents) {
		if strings.HasPrefix(comment, "depends-on:") {
			declared, continuation = true, true
			comment = strings.TrimSpace(strings.TrimPrefix(comment, "depends-on:"))
		} else if !continuation || !dependencyContinuation(comment) {
			continuation = false
			continue
		}
		if comment == "" {
			continue
		}
		if comment == "none" {
			none = true
			continue
		}
		var lineDependencies []Dependency
		if strings.HasPrefix(comment, "query:") {
			dependency, err := parseQueryRequirement(strings.TrimSpace(strings.TrimPrefix(comment, "query:")), metadata.Namespace)
			if err != nil {
				return nil, fmt.Errorf("depends-on: %w; put each query requirement on its own line, separate from graph inputs", err)
			}
			lineDependencies = append(lineDependencies, dependency)
		} else {
			for _, item := range strings.Split(comment, ",") {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				kindType, name, found := strings.Cut(item, ":")
				if !found {
					return nil, fmt.Errorf("malformed dependency; use node:User, relationship:MemberOf, or a separate query requirement")
				}
				kindType, name = strings.TrimSpace(kindType), strings.TrimSpace(name)
				if kindType == "query" {
					return nil, fmt.Errorf("put each query requirement on its own depends-on line, separate from graph inputs")
				}
				if kindType != "node" && kindType != "relationship" {
					return nil, fmt.Errorf("unknown dependency type; use node, relationship, or query")
				}
				if !identifier.MatchString(name) {
					return nil, fmt.Errorf("invalid dependency kind")
				}
				lineDependencies = append(lineDependencies, Dependency{Type: kindType, Name: name})
			}
		}
		for _, dependency := range lineDependencies {
			key := dependency.Type + ":" + dependency.Name
			if seen[key] {
				return nil, fmt.Errorf("duplicate dependency %s", key)
			}
			if dependency.Type == "query" {
				if parameters[dependency.Parameter] {
					return nil, fmt.Errorf("duplicate parameter $%s; give the queries distinct aliases using as", dependency.Parameter)
				}
				parameters[dependency.Parameter] = true
			}
			seen[key] = true
			dependencies = append(dependencies, dependency)
		}
	}
	if !declared {
		return nil, fmt.Errorf("missing // depends-on declaration")
	}
	if none && len(dependencies) != 0 {
		return nil, fmt.Errorf("depends-on: none cannot be combined with dependencies")
	}
	if !none && len(dependencies) == 0 {
		return nil, fmt.Errorf("empty dependency declaration; use none for constants")
	}
	return dependencies, nil
}
