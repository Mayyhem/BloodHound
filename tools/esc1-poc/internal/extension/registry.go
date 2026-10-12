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
	"os"
	"path/filepath"
	"regexp"
)

var schemaFilename = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.schema\.json$`)

// IsSchemaFile recognizes the standalone definition and named definitions in a registry.
func IsSchemaFile(name string) bool {
	return name == "schema.json" || schemaFilename.MatchString(name)
}

// os.ReadDir returns filename order, making registration and the combined plan stable.
func loadDefinitions(directory string, entries []os.DirEntry) ([]Definition, map[string]string, error) {
	var (
		definitions []Definition
		files       = make(map[string]string)
		namespaces  = make(map[string]string)
		kinds       = make(map[string]string)
	)
	for _, entry := range entries {
		if entry.IsDir() || !IsSchemaFile(entry.Name()) {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, nil, err
		}
		definition, err := ParseDefinition(contents)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if previous, exists := files[definition.Schema.Name]; exists {
			return nil, nil, fmt.Errorf("duplicate schema name %q in %s and %s", definition.Schema.Name, previous, entry.Name())
		}
		if previous, exists := namespaces[definition.Schema.Namespace]; exists {
			return nil, nil, fmt.Errorf("duplicate schema namespace %q in %s and %s", definition.Schema.Namespace, previous, entry.Name())
		}
		var localKinds []string
		for _, kind := range definition.NodeKinds {
			localKinds = append(localKinds, kind.Name)
		}
		for _, kind := range definition.RelationshipKinds {
			localKinds = append(localKinds, kind.Name)
		}
		for _, kind := range localKinds {
			if previous, exists := kinds[kind]; exists && previous != entry.Name() {
				return nil, nil, fmt.Errorf("duplicate declared kind %q in %s and %s", kind, previous, entry.Name())
			}
			kinds[kind] = entry.Name()
		}
		files[definition.Schema.Name] = entry.Name()
		namespaces[definition.Schema.Namespace] = entry.Name()
		definitions = append(definitions, definition)
	}
	if len(definitions) == 0 {
		return nil, nil, fmt.Errorf("no schema.json or *.schema.json definitions found")
	}
	return definitions, files, nil
}

// Output bindings identify their extension. Intermediate calculations inherit
// all extensions that consume them, so shared work does not get an arbitrary owner.
func attributeQueries(queries []Query, definitions []Definition) []Query {
	var (
		outputOwners = make(map[string]string)
		displayNames = make(map[string]string)
		consumers    = make(map[string]map[string]bool)
	)
	for _, definition := range definitions {
		displayNames[definition.Schema.Name] = definition.Schema.DisplayName
		for _, kind := range definition.NodeKinds {
			if kind.PostProcessing != nil {
				outputOwners[kind.PostProcessing.Query] = definition.Schema.Name
			}
		}
		for _, kind := range definition.RelationshipKinds {
			if kind.PostProcessing != nil {
				outputOwners[kind.PostProcessing.Query] = definition.Schema.Name
			}
		}
	}
	for _, query := range queries {
		consumers[query.Name] = make(map[string]bool)
		if owner := outputOwners[query.Name]; owner != "" {
			consumers[query.Name][owner] = true
		}
	}
	for index := len(queries) - 1; index >= 0; index-- {
		query := queries[index]
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				for owner := range consumers[query.Name] {
					consumers[dependency.Name][owner] = true
				}
			}
		}
	}
	for index := range queries {
		query := &queries[index]
		if owner := outputOwners[query.Name]; owner != "" {
			query.Extension, query.ExtensionName = owner, displayNames[owner]
		} else if len(consumers[query.Name]) == 1 {
			for owner := range consumers[query.Name] {
				query.Extension, query.ExtensionName = owner, displayNames[owner]
			}
		} else {
			query.Extension, query.ExtensionName = "shared", "Shared calculations"
		}
	}
	return queries
}
