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
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/cypher/frontend"
	"github.com/specterops/dawgs/drivers/pg"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/ops"
	"github.com/specterops/dawgs/query"
	"github.com/specterops/dawgs/util/size"
)

type PostgresDatabase struct{ driver *pg.Driver }
type postgresTransaction struct{ transaction *pg.ExplicitTransaction }

// This ceiling applies to DAWGS path materialization, not streamed answers or
// its separately bounded translation cache. The PoC does not collect paths.
const postgresGraphQueryMemoryLimit = 32 * size.Mebibyte

func OpenPostgres(ctx context.Context, address, username, password string) (*PostgresDatabase, error) {
	return openPostgres(ctx, address, username, password, false)
}
func OpenPostgresContainer(ctx context.Context, address, username, password string) (*PostgresDatabase, error) {
	return openPostgres(ctx, address, username, password, true)
}

func validatePostgresAddress(address string, container bool) error {
	var (
		parsed, err = url.Parse(address)
		allowedHost bool
	)
	if err != nil {
		return err
	}
	if container {
		var address = net.ParseIP(parsed.Hostname())
		allowedHost = address != nil && address.IsPrivate()
	} else {
		allowedHost = parsed.Hostname() == "127.0.0.1"
	}
	if parsed.Scheme != "postgresql" || !allowedHost || parsed.Port() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || parsed.RawPath != "" || len(parsed.Path) < 2 || strings.Contains(parsed.Path[1:], "/") {
		return fmt.Errorf("PostgreSQL address requires postgresql://verified-IP:PORT/DATABASE without credentials or options")
	}
	return nil
}

func openPostgres(ctx context.Context, address, username, password string, container bool) (*PostgresDatabase, error) {
	var (
		configuration *pgxpool.Config
		pool          *pgxpool.Pool
		database      *PostgresDatabase
		err           error
	)
	if err = validatePostgresAddress(address, container); err != nil {
		return nil, err
	}
	configuration, err = pgxpool.ParseConfig(address)
	if err != nil {
		return nil, fmt.Errorf("parse verified PostgreSQL address")
	}
	configuration.ConnConfig.User = username
	configuration.ConnConfig.Password = password
	configuration.ConnConfig.Fallbacks = nil
	configuration.ConnConfig.TLSConfig = nil
	configuration.ConnConfig.RuntimeParams = map[string]string{}
	configuration.AfterConnect = postgresAfterConnect
	// Eight read workers may run alongside the publication transaction.
	configuration.MaxConns = 9
	configuration.MinConns = 0
	configuration.MinIdleConns = 0
	pool, err = pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}
	database = &PostgresDatabase{driver: pg.NewDriver(postgresGraphQueryMemoryLimit, pool)}
	if err = pool.Ping(ctx); err == nil {
		err = verifyPostgresGraphSchema(ctx, pool)
	}
	if err == nil {
		err = verifyPostgresGraphScope(ctx, pool)
	}
	if err == nil {
		err = database.driver.RefreshKinds(ctx)
	}
	if err == nil {
		err = database.driver.SetDefaultGraph(ctx, graph.Graph{Name: "default"})
	}
	if err != nil {
		_ = database.Close(ctx)
		return nil, err
	}
	return database, nil
}

type postgresSchemaReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Resolve each unqualified graph table using this role's actual search path.
// Checking current_schema alone misses a shadowed table later in the path.
func verifyPostgresGraphSchema(ctx context.Context, reader postgresSchemaReader) error {
	var (
		tables = []string{"graph", "node", "edge", "kind"}
		seen   = make(map[string]bool, len(tables))
	)
	rows, err := reader.Query(ctx, `SELECT requested.name, COALESCE(namespace.nspname, ''), COALESCE(relation.relkind::pg_catalog.text, '')
FROM pg_catalog.unnest($1::pg_catalog.text[]) AS requested(name)
LEFT JOIN pg_catalog.pg_class AS relation ON relation.oid = pg_catalog.to_regclass(requested.name)
LEFT JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace`, tables)
	if err != nil {
		return fmt.Errorf("verify PostgreSQL graph table identities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, schema, kind string
		if err := rows.Scan(&name, &schema, &kind); err != nil {
			return fmt.Errorf("read PostgreSQL graph table identity: %w", err)
		}
		if schema != "public" || (kind != "r" && kind != "p") {
			return fmt.Errorf("PostgreSQL graph table %s must resolve to a table in public", name)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("finish PostgreSQL graph table verification: %w", err)
	}
	for _, name := range tables {
		if !seen[name] {
			return fmt.Errorf("PostgreSQL graph table %s could not be verified", name)
		}
	}
	return nil
}

// DAWGS reads the parent graph tables. Require a single graph and validated
// foreign keys so every node and edge belongs to that selected graph.
func verifyPostgresGraphScope(ctx context.Context, reader postgresSchemaReader) error {
	rows, err := reader.Query(ctx, `SELECT
(SELECT pg_catalog.count(*) FROM public.graph),
(SELECT COALESCE(pg_catalog.bool_and(name = 'default'), false) FROM public.graph),
EXISTS (SELECT 1 FROM pg_catalog.pg_constraint AS constraint_definition
JOIN pg_catalog.pg_attribute AS source_column ON source_column.attrelid = constraint_definition.conrelid AND source_column.attname = 'graph_id'
JOIN pg_catalog.pg_attribute AS target_column ON target_column.attrelid = constraint_definition.confrelid AND target_column.attname = 'id'
WHERE constraint_definition.conrelid = 'public.node'::pg_catalog.regclass AND constraint_definition.confrelid = 'public.graph'::pg_catalog.regclass
AND constraint_definition.contype = 'f' AND constraint_definition.convalidated AND source_column.attnotnull
AND constraint_definition.conkey = ARRAY[source_column.attnum] AND constraint_definition.confkey = ARRAY[target_column.attnum]),
EXISTS (SELECT 1 FROM pg_catalog.pg_constraint AS constraint_definition
JOIN pg_catalog.pg_attribute AS source_column ON source_column.attrelid = constraint_definition.conrelid AND source_column.attname = 'graph_id'
JOIN pg_catalog.pg_attribute AS target_column ON target_column.attrelid = constraint_definition.confrelid AND target_column.attname = 'id'
WHERE constraint_definition.conrelid = 'public.edge'::pg_catalog.regclass AND constraint_definition.confrelid = 'public.graph'::pg_catalog.regclass
AND constraint_definition.contype = 'f' AND constraint_definition.convalidated AND source_column.attnotnull
AND constraint_definition.conkey = ARRAY[source_column.attnum] AND constraint_definition.confkey = ARRAY[target_column.attnum])`)
	if err != nil {
		return fmt.Errorf("verify PostgreSQL graph scope: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return fmt.Errorf("PostgreSQL graph scope could not be verified: %v", rows.Err())
	}
	var (
		count                                        int64
		defaultGraph, nodeForeignKey, edgeForeignKey bool
	)
	if err := rows.Scan(&count, &defaultGraph, &nodeForeignKey, &edgeForeignKey); err != nil {
		return fmt.Errorf("read PostgreSQL graph scope: %w", err)
	}
	if count != 1 || !defaultGraph {
		return fmt.Errorf("PostgreSQL PoC requires exactly one graph named default")
	}
	if !nodeForeignKey || !edgeForeignKey {
		return fmt.Errorf("PostgreSQL node and edge graph_id foreign keys must reference public.graph(id) and be validated")
	}
	if rows.Next() {
		return fmt.Errorf("PostgreSQL graph scope returned an unexpected answer")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("finish PostgreSQL graph scope verification: %w", err)
	}
	return nil
}

func (s *PostgresDatabase) Close(ctx context.Context) error { return s.driver.Close(ctx) }
func (s *PostgresDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	var transaction, err = s.driver.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &postgresTransaction{transaction: transaction}, nil
}
func validatePostgresRead(query string) error {
	var _, err = frontend.ParseCypher(frontend.NewContext(&frontend.UpdatingNotAllowedClauseFilter{}, &frontend.ExplicitProcedureInvocationFilter{}, &frontend.ImplicitProcedureInvocationFilter{}), query)
	if err != nil {
		return fmt.Errorf("analysis queries must only read the graph: %w", err)
	}
	return nil
}
func (s *postgresTransaction) Read(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	if err := validatePostgresRead(query); err != nil {
		return nil, err
	}
	return s.execute(ctx, query, parameters)
}
func (s *postgresTransaction) Write(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	return nil, fmt.Errorf("PostgreSQL publication requires structured graph operations")
}
func (s *postgresTransaction) execute(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	var rows = runner.Rows{}
	if err := s.stream(ctx, query, parameters, func(row map[string]any) error { rows = append(rows, row); return nil }); err != nil {
		return nil, err
	}
	return rows, nil
}
func (s *postgresTransaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := validatePostgresRead(query); err != nil {
		return err
	}
	return s.stream(ctx, query, parameters, consume)
}
func (s *postgresTransaction) stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	var result = s.transaction.QueryContext(ctx, query, parameters)
	defer result.Close()
	for result.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row = make(map[string]any, len(result.Keys()))
		for index, key := range result.Keys() {
			row[key] = normalizePostgresValue(result.Values()[index])
		}
		if err := consume(row); err != nil {
			return err
		}
	}
	return result.Error()
}
func (s *postgresTransaction) Commit(ctx context.Context) error {
	var err = s.transaction.CommitContext(ctx)
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		return fmt.Errorf("%w: %w", runner.ErrCommitRolledBack, err)
	}
	return err
}
func (s *postgresTransaction) Rollback(ctx context.Context) error { return s.transaction.Rollback(ctx) }

// VerifyPostgresGraphDriver checks the persisted driver override in BloodHound's
// selected application database. It only reads existing tables.
func VerifyPostgresGraphDriver(ctx context.Context, address, username, password, expected string) error {
	var (
		parsed, err   = url.Parse(address)
		container     bool
		configuration *pgxpool.Config
		pool          *pgxpool.Pool
		tableExists   bool
		drivers       []string
	)
	if err != nil {
		return err
	}
	container = net.ParseIP(parsed.Hostname()) != nil && net.ParseIP(parsed.Hostname()).IsPrivate()
	if err = validatePostgresAddress(address, container); err != nil {
		return err
	}
	configuration, err = pgxpool.ParseConfig(address)
	if err != nil {
		return fmt.Errorf("parse verified PostgreSQL address")
	}
	configuration.ConnConfig.User = username
	configuration.ConnConfig.Password = password
	configuration.ConnConfig.Fallbacks = nil
	configuration.ConnConfig.TLSConfig = nil
	configuration.ConnConfig.RuntimeParams = map[string]string{}
	pool, err = pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.QueryRow(ctx, "SELECT to_regclass('database_switch') IS NOT NULL").Scan(&tableExists); err != nil {
		return err
	}
	if !tableExists {
		return fmt.Errorf("BloodHound database_switch table is missing; selected graph driver cannot be verified")
	}
	rows, err := pool.Query(ctx, "SELECT driver FROM database_switch")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var driver string
		if err = rows.Scan(&driver); err != nil {
			return err
		}
		drivers = append(drivers, driver)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(drivers) > 1 || (len(drivers) == 1 && drivers[0] != expected) {
		return fmt.Errorf("BloodHound persisted graph driver does not match %s", expected)
	}
	return nil
}

func normalizePostgresValue(value any) any {
	switch value := value.(type) {
	case int16:
		return int64(value)
	case int32:
		return int64(value)
	case graph.ID:
		return int64(value)
	default:
		return value
	}
}

func (s *postgresTransaction) PutNode(ctx context.Context, kind string, properties map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var identifier, valid = properties["objectid"].(string)
	if !valid || identifier == "" {
		return fmt.Errorf("helper objectid is required")
	}
	nodes, err := ops.FetchNodes(s.transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), identifier)))
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		_, err = s.transaction.CreateNode(graph.AsProperties(properties), graph.StringKind(kind))
		return err
	}
	if len(nodes) != 1 || len(nodes[0].Kinds) != 1 || nodes[0].Kinds[0].String() != kind || nodes[0].Properties.Map["poc_owner"] != properties["poc_owner"] {
		return fmt.Errorf("helper identity or ownership changed during publication")
	}
	for key := range nodes[0].Properties.Map {
		if _, found := properties[key]; !found {
			nodes[0].Properties.Delete(key)
		}
	}
	nodes[0].Properties.SetAll(properties)
	return s.transaction.UpdateNode(nodes[0])
}

func (s *postgresTransaction) CreateRelationships(ctx context.Context, kind, owner string, relationships []runner.Relationship) (int, error) {
	var (
		identifiers = make([]string, 0, 2*len(relationships))
		endpointIDs = make(map[string]graph.ID, 2*len(relationships))
		creations   = make([]pg.RelationshipCreation, len(relationships))
	)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(relationships) == 0 {
		return 0, nil
	}
	for index, relationship := range relationships {
		properties, err := relationshipCreationProperties(owner, relationship.Properties)
		if err != nil {
			return 0, err
		}
		creations[index].Properties = graph.AsProperties(properties)
		identifiers = append(identifiers, relationship.Source, relationship.Target)
	}
	nodes, err := ops.FetchNodes(s.transaction.Nodes().Filter(query.In(query.NodeProperty("objectid"), identifiers)))
	if err != nil {
		return 0, err
	}
	for _, node := range nodes {
		identifier, valid := node.Properties.Map["objectid"].(string)
		if !valid {
			return 0, fmt.Errorf("relationship endpoint has an invalid objectid")
		}
		if _, found := endpointIDs[identifier]; found {
			return 0, fmt.Errorf("relationship endpoint %q identifies multiple nodes", identifier)
		}
		endpointIDs[identifier] = node.ID
	}
	for index, relationship := range relationships {
		source, sourceFound := endpointIDs[relationship.Source]
		target, targetFound := endpointIDs[relationship.Target]
		if !sourceFound || !targetFound {
			return 0, fmt.Errorf("relationship endpoints must identify exactly one node")
		}
		creations[index].SourceID, creations[index].TargetID = source, target
	}
	count, err := s.transaction.CreateRelationships(ctx, graph.StringKind(kind), creations)
	return int(count), err
}

func (s *postgresTransaction) UpdateRelationships(ctx context.Context, kind, owner string, updates []runner.RelationshipUpdate) (int, error) {
	var patches = make([]pg.RelationshipPropertyUpdate, len(updates))
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := validateRelationshipUpdates(owner, updates); err != nil {
		return 0, err
	}
	if len(updates) == 0 {
		return 0, nil
	}
	for index, update := range updates {
		properties := graph.NewProperties()
		patches[index].ID = graph.ID(update.ID)
		for key, value := range update.Properties {
			if value == nil {
				patches[index].DeleteProperties = append(patches[index].DeleteProperties, key)
			} else {
				properties.Set(key, value)
			}
		}
		patches[index].Properties = properties
	}
	count, err := s.transaction.UpdateRelationshipProperties(ctx, graph.StringKind(kind), graph.AsProperties(map[string]any{"poc_owner": owner}), patches)
	return int(count), err
}

func (s *postgresTransaction) DeleteRelationships(ctx context.Context, kind, owner string, ids []int64) (int, error) {
	var graphIDs = make([]graph.ID, len(ids))
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	for index, identifier := range ids {
		if identifier < 0 {
			return 0, fmt.Errorf("invalid relationship ID")
		}
		graphIDs[index] = graph.ID(identifier)
	}
	if len(graphIDs) == 0 {
		return 0, nil
	}
	count, err := s.transaction.DeleteRelationshipIDs(ctx, graph.StringKind(kind), graph.AsProperties(map[string]any{"poc_owner": owner}), graphIDs)
	return int(count), err
}

func (s *PostgresDatabase) SupportsParallelReads() bool { return true }

// Register declared kinds before read queries so a missing kind matches no
// objects. This prepares catalog metadata without changing nodes or edges.
func (s *PostgresDatabase) PrepareKinds(ctx context.Context, kinds []string) error {
	var (
		identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
		declared   = make(graph.Kinds, 0, len(kinds))
		seen       = make(map[string]bool, len(kinds))
	)
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, kind := range kinds {
		if !identifier.MatchString(kind) {
			return fmt.Errorf("invalid declared graph kind %q", kind)
		}
		if !seen[kind] {
			declared = append(declared, graph.StringKind(kind))
			seen[kind] = true
		}
	}
	if len(declared) == 0 {
		return nil
	}
	if _, err := s.driver.SchemaManager.AssertKinds(ctx, declared); err != nil {
		return fmt.Errorf("prepare declared graph kinds: %w", err)
	}
	return nil
}

func (s *postgresTransaction) EndpointKinds(ctx context.Context, identifiers []string) (map[string][]string, error) {
	var kinds = make(map[string][]string, len(identifiers))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(identifiers) == 0 {
		return kinds, nil
	}
	nodes, err := ops.FetchNodes(s.transaction.Nodes().Filter(query.In(query.NodeProperty("objectid"), identifiers)))
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		identifier, valid := node.Properties.Map["objectid"].(string)
		if !valid {
			return nil, fmt.Errorf("relationship endpoint has an invalid objectid")
		}
		if _, found := kinds[identifier]; found {
			return nil, fmt.Errorf("relationship endpoint %q identifies multiple nodes", identifier)
		}
		kinds[identifier] = node.Kinds.Strings()
	}
	for _, identifier := range identifiers {
		if _, found := kinds[identifier]; !found {
			return nil, fmt.Errorf("relationship endpoint %q is missing", identifier)
		}
	}
	return kinds, nil
}
