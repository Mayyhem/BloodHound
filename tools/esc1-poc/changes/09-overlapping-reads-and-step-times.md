# Change 9: independent queries keep overlapping after graph changes, and every step records when it ran

Repository: the BloodHound clone at the repository root, branch `experimental-postprocessing`. The work is in the analysis engine under [tools/esc1-poc/internal/runner](../internal/runner), with a small addition to the Analysis Workspace page's **Last run** tab. This is analysis-engine work, not designer work, so the note lives under `tools/esc1-poc/changes`.

## The problem

Two problems, found while deciding how the Analysis Workspace page should show what runs at the same time (19 October 2026).

1. **Most of a run was single file for no good reason.** The engine lets up to five ready queries read at once. But the moment it staged its first helper object or calculated relationship, it dropped to one place for the rest of the run. Every remaining query waited in line, including queries from unrelated extensions that never read anything the run had changed. On the small lab graphs that costs seconds. On an environment with ten million objects, where one query can take minutes, it turns possible overlap into hours of waiting, and it hurts most exactly when several extensions are installed together.

2. **Nothing recorded when a step ran.** The run report kept only each step's duration and the order in which answers were accepted. Two steps' durations say nothing about whether they ran at the same time, so an operator looking at a slow run could not tell where the time went, and no picture of the last run's overlap could be drawn.

## The solution

1. **Place each query by what it declares.** Helper objects and calculated relationships are staged inside one shared database transaction before saving. A query that declares one of those kinds under `// depends-on`, or reads without naming kinds at all, must read on that transaction to see them, and only one query can read there at a time. Every other query keeps reading on its own connection, so independent chains keep overlapping after the first graph change. The shared transaction is treated as one lane: a query reading on it, and the coordinator checking endpoints or writing helpers on it, each take their turn.

2. **Record a start and finish clock time for every step that ran.** They sit beside the existing duration in the run report as `started_at` and `finished_at`. A step that never started has neither. The **Last run** tab on the page shows them as **Started** and **Finished**.

Nothing changes for extension authors. The engine trusts the same declarations it already requires: the loader rejects a file that reads a kind it did not declare, so a declaration is already the complete list of what a file reads.

## Decisions (19 October 2026)

These came out of the design conversation about showing execution order on the page.

| # | Question | Decision |
|---|---|---|
| 1 | Show a "by thread" view on the page? | No. The engine has no threads to show, only places in a queue, and nothing records which place ran what. The lane-switch idea was dropped. |
| 2 | Why do independent chains stop overlapping? | Because of one blanket rule, not a physical limit: after the first graph change, everything read inside the shared transaction. Only readers of the changed kinds need that. |
| 3 | Change the engine before measuring on a large graph? | Yes for these two changes. The gain from overlap follows from the rule alone and grows with graph size; measurement can only say how much. The timing record is a precondition for any useful measurement. |
| 4 | What stays single file? | Readers of staged helper objects, staged calculated relationships, retired relationships the run clears, and queries that read without naming kinds. A producer reading its own output kind reads the previous graph on purpose and keeps its own connection. |
| 5 | Does a helper-producing query still run alone? | Yes. That rule gives staging a clean moment with nothing else reading, and it was not the cause of the problem. |
| 6 | A picture of the last run's overlap? | Later, from the recorded times, after one run on a representative large graph shows whether it is worth drawing. Not in this change. |
| 7 | Show the new times on the page now? | Yes, two plain lines on **Last run**, so the data can be checked by eye. No timeline yet. |

## Alternatives considered

- **Keep the single-file rule and only draw it.** Honest, but it would have drawn a limitation instead of removing it. Rejected.
- **A lock around every individual read and write on the shared transaction, inside the database adapters.** Simpler to describe, but a read streams rows after the call returns, so a lock taken only inside the call would not stop a write from landing in the middle of a stream. The turn has to cover the whole read, which is why the runner takes it, not the adapter.
- **Let a transaction reader start only when nothing else is running, with no lock at all.** This avoids the lock but brings back most of the waiting: a producer finishing on its own connection needs the transaction to check its endpoints, and without a turn it would have to wait for the whole transaction read to finish before its answer could even be looked at. Rejected in favour of the turn, which lets that check wait only as long as it must.
- **Start and finish times measured in the coordinator instead of the reading goroutine.** Would include queueing time in the engine itself, which is not what an operator wants to know. The times bracket the read, exactly as the existing duration does.

## How it works

**Deciding where a query reads.** [queriesReadingStagedGraph](../internal/runner/calculate.go#L390) lists, once per run, every query that must read on the shared transaction: any query marked as a broad read by the language check, and any query whose declarations name a helper kind, a calculated relationship kind, or a retired relationship kind, other than its own output kind. The coordinator keeps that list as `readsOnTransaction` ([calculate.go:117](../internal/runner/calculate.go#L117)).

**Starting queries.** [nextReadyQuery](../internal/runner/calculate.go#L423) now skips a query that needs the transaction while another query is reading on it (`transactionBusy`), and keeps offering queries that have their own connection. When a query starts, [calculate.go:212](../internal/runner/calculate.go#L212) gives it its own connection unless it is on the list or the database allows only one reader at all. A query on the list is remembered as the `transactionReader` until its answer comes back ([calculate.go:243](../internal/runner/calculate.go#L243)).

**Taking turns.** The reading goroutine holds `transactionTurn` for the whole read when it reads on the shared transaction ([calculate.go:218](../internal/runner/calculate.go#L218)). The coordinator takes the same turn before writing or checking helper objects ([calculate.go:280](../internal/runner/calculate.go#L280)) and before checking a relationship answer's endpoints ([calculate.go:306](../internal/runner/calculate.go#L306)). Staging a relationship kind still happens only when nothing is running, as before, so it needs no turn.

**What was removed.** The `graphChangesStaged` flag and every `workers = 1` that it triggered. With them went the rule that put every read on the shared transaction after the first graph change.

**Recording times.** The goroutine notes the clock before and after its read and hands both back with the answer ([queryAnswer](../internal/runner/calculate.go#L30)). [recordRead](../internal/runner/calculate.go#L76) stores them on the step's report entry as `StartedAt` and `FinishedAt` ([report.go:70](../internal/runner/report.go#L70)) and derives the existing `Elapsed` from them, so the three can never disagree. A canceled step gets its times too, because its read did start. When a failed step is set aside and the run retried, the retry copies the times from the attempt that ran it ([partial.go:151](../internal/runner/partial.go#L151)), exactly as it already copied the duration and row count.

**On the page.** The page's outcome type gains the two optional fields ([types.ts:75](../../../cmd/ui/src/views/AnalysisWorkspace/types.ts#L75)), and **Last run** shows them under **Time** as **Started** and **Finished** ([QueryDetails.tsx:322](../../../cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx#L322)), formatted the same way as the existing "Last ran" time. Older reports without the fields show nothing extra.

**API description.** The two fields are described in the OpenAPI source ([model.analysis-execution.yaml:115](../../../packages/go/openapi/src/schemas/model.analysis-execution.yaml#L115)); `openapi.json` was regenerated with the usual `redocly bundle` command from the justfile.

## New dependencies

None. The turn is Go's standard mutex.

## Downstream effects

- **The rule service must be rebuilt together with the app when the external rule service is in use.** The worker posts each run's status to the rule service, which decodes it strictly and rejects unknown fields. An older rule service would refuse a report carrying `started_at` and `finished_at`. The lab's rule-service container had been stopped since 17 October; it was rebuilt from this source and started again as part of the deployment below.
- **Up to six database operations at once instead of five**: the five places plus the shared transaction, which now reads while other places are busy. Nothing in the limits changes; `--workers` still caps the places.
- **Which queries overlap depends on declarations.** A file that declares a helper kind or calculated relationship kind reads in the transaction's single lane; one that declares only collected kinds keeps its own connection. [DEVELOPERS.md](../DEVELOPERS.md) says so in the section on reading calculated kinds.
- **Reports are a little larger**: two timestamps per step that ran.
- The guides that described the old rule now describe the new one: [DESIGN.md](../DESIGN.md) (report contents, execution, the final transaction, the `--workers` row) and [USAGE.md](../USAGE.md) (report contents, stages).

## How it was tested

- **Engine unit tests** (`go test -count=1 -race ./internal/runner/` in `tools/esc1-poc`): pass. New or changed:
  - [TestIndependentQueryOverlapsWithTransactionReaderAfterGraphChanges](../internal/runner/parallel_test.go#L611): after the helper is staged, the declared helper reader reads on the shared transaction and an unrelated query reads on its own connection at the same time. Neither read is allowed to finish until both have started, so the run can only complete if they overlap. Under the old rule this test would time out.
  - [TestQueriesNeedingTheSharedTransactionTakeTurns](../internal/runner/parallel_test.go#L681): a declared helper reader and a read without named kinds both read on the shared transaction and never at the same time.
  - [TestEndpointCheckWaitsForTheTransactionReader](../internal/runner/parallel_test.go#L731): a producer finishing on its own connection does not have its endpoints checked on the shared transaction while another query is still reading there.
  - [TestDependentReadSeesNewHelperInPublicationTransaction](../internal/runner/parallel_test.go#L568) now declares the helper kind it reads, as the loader requires of a real file. The expected connection count is unchanged.
  - [TestReportsSeparateCalculationAndSaving](../internal/runner/report_test.go#L56) checks that every step that ran has both times, that the finish is not before the start, that the duration equals their distance, and that the stored JSON carries `started_at` and `finished_at`. [TestReportsExplainSkippedAndDisabledQueries](../internal/runner/report_test.go#L138) checks that a skipped step has neither. [TestFailingStepIsSetAsideAndUnrelatedStepsAreSaved](../internal/runner/partial_test.go#L92) checks that a set-aside failed step keeps the times from the attempt that ran it.
  - The three scheduling tests were run three times in a row with the race detector; all passed every time.
- **Whole lab suite** (`go test -count=1 -race ./...` in `tools/esc1-poc`): all packages pass. `gofmt` clean; `go vet ./...` and `go vet -tags integration ./...` clean.
- **Real databases** (`go test -race -tags integration -count=1 ./internal/graphdb` against the isolated Neo4j and PostgreSQL test databases, `esc1-poc-test-graph-1` on Bolt 27687 and `esc1-poc-test-pg-graph-1` on 25432): passes, 504.5 seconds. This suite runs ordinary and advanced ESC1 plans end to end on both backends, so it exercises the new placement with real connections: readers on the shared transaction while others use their own. Note for next time: with the race detector the suite needs well over four minutes; a shorter `-timeout` aborts it mid-test, which looks like a hang and leaves fixtures behind that the next run refuses to touch. Clear them with the fixture-deletion statement from `setup` in `neo4j_integration_test.go` and `TRUNCATE edge, node RESTART IDENTITY` on the PostgreSQL test database.
- **BloodHound build** (`go build ./cmd/api/...` from the repository root, which embeds the engine): passes.
- **Page tests** (`npx vitest run src/views/AnalysisWorkspace` in `cmd/ui`): 9 files, 100 tests, all passed. The coordination test now also checks the **Started** and **Finished** lines against the fixture's times. `tsc --noEmit` and `prettier --list-different` on the changed page files: clean.
- Not run: `just test` and `just prepare-for-codereview` (they take a long time; ask before running), and a deployed run on the lab.

## Deployment

**Deployed 19 October 2026** to the lab project `esc1-workspace-manual`, at your request. Two containers were replaced: the app, now `esc1-poc-bloodhound-workspace:20261019-overlapping-reads` (program sha256 `10ea9edd…541e07c8`), and the rule service, now `esc1-poc-rules-workspace:20261019-overlapping-reads` (program sha256 `ab0b40fb…0df20bcc8`). The rule service had been stopped since 17 October and had to come back for analysis to report at all; it was rebuilt because its strict decoder would otherwise reject the new report fields. Both databases kept their start times (`work/overlap-deploy/start-before.txt` and `start-after.txt`), and `active.json` is byte-identical. Build method as before: `BUILD_PATH=work/overlap-deploy/ui npx vite build`, rsync source copy `work/overlap-deploy/build-checkout` with the page in `cmd/api/src/api/static/assets`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath`, thin Dockerfiles under `work/overlap-deploy/native` and `rule-image`. The compose file before the change is `work/overlap-deploy/compose-before.yaml`.

**What the startup run showed.** The analysis that runs at startup finished in 2.35 seconds with the lab's usual "partial" outcome (the two `domain_helpers` files with empty domain names, as before). Every one of the 29 steps now carries start and finish times. After the two failures were set aside and the rest retried, five steps started at the same instant, which is the full pool, and 25 pairs of steps overlapped in time. The printout is `work/overlap-deploy/startup-run-times.txt`, built from the saved API response `workspace-after-deploy.json`. The latest report on the page is the automatic retry a minute later, which reran only the two failing steps; the full run is in the kept history.

Rollback: set the two `image:` lines in `work/workspace-deployment/compose.yaml` back to the values in `compose-before.yaml` and run `docker compose -p esc1-workspace-manual --env-file deployment.env -f compose.yaml up -d --no-deps --no-build --pull never bloodhound rule-service` from `work/workspace-deployment`. The old rule service will then refuse reports from a newer app, so roll both back together.

## Manual testing steps

After deploying:

1. Sign in, open **Administration → OpenGraph analysis**, and click **Run Analysis** with the ESC1 rules active.
2. When the run completes, click any step, open **Last run**, and check that **Started** and **Finished** appear under **Time**, and that the distance between them matches **Time**.
3. Compare the **Started** and **Finished** of two steps from the first stage. Their ranges should overlap, because both read on their own connections.
4. Compare `esc1.cypher`, which declares calculated kinds, with any other step that does: their ranges must not overlap.
5. Expand **Latest analysis report** and confirm the run's calculation and save both succeeded and the counts match the previous run's.

## Suggested commit

After your review, and after `just prepare-for-codereview` (not run yet; it takes a while):

```sh
git add -- tools/esc1-poc/internal/runner/calculate.go tools/esc1-poc/internal/runner/report.go \
  tools/esc1-poc/internal/runner/partial.go tools/esc1-poc/internal/runner/parallel_test.go \
  tools/esc1-poc/internal/runner/report_test.go tools/esc1-poc/internal/runner/partial_test.go \
  cmd/ui/src/views/AnalysisWorkspace/types.ts cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx \
  cmd/ui/src/views/AnalysisWorkspace/testData.ts cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.test.tsx \
  packages/go/openapi/src/schemas/model.analysis-execution.yaml packages/go/openapi/doc/openapi.json \
  tools/esc1-poc/DESIGN.md tools/esc1-poc/USAGE.md tools/esc1-poc/DEVELOPERS.md tools/esc1-poc/HANDOFF.md \
  tools/esc1-poc/changes/09-overlapping-reads-and-step-times.md
git diff --cached --stat
git commit -m "perf: Keep independent analysis queries overlapping after graph changes and record step times <JIRA-TAG>" \
  -m "After the first helper object or calculated relationship was staged, every remaining query read one at a time inside the shared transaction, including queries from unrelated extensions. Now only queries that declare a staged kind, or read without naming kinds, read on that transaction, taking turns with the coordinator; the rest keep their own connections. Each step's report records when its read started and finished, shown on the page's Last run tab. Tests cover overlap after staging, turn-taking on the transaction, the endpoint check waiting for a transaction reader, and the recorded times including set-aside retries." \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git push
```

Replace `<JIRA-TAG>` before committing.

## Not in this change

- A timeline of the last run on the page, drawn from the recorded times. Decide after one run on a representative large graph.
- Any marker on the graph canvas for queries that read in the transaction's lane.
- Live progress while a run is going.
- Measurement on a large graph. The lab cannot reach ten million objects; a generated or borrowed graph is needed, and that is a decision for you.
- The other scale risks found in the same conversation: one transaction holding every write and every late read for the whole run, the fifteen-minute query and two-hour run time limits, and the untested memory cost of either reachability style on a large directory.
