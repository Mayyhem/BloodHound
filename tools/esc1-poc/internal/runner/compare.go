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
	"encoding/json"
)

// Reports carry examples, not another complete copy of the results. Limit
// both their count and their encoded size: a few very long IDs can otherwise
// produce an enormous report even when the pair-count limit is small.
const maxReportSampleBytes int64 = 1 << 20

func appendReportPair(sample *[]Pair, pair Pair, countLimit int, remainingBytes *int64) {
	if len(*sample) >= countLimit {
		return
	}
	encoded, _ := json.Marshal(pair) // Pair contains only strings.
	bytes := int64(len(encoded) + 1) // Include the comma between examples.
	if bytes > *remainingBytes {
		return
	}
	*remainingBytes -= bytes
	*sample = append(*sample, pair)
}

type Comparison struct {
	MissingCount int64  `json:"missing_count"`
	ExtraCount   int64  `json:"extra_count"`
	Truncated    bool   `json:"truncated"`
	Missing      []Pair `json:"missing"`
	Extra        []Pair `json:"extra"`
}

// ComparePublished checks saved results without collecting either complete
// list in memory. It uses the same temporary-file limits as calculation. The
// caller keeps other writers stopped and rolls back this read transaction.
func ComparePublished(ctx context.Context, transaction Transaction, owner, temporaryDirectory string, limits Limits) (Comparison, error) {
	limits, err := limits.checked()
	if err != nil {
		return Comparison{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, limits.RunTimeout)
	defer cancel()
	space, err := newAnswerSpace(temporaryDirectory, limits)
	if err != nil {
		return Comparison{}, err
	}
	defer space.close()
	writer, err := space.create()
	if err != nil {
		return Comparison{}, err
	}
	defer writer.file.Close()
	query := "MATCH (source)-[edge:PoC_ADCSESC1]->(target) WHERE edge.poc_owner = $owner RETURN DISTINCT source.objectid AS source, target.objectid AS target"
	if err := streamWithTimeout(ctx, transaction, limits.QueryTimeout, query, map[string]any{"owner": producer(owner, "PoC_ADCSESC1")}, writer.append); err != nil {
		return Comparison{}, err
	}
	raw, err := writer.finish()
	if err != nil {
		return Comparison{}, err
	}
	desired, err := checkedPairs(ctx, raw)
	if err != nil {
		return Comparison{}, err
	}
	if err := raw.remove(); err != nil {
		return Comparison{}, err
	}
	return compareFiles(ctx, transaction, desired)
}

// Compare is also useful when testing against a small, written expected list.
func Compare(expected, actual []Pair) Comparison {
	var (
		comparison  = Comparison{Missing: []Pair{}, Extra: []Pair{}}
		expectedSet = map[Pair]bool{}
		actualSet   = map[Pair]bool{}
	)
	for _, pair := range expected {
		expectedSet[pair] = true
	}
	for _, pair := range actual {
		actualSet[pair] = true
	}
	for pair := range expectedSet {
		if !actualSet[pair] {
			comparison.Missing = append(comparison.Missing, pair)
		}
	}
	for pair := range actualSet {
		if !expectedSet[pair] {
			comparison.Extra = append(comparison.Extra, pair)
		}
	}
	comparison.MissingCount = int64(len(comparison.Missing))
	comparison.ExtraCount = int64(len(comparison.Extra))
	sortPairs(comparison.Missing)
	sortPairs(comparison.Extra)
	return comparison
}
