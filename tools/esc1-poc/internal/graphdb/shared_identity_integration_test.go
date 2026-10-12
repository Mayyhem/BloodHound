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
	"os"
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

// Existing helper IDs and relationship endpoints determine which objects are
// refreshed. A property written by an older version cannot protect or claim an
// object, and that property's name can now be supplied like any other property.
func TestExistingObjectsWithLegacyPropertiesOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	for _, native := range []bool{false, true} {
		var name = "standalone"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			var neoDatabase, fixture = setup(t)
			postgresDatabase, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
				t.Run(backend.name, func(t *testing.T) {
					var plan = fixture
					plan.Queries = append([]extension.Query(nil), fixture.Queries...)
					plan.Schema.Relationships[0].ExistingEdgePolicy = extension.MergeProperties
					replaceESC1Query(&plan, "RETURN 'PoC_Test_User' AS source, 'PoC_Test_Domain' AS target, {name:'updated', poc_owner:'ordinary property'} AS properties")
					if err := backend.native.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
						var properties = graph.AsProperties(map[string]any{"name": "old", "poc_owner": "someone-else"})
						target, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_Domain")).First()
						if err != nil {
							return err
						}
						for _, identifier := range []string{"PoC_Test_User", "PoC_Test_Group"} {
							source, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), identifier)).First()
							if err != nil {
								return err
							}
							if _, err := transaction.CreateRelationshipByIDs(source.ID, target.ID, graph.StringKind("PoC_ADCSESC1"), properties); err != nil {
								return err
							}
						}
						// This pair is outside the schema's User/Group -> Domain
						// scope, even though it uses the same relationship type.
						outsideSource, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_User")).First()
						if err != nil {
							return err
						}
						outsideTarget, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_Host")).First()
						if err != nil {
							return err
						}
						if _, err := transaction.CreateRelationshipByIDs(outsideSource.ID, outsideTarget.ID, graph.StringKind("PoC_ADCSESC1"), properties); err != nil {
							return err
						}
						_, err = transaction.CreateNode(graph.AsProperties(map[string]any{"objectid": "PoC_DomainHelper_PoC_Test_Domain", "name": "old", "poc_owner": "someone-else"}), graph.StringKind("PoC_DomainHelper"))
						return err
					}); err != nil {
						t.Fatal(err)
					}
					before := read(t, backend.database, "MATCH (:User)-[edge:PoC_ADCSESC1]->(:Domain) RETURN id(edge) AS id", nil)
					outside := read(t, backend.database, "MATCH (:User)-[edge:PoC_ADCSESC1]->(:Computer) RETURN id(edge) AS id, properties(edge) AS properties", nil)
					run := func() (runner.Report, error) {
						if native {
							return nativeIntegrationRun(t, backend, plan, options())
						}
						return runner.Run(t.Context(), backend.database, plan, options())
					}
					report, err := run()
					if err != nil || !report.Committed || report.Updated != 1 || report.Removed != 1 || report.HelpersChanged != 1 {
						t.Fatalf("refresh existing objects: %+v %v", report, err)
					}
					after := read(t, backend.database, "MATCH ()-[edge:PoC_ADCSESC1]->(:Domain) RETURN id(edge) AS id, properties(edge) AS properties", nil)
					if len(after) != 1 || after[0]["id"] != before[0]["id"] {
						t.Fatalf("merge changed the surviving relationship identity: %v", after)
					}
					properties := after[0]["properties"].(map[string]any)
					if properties["name"] != "updated" || properties["poc_owner"] != "ordinary property" {
						t.Fatalf("ordinary properties were not updated: %v", properties)
					}
					if unchanged := read(t, backend.database, "MATCH (:User)-[edge:PoC_ADCSESC1]->(:Computer) RETURN id(edge) AS id, properties(edge) AS properties", nil); !reflect.DeepEqual(outside, unchanged) {
						t.Fatalf("refresh changed a connection outside the declared endpoint pairs: %v", unchanged)
					}
					helpers := read(t, backend.database, "MATCH (helper:PoC_DomainHelper) RETURN helper.name AS name, properties(helper) AS properties", nil)
					if len(helpers) != 1 || helpers[0]["name"] != "TEST DOMAIN" {
						t.Fatalf("existing helper was not refreshed: %v", helpers)
					}
					if _, found := helpers[0]["properties"].(map[string]any)["poc_owner"]; found {
						t.Fatal("helper retained an unspecified legacy property")
					}
					replaceESC1Query(&plan, "RETURN 'PoC_Test_User' AS source, 'PoC_Test_Domain' AS target, {poc_owner:null} AS properties")
					if report, err := run(); err != nil || !report.Committed || report.Updated != 1 {
						t.Fatalf("remove ordinary property: %+v %v", report, err)
					}
					if properties := read(t, backend.database, "MATCH ()-[edge:PoC_ADCSESC1]->(:Domain) RETURN id(edge) AS id, properties(edge) AS properties", nil)[0]["properties"].(map[string]any); properties["name"] != "updated" || properties["poc_owner"] != nil {
						t.Fatalf("ordinary property removal changed other values: %v", properties)
					}
				})
			}
		})
	}
}
