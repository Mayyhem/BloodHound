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
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

// Malformed unrelated objects must still stop publication. Migration records
// are the only exception, and only while they have exactly one kind and no links.
// Exercise the application's real adapters as well as the ordinary read path.
func TestPreparationIdentityChecks(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated databases are required")
	}
	var cases = []struct {
		name       string
		kinds      []string
		properties map[string]any
		link       string
		allowed    bool
	}{
		{"unrelated missing", []string{"PoC_IdentityUnrelated"}, nil, "", false},
		{"unlabelled missing", nil, nil, "", false},
		{"unrelated null", []string{"PoC_IdentityUnrelated"}, map[string]any{"objectid": nil}, "", false},
		{"unrelated empty", []string{"PoC_IdentityUnrelated"}, map[string]any{"objectid": ""}, "", false},
		{"unrelated whitespace", []string{"PoC_IdentityUnrelated"}, map[string]any{"objectid": " \t\n"}, "", false},
		{"unrelated duplicate", []string{"PoC_IdentityUnrelated"}, map[string]any{"objectid": "PoC_Test_User"}, "", false},
		{"unrelated valid", []string{"PoC_IdentityUnrelated"}, map[string]any{"objectid": "PoC_Test_Unrelated"}, "", true},
		{"isolated migration", []string{"MigrationData"}, nil, "", true},
		{"isolated migration duplicate non-endpoint", []string{"MigrationData"}, map[string]any{"objectid": "PoC_Test_Host"}, "", true},
		{"mixed migration", []string{"MigrationData", "PoC_IdentityUnrelated"}, nil, "", false},
		{"outgoing migration", []string{"MigrationData"}, nil, "out", false},
		{"incoming migration", []string{"MigrationData"}, nil, "in", false},
		{"self linked migration", []string{"MigrationData"}, nil, "self", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				neoDatabase, plan        = setup(t)
				postgresDatabase, driver = postgresESC1Database(t)
			)
			copyESC1Fixture(t, neoDatabase, driver)
			for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
				t.Run(backend.name, func(t *testing.T) {
					var settings = options()
					if report, err := nativeIntegrationRun(t, backend, plan, settings); err != nil || !report.Committed {
						t.Fatalf("initial save: %v", err)
					}
					err := backend.native.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
						var properties = graph.NewProperties()
						for name, value := range testCase.properties {
							properties.Set(name, value)
						}
						properties.Set("poc_fixture", true)
						node, err := transaction.CreateNode(properties, graph.StringsToKinds(testCase.kinds)...)
						if err != nil {
							return err
						}
						if testCase.link == "" {
							return nil
						}
						group, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_Group")).First()
						if err != nil {
							return err
						}
						source, target := node.ID, group.ID
						if testCase.link == "in" {
							source, target = target, source
						}
						if testCase.link == "self" {
							target = source
						}
						_, err = transaction.CreateRelationshipByIDs(source, target, graph.StringKind("PoC_IdentityLink"), graph.NewProperties())
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
					before := guardGraphSnapshot(t, backend.database)
					settings.BatchID = "identity-check-second-run"
					report, err := nativeIntegrationRun(t, backend, plan, settings)
					if testCase.allowed {
						if err != nil || !report.Committed {
							t.Fatalf("valid graph rejected: %v", err)
						}
					} else {
						if err == nil || !strings.Contains(err.Error(), "duplicate objectids") || report.Committed {
							t.Fatalf("malformed identity accepted: %+v, %v", report, err)
						}
						if !reflect.DeepEqual(before, guardGraphSnapshot(t, backend.database)) {
							t.Fatal("identity failure changed objects, connections, or the previous success record")
						}
					}
					steps := report.Preparation
					if steps.QueryValidation <= 0 || steps.Database <= 0 || steps.Transaction <= 0 || steps.Selection <= 0 || steps.Identities <= 0 {
						t.Fatalf("missing preparation timing, including work outside the runner: %+v", steps)
					}
					if steps.QueryValidation+steps.Database+steps.Transaction+steps.Selection+steps.Identities > report.PreparationElapsed {
						t.Fatalf("preparation steps overlap: %+v exceeds %s", steps, report.PreparationElapsed)
					}
				})
			}
		})
	}
}
