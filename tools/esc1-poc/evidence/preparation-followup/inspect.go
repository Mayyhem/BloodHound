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

//go:build ignore

// Inspect an existing synthetic graph in a read-only transaction.
// EXPLAIN ANALYZE executes the supplied query and records its actual work.
// This is a local measurement utility, not a production query entry point.
package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/specterops/dawgs/cypher/frontend"
	"github.com/specterops/dawgs/cypher/models/pgsql/translate"
	"github.com/specterops/dawgs/drivers/pg/pgutil"
	"github.com/specterops/dawgs/graph"
)

func main() {
	var (
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
		parameters  = map[string]any{}
		mapper      = pgutil.NewInMemoryKindMapper()
		graphID     int32
		plan        json.RawMessage
	)
	defer cancel()
	connection, err := pgx.Connect(ctx, os.Getenv("CONNECTION_STRING"))
	if err != nil {
		panic(err)
	}
	defer connection.Close(context.Background())
	transaction, err := connection.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		panic(err)
	}
	defer transaction.Rollback(context.Background())

	if err := transaction.QueryRow(ctx, "SELECT id FROM graph WHERE name='default'").Scan(&graphID); err != nil {
		panic(err)
	}
	kinds, err := transaction.Query(ctx, "SELECT id,name FROM kind")
	if err != nil {
		panic(err)
	}
	for kinds.Next() {
		var id int16
		var name string
		if err := kinds.Scan(&id, &name); err != nil {
			panic(err)
		}
		kind := graph.StringKind(name)
		mapper.KindToID[kind], mapper.IDToKind[id] = id, kind
	}
	kinds.Close()
	if err := kinds.Err(); err != nil {
		panic(err)
	}
	if len(os.Args) > 3 && os.Args[3] == "membership" {
		// These are the complete enrollers in the generated users-shaped fixture.
		seeds, err := transaction.Query(ctx, "SELECT DISTINCT node.id FROM node JOIN edge ON edge.start_id = node.id JOIN kind ON kind.id = edge.kind_id WHERE kind.name = 'Enroll' ORDER BY node.id")
		if err != nil {
			panic(err)
		}
		var values []map[string]any
		for seeds.Next() {
			var identifier int64
			if err := seeds.Scan(&identifier); err != nil {
				panic(err)
			}
			values = append(values, map[string]any{"graph_id": identifier})
		}
		seeds.Close()
		if err := seeds.Err(); err != nil {
			panic(err)
		}
		parameters["enrollment_principals"] = values
	}
	source, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	parsed, err := frontend.ParseCypher(frontend.NewContext(), string(source))
	if err != nil {
		panic(err)
	}
	translated, err := translate.Translate(ctx, parsed, mapper, parameters, graphID)
	if err != nil {
		panic(err)
	}
	formatted, err := translate.Translated(translated)
	if err != nil {
		panic(err)
	}
	for key, value := range formatted.Parameters {
		translated.Parameters[key] = value
	}
	if err := os.WriteFile(os.Args[2]+".sql", []byte(formatted.Statement+"\n"), 0600); err != nil {
		panic(err)
	}
	if err := transaction.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+strings.TrimSpace(formatted.Statement), pgx.NamedArgs(translated.Parameters)).Scan(&plan); err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[2]+".json", plan, 0600); err != nil {
		panic(err)
	}
	rows, err := transaction.Query(ctx, formatted.Statement, pgx.NamedArgs(translated.Parameters))
	if err != nil {
		panic(err)
	}
	var answers []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			panic(err)
		}
		answer := map[string]any{}
		for index, field := range rows.FieldDescriptions() {
			answer[field.Name] = values[index]
		}
		answers = append(answers, answer)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		panic(err)
	}
	encoded, err := json.MarshalIndent(answers, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[2]+".answers.json", encoded, 0600); err != nil {
		panic(err)
	}

}
