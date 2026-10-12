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
package graphdb

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPostgresAddressValidation(t *testing.T) {
	for _, address := range []string{"postgresql://127.0.0.1:25432/esc1_poc_test", "postgresql://127.0.0.1:15432/bloodhound"} {
		if err := validatePostgresAddress(address, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range []string{"postgresql://172.18.0.2:5432/bloodhound", "postgresql://[fd00::2]:5432/bloodhound"} {
		if err := validatePostgresAddress(address, true); err != nil {
			t.Fatal(err)
		}
		if err := validatePostgresAddress(address, false); err == nil {
			t.Fatal("host mode accepted private container address")
		}
	}
	for _, address := range []string{
		"postgresql://localhost:5432/bloodhound", "postgresql://graph-db:5432/bloodhound", "postgresql://8.8.8.8:5432/bloodhound", "postgres://127.0.0.1:5432/bloodhound", "postgresql://user:password@127.0.0.1:5432/bloodhound", "postgresql://127.0.0.1/bloodhound", "postgresql://127.0.0.1:5432/bloodhound?host=8.8.8.8", "postgresql://127.0.0.1:5432/bloodhound?", "postgresql://127.0.0.1:5432/bloodhound#fragment", "postgresql://127.0.0.1:5432/", "postgresql://127.0.0.1:5432/a/b", "postgresql://127.0.0.1:5432/bloodhound%2fother",
	} {
		if err := validatePostgresAddress(address, false); err == nil {
			t.Fatalf("accepted unsafe address %s", address)
		}
	}
}

func TestPostgresDriverVerificationRejectsMalformedURLBeforeConnecting(t *testing.T) {
	if err := VerifyPostgresGraphDriver(t.Context(), "postgresql://%invalid", "unused", "unused", "pg"); err == nil {
		t.Fatal("accepted a malformed PostgreSQL address")
	}
}

func TestPostgresKindPreparationValidatesEverythingBeforeCatalogWrites(t *testing.T) {
	var database = &PostgresDatabase{}
	for _, kind := range []string{"", "name`", "name:Other", "name Other", "name;DELETE", "1Kind", "Kind\nOther"} {
		if err := database.PrepareKinds(t.Context(), []string{"ValidKind", kind}); err == nil {
			t.Fatalf("accepted invalid kind %q", kind)
		}
	}
	if err := database.PrepareKinds(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := database.PrepareKinds(ctx, []string{"ValidKind"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation error %v", err)
	}
}

func TestPostgresReadOnlyUsesParsedClauses(t *testing.T) {
	for _, query := range []string{"RETURN 'CREATE SET DELETE' AS text", "MATCH (n) WHERE n.objectid = $identifier RETURN n.objectid AS id", "// DELETE\nRETURN 1 AS value"} {
		if err := validatePostgresRead(query); err != nil {
			t.Fatalf("read rejected: %s: %v", query, err)
		}
	}
	for _, query := range []string{"CREATE (n)", "MATCH (n) SET n.value = 1 RETURN n", "MATCH (n) DELETE n", "CALL { CREATE (n) RETURN n } RETURN n", "CALL db.labels()", "MATCH (n) RETURN n; CREATE (other)", "RETURN ("} {
		if err := validatePostgresRead(query); err == nil {
			t.Fatalf("accepted write or invalid query: %s", query)
		}
	}
}

type postgresSchemaTestReader struct {
	rows pgx.Rows
}

type postgresScopeTestRows struct {
	pgx.Rows
	count                                        int64
	defaultGraph, nodeForeignKey, edgeForeignKey bool
	position                                     int
	err                                          error
	closed                                       bool
}

func (s *postgresScopeTestRows) Next() bool {
	if s.position != 0 {
		return false
	}
	s.position++
	return true
}
func (s *postgresScopeTestRows) Scan(destinations ...any) error {
	*destinations[0].(*int64) = s.count
	*destinations[1].(*bool) = s.defaultGraph
	*destinations[2].(*bool) = s.nodeForeignKey
	*destinations[3].(*bool) = s.edgeForeignKey
	return nil
}
func (s *postgresScopeTestRows) Err() error { return s.err }
func (s *postgresScopeTestRows) Close()     { s.closed = true }

func TestPostgresGraphScopeRejectsAdditionalGraphsAndUnvalidatedMembership(t *testing.T) {
	for _, test := range []struct {
		name                                         string
		count                                        int64
		defaultGraph, nodeForeignKey, edgeForeignKey bool
		err                                          error
		valid                                        bool
	}{
		{name: "one default graph", count: 1, defaultGraph: true, nodeForeignKey: true, edgeForeignKey: true, valid: true},
		{name: "multiple graphs", count: 2, defaultGraph: true, nodeForeignKey: true, edgeForeignKey: true},
		{name: "missing graph", count: 0, nodeForeignKey: true, edgeForeignKey: true},
		{name: "different graph", count: 1, nodeForeignKey: true, edgeForeignKey: true},
		{name: "unverified node membership", count: 1, defaultGraph: true, edgeForeignKey: true},
		{name: "unverified edge membership", count: 1, defaultGraph: true, nodeForeignKey: true},
		{name: "cursor error", count: 1, defaultGraph: true, nodeForeignKey: true, edgeForeignKey: true, err: errors.New("scope query interrupted")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rows = &postgresScopeTestRows{count: test.count, defaultGraph: test.defaultGraph, nodeForeignKey: test.nodeForeignKey, edgeForeignKey: test.edgeForeignKey, err: test.err}
			err := verifyPostgresGraphScope(t.Context(), postgresSchemaTestReader{rows: rows})
			if (err == nil) != test.valid {
				t.Fatalf("scope error %v, expected valid %v", err, test.valid)
			}
			if !rows.closed {
				t.Fatal("scope verification left its cursor open")
			}
		})
	}
}

func (s postgresSchemaTestReader) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return s.rows, nil
}

type postgresSchemaTestRows struct {
	pgx.Rows
	identities [][3]string
	position   int
	err        error
	closed     bool
}

func (s *postgresSchemaTestRows) Next() bool {
	if s.position >= len(s.identities) {
		return false
	}
	s.position++
	return true
}
func (s *postgresSchemaTestRows) Scan(destinations ...any) error {
	for index, value := range s.identities[s.position-1] {
		*destinations[index].(*string) = value
	}
	return nil
}
func (s *postgresSchemaTestRows) Err() error { return s.err }
func (s *postgresSchemaTestRows) Close()     { s.closed = true }

func TestPostgresGraphSchemaVerificationRejectsShadowedAndMissingTables(t *testing.T) {
	for _, test := range []struct {
		name       string
		identities [][3]string
		err        error
		valid      bool
	}{
		{name: "public tables", identities: [][3]string{{"graph", "public", "r"}, {"node", "public", "p"}, {"edge", "public", "p"}, {"kind", "public", "r"}}, valid: true},
		{name: "shadowed node", identities: [][3]string{{"graph", "public", "r"}, {"node", "alternate", "p"}, {"edge", "public", "p"}, {"kind", "public", "r"}}},
		{name: "temporary edge", identities: [][3]string{{"graph", "public", "r"}, {"node", "public", "p"}, {"edge", "pg_temp_3", "r"}, {"kind", "public", "r"}}},
		{name: "missing kind", identities: [][3]string{{"graph", "public", "r"}, {"node", "public", "p"}, {"edge", "public", "p"}, {"kind", "", ""}}},
		{name: "unexpected view", identities: [][3]string{{"graph", "public", "v"}}},
		{name: "incomplete answer", identities: [][3]string{{"graph", "public", "r"}}},
		{name: "failed tail", identities: [][3]string{{"graph", "public", "r"}, {"node", "public", "p"}, {"edge", "public", "p"}, {"kind", "public", "r"}}, err: errors.New("cursor interrupted")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rows = &postgresSchemaTestRows{identities: test.identities, err: test.err}
			err := verifyPostgresGraphSchema(t.Context(), postgresSchemaTestReader{rows: rows})
			if (err == nil) != test.valid {
				t.Fatalf("schema verification error %v, expected valid %v", err, test.valid)
			}
			if !rows.closed {
				t.Fatal("schema verification left the cursor open")
			}
		})
	}
}
