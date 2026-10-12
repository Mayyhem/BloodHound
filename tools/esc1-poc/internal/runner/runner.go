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

// Package runner calculates experimental results and publishes them together.
// It knows nothing about HTTP uploads or command-line flags. This keeps the
// rules that protect existing graph data together in one small package.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// ErrCommitUncertain means the database did not confirm publication. Callers
// must inspect the graph before allowing another writer to retry the run.
var ErrCommitUncertain = errors.New("graph commit result is uncertain")

// ErrCommitRolledBack means the database confirmed rollback instead of commit.
// Existing graph data remains unchanged and other writers may continue.
var ErrCommitRolledBack = errors.New("graph commit was rolled back")

// Rows is a query's table of answers. Each map is one row, with column names as
// keys. For example, a relationship row has "source" and "target" keys.
type Rows = []map[string]any

// Transaction is a group of database changes that succeed or fail together.
// Read must reject a query that could change the graph, even though this same
// transaction also permits the runner's own carefully limited writes.
type Transaction interface {
	Read(context.Context, string, map[string]any) (Rows, error)
	Write(context.Context, string, map[string]any) (Rows, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type Database interface {
	Begin(context.Context) (Transaction, error)
}

// Changes describes confirmed changes in the graph, not just types found in an
// upload. Deletions count as changes too. CoverageKnown must be false whenever
// native processing might have made additional changes we did not observe.
type Changes struct {
	CoverageKnown bool     `json:"coverage_known"`
	Nodes         []string `json:"nodes"`
	Relationships []string `json:"relationships"`
}

type Options struct {
	BatchID            string
	IngestionComplete  bool
	WritersPaused      bool
	Changes            Changes
	Limits             Limits
	TemporaryDirectory string
}

// Report describes committed work. On an error, Committed remains false and
// the caller must say that previously published results may be out of date.
type Report struct {
	ContainerImages     map[string]string `json:"container_images,omitempty"`
	BatchID             string            `json:"batch_id"`
	Committed           bool              `json:"committed"`
	Skipped             bool              `json:"skipped"`
	Queries             []string          `json:"queries"`
	QueryRows           map[string]int    `json:"query_rows"`
	Desired             map[string][]Pair `json:"desired_relationships"`
	DesiredCounts       map[string]int64  `json:"desired_counts"`
	DesiredTruncated    bool              `json:"desired_relationships_truncated"`
	TemporaryPeakBytes  int64             `json:"temporary_peak_bytes"`
	Added               int               `json:"relationships_added"`
	Removed             int               `json:"relationships_removed"`
	Updated             int               `json:"relationships_updated"`
	Unchanged           int               `json:"relationships_unchanged"`
	HelpersChanged      int               `json:"helpers_changed"`
	GraphObjectsChanged int               `json:"graph_objects_changed"`
	NativeComparison    *Comparison       `json:"native_comparison,omitempty"`
	Elapsed             time.Duration     `json:"elapsed_nanoseconds"`
}

// Run finishes and checks every calculation before publishing changes. Reads
// may run together; only this coordinator writes and commits the final result.
func Run(ctx context.Context, database Database, plan extension.Plan, options Options) (Report, error) {
	var (
		started = time.Now()
		report  = Report{BatchID: options.BatchID, QueryRows: map[string]int{}, Desired: map[string][]Pair{}, DesiredCounts: map[string]int64{}}
	)
	if options.BatchID == "" || !options.IngestionComplete {
		return report, fmt.Errorf("a successfully completed ingestion batch is required; existing results were kept")
	}
	if !options.WritersPaused {
		return report, fmt.Errorf("other graph writers must be paused before calculation; existing results were kept")
	}
	limits, err := options.Limits.checked()
	if err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(ctx, limits.RunTimeout)
	defer cancel()
	space, err := newAnswerSpace(options.TemporaryDirectory, limits)
	if err != nil {
		return report, err
	}
	defer space.close()
	if err := prepareDatabase(ctx, database, plan); err != nil {
		return report, err
	}
	transaction, err := database.Begin(ctx)
	if err != nil {
		return report, err
	}
	// Rollback is harmless after a successful commit. Use a fresh, short-lived
	// context so a canceled run still gets a chance to release its transaction.
	defer rollback(transaction)

	// The saved hash always describes the WHOLE plan, even when this run only
	// checks part of it. A first run or any edit must establish a new baseline.
	planHash := fingerprint(plan)
	plan = runnablePlan(plan)
	if options.Changes.CoverageKnown {
		previous, err := transaction.Read(ctx, "MATCH (run:PoC_Run {objectid: $id, poc_owner: $owner}) RETURN run.plan_hash AS plan_hash", map[string]any{"id": stateID(plan), "owner": plan.Schema.Owner})
		if err != nil {
			return report, err
		}
		if len(previous) == 1 && previous[0]["plan_hash"] == planHash {
			plan = affectedPlan(plan, options.Changes)
			if len(plan.Queries) == 0 {
				report.Skipped = true
				report.Elapsed = time.Since(started)
				return report, nil
			}
		}
	}
	// Missing IDs in an intermediate query can disappear from a collected list.
	// Check them before that happens, so malformed evidence cannot look like a
	// valid reason to remove an old connection. Duplicate IDs are ambiguous too.
	// Native BloodHound also keeps an isolated MigrationData node with version
	// numbers and no objectid. It is bookkeeping, not collected evidence. Exempt
	// only that exact standalone kind; any connection makes its identity required.
	invalidIdentities, err := transaction.Read(ctx, "MATCH (item) WHERE NOT (labels(item) = ['MigrationData'] AND NOT (item)--()) WITH item.objectid AS objectid, count(item) AS matches WHERE objectid IS NULL OR trim(objectid) = '' OR matches <> 1 RETURN objectid LIMIT 1", nil)
	if err != nil {
		return report, fmt.Errorf("check graph identities: %w", err)
	}
	if len(invalidIdentities) != 0 {
		return report, fmt.Errorf("graph contains missing, empty, or duplicate objectids; existing results were kept")
	}

	desiredByKind, err := calculate(ctx, database, transaction, plan, space, &report)
	if err != nil {
		return report, err
	}
	if plan.Schema.CompareNativeESC1 {
		comparison, err := compareFiles(ctx, transaction, desiredByKind["PoC_ADCSESC1"])
		report.NativeComparison = &comparison
		if err != nil {
			return report, err
		}
		if comparison.MissingCount != 0 || comparison.ExtraCount != 0 {
			return report, fmt.Errorf("experimental ESC1 differs from native ESC1: %d missing and %d extra connections; all changes were rolled back", comparison.MissingCount, comparison.ExtraCount)
		}
	}
	// An explicit rule removal means this owner now wants no connections of
	// that kind. Use the normal bounded comparison and ownership checks, with
	// an empty answer. Nothing is deleted unless every remaining rule succeeds.
	for _, kind := range plan.RetiredRelationships {
		writer, err := space.create()
		if err != nil {
			return report, err
		}
		empty, err := writer.finish()
		if err != nil {
			return report, err
		}
		desiredByKind[kind] = empty
		plan.Schema.Relationships = append(plan.Schema.Relationships, extension.RelationshipBinding{Kind: kind, Owner: plan.RetiredOwners[kind]})
		report.DesiredCounts[kind] = 0
		report.Desired[kind] = []Pair{}
	}
	var changes []relationshipChanges
	var changed = int64(report.HelpersChanged) + 1 // Include the saved run record.
	for _, binding := range plan.Schema.Relationships {
		prepared, err := prepareChanges(ctx, transaction, binding.EffectiveOwner(plan.Schema.Owner), binding, desiredByKind[binding.Kind])
		if err != nil {
			return report, err
		}
		changed += prepared.additions.rows + prepared.removals.rows + prepared.updates.rows
		if changed > limits.ChangedObjects {
			return report, fmt.Errorf("combined graph changes exceed the %d-change transaction budget", limits.ChangedObjects)
		}
		changes = append(changes, prepared)
	}
	// These batches all belong to ONE transaction. A late failure, cancellation,
	// or ownership conflict rolls back every earlier batch and helper change.
	for _, change := range changes {
		if err := applyChanges(ctx, transaction, change.binding.EffectiveOwner(plan.Schema.Owner), change); err != nil {
			return report, err
		}
		report.Added += int(change.additions.rows)
		report.Removed += int(change.removals.rows)
		report.Updated += int(change.updates.rows)
		report.Unchanged += change.unchanged
	}
	report.TemporaryPeakBytes = space.peak

	if err := saveRun(ctx, transaction, plan, options.BatchID, planHash); err != nil {
		return report, err
	}
	if err := transaction.Commit(ctx); err != nil {
		if errors.Is(err, ErrCommitRolledBack) {
			return report, err
		}
		return report, fmt.Errorf("%w: %w; inspect graph ownership and the saved batch ID before retrying", ErrCommitUncertain, err)
	}
	report.Committed = true
	report.GraphObjectsChanged = report.Added + report.Removed + report.Updated + report.HelpersChanged + 1
	report.Elapsed = time.Since(started)
	return report, nil
}

// Preparing type names and lookup indexes does not create graph evidence.
// PostgreSQL needs a kind registered even when that kind currently has no
// objects. Neo4j needs indexes for the input and endpoint lookups. These small
// catalog changes may remain after failure; all node and edge changes still
// belong to the single publication transaction below them.
func prepareDatabase(ctx context.Context, database Database, plan extension.Plan) error {
	var (
		nodeKinds = map[string]bool{}
		allKinds  = map[string]bool{"PoC_Run": true, "MigrationData": true}
	)
	for _, kind := range plan.RetiredRelationships {
		allKinds[kind] = true
	}
	for _, binding := range plan.Schema.Nodes {
		allKinds[binding.Kind] = true
		nodeKinds[binding.Kind] = true
	}
	for _, binding := range plan.Schema.Relationships {
		allKinds[binding.Kind] = true
		for _, endpoint := range binding.Endpoints {
			nodeKinds[endpoint.SourceKind] = true
			nodeKinds[endpoint.TargetKind] = true
		}
	}
	for _, query := range plan.Queries {
		for _, dependency := range query.Dependencies {
			switch dependency.Type {
			case "node":
				nodeKinds[dependency.Name] = true
			case "relationship":
				allKinds[dependency.Name] = true
			}
		}
		if query.Reach != nil {
			for _, kind := range query.Reach.NodeKinds {
				nodeKinds[kind] = true
			}
			for _, kind := range query.Reach.Relationships {
				allKinds[kind] = true
			}
		}
	}
	for kind := range nodeKinds {
		allKinds[kind] = true
	}
	if plan.Schema.CompareNativeESC1 {
		allKinds["ADCSESC1"] = true
	}
	if preparer, ok := database.(interface {
		PrepareKinds(context.Context, []string) error
	}); ok {
		if err := preparer.PrepareKinds(ctx, sortedNames(allKinds)); err != nil {
			return fmt.Errorf("prepare declared graph kinds: %w", err)
		}
	}
	if preparer, ok := database.(interface {
		PrepareEndpoints(context.Context, []string) error
	}); ok {
		if err := preparer.PrepareEndpoints(ctx, sortedNames(nodeKinds)); err != nil {
			return fmt.Errorf("prepare endpoint lookups: %w", err)
		}
	}
	return nil
}

func sortedNames(names map[string]bool) []string {
	var result = make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func fingerprint(plan extension.Plan) string {
	var encoded, _ = json.Marshal(plan)
	var digest = sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Disabled rules remain in the saved plan, but neither their calculations nor
// their output bindings enter publication. A missing calculation is not an
// empty answer: its existing connections must remain exactly as they were.
func runnablePlan(plan extension.Plan) extension.Plan {
	var names = map[string]bool{}
	for _, query := range plan.Queries {
		if query.DisabledReason == "" {
			names[query.Name] = true
		}
	}
	return keepQueries(plan, names)
}

func keepQueries(plan extension.Plan, names map[string]bool) extension.Plan {
	var result = plan
	result.Queries = nil
	result.Schema.Nodes = nil
	result.Schema.Relationships = nil
	result.Schema.CompareNativeESC1 = false
	for _, query := range plan.Queries {
		if names[query.Name] {
			result.Queries = append(result.Queries, query)
		}
	}
	for _, binding := range plan.Schema.Nodes {
		if names[binding.Query] {
			result.Schema.Nodes = append(result.Schema.Nodes, binding)
		}
	}
	for _, binding := range plan.Schema.Relationships {
		if names[binding.Query] {
			result.Schema.Relationships = append(result.Schema.Relationships, binding)
			if binding.Kind == "PoC_ADCSESC1" {
				result.Schema.CompareNativeESC1 = plan.Schema.CompareNativeESC1
			}
		}
	}
	return result
}

// Keep the selected queries AND only their output bindings. Keeping a skipped
// binding would mistake its missing answer for an empty answer and delete its
// existing connections. Selected queries still calculate their complete answer;
// we are choosing whole calculations, not slices of their results.
func affectedPlan(plan extension.Plan, changes Changes) extension.Plan {
	var names = map[string]bool{}
	for _, name := range plan.Affected(changes.Nodes, changes.Relationships, true) {
		names[name] = true
	}
	return keepQueries(plan, names)
}

func stateID(plan extension.Plan) string {
	return "PoC_Run_" + plan.Schema.Name
}

func saveRun(ctx context.Context, transaction Transaction, plan extension.Plan, batchID, planHash string) error {
	var parameters = map[string]any{"id": stateID(plan), "owner": plan.Schema.Owner, "batch": batchID, "hash": planHash, "completed": time.Now().UTC().Format(time.RFC3339Nano), "reference": plan.Schema.ReferenceCommit}
	rows, err := transaction.Read(ctx, "MATCH (item {objectid: $id}) RETURN labels(item) AS kinds, item.poc_owner AS owner", parameters)
	if err != nil {
		return err
	}
	if len(rows) > 1 || len(rows) == 1 && (!onlyKind(rows[0]["kinds"], "PoC_Run") || rows[0]["owner"] != plan.Schema.Owner) {
		return fmt.Errorf("saved-run identity is already used by another object")
	}
	if publisher, ok := transaction.(Publisher); ok {
		return publisher.PutNode(ctx, "PoC_Run", map[string]any{"objectid": parameters["id"], "poc_owner": parameters["owner"], "batch_id": parameters["batch"], "plan_hash": parameters["hash"], "completed_at": parameters["completed"], "reference_commit": parameters["reference"]})
	}
	_, err = transaction.Write(ctx, "MERGE (run:PoC_Run {objectid: $id}) SET run.poc_owner = $owner, run.batch_id = $batch, run.plan_hash = $hash, run.completed_at = $completed, run.reference_commit = $reference", parameters)
	return err
}

func rollback(transaction Transaction) {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = transaction.Rollback(cleanup)
}

// PrepareDatabase prepares declared kinds and endpoint indexes before opening
// a borrowed native publication transaction.
func PrepareDatabase(ctx context.Context, database Database, plan extension.Plan) error {
	return prepareDatabase(ctx, database, plan)
}
