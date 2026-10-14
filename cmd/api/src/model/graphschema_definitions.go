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

package model

import "encoding/json"

// An absent definition means the extension predates definition preservation.
// Its schema can be uploaded again through the existing management page.
type InstalledGraphDefinition struct {
	ID         int32           `json:"id"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace"`
	Version    string          `json:"version"`
	Definition json.RawMessage `json:"definition"`
}

type GraphDefinitionsResponse struct {
	Definitions []InstalledGraphDefinition `json:"definitions"`
}

func (s GraphSchemaExtensions) DefinitionsResponse() GraphDefinitionsResponse {
	var response = GraphDefinitionsResponse{Definitions: []InstalledGraphDefinition{}}
	for _, extension := range s {
		if !extension.IsBuiltin {
			response.Definitions = append(response.Definitions, InstalledGraphDefinition{
				ID: extension.ID, Name: extension.Name, Namespace: extension.Namespace,
				Version: extension.Version, Definition: extension.Definition,
			})
		}
	}
	return response
}
