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
// This command runs only against the isolated local comparison databases.
// It is a measurement tool, not an upload or application deployment command.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/specterops/bloodhound/packages/go/graphschema"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs"
	_ "github.com/specterops/dawgs/drivers/neo4j"
	"github.com/specterops/dawgs/drivers/pg"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/util/size"
)

type measurement struct {
	Action             string         `json:"action"`
	Backend            string         `json:"backend"`
	Error              string         `json:"error,omitempty"`
	Elapsed            time.Duration  `json:"operation_nanoseconds"`
	CheckElapsed       time.Duration  `json:"check_nanoseconds"`
	PreparationElapsed time.Duration  `json:"preparation_nanoseconds"`
	CalculationElapsed time.Duration  `json:"calculation_nanoseconds"`
	Report             *runner.Report `json:"report,omitempty"`
	Connections        int64          `json:"connections"`
	AnswerHash         string         `json:"answer_sha256"`
	IdentityHash       string         `json:"saved_identity_sha256,omitempty"`
	SavedHash          string         `json:"saved_outputs_sha256,omitempty"`
	RunRecordHash      string         `json:"run_record_sha256,omitempty"`
	Limits             runner.Limits  `json:"limits"`
	Observation        *observation   `json:"observation,omitempty"`
	Dataset            *datasetReport `json:"dataset,omitempty"`
}

func main() {
	var (
		probes       = flag.Bool("probes", false, "measure simultaneous database searches and graph-writer waiting")
		action       = flag.String("action", "run", "run, native, load, initialize, reset, clear, change, remove-one, or remove-last")
		objects      = flag.Int("objects", 1000, "total objects in a generated graph")
		members      = flag.Int("members", 100, "users in the relevant membership graph")
		authorities  = flag.Int("authorities", 2, "authorities sharing certificate trust and a template")
		shape        = flag.String("answer-shape", "users", "users for a large answer or groups for a compact answer")
		directory    = flag.String("extension", "extension", "complete extension directory")
		changesPath  = flag.String("changes", "", "confirmed changes JSON; empty means full recalculation")
		batchRows    = flag.Int("batch-rows", 500, "same batch size for every comparison")
		queryTimeout = flag.Duration("query-timeout", 15*time.Minute, "same per-query limit for every comparison")
		runTimeout   = flag.Duration("run-timeout", 2*time.Hour, "same whole-run limit for every comparison")
		result       measurement
	)
	flag.Parse()
	result.Action = *action
	result.Limits = runner.DefaultLimits()
	result.Limits.BatchRows, result.Limits.QueryTimeout, result.Limits.RunTimeout = *batchRows, *queryTimeout, *runTimeout
	if err := result.Limits.Validate(); err != nil {
		result.Error = err.Error()
	} else {
		err := execute(*action, *directory, *changesPath, *probes, datasetSettings{Objects: *objects, Members: *members, Authorities: *authorities, Shape: *shape}, &result)
		if err != nil {
			result.Error = err.Error()
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if result.Error != "" {
		os.Exit(1)
	}
}

func execute(action, directory, changesPath string, probes bool, dataset datasetSettings, result *measurement) error {
	var (
		address     = os.Getenv("CONNECTION_STRING")
		ctx, cancel = context.WithTimeout(context.Background(), result.Limits.RunTimeout)
	)
	defer cancel()
	parsed, err := url.Parse(address)
	if err != nil {
		return err
	}
	// Fixed test ports and a separate PostgreSQL database prevent this tool
	// from accidentally writing to the user's BloodHound application.
	if parsed.Hostname() != "127.0.0.1" {
		return errors.New("use the isolated databases on 127.0.0.1")
	}
	switch parsed.Scheme {
	case "neo4j":
		if parsed.Port() != "27687" {
			return errors.New("the Neo4j comparison requires test port 27687")
		}
		result.Backend = "neo4j"
	case "postgresql":
		if parsed.Port() != "25432" || parsed.Path != "/cypher_comparison" {
			return errors.New("use cypher_comparison on test port 25432")
		}
		result.Backend = "pg"
	default:
		return errors.New("use neo4j or postgresql")
	}
	configuration := dawgs.Config{ConnectionString: address, GraphQueryMemoryLimit: size.Size(32 << 20)}
	if result.Backend == "pg" {
		poolConfiguration, err := pgxpool.ParseConfig(address)
		if err != nil {
			return err
		}
		pool, err := pg.NewPool(poolConfiguration)
		if err != nil {
			return err
		}
		configuration.Pool = pool
	}
	database, err := dawgs.Open(ctx, result.Backend, configuration)
	if err != nil {
		return err
	}
	defer database.Close(context.Background())
	schema := graphschema.CombinedGraphSchema("default")
	schema.Nodes = schema.Nodes.Add(graph.StringsToKinds([]string{"PoC_Vault_Base", "PoC_Vault_Container", "PoC_Vault_Account"})...)
	schema.Edges = schema.Edges.Add(graph.StringsToKinds([]string{"PoC_Vault_ContainsAccount", "PoC_ComparisonLink"})...)
	if err := database.AssertSchema(ctx, graph.Schema{DefaultGraph: schema, Graphs: []graph.Graph{schema}}); err != nil {
		return err
	}
	if action == "initialize" {
		return nil
	}
	started := time.Now()
	switch action {
	case "reset", "clear", "change", "remove-one", "remove-last":
		return changeDataset(ctx, database, action)
	case "load":
		result.Dataset, err = loadDataset(ctx, database, dataset)
		result.Elapsed = time.Since(started)
		return err
	case "probe":
		result.Observation = searchBaseline(ctx, database)
		return nil
	case "native":
		return measureNative(ctx, database, result)
	case "run":
		plan, err := extension.Load(directory)
		if err != nil {
			return err
		}
		// Native answers are checked separately and hashed in full. Neither
		// authoring style receives a precomputed native answer as input.
		plan.Schema.CompareNativeESC1 = false
		options := runner.Options{BatchID: "cypher-comparison", IngestionComplete: true, WritersPaused: true, Limits: result.Limits}
		if changesPath != "" {
			contents, err := os.ReadFile(changesPath)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(contents, &options.Changes); err != nil {
				return err
			}
		}
		closeEvidence := func() error { return nil }
		if directory := os.Getenv("COMPARISON_INTERMEDIATE"); directory != "" {
			observer, closeFiles, err := answerObserver(directory)
			if err != nil {
				return err
			}
			options.ObserveAnswer, closeEvidence = observer, closeFiles
		}
		report, runError := runObserved(ctx, database, result.Backend, plan, options, probes, result)
		result.Report, result.Elapsed = &report, time.Since(started)
		runError = errors.Join(runError, closeEvidence())
		checkStarted := time.Now()
		// Also check saved answers after failure: the caller compares their
		// hash with the restored starting state, rather than trusting counts.
		checkContext, stopCheck := context.WithTimeout(context.Background(), 5*time.Minute)
		defer stopCheck()
		checkError := errors.Join(hashConnections(checkContext, database, "PoC_ADCSESC1", result), hashSavedOutputs(checkContext, database, result))
		result.CheckElapsed = time.Since(checkStarted)
		return errors.Join(runError, checkError)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func hashConnections(ctx context.Context, database graph.Database, kind string, result *measurement) error {
	var digest = sha256.New()
	err := database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
		rows := transaction.Query("MATCH (s)-[r:"+kind+"]->(d) RETURN s.objectid AS from_object, d.objectid AS to_object ORDER BY from_object, to_object", nil)
		defer rows.Close()
		for rows.Next() {
			var source, target string
			if err := rows.Scan(&source, &target); err != nil {
				return err
			}
			if err := json.NewEncoder(digest).Encode([2]string{source, target}); err != nil {
				return err
			}
			result.Connections++
		}
		return rows.Error()
	})
	result.AnswerHash = hex.EncodeToString(digest.Sum(nil))
	return err
}
