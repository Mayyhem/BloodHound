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

import "fmt"

// SelectPrerequisites returns the requested queries and everything they need
// to run. It follows waits from a query to its prerequisites, so it does not add
// queries that merely consume the selected query's results.
func (s Plan) SelectPrerequisites(requested []string) ([]string, error) {
	var (
		queriesByName = make(map[string]Query, len(s.Queries))
		selected      = make(map[string]bool)
	)
	for _, query := range s.Queries {
		queriesByName[query.Name] = query
	}

	var includePrerequisites func(string) error
	includePrerequisites = func(name string) error {
		if selected[name] {
			return nil
		}
		query, exists := queriesByName[name]
		if !exists {
			return fmt.Errorf("required query %q is not in the execution plan", name)
		}
		selected[name] = true
		for _, dependency := range s.ExecutionDependencies(query) {
			if err := includePrerequisites(dependency.Prerequisite); err != nil {
				return fmt.Errorf("query %q has an incomplete prerequisite: %w", name, err)
			}
		}
		return nil
	}
	for _, name := range requested {
		if _, exists := queriesByName[name]; !exists {
			return nil, fmt.Errorf("query %q is not in the execution plan", name)
		}
		if err := includePrerequisites(name); err != nil {
			return nil, err
		}
	}

	var result []string
	for _, query := range s.Queries {
		if selected[query.Name] {
			result = append(result, query.Name)
		}
	}
	return result, nil
}
