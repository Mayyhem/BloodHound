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
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// This recorder belongs only to the small correctness tests. It observes the
// complete answers supplied to later queries, including answers calculated in
// Go. Performance runs use the original database without this extra copying.
type comparisonDatabase struct {
	runner.Database
	mutex   sync.Mutex
	answers map[string]map[string]bool
}

func (s *comparisonDatabase) SupportsParallelReads() bool {
	return s.Database.(interface{ SupportsParallelReads() bool }).SupportsParallelReads()
}

func (s *comparisonDatabase) PrepareKinds(ctx context.Context, kinds []string) error {
	if preparer, ok := s.Database.(interface {
		PrepareKinds(context.Context, []string) error
	}); ok {
		return preparer.PrepareKinds(ctx, kinds)
	}
	return nil
}

func (s *comparisonDatabase) PrepareEndpoints(ctx context.Context, kinds []string) error {
	if preparer, ok := s.Database.(interface {
		PrepareEndpoints(context.Context, []string) error
	}); ok {
		return preparer.PrepareEndpoints(ctx, kinds)
	}
	return nil
}

func (s *comparisonDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	transaction, err := s.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &comparisonTransaction{
		Transaction:    transaction,
		Publisher:      transaction.(runner.Publisher),
		EndpointReader: transaction.(runner.EndpointReader),
		database:       s,
	}, nil
}

type comparisonTransaction struct {
	runner.Transaction
	runner.Publisher
	runner.EndpointReader
	database *comparisonDatabase
}

func (s *comparisonTransaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	if err := s.database.record(parameters); err != nil {
		return err
	}
	// group-overlap consumes this table in Go and removes it from the SQL
	// parameters. Record its producer's answer as well, before that happens.
	if strings.Contains(query, "AS has_special_member") {
		if err := s.database.record(map[string]any{"membership": runner.Rows{}}); err != nil {
			return err
		}
		return s.Transaction.(runner.StreamingTransaction).Stream(ctx, query, parameters, func(row map[string]any) error {
			if err := s.database.record(map[string]any{"membership": runner.Rows{row}}); err != nil {
				return err
			}
			return consume(row)
		})
	}
	return s.Transaction.(runner.StreamingTransaction).Stream(ctx, query, parameters, consume)
}

func (s *comparisonTransaction) Read(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	if err := s.database.record(parameters); err != nil {
		return nil, err
	}
	return s.Transaction.Read(ctx, query, parameters)
}

func (s *comparisonDatabase) record(parameters map[string]any) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, name := range []string{"membership_reach", "ca_root_reach", "ca_auth_reach", "forest_reach", "membership", "trusted_cas"} {
		value, present := parameters[name]
		if !present {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var rows []map[string]any
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&rows); err != nil {
			return err
		}
		if s.answers[name] == nil {
			s.answers[name] = map[string]bool{}
		}
		for _, row := range rows {
			// These columns are sets: the consumers test membership and use
			// explicit object numbers for group choices. Their list order is
			// not part of the answer. Other columns are preserved verbatim.
			for _, column := range []string{"reach", "kinds"} {
				if values, ok := row[column].([]any); ok {
					sort.Slice(values, func(left, right int) bool { return values[left].(string) < values[right].(string) })
				}
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				return err
			}
			s.answers[name][string(encoded)] = true
		}
	}
	return nil
}
