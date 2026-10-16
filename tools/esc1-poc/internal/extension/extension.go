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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/querycheck"
)

// Schema is the small execution plan derived from the OpenGraph definition.
// It is kept in memory; extension authors maintain their schema files.
type Schema struct {
	Name            string `json:"name"`
	ReferenceCommit string `json:"reference_commit"`
	// Only the ESC1 example has a built-in BloodHound answer to compare with.
	// Other extensions still use the same identity and rollback rules.
	CompareNativeESC1 bool                  `json:"compare_native_esc1,omitempty"`
	Nodes             []NodeBinding         `json:"nodes"`
	Relationships     []RelationshipBinding `json:"relationships"`
}

type NodeBinding struct {
	Kind     string `json:"kind"`
	Query    string `json:"query"`
	IDPrefix string `json:"id_prefix"`
}

type RelationshipBinding struct {
	ExistingEdgePolicy ExistingEdgePolicy `json:"existing_edge_policy,omitempty"`
	Kind               string             `json:"kind"`
	Query              string             `json:"query"`
	Endpoints          []Endpoint         `json:"endpoints"`
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
	// Query names include their namespace. Parameter is the local name used
	// by this consumer; Conditions constrain the producer's own version.
	Parameter  string
	Conditions string
}
type Query struct {
	QueryMetadata
	Columns       []string
	ColumnsKnown  bool
	SharedFields  []querycheck.SharedField
	SchemaPaused  bool
	SchemaVersion string
	// BroadRead prevents unsafe skipping when graph inputs cannot be narrowed to named kinds.
	BroadRead bool

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
	CleanupPending bool `json:"cleanup_pending,omitempty"`
	SchemaRevision string
	// EditorTarget is calculated for a preview only. The editor execution path
	// must inspect its answer without publishing its node or relationship output.
	// Ordinary loaded plans leave this blank.
	EditorTarget string `json:"editor_target,omitempty"`
	// Removed rules still need their old connections cleared on the next
	// successful run. This list comes from the saved rule history, not Cypher.
	RetiredRelationships []string              `json:"retired_relationships,omitempty"`
	RetiredEndpoints     map[string][]Endpoint `json:"retired_endpoints,omitempty"`
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

// Load is the offline entry point. Schema files here are local test inputs;
// the rules page uses LoadFiles with definitions read from BloodHound.
func Load(directory string) (Plan, error) {
	var files = map[string]string{}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Plan{}, err
	}
	definitions, schemaFiles, err := loadDefinitions(directory, entries)
	if err != nil {
		return Plan{}, err
	}
	err = filepath.WalkDir(directory, func(filename string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() || filepath.Ext(filename) != ".cypher" {
			return nil
		}
		name, err := filepath.Rel(directory, filename)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)] = string(contents)
		return nil
	})
	if err != nil {
		return Plan{}, err
	}
	plan, err := LoadFiles(definitions, files)
	plan.SchemaFiles = schemaFiles
	return plan, err
}

// LoadFiles keeps installed definitions separate from uploaded analyses. Work
// on a private copy because discovering outputs adds temporary run settings.
func LoadFiles(definitions []Definition, files map[string]string) (Plan, error) {
	return LoadFilesWithDisabled(definitions, files, nil)
}

// Disabled consumers can stay installed while a reviewed provider update no
// longer satisfies their version condition. They cannot execute until fixed.
func LoadFilesWithDisabled(definitions []Definition, files map[string]string, disabled []string) (Plan, error) {
	return loadFiles(definitions, files, disabled, "", true)
}

// LoadEditorFilesWithDisabled prepares one existing or new file for an editor
// test. The target is treated as enabled even if its saved counterpart is
// disabled, so an author can repair and test it. Other disabled files retain
// their state. Its literal output kind may be absent from the installed schema
// so an author can test a proposed kind before declaring it. That exception is
// limited to this one target: every prerequisite still receives the ordinary
// validation checks.
//
// The returned target keeps OutputKind for result preview, but the kind is not
// added to Schema. A caller must never use this editor plan to publish changes.
func LoadEditorFilesWithDisabled(definitions []Definition, files map[string]string, disabled []string, target string) (Plan, error) {
	if target == "" {
		return Plan{}, fmt.Errorf("choose an analysis file to test")
	}
	var editorDisabled []string
	for _, disabledName := range disabled {
		if disabledName != target {
			editorDisabled = append(editorDisabled, disabledName)
		}
	}
	// Ordinary loading relaxes a few checks for paused saved rules so they stay
	// visible. Editor plans keep their paused state, but still require a complete
	// valid prerequisite chain, including version and column checks.
	return loadFiles(definitions, files, editorDisabled, target, false)
}

func loadFiles(definitions []Definition, files map[string]string, disabled []string, editorTarget string, relaxDisabledChecks bool) (Plan, error) {
	var (
		plan        = Plan{EditorTarget: editorTarget}
		schemas     []Schema
		queries     = map[string]Query{}
		filenames   []string
		targetFound bool
	)
	if err := ValidateDefinitions(definitions); err != nil {
		return Plan{}, err
	}
	contents, err := json.Marshal(definitions)
	if err != nil {
		return Plan{}, err
	}
	if err := json.Unmarshal(contents, &plan.Definitions); err != nil {
		return Plan{}, err
	}
	plan.SchemaFiles = map[string]string{}
	for index := range plan.Definitions {
		definition := &plan.Definitions[index]
		definition.LegacyProcessing = definitions[index].LegacyProcessing
		plan.SchemaFiles[definition.Schema.Name] = definition.Schema.Name
		// Legacy offline fixtures name their bindings locally. Resolve them
		// once so even those tests exercise the same full query identities.
		for index := range definition.NodeKinds {
			if binding := definition.NodeKinds[index].PostProcessing; binding != nil && !strings.Contains(binding.Query, "/") {
				binding.Query = definition.Schema.Namespace + "/" + binding.Query
			}
		}
		for index := range definition.RelationshipKinds {
			if binding := definition.RelationshipKinds[index].PostProcessing; binding != nil && !strings.Contains(binding.Query, "/") {
				binding.Query = definition.Schema.Namespace + "/" + binding.Query
			}
		}
	}
	for index := range plan.Definitions {
		definition := &plan.Definitions[index]
		if definition.PostProcessing == nil {
			definition.PostProcessing = &ProcessingSettings{ReferenceCommit: BuildReferenceCommit}
		}
	}
	for filename := range files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	for _, filename := range filenames {
		contents := files[filename]
		name, err := QueryFileKey(filename, contents)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", filename, err)
		}
		if _, exists := queries[name]; exists {
			return Plan{}, fmt.Errorf("duplicate query %s", name)
		}
		if name == editorTarget {
			targetFound = true
		}
		metadata, err := ReadQueryMetadata(contents)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", name, err)
		}
		var owner *Definition
		for index := range plan.Definitions {
			if plan.Definitions[index].Schema.Namespace == metadata.Namespace {
				owner = &plan.Definitions[index]
			}
		}
		if owner == nil {
			return Plan{}, fmt.Errorf("%s: install the schema for namespace %s in OpenGraph Management", name, metadata.Namespace)
		}
		paused := false
		for _, disabledName := range disabled {
			paused = paused || disabledName == name
		}
		compatible, err := SchemaVersionMatches(owner.Schema.Version, metadata.ExtensionVersions)
		if (!paused || !relaxDisabledChecks) && (err != nil || !compatible) {
			return Plan{}, fmt.Errorf("%s: installed schema version %s does not satisfy extension-versions %s", name, owner.Schema.Version, metadata.ExtensionVersions)
		}
		dependencies, err := parseDependencies(string(contents))
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", filename, err)
		}
		reach, err := parseReachability(string(contents), dependencies)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", filename, err)
		}
		overlap, err := parseGroupOverlap(string(contents), dependencies)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", filename, err)
		}
		if overlap != nil && reach != nil {
			return Plan{}, fmt.Errorf("%s: group-overlap cannot also request reachability", filename)
		}
		batchDependency, err := parseBatchDependency(string(contents), dependencies)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", filename, err)
		}
		query := Query{QueryMetadata: metadata, SchemaVersion: owner.Schema.Version, Extension: owner.Schema.Name, ExtensionName: owner.Schema.DisplayName, Name: name, Parameter: strings.TrimSuffix(filepath.Base(filename), ".cypher"), Text: string(contents), Dependencies: dependencies, Reach: reach, BatchDependency: batchDependency, Overlap: overlap}
		if paused {
			query.DisabledReason = "Disabled by you. Existing connections are kept."
		}
		if err := checkReadOnlyText(query.Text); err != nil {
			return Plan{}, fmt.Errorf("%s: %w", query.Name, err)
		}
		checked, err := querycheck.Check(query.Text)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", query.Name, err)
		}
		query.BroadRead = checked.BroadRead
		query.Columns, query.ColumnsKnown, query.SharedFields = checked.Columns, checked.ColumnsKnown, checked.SharedFields
		if query.Reach != nil || query.Overlap != nil {
			// Advanced calculations transform the rows after the Cypher read.
			// Their dedicated checks describe the resulting answer instead.
			query.ColumnsKnown = false
		}
		var missing []error
		for _, input := range checked.Inputs {
			declared := false
			for _, dependency := range dependencies {
				if dependency.Type == input.Type && (dependency.Name == input.Name || input.Type == "query" && dependency.AnswerParameter()+".cypher" == input.Name) {
					declared = true
					break
				}
			}
			if !declared {
				missing = append(missing, fmt.Errorf("%s: line %d: missing dependency; add %s:%s to // depends-on", query.Name, input.Line, input.Type, strings.TrimSuffix(input.Name, ".cypher")))
			}
		}
		if len(missing) > 0 {
			return Plan{}, errors.Join(missing...)
		}
		output, err := parseQueryOutput(query.Text)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: line %d: output: %w", query.Name, finalReturnLine(query.Text), err)
		}
		if err := bindQueryOutput(plan.Definitions, &query, output, name == editorTarget); err != nil {
			return Plan{}, fmt.Errorf("%s: line %d: %w", query.Name, finalReturnLine(query.Text), err)
		}
		queries[query.Name] = query
	}
	if editorTarget != "" && !targetFound {
		return Plan{}, fmt.Errorf("analysis file %q is not available for testing", editorTarget)
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
	ordered, err := orderQueries(queries, plan.Schema, editorTarget, relaxDisabledChecks)
	if err != nil {
		return Plan{}, err
	}
	if err := validateInputKinds(ordered, plan.Definitions); err != nil {
		return Plan{}, err
	}
	plan.Queries = ordered
	return plan, nil
}

// QueryDependencies reads the author's input declarations without executing
// the query. The rules page uses the same reader when removing dependent rules.
func QueryDependencies(contents string) ([]Dependency, error) {
	return parseDependencies(contents)
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
		kinds               = make(map[string]string)
		policies            = make(map[string]ExistingEdgePolicy)
		bindings            = make(map[string]bool)
		prefixes            = make(map[string]bool)
		hasESC1Relationship bool
	)
	if strings.TrimSpace(schema.Name) == "" {
		return fmt.Errorf("schema name must be nonblank")
	}
	if !referenceCommit.MatchString(schema.ReferenceCommit) {
		return fmt.Errorf("reference_commit must contain 40 hexadecimal characters")
	}
	bind := func(kind, filename, outputType string) error {
		if !publishedIdentifier.MatchString(kind) {
			return fmt.Errorf("published kind %q must start with PoC_ and be an identifier", kind)
		}
		if previous := kinds[kind]; previous != "" && (previous != "relationship" || outputType != "relationship") {
			return fmt.Errorf("duplicate published kind %q", kind)
		}
		if bindings[filename] {
			return fmt.Errorf("query %q has multiple output bindings", filename)
		}
		if _, exists := queries[filename]; !exists {
			return fmt.Errorf("binding for %q refers to missing query %q", kind, filename)
		}
		kinds[kind] = outputType
		bindings[filename] = true
		return nil
	}
	for _, binding := range schema.Nodes {
		if err := bind(binding.Kind, binding.Query, "node"); err != nil {
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
		if previous := policies[binding.Kind]; previous != "" && previous != binding.EffectiveExistingEdgePolicy() {
			return fmt.Errorf("producers for %s must agree on existing-edge-policy", binding.Kind)
		}
		policies[binding.Kind] = binding.EffectiveExistingEdgePolicy()
		if binding.Kind == "PoC_ADCSESC1" {
			hasESC1Relationship = true
		}
		if err := bind(binding.Kind, binding.Query, "relationship"); err != nil {
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

func orderQueries(queries map[string]Query, schema Schema, editorTarget string, relaxDisabledChecks bool) ([]Query, error) {
	var (
		names   []string
		ordered []Query
		state   = make(map[string]int)
		plan    = Plan{Schema: schema}
		path    []string
	)
	// Editing a producer may intentionally change columns that its current
	// consumers use. Test only the needed chain; ordinary activation still
	// validates the complete set and catches those downstream changes.
	var selected map[string]bool
	if editorTarget != "" {
		selectionPlan := plan
		for _, query := range queries {
			selectionPlan.Queries = append(selectionPlan.Queries, query)
		}
		selection, err := selectionPlan.SelectPrerequisites([]string{editorTarget})
		if err != nil {
			return nil, err
		}
		selected = map[string]bool{}
		for _, name := range selection {
			selected[name] = true
		}
	}
	for name, query := range queries {
		if selected != nil && !selected[name] {
			continue
		}
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
				return nil, fmt.Errorf("%s: line %d: missing prerequisite %s; upload that file with this draft", name, textLine(query.Text, dependency.Name), dependency.Name)
			}
			compatible, err := VersionMatches(queries[dependency.Name].Version, dependency.Conditions)
			if (query.DisabledReason == "" || !relaxDisabledChecks) && (err != nil || !compatible) {
				return nil, fmt.Errorf("%s: prerequisite %s at %s does not satisfy %s", name, dependency.Name, queries[dependency.Name].Version, dependency.Conditions)
			}
			declaredParameters[dependency.AnswerParameter()] = true
			producer := queries[dependency.Name]
			if producer.ColumnsKnown && (query.DisabledReason == "" || !relaxDisabledChecks) {
				for _, field := range query.SharedFields {
					if field.Parameter != dependency.AnswerParameter() {
						continue
					}
					found := false
					for _, column := range producer.Columns {
						found = found || column == field.Column
					}
					if !found {
						return nil, fmt.Errorf("%s: line %d: prerequisite %s does not return column %s", name, field.Line, dependency.Name, field.Column)
					}
				}
			}
		}
		for _, match := range queryParameter.FindAllStringSubmatch(parameterText(query.Text), -1) {
			if !declaredParameters[match[1]] {
				return nil, fmt.Errorf("%s: parameter $%s needs a declared query prerequisite", name, match[1])
			}
		}
	}
	// Files that only return rows are valid intermediate steps. Their answers
	// can be inspected and reused while the developer builds later consumers.
	sort.Strings(names)
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("query dependency cycle (including produced graph kinds): %s", strings.Join(append(path, name), " -> "))
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		path = append(path, name)
		for _, prerequisite := range plan.Prerequisites(queries[name]) {
			if err := visit(prerequisite); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[name] = 2
		ordered = append(ordered, queries[name])
		return nil
	}
	for _, name := range names {
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
		return fmt.Errorf("line %d: analysis files cannot use %s; only the runner may write graph data", tokenLine(contents, word), word)
	}
	for _, location := range calls.FindAllStringIndex(text, -1) {
		if location[1] == len(text) || text[location[1]] != '{' {
			return fmt.Errorf("line %d: analysis files cannot call procedures or contact external services", tokenLine(contents, "CALL"))
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
		if !coverageKnown || query.BroadRead && (len(changedNodes) != 0 || len(changedRelationships) != 0) {
			affected[query.Name] = true
		}
		for _, dependency := range query.Dependencies {
			if dependency.Type == "node" && nodes[dependency.Name] || dependency.Type == "relationship" && relationships[dependency.Name] || dependency.Type == "query" && affected[dependency.Name] {
				affected[query.Name] = true
			}
		}
	}

	for _, query := range s.Queries {
		if affected[query.Name] {
			result = append(result, query.Name)
		}
	}
	return s.ExpandSelection(result)
}

// Prerequisites includes returned rows and calculated graph kinds. Graph readers
// wait for every producer so they see the complete refreshed relationship set.
// Reading a rule's own relationship type uses the previous graph state. It does
// not wait for itself or its peers: their combined new answer needs this rule too.
func (s Plan) Prerequisites(query Query) []string {
	var (
		result []string
		seen   = make(map[string]bool)
	)
	for _, dependency := range s.ExecutionDependencies(query) {
		if !seen[dependency.Prerequisite] {
			seen[dependency.Prerequisite] = true
			result = append(result, dependency.Prerequisite)
		}
	}
	sort.Strings(result)
	return result
}

// ExecutionDependency explains why one query waits for another. The runner and
// workspace use this same list, so the picture cannot invent a different order.
// Several reasons can connect the same two queries.
type ExecutionDependency struct {
	Prerequisite string `json:"prerequisite"`
	Consumer     string `json:"consumer"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Parameter    string `json:"parameter,omitempty"`
	Conditions   string `json:"conditions,omitempty"`
}

func (s Plan) ExecutionDependencies(query Query) []ExecutionDependency {
	var (
		result                = []ExecutionDependency{}
		seen                  = make(map[ExecutionDependency]bool)
		producedRelationships = make(map[string]bool)
	)
	for _, binding := range s.Schema.Relationships {
		if binding.Query == query.Name {
			producedRelationships[binding.Kind] = true
		}
	}
	add := func(name string, dependency Dependency) {
		var connection = ExecutionDependency{Prerequisite: name, Consumer: query.Name, Type: dependency.Type, Name: dependency.Name}
		if dependency.Type == "query" {
			connection.Parameter, connection.Conditions = dependency.AnswerParameter(), dependency.Conditions
		}
		if !seen[connection] && (dependency.Type == "query" || name != query.Name) {
			seen[connection] = true
			result = append(result, connection)
		}
	}
	for _, dependency := range query.Dependencies {
		switch dependency.Type {
		case "query":
			add(dependency.Name, dependency)
		case "node":
			for _, binding := range s.Schema.Nodes {
				if binding.Kind == dependency.Name {
					add(binding.Query, dependency)
				}
			}
		case "relationship":
			if producedRelationships[dependency.Name] {
				continue
			}
			for _, binding := range s.Schema.Relationships {
				if binding.Kind == dependency.Name {
					add(binding.Query, dependency)
				}
			}
		}
	}
	return result
}

// ExpandSelection keeps every refreshed relationship type complete. Selecting
// one producer includes its peers, all needed inputs, and all consumers. Repeat
// until nothing changes: adding an input can itself affect another output.
func (s Plan) ExpandSelection(names []string) []string {
	var (
		selected = make(map[string]bool)
		result   []string
		changed  = true
	)
	for _, name := range names {
		selected[name] = true
	}
	for changed {
		changed = false
		include := func(name string) {
			if !selected[name] {
				selected[name] = true
				changed = true
			}
		}
		for _, binding := range s.Schema.Relationships {
			if !selected[binding.Query] {
				continue
			}
			for _, peer := range s.Schema.Relationships {
				if peer.Kind == binding.Kind {
					include(peer.Query)
				}
			}
		}
		for _, query := range s.Queries {
			for _, prerequisite := range s.Prerequisites(query) {
				if selected[query.Name] {
					include(prerequisite)
				}
				if selected[prerequisite] {
					include(query.Name)
				}
			}
		}
	}
	for _, query := range s.Queries {
		if selected[query.Name] {
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
