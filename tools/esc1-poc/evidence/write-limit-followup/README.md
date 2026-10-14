# Limits before writes — 13 October 2026

This is the separate correction for TODO #10. PostgreSQL previously dropped the ordering, skipped rows and limit from an intermediate `WITH`. A following write could therefore change more objects than the query selected. The earlier comparison's explicit-ID cleanup workaround remains unchanged, as do all historical measurements.

## How the correction works

The translator now finishes each `WITH` selection before the following operation reads it:

1. Calculate the requested values, including grouping and `DISTINCT`.
2. Apply `ORDER BY`, `SKIP` and `LIMIT` to that result.
3. Apply a filter attached to that bounded `WITH`.
4. Pass the selected rows to the following operation.

A filter attached to an earlier `MATCH` still runs before selection. Ordinary, unbounded filters retain their existing placement. Sorting can read newly assigned names and calculated values. A value needed only for sorting or filtering remains private to that step. The translator reuses a name reserved while reading the sort expression, so renaming an object does not leave the sort pointing at a missing value.

`LIMIT 0` passes no rows and performs no changes. A limit on the final `RETURN` only restricts the answer returned to the caller; it does not restrict an earlier write. Without ordering, a limit bounds the number of input rows without promising which rows are chosen. Repeated input rows can refer to the same object, so input-row counts and distinct changed-object counts need not be equal.

## Checks and evidence

The initial 26 shared examples passed on Neo4j; PostgreSQL passed three and failed 23 before the correction. The final shared file has 38 cases with written expectations for both databases. It covers node and relationship deletion, property and node-kind changes, node and relationship creation, sorting in both directions, skipped rows, empty input, zero and oversized limits, repeated rows, renamed and calculated values, successive selections, grouping, `DISTINCT`, and filters before and after selection.

The shared test reader now supports optional follow-up reads inside the test transaction. These check the actual remaining or created graph. Separate checks read the saved graph from a new transaction after negative bounds, a failed property calculation, and cancellation after a valid deletion. PostgreSQL also runs the 38 shared cases with its optional optimizations disabled.

Parser examples and 36 generated SQL examples accompany the change. All 408 existing SQL cases are retained, for 444 total. During development, a generated-output update silently omitted cases after translation errors. The original fixtures were restored from the task's starting snapshot, including earlier uncommitted changes; the final case list was checked against that snapshot. The existing generator's ignored errors deserve a separate correction.

Full validation results are recorded in [validation.json](validation.json). Private command output and unsuccessful attempts are retained under `tools/esc1-poc/work/write-limits/`. No new performance study was run.

| Final check | Result |
| --- | --- |
| Complete DAWGS suite, PostgreSQL | Passed; 381.028 seconds. |
| Complete DAWGS suite, Neo4j | Passed; 202.636 seconds. |
| Go static checks for PostgreSQL code and the integration package | Passed. |
| Pilot runner and graph database unit packages, with race detection | Passed; 10.160 seconds overall. |
| Formatting and whitespace checks | Passed; unrelated automatic formatting was restored to its starting content. |
| Comparable DAWGS unit coverage | Increased from 62.0691% to 63.2539%. |

Each complete database run includes all eight existing scenario tests. The two temporary containers are stopped, with storage preserved. Root `just test` and `just prepare-for-codereview` were not run: BloodHound changes here are documentation only, and no commit was made. The latter remains required before the user's manual commit.

## Source and database state

The starting BloodHound commit was `ed94c2c96855599dfe66d0e83f9a976a0969dff7`; the separate DAWGS checkout was at `5f16c574e48045472d15a447c2fa9707d394a4bd` with earlier uncommitted changes. A complete starting-source archive, diff and file fingerprints were saved privately before editing. The validation record identifies the tested source with hashes; a commit alone would not identify this working tree.

Tests use new, isolated PostgreSQL 18 and Neo4j 4.4.42 storage under `work/write-limits/`. The dedicated containers are named `esc1-write-limits-test-pg-graph-1` and `esc1-write-limits-test-graph-1`. Existing application databases and earlier test storage were not used. The final validation record records the new containers' stopped state. No application deployment, staging or commit was performed.

## Files to review

DAWGS is a separate repository. Paths below are relative to `tools/DAWGS/`:

| Files | Purpose |
| --- | --- |
| `cypher/models/pgsql/translate/with.go`, `query.go`, `translator.go` | Preserve and correctly place the intermediate selection. |
| `cypher/models/pgsql/translate/with_selection_test.go` | Check translation with optimizations enabled and disabled; reject unsupported forms. |
| `integration/testdata/templates/write_selection.json` | Shared written expectations and graph checks. |
| `integration/cypher_template_test.go` | Allow optional graph checks after a write. |
| `integration/pgsql_path_input_integration_test.go` | Reuse the PostgreSQL optimization-switch test helper for these cases. |
| `integration/write_selection_integration_test.go` | Check that failed or canceled writes preserve the saved graph. |
| `cypher/test/cases/mutation_tests.json`, `cypher/models/pgsql/test/translation_cases/write_selection.sql` | Parser examples and generated SQL. |
| `README.md` | Explain supported behavior and limitations. |

BloodHound changes are documentation only: this evidence directory, `TODO.md`, `REVIEW.md`, `HANDOFF.md` and `OPENGRAPH_ANALYSIS_MIGRATION_PLAN.md` under `tools/esc1-poc/`. The migration plan keeps the correction and its tests together as D26, with its documentation in B69. Concurrent TODO #6 planning is separate work.

## Remaining limitations and follow-up

This change does not add wildcard `WITH`, parameterized bounds, or write/`WITH`/write sequences that PostgreSQL rejects because the translated writes are nested. Supported bounds are integer literals. These are existing unsupported forms, not new instructions for extension authors.

A raw property assigned to a name and then compared numerically can fail with a PostgreSQL JSONB/integer type error. A calculated numeric value works and is covered here. Broader property-alias typing deserves a separate follow-up. Grouped filters and other Cypher forms beyond the tested selections are not a claim of complete Neo4j compatibility.

No extension file format, public API or depth limit changed. The earlier depth-limit review remains outstanding.

## Repeating the checks and committing

From the BloodHound root, use the existing local Go wrapper and isolated databases:

```sh
export POC_MODULE_CACHE=/home/cthompson/go/pkg/mod
export CGO_ENABLED=1
export CONNECTION_STRING='postgresql://bloodhound:experiment-only@127.0.0.1:26432/write_limits_test'
make -C tools/DAWGS test_all GO_CMD="$PWD/tools/esc1-poc/scripts/go.sh -C ../DAWGS"
export CONNECTION_STRING='neo4j://neo4j:experiment-only@127.0.0.1:28687/neo4j'
make -C tools/DAWGS test_all GO_CMD="$PWD/tools/esc1-poc/scripts/go.sh -C ../DAWGS"
tools/esc1-poc/scripts/go.sh -C ../DAWGS vet ./cypher/models/pgsql/... ./integration
tools/esc1-poc/scripts/go.sh test -race -count=1 ./internal/runner ./internal/graphdb
```

The complete database suites include race checks and the scenario-based integration suite. The ordinary pilot unit command does not run database integration tests.

This is a useful manual commit checkpoint after review. Before committing, run the required `just prepare-for-codereview` from the BloodHound root and inspect its generated changes. That command is not a replacement for the database tests above. DAWGS already contained substantial uncommitted work: review and stage the intended changes there separately; adding files in BloodHound does not capture that checkout. Suggested commands, with the appropriate Jira tag substituted:

```sh
just prepare-for-codereview
git -C tools/DAWGS diff
git -C tools/DAWGS status --short
git -C tools/DAWGS add -p
git -C tools/DAWGS add cypher/models/pgsql/translate/with_selection_test.go cypher/models/pgsql/test/translation_cases/write_selection.sql integration/testdata/templates/write_selection.json integration/write_selection_integration_test.go
git -C tools/DAWGS diff --cached
# Include any earlier uncommitted dependencies needed by this change before committing.
git -C tools/DAWGS commit -m "fix: Preserve row selection before PostgreSQL writes <JIRA-TAG>"
git add -p tools/esc1-poc/TODO.md tools/esc1-poc/REVIEW.md tools/esc1-poc/HANDOFF.md tools/esc1-poc/OPENGRAPH_ANALYSIS_MIGRATION_PLAN.md
git add tools/esc1-poc/evidence/write-limit-followup
git diff --cached
git commit -m "docs: Record the write selection correction <JIRA-TAG>"
```

These are suggested commands for the user; the agent did not stage or commit.
