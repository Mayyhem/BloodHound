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
	"strings"
)

type QueryMetadata struct {
	Namespace         string `json:"namespace"`
	Version           string `json:"version"`
	ExtensionVersions string `json:"extension_versions,omitempty"`
}

// Only comments before the first Cypher statement can declare file metadata.
// Text inside a string or a later explanatory comment cannot change identity.
func leadingComments(contents string) []string {
	var (
		comments []string
		inBlock  bool
	)
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if inBlock {
			_, rest, ended := strings.Cut(line, "*/")
			if !ended {
				continue
			}
			inBlock, line = false, strings.TrimSpace(rest)
		}
		if strings.HasPrefix(line, "/*") {
			_, rest, ended := strings.Cut(line, "*/")
			inBlock = !ended
			if !ended {
				continue
			}
			line = strings.TrimSpace(rest)
		}
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		comments = append(comments, strings.TrimSpace(strings.TrimPrefix(line, "//")))
	}
	return comments
}

func ReadQueryMetadata(contents string) (QueryMetadata, error) {
	var (
		result QueryMetadata
		seen   = map[string]bool{}
	)
	for _, comment := range leadingComments(contents) {
		name, value, found := strings.Cut(comment, ":")
		if !found {
			continue
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if name != "namespace" && name != "version" && name != "extension-versions" {
			continue
		}
		if seen[name] || value == "" {
			return QueryMetadata{}, fmt.Errorf("%s must occur once and cannot be empty", name)
		}
		seen[name] = true
		switch name {
		case "namespace":
			result.Namespace = value
		case "version":
			result.Version = value
		case "extension-versions":
			result.ExtensionVersions = value
		}
	}
	if !identifier.MatchString(result.Namespace) {
		return QueryMetadata{}, fmt.Errorf("add one leading // namespace: declaration with the extension's namespace")
	}
	if _, err := parseVersion(result.Version); err != nil {
		return QueryMetadata{}, fmt.Errorf("// version: %w", err)
	}
	if _, err := parseVersionConditions(result.ExtensionVersions); err != nil {
		return QueryMetadata{}, fmt.Errorf("// extension-versions: %w", err)
	}
	return result, nil
}

// QueryFileKey is an identifier for a saved file, not an arbitrary path. Every
// caller checks both components before using it in a store or temporary folder.
func QueryFileKey(filename, contents string) (string, error) {
	metadata, err := ReadQueryMetadata(contents)
	if err != nil {
		return "", err
	}
	parts := strings.Split(filename, "/")
	if len(parts) > 2 || !queryFilename.MatchString(parts[len(parts)-1]) {
		return "", fmt.Errorf("use a snake_case.cypher filename")
	}
	if len(parts) == 2 && parts[0] != metadata.Namespace {
		return "", fmt.Errorf("the selected extension and the file's namespace do not match")
	}
	return metadata.Namespace + "/" + parts[len(parts)-1], nil
}
