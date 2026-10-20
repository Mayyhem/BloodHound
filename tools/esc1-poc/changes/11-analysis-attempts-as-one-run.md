# Change 11: the page shows a run and its retries as one analysis

Repository: the BloodHound clone at the repository root, branch `experimental-postprocessing`. The work is in the Analysis Workspace page, [cmd/ui/src/views/AnalysisWorkspace](../../../cmd/ui/src/views/AnalysisWorkspace). It builds on [change 10](10-up-to-date-state.md), the engine rename that lets the page tell "nothing to do" from "did not get to run".

## The problem

When a step fails, the engine sets it aside, saves everything else, and retries the set-aside steps once, about a minute later. The retry reruns only those steps and reports every other step as up to date. The page showed only the latest report, so after the retry it said "Finished with 2 failures: 0 steps saved; 2 kept their previous results", every other step read "skipped, 0.000 s", and the start and finish times of the run that did the work were not visible anywhere. Seen on the lab on 20 October 2026, it looked as though the engine had abandoned 27 steps that had in fact all run and saved a minute earlier.

## The solution

1. **A run and its retries are one analysis.** Every attempt reports the time of the BloodHound event that triggered it and the rule revision it used; a retry carries the same two values as the run it continues. The page groups attempts by them.
2. **Each step shows the newest attempt that decided something about it.** A retry's "up to date" decides nothing about a step it did not rerun, so the page looks back to the attempt that ran it. On your lab the table now shows 27 steps succeeded from attempt 1 with their rows, times, Started and Finished, and the two `domain_helpers` steps failed from attempt 2, marked "2 (retry)" in a new **Attempt** column.
3. **The status line describes the analysis, not the last attempt.** It now reads, for example: "Run started 11:24:28 AM, 2 attempts: 27 steps saved; 2 failed (PoC/domain_helpers.cypher, PoC_Cypher/domain_helpers.cypher) kept their previous results. The retry at 11:25:21 AM reran only the set-aside steps, and they failed again. Automatic retries have stopped; the set-aside steps run again after the next BloodHound analysis (Run Analysis or a data upload)." The last sentence is the engine's own.
4. **The per-attempt view is still there.** The Analysis report panel lists the attempts and has a switch, **Show the latest attempt on its own**, which puts the page back to showing the latest report alone.
5. **Last run shows where its facts came from.** A step's Last run tab adds "From attempt: 1 of 2" (or "2 of 2 (an automatic retry)") with that attempt's time, and takes its save outcome and failure explanation from that attempt.

Nothing changes in what the engine stores. The reports stay attempt by attempt; the grouping is a view.

## Decisions (20 October 2026)

| # | Question | Decision |
|---|---|---|
| 1 | Group attempts by trigger event? | Yes, by event time and rule revision together. |
| 2 | Rename the no-change state in the engine or only reword it? | In the engine: change 10. |
| 3 | Keep a per-attempt view? | Yes, behind a switch, off by default. |
| 4 | One change note or two? | Two: the engine rename (10) and this page work (11). |

## Alternatives considered

- **Mix outcomes from any earlier run into one table.** Simpler, but it would hide a genuine regression: a step that succeeded last week and failed today would look fine if today's attempt happened not to rerun it. Grouping only attempts of the same event and revision keeps the table honest.
- **Have the engine carry the previous attempt's outcomes into the retry's report.** That would make the stored report say things the retry did not do, and every later reader would have to know which fields were inherited. The stored reports remain the truthful record; the page does the folding.
- **Drop the per-attempt view.** Cheaper, but an operator comparing attempts, or reading an old report, sometimes needs the attempt alone.

## How it works

- [analysisAttempts](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L142) takes the latest completed report, then keeps every kept report with the same `event` and `revision`, oldest first. Reports are deduplicated by run id and time, as `stepTimes` already did.
- [combinedOutcomes](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L170) walks the attempts newest first for each step and takes the first outcome whose state is not `up-to-date`; if every attempt says up to date it takes the newest. Each entry remembers its attempt, the attempt number counted from 1, and the number of attempts ([AttemptOutcome](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L163)). [latestAttemptOutcomes](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L193) gives the latest attempt alone in the same shape.
- [analysisSummary](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L386) writes the status line from the grouped outcomes: start time of the first attempt, number of attempts, steps saved (succeeded in an attempt whose save succeeded), steps failed with their names, steps set aside, what the retry did, and the engine's own sentence about further retries, found in the latest message. `runPhase` uses it for a partly saved run; a failed run with several attempts says so in its first words.
- The page keeps the switch state and builds its outcome map from either function, only when the latest report matches the current plan as before ([AnalysisWorkspace.tsx:169](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx#L169)). The rules table, the graph colours, the report table and the details panel all read that one map.
- [RunSummary.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/RunSummary.tsx#L140) lists the attempts with their time, state and message, shows the switch, adds the Attempt column, and in the combined view explains each failed step's failure from the attempt that produced it while keeping the latest attempt's run-level failures, such as an unconfirmed save.
- [QueryDetails.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx#L316) shows "From attempt" and takes the save state and failure text from that attempt.
- The guides describe the grouping: [USAGE.md](../USAGE.md), [END_USERS.md](../END_USERS.md) and [DESIGN.md](../DESIGN.md).

## New dependencies

None. The switch is MUI's `Switch`, already used elsewhere on the page.

## Downstream effects

- Only the Analysis Workspace page changes. The engine, the rule service and the stored reports are untouched by this change (change 10 touches the engine).
- Older kept reports that still say `skipped` for "nothing to do" are treated as deciding something, so for an analysis whose retry predates change 10 the combined view shows the retry's "skipped" rather than the first attempt's success. New reports are grouped correctly.
- The "Last ran" and "Last success" columns are unchanged; they already looked through history.

## How it was tested

- Model tests ([model.test.ts](../../../cmd/ui/src/views/AnalysisWorkspace/model.test.ts#L357)): grouping by event, oldest first, leaving an unrelated run out; the failed step coming from the retry and a saved step from the first attempt; the latest-attempt view still saying up to date; the status line text in full; the new wording of the state.
- Page test ([AnalysisWorkspace.test.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.test.tsx#L712)) on a two-attempt fixture ([retriedAnalysisFixture](../../../cmd/ui/src/views/AnalysisWorkspace/testData.ts#L128)): the status line names the failed step and the counts and never says "0 steps saved"; the Attempts list has two entries; the report table shows the saved step from attempt 1 and the failed step from "2 (retry)"; the details panel says "From attempt: 1 of 2"; the switch turns the same step into "Up to date".
- The existing test that keeps run-level failures separate from calculation success found the first version of the combined failure list dropping an unconfirmed-save failure; that is fixed and the test passes.
- `npx vitest run src/views/AnalysisWorkspace` in `cmd/ui`: 9 files, 105 tests, all passed.
- `tsc --noEmit` and prettier: clean. Engine suite: see change 10.

## Deployment

**Deployed 20 October 2026** to the lab project `esc1-workspace-manual`, both containers, because change 10 adds a state the rule service must accept: app `esc1-poc-bloodhound-workspace:20261020-analysis-attempts` (program sha256 `84173264…`) and rule service `esc1-poc-rules-workspace:20261020-analysis-attempts` (program sha256 `6e3e6536…`). Same build method as change 9, under `work/attempts-deploy`; the compose file before the change is `work/attempts-deploy/compose-before.yaml`, container start times are in `start-before.txt` and `start-after.txt`, and `active.json` is byte-identical.

**What the startup analysis showed.** The run at 16:14:00 UTC reported 27 steps succeeded and 2 failed; the automatic retry at 16:14:43 reported the same 2 failed and the other 27 as `up-to-date`, the new state, and the rule service stored it (`work/attempts-deploy/workspace-after-deploy.json`). Both attempts carry the same event time, which is what the page groups on. No browser check was run; you are testing by hand.

Rollback: both `image:` lines in `work/workspace-deployment/compose.yaml` back to the values in `compose-before.yaml` (the change 9 images), then `docker compose -p esc1-workspace-manual --env-file deployment.env -f compose.yaml up -d --no-deps --no-build --pull never bloodhound rule-service` from `work/workspace-deployment`. Roll both back together: the change 9 rule service rejects `up-to-date`.

## Manual testing steps

1. Open the page after an analysis that had a failure and a retry (the lab's `domain_helpers` files give you one). The status line should start "Run started …, 2 attempts: 27 steps saved; 2 failed …".
2. Open **Analysis report**. Above the report, "Attempts of the latest analysis: 2" lists both attempts. In the table, the 27 steps show "succeeded" with rows and times and Attempt "1"; the two failed steps show Attempt "2 (retry)".
3. Click a succeeded step, open **Last run**: Outcome succeeded, "From attempt: 1 of 2", Started and Finished present.
4. Turn on **Show the latest attempt on its own**: the same steps now read "Up to date (nothing it reads changed)" and the status line is unchanged.

## Suggested commit

Changes 9, 10 and 11 touch the same files (the engine's report, the page's model and details panel, the API description and the guides), so they cannot be staged apart without picking hunks by hand. After your review, and after `just prepare-for-codereview` (not run yet; it takes a while):

```sh
git add -- tools/esc1-poc/internal/runner/calculate.go tools/esc1-poc/internal/runner/report.go \
  tools/esc1-poc/internal/runner/partial.go tools/esc1-poc/internal/runner/parallel_test.go \
  tools/esc1-poc/internal/runner/report_test.go tools/esc1-poc/internal/runner/partial_test.go \
  tools/esc1-poc/internal/ruleservice/report.go tools/esc1-poc/internal/ruleservice/schemas_test.go \
  cmd/ui/src/views/AnalysisWorkspace/types.ts cmd/ui/src/views/AnalysisWorkspace/model.ts \
  cmd/ui/src/views/AnalysisWorkspace/model.test.ts cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx \
  cmd/ui/src/views/AnalysisWorkspace/RunSummary.tsx cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx \
  cmd/ui/src/views/AnalysisWorkspace/testData.ts cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.test.tsx \
  packages/go/openapi/src/schemas/model.analysis-execution.yaml packages/go/openapi/doc/openapi.json \
  tools/esc1-poc/DESIGN.md tools/esc1-poc/USAGE.md tools/esc1-poc/DEVELOPERS.md tools/esc1-poc/END_USERS.md \
  tools/esc1-poc/HANDOFF.md tools/esc1-poc/changes/09-overlapping-reads-and-step-times.md \
  tools/esc1-poc/changes/10-up-to-date-state.md tools/esc1-poc/changes/11-analysis-attempts-as-one-run.md
git diff --cached --stat
git commit -m "feat: Overlapping reads after graph changes, step times, and one view of a run and its retries <JIRA-TAG>" \
  -m "Engine: only queries that declare a staged kind, or read without naming kinds, read on the shared transaction, taking turns; the rest keep their own connections after the first graph change. Each step's report records when its read started and finished. A step with nothing to do reports up-to-date instead of skipped, at step and run level; the rule service accepts the new state. Page: a run and its automatic retries are shown as one analysis, each step from the newest attempt that decided it, with an Attempt column, a per-attempt switch, a status line written from the whole analysis, and Started/Finished on Last run. Change notes 09, 10 and 11 explain each part." \
  -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git push
```

Replace `<JIRA-TAG>` before committing.

## Not in this change

- A timeline drawn from the recorded times. Still waiting on a run on a representative large graph.
- Showing more than the latest analysis as a group; older analyses are reachable through the attempts list only as single reports, as before.
