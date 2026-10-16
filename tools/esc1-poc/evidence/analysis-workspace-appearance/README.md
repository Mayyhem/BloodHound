# Analysis workspace labels, colors, and focus — 14 October 2026

The user requested dependency depth in place of Stage, availability colors,
analysis versions beside namespaces, and BloodHound-style path focus.
The updated page is deployed at
[OpenGraph Analysis](http://127.0.0.1:28080/ui/administration/opengraph-analysis).
The lab account remains `admin` / `experiment-only`.

## What changed

- The table and query details display **dependency depth**, starting at 0.
  This is the longest prerequisite chain's arrow count. Existing API stage
  values and saved reports still count from 1; the interface subtracts one.
  Scheduling and extension author requirements are unchanged.
- Enabled nodes are green, disabled nodes red, blocked nodes orange, and the
  focused query blue. Node sublabels show namespace and analysis version.
  Availability and completed outcomes remain readable in the table and details.
- Selecting a query keeps its prerequisite and consumer paths visible. Selecting
  an arrow keeps prerequisites before its source, that arrow, and consumers
  after its target visible. Side branches and shortcuts bypassing the selected
  item fade to gray. Selecting an edge also opens its explanation and query links.
- Edges use the same light/dark gray as Explore, through a shared color helper.
  Explore's existing colors are unchanged. A selected arrow is thicker.
- Clear focus, Escape from the graph controls, or clicking empty graph space
  restores the original colors. Focus preserves the camera. Dependency
  explanations provide keyboard access to the same edge selection.

Read the interface files under `cmd/ui/src/views/AnalysisWorkspace/`, the shared
edge color in `cmd/ui/src/rendering/utils/utils.ts`, and its existing Explore
call site. Usage, design, end-user guidance, the plan, and the handoff were updated.

## Verification

- All five focused interface test files passed: **34 tests**. These include
  workspace coordination, direction-preserving path selection, existing Explore
  layout/highlighting, and Administration navigation.
- Visual review found a false hidden-query notice after selecting an edge. The
  notice now distinguishes a selected query from a selected edge; its regression
  check and the other workspace tests passed in the final run: **21 tests**.
  These are a rerun of the workspace subset, not 21 additional distinct tests.
- Type checking and targeted style checks passed. The production UI build and
  native executable build passed.
- [Browser checks](browser-results.json) used real Chromium drawing and clicks
  with shipped example snapshots and synthetic disabled/blocked states. Both
  themes passed status/focus colors, version labels, default edge gray, actual
  node and arrow clicks, fading, camera preservation, and color restoration.
  The 13-query ESC1 view and a narrow viewport were also inspected. These sample
  checks did not change the running rules or graph database.
- [Live browser checks](live-browser-results.json) used normal login and actual
  deployed responses: all 16 queries, zero-based depth, edge details, no false
  hidden-query notice, clearing focus, dependency navigation, filtering,
  refresh, and a byte-exact original download passed without page errors.
  Workspace and download requests returned HTTP 200.
- Full repository test results are recorded in `results.json` once the final
  command completes. Earlier interrupted attempts are not passing results.

The first browser attempt timed out while a heavily parallel repository test
run competed for resources. The temporary development server also reloaded when
generated coverage and build-checkout files changed. Those directories were
excluded from its file watcher. The later both-theme browser check passed.
The unrestricted full test attempt was stopped and rerun with two workers per
Vitest process; the user then interrupted that run. The final command uses the
same limit through an ignored local Yarn wrapper. No repository test settings
were changed.

No Go behavior or DAWGS source changed in this follow-up. The earlier full-suite,
OpenAPI generator, and preparation limitations are documented in
[the initial workspace evidence](../analysis-workspace/README.md).
`just prepare-for-codereview` is still required before a manual commit; this
follow-up does not claim another successful preparation run.

The running manual-test lab separately reports a failure in
`PoC/domain_helpers.cypher` during `check-answers`, with saving `not-started`.
The sanitized explanation does not establish the cause; investigation of the
query inputs and private application logs remains follow-up work. No answer
validation or execution code changed here. See
[the observed report](observed-analysis-report.json). These interface checks
must not be read as a successful full analysis of the lab data.

## Deployment and preserved state

Only the BloodHound application container was replaced, using
`esc1-poc-bloodhound-workspace:20261014-appearance-final`. The rule service and both
database containers were not restarted. The active rule file's hash was unchanged
across deployment, preserving the user's manual-test selections.
[Deployment state](deployment-state.json) records the image and executable hash;
[the previous state](before-deployment.json) records the prior containers.
[Source hashes](build-source-files-final.json) identify the final files copied
into the isolated build checkout over base commit `27bf6a0ed`. Documentation added
after compilation is outside that source snapshot. Historical evidence remains
unchanged.

Builds, logs, screenshots, and the isolated checkout remain under ignored
`tools/esc1-poc/work/workspace-appearance/`. The active Compose file remains
`tools/esc1-poc/work/workspace-deployment/compose.yaml`, with its BloodHound image
updated. `work/workspace-appearance/compose-before.yaml` preserves the previous
configuration. The original pre-workspace lab remains stopped and untouched.

To roll back only this interface update from the repository root:

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-appearance/compose-before.yaml \
  up -d --no-deps --no-build --pull never bloodhound
```

That starts the earlier application image against the same manual-test data.
If retaining the rollback, also restore the earlier image setting in the active
Compose file so the next restart does not reapply this update.

Nothing was staged or committed. Review the changed-file list and complete the
remaining repository checks, including isolated `just prepare-for-codereview`,
before committing manually. The list covers the
whole uncommitted workspace feature, including this requested follow-up:

```bash
git diff --stat
cat tools/esc1-poc/evidence/analysis-workspace-appearance/changed-files.txt
git add --pathspec-from-file=tools/esc1-poc/evidence/analysis-workspace-appearance/changed-files.txt
git diff --cached --stat
git commit -m "feat: add OpenGraph analysis workspace"
```

Run preparation in an isolated checkout without ignored lab databases, as
explained in the initial evidence. The staging and commit commands above are
suggestions for the user and were not executed.
