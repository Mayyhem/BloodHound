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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// A finished query hands its closed answer file back to the coordinator. Only
// the coordinator changes the plan's progress, releases files, or writes objects.
// The two clock times bracket the read, so a report can show which queries
// ran at the same time.
type queryAnswer struct {
	query    extension.Query
	file     *answerFile
	started  time.Time
	finished time.Time
	err      error
}

// calculate starts ready queries whenever a place is free. A slow query does
// not hold up the next step of an unrelated chain. Shared prerequisites run once
// and their files stay available until every consumer has finished reading.
func calculate(ctx context.Context, database Database, transaction Transaction, plan extension.Plan, space *answerSpace, report *Report, stage func(extension.RelationshipBinding, *answerFile) error, validateOnlyNodes, withheldRelationships map[string]bool) (map[string]*answerFile, error) {
	var currentQuery string
	// A failing query stops its peers through this context and records itself
	// as the reason.
	peers, stopPeers := context.WithCancelCause(ctx)
	result, err := calculateQueries(peers, stopPeers, database, transaction, plan, space, report, stage, &currentQuery, validateOnlyNodes, withheldRelationships)
	if err != nil {
		var failure *queryFailure
		// When one query fails, a peer whose answers were being checked at
		// that moment stops with only "canceled". Blame the query that
		// actually failed, not the peer: otherwise a healthy query would be
		// set aside in its place.
		if errors.Is(err, context.Canceled) && errors.As(context.Cause(peers), &failure) {
			err = failure
		}
		if errors.As(err, &failure) {
			currentQuery = failure.query
			report.Execution.Phase = "read-query"
		}
		report.Execution.fail(currentQuery, report.Execution.Phase, err)
	}
	return result, err
}

type queryFailure struct {
	query string
	cause error
}

func (s *queryFailure) Error() string { return "run " + s.query + ": " + s.cause.Error() }
func (s *queryFailure) Unwrap() error { return s.cause }

func recordRead(report *Report, answer queryAnswer) {
	var (
		detail   = report.Execution.query(answer.query.Name)
		started  = answer.started
		finished = answer.finished
	)
	detail.StartedAt, detail.FinishedAt = &started, &finished
	detail.Elapsed = finished.Sub(started)
	if answer.err != nil {
		detail.State = "canceled"
		return
	}
	if answer.file != nil {
		rows := answer.file.rows
		detail.Rows = &rows
	}
}

func calculateQueries(ctx context.Context, cancel context.CancelCauseFunc, database Database, transaction Transaction, plan extension.Plan, space *answerSpace, report *Report, stage func(extension.RelationshipBinding, *answerFile) error, currentQuery *string, validateOnlyNodes, withheldRelationships map[string]bool) (map[string]*answerFile, error) {
	var (
		answers       = map[string]*answerFile{}
		remaining     = map[string]int{}
		started       = map[string]bool{}
		completed     = map[string]bool{}
		helperQueries = map[string]bool{}
		desired       = map[string]*answerFile{}
		produced      = map[string]map[string]*answerFile{}
		staged        = map[string]bool{}
		graphInputs   = map[string]bool{}
		finished      = make(chan queryAnswer, space.limits.Workers)
		running       int
		workers       = 1
		sampleBytes   = maxReportSampleBytes
		// The run writes helper objects and calculated relationships into
		// one shared transaction before saving. A query that reads any of
		// those must read on that transaction to see them. The shared
		// transaction is one lane: only one reader at a time, and the
		// coordinator takes its turn too whenever it uses the transaction
		// while queries may be running. Every other query reads on its own
		// connection, so independent chains keep overlapping even after the
		// first graph change.
		readsOnTransaction = queriesReadingStagedGraph(plan)
		transactionTurn    sync.Mutex
		// The query currently reading on the shared transaction, if any.
		transactionReader string
	)
	defer func() {
		// A failed query cancels its peers immediately. We must still wait for
		// all of them to close their readers before rollback or file removal.
		cancel(nil)
		for running > 0 {
			recordRead(report, <-finished)
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
			if dependency.Type == "relationship" {
				graphInputs[dependency.Name] = true
			}
			if dependency.Type == "query" {
				remaining[dependency.Name]++
			}
		}
	}
	// Retired types have an empty complete answer. Clear them before any
	// consumer reads the graph; the outer transaction still protects rollback.
	for _, kind := range plan.RetiredRelationships {
		file, err := combineRelationshipAnswers(ctx, space, nil)
		if err != nil {
			return nil, err
		}
		if err := stage(retiredBinding(plan, kind), file); err != nil {
			return nil, err
		}
		desired[kind] = file
		report.DesiredCounts[kind] = 0
		report.Desired[kind] = []Pair{}
	}
	for len(completed) < len(plan.Queries) {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		// Graph consumers need the complete combined type, not just the
		// producer that finished first. Wait for every reader to finish
		// before staging it, so no reader is halfway through an older view.
		if running == 0 {
			for _, binding := range plan.Schema.Relationships {
				if staged[binding.Kind] || !graphInputs[binding.Kind] {
					continue
				}
				ready := true
				for _, producer := range plan.Schema.Relationships {
					if producer.Kind == binding.Kind && !completed[producer.Query] {
						ready = false
					}
				}
				if !ready {
					continue
				}
				file, err := combineRelationshipAnswers(ctx, space, produced[binding.Kind])
				if err != nil {
					return nil, err
				}
				desired[binding.Kind] = file
				if !withheldRelationships[binding.Kind] {
					if err := stage(binding, file); err != nil {
						return nil, err
					}
				}
				staged[binding.Kind] = true
			}
		}
		for running < workers {
			query, ready := nextReadyQuery(plan, started, completed, helperQueries, staged, readsOnTransaction, transactionReader != "", running)
			if !ready || ctx.Err() != nil {
				break
			}
			// Each reader gets its own small map. The coordinator can then add
			// new answers without changing a map another query is using.
			inputs := map[string]*answerFile{}
			for _, dependency := range query.Dependencies {
				if dependency.Type == "query" {
					parameter := dependency.AnswerParameter()
					inputs[parameter] = answers[dependency.Name]
				}
			}
			report.Execution.query(query.Name).State = "running"
			started[query.Name] = true
			running++
			separateReader := workers > 1 && !readsOnTransaction[query.Name]
			if !separateReader {
				transactionReader = query.Name
			}
			go func() {
				started := time.Now()
				if !separateReader {
					transactionTurn.Lock()
				}
				file, err := readQueryAnswers(ctx, database, transaction, separateReader, query, inputs, space, cancel)
				if !separateReader {
					transactionTurn.Unlock()
				}
				finished <- queryAnswer{query: query, file: file, err: err, started: started, finished: time.Now()}
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
		recordRead(report, answer)
		if answer.query.Name == transactionReader {
			transactionReader = ""
		}
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		query, file := answer.query, answer.file
		*currentQuery = query.Name
		report.Execution.Phase = "check-answers"
		answers[query.Name] = file
		report.Queries = append(report.Queries, query.Name)
		report.QueryRows[query.Name] = int(file.rows)
		if space.observeAnswer != nil {
			if err := file.each(ctx, func(row map[string]any) error {
				return space.observeAnswer(query.Name, row)
			}); err != nil {
				return nil, fmt.Errorf("record answers from %s: %w", query.Name, err)
			}
		}
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
			// Helper objects go into the shared transaction, so the coordinator
			// takes its turn on it. A helper query runs alone, so nothing is
			// reading at this moment; the turn is taken anyway so that every
			// use of the transaction follows the same rule.
			var publishErr error
			transactionTurn.Lock()
			if validateOnlyNodes[binding.Kind] {
				publishErr = validateHelperAnswers(ctx, transaction, binding, sorted, report)
			} else {
				publishErr = publishHelperAnswers(ctx, transaction, binding, sorted, report)
			}
			transactionTurn.Unlock()
			if publishErr != nil {
				return nil, fmt.Errorf("helper answers from %s: %w", query.Name, publishErr)
			}
			if err := sorted.remove(); err != nil {
				return nil, err
			}
			count := file.rows
			report.Execution.query(query.Name).Proposed = &count
		}
		for _, binding := range plan.Schema.Relationships {
			if binding.Query != query.Name {
				continue
			}
			pairs, err := checkedRelationships(ctx, file, query.OutputKind)
			if err != nil {
				return nil, fmt.Errorf("relationship answers from %s: %w", query.Name, err)
			}
			// Endpoints are checked on the shared transaction while other
			// queries may still be reading on it, so take the turn first.
			transactionTurn.Lock()
			err = validateFileEndpoints(ctx, transaction, binding, pairs)
			transactionTurn.Unlock()
			if err != nil {
				return nil, err
			}
			if produced[binding.Kind] == nil {
				produced[binding.Kind] = map[string]*answerFile{}
			}
			produced[binding.Kind][query.Name] = pairs
			count := pairs.rows
			report.Execution.query(query.Name).Proposed = &count
		}
		// Helpers and staged relationships are not committed yet. Their declared
		// readers use this same transaction so they can see them. Publication
		// still succeeds or fails for the whole run; we never save one chain
		// on its own.
		completed[query.Name] = true
		report.Execution.query(query.Name).State = "succeeded"
		if space.inspectTestAnswer != nil && query.Name == space.testTarget {
			if running != 0 {
				return nil, fmt.Errorf("editor results require all prerequisite readers to finish")
			}
			if err := space.inspectTestAnswer(ctx, transaction, query.Name, CompleteAnswer{file: file}); err != nil {
				return nil, fmt.Errorf("inspect editor results: %w", err)
			}
		}
		*currentQuery = ""
		report.Execution.Phase = "calculation"
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				remaining[dependency.Name]--
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
	for _, binding := range plan.Schema.Relationships {
		if _, exists := desired[binding.Kind]; exists {
			continue
		}
		file, err := combineRelationshipAnswers(ctx, space, produced[binding.Kind])
		if err != nil {
			return nil, err
		}
		desired[binding.Kind] = file
	}
	for _, kind := range sortedAnswerNames(desired) {
		file := desired[kind]
		report.DesiredCounts[kind] = file.rows
		report.Desired[kind] = []Pair{}
		if err := file.each(ctx, func(row map[string]any) error {
			sample := report.Desired[kind]
			appendReportPair(&sample, pairFromRow(row), space.limits.ReportPairs, &sampleBytes)
			report.Desired[kind] = sample
			return nil
		}); err != nil {
			return nil, err
		}
		if file.rows > int64(len(report.Desired[kind])) {
			report.DesiredTruncated = true
		}
	}

	return desired, nil
}

// queriesReadingStagedGraph names the queries that must read on the shared
// transaction, because they read something this run changes before saving:
// helper objects, calculated relationships, or retired relationships that the
// run clears. A query that reads without naming kinds could see any of those,
// so it reads on the transaction too. A producer that reads its own output
// kind deliberately reads the previous graph and needs nothing from the
// transaction for that. Every other query reads on its own connection.
//
// This trusts each file's declarations, as the loader already does: it rejects
// a file that reads a kind it did not declare.
func queriesReadingStagedGraph(plan extension.Plan) map[string]bool {
	var (
		changedKinds = map[string]bool{}
		result       = map[string]bool{}
	)
	for _, binding := range plan.Schema.Nodes {
		changedKinds[binding.Kind] = true
	}
	for _, binding := range plan.Schema.Relationships {
		changedKinds[binding.Kind] = true
	}
	for _, kind := range plan.RetiredRelationships {
		changedKinds[kind] = true
	}
	for _, query := range plan.Queries {
		if query.BroadRead {
			result[query.Name] = true
			continue
		}
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" || !changedKinds[dependency.Name] || dependency.Name == query.OutputKind {
				continue
			}
			result[query.Name] = true
		}
	}
	return result
}

// Prefer ordinary reads while they can still make progress. A helper can be
// created once those reads finish, without another connection missing it.
// While one query is reading on the shared transaction, the others that need
// it wait; queries with their own connection are still started.
func nextReadyQuery(plan extension.Plan, started, completed, helpers, staged, readsOnTransaction map[string]bool, transactionBusy bool, running int) (extension.Query, bool) {
	var helper *extension.Query
	for _, query := range plan.Queries {
		if started[query.Name] || transactionBusy && readsOnTransaction[query.Name] {
			continue
		}
		ready := true
		for _, name := range plan.Prerequisites(query) {
			if !completed[name] {
				ready = false
				break
			}
		}
		for _, dependency := range query.Dependencies {
			if dependency.Type != "relationship" {
				continue
			}
			readsOwnOutput := false
			for _, binding := range plan.Schema.Relationships {
				if binding.Kind == dependency.Name && binding.Query == query.Name {
					readsOwnOutput = true
					break
				}
			}
			// Producers may read the previous graph for their own output type.
			// Waiting for its combined answer would also wait for this query.
			if readsOwnOutput {
				continue
			}
			for _, binding := range plan.Schema.Relationships {
				if binding.Kind == dependency.Name && binding.Query != query.Name && !staged[binding.Kind] {
					ready = false
				}
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
func readQueryAnswers(ctx context.Context, database Database, transaction Transaction, separateReader bool, query extension.Query, inputs map[string]*answerFile, space *answerSpace, stopPeers context.CancelCauseFunc) (*answerFile, error) {
	ctx, cancel := context.WithTimeout(ctx, space.limits.QueryTimeout)
	defer cancel()
	if separateReader {
		reader, err := database.Begin(ctx)
		if err != nil {
			stopPeers(&queryFailure{query: query.Name, cause: err})
			return nil, err
		}
		defer rollback(reader)
		transaction = reader
	}
	file, err := calculateQuery(ctx, transaction, query, inputs, space)
	if err != nil {
		// Stop peers before closing this connection, since closing can take
		// time. CancelCause keeps this first explanation instead of replacing
		// it with a peer's later cancellation message.
		stopPeers(&queryFailure{query: query.Name, cause: err})
	}
	return file, err
}

func calculateQuery(ctx context.Context, transaction Transaction, query extension.Query, answers map[string]*answerFile, space *answerSpace) (*answerFile, error) {
	var parameters = map[string]any{}
	var parameterBytes int64 = 2 // Opening and closing parameter-map braces.
	var partition *answerFile
	for _, dependency := range query.Dependencies {
		if dependency.Type != "query" {
			continue
		}
		parameter := dependency.AnswerParameter()
		parameterBytes += encodedSize(parameter) + 1 // The name, colon, and comma.
		if parameterBytes > space.limits.ParameterBytes {
			return nil, fmt.Errorf("prerequisite names exceed the parameter budget")
		}
		file := answers[parameter]
		if file == nil {
			return nil, fmt.Errorf("missing completed prerequisite %s", dependency.Name)
		}
		if dependency.AnswerParameter() == query.BatchDependency {
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
			if query.ColumnsKnown {
				if len(row) != len(query.Columns) {
					return fmt.Errorf("returned columns do not match the final RETURN")
				}
				for _, column := range query.Columns {
					if _, exists := row[column]; !exists {
						return fmt.Errorf("returned columns do not match the final RETURN")
					}
				}
			}
			if query.OutputKind != "" {
				// Relationships return kind; helpers return a one-item kinds
				// list. Their full row shapes are checked before saving either.
				if row["kind"] != query.OutputKind && !onlyKind(row["kinds"], query.OutputKind) {
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
		parameter := query.BatchDependency
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
