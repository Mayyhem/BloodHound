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
	"errors"
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestEditorTestStagesPrerequisitesThenDiscardsEverything(t *testing.T) {
	var database = fixtureDatabase()
	var before = cloneGraph(*database.graph)
	options := completeBatch()
	options.TestTarget = "esc1.cypher"
	report, err := runner.Test(t.Context(), database, fixturePlan(), options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Committed || !report.Tested || !report.Discarded || report.GraphObjectsChanged != 0 {
		t.Fatalf("test claimed the wrong outcome: %+v", report)
	}
	if database.commits != 0 || database.rollbacks != 1 {
		t.Fatalf("commit/rollback = %d/%d", database.commits, database.rollbacks)
	}
	if database.helperWrites == 0 || database.relationshipWrites != 0 {
		t.Fatalf("helper/final writes = %d/%d", database.helperWrites, database.relationshipWrites)
	}
	if !reflect.DeepEqual(*database.graph, before) {
		t.Fatalf("test changed saved graph: before=%+v after=%+v", before, *database.graph)
	}
	for _, node := range database.graph.Nodes {
		if len(node.Kinds) == 1 && node.Kinds[0] == "PoC_Run" {
			t.Fatal("test saved a PoC_Run node")
		}
	}
}

func TestEditorTestValidatesFinalHelperWithoutPublishingIt(t *testing.T) {
	var (
		database = fixtureDatabase()
		before   = cloneGraph(*database.graph)
		plan     = fixturePlan()
		options  = completeBatch()
	)
	plan.Queries = plan.Queries[:1]
	plan.Schema.Relationships = nil
	options.TestTarget = "helper.cypher"
	report, err := runner.Test(t.Context(), database, plan, options)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Tested || !report.Discarded || database.helperWrites != 0 || database.relationshipWrites != 0 {
		t.Fatalf("final helper was published: report=%+v helper/relationship writes=%d/%d", report, database.helperWrites, database.relationshipWrites)
	}
	if !reflect.DeepEqual(*database.graph, before) {
		t.Fatal("final helper changed the saved graph")
	}
}

func TestEditorTestTargetMayReadItsPreviouslySavedRelationshipKind(t *testing.T) {
	var (
		database = fixtureDatabase()
		plan     = fixturePlan()
		options  = completeBatch()
	)
	plan.Queries[1].Dependencies = []extension.Dependency{{Type: "relationship", Name: "PoC_ADCSESC1"}}
	options.TestTarget = "esc1.cypher"
	report, err := runner.Test(t.Context(), database, plan, options)
	if err != nil || !report.Tested || !report.Discarded || database.relationshipWrites != 0 {
		t.Fatalf("report=%+v error=%v relationship writes=%d", report, err, database.relationshipWrites)
	}
}

func TestEditorTestFailureAndCancellationDiscardChanges(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		context func(*testing.T) context.Context
		prepare func(*fakeDatabase)
	}{
		{
			name:    "query failure",
			context: func(t *testing.T) context.Context { return t.Context() },
			prepare: func(database *fakeDatabase) {
				database.readErrors[relationshipQuery] = errors.New("query failed")
			},
		},
		{
			name: "canceled context",
			context: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			prepare: func(*fakeDatabase) {},
		},
		{
			name:    "invalid final output",
			context: func(t *testing.T) context.Context { return t.Context() },
			prepare: func(database *fakeDatabase) {
				database.answers[relationshipQuery] = runner.Rows{{"source": "USER"}}
			},
		},
		{
			name:    "helper change limit",
			context: func(t *testing.T) context.Context { return t.Context() },
			prepare: func(database *fakeDatabase) {
				_ = database
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var database = fixtureDatabase()
			var before = cloneGraph(*database.graph)
			testCase.prepare(database)
			options := completeBatch()
			options.TestTarget = "esc1.cypher"
			if testCase.name == "helper change limit" {
				options.Limits = runner.DefaultLimits()
				options.Limits.ChangedObjects = 1
			}
			report, err := runner.Test(testCase.context(t), database, fixturePlan(), options)
			if err == nil || report.Tested || report.Discarded || report.Committed {
				t.Fatalf("report=%+v error=%v", report, err)
			}
			if database.commits != 0 || database.rollbacks != 1 || !reflect.DeepEqual(*database.graph, before) {
				t.Fatalf("test failure did not discard graph: commits=%d rollbacks=%d", database.commits, database.rollbacks)
			}
		})
	}
}

type rollbackFailureDatabase struct{ *fakeDatabase }

func (s rollbackFailureDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	transaction, err := s.fakeDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return rollbackFailureTransaction{Transaction: transaction}, nil
}

type rollbackFailureTransaction struct{ runner.Transaction }

func (s rollbackFailureTransaction) Rollback(context.Context) error {
	return errors.New("database did not confirm rollback")
}

func TestEditorTestDoesNotSucceedWhenRollbackFails(t *testing.T) {
	var database = fixtureDatabase()
	options := completeBatch()
	options.TestTarget = "esc1.cypher"
	report, err := runner.Test(t.Context(), rollbackFailureDatabase{database}, fixturePlan(), options)
	if err == nil || report.Tested || report.Discarded || report.Committed {
		t.Fatalf("report=%+v error=%v", report, err)
	}
}
