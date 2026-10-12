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
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// A finished query hands its closed answer file back to the coordinator. Only
// the coordinator changes the plan's progress, releases files, or writes objects.
type queryAnswer struct {
	query extension.Query
	file  *answerFile
}

// calculate starts ready queries whenever a place is free. A slow query does
// not hold up the next step of an unrelated chain. Shared prerequisites run once
// and their files stay available until every consumer has finished reading.
func calculate(ctx context.Context, database Database, transaction Transaction, plan extension.Plan, space *answerSpace, report *Report) (map[string]*answerFile, error) {
	var (
		answers       = map[string]*answerFile{}
		remaining     = map[string]int{}
		started       = map[string]bool{}
		completed     = map[string]bool{}
		helperQueries = map[string]bool{}
		desired       = map[string]*answerFile{}
		finished      = make(chan queryAnswer, space.limits.Workers)
		running       int
		workers       = 1
		helpersStaged bool
		sampleBytes   = maxReportSampleBytes
	)
	ctx, cancel := context.WithCancelCause(ctx)
	defer func() {
		// A failed query cancels its peers immediately. We must still wait for
		// all of them to close their readers before rollback or file removal.
		cancel(nil)
		for running > 0 {
			<-finished
			running--
		}
	}()
	if capable, ok := database.(interface{ SupportsParallelReads() bool }); ok && capable.SupportsParallelReads() {
		workers = space.limits.Workers
	}
	for _, binding := range plan.Schema.Nodes {
		helperQueries[binding.Query] = true
	}
	for _, query := range plan.Queries {
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				remaining[strings.TrimSuffix(dependency.Name, ".cypher")]++
			}
		}
	}
	for len(completed) < len(plan.Queries) {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		for running < workers {
			query, ready := nextReadyQuery(plan.Queries, started, completed, helperQueries, running)
			if !ready || ctx.Err() != nil {
				break
			}
			// Each reader gets its own small map. The coordinator can then add
			// new answers without changing a map another query is using.
			inputs := map[string]*answerFile{}
			for _, dependency := range query.Dependencies {
				if dependency.Type == "query" {
					parameter := strings.TrimSuffix(dependency.Name, ".cypher")
					inputs[parameter] = answers[parameter]
				}
			}
			started[query.Name] = true
			running++
			separateReader := workers > 1 && !helpersStaged
			go func() {
				file := readQueryAnswers(ctx, database, transaction, separateReader, query, inputs, space, cancel)
				finished <- queryAnswer{query: query, file: file}
			}()
			if helperQueries[query.Name] {
				// A helper-producing query starts only when other readers have
				// finished. Do not start another reader before staging its objects.
				break
			}
		}
		if running == 0 {
			if err := context.Cause(ctx); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("query dependencies cannot make progress")
		}
		answer := <-finished
		running--
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		query, file := answer.query, answer.file
		answers[query.Parameter] = file
		report.Queries = append(report.Queries, query.Name)
		report.QueryRows[query.Name] = int(file.rows)
		for _, binding := range plan.Schema.Nodes {
			if binding.Query != query.Name {
				continue
			}
			// Helper IDs must be unique across the whole answer, including
			// rows far apart on disk. Sort a copy because consumers may
			// still need the original answer and its original order.
			copy, err := copyAnswers(ctx, file)
			if err != nil {
				return nil, err
			}
			sorted, err := sortAnswers(ctx, copy, "id")
			if err != nil {
				return nil, err
			}
			if err := publishHelperAnswers(ctx, transaction, binding.EffectiveOwner(plan.Schema.Owner), binding, sorted, report); err != nil {
				return nil, fmt.Errorf("helper answers from %s: %w", query.Name, err)
			}
			if err := sorted.remove(); err != nil {
				return nil, err
			}
			helpersStaged = true
		}
		for _, binding := range plan.Schema.Relationships {
			if binding.Query != query.Name {
				continue
			}
			pairs, err := checkedRelationships(ctx, file, query.OutputKind)
			if err != nil {
				return nil, fmt.Errorf("relationship answers from %s: %w", query.Name, err)
			}
			if err := validateFileEndpoints(ctx, transaction, binding, pairs); err != nil {
				return nil, err
			}
			desired[binding.Kind] = pairs
			report.DesiredCounts[binding.Kind] = pairs.rows
			report.Desired[binding.Kind] = []Pair{}
			if err := pairs.each(ctx, func(row map[string]any) error {
				sample := report.Desired[binding.Kind]
				appendReportPair(&sample, pairFromRow(row), space.limits.ReportPairs, &sampleBytes)
				report.Desired[binding.Kind] = sample
				return nil
			}); err != nil {
				return nil, err
			}
			if pairs.rows > int64(len(report.Desired[binding.Kind])) {
				report.DesiredTruncated = true
			}
		}
		// Helpers are not committed yet. Later reads use this same database
		// operation so they can see them. Publication still succeeds or fails
		// for the whole run; we never save one chain on its own.
		if helpersStaged {
			workers = 1
		}
		completed[query.Name] = true
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				remaining[strings.TrimSuffix(dependency.Name, ".cypher")]--
			}
		}
		for parameter, file := range answers {
			if remaining[parameter] == 0 {
				if err := file.remove(); err != nil {
					return nil, err
				}
				delete(answers, parameter)
			}
		}
	}
	return desired, nil
}

// Prefer ordinary reads while they can still make progress. A helper can be
// created once those reads finish, without another connection missing it.
func nextReadyQuery(queries []extension.Query, started, completed, helpers map[string]bool, running int) (extension.Query, bool) {
	var helper *extension.Query
	for _, query := range queries {
		if started[query.Name] {
			continue
		}
		ready := true
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" && !completed[dependency.Name] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		if !helpers[query.Name] {
			return query, true
		}
		if helper == nil {
			helper = &query
		}
	}
	if helper != nil && running == 0 {
		return *helper, true
	}
	return extension.Query{}, false
}

// This returns only after its independent database reader has closed. The
// shared publication operation belongs to the coordinator and stays open.
func readQueryAnswers(ctx context.Context, database Database, transaction Transaction, separateReader bool, query extension.Query, inputs map[string]*answerFile, space *answerSpace, stopPeers context.CancelCauseFunc) *answerFile {
	ctx, cancel := context.WithTimeout(ctx, space.limits.QueryTimeout)
	defer cancel()
	if separateReader {
		reader, err := database.Begin(ctx)
		if err != nil {
			stopPeers(fmt.Errorf("run %s: %w", query.Name, err))
			return nil
		}
		defer rollback(reader)
		transaction = reader
	}
	file, err := calculateQuery(ctx, transaction, query, inputs, space)
	if err != nil {
		// Stop peers before closing this connection, since closing can take
		// time. CancelCause keeps this first explanation instead of replacing
		// it with a peer's later cancellation message.
		stopPeers(fmt.Errorf("run %s: %w", query.Name, err))
	}
	return file
}

func calculateQuery(ctx context.Context, transaction Transaction, query extension.Query, answers map[string]*answerFile, space *answerSpace) (*answerFile, error) {
	var parameters = map[string]any{}
	var parameterBytes int64 = 2 // Opening and closing parameter-map braces.
	var partition *answerFile
	for _, dependency := range query.Dependencies {
		if dependency.Type != "query" {
			continue
		}
		parameter := strings.TrimSuffix(dependency.Name, ".cypher")
		parameterBytes += encodedSize(parameter) + 1 // The name, colon, and comma.
		if parameterBytes > space.limits.ParameterBytes {
			return nil, fmt.Errorf("prerequisite names exceed the parameter budget")
		}
		file := answers[parameter]
		if file == nil {
			return nil, fmt.Errorf("missing completed prerequisite %s", dependency.Name)
		}
		if dependency.Name == query.BatchDependency {
			partition = file
			continue
		}
		rows, loadedBytes, err := file.load(ctx, space.limits.ParameterBytes-parameterBytes)
		if err != nil {
			return nil, err
		}
		parameterBytes += loadedBytes
		parameters[parameter] = rows
	}
	if query.Reach != nil {
		if partition != nil {
			return nil, fmt.Errorf("a reachability step cannot also split its input")
		}
		return calculateReach(ctx, transaction, query, parameters, space)
	}
	writer, err := space.create()
	if err != nil {
		return nil, err
	}
	defer writer.file.Close()
	var overlap *groupOverlap
	if query.Overlap != nil {
		overlap, err = newGroupOverlap(ctx, query, parameters, space.limits)
		if err != nil {
			return nil, err
		}
	}
	execute := func() error {
		return stream(ctx, transaction, query.Text, parameters, func(row map[string]any) error {
			if query.OutputKind != "" {
				if row["kind"] != query.OutputKind {
					return fmt.Errorf("query %s returned a kind different from its declared literal %s", query.Name, query.OutputKind)
				}
			}
			if overlap != nil {
				if query.OutputKind != "" {
					delete(row, "kind")
				}
				return overlap.emit(ctx, row, func(result map[string]any) error {
					if query.OutputKind != "" {
						result["kind"] = query.OutputKind
					}
					return writer.append(result)
				})
			}
			return writer.append(row)
		})
	}

	if partition == nil {
		if err := execute(); err != nil {
			return nil, err
		}
	} else {
		// The author explicitly promises that each input row is an independent
		// calculation. We combine complete answers; publication still waits for
		// every input batch. Ordinary queries are never split by guessing.
		parameter := strings.TrimSuffix(query.BatchDependency, ".cypher")
		var batch = Rows{}
		var bytes int64 = 2 // The batched list also needs brackets.
		flush := func() error {
			parameters[parameter] = batch
			if err := execute(); err != nil {
				return err
			}
			batch = Rows{}
			bytes = 2
			return nil
		}
		if err := partition.each(ctx, func(row map[string]any) error {
			size := encodedSize(row)
			if size+2+parameterBytes > space.limits.ParameterBytes {
				return fmt.Errorf("one batched prerequisite exceeds the parameter budget")
			}
			if len(batch) > 0 && (len(batch) >= space.limits.BatchRows || bytes+size+parameterBytes > space.limits.ParameterBytes) {
				if err := flush(); err != nil {
					return err
				}
			}
			batch = append(batch, row)
			bytes += size
			return nil
		}); err != nil {
			return nil, err
		}
		if len(batch) > 0 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	return writer.finish()
}

func copyAnswers(ctx context.Context, input *answerFile) (*answerFile, error) {
	writer, err := input.space.create()
	if err != nil {
		return nil, err
	}
	defer writer.file.Close()
	if err := input.each(ctx, writer.append); err != nil {
		return nil, err
	}
	return writer.finish()
}

func checkedPairs(ctx context.Context, input *answerFile) (*answerFile, error) {
	return checkedConnections(ctx, input, false, "")
}

func checkedRelationships(ctx context.Context, input *answerFile, outputKinds ...string) (*answerFile, error) {
	var outputKind string
	if len(outputKinds) > 0 {
		outputKind = outputKinds[0]
	}
	return checkedConnections(ctx, input, true, outputKind)
}

// Native comparisons need only endpoint pairs. Publication also checks the
// optional properties and rejects contradictory answers for the same pair.
func checkedConnections(ctx context.Context, input *answerFile, withProperties bool, outputKind string) (*answerFile, error) {
	copy, err := input.space.create()
	if err != nil {
		return nil, err
	}
	defer copy.file.Close()
	if err := input.each(ctx, func(row map[string]any) error {
		if withProperties {
			if outputKind != "" {
				if row["kind"] != outputKind {
					return fmt.Errorf("relationship output kind must be %s", outputKind)
				}
				delete(row, "kind") // Publication uses the already validated binding.
			}
			checked, err := checkedRelationship(row)
			if err != nil {
				return err
			}
			row = checked
		} else {
			if _, err := readPairs(Rows{row}); err != nil {
				return err
			}
		}
		return copy.append(row)
	}); err != nil {
		return nil, err
	}
	unsorted, err := copy.finish()
	if err != nil {
		return nil, err
	}
	sorted, err := sortAnswers(ctx, unsorted, "source", "target")
	if err != nil {
		return nil, err
	}
	writer, err := input.space.create()
	if err != nil {
		return nil, err
	}
	defer writer.file.Close()
	var previous Pair
	var previousProperties any
	if err := sorted.each(ctx, func(row map[string]any) error {
		pair := pairFromRow(row)
		if pair == previous {
			if withProperties && !samePropertyValue(previousProperties, row["properties"]) {
				return fmt.Errorf("the same source and target have conflicting relationship properties; return one agreed property map per connection")
			}
			return nil
		}
		previous = pair
		previousProperties = row["properties"]
		return writer.append(row)
	}); err != nil {
		return nil, err
	}
	if err := sorted.remove(); err != nil {
		return nil, err
	}
	return writer.finish()
}

func pairFromRow(row map[string]any) Pair {
	return Pair{Source: row["source"].(string), Target: row["target"].(string)}
}
