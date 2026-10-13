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

package graphdb

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/drivers/pg"
	"github.com/specterops/dawgs/graph"
)

// RunNative borrows the application's graph driver. The caller holds its graph
// write gate for the whole call and continues to own the driver's lifecycle.
// backend is the application's resolved graph driver, not an uploaded setting.
func RunNative(ctx context.Context, database graph.Database, backend string, plan extension.Plan, options runner.Options) (runner.Report, error) {
	if database == nil {
		return runner.Report{}, errors.New("native graph database is required")
	}
	if err := options.Limits.Validate(); err != nil {
		return runner.Report{}, err
	}
	if options.BatchID == "" || !options.IngestionComplete || !options.WritersPaused {
		return runner.Report{}, errors.New("native calculation requires completed ingestion and the graph write gate")
	}
	for _, statement := range plan.Queries {
		if statement.Text == "" {
			continue
		}
		if err := validatePostgresRead(statement.Text); err != nil {
			return runner.RejectedQueryReport(plan, options, statement.Name, err), fmt.Errorf("%s (%s): %w", statement.Name, backend, err)
		}
	}
	var driver, postgres = graph.AsDriver[*pg.Driver](database)
	switch backend {
	case "pg", "postgres", "postgresql":
		if !postgres || driver == nil {
			return runner.Report{}, errors.New("native PostgreSQL driver does not match the selected backend")
		}
		return runner.Run(ctx, &nativePostgresDatabase{PostgresDatabase: &PostgresDatabase{driver: driver}, native: database}, plan, options)
	case "neo4j":
		if postgres {
			return runner.Report{}, errors.New("native Neo4j driver does not match the selected backend")
		}
		return runNativeNeo4j(ctx, database, plan, options)
	default:
		return runner.Report{}, fmt.Errorf("unsupported native graph backend %q", backend)
	}
}

type nativePostgresDatabase struct {
	*PostgresDatabase
	native   graph.Database
	verified bool
}
type nativePostgresTransaction struct{ *postgresTransaction }

// Verify the graph tables through the borrowed driver before changing its kind
// catalog. Native connections are trusted; graph scope still must be unambiguous.
func (s *nativePostgresDatabase) PrepareKinds(ctx context.Context, kinds []string) error {
	if !s.verified {
		if err := s.native.ReadTransaction(ctx, func(transaction graph.Transaction) error {
			reader := nativePostgresSchemaReader{transaction: transaction}
			if err := verifyPostgresGraphSchema(ctx, reader); err != nil {
				return err
			}
			return verifyPostgresGraphScope(ctx, reader)
		}); err != nil {
			return err
		}
		selected, found := s.driver.DefaultGraph()
		if !found || selected.Name != "default" {
			return errors.New("native PostgreSQL default graph must be selected")
		}
		s.verified = true
	}
	return s.PostgresDatabase.PrepareKinds(ctx, kinds)
}

type nativePostgresSchemaReader struct{ transaction graph.Transaction }
type nativePostgresSchemaRows struct {
	pgx.Rows
	result graph.Result
}

func (s nativePostgresSchemaReader) Query(ctx context.Context, source string, arguments ...any) (pgx.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var parameters = make(map[string]any, len(arguments))
	for index := len(arguments) - 1; index >= 0; index-- {
		name := fmt.Sprintf("p%d", index)
		source = strings.ReplaceAll(source, fmt.Sprintf("$%d", index+1), "@"+name)
		parameters[name] = arguments[index]
	}
	return &nativePostgresSchemaRows{result: s.transaction.Raw(source, parameters)}, nil
}
func (s *nativePostgresSchemaRows) Next() bool { return s.result.Next() }
func (s *nativePostgresSchemaRows) Scan(destinations ...any) error {
	return s.result.Scan(destinations...)
}
func (s *nativePostgresSchemaRows) Err() error { return s.result.Error() }
func (s *nativePostgresSchemaRows) Close()     { s.result.Close() }

func (s *nativePostgresDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	var transaction, err = s.PostgresDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &nativePostgresTransaction{postgresTransaction: transaction.(*postgresTransaction)}, nil
}

func (s *nativePostgresTransaction) Read(ctx context.Context, source string, parameters map[string]any) (runner.Rows, error) {
	var rows = runner.Rows{}
	if err := s.Stream(ctx, source, parameters, func(row map[string]any) error { rows = append(rows, row); return nil }); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *nativePostgresTransaction) Stream(ctx context.Context, source string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := validatePostgresRead(source); err != nil {
		return err
	}
	var result = s.transaction.QueryContextWithJSONDecoder(ctx, source, parameters, func(data []byte) (any, error) {
		var value any
		if err := decodePostgresJSON(data, &value); err != nil {
			return nil, err
		}
		return value, nil
	})
	defer result.Close()
	for result.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := make(map[string]any, len(result.Keys()))
		for index, key := range result.Keys() {
			row[key] = normalizePostgresValue(result.Values()[index])
		}
		if err := consume(row); err != nil {
			return err
		}
	}
	if err := result.Error(); err != nil {
		return err
	}
	return ctx.Err()
}

// Native Neo4j still uses DAWGS's legacy driver. A blocked network call cannot
// observe a query context until it returns; its transaction timeout is enforced
// by the server. We never abandon the callback or let it outlive this call.
type nativeNeo4jDatabase struct {
	database      graph.Database
	main          *nativeNeo4jTransaction
	begun         bool
	prepared      bool
	endpointKinds []string
}

type nativeNeo4jTransaction struct {
	database        *nativeNeo4jDatabase
	transaction     graph.Transaction
	commitRequested bool
	closed          bool
	endpointIDs     map[string]int64
}

type nativeNeo4jReader struct{ database graph.Database }

func nativeTimeout(ctx context.Context) graph.TransactionOption {
	return func(configuration *graph.TransactionConfig) {
		configuration.Timeout = runner.DefaultLimits().RunTimeout
		if deadline, found := ctx.Deadline(); found {
			configuration.Timeout = time.Until(deadline)
		}
	}
}

func runNativeNeo4j(ctx context.Context, database graph.Database, plan extension.Plan, options runner.Options) (runner.Report, error) {
	var (
		started                           = time.Now()
		limits                            = options.Limits
		report                            runner.Report
		callbackError                     error
		callbackStarted, callbackFinished time.Time
		entered                           bool
		borrowed                          = &nativeNeo4jDatabase{database: database}
	)
	if limits == (runner.Limits{}) {
		limits = runner.DefaultLimits()
	}
	ctx, cancel := context.WithTimeout(ctx, limits.RunTimeout)
	defer cancel()
	if err := runner.PrepareDatabase(ctx, borrowed, plan); err != nil {
		report.PreparationElapsed = time.Since(started)
		report.Elapsed = report.PreparationElapsed
		return report, err
	}
	borrowed.prepared = true
	err := database.WriteTransaction(ctx, func(transaction graph.Transaction) error {
		callbackStarted = time.Now()
		entered = true
		borrowed.main = &nativeNeo4jTransaction{database: borrowed, transaction: transaction}
		report, callbackError = runner.Run(ctx, borrowed, plan, options)
		callbackFinished = time.Now()
		report.Committed = false
		return callbackError
	}, nativeTimeout(ctx))
	// Neo4j opens and finishes the surrounding transaction outside runner.Run.
	// Include that preparation and final save/rollback in the phase measurements.
	if entered {
		report.PreparationElapsed += callbackStarted.Sub(started)
		report.SavingElapsed += time.Since(callbackFinished)
	} else {
		report.PreparationElapsed = time.Since(started)
	}
	report.Elapsed = time.Since(started)
	if err != nil {
		report.GraphObjectsChanged = 0
		if entered && callbackError == nil && borrowed.main.commitRequested {
			err = fmt.Errorf("%w: %w", runner.ErrCommitUncertain, err)
			runner.ConfirmNativeSave(&report, err)
			return report, err
		}
		return report, err
	}
	report.Committed = borrowed.main != nil && borrowed.main.commitRequested
	runner.ConfirmNativeSave(&report, nil)
	return report, nil
}

func (s *nativeNeo4jDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.begun {
		if s.main == nil {
			return nil, errors.New("native publication callback is not open")
		}
		s.begun = true
		return s.main, nil
	}
	return &nativeNeo4jReader{database: s.database}, nil
}
func (s *nativeNeo4jDatabase) SupportsParallelReads() bool { return true }
func (s *nativeNeo4jDatabase) PrepareEndpoints(ctx context.Context, kinds []string) error {
	if s.prepared {
		return nil
	}
	var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	for _, kind := range kinds {
		if !identifier.MatchString(kind) {
			return errors.New("invalid endpoint kind")
		}
	}
	for _, kind := range kinds {
		digest := sha256.Sum256([]byte(kind))
		source := fmt.Sprintf("CREATE INDEX poc_endpoint_%x IF NOT EXISTS FOR (item:%s) ON (item.objectid)", digest[:8], quotedKind(kind))
		if err := s.database.Run(ctx, source, nil); err != nil {
			return fmt.Errorf("prepare native endpoint index: %w", err)
		}
	}
	if len(kinds) > 0 {
		seconds := int64(15 * 60)
		if deadline, found := ctx.Deadline(); found {
			seconds = min(seconds, int64(time.Until(deadline).Seconds()))
		}
		if seconds <= 0 {
			return errors.New("native endpoint preparation deadline expired")
		}
		if err := s.database.Run(ctx, "CALL db.awaitIndexes($seconds)", map[string]any{"seconds": seconds}); err != nil {
			return err
		}
	}
	s.endpointKinds = append([]string(nil), kinds...)
	return nil
}

func (s *nativeNeo4jTransaction) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", runner.ErrCommitRolledBack, err)
	}
	if s.closed {
		return fmt.Errorf("%w: native transaction is closed", runner.ErrCommitRolledBack)
	}
	s.commitRequested, s.closed = true, true
	return nil
}
func (s *nativeNeo4jTransaction) Rollback(context.Context) error { s.closed = true; return nil }
func (s *nativeNeo4jTransaction) Read(ctx context.Context, source string, parameters map[string]any) (runner.Rows, error) {
	if err := validatePostgresRead(source); err != nil {
		return nil, err
	}
	return s.execute(ctx, source, parameters)
}
func (s *nativeNeo4jTransaction) Write(context.Context, string, map[string]any) (runner.Rows, error) {
	return nil, errors.New("native publication requires typed graph operations")
}
func (s *nativeNeo4jTransaction) Stream(ctx context.Context, source string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := validatePostgresRead(source); err != nil {
		return err
	}
	return s.stream(ctx, source, parameters, consume)
}
func (s *nativeNeo4jTransaction) execute(ctx context.Context, source string, parameters map[string]any) (runner.Rows, error) {
	var rows = runner.Rows{}
	if err := s.stream(ctx, source, parameters, func(row map[string]any) error { rows = append(rows, row); return nil }); err != nil {
		return nil, err
	}
	return rows, nil
}
func (s *nativeNeo4jTransaction) stream(ctx context.Context, source string, parameters map[string]any, consume func(map[string]any) error) error {
	if s.closed {
		return errors.New("native transaction is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var result = s.transaction.Raw(source, parameters)
	for result.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := make(map[string]any, len(result.Keys()))
		for index, key := range result.Keys() {
			row[key] = result.Values()[index]
		}
		if err := consume(row); err != nil {
			return err
		}
	}
	if err := result.Error(); err != nil {
		return err
	}
	// On failure the enclosing callback rolls back the cursor. Closing a legacy
	// Neo4j result early would consume its remaining rows despite cancellation.
	result.Close()
	return ctx.Err()
}
func (s *nativeNeo4jReader) Stream(ctx context.Context, source string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := validatePostgresRead(source); err != nil {
		return err
	}
	return s.database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
		var borrowed = &nativeNeo4jTransaction{transaction: transaction}
		return borrowed.stream(ctx, source, parameters, consume)
	}, nativeTimeout(ctx))
}
func (s *nativeNeo4jReader) Read(ctx context.Context, source string, parameters map[string]any) (runner.Rows, error) {
	var rows = runner.Rows{}
	if err := s.Stream(ctx, source, parameters, func(row map[string]any) error { rows = append(rows, row); return nil }); err != nil {
		return nil, err
	}
	return rows, nil
}
func (s *nativeNeo4jReader) Write(context.Context, string, map[string]any) (runner.Rows, error) {
	return nil, errors.New("native reader cannot write")
}
func (s *nativeNeo4jReader) Commit(context.Context) error {
	return errors.New("native reader cannot commit")
}
func (s *nativeNeo4jReader) Rollback(context.Context) error { return nil }

func (s *nativeNeo4jTransaction) PutNode(ctx context.Context, kind string, properties map[string]any) error {
	var identifier, valid = properties["objectid"].(string)
	if !valid || identifier == "" {
		return fmt.Errorf("helper objectid is required")
	}
	// Object IDs identify graph objects across all types. Reusing a helper is
	// allowed only when its ID identifies one object of the expected type.
	existing, err := s.execute(ctx, "MATCH (item {objectid: $identifier}) RETURN labels(item) AS kinds", map[string]any{"identifier": identifier})
	if err != nil {
		return err
	}
	if len(existing) > 1 {
		return fmt.Errorf("helper identity changed during publication")
	}
	if len(existing) == 1 {
		kinds, valid := existing[0]["kinds"].([]any)
		if !valid || len(kinds) != 1 || kinds[0] != kind {
			return fmt.Errorf("helper identity changed during publication")
		}
	}
	rows, err := s.execute(ctx, "MERGE (item:"+quotedKind(kind)+" {objectid: $identifier}) SET item = $properties RETURN id(item) AS id", map[string]any{"identifier": identifier, "properties": properties})
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("helper identity changed during publication")
	}
	s.endpointIDs = nil
	return nil
}

func (s *nativeNeo4jTransaction) EndpointKinds(ctx context.Context, identifiers []string) (map[string][]string, error) {
	var (
		kinds       = make(map[string][]string, len(identifiers))
		endpointIDs = make(map[string]int64, len(identifiers))
	)
	if len(identifiers) == 0 {
		s.endpointIDs = endpointIDs
		return kinds, nil
	}
	if len(s.database.endpointKinds) == 0 {
		return nil, fmt.Errorf("prepare indexed endpoint kinds before endpoint publication")
	}
	statements := make([]string, len(s.database.endpointKinds))
	for index, kind := range s.database.endpointKinds {
		statements[index] = "MATCH (item:" + quotedKind(kind) + ") USING INDEX item:" + quotedKind(kind) + "(objectid) WHERE item.objectid IN $identifiers RETURN item.objectid AS objectid, id(item) AS id, labels(item) AS kinds"
	}
	rows, err := s.execute(ctx, strings.Join(statements, " UNION "), map[string]any{"identifiers": identifiers})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		identifier, identifierValid := row["objectid"].(string)
		nodeID, idValid := row["id"].(int64)
		labels, labelsValid := row["kinds"].([]any)
		if !identifierValid || !idValid || !labelsValid {
			return nil, fmt.Errorf("database returned invalid endpoint metadata")
		}
		if _, found := endpointIDs[identifier]; found {
			return nil, fmt.Errorf("relationship endpoint %q identifies multiple nodes", identifier)
		}
		endpointIDs[identifier] = nodeID
		for _, label := range labels {
			kind, valid := label.(string)
			if !valid {
				return nil, fmt.Errorf("database returned an invalid endpoint kind")
			}
			kinds[identifier] = append(kinds[identifier], kind)
		}
	}
	for _, identifier := range identifiers {
		if _, found := endpointIDs[identifier]; !found {
			return nil, fmt.Errorf("relationship endpoint %q is missing", identifier)
		}
	}
	// Retain only this bounded batch, rather than all endpoints in a large run.
	s.endpointIDs = endpointIDs
	return kinds, nil
}

func (s *nativeNeo4jTransaction) CreateRelationships(ctx context.Context, kind string, relationships []runner.Relationship) (int, error) {
	var (
		identifiers = make([]string, 0, 2*len(relationships))
		creations   = make([]map[string]any, len(relationships))
		missing     bool
	)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(relationships) == 0 {
		return 0, nil
	}
	for index, relationship := range relationships {
		properties := relationshipCreationProperties(relationship.Properties)
		creations[index] = map[string]any{"properties": properties}
		identifiers = append(identifiers, relationship.Source, relationship.Target)
		if _, found := s.endpointIDs[relationship.Source]; !found {
			missing = true
		}
		if _, found := s.endpointIDs[relationship.Target]; !found {
			missing = true
		}
	}
	if missing {
		if _, err := s.EndpointKinds(ctx, identifiers); err != nil {
			return 0, err
		}
	}
	for index, relationship := range relationships {
		creations[index]["source"] = s.endpointIDs[relationship.Source]
		creations[index]["target"] = s.endpointIDs[relationship.Target]
	}
	rows, err := s.execute(ctx, "UNWIND $rows AS row MATCH (source), (target) WHERE id(source) = row.source AND id(target) = row.target CREATE (source)-[edge:"+quotedKind(kind)+"]->(target) SET edge = row.properties RETURN id(edge) AS id", map[string]any{"rows": creations})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *nativeNeo4jTransaction) UpdateRelationships(ctx context.Context, kind string, updates []runner.RelationshipUpdate) (int, error) {
	var patches = make([]map[string]any, len(updates))
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := validateRelationshipUpdates(updates); err != nil {
		return 0, err
	}
	if len(updates) == 0 {
		return 0, nil
	}
	for index, update := range updates {
		properties := update.Properties
		if properties == nil {
			properties = map[string]any{}
		}
		patches[index] = map[string]any{"id": update.ID, "properties": properties}
	}
	rows, err := s.execute(ctx, "UNWIND $rows AS row MATCH ()-[edge:"+quotedKind(kind)+"]->() WHERE id(edge) = row.id SET edge += row.properties RETURN id(edge) AS id", map[string]any{"rows": patches})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *nativeNeo4jTransaction) DeleteRelationships(ctx context.Context, kind string, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	rows, err := s.execute(ctx, "UNWIND $ids AS identifier MATCH ()-[edge:"+quotedKind(kind)+"]->() WHERE id(edge) = identifier WITH edge, id(edge) AS old_id DELETE edge RETURN old_id", map[string]any{"ids": ids})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}
