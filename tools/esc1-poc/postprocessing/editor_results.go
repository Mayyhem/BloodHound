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

package postprocessing

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// collectEditorResults reads the tested file's complete answer but keeps only
// a preview table for the browser: at most MaxEditorPreviewRows rows and 1 MiB.
// The total row count still describes the complete answer.
func collectEditorResults(ctx context.Context, answer runner.CompleteAnswer, result *EditorTestResult) error {
	var (
		rows         = []map[string]any{}
		previewBytes = 0
	)
	result.TotalRows = answer.Rows()
	err := answer.Each(ctx, func(row map[string]any) error {
		if len(rows) >= MaxEditorPreviewRows {
			return nil
		}
		public := editorJSONRow(row)
		encoded, err := json.Marshal(public)
		if err != nil {
			return err
		}
		if previewBytes+len(encoded) > 1<<20 {
			return nil
		}
		previewBytes += len(encoded)
		rows = append(rows, public)
		return nil
	})
	if err != nil {
		return err
	}
	result.Rows = rows
	result.Truncated = int64(len(rows)) < result.TotalRows
	return nil
}

// Browsers lose precision on whole numbers beyond 2^53, so very large numbers
// are sent as text. Nested lists and maps are converted the same way.
func editorJSONRow(row map[string]any) map[string]any {
	result := map[string]any{}
	for column, value := range row {
		result[column] = editorJSONValue(value)
	}
	return result
}
func editorJSONValue(value any) any {
	switch item := value.(type) {
	case int64:
		if item > 9007199254740991 || item < -9007199254740991 {
			return strconv.FormatInt(item, 10)
		}
	case map[string]any:
		return editorJSONRow(item)
	case []any:
		result := make([]any, len(item))
		for index, value := range item {
			result[index] = editorJSONValue(value)
		}
		return result
	}
	return value
}
