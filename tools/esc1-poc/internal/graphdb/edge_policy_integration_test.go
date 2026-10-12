// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package graphdb_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func policyPlan(plan extension.Plan, policy extension.ExistingEdgePolicy, properties string) extension.Plan {
	plan.Schema.CompareNativeESC1 = false
	plan.Schema.Nodes = nil
	plan.Schema.Relationships[0].ExistingEdgePolicy = policy
	plan.Queries = []extension.Query{{Name: "esc1.cypher", OutputKind: "PoC_ADCSESC1", Text: "UNWIND ['PoC_Test_User','PoC_Test_Group'] AS identifier RETURN identifier AS source, 'PoC_Test_Domain' AS target, 'PoC_ADCSESC1' AS kind, " + properties + " AS properties"}}
	return plan
}

func policyEdges(t *testing.T, database runner.Database) runner.Rows {
	t.Helper()
	return read(t, database, "MATCH (source)-[edge:PoC_ADCSESC1]->(target) RETURN source.objectid AS source, target.objectid AS target, id(edge) AS id, properties(edge) AS properties ORDER BY source", nil)
}

func TestExistingEdgePoliciesOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting, extension.MergeProperties, extension.KeepExisting} {
		name := string(policy)
		if name == "" {
			name = "omitted default"
		}
		t.Run(name, func(t *testing.T) {
			neoDatabase, plan := setup(t)
			postgresDatabase, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			for _, backend := range []struct {
				name     string
				database runner.Database
			}{{"neo4j", neoDatabase}, {"postgres", postgresDatabase}} {
				t.Run(backend.name, func(t *testing.T) {
					firstPlan := policyPlan(plan, policy, "{name:'first',keep:'untouched',remove:'old',integer:9007199254740993,decimal:1.25,tags:['one','two'],empty:[],count:2.0}")
					if report, err := runner.Run(t.Context(), backend.database, firstPlan, options()); err != nil || !report.Committed || report.Added != 2 {
						t.Fatalf("initial publication %+v: %v", report, err)
					}
					before := policyEdges(t, backend.database)
					for _, row := range before {
						properties := row["properties"].(map[string]any)
						if properties["integer"] != int64(9007199254740993) || properties["decimal"] != float64(1.25) || properties["count"] != int64(2) || !reflect.DeepEqual(properties["tags"], []any{"one", "two"}) || !reflect.DeepEqual(properties["empty"], []any{}) {
							t.Fatalf("initial array/numeric property shapes: %#v", properties)
						}
					}

					secondPlan := policyPlan(plan, policy, "{name:'changed',remove:null,integer:9007199254740995,decimal:3.75,tags:['changed'],empty:[],count:2}")
					if report, err := runner.Run(t.Context(), backend.database, secondPlan, options()); err != nil || !report.Committed {
						t.Fatalf("policy publication %+v: %v", report, err)
					}
					after := policyEdges(t, backend.database)
					if len(before) != 2 || len(after) != 2 {
						t.Fatalf("policy row counts before=%v after=%v", before, after)
					}
					for index, row := range after {
						properties, valid := row["properties"].(map[string]any)
						if !valid {
							t.Fatalf("properties %T", row["properties"])
						}
						previous := before[index]
						if properties["poc_owner"] != plan.Schema.Owner+"/PoC_ADCSESC1" {
							t.Fatalf("policy changed ownership: %v", properties)
						}
						if policy != extension.KeepExisting && (properties["count"] != int64(2) || !reflect.DeepEqual(properties["tags"], []any{"changed"}) || !reflect.DeepEqual(properties["empty"], []any{})) {
							t.Fatalf("array/integral numeric patch: %#v", properties)
						}

						switch policy {
						case extension.KeepExisting:
							if !reflect.DeepEqual(previous, row) {
								t.Fatalf("keep changed relationship: before=%v after=%v", previous, row)
							}
						case extension.MergeProperties:
							if previous["id"] != row["id"] || properties["keep"] != "untouched" {
								t.Fatalf("merge replaced identity/unmentioned property: %v", row)
							}
							if _, found := properties["remove"]; found {
								t.Fatalf("merge retained explicitly removed key: %v", properties)
							}
							if properties["name"] != "changed" || properties["integer"] != int64(9007199254740995) || properties["decimal"] != float64(3.75) {
								t.Fatalf("merge property precision: %#v", properties)
							}
						default:
							if previous["id"] == row["id"] {
								t.Fatal("replacement retained existing relationship identity")
							}
							if _, found := properties["keep"]; found {
								t.Fatal("replacement retained an unspecified property")
							}
							if _, found := properties["remove"]; found {
								t.Fatal("creation retained null property")
							}
							if properties["name"] != "changed" || properties["integer"] != int64(9007199254740995) || properties["decimal"] != float64(3.75) {
								t.Fatalf("replacement property precision: %#v", properties)
							}
						}
					}
					if policy == extension.MergeProperties {
						repeated, err := runner.Run(t.Context(), backend.database, secondPlan, options())
						if err != nil || !repeated.Committed || repeated.Updated != 0 || repeated.Unchanged != 2 || repeated.Added != 0 || repeated.Removed != 0 {
							t.Fatalf("equivalent repeated merge %+v: %v", repeated, err)
						}
						if repeatedEdges := policyEdges(t, backend.database); !reflect.DeepEqual(after, repeatedEdges) {
							t.Fatalf("no-op merge changed IDs/properties: %v", repeatedEdges)
						}
					}

				})
			}
		})
	}
}

type updateGuardFailureDatabase struct{ runner.Database }
type updateGuardFailureTransaction struct {
	runner.Transaction
	publisher runner.Publisher
	calls     int
}

func (s *updateGuardFailureDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	transaction, err := s.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &updateGuardFailureTransaction{Transaction: transaction, publisher: transaction.(runner.Publisher)}, nil
}
func (s *updateGuardFailureTransaction) PutNode(ctx context.Context, kind string, properties map[string]any) error {
	return s.publisher.PutNode(ctx, kind, properties)
}
func (s *updateGuardFailureTransaction) CreateRelationships(ctx context.Context, kind, owner string, relationships []runner.Relationship) (int, error) {
	return s.publisher.CreateRelationships(ctx, kind, owner, relationships)
}
func (s *updateGuardFailureTransaction) DeleteRelationships(ctx context.Context, kind, owner string, ids []int64) (int, error) {
	return s.publisher.DeleteRelationships(ctx, kind, owner, ids)
}
func (s *updateGuardFailureTransaction) EndpointKinds(ctx context.Context, identifiers []string) (map[string][]string, error) {
	return s.Transaction.(runner.EndpointReader).EndpointKinds(ctx, identifiers)
}
func (s *updateGuardFailureTransaction) UpdateRelationships(ctx context.Context, kind, owner string, updates []runner.RelationshipUpdate) (int, error) {
	s.calls++
	if s.calls == 2 {
		owner = "foreign-owner"
	}
	return s.publisher.UpdateRelationships(ctx, kind, owner, updates)
}

func TestLatePropertyBatchGuardFailureRollsBackBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	neoDatabase, plan := setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range []struct {
		name     string
		database runner.Database
	}{{"neo4j", neoDatabase}, {"postgres", postgresDatabase}} {
		t.Run(backend.name, func(t *testing.T) {
			firstPlan := policyPlan(plan, extension.MergeProperties, "{name:'first',remove:'old',integer:9007199254740993,decimal:1.25,tags:['one','two'],empty:[],count:2.0}")
			if report, err := runner.Run(t.Context(), backend.database, firstPlan, options()); err != nil || !report.Committed {
				t.Fatalf("initial publication %+v: %v", report, err)
			}
			before := policyEdges(t, backend.database)
			secondPlan := policyPlan(plan, extension.MergeProperties, "{name:'changed',remove:null,integer:9007199254740995,decimal:3.75,tags:['changed'],empty:[],count:2}")
			runOptions := options()
			runOptions.Limits = runner.DefaultLimits()
			runOptions.Limits.BatchRows = 1
			failed, err := runner.Run(t.Context(), &updateGuardFailureDatabase{Database: backend.database}, secondPlan, runOptions)
			if err == nil || failed.Committed || !strings.Contains(err.Error(), "update") {
				t.Fatalf("late guard failure %+v: %v", failed, err)
			}
			if after := policyEdges(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatalf("late failed patch changed properties/IDs: before=%v after=%v", before, after)
			}
		})
	}
}
