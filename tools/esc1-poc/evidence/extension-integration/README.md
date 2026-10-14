# Extension association and shared-query verification

Recorded 13–14 October 2026 for the uncommitted TODO 6 implementation. [Results](results.json) identify the source commits, database state, checks and limitations. [Source hashes](source-files.json) pin changed code, examples and generated files. Historical evidence was not replaced.

Schemas upload through OpenGraph Management; analyses use the existing rules page. The parser remains in `tools/esc1-poc`. BloodHound preserves uploaded definitions, exposes an authenticated read, and makes schema changes wait for running analysis. Files declare their namespace and independent analysis version. Query dependencies support compatible version conditions and consumer aliases. Shared answers keep descriptive RETURN columns and ordinary documentation.

## Passed

- All tool unit tests, including schema changes, incompatible dependencies, matching filenames, plain shared values, row-column checks, retained pauses, explicit old-state refusal and cleanup ownership after a kind rename.
- BloodHound Go tests: `go test ./cmd/api/src/... ./server/...`.
- Focused graph scenarios on isolated PostgreSQL and Neo4j: multiple schemas, shared producers, disable/delete, rollback, tutorial stages, shipped vault, native ESC1, late publication failure and exact JSON batch numbers. Final result: 23.291 seconds. This is a test duration, not a performance comparison.
- Real database migration/save/rollback check: a rejected definition save also rolls back extension metadata.
- Browser checks for three extensions with the same query filename, schema-before/query-before ordering, compatible updates, deletion/reinstallation pauses, draft restart and stale review, and rules table controls. The association and draft checks use a real rules service with a synthetic schema reader. They do not establish full native UI upload coverage.
- OpenAPI JSON parsing and reference resolution, and `git diff --check`.

## Incomplete repository checks

`CI=true just test` failed in UI files unchanged by this work: an EdgeFilteringDialog hook timed out, and other UI tests reported unhandled errors. The shared UI summary was 1 failed, 1,269 passed and 5 skipped tests. This is not a clean full-repository test result; baseline failures have not been reproduced separately.

`just prepare-for-codereview` passed dependency setup and code generation after using project-local caches. It was stopped during recursive `goimports`, which walked ignored lab directories. Unrelated generated/formatting changes were restored, including the dependency's import-only change. Later preparation stages did not finish. This command is not evidence of a successful test run.

The OpenAPI bundle's affected entries were updated from source YAML and checked locally. Offline `just gen-spec` failed with `ENOTCACHED` because the Redocly CLI was unavailable in the project cache; rerun `just gen-spec` in the normal development environment. The maintained persistent-service script now uploads schemas separately, but its full lab demonstration was not repeated.

## Local artifacts and repeat commands

Full logs, earlier failed attempts, the first failed graph database dump, source diff and detached migration checkout remain under ignored `tools/esc1-poc/work/extension-integration/`. The isolated Docker Compose project is `esc1-extension-integration`; its databases contain synthetic test data. Stop its containers while retaining the data. No existing running BloodHound/rules service was migrated or redeployed.

From the repository root, with the normal development toolchain available:

```sh
(cd tools/esc1-poc && go test ./...)
go test ./cmd/api/src/... ./server/...
CI=true just test
just gen-spec
just prepare-for-codereview
git diff --check
```

Run the broad formatting/preparation command in a clean checkout without the ignored lab history. Review its generated changes. Before committing, resolve or explicitly account for the remaining repository checks; do not treat the feature-specific passes as a replacement.

Review `work/extension-integration/changed-files.txt` before manually staging anything: some documentation also contains earlier work. One manual commit is reasonable after review because namespaced file storage and dependency resolution must change together. If that file list still matches the intended patch, these commands stage only that reviewed list (plus this verification directory) and create the commit:

```sh
git diff --stat
while IFS= read -r file; do git add -- "$file"; done < tools/esc1-poc/work/extension-integration/changed-files.txt
git add -- tools/esc1-poc/evidence/extension-integration
git diff --cached --stat
git commit -m "feat: associate OpenGraph analyses with installed extensions"
```

These staging/commit commands have not been run. Add the real Jira tag to the title if this becomes a PR.
