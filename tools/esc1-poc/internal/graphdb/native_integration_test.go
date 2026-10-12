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

//go:build integration

package graphdb_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
	"github.com/specterops/dawgs"
	_ "github.com/specterops/dawgs/drivers/neo4j"
	"github.com/specterops/dawgs/drivers/pg"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/util/size"
)

type nativeIntegrationBackend struct {
	name     string
	database runner.Database
	native   *graph.DatabaseSwitch
	gate     *postprocessing.Gate
}

func nativeIntegrationBackends(t *testing.T, neoDatabase *graphdb.Database, postgresDatabase *graphdb.PostgresDatabase, postgresDriver *pg.Driver) []nativeIntegrationBackend {
	t.Helper()
	address, err := url.Parse(os.Getenv("NEO4J_TEST_URI"))
	if err != nil || address.Host != "127.0.0.1:27687" {
		t.Fatal("native tests require the isolated Neo4j graph")
	}
	address.Scheme = "neo4j"
	address.User = url.UserPassword("neo4j", "experiment-only")
	address.Path = "/neo4j"
	neoDriver, err := dawgs.Open(t.Context(), "neo4j", dawgs.Config{ConnectionString: address.String(), GraphQueryMemoryLimit: size.Size(32 * 1024 * 1024)})
	if err != nil {
		t.Fatal(err)
	}
	// Closing belongs to this application-shaped test harness, never RunNative.
	t.Cleanup(func() { _ = neoDriver.Close(context.Background()) })
	result := []nativeIntegrationBackend{{name: "neo4j", database: neoDatabase, native: graph.NewDatabaseSwitch(t.Context(), neoDriver), gate: postprocessing.NewGate()}, {name: "pg", database: postgresDatabase, native: graph.NewDatabaseSwitch(t.Context(), postgresDriver), gate: postprocessing.NewGate()}}
	for index := range result {
		result[index].native.SetWriteGate(result[index].gate.EnterWrite)
	}
	return result
}
func nativeIntegrationRun(t *testing.T, backend nativeIntegrationBackend, plan extension.Plan, options runner.Options) (runner.Report, error) {
	t.Helper()
	var report runner.Report
	err := backend.gate.Alone(t.Context(), 30*time.Second, func(ctx context.Context) error {
		var err error
		report, err = graphdb.RunNative(ctx, backend.native, backend.name, plan, options)
		return err
	})
	return report, err
}

func TestNativeESC1QueriesOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graphs are required")
	}
	for _, testCase := range esc1Cases() {
		t.Run(testCase.name, func(t *testing.T) {
			neoDatabase, plan := setup(t)
			if testCase.change != "" {
				write(t, neoDatabase, testCase.change, nil)
			}
			postgresDatabase, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			before := postgresFixtureSnapshot(t, driver, true)
			neoBefore := nativeSnapshot(t, neoDatabase)
			for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
				t.Run(backend.name, func(t *testing.T) {
					report, err := nativeIntegrationRun(t, backend, plan, options())
					if err != nil || !report.Committed {
						t.Fatalf("native publication %+v: %v", report, err)
					}
					actual := read(t, backend.database, "MATCH (s)-[:PoC_ADCSESC1]->(d) RETURN s.objectid AS source,d.objectid AS target ORDER BY source", nil)
					expected := runner.Rows{}
					for _, source := range testCase.sources {
						expected = append(expected, map[string]any{"source": "PoC_Test_" + source, "target": "PoC_Test_Domain"})
					}
					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("native connections: got %v want %v", actual, expected)
					}
					if err := backend.native.ReadTransaction(t.Context(), func(transaction graph.Transaction) error {
						result := transaction.Query("RETURN 'ready' AS value", nil)
						defer result.Close()
						if !result.Next() || result.Values()[0] != "ready" {
							return fmt.Errorf("application driver unavailable after native run: %v", result.Error())
						}
						return result.Error()
					}); err != nil {
						t.Fatal(err)
					}
				})
			}
			if postgresFixtureSnapshot(t, driver, true) != before || nativeSnapshot(t, neoDatabase) != neoBefore {
				t.Fatal("native run changed input graph")
			}
		})
	}
}
func TestNativeExistingEdgePoliciesOnBothDatabases(t *testing.T) {
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
			for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
				t.Run(backend.name, func(t *testing.T) {
					firstPlan := policyPlan(plan, policy, "{name:'first',keep:'untouched',remove:'old',integer:9007199254740993,decimal:1.25,tags:['one','two'],empty:[],count:2.0}")
					if report, err := nativeIntegrationRun(t, backend, firstPlan, options()); err != nil || !report.Committed || report.Added != 2 {
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
					if report, err := nativeIntegrationRun(t, backend, secondPlan, options()); err != nil || !report.Committed {
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
						repeated, err := nativeIntegrationRun(t, backend, secondPlan, options())
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

func TestNativeGateKeepsReadersAvailableAndPoolCodecsUnchanged(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graphs are required")
	}
	neoDatabase, plan := setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			err := backend.gate.Alone(t.Context(), 30*time.Second, func(ctx context.Context) error {
				writerContext, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
				blocked := make(chan error, 1)
				go func() {
					blocked <- backend.native.WriteTransaction(writerContext, func(graph.Transaction) error { return errors.New("ordinary writer entered exclusive turn") })
				}()
				// This uses an ordinary caller's context, without the turn privilege.
				if err := backend.native.ReadTransaction(t.Context(), func(transaction graph.Transaction) error {
					result := transaction.Query("RETURN 'ready' AS value", nil)
					defer result.Close()
					if !result.Next() {
						return fmt.Errorf("UI read failed: %v", result.Error())
					}
					return result.Error()
				}); err != nil {
					return err
				}
				if err := <-blocked; !errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("ordinary writer was not held: %v", err)
				}
				report, err := graphdb.RunNative(ctx, backend.native, backend.name, plan, options())
				if err != nil || !report.Committed {
					return fmt.Errorf("native publication %+v: %w", report, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := driver.ReadTransaction(t.Context(), func(transaction graph.Transaction) error {
		result := transaction.Query("RETURN {integer:9007199254740993} AS properties", nil)
		defer result.Close()
		if !result.Next() {
			return result.Error()
		}
		properties := result.Values()[0].(map[string]any)
		if _, standardCodec := properties["integer"].(float64); !standardCodec {
			return fmt.Errorf("native shared JSON codec was changed: %#v", properties)
		}
		return result.Error()
	}); err != nil {
		t.Fatal(err)
	}
}

type nativeMarkerFailureDatabase struct{ graph.Database }
type nativeMarkerFailureTransaction struct{ graph.Transaction }

func (s *nativeMarkerFailureTransaction) Raw(source string, parameters map[string]any) graph.Result {
	if strings.HasPrefix(source, "MERGE") && strings.Contains(source, "PoC_Run") {
		return graph.NewErrorResult(errors.New("late native marker failure"))
	}
	return s.Transaction.Raw(source, parameters)
}
func (s *nativeMarkerFailureDatabase) WriteTransaction(ctx context.Context, callback graph.TransactionDelegate, options ...graph.TransactionOption) error {
	return s.Database.WriteTransaction(ctx, func(transaction graph.Transaction) error {
		return callback(&nativeMarkerFailureTransaction{Transaction: transaction})
	}, options...)
}

func TestNativeLatePublicationFailureRestoresPropertiesAndIDs(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graphs are required")
	}
	neoDatabase, plan := setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			first := policyPlan(plan, extension.MergeProperties, "{name:'first',integer:9007199254740993,tags:['one','two'],empty:[]}")
			if report, err := nativeIntegrationRun(t, backend, first, options()); err != nil || !report.Committed {
				t.Fatalf("initial %+v: %v", report, err)
			}
			before := policyEdges(t, backend.database)
			second := policyPlan(plan, extension.MergeProperties, "{name:'changed',integer:9007199254740995,tags:['changed'],empty:[]}")
			var borrowed graph.Database = backend.native
			if backend.name == "neo4j" {
				borrowed = &nativeMarkerFailureDatabase{Database: backend.native}
			} else {
				// A scoped trigger fails only this fixture's final run-marker write,
				// after its relationship batches have changed within the transaction.
				statements := []string{
					`CREATE FUNCTION public.poc_native_fail_marker() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.properties ->> 'poc_owner' = 'esc1-poc/integration-test' THEN RAISE EXCEPTION 'late native marker failure'; END IF; RETURN NEW; END $$`,
					`CREATE TRIGGER poc_native_fail_marker BEFORE INSERT OR UPDATE ON public.node FOR EACH ROW EXECUTE FUNCTION public.poc_native_fail_marker()`,
				}
				for _, source := range statements {
					if err := nativeFixtureSQL(t.Context(), driver, source); err != nil {
						t.Fatal(err)
					}
				}
				t.Cleanup(func() {
					if err := nativeFixtureSQL(context.Background(), driver, "DROP TRIGGER IF EXISTS poc_native_fail_marker ON public.node"); err != nil {
						t.Error(err)
					}
					if err := nativeFixtureSQL(context.Background(), driver, "DROP FUNCTION IF EXISTS public.poc_native_fail_marker()"); err != nil {
						t.Error(err)
					}
				})
			}
			var report runner.Report
			err := backend.gate.Alone(t.Context(), 30*time.Second, func(ctx context.Context) error {
				var err error
				report, err = graphdb.RunNative(ctx, borrowed, backend.name, second, options())
				return err
			})
			if err == nil || report.Committed || (!strings.Contains(err.Error(), "late native marker failure") && !errors.Is(err, runner.ErrCommitRolledBack)) {
				t.Fatalf("late failure %+v: %v", report, err)
			}
			if backend.name == "pg" && (!errors.Is(err, runner.ErrCommitRolledBack) || errors.Is(err, runner.ErrCommitUncertain)) {
				t.Fatalf("confirmed PG rollback classified as uncertain: %v", err)
			}
			if after := policyEdges(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed run changed IDs/properties: before=%v after=%v", before, after)
			}
		})
	}
}
func nativeFixtureSQL(ctx context.Context, driver *pg.Driver, source string) error {
	return driver.WriteTransaction(ctx, func(transaction graph.Transaction) error {
		result := transaction.Raw(source, nil)
		defer result.Close()
		for result.Next() {
		}
		return result.Error()
	})
}
