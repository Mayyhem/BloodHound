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
package runner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestExpandedHelperPropertiesRespectParameterAllowance(t *testing.T) {
	var (
		database = fixtureDatabase()
		options  = completeBatch()
	)
	options.Limits = runner.DefaultLimits()
	options.Limits.RowBytes = 500
	options.Limits.ParameterBytes = 500
	database.answers[helperQuery][0]["properties"] = map[string]any{"note": strings.Repeat("\x00", 100)}
	assertRollback(t, database, cloneGraph(*database.graph), fixturePlan(), options, "helper properties exceed the parameter budget")
	if database.helperWrites != 0 || database.relationshipWrites != 0 {
		t.Fatal("oversized helper reached publication")
	}
}

// This fake exercises the same batched publication contract as both real
// adapters. Every row still changes only the transaction's private staged graph.
type batchedDatabase struct {
	*fakeDatabase
	failCreate    int
	failDelete    int
	createBatches int
	deleteBatches int
}
type batchedTransaction struct {
	*fakeTransaction
	owner *batchedDatabase
}

var _ runner.Publisher = (*batchedTransaction)(nil)

func (s *batchedDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	transaction, err := s.fakeDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &batchedTransaction{fakeTransaction: transaction.(*fakeTransaction), owner: s}, nil
}
func (s *batchedTransaction) PutNode(ctx context.Context, kind string, properties map[string]any) error {
	s.storeNode(fakeNode{ID: properties["objectid"].(string), Kinds: []string{kind}, Properties: cloneProperties(properties)})
	if kind == "PoC_Helper" {
		s.database.helperWrites++
	}
	return nil
}
func (s *batchedTransaction) CreateRelationships(ctx context.Context, kind, owner string, relationships []runner.Relationship) (int, error) {
	s.owner.createBatches++
	if s.owner.createBatches == s.owner.failCreate {
		return 0, fmt.Errorf("injected late create failure")
	}
	count := 0
	for _, relationship := range relationships {
		rows, err := s.Write(ctx, createRelationshipQuery, map[string]any{"source": relationship.Source, "target": relationship.Target, "owner": owner, "properties": relationship.Properties})
		if err != nil {
			return count, err
		}
		count += len(rows)
	}
	return count, nil
}
func (s *batchedTransaction) UpdateRelationships(ctx context.Context, kind, owner string, updates []runner.RelationshipUpdate) (int, error) {
	var count int
	for _, update := range updates {
		rows, err := s.Write(ctx, updateRelationshipQuery, map[string]any{"id": update.ID, "owner": owner, "properties": update.Properties})
		if err != nil {
			return count, err
		}
		count += len(rows)
	}
	return count, nil
}
func (s *batchedTransaction) DeleteRelationships(ctx context.Context, kind, owner string, identifiers []int64) (int, error) {
	s.owner.deleteBatches++
	if s.owner.deleteBatches == s.owner.failDelete {
		return 0, fmt.Errorf("injected late delete failure")
	}
	count := 0
	for _, identifier := range identifiers {
		rows, err := s.Write(ctx, deleteRelationshipQuery, map[string]any{"id": identifier, "owner": owner})
		if err != nil {
			return count, err
		}
		count += len(rows)
	}
	return count, nil
}

func addManyAnswers(database *fakeDatabase, count int) {
	database.answers[relationshipQuery] = runner.Rows{}
	for index := 0; index < count; index++ {
		identifier := fmt.Sprintf("BATCH-USER-%06d", index)
		database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: identifier, Kinds: []string{"User"}, Properties: map[string]any{}})
		database.answers[relationshipQuery] = append(database.answers[relationshipQuery], map[string]any{"source": identifier, "target": "DOMAIN"})
	}
}

func TestLatePublicationBatchFailuresKeepAllPreviousResults(t *testing.T) {
	for _, phase := range []string{"create", "delete"} {
		t.Run(phase, func(t *testing.T) {
			database := &batchedDatabase{fakeDatabase: fixtureDatabase()}
			plan := fixturePlan()
			plan.Schema.CompareNativeESC1 = false
			options := completeBatch()
			options.Limits = runner.DefaultLimits()
			options.Limits.BatchRows = 10
			options.TemporaryDirectory = t.TempDir()
			addManyAnswers(database.fakeDatabase, 35)
			if phase == "delete" {
				first, err := runner.Run(t.Context(), database, plan, options)
				if err != nil || !first.Committed {
					t.Fatalf("prepare existing results: %+v %v", first, err)
				}
				database.answers[relationshipQuery] = runner.Rows{}
				database.failDelete = 3
			} else {
				database.failCreate = 3
			}
			before := cloneGraph(*database.graph)
			report, err := runner.Run(t.Context(), database, plan, options)
			if err == nil || report.Committed {
				t.Fatalf("late %s failure was accepted: %+v %v", phase, report, err)
			}
			if !reflect.DeepEqual(before, *database.graph) {
				t.Fatalf("late %s failure changed persisted graph", phase)
			}
			if phase == "create" && database.createBatches != 3 || phase == "delete" && database.deleteBatches != 3 {
				t.Fatal("failure did not occur after earlier successful batches")
			}
		})
	}
}

func TestResourceBrakesDoNotAuthorizeCleanup(t *testing.T) {
	for _, resource := range []string{"row", "disk", "result count", "changed edges", "parameter"} {
		t.Run(resource, func(t *testing.T) {
			database := fixtureDatabase()
			runSuccessfully(t, database)
			plan := fixturePlan()
			plan.Schema.CompareNativeESC1 = false
			options := completeBatch()
			options.Limits = runner.DefaultLimits()
			options.TemporaryDirectory = t.TempDir()
			addManyAnswers(database, 35)
			expectedMessage := ""
			switch resource {
			case "row":
				options.Limits.RowBytes = 1
				expectedMessage = "row limit"
			case "disk":
				options.Limits.RowBytes = 512
				options.Limits.SortBytes = 512
				options.Limits.DiskBytes = 1024
				expectedMessage = "disk budget"
			case "result count":
				options.Limits.ResultRows = 1
				expectedMessage = "row limit"
			case "changed edges":
				options.Limits.ChangedObjects = 1
				expectedMessage = "transaction budget"
			case "parameter":
				options.Limits.RowBytes = 512
				options.Limits.ParameterBytes = 512
				expectedMessage = "parameter budget"
				shared := extension.Query{Name: "shared.cypher", Parameter: "shared", Text: "RETURN 'shared' AS value"}
				for index := range plan.Queries {
					if plan.Queries[index].Name == "esc1.cypher" {
						plan.Queries[index].Dependencies = append(plan.Queries[index].Dependencies, extension.Dependency{Type: "query", Name: shared.Name})
					}
				}
				plan.Queries = append([]extension.Query{shared}, plan.Queries...)
				for index := 0; index < 20; index++ {
					database.answers[shared.Text] = append(database.answers[shared.Text], map[string]any{"value": "shared-data"})
				}
			}
			before := cloneGraph(*database.graph)
			report, err := runner.Run(t.Context(), database, plan, options)
			if err == nil || report.Committed {
				t.Fatalf("%s brake failed: %+v %v", resource, report, err)
			}
			if !strings.Contains(err.Error(), expectedMessage) {
				t.Fatalf("wrong %s brake: %v", resource, err)
			}
			if !reflect.DeepEqual(before, *database.graph) {
				t.Fatalf("%s failure changed persisted graph", resource)
			}
		})
	}
}

func TestTransactionBudgetIncludesHelperChanges(t *testing.T) {
	var (
		database = fixtureDatabase()
		before   = cloneGraph(*database.graph)
		options  = completeBatch()
	)
	options.Limits = runner.DefaultLimits()
	options.Limits.ChangedObjects = 2
	options.TemporaryDirectory = t.TempDir()
	database.answers[helperQuery] = runner.Rows{}
	for index := 0; index < 3; index++ {
		database.answers[helperQuery] = append(database.answers[helperQuery], map[string]any{"id": fmt.Sprintf("PoC_Helper_%d", index), "kinds": []string{"PoC_Helper"}, "properties": map[string]any{"name": "helper"}})
	}
	report, err := runner.Run(t.Context(), database, fixturePlan(), options)
	if err == nil || !strings.Contains(err.Error(), "transaction budget") || report.Committed || !reflect.DeepEqual(before, *database.graph) {
		t.Fatalf("helper changes escaped the transaction budget: committed=%v error=%v", report.Committed, err)
	}
}

func TestLongIdentifiersCannotMakeReportsUnbounded(t *testing.T) {
	var (
		database = fixtureDatabase()
		plan     = fixturePlan()
		options  = completeBatch()
	)
	plan.Schema.CompareNativeESC1 = false
	options.TemporaryDirectory = t.TempDir()
	database.answers[relationshipQuery] = runner.Rows{}
	for index := 0; index < 12; index++ {
		// Quotes need escaping in JSON. Count the size of the final report,
		// rather than assuming its size equals the length of these strings.
		identifier := fmt.Sprintf("LONG-%02d-", index) + strings.Repeat("\"", 100_000)
		database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: identifier, Kinds: []string{"User"}, Properties: map[string]any{}})
		database.answers[relationshipQuery] = append(database.answers[relationshipQuery], map[string]any{"source": identifier, "target": "DOMAIN"})
	}
	report, err := runner.Run(t.Context(), database, plan, options)
	if err != nil || !report.Committed || report.Added != 12 || report.DesiredCounts["PoC_ADCSESC1"] != 12 || !report.DesiredTruncated {
		t.Fatalf("bounded examples changed the complete result: added=%d counts=%v truncated=%v error=%v", report.Added, report.DesiredCounts, report.DesiredTruncated, err)
	}
	encoded, err := json.Marshal(report.Desired)
	if err != nil || len(encoded) > (1<<20)+100 {
		t.Fatalf("example report exceeded its byte budget: bytes=%d error=%v", len(encoded), err)
	}
}

func TestPublishedComparisonKeepsExactCountsWithoutExamples(t *testing.T) {
	var (
		database = fixtureDatabase()
		limits   = runner.DefaultLimits()
	)
	limits.ReportPairs = 0
	database.answers["MATCH (source)-[edge:PoC_ADCSESC1]->(target) WHERE edge.poc_owner = $owner RETURN DISTINCT source.objectid AS source, target.objectid AS target"] = runner.Rows{
		{"source": "EXTRA", "target": "DOMAIN"},
		{"source": "EXTRA", "target": "DOMAIN"},
	}
	database.answers[nativeQuery] = runner.Rows{{"source": "MISSING", "target": "DOMAIN"}}
	transaction, err := database.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(t.Context())
	comparison, err := runner.ComparePublished(t.Context(), transaction, "experiment", t.TempDir(), limits)
	if err != nil || comparison.MissingCount != 1 || comparison.ExtraCount != 1 || !comparison.Truncated || len(comparison.Missing) != 0 || len(comparison.Extra) != 0 {
		t.Fatalf("comparison lost an unsampled mismatch: %+v error=%v", comparison, err)
	}
	if database.commits != 0 || database.relationshipWrites != 0 {
		t.Fatal("comparison changed the graph")
	}
}
