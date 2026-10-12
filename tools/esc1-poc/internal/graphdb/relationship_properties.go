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

// A null property means the key should be absent. Copy the supplied values so
// preparing a database write never changes the query's answer.
func relationshipCreationProperties(properties map[string]any) map[string]any {
	var copied = make(map[string]any, len(properties))
	for key, value := range properties {
		if value != nil {
			copied[key] = value
		}
	}
	return copied
}

func validateRelationshipUpdates(updates []runner.RelationshipUpdate) error {
	var seen = make(map[int64]bool, len(updates))
	for _, update := range updates {
		if update.ID < 0 {
			return fmt.Errorf("invalid relationship ID")
		}
		if seen[update.ID] {
			return fmt.Errorf("duplicate relationship update ID")
		}
		seen[update.ID] = true
	}
	return nil
}
