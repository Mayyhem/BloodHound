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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/drivers/pg"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/util/size"
)

func TestPostgresPrepareKindsMakesMissingKindMatchEmpty(t *testing.T) {
	var (
		database = postgresTestDatabase(t)
		ctx      = context.Background()
		kind     = fmt.Sprintf("POC_PreparedEmpty_%d", time.Now().UnixNano())
		cypher   = "MATCH (item:" + kind + ") RETURN item.objectid AS id"
	)
	transaction, err := database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transaction.Read(ctx, cypher, nil)
	_ = transaction.Rollback(ctx)
	if err == nil || !strings.Contains(err.Error(), "map kind") {
		t.Fatalf("new kind must initially be absent from the catalog: %v", err)
	}
	if err := database.PrepareKinds(ctx, []string{kind, kind}); err != nil {
		t.Fatal(err)
	}
	transaction, err = database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	rows, err := transaction.Read(ctx, cypher, nil)
	if err != nil || len(rows) != 0 {
		t.Fatalf("prepared missing kind must match no objects: %v, %v", rows, err)
	}
}

func postgresTestDatabase(t *testing.T) *graphdb.PostgresDatabase {
	t.Helper()
	var (
		address = os.Getenv("ESC1_POC_POSTGRES_TEST_URI")
		ctx     = context.Background()
	)
	if address == "" {
		t.Skip("ESC1_POC_POSTGRES_TEST_URI selects the dedicated PostgreSQL test graph")
	}
	if address != "postgresql://127.0.0.1:25432/esc1_poc_test" {
		t.Fatal("PostgreSQL tests require the dedicated port and database")
	}
	configuration, err := pgxpool.ParseConfig(address)
	if err != nil {
		t.Fatal(err)
	}
	configuration.ConnConfig.User, configuration.ConnConfig.Password = "bloodhound", "experiment-only"
	configuration.ConnConfig.Fallbacks, configuration.ConnConfig.TLSConfig = nil, nil
	configuration.ConnConfig.RuntimeParams = map[string]string{}
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	driver := pg.NewDriver(size.Size(32*1024*1024), pool)
	defaultGraph := graph.Graph{Name: "default", Nodes: graph.Kinds{graph.StringKind("POCTestNode")}, Edges: graph.Kinds{graph.StringKind("POCTestRelationship")}}
	if err = driver.AssertSchema(ctx, graph.Schema{DefaultGraph: defaultGraph, Graphs: []graph.Graph{defaultGraph}}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "TRUNCATE edge, node RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
	_ = driver.Close(ctx)
	database, err := graphdb.OpenPostgres(ctx, address, "bloodhound", "experiment-only")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	return database
}

func TestPostgresAtomicPublicationAndStreaming(t *testing.T) {
	var (
		database      = postgresTestDatabase(t)
		ctx           = context.Background()
		callbackError = errors.New("stop stream")
	)
	t.Cleanup(func() {
		configuration, err := pgxpool.ParseConfig(os.Getenv("ESC1_POC_POSTGRES_TEST_URI"))
		if err != nil {
			t.Error(err)
			return
		}
		configuration.ConnConfig.User, configuration.ConnConfig.Password = "bloodhound", "experiment-only"
		configuration.ConnConfig.Fallbacks, configuration.ConnConfig.TLSConfig = nil, nil
		configuration.ConnConfig.RuntimeParams = map[string]string{}
		pool, err := pgxpool.NewWithConfig(context.Background(), configuration)
		if err != nil {
			t.Error(err)
			return
		}
		defer pool.Close()
		if _, err = pool.Exec(context.Background(), "DELETE FROM edge WHERE properties ->> 'poc_owner' = 'test/POCTestRelationship'; DELETE FROM node WHERE properties ->> 'poc_owner' = 'test/POCTestNode'"); err != nil {
			t.Error(err)
		}
	})
	transaction, err := database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	publisher := transaction.(runner.Publisher)
	for _, identifier := range []string{"source", "target"} {
		if err = publisher.PutNode(ctx, "POCTestNode", map[string]any{"objectid": identifier, "poc_owner": "test/POCTestNode", "name": identifier}); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := publisher.CreateRelationships(ctx, "POCTestRelationship", "test/POCTestRelationship", []runner.Relationship{{Source: "source", Target: "target"}}); err != nil || count != 1 {
		t.Fatalf("create count %d: %v", count, err)
	}
	if err = transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	transaction, err = database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	publisher = transaction.(runner.Publisher)
	if err = publisher.PutNode(ctx, "POCTestNode", map[string]any{"objectid": "source", "poc_owner": "test/POCTestNode", "name": "changed"}); err != nil {
		t.Fatal(err)
	}
	rows, err := transaction.Read(ctx, "MATCH ()-[edge:POCTestRelationship]->() RETURN id(edge) AS id", nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("relationship rows %v: %v", rows, err)
	}
	identifier, valid := rows[0]["id"].(int64)
	if !valid {
		t.Fatalf("unnormalized ID %T", rows[0]["id"])
	}
	if count, err := publisher.DeleteRelationships(ctx, "POCTestRelationship", "foreign", []int64{identifier}); err != nil || count != 0 {
		t.Fatalf("foreign owner delete %d: %v", count, err)
	}
	if count, err := publisher.DeleteRelationships(ctx, "POCTestRelationship", "test/POCTestRelationship", []int64{identifier}); err != nil || count != 1 {
		t.Fatalf("owned delete %d: %v", count, err)
	}
	if err = transaction.(runner.StreamingTransaction).Stream(ctx, "MATCH (n:POCTestNode) RETURN n.objectid AS objectid", nil, func(map[string]any) error { return callbackError }); !errors.Is(err, callbackError) {
		t.Fatalf("stream error %v", err)
	}
	if err = transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	transaction, err = database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	rows, err = transaction.Read(ctx, "MATCH (n:POCTestNode {objectid: $identifier}) RETURN n.name AS name", map[string]any{"identifier": "source"})
	if err != nil || len(rows) != 1 || rows[0]["name"] != "source" {
		t.Fatalf("rollback changed helper: %v: %v", rows, err)
	}
	rows, err = transaction.Read(ctx, "MATCH ()-[edge:POCTestRelationship]->() RETURN id(edge) AS id", nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rollback removed relationship: %v: %v", rows, err)
	}
	if _, err = transaction.Read(ctx, "MATCH (n) DELETE n", nil); err == nil {
		t.Fatal("read accepted a write")
	}
}

func TestPostgresStreamPreservesExactJSONBatchNumbers(t *testing.T) {
	var (
		database = postgresTestDatabase(t)
		ctx      = t.Context()
		integer  = int64(9007199254740993)
		expected = map[string]any{"graph_id": integer, "nested": map[string]any{"integer": integer}, "numbers": []any{int64(7), float64(1.5), float64(2)}}
		count    int
	)
	transaction, err := database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	streamer := transaction.(runner.StreamingTransaction)
	parameters := map[string]any{"rows": []map[string]any{{"graph_id": integer, "nested": map[string]any{"integer": integer}, "numbers": []any{int64(7), float64(1.5), json.Number("2.0")}}}}
	if err = streamer.Stream(ctx, "UNWIND $rows AS row RETURN row.graph_id AS graph_id, row.nested AS nested, row.numbers AS numbers", parameters, func(row map[string]any) error {
		count++
		if !reflect.DeepEqual(row, expected) {
			t.Fatalf("imprecise JSON batch answer %#v", row)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("answer count %d", count)
	}
	count = 0
	parameters = map[string]any{"rows": []map[string]any{{"graph_id": integer}, {"graph_id": json.Number("9223372036854775808")}}}
	if err = streamer.Stream(ctx, "UNWIND $rows AS row RETURN row.graph_id AS graph_id", parameters, func(row map[string]any) error { count++; return nil }); err == nil {
		t.Fatal("late integer overflow appeared to be a complete answer")
	}
	if count != 1 {
		t.Fatalf("overflow callback count %d", count)
	}
}
