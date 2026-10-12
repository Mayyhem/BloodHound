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
	"syscall"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// This opt-in benchmark measures local calculation and storage. It has no
// database connection, ingestion, native comparison, or publication transaction.
type scaleTransaction struct {
	existing  int
	diamonds  int
	reads     int
	edgeReads int
}

func (s *scaleTransaction) Read(context.Context, string, map[string]any) (Rows, error) {
	return nil, fmt.Errorf("benchmark requires streaming reads")
}
func (s *scaleTransaction) Write(context.Context, string, map[string]any) (Rows, error) {
	return nil, fmt.Errorf("benchmark cannot publish")
}
func (s *scaleTransaction) Commit(context.Context) error {
	return fmt.Errorf("benchmark cannot commit")
}
func (s *scaleTransaction) Rollback(context.Context) error { return nil }
func scaleNodeID(index int64) string                       { return fmt.Sprintf("NODE-%08d", index) }
func (s *scaleTransaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	s.reads++
	if s.existing > 0 {
		// 75,000 overlap, 25,000 obsolete, and 25,000 desired additions.
		for index := s.existing + 24999; index >= 25000; index-- {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := consume(map[string]any{"id": int64(index), "owner": "scale/PoC_Scale", "source": scaleNodeID(int64(index)), "target": "DOMAIN"}); err != nil {
				return err
			}
		}
		return nil
	}
	if query == "seeds" {
		return consume(map[string]any{"id": scaleNodeID(0), "graph_id": int64(0), "kinds": []string{"Group"}})
	}
	for _, source := range parameters["_reach_frontier"].([]int64) {
		var targets []int64
		if source == 0 {
			for index := 1; index <= s.diamonds*2; index++ {
				targets = append(targets, int64(index))
			}
		} else if source <= int64(s.diamonds*2) {
			targets = []int64{int64(s.diamonds*2 + 1)}
		}
		for _, target := range targets {
			if err := ctx.Err(); err != nil {
				return err
			}
			s.edgeReads++
			if err := consume(map[string]any{"source": scaleNodeID(source), "target": scaleNodeID(target), "graph_id": target, "kinds": []string{"Group"}}); err != nil {
				return err
			}
		}
	}
	return nil
}
func reportScaleResources(b *testing.B, space *answerSpace, started time.Time) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(space.peak), "peak-temp-B")
	b.ReportMetric(float64(usage.Maxrss), "process-peak-KiB")
	b.Logf("elapsed=%s temporary_peak_bytes=%d process_peak_KiB=%d limits: sort_bytes=%d disk_bytes=%d parameter_bytes=%d row_bytes=%d batch_rows=%d", time.Since(started), space.peak, usage.Maxrss, space.limits.SortBytes, space.limits.DiskBytes, space.limits.ParameterBytes, space.limits.RowBytes, space.limits.BatchRows)
}
func benchmarkSpace(b *testing.B) *answerSpace {
	var limits = DefaultLimits()
	limits.DiskBytes = 256 << 20
	limits.SortBytes = 8 << 20
	limits.RowBytes = 1 << 20
	space, err := newAnswerSpace(b.TempDir(), limits)
	if err != nil {
		b.Fatal(err)
	}
	return space
}

func BenchmarkStreamSortAndReconcile100000Pairs(b *testing.B) {
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkStreamSortAndReconcile100000Pairs(b)
	}
}

func benchmarkStreamSortAndReconcile100000Pairs(b *testing.B) {
	var (
		space       = benchmarkSpace(b)
		started     = time.Now()
		transaction = &scaleTransaction{existing: 100000}
	)
	defer space.close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	writer, err := space.create()
	if err != nil {
		b.Fatal(err)
	}
	defer writer.file.Close()
	for index := 99999; index >= 0; index-- {
		row := map[string]any{"source": scaleNodeID(int64(index)), "target": "DOMAIN"}
		if err := writer.append(row); err != nil {
			b.Fatal(err)
		}
		if index%100 == 0 {
			if err := writer.append(row); err != nil {
				b.Fatal(err)
			}
		}
	}
	input, err := writer.finish()
	if err != nil {
		b.Fatal(err)
	}
	desired, err := checkedPairs(ctx, input)
	if err != nil {
		b.Fatal(err)
	}
	if desired.rows != 100000 {
		b.Fatalf("incomplete desired result: %d", desired.rows)
	}
	b.Logf("complete desired sort: input_rows=%d deduplicated_pairs=%d elapsed=%s", input.rows, desired.rows, time.Since(started))
	changes, err := prepareChanges(ctx, transaction, "scale", extension.RelationshipBinding{ExistingEdgePolicy: extension.KeepExisting, Kind: "PoC_Scale"}, desired)
	if err != nil {
		reportScaleResources(b, space, started)
		b.Fatal(err)
	}
	if changes.additions.rows != 25000 || changes.removals.rows != 25000 || changes.unchanged != 75000 {
		b.Fatalf("wrong reconciliation: add=%d remove=%d unchanged=%d", changes.additions.rows, changes.removals.rows, changes.unchanged)
	}
	b.ReportMetric(float64(desired.rows), "desired-pairs")
	b.Logf("input_rows=%d desired_pairs=%d existing_pairs=100000 additions=%d removals=%d unchanged=%d", input.rows, desired.rows, changes.additions.rows, changes.removals.rows, changes.unchanged)
	reportScaleResources(b, space, started)
}

func BenchmarkSharedDiamondReach10000(b *testing.B) {
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkSharedDiamondReach10000(b)
	}
}

func benchmarkSharedDiamondReach10000(b *testing.B) {
	var (
		space       = benchmarkSpace(b)
		transaction = &scaleTransaction{diamonds: 10000}
		started     = time.Now()
		query       = extension.Query{Text: "seeds", Reach: &extension.Reachability{Direction: "incoming", Relationships: []string{"MemberOf"}, RejectCycles: true}}
	)
	// All 10,000 diamonds share one root and terminal member. Converging
	// membership edges must not reread the shared terminal 20,000 times.
	defer space.close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	answer, err := calculateReach(ctx, transaction, query, nil, space)
	if err != nil {
		b.Fatal(err)
	}
	rows, _, err := answer.load(ctx, space.limits.ParameterBytes)
	if err != nil {
		b.Fatal(err)
	}
	reach, ok := stringsFrom(rows[0]["reach"])
	if answer.rows != 1 || !ok || len(reach) != 20001 || transaction.edgeReads != 40000 || transaction.reads != 43 {
		b.Fatalf("incomplete reach: rows=%d reachable=%d edges=%d reads=%d", answer.rows, len(reach), transaction.edgeReads, transaction.reads)
	}
	b.ReportMetric(float64(len(reach)), "reachable-nodes")
	b.Logf("diamonds=10000 nodes=20002 edges_read=%d streaming_queries=%d output_rows=%d root_reachable=%d", transaction.edgeReads, transaction.reads, answer.rows, len(reach))
	reportScaleResources(b, space, started)
}
