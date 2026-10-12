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
	"os"
	"path/filepath"
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
	MaxStatusBytes = 16 << 10
)

var (
	queryName       = regexp.MustCompile(`^[a-z][a-z0-9_]*\.cypher$`)
	retiredKindName = regexp.MustCompile(`^PoC_[A-Za-z_][A-Za-z0-9_]*$`)
)

// Bundle is one complete snapshot. A worker keeps its snapshot even if an author
// uploads replacement files while that worker is calculating.
type Bundle struct {
	Revision string            `json:"revision"`
	Files    map[string]string `json:"files"`
	// Platform-owned identities survive replacement of legacy schema settings.
	Owners map[string]string `json:"owners,omitempty"`
	// Keep this history across uploads, including rules removed before the
	// worker fetched them. An older, already-running snapshot may still finish.
	RetiredRelationships []string             `json:"retired_relationships,omitempty"`
	RetiredOwners        map[string]string    `json:"retired_owners,omitempty"`
	Disabled             []string             `json:"disabled,omitempty"`
	AddedAt              map[string]time.Time `json:"added_at,omitempty"`
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
	if len(bundle.Disabled) == 0 && len(bundle.RetiredOwners) == 0 && len(bundle.Owners) == 0 {
		return revision(bundle.Files, bundle.RetiredRelationships)
	}
	contents, _ := json.Marshal(Bundle{Files: bundle.Files, Owners: bundle.Owners, RetiredRelationships: bundle.RetiredRelationships, RetiredOwners: bundle.RetiredOwners, Disabled: bundle.Disabled})
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func checkFiles(files map[string]string) error {
	if len(files) == 0 || len(files) > MaxFiles {
		return fmt.Errorf("a bundle must contain 1 to %d files", MaxFiles)
	}
	var hasSchema bool
	for name := range files {
		if extension.IsSchemaFile(name) {
			hasSchema = true
		}
	}
	if !hasSchema {
		return fmt.Errorf("include schema.json or a named .schema.json file and its required queries")
	}
	for name, contents := range files {
		if !extension.IsSchemaFile(name) && !queryName.MatchString(name) {
			return fmt.Errorf("use schema.json, extension_name.schema.json, and snake_case.cypher filenames")
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
	plan, err := loadFiles(directory, bundle.Files)
	if err != nil {
		return extension.Plan{}, err
	}
	if err := applyOwners(bundle.Owners, &plan); err != nil {
		return extension.Plan{}, err
	}
	if err := checkRetirements(bundle, plan); err != nil {
		return extension.Plan{}, err
	}
	if err := applyRuleSettings(bundle, &plan); err != nil {
		return extension.Plan{}, err
	}
	plan.RetiredRelationships = append([]string(nil), bundle.RetiredRelationships...)
	plan.RetiredOwners = copyOwners(bundle.RetiredOwners)
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
	for kind, owner := range bundle.RetiredOwners {
		if !seen[kind] || strings.TrimSpace(owner) == "" {
			return fmt.Errorf("removed relationship %s has invalid ownership", kind)
		}
	}
	if len(plan.Definitions) > 1 {
		for kind := range seen {
			if bundle.RetiredOwners[kind] == "" {
				return fmt.Errorf("removed relationship %s is missing its original owner", kind)
			}
		}
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
	for _, query := range plan.Queries {
		for _, input := range query.Dependencies {
			if input.Type == "relationship" && seen[input.Name] {
				return fmt.Errorf("%s still reads removed relationship %s; update or remove that rule too", query.Name, input.Name)
			}
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

func loadFiles(parent string, files map[string]string) (extension.Plan, error) {
	directory, err := os.MkdirTemp(parent, "validate-")
	if err != nil {
		return extension.Plan{}, err
	}
	defer os.RemoveAll(directory)
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0600); err != nil {
			return extension.Plan{}, err
		}
	}
	return extension.Load(directory)
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
