// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"fmt"
	"sort"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func sortedAnswerNames(files map[string]*answerFile) []string {
	var names = make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Combine complete answers in filename order. When two rules describe the
// same connection, later filenames override only the property keys they supply.
// Sorting on disk keeps the number of connections from dictating memory use.
func combineRelationshipAnswers(ctx context.Context, space *answerSpace, files map[string]*answerFile) (*answerFile, error) {
	var (
		previous Pair
		combined map[string]any
	)
	// A single producer already has a checked, sorted answer. Reuse it.
	if len(files) == 1 {
		for _, file := range files {
			return file, nil
		}
	}
	writer, err := space.create()
	if err != nil {
		return nil, err
	}
	defer writer.file.Close()
	for _, name := range sortedAnswerNames(files) {
		file := files[name]
		if err := file.each(ctx, func(row map[string]any) error {
			row["query"] = name
			return writer.append(row)
		}); err != nil {
			return nil, err
		}
		if err := file.remove(); err != nil {
			return nil, err
		}
	}
	input, err := writer.finish()
	if err != nil {
		return nil, err
	}
	sorted, err := sortAnswers(ctx, input, "source", "target", "query")
	if err != nil {
		return nil, err
	}
	output, err := space.create()
	if err != nil {
		return nil, err
	}
	defer output.file.Close()
	flush := func() error {
		if combined == nil {
			return nil
		}
		return output.append(map[string]any{"source": previous.Source, "target": previous.Target, "properties": combined})
	}
	if err := sorted.each(ctx, func(row map[string]any) error {
		pair := pairFromRow(row)
		if pair != previous {
			if err := flush(); err != nil {
				return err
			}
			previous = pair
			combined = map[string]any{}
		}
		for key, value := range row["properties"].(map[string]any) {
			combined[key] = value
		}
		remaining := space.limits.RowBytes
		if !fitsRowBudget(combined, &remaining) {
			return fmt.Errorf("combined relationship properties exceed the row limit")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if err := sorted.remove(); err != nil {
		return nil, err
	}
	return output.finish()
}

// Saved removal history keeps the old endpoint pairs even if the author also
// renames the type in the schema. Clear only that previously calculated scope.
func retiredBinding(plan extension.Plan, name string) extension.RelationshipBinding {
	var binding = extension.RelationshipBinding{Kind: name}
	if endpoints, found := plan.RetiredEndpoints[name]; found {
		binding.Endpoints = endpoints
		return binding
	}
	for _, definition := range plan.Definitions {
		for _, kind := range definition.RelationshipKinds {
			if kind.Name == name {
				binding.Endpoints = kind.CompatibleKinds
			}
		}
	}
	return binding
}
