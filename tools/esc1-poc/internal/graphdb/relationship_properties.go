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
package graphdb

import (
	"fmt"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func relationshipCreationProperties(owner string, properties map[string]any) (map[string]any, error) {
	var copied = make(map[string]any, len(properties)+1)
	if owner == "" {
		return nil, fmt.Errorf("relationship owner is required")
	}
	for key, value := range properties {
		if key == "poc_owner" {
			return nil, fmt.Errorf("relationship ownership cannot be supplied as an ordinary property")
		}
		if value != nil {
			copied[key] = value
		}
	}
	copied["poc_owner"] = owner
	return copied, nil
}

func validateRelationshipUpdates(owner string, updates []runner.RelationshipUpdate) error {
	var seen = make(map[int64]bool, len(updates))
	if owner == "" {
		return fmt.Errorf("relationship owner is required")
	}
	for _, update := range updates {
		if update.ID < 0 {
			return fmt.Errorf("invalid relationship ID")
		}
		if seen[update.ID] {
			return fmt.Errorf("duplicate relationship update ID")
		}
		if _, found := update.Properties["poc_owner"]; found {
			return fmt.Errorf("relationship ownership cannot be modified")
		}
		seen[update.ID] = true
	}
	return nil
}
