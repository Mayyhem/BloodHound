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
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestNeo4jIndexedBulkPublication(t *testing.T) {
	var database, _ = setup(t)
	if err := database.PrepareEndpoints(t.Context(), []string{"User", "Group", "Domain", "POCTestNode"}); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(t.Context())
	endpointReader := transaction.(runner.EndpointReader)
	kinds, err := endpointReader.EndpointKinds(t.Context(), []string{"PoC_Test_User", "PoC_Test_Domain"})
	if err != nil || len(kinds) != 2 {
		t.Fatalf("endpoint kinds %v: %v", kinds, err)
	}
	if _, err = endpointReader.EndpointKinds(t.Context(), []string{"missing"}); err == nil {
		t.Fatal("accepted missing endpoint")
	}
	publisher := transaction.(runner.Publisher)
	if err = publisher.PutNode(t.Context(), "POCTestNode", map[string]any{"objectid": "PoC_Test_BulkHelper", "name": "first"}); err != nil {
		t.Fatal(err)
	}
	if err = publisher.PutNode(t.Context(), "POCTestNode", map[string]any{"objectid": "PoC_Test_BulkHelper", "name": "second"}); err != nil {
		t.Fatal(err)
	}
	if err = publisher.PutNode(t.Context(), "User", map[string]any{"objectid": "PoC_Test_BulkHelper", "name": "wrong type"}); err == nil {
		t.Fatal("helper write reused an object ID belonging to another type")
	}
	if count, err := publisher.CreateRelationships(t.Context(), "PoC_Test_BulkRelationship", []runner.Relationship{{Source: "PoC_Test_User", Target: "PoC_Test_Domain"}}); err != nil || count != 1 {
		t.Fatalf("bulk create %d: %v", count, err)
	}
	rows, err := transaction.Read(t.Context(), "MATCH ()-[edge:PoC_Test_BulkRelationship]->() RETURN id(edge) AS id", nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("bulk relationships %v: %v", rows, err)
	}
	identifier := rows[0]["id"].(int64)
	if count, err := publisher.DeleteRelationships(t.Context(), "PoC_Test_BulkRelationship", []int64{identifier + 1000000}); err != nil || count != 0 {
		t.Fatalf("missing ID delete %d: %v", count, err)
	}
	if count, err := publisher.DeleteRelationships(t.Context(), "PoC_Test_BulkRelationship", []int64{identifier}); err != nil || count != 1 {
		t.Fatalf("delete %d: %v", count, err)
	}
	if _, err = transaction.Write(t.Context(), "MATCH (node:User {objectid: 'PoC_Test_User'}) SET node:Group", nil); err != nil {
		t.Fatal(err)
	}
	if kinds, err = endpointReader.EndpointKinds(t.Context(), []string{"PoC_Test_User"}); err != nil || len(kinds) != 1 {
		t.Fatalf("multi-label endpoint duplicated: %v: %v", kinds, err)
	}
	if _, err = transaction.Write(t.Context(), "CREATE (:Group {objectid: 'PoC_Test_User', poc_fixture: true})", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = endpointReader.EndpointKinds(t.Context(), []string{"PoC_Test_User"}); err == nil {
		t.Fatal("accepted duplicate endpoint objectid")
	}
	if err = transaction.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rows = read(t, database, "MATCH (n:POCTestNode) RETURN n.objectid AS id", nil); len(rows) != 0 {
		t.Fatalf("rollback leaked helper %v", rows)
	}
}
