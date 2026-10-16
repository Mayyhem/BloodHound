# Analysis file editor — 14 October 2026

The user approved the complete create/edit workflow and then approved standalone row-returning files before they have consumers. The full discussion remains in [_plans/opengraph-analysis-workspace.md](../../../../_plans/opengraph-analysis-workspace.md). Nothing was staged or committed.

The editor is deployed at <http://127.0.0.1:28080/ui/administration/opengraph-analysis>. Choose **Create analysis file**, or select a query and choose **Edit analysis file**. Both app and rule-service images use `20261014-editor`. The deployment replaced only those two containers. Both database containers, active-rule revision, schema revision, and pending-upload contents are checked separately below.

## What is implemented

- Installed-extension selection, ordinary Cypher editing, prerequisite node/list selection, editable parameter aliases, and a preview of the actual needed chain.
- Recalculate only that chain, or reuse one complete matching successful source. Failed or incomplete answers do not qualify. Prerequisites using internal database IDs, reachability, or overlap must be recalculated; stable object identifiers can be reused.
- Temporary graph outputs for prerequisites, final returned rows, and a graph containing only result-identified entities/connections. Resolve familiar returned identifiers automatically; unfamiliar object columns can be selected and tested again.
- Private resumable working copies, separate from shared uploads. A successful exact-file test is required before review and activation. Only that reviewed file is activated; publishing its calculated graph changes still requires normal analysis.
- Testing can preview an undeclared output type, but activation still requires the installed schema. Row-only intermediate files can activate before another file consumes them.

All graph writes must pass through the same BloodHound process, on either database. The app serializes editor work with its graph writers and checks file/schema/graph versions again before activation. External database writers and multiple independent app processes are outside this agreed guarantee. Graph objects, relationships, and properties from tests are discarded; database kind/index metadata can remain. A failed or uncertain discard blocks further app graph writes.

Saved answers have private checked files, a 24-hour lifetime, four-source and byte limits, and fresh process identity after restart. Test receipts expire after 30 minutes. A source may be listed but still fail the exact chain check at execution. Missing answers require recalculation. This adds an editor result preview and reusable inputs, not a standalone historical-answer or historical-graph viewer.

## Recorded checks

| Check | Actual result |
| --- | --- |
| Complete PoC `scripts/go.sh test ./...` | Passed; `poc-all-tests.log` |
| Workspace interface | 48 tests in seven files passed; `ui-tests.log` |
| Final editor interaction subset | Nine passed after review presentation changes; `ui-editor-final.log` |
| Native API/registration/services packages | Passed; `native-tests.log` |
| Neo4j and PostgreSQL editor integration | Passed in 6.401 seconds; `native-editor-integration.log` |
| Final service package after the startup fix | Passed in 28.636 seconds; `postprocessing-final.log` |
| Optional editor-storage startup failure | Passed: ordinary execution and writer coordination remain available, editor reports unavailable; `storage-failure-test.log` |
| Final interface type check and formatting | Passed; `types-production.log`, `style-production.log` |
| Production UI and both native binaries/images | Built successfully; build logs retained in ignored `work/analysis-editor-final/` |
| Real deployed browser | Passed without mocked responses: ordinary query, standalone review, private save/resume, returned-object graph, dark mode, correct schema link; `live-browser-results.json` |
| Repository `just test` | Failed in `bh-shared-ui` after 7m49s when Vitest lost a temporary coverage file; dependent interface packages, Go tests and final coverage were not reached. The reason the file disappeared is not established. `just-test.log` |
| Repository `just prepare-for-codereview` | Failed at the analyzer stage while building custom golangci-lint: its dependency chain needed an uncached module with `GOPROXY=off`. Dependencies, module synchronization, generation/formatting and license checks completed. The later analysis/show steps did not complete. `prepare-for-codereview.log` |

The real database checks exercise temporary prerequisite nodes and edges, result lookup before discard, withheld final publication, cancellation after writes, bad outputs and limits, stale/unsafe reuse refusal, successful stable-identifier reuse, schema validation, and test/review/activation through the service. Whole graph snapshots include saved analysis objects and run records. This does not claim every older test in the repository passed.

An initial separate `just gen-spec` attempt failed to obtain Redocly. Later, repository preparation successfully ran the standard `npx @redocly/cli@1.18.1 bundle` step. That generated bundle was reviewed and adopted; all five editor routes and every internal reference were verified. The earlier local fallback is preserved in ignored work, and is no longer the submitted bundle.

## Review and reproducibility

`production-source-files-final.json` identifies the final production source copy, including the startup safeguard and its regression test. `binary-sha256-final.json` identifies the final app executable and unchanged editor rule-service executable. Earlier hashes remain as first-deployment evidence. `changed-files.txt` lists the broader current unstaged workspace changes, including earlier workspace work; it is not a claim that every file originated in this editor follow-up. Historical evidence remains untouched. Runtime output and screenshots stay in ignored `work/analysis-editor-final/`.

The active lab already had a separate `domain_helpers` answer-checking failure before this deployment. This work does not claim that normal full analysis succeeded or repair that separate issue.

Suggested manual review/checkpoint commands, not executed by the agent:

```sh
git status --short
git diff --check
just test
just prepare-for-codereview
git diff --stat
git diff -- cmd/api/src/api/registration cmd/api/src/api/v2 cmd/api/src/services/entrypoint.go cmd/ui/src/views/AnalysisWorkspace tools/esc1-poc packages/go/openapi
```

For a combined workspace checkpoint after reviewing the listed files, these are suggested commands only. They include the earlier workspace work already present in this checkout, so use them only if that combined scope is intended:

```sh
git add -- _plans/opengraph-analysis-workspace.md cmd/api/src/api/registration cmd/api/src/api/v2/analysis_*.go cmd/api/src/api/v2/model.go cmd/api/src/services/entrypoint.go cmd/ui/src/views/AnalysisWorkspace cmd/ui/src/views/Administration/Administration.tsx cmd/ui/src/routes/constants.ts cmd/ui/src/rendering/utils cmd/ui/src/views/Explore/utils.ts packages/javascript/bh-shared-ui/src/utils/permissions.ts packages/go/openapi tools/esc1-poc
git diff --cached --stat
git diff --cached
git commit -m "feat: Add the OpenGraph analysis editor <JIRA-TAG>"
```

Replace the Jira placeholder. The agent has not run those staging or commit commands. A checkpoint is appropriate after the final checks are acceptable.

Rollback app/rule-service image choices are preserved in ignored `work/analysis-editor-final/compose-before.yaml`. Use the same existing environment file and `up -d --no-deps --no-build --pull never rule-service bloodhound`; do not recreate either database.

The deployed browser returned two collected domain objects from stable identifiers and no unrelated edges. Its private working copy was deleted afterward. `deployment-state.json` confirms 29 active files and unchanged rule/schema revisions and pending-upload checksum. `containers-before.json` and `containers-after.json` confirm both database identities/start times are unchanged. There were no browser page errors. Two earlier harness attempts corrected a test declaration and an ambiguous selector; their logs remain in ignored work and are not counted as application failures. All 593 checked local documentation targets exist. Temporary editor-test database containers were stopped with their storage preserved.

The first full-suite attempt was not completed reliably through the tool session and is not counted as a pass. The replacement was monitored to its actual exit code 1. Earlier numbered-extension evidence also recorded a shared-interface failure, but that does not establish this failure's cause or prove it unrelated. No full-suite rerun is claimed.

The final app rebuild includes the optional-storage startup safeguard. It used a separate source copy so repository generation could continue without changing the build inputs. The already-tested production UI is embedded unchanged. `api-final-results.json` records a fresh authenticated test and exact-file review against that final app, without activation. `containers-final.json` and refreshed `deployment-state.json` confirm preservation after the final app replacement. The complete PoC/native/browser checks above preceded this small startup safeguard; the dedicated regression and complete service-package rerun cover the safeguard afterward.

The complete browser workflow was repeated successfully against the final app build, waiting for the finished result display and confirming exactly two SVG graph nodes. Its latest private working copy was deleted and preservation checks passed again. There were no mocked browser responses or page errors. Repository preparation changed only its isolated checkout; those changes were not copied into the working tree indiscriminately. No staging or commits occurred.

Preparation's reviewed import-spacing/explicit-alias changes and one complete license header were adopted in eight files. Other generated changes, including changes to copies of historical SQL evidence, stayed in the isolated checkout. Standard OpenAPI generation also completed and its checked bundle was adopted. `adopted-preparation-formatting.json` lists the eight source files; no behavior changed in those formatting edits.

The final release was rebuilt once more with the standard generated API bundle and reviewed formatting/header fixes, then deployed and checked through authenticated query test/review again. `api-release.log` and `deployment-check-release.log` record that release check; `app-image-release.log`, `deploy-release.log`, and final source/binary hashes identify it. The UI and query behavior did not change after the full browser check. All verification processes are finished. The manual application remains running; the isolated test databases remain stopped.
