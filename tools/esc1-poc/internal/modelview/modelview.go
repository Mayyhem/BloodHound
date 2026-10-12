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

// Package modelview renders a proposed OpenGraph type model as an offline viewer.
package modelview

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

//go:embed viewer.html
var viewer string

// Render returns a standalone HTML page for one or more schema files. All
// referenced custom kinds must be defined in this set, just as for rule upload.
// Re-encoding JSON keeps labels from closing the embedded script element.
func Render(contents ...[]byte) ([]byte, error) {
	var (
		definitions = make([]extension.Definition, 0, len(contents))
		page        = viewer
	)
	for _, contents := range contents {
		definition, err := extension.ParseDefinition(contents)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	if err := extension.ValidateDefinitions(definitions); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(definitions)
	if err != nil {
		return nil, fmt.Errorf("encode model definitions: %w", err)
	}
	nativeKinds, err := json.Marshal(extension.NativeNodeKinds())
	if err != nil {
		return nil, fmt.Errorf("encode native node kinds: %w", err)
	}
	page = strings.Replace(page, "__MODEL_JSON__", string(encoded), 1)
	page = strings.Replace(page, "__NATIVE_NODE_KINDS__", string(nativeKinds), 1)
	return []byte(page), nil
}
