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
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
	"github.com/specterops/dawgs/graph"
)

type observation struct {
	SearchNanoseconds     []int64 `json:"search_nanoseconds"`
	SearchErrors          int     `json:"search_errors"`
	WriterWaitNanoseconds []int64 `json:"writer_wait_nanoseconds"`
	WriterErrors          int     `json:"writer_errors"`
}

// These are database-level search patterns, not HTTP or browser measurements.
// They use a stable ID lookup, a name-prefix search and a membership lookup.
func searchOnce(ctx context.Context, database graph.Database, index int) error {
	var statements = []string{
		"MATCH (n:Base {objectid:'Comparison_User_000000000'}) RETURN n.objectid LIMIT 20",
		"MATCH (n:Base) WHERE n.name STARTS WITH 'USER 0000000' RETURN n.objectid LIMIT 20",
		"MATCH (n:Base {objectid:'Comparison_User_000000000'})-[:MemberOf|MemberOfLocalGroup]->(g) RETURN g.objectid LIMIT 20",
	}
	return database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
		rows := transaction.Query(statements[index%len(statements)], nil)
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Error()
	})
}

func searchBaseline(ctx context.Context, database graph.Database) *observation {
	var result = &observation{}
	for index := 0; index < 150; index++ {
		started := time.Now()
		err := searchOnce(ctx, database, index)
		result.SearchNanoseconds = append(result.SearchNanoseconds, time.Since(started).Nanoseconds())
		if err != nil {
			result.SearchErrors++
		}
	}
	return result
}

func runObserved(ctx context.Context, database graph.Database, backend string, plan extension.Plan, options runner.Options, probes bool, result *measurement) (runner.Report, error) {
	var (
		gate         = postprocessing.NewGate()
		switched     = graph.NewDatabaseSwitch(ctx, database)
		report       runner.Report
		observations = &observation{}
		mutex        sync.Mutex
		readers      sync.WaitGroup
		writers      sync.WaitGroup
		done         = make(chan struct{})
	)
	switched.SetWriteGate(gate.EnterWrite)
	err := gate.Alone(ctx, time.Minute, func(turn context.Context) error {
		if probes {
			// Readers run one at a time. Writer requests arrive every 10 ms,
			// capped at 200 per run; they only acquire/release the real gate.
			// Their waiting is measured without changing the comparison input.
			readers.Add(2)
			go func() {
				defer readers.Done()
				for index := 0; ; index++ {
					select {
					case <-done:
						return
					default:
					}
					started := time.Now()
					searchError := searchOnce(ctx, database, index)
					mutex.Lock()
					observations.SearchNanoseconds = append(observations.SearchNanoseconds, time.Since(started).Nanoseconds())
					if searchError != nil {
						observations.SearchErrors++
					}
					mutex.Unlock()
					select {
					case <-done:
						return
					case <-time.After(5 * time.Millisecond):
					}
				}
			}()
			go func() {
				defer readers.Done()
				for index := 0; index < 200; index++ {
					select {
					case <-done:
						return
					default:
					}
					writers.Add(1)
					go func() {
						defer writers.Done()
						started := time.Now()
						_, release, writeError := gate.EnterWrite(ctx)
						elapsed := time.Since(started).Nanoseconds()
						if release != nil {
							release()
						}
						mutex.Lock()
						observations.WriterWaitNanoseconds = append(observations.WriterWaitNanoseconds, elapsed)
						if writeError != nil {
							observations.WriterErrors++
						}
						mutex.Unlock()
					}()
					select {
					case <-done:
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
			}()
		}
		var runError error
		report, runError = graphdb.RunNative(turn, switched, backend, plan, options)
		close(done)
		return runError
	})
	readers.Wait()
	writers.Wait()
	if probes {
		result.Observation = observations
	}
	return report, err
}

// This observer is enabled only in separate answer checks, never timed runs.
// Reach and kind lists are sets in this example. Sort those lists while retaining
// duplicate rows, so complete answers can be compared without depending on the
// database's incidental row or membership-list order.
func answerObserver(directory string) (func(string, map[string]any) error, func() error, error) {
	var files = map[string]*os.File{}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, nil, err
	}
	observe := func(name string, row map[string]any) error {
		file := files[name]
		if file == nil {
			var err error
			file, err = os.Create(filepath.Join(directory, name+".jsonl"))
			if err != nil {
				return err
			}
			files[name] = file
		}
		for _, field := range []string{"reach", "kinds"} {
			switch values := row[field].(type) {
			case []string:
				sort.Strings(values)
			case []any:
				sort.Slice(values, func(left, right int) bool { return fmt.Sprint(values[left]) < fmt.Sprint(values[right]) })
			}
		}
		return json.NewEncoder(file).Encode(row)
	}
	closeFiles := func() error {
		var failures []error
		for _, file := range files {
			failures = append(failures, file.Close())
		}
		return errors.Join(failures...)
	}
	return observe, closeFiles, nil
}
