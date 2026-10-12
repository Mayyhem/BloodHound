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
	"sort"

	"github.com/specterops/bloodhound/packages/go/graphschema"
)

var schemaFilename = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.schema\.json$`)

// IsSchemaFile recognizes the standalone definition and named definitions in a registry.
func IsSchemaFile(name string) bool {
	return name == "schema.json" || schemaFilename.MatchString(name)
}

// LoadDefinitions reads the whole model without requiring query files or a
// database. A referenced custom kind must come from a schema in this directory.
func LoadDefinitions(directory string) ([]Definition, error) {
	var entries, err = os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	definitions, _, err := loadDefinitions(directory, entries)
	return definitions, err
}

// NativeNodeKinds uses the same list as BloodHound's graph setup, including AD,
// Azure, and common kinds. The offline viewer embeds this list when it is built.
func NativeNodeKinds() []string {
	var names []string
	for _, kind := range graphschema.CombinedGraphSchema("").Nodes {
		names = append(names, kind.String())
	}
	sort.Strings(names)
	return names
}

// ValidateDefinitions checks the complete set, so a reference can appear before
// its defining schema. A relationship name never qualifies as a node kind.
func ValidateDefinitions(definitions []Definition) error {
	var (
		names      = make(map[string]bool)
		namespaces = make(map[string]bool)
		kinds      = make(map[string]bool)
		nodeKinds  = make(map[string]bool)
	)
	if len(definitions) == 0 {
		return fmt.Errorf("include at least one schema definition")
	}
	for _, name := range NativeNodeKinds() {
		nodeKinds[name] = true
	}
	for _, definition := range definitions {
		if err := definition.validate(); err != nil {
			return fmt.Errorf("schema %q: %w", definition.Schema.Name, err)
		}
		if names[definition.Schema.Name] {
			return fmt.Errorf("duplicate schema name %q", definition.Schema.Name)
		}
		if namespaces[definition.Schema.Namespace] {
			return fmt.Errorf("duplicate schema namespace %q", definition.Schema.Namespace)
		}
		names[definition.Schema.Name] = true
		namespaces[definition.Schema.Namespace] = true
		for _, kind := range definition.NodeKinds {
			if kinds[kind.Name] || nodeKinds[kind.Name] {
				return fmt.Errorf("duplicate declared kind %q", kind.Name)
			}
			kinds[kind.Name], nodeKinds[kind.Name] = true, true
		}
		for _, kind := range definition.RelationshipKinds {
			if kinds[kind.Name] || nodeKinds[kind.Name] {
				return fmt.Errorf("duplicate declared kind %q", kind.Name)
			}
			kinds[kind.Name] = true
		}
	}
	for _, definition := range definitions {
		for _, relationship := range definition.RelationshipKinds {
			for _, pair := range relationship.CompatibleKinds {
				for _, name := range []string{pair.SourceKind, pair.TargetKind} {
					if !nodeKinds[name] {
						return fmt.Errorf("schema %q relationship %q: compatible_kinds references unknown node kind %q; include the schema that defines it in node_kinds, or correct the kind name", definition.Schema.Name, relationship.Name, name)
					}
				}
			}
		}
	}
	return nil
}

// A graph input may come from collected data or another calculation, but its
// kind still needs a definition. Read only the author's existing depends-on
// declarations here; discovering undeclared inputs inside Cypher is separate work.
func validateInputKinds(queries []Query, definitions []Definition) error {
	var (
		known  = make(map[Dependency]bool)
		native = graphschema.CombinedGraphSchema("")
	)
	for _, kind := range native.Nodes {
		known[Dependency{Type: "node", Name: kind.String()}] = true
	}
	for _, kind := range native.Edges {
		known[Dependency{Type: "relationship", Name: kind.String()}] = true
	}
	for _, definition := range definitions {
		for _, kind := range definition.NodeKinds {
			known[Dependency{Type: "node", Name: kind.Name}] = true
		}
		for _, kind := range definition.RelationshipKinds {
			known[Dependency{Type: "relationship", Name: kind.Name}] = true
		}
	}
	for _, query := range queries {
		for _, input := range query.Dependencies {
			if input.Type != "query" && !known[input] {
				return fmt.Errorf("%s: line %d: depends-on references unknown %s kind %q; include the schema that defines it, or correct the kind name", query.Name, textLine(query.Text, input.Name), input.Type, input.Name)
			}
		}
	}
	return nil
}

// os.ReadDir returns filename order, making registration and the combined plan stable.
func loadDefinitions(directory string, entries []os.DirEntry) ([]Definition, map[string]string, error) {
	var (
		definitions []Definition
		files       = make(map[string]string)
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
		files[definition.Schema.Name] = entry.Name()
		definitions = append(definitions, definition)
	}
	if len(definitions) == 0 {
		return nil, nil, fmt.Errorf("no schema.json or *.schema.json definitions found")
	}
	if err := ValidateDefinitions(definitions); err != nil {
		return nil, nil, err
	}
	return definitions, files, nil
}

// Output bindings identify their extension. Intermediate calculations inherit
// all extensions that consume them, so shared work can be displayed together.
func attributeQueries(queries []Query, definitions []Definition) []Query {
	var (
		outputExtensions = make(map[string]string)
		displayNames     = make(map[string]string)
		consumers        = make(map[string]map[string]bool)
	)
	for _, definition := range definitions {
		displayNames[definition.Schema.Name] = definition.Schema.DisplayName
		for _, kind := range definition.NodeKinds {
			if kind.PostProcessing != nil {
				outputExtensions[kind.PostProcessing.Query] = definition.Schema.Name
			}
		}
		for _, kind := range definition.RelationshipKinds {
			for _, query := range kind.producingQueries() {
				outputExtensions[query] = definition.Schema.Name
			}
		}
	}
	for _, query := range queries {
		consumers[query.Name] = make(map[string]bool)
		if extension := outputExtensions[query.Name]; extension != "" {
			consumers[query.Name][extension] = true
		}
	}
	for index := len(queries) - 1; index >= 0; index-- {
		query := queries[index]
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				for extension := range consumers[query.Name] {
					consumers[dependency.Name][extension] = true
				}
			}
		}
	}
	for index := range queries {
		query := &queries[index]
		if extension := outputExtensions[query.Name]; extension != "" {
			query.Extension, query.ExtensionName = extension, displayNames[extension]
		} else if len(consumers[query.Name]) == 1 {
			for extension := range consumers[query.Name] {
				query.Extension, query.ExtensionName = extension, displayNames[extension]
			}
		} else {
			query.Extension, query.ExtensionName = "shared", "Shared calculations"
		}
	}
	return queries
}
