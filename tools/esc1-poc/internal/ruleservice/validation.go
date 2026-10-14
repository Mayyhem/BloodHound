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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

const (
	MaxFiles       = 32
	MaxFileBytes   = 1 << 20
	MaxBundleBytes = 4 << 20
	MaxStatusBytes = 64 << 10
)

var (
	queryName        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*/[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.cypher$`)
	retiredKindName  = regexp.MustCompile(`^PoC_[A-Za-z_][A-Za-z0-9_]*$`)
	endpointKindName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Keep the installed owner of a removed output even if a newer definition
// no longer declares that kind. This record cannot install or restore a schema.
type SchemaAssociation struct {
	ID        int32  `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Bundle is one complete snapshot. A worker keeps its snapshot even if an author
// uploads replacement files while that worker is calculating.
type Bundle struct {
	Schemas        []InstalledSchema `json:"schemas"`
	SchemaRevision string            `json:"schema_revision"`
	Suspended      map[string]string `json:"suspended,omitempty"`
	Revision       string            `json:"revision"`
	Files          map[string]string `json:"files"`
	// Keep this history across uploads, including rules removed before the
	// worker fetched them. An older, already-running snapshot may still finish.
	RetiredRelationships []string                        `json:"retired_relationships,omitempty"`
	RetiredEndpoints     map[string][]extension.Endpoint `json:"retired_endpoints,omitempty"`
	RetiredSchemas       map[string]SchemaAssociation    `json:"retired_schemas,omitempty"`
	Disabled             []string                        `json:"disabled,omitempty"`
	AddedAt              map[string]time.Time            `json:"added_at,omitempty"`
}

func revision(files map[string]string, retired ...[]string) string {
	var contents []byte
	if len(retired) == 0 || len(retired[0]) == 0 {
		contents, _ = json.Marshal(files)
	} else {
		contents, _ = json.Marshal(Bundle{Files: files, RetiredRelationships: retired[0]})
	}
	fingerprint := sha256.Sum256(contents)
	return hex.EncodeToString(fingerprint[:])
}

// Dates describe the saved files but do not change what runs. Leaving them out
// lets Preview and Apply agree even though new dates are assigned only on save.
func bundleRevision(bundle Bundle) string {
	bundle.Revision, bundle.AddedAt = "", nil
	contents, _ := json.Marshal(bundle)
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func checkFiles(files map[string]string) error { return checkFileContents(files) }

// Drafts use the same filenames and size limits, but may be missing schemas or
// queries. Only a complete, validated bundle can be activated.
func checkFileContents(files map[string]string) error {
	if len(files) > MaxFiles {
		return fmt.Errorf("a bundle must contain 1 to %d files", MaxFiles)
	}
	for name, contents := range files {
		if !queryName.MatchString(name) {
			return fmt.Errorf("upload .cypher files with namespace/version comments; upload schemas through OpenGraph Management")
		}
		if len(contents) == 0 || len(contents) > MaxFileBytes || !utf8.ValidString(contents) {
			return fmt.Errorf("%s must be nonempty UTF-8 text, at most %d bytes", name, MaxFileBytes)
		}
	}
	contents, err := json.Marshal(Bundle{Revision: revision(files), Files: files})
	if err != nil {
		return err
	}
	if len(contents) > MaxBundleBytes {
		return fmt.Errorf("the complete bundle exceeds the %d-byte limit", MaxBundleBytes)
	}
	for name, contents := range files {
		if extension.IsSchemaFile(name) {
			if err := uniqueJSON([]byte(contents)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateBundle checks the advertised digest and the same definition/query
// contract used by the runner. Temporary validation files are always removed.
func ValidateBundle(bundle Bundle) (extension.Plan, error) {
	return validateBundle("", bundle)
}

// The registry container has a read-only filesystem apart from its rule
// directory. Reopening saved rules must validate inside that writable folder.
func validateBundle(directory string, bundle Bundle) (extension.Plan, error) {
	if err := checkFiles(bundle.Files); err != nil {
		return extension.Plan{}, err
	}
	if bundle.Revision != bundleRevision(bundle) {
		return extension.Plan{}, fmt.Errorf("bundle revision does not match its files and removal history")
	}
	plan, err := loadBundle(bundle)
	if err != nil {
		return extension.Plan{}, err
	}
	if err := checkRetirements(bundle, plan); err != nil {
		return extension.Plan{}, err
	}
	if err := applyRuleSettings(bundle, &plan); err != nil {
		return extension.Plan{}, err
	}
	plan.RetiredRelationships = append([]string(nil), bundle.RetiredRelationships...)
	plan.RetiredEndpoints = copyEndpoints(bundle.RetiredEndpoints)
	return plan, nil
}

// Removal history is bounded and must never overlap a current producer. Keep
// old names until they are explicitly reintroduced: a success report can be
// lost, and an earlier pinned revision may finish after a newer upload.
func checkRetirements(bundle Bundle, plan extension.Plan) error {
	var seen = map[string]bool{}
	if len(bundle.RetiredRelationships) > 1024 || !sort.StringsAreSorted(bundle.RetiredRelationships) {
		return fmt.Errorf("removal history must be sorted and contain at most 1024 kinds")
	}
	for _, kind := range bundle.RetiredRelationships {
		if !retiredKindName.MatchString(kind) || seen[kind] {
			return fmt.Errorf("invalid or duplicate removed relationship kind %q", kind)
		}
		seen[kind] = true
	}
	for _, binding := range plan.Schema.Relationships {
		if seen[binding.Kind] {
			return fmt.Errorf("%s cannot be both active and removed", binding.Kind)
		}
	}
	for _, binding := range plan.Schema.Nodes {
		if seen[binding.Kind] {
			return fmt.Errorf("removed relationship %s cannot become a helper kind", binding.Kind)
		}
	}
	// Graph readers remain valid after a producer is deleted. Save the old
	// endpoint pairs separately so an author can rename a type in the schema
	// without losing the scope needed to clear its previous connections.
	for _, kind := range bundle.RetiredRelationships {
		owner := bundle.RetiredSchemas[kind]
		if owner.ID <= 0 || owner.Name == "" || owner.Namespace == "" {
			return fmt.Errorf("removed relationship %s is missing its extension association", kind)
		}
		if len(bundle.RetiredEndpoints[kind]) == 0 {
			return fmt.Errorf("removed relationship %s is missing its previous endpoints", kind)
		}
	}
	for kind := range bundle.RetiredSchemas {
		if !seen[kind] {
			return fmt.Errorf("saved schema association refers to a relationship that was not removed: %s", kind)
		}
	}
	for kind, endpoints := range bundle.RetiredEndpoints {
		var pairs = map[extension.Endpoint]bool{}
		if !seen[kind] {
			return fmt.Errorf("saved endpoints refer to a relationship that was not removed: %s", kind)
		}
		for _, endpoint := range endpoints {
			if !endpointKindName.MatchString(endpoint.SourceKind) || !endpointKindName.MatchString(endpoint.TargetKind) || pairs[endpoint] {
				return fmt.Errorf("removed relationship %s has an invalid or repeated endpoint pair", kind)
			}
			pairs[endpoint] = true
		}
	}
	contents, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if len(contents) > MaxBundleBytes {
		return fmt.Errorf("rules and removal history exceed the bundle byte limit")
	}
	return nil
}

func loadBundle(bundle Bundle) (extension.Plan, error) {
	definitions, err := schemaDefinitions(bundle.Schemas)
	if err != nil {
		return extension.Plan{}, err
	}
	// Legacy offline fixtures can bind output in their schema. Removing a
	// file also removes that binding; the saved snapshot stays untouched.
	for index := range definitions {
		definition := &definitions[index]
		for kindIndex := range definition.NodeKinds {
			binding := definition.NodeKinds[kindIndex].PostProcessing
			if binding != nil {
				if _, exists := bundle.Files[savedQueryKey(definition.Schema.Namespace, binding.Query)]; !exists {
					definition.NodeKinds[kindIndex].PostProcessing = nil
				}
			}
		}
		for kindIndex := range definition.RelationshipKinds {
			binding := definition.RelationshipKinds[kindIndex].PostProcessing
			if binding != nil {
				if _, exists := bundle.Files[savedQueryKey(definition.Schema.Namespace, binding.Query)]; !exists {
					definition.RelationshipKinds[kindIndex].PostProcessing = nil
				}
			}
		}
	}
	var disabled = append([]string{}, bundle.Disabled...)
	for name := range bundle.Suspended {
		disabled = append(disabled, name)
	}
	return extension.LoadFilesWithDisabled(definitions, bundle.Files, disabled)
}

// Uploaded filenames are namespaced by the file itself. Control operations
// use the full saved key; there is no global basename lookup.
func normalizeChange(change Change) (Change, error) {
	var files = map[string]string{}
	change.Overwrite = append([]string{}, change.Overwrite...)
	for filename, contents := range change.Files {
		name, err := extension.QueryFileKey(filename, contents)
		if err != nil {
			return Change{}, fmt.Errorf("%s: %w", filename, err)
		}
		if _, exists := files[name]; exists {
			return Change{}, fmt.Errorf("duplicate analysis %s", name)
		}
		files[name] = contents
		for index, overwrite := range change.Overwrite {
			if overwrite == filename {
				change.Overwrite[index] = name
			}
		}
	}
	change.Files = files
	return change, nil
}

// Reject duplicate keys instead of letting a later key silently replace an
// earlier one. This also covers duplicate keys inside the uploaded schema.
func uniqueJSON(contents []byte) error {
	if !utf8.Valid(contents) {
		return fmt.Errorf("JSON must be valid UTF-8")
	}
	var decoder = json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := consumeJSON(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func consumeJSON(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return fmt.Errorf("JSON nesting exceeds the limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return fmt.Errorf("invalid JSON delimiter")
	}
	var keys = map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, valid := token.(string)
			if !valid || keys[key] {
				return fmt.Errorf("duplicate or invalid JSON object key")
			}
			keys[key] = true
		}
		if err := consumeJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func decodeJSON(contents []byte, destination any) error {
	if err := uniqueJSON(contents); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

// Old offline examples may already carry a full name in their output binding.
func savedQueryKey(namespace, name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	return namespace + "/" + name
}
