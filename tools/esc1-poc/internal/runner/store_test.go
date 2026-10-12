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
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func TestOversizedRowIsRejectedBeforeMakingAnEncodedCopy(t *testing.T) {
	var space = testSpace(t)
	space.limits.RowBytes = 500
	writer, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.file.Close()
	if err := writer.append(map[string]any{"large": strings.Repeat("x", 1000)}); err == nil {
		t.Fatal("oversized value was accepted")
	}
	if writer.rowBuffer.Cap() != 0 || writer.answer.rows != 0 {
		t.Fatal("oversized value was copied or counted before rejection")
	}
}

func testSpace(t *testing.T) *answerSpace {
	t.Helper()
	space, err := newAnswerSpace(t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(space.close)
	return space
}

func TestAnswersPreserveExactValues(t *testing.T) {
	var space = testSpace(t)
	// Later rows introduce types and keys the decoder has not seen yet. A
	// reused decoder must preserve those values without retaining earlier keys.
	var wanted = Rows{
		{"first": "simple"},
		{"large_id": int64(9007199254740993), "whole_decimal": float64(1), "fraction": 1.5, "typed_list": []string{"A", "B"}, "mixed": []any{int64(1), float64(2), map[string]any{"yes": true}}, "null": nil},
		{"last": int(7), "different_list": []any{"text", false}},
		{"empty_list": []any{}, "empty_text_list": []string{}, "nested": map[string]any{"empty_list": []any{}, "empty_map": map[string]any{}}, "lists": []any{[]any{}, []string{}}},
	}
	writer, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range wanted {
		if err := writer.append(row); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := file.load(t.Context(), space.limits.ParameterBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, wanted) {
		t.Fatalf("temporary storage changed the answer: %#v", rows)
	}
}

func TestLoadingPrerequisitesChecksExpandedSizeAndCancellation(t *testing.T) {
	space := testSpace(t)
	writer, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.append(map[string]any{"escaped": strings.Repeat("\x00", 100)}); err != nil {
		t.Fatal(err)
	}
	file, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	if file.bytes >= 500 {
		t.Fatal("fixture must fit on disk while exceeding its JSON allowance")
	}
	if _, _, err := file.load(t.Context(), 500); err == nil || !strings.Contains(err.Error(), "parameter budget") {
		t.Fatalf("expanded parameter escaped the limit: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := file.load(ctx, space.limits.ParameterBytes); err != context.Canceled {
		t.Fatalf("canceled prerequisite still loaded: %v", err)
	}
}

func TestPublicationBatchesBoundBytesAsWellAsRowCounts(t *testing.T) {
	var (
		space             = testSpace(t)
		consumed, batches int
	)
	space.limits.RowBytes = 500
	space.limits.ParameterBytes = 500
	writer, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if err := writer.append(map[string]any{"source": fmt.Sprintf("%d", index) + strings.Repeat("\"", 100), "target": "D"}); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.batches(t.Context(), 0, func(rows Rows) error {
		if encodedSize(rows) > space.limits.ParameterBytes {
			t.Fatal("long IDs exceeded a batch's byte allowance")
		}
		batches++
		consumed += len(rows)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if consumed != 5 || batches < 2 {
		t.Fatalf("batching lost rows or ignored the byte allowance: rows=%d batches=%d", consumed, batches)
	}
}

func TestTruncatedAnswersNeverLookComplete(t *testing.T) {
	for _, damage := range []string{"empty file", "missing body", "partial final row", "extra data", "changed valid string"} {
		t.Run(damage, func(t *testing.T) {
			space := testSpace(t)
			writer, err := space.create()
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.append(map[string]any{"source": "A", "target": "B"}); err != nil {
				t.Fatal(err)
			}
			file, err := writer.finish()
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "empty file":
				err = os.Truncate(file.path, 0)
			case "missing body":
				err = os.Truncate(file.path, 8)
			case "partial final row":
				err = os.Truncate(file.path, file.bytes-1)
			case "changed valid string":
				contents, readError := os.ReadFile(file.path)
				if readError != nil {
					t.Fatal(readError)
				}
				position := bytes.LastIndexByte(contents, 'A')
				if position < 0 {
					t.Fatal("fixture string is absent from its encoded row")
				}
				contents[position] = 'Z'
				err = os.WriteFile(file.path, contents, 0600)
			case "extra data":
				output, openError := os.OpenFile(file.path, os.O_APPEND|os.O_WRONLY, 0600)
				if openError != nil {
					t.Fatal(openError)
				}
				_, err = output.Write([]byte{1})
				_ = output.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := file.each(context.Background(), func(map[string]any) error { return nil }); err == nil {
				t.Fatal("damaged answer was accepted as complete")
			}
		})
	}
}

func TestExternalSortAndDedupAcrossSeveralMergePasses(t *testing.T) {
	var space = testSpace(t)
	space.limits.SortBytes = 1024
	writer, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	for index := 200; index >= 0; index-- {
		row := map[string]any{"source": fmt.Sprintf("%06d", index), "target": "D"}
		if err := writer.append(row); err != nil {
			t.Fatal(err)
		}
		if err := writer.append(row); err != nil {
			t.Fatal(err)
		}
	}
	input, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	output, err := checkedPairs(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := output.each(context.Background(), func(row map[string]any) error {
		if row["source"] != fmt.Sprintf("%06d", count) {
			return fmt.Errorf("wrong ordered answer at %d: %v", count, row)
		}
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 201 || output.rows != 201 {
		t.Fatalf("lost or duplicated pairs: %d", count)
	}
	files, err := os.ReadDir(space.directory)
	if err != nil {
		t.Fatal(err)
	}
	// Only the caller's original and the complete, deduplicated result remain.
	if len(files) != 2 {
		t.Fatalf("temporary merge files leaked: %d", len(files))
	}
}

func TestDiskBudgetIsSharedAndReleased(t *testing.T) {
	var space = testSpace(t)
	first, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.append(map[string]any{"value": "one"}); err != nil {
		t.Fatal(err)
	}
	complete, err := first.finish()
	if err != nil {
		t.Fatal(err)
	}
	space.limits.DiskBytes = complete.bytes
	second, err := space.create()
	if err != nil {
		t.Fatal(err)
	}
	defer second.file.Close()
	if err := second.append(map[string]any{"value": "two"}); err == nil {
		t.Fatal("another file independently spent the same disk budget")
	}
	if err := complete.remove(); err != nil {
		t.Fatal(err)
	}
	if err := second.append(map[string]any{"value": "two"}); err != nil {
		t.Fatalf("removed file did not release budget: %v", err)
	}
}

func TestPartialStreamDoesNotProduceACompleteFile(t *testing.T) {
	var space = testSpace(t)
	var transaction = failingStream{}
	_, err := calculateQuery(context.Background(), transaction, extension.Query{Name: "result.cypher", Text: "RETURN 1 AS value"}, nil, space)
	if err == nil {
		t.Fatal("late query error accepted as a complete answer")
	}
}

type failingStream struct{}

func (s failingStream) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := consume(map[string]any{"source": "A", "target": "B"}); err != nil {
		return err
	}
	return fmt.Errorf("connection lost after first row")
}
func (s failingStream) Read(context.Context, string, map[string]any) (Rows, error) {
	panic("must stream")
}
func (s failingStream) Write(context.Context, string, map[string]any) (Rows, error) {
	panic("must not write")
}
func (s failingStream) Commit(context.Context) error   { panic("must not commit") }
func (s failingStream) Rollback(context.Context) error { return nil }
