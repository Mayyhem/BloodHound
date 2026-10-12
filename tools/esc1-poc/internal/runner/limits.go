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

package runner

import (
	"context"
	"fmt"
	"time"
)

// Limits are brakes, not permission to return incomplete answers. Reaching a
// brake fails the whole run and leaves previously published results in place.
// Byte limits measure encoded answers; Go and database working memory are extra.
type Limits struct {
	Workers        int
	BatchRows      int
	RowBytes       int64
	ParameterBytes int64
	SortBytes      int64
	DiskBytes      int64
	ResultRows     int64
	ChangedObjects int64
	QueryTimeout   time.Duration
	RunTimeout     time.Duration
	ReportPairs    int
}

func DefaultLimits() Limits {
	return Limits{Workers: 5, BatchRows: 500, RowBytes: 1 << 20,
		ParameterBytes: 32 << 20, SortBytes: 8 << 20, DiskBytes: 8 << 30,
		ResultRows: 100_000_000, ChangedObjects: 10_000_000,
		QueryTimeout: 15 * time.Minute, RunTimeout: 2 * time.Hour, ReportPairs: 100}
}

func (s Limits) checked() (Limits, error) {
	if s == (Limits{}) {
		s = DefaultLimits()
	}
	if s.Workers < 1 || s.Workers > 5 || s.BatchRows < 1 || s.BatchRows > 10000 ||
		s.RowBytes < 1 || s.ParameterBytes < s.RowBytes || s.SortBytes < s.RowBytes ||
		s.DiskBytes < s.SortBytes || s.ResultRows < 1 || s.ChangedObjects < 1 ||
		s.QueryTimeout <= 0 || s.RunTimeout <= 0 || s.ReportPairs < 0 || s.ReportPairs > 10000 {
		return s, fmt.Errorf("invalid calculation limits; all capacities must be positive, workers 1–5, batches 1–10000, and reports 0–10000 pairs")
	}
	return s, nil
}

// StreamingTransaction lets a database hand us one row at a time. The callback
// error must stop the query, and any late database error must still be returned.
type StreamingTransaction interface {
	Stream(context.Context, string, map[string]any, func(map[string]any) error) error
}

func stream(ctx context.Context, transaction Transaction, query string, parameters map[string]any, consume func(map[string]any) error) error {
	if streaming, ok := transaction.(StreamingTransaction); ok {
		return streaming.Stream(ctx, query, parameters, consume)
	}
	// Small in-memory test databases may implement only the basic interface.
	rows, err := transaction.Read(ctx, query, parameters)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consume(row); err != nil {
			return err
		}
	}
	return nil
}

// Existing-edge and comparison reads can be just as large as an extension's
// query. Give each complete read the same deadline, including its last row.
func streamWithTimeout(ctx context.Context, transaction Transaction, timeout time.Duration, query string, parameters map[string]any, consume func(map[string]any) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return stream(ctx, transaction, query, parameters, consume)
}

// Validate lets callers reject unsafe settings before uploading evidence.
func (s Limits) Validate() error { _, err := s.checked(); return err }
