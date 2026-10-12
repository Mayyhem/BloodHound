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

// Package extension reads an experiment's schema and orders its read-only queries.
package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Schema is the small execution plan derived from the OpenGraph definition.
// It is kept in memory; extension authors maintain their schema files.
type Schema struct {
	Name            string `json:"name"`
	Owner           string `json:"owner"`
	ReferenceCommit string `json:"reference_commit"`
	// Only the ESC1 example has a built-in BloodHound answer to compare with.
	// Other extensions still use the same identity, ownership, and rollback rules.
	CompareNativeESC1 bool                  `json:"compare_native_esc1,omitempty"`
	Nodes             []NodeBinding         `json:"nodes"`
	Relationships     []RelationshipBinding `json:"relationships"`
}

type NodeBinding struct {
	Owner    string `json:"owner,omitempty"`
	Kind     string `json:"kind"`
	Query    string `json:"query"`
	IDPrefix string `json:"id_prefix"`
}

type RelationshipBinding struct {
	Owner              string             `json:"owner,omitempty"`
	ExistingEdgePolicy ExistingEdgePolicy `json:"existing_edge_policy,omitempty"`
	Kind               string             `json:"kind"`
	Query              string             `json:"query"`
	Endpoints          []Endpoint         `json:"endpoints"`
}

// EffectiveOwner preserves the owner of a binding when several extensions run together.
func (s NodeBinding) EffectiveOwner(fallback string) string {
	if s.Owner != "" {
		return s.Owner
	}
	return fallback
}

func (s RelationshipBinding) EffectiveOwner(fallback string) string {
	if s.Owner != "" {
		return s.Owner
	}
	return fallback
}

// EffectiveExistingEdgePolicy gives omitted settings their publication default.
func (s RelationshipBinding) EffectiveExistingEdgePolicy() ExistingEdgePolicy {
	if s.ExistingEdgePolicy == "" {
		return ReplaceExisting
	}
	return s.ExistingEdgePolicy
}

type Dependency struct {
	Type string
	Name string
}
type Query struct {
	// OutputKind is read from a literal final RETURN, before executing any rows.
	OutputKind      string
	Extension       string
	ExtensionName   string
	DisabledReason  string
	Overlap         *GroupOverlap
	BatchDependency string
	Reach           *Reachability
	Name            string
	Parameter       string
	Text            string
	Dependencies    []Dependency
}
type Plan struct {
	// Removed rules still need their old connections cleared on the next
	// successful run. This list comes from the saved rule history, not Cypher.
	RetiredRelationships []string          `json:"retired_relationships,omitempty"`
	RetiredOwners        map[string]string `json:"retired_owners,omitempty"`
	Definitions          []Definition
	SchemaFiles          map[string]string
	Schema               Schema
	Definition           Definition
	Queries              []Query
}

var (
	identifier          = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	publishedIdentifier = regexp.MustCompile(`^PoC_[A-Za-z_][A-Za-z0-9_]*$`)
	queryFilename       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.cypher$`)
	referenceCommit     = regexp.MustCompile(`^[A-Fa-f0-9]{40}$`)
	queryParameter      = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
)

func Load(directory string) (Plan, error) {
	var (
		plan    Plan
		err     error
		schemas []Schema
		queries = make(map[string]Query)
		entries []os.DirEntry
		ordered []Query
	)
	entries, err = os.ReadDir(directory)
	if err != nil {
		return Plan{}, err
	}
	plan.Definitions, plan.SchemaFiles, err = loadDefinitions(directory, entries)
	if err != nil {
		return Plan{}, err
	}
	for index := range plan.Definitions {
		definition := &plan.Definitions[index]
		if definition.PostProcessing == nil {
			definition.PostProcessing = &ProcessingSettings{Owner: "extension/" + definition.Schema.Name, ReferenceCommit: BuildReferenceCommit}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".cypher" {
			continue
		}
		if !queryFilename.MatchString(entry.Name()) {
			return Plan{}, fmt.Errorf("invalid query filename %q: use snake_case.cypher", entry.Name())
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return Plan{}, err
		}
		dependencies, err := parseDependencies(string(contents))
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		reach, err := parseReachability(string(contents), dependencies)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		overlap, err := parseGroupOverlap(string(contents), dependencies)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if overlap != nil && reach != nil {
			return Plan{}, fmt.Errorf("%s: group-overlap cannot also request reachability", entry.Name())
		}
		batchDependency, err := parseBatchDependency(string(contents), dependencies)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		query := Query{Name: entry.Name(), Parameter: strings.TrimSuffix(entry.Name(), ".cypher"), Text: string(contents), Dependencies: dependencies, Reach: reach, BatchDependency: batchDependency, Overlap: overlap}
		if err := checkReadOnlyText(query.Text); err != nil {
			return Plan{}, fmt.Errorf("%s: %w", query.Name, err)
		}
		output, err := parseQueryOutput(query.Text)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", query.Name, err)
		}
		if err := bindQueryOutput(plan.Definitions, &query, output); err != nil {
			return Plan{}, fmt.Errorf("%s: %w", query.Name, err)
		}
		queries[entry.Name()] = query
	}
	plan.Definition = plan.Definitions[0]
	for _, definition := range plan.Definitions {
		// The shipped experiment's native-agreement gate is platform policy.
		// Other extensions do not need to describe ESC1 or its reference build.
		if !definition.LegacyProcessing {
			for _, kind := range definition.RelationshipKinds {
				if kind.Name == "PoC_ADCSESC1" && kind.PostProcessing != nil {
					definition.PostProcessing.CompareNativeESC1 = true
				}
			}
		}
		schema, err := definition.processingSchema()
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", plan.SchemaFiles[definition.Schema.Name], err)
		}
		schemas = append(schemas, schema)
		if len(schemas) == 1 {
			plan.Schema = schema
		} else {
			plan.Schema.Name = "registered_extensions"
			plan.Schema.Owner = "rules-service/v1"
			plan.Schema.CompareNativeESC1 = plan.Schema.CompareNativeESC1 || schema.CompareNativeESC1
			plan.Schema.Nodes = append(plan.Schema.Nodes, schema.Nodes...)
			plan.Schema.Relationships = append(plan.Schema.Relationships, schema.Relationships...)
		}
	}
	for _, query := range queries {
		if query.Overlap == nil && query.BatchDependency == "" {
			continue
		}
		if query.Reach != nil {
			return Plan{}, fmt.Errorf("%s: reachability seeds cannot also batch their prerequisites", query.Name)
		}
		var publishesRelationships bool
		for _, binding := range plan.Schema.Relationships {
			if binding.Query == query.Name {
				publishesRelationships = true
			}
		}
		if !publishesRelationships {
			return Plan{}, fmt.Errorf("%s: group-overlap and batch-on are allowed only for relationship producers", query.Name)
		}
	}
	for index, schema := range schemas {
		if err := validateSchema(schema, queries); err != nil {
			return Plan{}, fmt.Errorf("%s: %w", plan.SchemaFiles[plan.Definitions[index].Schema.Name], err)
		}
	}
	if err := validateSchema(plan.Schema, queries); err != nil {
		return Plan{}, err
	}
	ordered, err = orderQueries(queries, plan.Schema)
	if err != nil {
		return Plan{}, err
	}
	plan.Queries = attributeQueries(ordered, plan.Definitions)
	return plan, nil
}

// QueryDependencies reads the author's input declarations without executing
// the query. The rules page uses the same reader when removing dependent rules.
func QueryDependencies(contents string) ([]Dependency, error) {
	return parseDependencies(contents)
}

func parseDependencies(contents string) ([]Dependency, error) {
	var (
		dependencies []Dependency
		seen         = make(map[string]bool)
		declared     bool
		continuation bool
		none         bool
	)
	for _, line := range strings.Split(contents, "\n") {
		comment := strings.TrimSpace(line)
		if !strings.HasPrefix(comment, "//") {
			continuation = false
			continue
		}
		comment = strings.TrimSpace(strings.TrimPrefix(comment, "//"))
		if strings.HasPrefix(comment, "depends-on:") {
			declared = true
			continuation = true
			comment = strings.TrimSpace(strings.TrimPrefix(comment, "depends-on:"))
		} else if !continuation || !dependencyContinuation(comment) {
			continuation = false
			continue
		}
		// Empty declaration lines allow the list to start on the next comment line.
		if comment == "" {
			continue
		}
		if comment == "none" {
			none = true
			continue
		}
		for _, item := range strings.Split(comment, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			} // A trailing comma can continue onto the next line.
			parts := strings.Split(item, ":")
			if len(parts) != 2 {
				return nil, fmt.Errorf("malformed dependency %q", item)
			}
			dependency := Dependency{Type: strings.TrimSpace(parts[0]), Name: strings.TrimSpace(parts[1])}
			switch dependency.Type {
			case "node", "relationship":
				if !identifier.MatchString(dependency.Name) {
					return nil, fmt.Errorf("invalid dependency kind %q", dependency.Name)
				}
			case "query":
				if !queryFilename.MatchString(dependency.Name) {
					return nil, fmt.Errorf("invalid prerequisite filename %q", dependency.Name)
				}
			default:
				return nil, fmt.Errorf("unknown dependency type %q", dependency.Type)
			}
			key := dependency.Type + ":" + dependency.Name
			if seen[key] {
				return nil, fmt.Errorf("duplicate dependency %q", key)
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

// Ordinary explanatory comments can appear between dependency declarations.
func dependencyContinuation(comment string) bool {
	for _, prefix := range []string{"node:", "relationship:", "query:", "node ", "relationship ", "query ", ","} {
		if strings.HasPrefix(comment, prefix) {
			return true
		}
	}
	return comment == "node" || comment == "relationship" || comment == "query" || comment == "none"
}

func validateSchema(schema Schema, queries map[string]Query) error {
	var (
		kinds               = make(map[string]bool)
		bindings            = make(map[string]bool)
		prefixes            = make(map[string]bool)
		hasESC1Relationship bool
	)
	if strings.TrimSpace(schema.Name) == "" || strings.TrimSpace(schema.Owner) == "" {
		return fmt.Errorf("schema name and owner must be nonblank")
	}
	if !referenceCommit.MatchString(schema.ReferenceCommit) {
		return fmt.Errorf("reference_commit must contain 40 hexadecimal characters")
	}
	bind := func(kind, filename string) error {
		if !publishedIdentifier.MatchString(kind) {
			return fmt.Errorf("published kind %q must start with PoC_ and be an identifier", kind)
		}
		if kinds[kind] {
			return fmt.Errorf("duplicate published kind %q", kind)
		}
		if bindings[filename] {
			return fmt.Errorf("query %q has multiple output bindings", filename)
		}
		if _, exists := queries[filename]; !exists {
			return fmt.Errorf("binding for %q refers to missing query %q", kind, filename)
		}
		kinds[kind] = true
		bindings[filename] = true
		return nil
	}
	for _, binding := range schema.Nodes {
		if err := bind(binding.Kind, binding.Query); err != nil {
			return err
		}
		if !publishedIdentifier.MatchString(binding.IDPrefix) {
			return fmt.Errorf("invalid helper ID prefix %q", binding.IDPrefix)
		}
		if prefixes[binding.IDPrefix] {
			return fmt.Errorf("duplicate helper ID prefix %q", binding.IDPrefix)
		}
		prefixes[binding.IDPrefix] = true
	}
	for _, binding := range schema.Relationships {
		if !binding.EffectiveExistingEdgePolicy().valid() {
			return fmt.Errorf("relationship %q has an invalid existing_edge_policy", binding.Kind)
		}
		if binding.Kind == "PoC_ADCSESC1" {
			hasESC1Relationship = true
		}
		if err := bind(binding.Kind, binding.Query); err != nil {
			return err
		}
		if len(binding.Endpoints) == 0 {
			return fmt.Errorf("relationship %q needs endpoints", binding.Kind)
		}

	}
	if schema.CompareNativeESC1 && !hasESC1Relationship {
		return fmt.Errorf("compare_native_esc1 requires a PoC_ADCSESC1 relationship producer")
	}
	return nil
}

func orderQueries(queries map[string]Query, schema Schema) ([]Query, error) {
	var (
		names   []string
		ordered []Query
		state   = make(map[string]int)
		used    = make(map[string]bool)
	)
	for name, query := range queries {
		names = append(names, name)
		if err := checkReadOnlyText(query.Text); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		declaredParameters := make(map[string]bool)
		for _, dependency := range query.Dependencies {
			if dependency.Type != "query" {
				continue
			}
			if _, exists := queries[dependency.Name]; !exists {
				return nil, fmt.Errorf("%s: missing prerequisite %s", name, dependency.Name)
			}
			used[dependency.Name] = true
			declaredParameters[strings.TrimSuffix(dependency.Name, ".cypher")] = true
		}
		for _, match := range queryParameter.FindAllStringSubmatch(parameterText(query.Text), -1) {
			if !declaredParameters[match[1]] {
				return nil, fmt.Errorf("%s: parameter $%s needs a declared query prerequisite", name, match[1])
			}
		}
	}
	for _, binding := range schema.Nodes {
		used[binding.Query] = true
	}
	for _, binding := range schema.Relationships {
		used[binding.Query] = true
	}
	sort.Strings(names)
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("query dependency cycle at %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		var prerequisites []string
		for _, dependency := range queries[name].Dependencies {
			if dependency.Type == "query" {
				prerequisites = append(prerequisites, dependency.Name)
			}
		}
		sort.Strings(prerequisites)
		for _, prerequisite := range prerequisites {
			if err := visit(prerequisite); err != nil {
				return err
			}
		}
		state[name] = 2
		ordered = append(ordered, queries[name])
		return nil
	}
	for _, name := range names {
		if !used[name] {
			return nil, fmt.Errorf("unused query %s has no output binding or dependent", name)
		}
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// These files ask questions about this graph only. External procedures and
// LOAD CSV can contact other services even when a query is described as a read.
// We exclude them. Neo4j also checks the query's actual read/write classification
// before execution; this small text check does not replace that database check.
func checkReadOnlyText(contents string) error {
	var (
		text      = strings.ToUpper(parameterText(contents))
		forbidden = regexp.MustCompile(`\b(CREATE|MERGE|DELETE|DETACH|SET|REMOVE|DROP|FOREACH|LOAD|GRANT|DENY|REVOKE|TERMINATE|USE|EXPLAIN|PROFILE)\b`)
		calls     = regexp.MustCompile(`\bCALL\s*`)
	)
	if word := forbidden.FindString(text); word != "" {
		return fmt.Errorf("analysis files cannot use %s; only the runner may write graph data", word)
	}
	for _, location := range calls.FindAllStringIndex(text, -1) {
		if location[1] == len(text) || text[location[1]] != '{' {
			return fmt.Errorf("analysis files cannot call procedures; a read-only CALL { ... } subquery is allowed")
		}
	}
	return nil
}

// Affected includes downstream consumers when evidence changes, including removals.
// Prerequisites also run because intermediate rows are not cached between runs.
// Without complete change coverage, every query must run to avoid stale results.
func (s Plan) Affected(changedNodes, changedRelationships []string, coverageKnown bool) []string {
	var (
		affected      = make(map[string]bool)
		nodes         = make(map[string]bool)
		relationships = make(map[string]bool)
		result        []string
	)
	for _, kind := range changedNodes {
		nodes[kind] = true
	}
	for _, kind := range changedRelationships {
		relationships[kind] = true
	}
	for _, binding := range s.Schema.Relationships {
		for _, endpoint := range binding.Endpoints {
			if nodes[endpoint.SourceKind] || nodes[endpoint.TargetKind] {
				affected[binding.Query] = true
			}
		}
	}
	for _, binding := range s.Schema.Nodes {
		if nodes[binding.Kind] {
			affected[binding.Query] = true
		}
	}
	for _, binding := range s.Schema.Relationships {
		if relationships[binding.Kind] {
			affected[binding.Query] = true
		}
		// Comparing ESC1 is itself a dependency: a changed native answer must
		// be checked even if none of this rule's collected inputs changed.
		if s.Schema.CompareNativeESC1 && binding.Kind == "PoC_ADCSESC1" && relationships["ADCSESC1"] {
			affected[binding.Query] = true
		}
	}
	for _, query := range s.Queries {
		if !coverageKnown {
			affected[query.Name] = true
		}
		for _, dependency := range query.Dependencies {
			if dependency.Type == "node" && nodes[dependency.Name] || dependency.Type == "relationship" && relationships[dependency.Name] || dependency.Type == "query" && affected[dependency.Name] {
				affected[query.Name] = true
			}
		}
	}
	// Walk backwards so a prerequisite's own prerequisites are also selected.
	for index := len(s.Queries) - 1; index >= 0; index-- {
		query := s.Queries[index]
		if !affected[query.Name] {
			continue
		}
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				affected[dependency.Name] = true
			}
		}
	}
	for _, query := range s.Queries {
		if affected[query.Name] {
			result = append(result, query.Name)
		}
	}
	return result
}

// Only executable query text can reference a prerequisite parameter. Ignore
// strings, escaped identifiers, and comments so examples cannot affect validation.
func parameterText(contents string) string {
	var (
		result       strings.Builder
		quote        byte
		lineComment  bool
		blockComment bool
	)
	for index := 0; index < len(contents); index++ {
		character := contents[index]
		if lineComment {
			if character == '\n' {
				lineComment = false
				result.WriteByte('\n')
			}
			continue
		}
		if blockComment {
			if character == '*' && index+1 < len(contents) && contents[index+1] == '/' {
				blockComment = false
				index++
			}
			continue
		}
		if quote != 0 {
			if character == '\\' {
				index++
				continue
			}
			if character == quote {
				if index+1 < len(contents) && contents[index+1] == quote {
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if character == '/' && index+1 < len(contents) {
			if contents[index+1] == '/' {
				lineComment = true
				index++
				result.WriteByte(' ')
				continue
			}
			if contents[index+1] == '*' {
				blockComment = true
				index++
				result.WriteByte(' ')
				continue
			}
		}
		if character == '\'' || character == '"' || character == '`' {
			quote = character
			result.WriteByte(' ')
			continue
		}
		result.WriteByte(character)
	}
	return result.String()
}
