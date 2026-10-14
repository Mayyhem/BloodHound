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

package ruleservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// These are read-only copies of BloodHound's installed records. Retaining a
// copy after deletion preserves file ownership and the old cleanup boundaries;
// no code in this package can install a schema.
type InstalledSchema struct {
	ID         int32           `json:"id"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace"`
	Version    string          `json:"version"`
	Definition json.RawMessage `json:"definition"`
}

type SchemaSnapshot struct {
	Definitions []InstalledSchema `json:"definitions"`
}

type SchemaReader interface {
	Schemas(context.Context) (SchemaSnapshot, error)
}

func ParseSchemas(contents []byte) (SchemaSnapshot, error) {
	var snapshot SchemaSnapshot
	if len(contents) > MaxBundleBytes || json.Unmarshal(contents, &snapshot) != nil || snapshot.Definitions == nil {
		return SchemaSnapshot{}, fmt.Errorf("invalid installed schema response")
	}
	if err := snapshot.validate(); err != nil {
		return SchemaSnapshot{}, err
	}
	return snapshot, nil
}

func (s SchemaSnapshot) validate() error {
	if s.Definitions == nil {
		return fmt.Errorf("installed schema response must contain a definitions list")
	}
	var (
		names      = map[string]bool{}
		namespaces = map[string]bool{}
		ids        = map[int32]bool{}
	)
	for _, schema := range s.Definitions {
		if schema.ID <= 0 || schema.Name == "" || schema.Namespace == "" || names[schema.Name] || namespaces[schema.Namespace] || ids[schema.ID] {
			return fmt.Errorf("installed schema identities are missing or repeated")
		}
		names[schema.Name], namespaces[schema.Namespace], ids[schema.ID] = true, true, true
		if len(schema.Definition) == 0 || string(schema.Definition) == "null" {
			continue
		}
		definition, err := extension.ParseDefinition(schema.Definition)
		if err != nil {
			return fmt.Errorf("installed definition for %s is not usable for analysis: %w", schema.Namespace, err)
		}
		if definition.Schema.Name != schema.Name || definition.Schema.Namespace != schema.Namespace || definition.Schema.Version != schema.Version {
			return fmt.Errorf("installed definition does not match its extension record")
		}
	}
	return nil
}

func (s SchemaSnapshot) revision() string {
	var schemas = append([]InstalledSchema{}, s.Definitions...)
	sort.Slice(schemas, func(left, right int) bool { return schemas[left].Namespace < schemas[right].Namespace })
	contents, _ := json.Marshal(schemas)
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func schemaDefinitions(schemas []InstalledSchema) ([]extension.Definition, error) {
	var definitions []extension.Definition
	for _, schema := range schemas {
		if len(schema.Definition) == 0 || string(schema.Definition) == "null" {
			continue
		}
		definition, err := extension.ParseDefinition(schema.Definition)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

// RefreshSchemas is called after an authenticated read, including before Apply.
// A deletion or incompatibility latches a pause on disk. A later reinstall
// cannot silently restart retained analyses; Enable must be reviewed first.
func (s *Store) RefreshSchemas(snapshot SchemaSnapshot) error {
	if err := snapshot.validate(); err != nil {
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.schemas = SchemaSnapshot{Definitions: cloneSchemas(snapshot.Definitions)}
	if s.active.Revision == "" {
		return nil
	}
	plan, err := ValidateInstalledBundle(s.active, snapshot)
	if err != nil {
		return err
	}
	candidate := cloneBundle(s.active)
	for _, query := range plan.Queries {
		if query.SchemaPaused {
			candidate.Suspended[query.Name] = query.DisabledReason
		}
	}
	candidate.Revision = bundleRevision(candidate)
	if _, err := s.activate(candidate, plan); err != nil {
		return err
	}
	// Compatible schema changes can alter endpoint limits or display metadata
	// without changing the analysis files. Keep the live view up to date too.
	s.plan = plan
	return nil
}

func containsName(names []string, wanted string) bool {
	for _, name := range names {
		if name == wanted {
			return true
		}
	}
	return false
}

func cloneSchemas(schemas []InstalledSchema) []InstalledSchema {
	var result = append([]InstalledSchema{}, schemas...)
	for index := range result {
		var compact bytes.Buffer
		if len(result[index].Definition) != 0 && json.Compact(&compact, result[index].Definition) == nil {
			result[index].Definition = append(json.RawMessage{}, compact.Bytes()...)
		} else {
			result[index].Definition = append(json.RawMessage{}, result[index].Definition...)
		}
	}
	return result
}

func (s *Store) reviewedSchemas(previous []InstalledSchema, change Change) ([]InstalledSchema, error) {
	if s.schemas.Definitions == nil {
		return nil, fmt.Errorf("connect the rules server to BloodHound to read installed schemas")
	}
	var (
		byNamespace = map[string]InstalledSchema{}
		result      []InstalledSchema
	)
	for _, schema := range previous {
		byNamespace[schema.Namespace] = schema
	}
	// Keep the declaration that explained retained files. Current schemas are
	// checked separately on every run. A new upload or reviewed Enable adopts
	// the current declaration for that extension. This lets unrelated changes
	// proceed while an old extension has missing or changed graph kinds.
	var reviewed = map[string]bool{}
	for name := range change.Files {
		reviewed[strings.SplitN(name, "/", 2)[0]] = true
	}
	for _, name := range change.Enable {
		reviewed[strings.SplitN(name, "/", 2)[0]] = true
	}
	for _, schema := range s.schemas.Definitions {
		if _, exists := byNamespace[schema.Namespace]; exists && !reviewed[schema.Namespace] {
			continue
		}
		if saved, exists := byNamespace[schema.Namespace]; exists && saved.Name != schema.Name {
			// Retained analyses must not become files of a different extension
			// merely because its new schema reuses the old namespace.
			continue
		}
		byNamespace[schema.Namespace] = schema
	}
	for _, schema := range byNamespace {
		result = append(result, schema)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Namespace < result[right].Namespace })
	return cloneSchemas(result), nil
}

func (s *Store) checkUploadedAssociations(files map[string]string, previous []InstalledSchema) error {
	for _, contents := range files {
		metadata, err := extension.ReadQueryMetadata(contents)
		if err != nil {
			return err
		}
		var installed *InstalledSchema
		for index := range s.schemas.Definitions {
			if s.schemas.Definitions[index].Namespace == metadata.Namespace {
				installed = &s.schemas.Definitions[index]
			}
		}
		if installed == nil {
			return fmt.Errorf("install the schema for %s through OpenGraph Management", metadata.Namespace)
		}
		for _, saved := range previous {
			if saved.Namespace == metadata.Namespace && saved.Name != installed.Name {
				return fmt.Errorf("namespace %s now belongs to a different extension; retained analyses cannot be reassigned", metadata.Namespace)
			}
		}
		compatible, err := extension.SchemaVersionMatches(installed.Version, metadata.ExtensionVersions)
		if err != nil || !compatible {
			return fmt.Errorf("%s schema version %s does not satisfy extension-versions %s", metadata.Namespace, installed.Version, metadata.ExtensionVersions)
		}
	}
	return nil
}

// Execution repeats this check while BloodHound holds the shared graph turn.
// The saved definitions explain retained files; only current installed records
// authorize execution. Missing inputs pause their consumers and shared outputs.
func ValidateInstalledBundle(bundle Bundle, snapshot SchemaSnapshot) (extension.Plan, error) {
	plan, err := ValidateBundle(bundle)
	if err != nil {
		return extension.Plan{}, err
	}
	return checkInstalledPlan(bundle, plan, snapshot)
}

func checkInstalledPlan(bundle Bundle, plan extension.Plan, snapshot SchemaSnapshot) (extension.Plan, error) {
	var (
		current       = map[string]InstalledSchema{}
		previous      = map[string]InstalledSchema{}
		nodes         = map[string]bool{}
		relationships = map[string]extension.RelationshipKind{}
		reasons       = map[string]string{}
	)
	if err := snapshot.validate(); err != nil {
		return extension.Plan{}, err
	}
	for _, schema := range snapshot.Definitions {
		current[schema.Namespace] = schema
	}
	for _, schema := range bundle.Schemas {
		previous[schema.Namespace] = schema
	}
	definitions, err := schemaDefinitions(snapshot.Definitions)
	if err != nil {
		return extension.Plan{}, err
	}
	for _, name := range extension.NativeNodeKinds() {
		nodes[name] = true
	}
	for _, definition := range definitions {
		for _, kind := range definition.NodeKinds {
			nodes[kind.Name] = true
		}
		for _, kind := range definition.RelationshipKinds {
			relationships[kind.Name] = kind
		}
	}
	for index := range plan.Queries {
		query := &plan.Queries[index]
		installed, exists := current[query.Namespace]
		saved := previous[query.Namespace]
		query.SchemaVersion = ""
		if exists {
			query.SchemaVersion = installed.Version
		}
		switch {
		case !exists:
			reasons[query.Name] = "Extension schema is absent. Existing connections are kept."
		case installed.Name != saved.Name || installed.ID != saved.ID:
			reasons[query.Name] = "The extension was replaced or reinstalled. Review before enabling this analysis."
		case len(installed.Definition) == 0 || string(installed.Definition) == "null":
			reasons[query.Name] = "Upload this extension's schema again through OpenGraph Management."
		default:
			compatible, err := extension.SchemaVersionMatches(installed.Version, query.ExtensionVersions)
			if err != nil || !compatible {
				reasons[query.Name] = "Installed schema version " + installed.Version + " does not satisfy extension-versions " + query.ExtensionVersions + "."
			}
			query.SchemaVersion = installed.Version
		}
		for _, input := range query.Dependencies {
			if input.Type == "node" && !nodes[input.Name] || input.Type == "relationship" && !extension.IsNativeRelationshipKind(input.Name) && relationships[input.Name].Name == "" {
				reasons[query.Name] = "A required graph kind is no longer installed: " + input.Name + ". Existing connections are kept."
			}
		}
	}
	for _, binding := range plan.Schema.Nodes {
		if !nodes[binding.Kind] {
			reasons[binding.Query] = "The helper kind is no longer installed. Existing objects are kept."
		}
	}
	for index := range plan.Schema.Relationships {
		binding := &plan.Schema.Relationships[index]
		kind, exists := relationships[binding.Kind]
		if !exists || len(kind.CompatibleKinds) == 0 {
			reasons[binding.Query] = "The output kind or its compatible_kinds definition is unavailable. Existing connections are kept."
			continue
		}
		for _, pair := range kind.CompatibleKinds {
			if !nodes[pair.SourceKind] || !nodes[pair.TargetKind] {
				reasons[binding.Query] = "An output endpoint kind is no longer installed. Existing connections are kept."
			}
		}
		binding.Endpoints = append([]extension.Endpoint{}, kind.CompatibleKinds...)
	}
	for index := range plan.Queries {
		plan.Queries[index].DisabledReason = reasons[plan.Queries[index].Name]
	}
	// File removal can leave cleanup waiting for a deleted extension. Keep
	// that history, but do not clear its old graph answer while its schema or
	// endpoint kinds are unavailable. An intentional schema rename still
	// permits cleanup when the same installed extension owns the old type.
	plan.RetiredRelationships = nil
	for _, kind := range bundle.RetiredRelationships {
		owner := bundle.RetiredSchemas[kind]
		installed, exists := current[owner.Namespace]
		available := exists && installed.ID == owner.ID && installed.Name == owner.Name && len(installed.Definition) > 0 && string(installed.Definition) != "null"
		for _, pair := range bundle.RetiredEndpoints[kind] {
			available = available && nodes[pair.SourceKind] && nodes[pair.TargetKind]
		}
		if available {
			plan.RetiredRelationships = append(plan.RetiredRelationships, kind)
		} else {
			plan.CleanupPending = true
		}
	}
	plan.SchemaRevision = snapshot.revision()
	if err := applyRuleSettings(Bundle{}, &plan); err != nil {
		return extension.Plan{}, err
	}
	for index := range plan.Queries {
		plan.Queries[index].SchemaPaused = plan.Queries[index].DisabledReason != ""
	}
	if err := applyRuleSettings(bundle, &plan); err != nil {
		return extension.Plan{}, err
	}
	return plan, nil
}

// A failed read must not make the last successful snapshot look current when
// explaining a newly saved draft. Files and downloads remain available.
func (s *Store) SchemasUnavailable() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.schemas = SchemaSnapshot{}
}
