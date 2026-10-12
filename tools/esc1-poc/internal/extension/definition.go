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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Definition describes the graph model. PostProcessing fields remain readable
// for existing saved bundles; new outputs are discovered from query RETURNs.
// Both publication and the model viewer use this one description of the graph.
// Optional standard sections are preserved, even when this experiment's upload
// command does not support registering them.
type Definition struct {
	// LegacyProcessing distinguishes saved author settings from inferred metadata.
	LegacyProcessing     bool                `json:"-"`
	Schema               Metadata            `json:"schema"`
	NodeKinds            []NodeKind          `json:"node_kinds"`
	RelationshipKinds    []RelationshipKind  `json:"relationship_kinds"`
	PostProcessing       *ProcessingSettings `json:"post_processing,omitempty"`
	Environments         json.RawMessage     `json:"environments,omitempty"`
	RelationshipFindings json.RawMessage     `json:"relationship_findings,omitempty"`
	Queries              json.RawMessage     `json:"queries,omitempty"`
	PZRules              json.RawMessage     `json:"pz_rules,omitempty"`
}

type Metadata struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	Namespace   string `json:"namespace"`
}

type NodeKind struct {
	Name           string          `json:"name"`
	DisplayName    string          `json:"display_name,omitempty"`
	Description    string          `json:"description,omitempty"`
	IsDisplayKind  *bool           `json:"is_display_kind,omitempty"`
	Icon           string          `json:"icon,omitempty"`
	Color          string          `json:"color,omitempty"`
	Info           json.RawMessage `json:"info,omitempty"`
	PostProcessing *NodeProcessing `json:"post_processing,omitempty"`
}

// ExistingEdgePolicy controls existing calculated relationships. It does
// not change how BloodHound ingests collected relationships.
type ExistingEdgePolicy string

const (
	ReplaceExisting ExistingEdgePolicy = "replace-existing"
	MergeProperties ExistingEdgePolicy = "merge-properties"
	KeepExisting    ExistingEdgePolicy = "keep-existing"
)

func (s ExistingEdgePolicy) valid() bool {
	return s == ReplaceExisting || s == MergeProperties || s == KeepExisting
}

// Omission uses the default; an explicitly supplied value must name a policy.
func (s *ExistingEdgePolicy) UnmarshalJSON(contents []byte) error {
	var (
		value  string
		policy ExistingEdgePolicy
	)
	if err := json.Unmarshal(contents, &value); err != nil {
		return fmt.Errorf("existing_edge_policy must be replace-existing, merge-properties, or keep-existing")
	}
	policy = ExistingEdgePolicy(value)
	if !policy.valid() {
		return fmt.Errorf("existing_edge_policy must be replace-existing, merge-properties, or keep-existing")
	}
	*s = policy
	return nil
}

type RelationshipKind struct {
	// These fields are inferred from query files, never supplied by an author.
	producerQueries    []string
	producerPolicy     ExistingEdgePolicy
	ExistingEdgePolicy ExistingEdgePolicy      `json:"existing_edge_policy,omitempty"`
	Name               string                  `json:"name"`
	Description        string                  `json:"description,omitempty"`
	IsTraversable      *bool                   `json:"is_traversable,omitempty"`
	Info               json.RawMessage         `json:"info,omitempty"`
	CompatibleKinds    []Endpoint              `json:"compatible_kinds,omitempty"`
	PostProcessing     *RelationshipProcessing `json:"post_processing,omitempty"`
}

// One entry permits exactly one source/target combination. Separate lists of
// sources and targets would accidentally permit combinations nobody intended.
type Endpoint struct {
	SourceKind string `json:"source_kind"`
	TargetKind string `json:"target_kind"`
}

type ProcessingSettings struct {
	ReferenceCommit   string `json:"reference_commit"`
	CompareNativeESC1 bool   `json:"compare_native_esc1,omitempty"`
}

type NodeProcessing struct {
	Query    string `json:"query"`
	IDPrefix string `json:"id_prefix"`
}

type RelationshipProcessing struct {
	Query string `json:"query"`
}

// LoadDefinition does not read queries or connect to BloodHound. A definition
// can describe collected kinds alone and still have a complete model diagram.
func LoadDefinition(directory string) (Definition, error) {
	var contents, err = os.ReadFile(filepath.Join(directory, "schema.json"))
	if err != nil {
		return Definition{}, err
	}
	definition, err := ParseDefinition(contents)
	if err != nil {
		return Definition{}, err
	}
	return definition, ValidateDefinitions([]Definition{definition})
}

// ParseDefinition checks one file's shape. References to other kinds are checked
// by ValidateDefinitions once all of the accompanying schemas have been read.
func ParseDefinition(contents []byte) (Definition, error) {
	var (
		definition Definition
		trailing   any
		decoder    = json.NewDecoder(bytes.NewReader(contents))
	)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return Definition{}, fmt.Errorf("schema.json: %w", err)
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Definition{}, fmt.Errorf("schema.json: expected one JSON object")
	}
	definition.LegacyProcessing = definition.PostProcessing != nil
	if err := definition.validate(); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

func (s Definition) validate() error {
	var (
		declared      = make(map[string]bool)
		relationships = make(map[string]bool)
		prefix        = s.Schema.Namespace + "_"
	)
	if strings.TrimSpace(s.Schema.Name) == "" || strings.TrimSpace(s.Schema.DisplayName) == "" || !identifier.MatchString(s.Schema.Namespace) || !strings.HasPrefix(s.Schema.Version, "v") {
		return fmt.Errorf("schema needs name, display_name, namespace, and a version beginning with v")
	}
	if s.NodeKinds == nil || s.RelationshipKinds == nil {
		return fmt.Errorf("node_kinds and relationship_kinds must be arrays")
	}
	for _, kind := range s.NodeKinds {
		if !identifier.MatchString(kind.Name) || !strings.HasPrefix(kind.Name, prefix) || len(kind.Name) == len(prefix) {
			return fmt.Errorf("node kind %q must have the schema namespace prefix %s", kind.Name, prefix)
		}
		if declared[kind.Name] {
			return fmt.Errorf("duplicate node kind %q", kind.Name)
		}
		declared[kind.Name] = true
	}
	for _, kind := range s.RelationshipKinds {
		if !identifier.MatchString(kind.Name) || !strings.HasPrefix(kind.Name, prefix) || len(kind.Name) == len(prefix) {
			return fmt.Errorf("relationship kind %q must have the schema namespace prefix %s", kind.Name, prefix)
		}
		if relationships[kind.Name] {
			return fmt.Errorf("duplicate relationship kind %q", kind.Name)
		}
		relationships[kind.Name] = true
		if kind.ExistingEdgePolicy != "" && !kind.ExistingEdgePolicy.valid() {
			return fmt.Errorf("relationship %q has an invalid existing_edge_policy", kind.Name)
		}
		if len(kind.CompatibleKinds) == 0 {
			return fmt.Errorf("relationship %q needs explicit compatible_kinds pairs", kind.Name)
		}
		seen := make(map[Endpoint]bool)
		for _, endpoint := range kind.CompatibleKinds {
			if !identifier.MatchString(endpoint.SourceKind) || !identifier.MatchString(endpoint.TargetKind) {
				return fmt.Errorf("%s compatible_kinds has invalid source_kind or target_kind names: %q -> %q", kind.Name, endpoint.SourceKind, endpoint.TargetKind)
			}
			if seen[endpoint] {
				return fmt.Errorf("%s has duplicate endpoint pair %s -> %s", kind.Name, endpoint.SourceKind, endpoint.TargetKind)
			}
			seen[endpoint] = true
		}
	}
	return nil
}

// BloodHoundJSON removes only our proposed additions. Current BloodHound can
// display the kinds, but does not understand these endpoint rules or run queries.
// Copy the slices before editing, so exporting cannot change the local model.
func (s Definition) BloodHoundJSON() ([]byte, error) {
	s.PostProcessing = nil
	s.NodeKinds = append([]NodeKind{}, s.NodeKinds...)
	s.RelationshipKinds = append([]RelationshipKind{}, s.RelationshipKinds...)
	for index := range s.NodeKinds {
		s.NodeKinds[index].PostProcessing = nil
	}
	for index := range s.RelationshipKinds {
		s.RelationshipKinds[index].ExistingEdgePolicy = ""
		s.RelationshipKinds[index].CompatibleKinds = nil
		s.RelationshipKinds[index].PostProcessing = nil
	}
	return json.Marshal(s)
}

// Only kinds naming a query become managed output. Merely describing a
// collected kind does not give the runner permission to update or delete it.
func (s Definition) processingSchema() (Schema, error) {
	var result Schema
	if s.PostProcessing == nil {
		return result, fmt.Errorf("running queries requires top-level post_processing settings")
	}
	result.Name = s.Schema.Name
	result.ReferenceCommit = s.PostProcessing.ReferenceCommit
	result.CompareNativeESC1 = s.PostProcessing.CompareNativeESC1
	for _, kind := range s.NodeKinds {
		if kind.PostProcessing != nil {
			result.Nodes = append(result.Nodes, NodeBinding{Kind: kind.Name, Query: kind.PostProcessing.Query, IDPrefix: kind.PostProcessing.IDPrefix})
		}
	}
	for _, kind := range s.RelationshipKinds {
		policy := kind.ExistingEdgePolicy
		if kind.producerPolicy != "" {
			policy = kind.producerPolicy
		}
		for _, query := range kind.producingQueries() {
			result.Relationships = append(result.Relationships, RelationshipBinding{Kind: kind.Name, Query: query, Endpoints: kind.CompatibleKinds, ExistingEdgePolicy: policy})
		}
	}
	return result, nil
}

// The schema can name a query explicitly, and additional query files can return
// the same kind. Keep the definition singular while retaining every producer.
func (s RelationshipKind) producingQueries() []string {
	var (
		result []string
		seen   = make(map[string]bool)
	)
	if s.PostProcessing != nil {
		result = append(result, s.PostProcessing.Query)
		seen[s.PostProcessing.Query] = true
	}
	for _, query := range s.producerQueries {
		if !seen[query] {
			result = append(result, query)
			seen[query] = true
		}
	}
	return result
}
