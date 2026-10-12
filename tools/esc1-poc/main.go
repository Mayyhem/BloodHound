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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/bloodhound"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/modelview"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

type configuration struct {
	command            string
	extensionDirectory string
	workDirectory      string
	composePath        string
	timeout            time.Duration
	limits             runner.Limits
	changesPath        string
	jobID              int64
	collectionPaths    []string
}

type runResult struct {
	runner.Report
	Error                string `json:"error,omitempty"`
	StaleResults         bool   `json:"stale_results"`
	PeakProcessMemoryKiB int64  `json:"peak_process_memory_kib"`
}

type plannedQuery struct {
	File         string              `json:"file"`
	Role         string              `json:"role"`
	Kind         string              `json:"kind,omitempty"`
	Dependencies []plannedDependency `json:"dependencies"`
}

type plannedDependency struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func main() {
	// A service runs until stopped; the one-shot command's two-hour timeout
	// must not shut down the rule upload page after deployment.
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serveRules(ctx, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	var (
		configuration, err  = parseArguments(os.Args[1:])
		signalContext, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	)
	defer stop()
	if err != nil && configuration.command == "run" {
		err = finishRun(configuration.workDirectory, runner.Report{}, err)
	}
	if err == nil {
		timeoutContext, cancel := context.WithTimeout(signalContext, configuration.timeout)
		defer cancel()
		err = execute(timeoutContext, configuration)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArguments(arguments []string) (configuration, error) {
	var (
		configuration = configuration{extensionDirectory: "extension", workDirectory: "work", composePath: "compose.yaml", timeout: 2 * time.Hour, limits: runner.DefaultLimits()}
		flags         *flag.FlagSet
	)
	if len(arguments) == 0 {
		return configuration, fmt.Errorf("usage: esc1-poc <plan|schema|model|run|compare> [flags] [collection.json ...]")
	}
	configuration.command = arguments[0]
	switch configuration.command {
	case "plan", "schema", "model", "run", "compare":
	default:
		return configuration, fmt.Errorf("unknown command %q", configuration.command)
	}
	if selectedGraphDriver() == "pg" {
		configuration.composePath = "compose.pg.yaml"
	}
	flags = flag.NewFlagSet(configuration.command, flag.ContinueOnError)
	flags.StringVar(&configuration.composePath, "compose", configuration.composePath, "Compose file for the verified local application")
	flags.StringVar(&configuration.workDirectory, "work", configuration.workDirectory, "private work directory shared by commands for this installation")
	flags.StringVar(&configuration.extensionDirectory, "extension", configuration.extensionDirectory, "extension directory")
	flags.DurationVar(&configuration.timeout, "timeout", configuration.timeout, "overall operation timeout")
	if configuration.command == "run" {
		flags.IntVar(&configuration.limits.Workers, "workers", configuration.limits.Workers, "maximum independent read queries at once (1–5)")
		flags.IntVar(&configuration.limits.BatchRows, "batch-rows", configuration.limits.BatchRows, "rows per database write or declared input batch")
		flags.Int64Var(&configuration.limits.ParameterBytes, "parameter-bytes", configuration.limits.ParameterBytes, "maximum encoded prerequisite bytes per query")
		flags.Int64Var(&configuration.limits.RowBytes, "row-bytes", configuration.limits.RowBytes, "maximum encoded bytes in one answer row")
		flags.Int64Var(&configuration.limits.SortBytes, "sort-bytes", configuration.limits.SortBytes, "working allowance per file sort")
		flags.Int64Var(&configuration.limits.DiskBytes, "temporary-bytes", configuration.limits.DiskBytes, "maximum live temporary answer bytes across this run")
		flags.Int64Var(&configuration.limits.ResultRows, "answer-rows", configuration.limits.ResultRows, "maximum rows in one complete answer")
		flags.Int64Var(&configuration.limits.ChangedObjects, "changed-objects", configuration.limits.ChangedObjects, "maximum helper, relationship, and saved-record changes in the final transaction")
		flags.DurationVar(&configuration.limits.QueryTimeout, "query-timeout", configuration.limits.QueryTimeout, "maximum time per complete query, including all its input batches")
		flags.IntVar(&configuration.limits.ReportPairs, "report-pairs", configuration.limits.ReportPairs, "maximum example pairs per kind or comparison side in reports")
		flags.StringVar(&configuration.changesPath, "changes", "", "optional confirmed-change JSON; omitted means unknown coverage")
		flags.Int64Var(&configuration.jobID, "job-id", 0, "existing successful ingestion job instead of new collection files")
	}
	if err := flags.Parse(arguments[1:]); err != nil {
		return configuration, err
	}
	configuration.collectionPaths = flags.Args()
	configuration.limits.RunTimeout = configuration.timeout
	if err := configuration.limits.Validate(); err != nil {
		return configuration, err
	}

	if configuration.timeout <= 0 {
		return configuration, fmt.Errorf("timeout must be positive")
	}
	if configuration.command == "run" {
		if configuration.jobID < 0 || (configuration.jobID > 0) == (len(configuration.collectionPaths) > 0) {
			return configuration, fmt.Errorf("run needs collection files or a positive -job-id, exclusively")
		}
	} else if len(configuration.collectionPaths) != 0 {
		return configuration, fmt.Errorf("%s does not accept collection files", configuration.command)
	}
	return configuration, nil
}

func execute(ctx context.Context, configuration configuration) error {
	switch configuration.command {
	case "plan":
		plan, err := extension.Load(configuration.extensionDirectory)
		if err != nil {
			return err
		}
		return printJSON(describePlan(plan))
	case "model":
		// Writing to standard output also works from the command container.
		// No credentials, Docker connection, or running database are needed.
		definitions, err := extension.LoadDefinitions(configuration.extensionDirectory)
		if err != nil {
			return err
		}
		var contents [][]byte
		for _, definition := range definitions {
			encoded, err := json.Marshal(definition)
			if err != nil {
				return err
			}
			contents = append(contents, encoded)
		}
		page, err := modelview.Render(contents...)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(page)
		return err
	case "schema":
		return uploadSchema(ctx, configuration)
	case "run":
		return executeRun(ctx, configuration)
	case "compare":
		return comparePublished(ctx, configuration)
	default:
		return fmt.Errorf("unknown command")
	}
}

func describePlan(plan extension.Plan) []plannedQuery {
	var queries = make([]plannedQuery, 0, len(plan.Queries))
	for _, query := range plan.Queries {
		entry := plannedQuery{File: query.Name, Role: "intermediate", Dependencies: []plannedDependency{}}
		for _, binding := range plan.Schema.Nodes {
			if binding.Query == query.Name {
				entry.Role = "node"
				entry.Kind = binding.Kind
			}
		}
		for _, binding := range plan.Schema.Relationships {
			if binding.Query == query.Name {
				entry.Role = "relationship"
				entry.Kind = binding.Kind
			}
		}
		for _, dependency := range query.Dependencies {
			entry.Dependencies = append(entry.Dependencies, plannedDependency{Type: dependency.Type, Name: dependency.Name})
		}
		queries = append(queries, entry)
	}
	return queries
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// This CLI controls one prepared stack. Letting the client addresses point
// elsewhere would make stopping that stack an unreliable writer exclusion.
func validatePreparedEndpoints() error {
	var endpoints = []struct{ name, required string }{{"BH_URL", "http://127.0.0.1:18080"}}
	switch selectedGraphDriver() {
	case "neo4j":
		endpoints = append(endpoints, struct{ name, required string }{"NEO4J_URI", "bolt://127.0.0.1:17687"}, struct{ name, required string }{"NEO4J_DATABASE", "neo4j"})
	case "pg":
		endpoints = append(endpoints, struct{ name, required string }{"POSTGRES_URI", "postgresql://127.0.0.1:15432/bloodhound"})
	default:
		return fmt.Errorf("GRAPH_DRIVER must be neo4j or pg")
	}
	for _, endpoint := range endpoints {
		if environment(endpoint.name, endpoint.required) != endpoint.required {
			return fmt.Errorf("%s must use the prepared stack default %s", endpoint.name, endpoint.required)
		}
	}
	return nil
}

func localClient() (*bloodhound.Client, error) {
	if err := validatePreparedEndpoints(); err != nil {
		return nil, err
	}
	return bloodhound.New(environment("BH_URL", "http://127.0.0.1:18080"), os.Getenv("BH_TOKEN"))
}

func localDatabase(ctx context.Context) (graphdb.Connection, error) {
	if err := validatePreparedEndpoints(); err != nil {
		return nil, err
	}
	if selectedGraphDriver() == "pg" {
		return graphdb.OpenPostgres(ctx, environment("POSTGRES_URI", "postgresql://127.0.0.1:15432/bloodhound"), environment("POSTGRES_USER", "bloodhound"), environment("POSTGRES_PASSWORD", "experiment-only"))
	}
	return graphdb.Open(ctx,
		environment("NEO4J_URI", "bolt://127.0.0.1:17687"),
		environment("NEO4J_USER", "neo4j"),
		environment("NEO4J_PASSWORD", "experiment-only"),
		environment("NEO4J_DATABASE", "neo4j"),
	)
}

func closeDatabase(database graphdb.Connection) {
	var cleanupContext, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = database.Close(cleanupContext)
}

func uploadSchema(ctx context.Context, configuration configuration) error {
	var (
		lock, err   = lockWork(configuration.workDirectory)
		plan        extension.Plan
		client      *bloodhound.Client
		payload     []byte
		application deployment
	)
	if err != nil {
		return err
	}
	defer lock.Close()
	plan, err = extension.Load(configuration.extensionDirectory)
	if err != nil {
		return err
	}
	application, err = configuredDeployment(configuration)
	if err != nil {
		return err
	}
	payload, err = plan.Definition.BloodHoundJSON()
	if err != nil {
		return err
	}
	if err := application.verify(ctx, plan.Schema.ReferenceCommit); err != nil {
		return err
	}
	client, err = application.client()
	if err != nil {
		return err
	}
	if err := application.ready(ctx); err != nil {
		return err
	}
	if err := client.UploadSchema(ctx, payload); err != nil {
		return err
	}
	return printJSON(map[string]bool{"uploaded": true})
}

func loadChanges(path string) (runner.Changes, error) {
	var (
		changes  runner.Changes
		contents []byte
		err      error
		trailing any
	)
	if path == "" {
		return changes, nil
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		return changes, err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&changes); err != nil {
		return changes, err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return changes, fmt.Errorf("changes file must contain one JSON object")
	}
	return changes, nil
}

func executeRun(ctx context.Context, configuration configuration) error {
	var (
		report    runner.Report
		lock, err = lockWork(configuration.workDirectory)
	)
	// Keep the lock through recovery and logging, so a new CLI command cannot
	// upload evidence while an earlier command is still restarting the service.
	if err == nil {
		defer lock.Close()
		report, err = calculateAndPublish(ctx, configuration)
	}
	return finishRun(configuration.workDirectory, report, err)
}

func finishRun(workDirectory string, report runner.Report, err error) error {
	var result = runResult{Report: report, PeakProcessMemoryKiB: peakProcessMemory()}
	if err != nil {
		result.Error = runErrorMessage(report, err)
		result.StaleResults = !report.Committed
	}
	if logError := appendRun(workDirectory, result); logError != nil {
		err = errors.Join(err, fmt.Errorf("save local run log (the graph remains authoritative): %w", logError))
		result.Error = runErrorMessage(report, err)
		result.StaleResults = !report.Committed
	}
	if outputError := printJSON(result); outputError != nil {
		err = errors.Join(err, outputError)
	}
	return err
}

func runErrorMessage(report runner.Report, err error) string {
	if report.Committed {
		return err.Error()
	}
	return "Existing experimental results may be out of date. " + err.Error()
}

func calculateAndPublish(ctx context.Context, configuration configuration) (runner.Report, error) {
	var (
		report      runner.Report
		plan, err   = extension.Load(configuration.extensionDirectory)
		changes     runner.Changes
		client      *bloodhound.Client
		receipt     bloodhound.Receipt
		application deployment
	)
	if err != nil {
		return report, err
	}
	changes, err = loadChanges(configuration.changesPath)
	if err != nil {
		return report, fmt.Errorf("read confirmed changes: %w", err)
	}
	if len(configuration.collectionPaths) != 0 {
		// Native processing can change more than the uploaded kinds suggest.
		// A new upload therefore always runs the complete query set. Confirmed
		// change lists are useful only for controlled edits after a completed job.
		changes.CoverageKnown = false
	}
	application, err = configuredDeployment(configuration)
	if err != nil {
		return report, err
	}
	if err := application.verify(ctx, plan.Schema.ReferenceCommit); err != nil {
		return report, err
	}
	client, err = application.client()
	if err != nil {
		return report, err
	}
	if err := application.ready(ctx); err != nil {
		return report, err
	}
	if configuration.jobID > 0 {
		receipt, err = client.Wait(ctx, configuration.jobID)
	} else {
		receipt, err = client.UploadAndWait(ctx, configuration.collectionPaths)
	}
	if receipt.JobID > 0 {
		report.BatchID = fmt.Sprintf("upload-%d", receipt.JobID)
	}
	if err != nil {
		return report, err
	}
	if err := client.CheckAllJobsComplete(ctx); err != nil {
		return report, fmt.Errorf("confirm complete ingestion history before publication: %w", err)
	}
	if err := client.WaitUntilIdle(ctx); err != nil {
		return report, fmt.Errorf("wait for native background work before publication: %w", err)
	}
	err = application.withBloodHoundStopped(ctx, func(ctx context.Context) error {
		database, err := application.database(ctx)
		if err != nil {
			return err
		}
		defer closeDatabase(database)
		var runError error
		temporaryDirectory, prepareError := prepareTemporaryDirectory(configuration.workDirectory)
		if prepareError != nil {
			return prepareError
		}
		configuration.limits.RunTimeout = configuration.timeout
		report, runError = runner.Run(ctx, database, plan, runner.Options{BatchID: report.BatchID, IngestionComplete: true, WritersPaused: true, Changes: changes, Limits: configuration.limits, TemporaryDirectory: temporaryDirectory})
		report.ContainerImages = application.images()
		return runError
	})
	return report, err
}

// This command reads both lists in one transaction and always rolls it back.
// It never repairs a mismatch or changes the native analysis answers.
func comparePublished(ctx context.Context, configuration configuration) error {
	var (
		lock, err   = lockWork(configuration.workDirectory)
		plan        extension.Plan
		application deployment
	)
	if err != nil {
		return err
	}
	defer lock.Close()
	plan, err = extension.Load(configuration.extensionDirectory)
	if err != nil {
		return err
	}
	if !plan.Schema.CompareNativeESC1 {
		return fmt.Errorf("compare requires an extension with compare_native_esc1 enabled; other extensions use their own written expectations")
	}
	application, err = configuredDeployment(configuration)
	if err != nil {
		return err
	}
	if err := application.verify(ctx, plan.Schema.ReferenceCommit); err != nil {
		return err
	}
	client, err := application.client()
	if err != nil {
		return err
	}
	if err := application.ready(ctx); err != nil {
		return err
	}
	if err := client.CheckAllJobsComplete(ctx); err != nil {
		return err
	}
	if err := client.WaitUntilIdle(ctx); err != nil {
		return err
	}
	return application.withBloodHoundStopped(ctx, func(ctx context.Context) error {
		return compareStoppedGraph(ctx, plan, application, configuration)
	})
}

func compareStoppedGraph(ctx context.Context, plan extension.Plan, application deployment, configuration configuration) error {
	var (
		database    graphdb.Connection
		transaction runner.Transaction
		err         error
	)
	database, err = application.database(ctx)
	if err != nil {
		return err
	}
	defer closeDatabase(database)
	transaction, err = database.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = transaction.Rollback(cleanupContext)
	}()
	temporaryDirectory, err := prepareTemporaryDirectory(configuration.workDirectory)
	if err != nil {
		return err
	}
	comparison, err := runner.ComparePublished(ctx, transaction, temporaryDirectory, configuration.limits)
	if err != nil {
		return err
	}
	if err := printJSON(comparison); err != nil {
		return err
	}
	if comparison.MissingCount != 0 || comparison.ExtraCount != 0 {
		return fmt.Errorf("published ESC1 differs from native ESC1")
	}
	return nil
}

func peakProcessMemory() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return usage.Maxrss
}

func appendRun(directory string, result runResult) error {
	var (
		file *os.File
		err  error
	)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	logLock, err := os.OpenFile(filepath.Join(directory, "runs.lock"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logLock.Close()
	// Even a command rejected by the main work lock may report that failure.
	// Give log rotation its own short lock so that report cannot race a run.
	if err := syscall.Flock(int(logLock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another process is saving a run report: %w", err)
	}
	defer syscall.Flock(int(logLock.Fd()), syscall.LOCK_UN)
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(encoded) > 8*1024*1024 {
		return fmt.Errorf("run report exceeds the 8 MiB log record limit")
	}
	path := filepath.Join(directory, "runs.jsonl")
	if info, err := os.Stat(path); err == nil && info.Size()+int64(len(encoded))+1 > 8*1024*1024 {
		// The work-directory lock prevents another run from rotating this log
		// while it is in use. Keep four older files and the current file.
		if err := os.Remove(path + ".4"); err != nil && !os.IsNotExist(err) {
			return err
		}
		for index := 3; index >= 1; index-- {
			if err := os.Rename(fmt.Sprintf("%s.%d", path, index), fmt.Sprintf("%s.%d", path, index+1)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err = os.OpenFile(filepath.Join(directory, "runs.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func printJSON(value any) error {
	var encoder = json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// This is called only while the work-directory lock is held. Temporary answers
// from a killed process are never resumed: their completeness is unknowable.
func prepareTemporaryDirectory(workDirectory string) (string, error) {
	var directory = filepath.Join(workDirectory, "answers")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "calculation-") {
			if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
				return "", err
			}
		}
	}
	return directory, nil
}
