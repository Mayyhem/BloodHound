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

// Package graphdb is the small connection between our runner and Neo4j's
// official Go driver. All graph-specific publication rules live in runner.
package graphdb

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

type Connection interface {
	runner.Database
	Close(context.Context) error
}

type Database struct {
	driver        neo4j.DriverWithContext
	name          string
	endpointKinds []string
}

type transaction struct {
	database    *Database
	session     neo4j.SessionWithContext
	transaction neo4j.ExplicitTransaction
	closed      bool
	endpointIDs map[string]int64
}

func Open(ctx context.Context, address, username, password, name string) (*Database, error) {
	return open(ctx, address, username, password, name, false)
}

// OpenContainer connects to a numeric private address after the caller verifies
// the Docker container and network that own it.
func OpenContainer(ctx context.Context, address, username, password, name string) (*Database, error) {
	return open(ctx, address, username, password, name, true)
}

func validateAddress(address string, container bool) error {
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
	// Bolt uses this server directly. Routing schemes could send the driver
	// to other servers, outside the verified experiment.
	if (parsed.Scheme != "bolt" && parsed.Scheme != "bolt+s") || !allowedHost || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || (parsed.Path != "" && parsed.Path != "/") {
		if container {
			return fmt.Errorf("Neo4j container address must use bolt or bolt+s at a numeric private IP address")
		}
		return fmt.Errorf("Neo4j address must use bolt://127.0.0.1:PORT or bolt+s://127.0.0.1:PORT")
	}
	return nil
}

func open(ctx context.Context, address, username, password, name string, container bool) (*Database, error) {
	var (
		driver neo4j.DriverWithContext
		err    error
	)
	if err = validateAddress(address, container); err != nil {
		return nil, err
	}
	driver, err = neo4j.NewDriverWithContext(address, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, err
	}
	if err = driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, err
	}
	return &Database{driver: driver, name: name}, nil
}

func (s *Database) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}

func (s *Database) Begin(ctx context.Context) (runner.Transaction, error) {
	var session = s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.name, AccessMode: neo4j.AccessModeWrite})
	explicit, err := session.BeginTransaction(ctx, func(configuration *neo4j.TransactionConfig) {
		configuration.Timeout = 2 * time.Hour
		if deadline, ok := ctx.Deadline(); ok {
			configuration.Timeout = time.Until(deadline)
		}
	})
	if err != nil {
		_ = session.Close(ctx)
		return nil, err
	}
	return &transaction{database: s, session: session, transaction: explicit}, nil
}

func (s *transaction) validateRead(ctx context.Context, query string, parameters map[string]any) error {
	// EXPLAIN asks Neo4j to examine a query without executing it. We use the
	// database's own read/write classification, rather than guessing from words
	// inside comments or strings.
	// Neo4j 4.4 will not accept writes after EXPLAIN in the same transaction.
	// Examine the plan in a separate session; execute the actual query below
	// inside our one publication transaction. EXPLAIN reads no answer rows and
	// changes no data, so it need not see our unfinished helper-node changes.
	var inspection = s.database.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.database.name, AccessMode: neo4j.AccessModeRead})
	defer inspection.Close(ctx)
	result, err := inspection.Run(ctx, "EXPLAIN\n"+query, parameters)
	if err != nil {
		return err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return err
	}
	if summary.StatementType() != neo4j.StatementTypeReadOnly {
		return fmt.Errorf("analysis queries must only read the graph")
	}
	return nil
}

func (s *transaction) Read(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	if err := s.validateRead(ctx, query, parameters); err != nil {
		return nil, err
	}
	return s.execute(ctx, query, parameters)
}

func (s *transaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := s.validateRead(ctx, query, parameters); err != nil {
		return err
	}
	return s.stream(ctx, query, parameters, consume)
}

func (s *transaction) Write(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	return s.execute(ctx, query, parameters)
}

func (s *transaction) execute(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	var rows = runner.Rows{}
	if err := s.stream(ctx, query, parameters, func(row map[string]any) error { rows = append(rows, row); return nil }); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *transaction) stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	var result, err = s.transaction.Run(ctx, query, parameters)
	if err != nil {
		return err
	}
	for result.Next(ctx) {
		if err := consume(result.Record().AsMap()); err != nil {
			return err
		}
	}
	if err := result.Err(); err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (s *transaction) Commit(ctx context.Context) error {
	if err := s.transaction.Commit(ctx); err != nil {
		return err
	}
	s.closed = true
	// The commit already succeeded. A later connection-close error cannot turn
	// that committed result into a failed publication.
	_ = s.session.Close(ctx)
	return nil
}

func (s *transaction) Rollback(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	defer s.session.Close(ctx)
	return s.transaction.Rollback(ctx)
}

func (s *Database) SupportsParallelReads() bool { return true }
