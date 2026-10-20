# Change 10: "up to date" instead of "skipped" for a step that had nothing to do

Repository: the BloodHound clone at the repository root, branch `experimental-postprocessing`. The work is in the analysis engine under [tools/esc1-poc/internal/runner](../internal/runner) and the rule service under [tools/esc1-poc/internal/ruleservice](../internal/ruleservice), with matching words on the Analysis Workspace page and in the API description. It belongs with [change 11](11-analysis-attempts-as-one-run.md), which is the page work that this rename makes readable; the two are split so each is reviewable on its own.

## The problem

The engine used one word, `skipped`, for two opposite things:

- "Nothing this step reads has changed since it last saved, so its saved result is still current." That is good news. It is what every unrelated step reports after a Vault upload, and what every step except the set-aside ones reports on an automatic retry.
- "The run stopped before this step could start." That is bad news.

Only the Explanation column told them apart. After a retry on the lab, you saw 27 steps marked "skipped" with 0.000 s and read it as the engine having given up on them, when in fact they had all run and saved a minute earlier (20 October 2026).

## The solution

The first meaning is now its own state, `up-to-date`, at both levels:

- **Per step:** a step that was not selected because none of its declared inputs changed reports `up-to-date`. Its reason text is unchanged: "No declared input or connected calculation was affected by the confirmed changes."
- **Whole run:** a run in which no step was affected reports calculation `up-to-date` (and save `unchanged`, as before).

`skipped` keeps its second meaning only: a step that was still waiting when the run stopped. The page reads `up-to-date` as "Up to date (nothing it reads changed)" and lists it with the states whose reason is headed "Why it did not run".

## Decisions (20 October 2026)

| # | Question | Decision |
|---|---|---|
| 1 | Rename in the engine, or only reword on the page? | In the engine, so the API, the older rules page and any log say the same thing. |
| 2 | Rename the whole-run calculation state too? | Yes; it has the same meaning, and leaving it would have reintroduced the ambiguity one level up. |
| 3 | Keep `skipped` for "the run stopped before this step started"? | Yes. That is still a true "skipped". |

## Alternatives considered

- **Reword only on the page.** Every other reader of the report, including the older rules page and the stored `status.json`, would keep the ambiguity. Rejected.
- **A different word, such as `unchanged` or `current`.** `unchanged` already names a save outcome, and `current` reads oddly in a table of outcomes. "Up to date" says what a reader needs to know.

## How it works

- [report.go:217](../internal/runner/report.go#L217) sets `up-to-date` for an unselected step when the run selects queries by changed kinds. [report.go:270](../internal/runner/report.go#L270) sets the whole-run calculation to `up-to-date` when the run had nothing to do.
- [ruleservice/report.go:37](../internal/ruleservice/report.go#L37) and [:84](../internal/ruleservice/report.go#L84) accept the new state in the calculation and per-step lists the rule service checks when a report arrives. A report with a state outside those lists is rejected, which is why the rule service must be rebuilt with the app.
- The API description lists the new value in both enums ([model.analysis-execution.yaml](../../../packages/go/openapi/src/schemas/model.analysis-execution.yaml)); `openapi.json` was regenerated with the usual `redocly bundle` command.
- The page translates the state in [completedState](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L101) and adds it to the "Why it did not run" list in [QueryDetails.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx#L310).
- [END_USERS.md](../END_USERS.md) and [USAGE.md](../USAGE.md) use the new words.

## New dependencies

None.

## Downstream effects

- **The rule service must be rebuilt together with the app.** An older rule service rejects a report carrying `up-to-date` as "invalid query state", and the worker would keep retrying delivery. Both lab containers were rebuilt and replaced together (see change 11's Deployment section).
- Reports kept from before this change still say `skipped` for the first meaning. The page shows them as "skipped" with their original reason, which stays correct in words.
- Anything that filters reports by the literal `skipped` state, such as a saved search over `status.json`, sees fewer matches from now on.

## How it was tested

- Engine: [TestReportsExplainSkippedAndDisabledQueries](../internal/runner/report_test.go#L138) now expects `up-to-date` per step and for the whole-run calculation. The rule-service test fixture that stores a no-work run uses the new state ([schemas_test.go:210](../internal/ruleservice/schemas_test.go#L210)). `go test -count=1 -race ./...` in `tools/esc1-poc`: every package passes. `gofmt` and `go vet ./...` clean.
- Page: [model.test.ts](../../../cmd/ui/src/views/AnalysisWorkspace/model.test.ts) checks the new wording; the page suite result is in change 11.

## Suggested commit

See change 11: the two changes touch the same files, so they go in one commit with change 9.

## Not in this change

- Any change to which steps are selected. Selection is unchanged; only its name in the report changed.
