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
	"io"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// Scan the helper ID prefix once, then compare two sorted files. The scan must
// include other node kinds too: finding an ID on a native or differently owned
// node is a conflict, not permission to create another node with the same ID.
// We never delete helpers that no longer appear in the desired file, because
// another calculation may still need them.
func publishHelperAnswers(ctx context.Context, transaction Transaction, owner string, binding extension.NodeBinding, desired *answerFile, report *Report) error {
	if desired.rows == 0 {
		// Even an empty answer must pass its completeness/fingerprint checks.
		return desired.each(ctx, func(map[string]any) error { return nil })
	}
	writer, err := desired.space.create()
	if err != nil {
		return err
	}
	defer writer.file.Close()
	parameters := map[string]any{"prefix": binding.IDPrefix}
	if encodedSize(parameters) > desired.space.limits.ParameterBytes {
		return fmt.Errorf("helper prefix exceeds the parameter budget")
	}
	query := "MATCH (item) WHERE item.objectid STARTS WITH $prefix RETURN item.objectid AS id, labels(item) AS kinds, properties(item) AS properties"
	if err := streamWithTimeout(ctx, transaction, desired.space.limits.QueryTimeout, query, parameters, writer.append); err != nil {
		return err
	}
	existing, err := writer.finish()
	if err != nil {
		return err
	}
	existing, err = sortAnswers(ctx, existing, "id")
	if err != nil {
		return err
	}
	reader, err := existing.open()
	if err != nil {
		return err
	}
	defer reader.file.Close()
	var previousExisting string
	next := func() (map[string]any, error) {
		row, err := reader.next()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		identifier := row["id"].(string) // sortAnswers already checked its type.
		if identifier == previousExisting {
			return nil, fmt.Errorf("helper id %q identifies several nodes", identifier)
		}
		previousExisting = identifier
		return row, nil
	}
	current, err := next()
	if err != nil {
		return err
	}
	var previousDesired string
	if err := desired.each(ctx, func(row map[string]any) error {
		identifier := row["id"].(string)
		if identifier == previousDesired {
			return fmt.Errorf("helper ID is invalid or repeated")
		}
		previousDesired = identifier
		for current != nil && current["id"].(string) < identifier {
			current, err = next()
			if err != nil {
				return err
			}
		}
		var matching map[string]any
		if current != nil && current["id"] == identifier {
			matching = current
		}
		changed, err := publishHelper(ctx, transaction, owner, binding, row, matching, desired.space.limits.ParameterBytes)
		if err != nil {
			return err
		}
		report.HelpersChanged += changed
		// Reserve one change for the successful-run record as well.
		if int64(report.HelpersChanged)+1 > desired.space.limits.ChangedObjects {
			return fmt.Errorf("helper changes exceed the %d-change transaction budget", desired.space.limits.ChangedObjects)
		}
		return nil
	}); err != nil {
		return err
	}
	// Drain even unmatched old helpers so late read errors and file-integrity
	// failures still prevent a commit. These unmatched helpers are left alone.
	for current != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err = next()
		if err != nil {
			return err
		}
	}
	return existing.remove()
}
