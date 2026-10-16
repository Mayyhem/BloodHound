# Read-only OpenGraph analysis workspace

Implemented on 14 October 2026 following the instruction to execute the [workspace plan](../../../../_plans/opengraph-analysis-workspace.md). This completes the implemented MVP scope; later rule management, collapsed views, historical diagrams, live progress, and interactive testing remain open. The remaining repository checks below prevent calling this a clean merge-ready result.

[Results](results.json) record source versions, database state, and actual checks. [Changed files](changed-files.txt) identify the review scope; [source hashes](source-files.json) identify the final source. Historical evidence remains unchanged. No files were staged or committed, and no running service was redeployed.

## What changed

- The plan explains the same answer, helper, and relationship waits used by the runner. Shared producers and previous-state reads do not create invented arrows.
- One authenticated service read supplies current files, dependencies, both revisions, and received reports. BloodHound keeps the service secret private and exposes two read-only routes under existing OpenGraph read permission and the extension-management flag. Responses are bounded; downloads check both displayed revisions and return the original file.
- **Administration → OpenGraph Analysis** coordinates graph focus, table, and details. It provides filters, dependency navigation, availability explanations, original Cypher/downloads, and matching completed outcomes. It links to existing management controls. The graph uses BloodHound's drawing and sequential layout; Explore keeps its original spacing and behavior. Independent queries are placed separately even when there are no arrows.
- Ordinary author files, graph storage, and DAWGS are unchanged. Browsing does not calculate or publish rules. Schema refresh can maintain existing rule pauses, as on the earlier plan read.

See [usage](../../USAGE.md#inspect-the-analysis-workspace), [design](../../DESIGN.md#a-native-bloodhound-page), and [source review order](../../REVIEW.md#read-only-analysis-workspace--14-october-2026).

## Passed

- All OpenGraph tool unit tests and race checks for the complete extension, rule-service, and postprocessing packages.
- Complete affected BloodHound API packages, including actual router permission/feature-flag rejection tests, fixed destination reads, stale downloads, and no graph connection.
- Five focused interface test files: **31 tests passed**, including existing Explore layout/highlighting and Administration tests. Cases include keyboard selection, hidden/absent prerequisites, changed revisions, matching reports, empty rules, unavailable service, and uncertain saving.
- Type checking and focused style checks.
- Real Chromium drawing/interaction checks for shipped ESC1 (13 queries, 15 arrows) and cross-extension (3 queries, 1 arrow) plans, in both themes. Graph clicks, prerequisite links, stable camera on selection, keyboard pan, zoom/fit, filters, original downloads, and a 390-pixel-wide page passed with no browser errors. Only workspace GET requests were made. [Browser results](browser-results.json) retain the assertions. Browser responses came from isolated rule-store snapshots; this is not a deployed full-stack application test.
- OpenAPI reference resolution and validation of both example workspace responses; `git diff --check`.

## Remaining checks

`just test` failed in unchanged shared UI test files. `EdgeFilteringDialog` had a timed-out setup hook; `ObjectSelect` could not find an option. The shared suite reported **12 failed, 1,258 passed, 5 skipped tests**, with three unhandled errors. Doodle UI's 134 tests passed. The command stopped before later dependent workspaces and native tests. These failures have not been separately reproduced on the base commit, so this is not a claim that the complete patch passes the repository suite.

`just prepare-for-codereview` ran in an isolated checkout to protect ignored lab history from recursive formatting. Dependencies, module tidy, generation, formatting, and the license scan finished. Analysis stopped while building the repository's custom Go linter because required dependencies were absent from the offline cache. Later analysis stages did not finish. The command is not evidence of a test run. Only the relevant test-import formatting and the license header for the changed tool module file were copied back; unrelated generated edits were left in the isolated checkout.

Offline `just gen-spec` failed with `ENOTCACHED` because Redocly was unavailable. The affected bundled entries were updated directly from the source YAML and checked locally. Repeat the normal generator with the development toolchain before merging.

Chromium initially lacked two libraries. After the user authorized downloading a browser runtime, Ubuntu `libnspr4` and `libnss3` packages were downloaded and unpacked under the ignored test directory. No system package was installed. The successful browser checks used those local libraries.

## Repeat and review

With the normal development tools available, run from the repository root:

```sh
(cd tools/esc1-poc && go test ./...)
(cd tools/esc1-poc && go test -race ./internal/extension ./internal/ruleservice ./postprocessing)
go test ./cmd/api/src/api/registration ./cmd/api/src/api/v2
yarn workspace bloodhound-ui test --run src/views/AnalysisWorkspace src/views/Explore/utils.test.ts src/components/SigmaChart/utils.test.tsx src/views/Administration/Administration.test.tsx
yarn workspace bloodhound-ui check-types
CI=true just test
just gen-spec
just prepare-for-codereview
git diff --check
```

Run preparation in a clean isolated checkout without ignored lab directories and review its generated edits. This session's isolated checkout is `tools/esc1-poc/work/analysis-workspace/review-checkout`. Full logs, the source snapshot helper, localhost browser harness, downloaded runtime libraries, and screenshots are retained beside it. No graph database was needed for this read-only feature, and no database integration/performance claim is made.

After reviewing this patch and resolving or accounting for the remaining checks, one manual commit is reasonable. The steps were checked separately as dependency/read access, native routes, and interface. Review the file list before using it; these commands have **not** been run:

```sh
git diff --stat
cat tools/esc1-poc/evidence/analysis-workspace/changed-files.txt
git add --pathspec-from-file=tools/esc1-poc/evidence/analysis-workspace/changed-files.txt
git diff --cached --stat
git commit -m "feat: add read-only OpenGraph analysis workspace"
```

Use the real Jira tag if preparing a PR. Build/restart the UI, API, and rule service through the existing lab workflow to make the new page available in a running installation; this task did not replace the current running containers.
