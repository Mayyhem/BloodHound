# Path input correction and ordinary query measurements

This is the follow-up for TODOs #13 and #12. See [the report](../../PREPARATION_AND_REACHABILITY.md#correcting-the-input-association-then-measuring-ordinary-queries). All retained graph values are synthetic. The earlier preparation study and its failing query results remain historical evidence in [preparation-followup](../preparation-followup/README.md).

The correction keeps each input attached to its own path search. The unchanged membership query processes 267 first-stage rows before DISTINCT, rather than 26,967, and returns identical complete values for all 125 objects. TODO #12 is completed through this direct removal of repeated work. There is no separate reachability shortcut, new syntax or author-file change.

## Retained files

- `input-before*` and `input-after13*`: SQL, actual plans and complete answers for the original two-group reproduction. Written expectation: the requested and actual root must agree, with 166 routes for group 0 and 46 for group 1. Before returns two additional mismatched pairs; after returns exactly the expected pairs.
- `membership-{before,after13}-{0,1,2,3}.json`: actual PostgreSQL plans, including one marked warmup per build. Repetition 1 also retains each build's SQL and complete answers. Every repetition compared all returned values, not only the row count.
- `plan-measurements.json`: execution and planning times extracted from those plans. The report distinguishes database code generation from other execution work and records the increase in cached page accesses.
- `ordinary-measurements.json` and `advanced-measurements.json`: complete-run reports for both databases. Each style contains 12 timed observations and four marked warmups. All save exactly 100 connections with identical complete saved-output hashes. Slow observations are included; complete-run ranges overlap.
- `source.json`, `binaries.json`, `starting-dawgs.patch`: source identities, input shape, unchanged limits, measured binary hashes and the pre-existing DAWGS changes used in the reference build. The reference includes the already committed #11 preparation fix. The patch reconstructs the starting runtime source on the stated DAWGS commit; the pre-existing untracked ordinary-query test template is not needed to build the measurement command.
- `final-runtime-files.json`: SHA-256 fingerprints of the final translator files changed by this task, including the later private bookkeeping adjustment.
- `validation.json` and `coverage.json`: completed validation commands, outcomes, coverage comparison and final container state. Detailed logs and coverage profiles remain under ignored `work/path-followup/`.

The before build contains the demonstrated association bug. It is a valid performance reference for this membership query because its DISTINCT removes the incorrect combinations and every complete returned value matches. It is not a reference for correct route counts with retained input columns. That separate discrepancy is checked against written expectations and Neo4j.

## Repeat the measurements

Use the same isolated synthetic fixture and local ports as [the earlier repeat instructions](../preparation-followup/README.md#repeat-the-focused-comparison). Preserve existing storage and container states. The input graph contains 100,000 objects, 100,316 relationships, 100 relevant users and two authorities. Setup/loading is outside the timed runs. Do not run builds or tests during timing.

Build `before` and `after` in a fresh directory under `work`. Both use BloodHound's commit recorded in `source.json`. For the reference DAWGS source, export the recorded DAWGS commit into a separate directory under `work`, then apply `starting-dawgs.patch` there. Point a copy of the pilot's `go.mod` at that source with a local `replace`, and build with `-modfile`; do not alter or revert the working DAWGS checkout. Build `after` against the corrected working checkout. Preserve both binaries and their source versions for later review.

From `tools/esc1-poc`, run the ordinary example comparison with a directory containing those two binaries:

```sh
python3 evidence/path-input-followup/measure-ordinary.py work/path-repeat
```

This is the exact measurement script used here. It alternates before/after order, runs backends sequentially, checks the complete saved-output hashes, and refuses to overwrite `measurements.json`. Its fixed test connections and five-minute run allowance match the earlier study. For the advanced example, use [the original script](../preparation-followup/measure.py) with another fresh output directory containing the same two binaries. Keep the two styles separate.

Build [the existing read-only inspector](../preparation-followup/inspect.go) against each DAWGS version and use it after complete-run measurements finish:

```sh
CONNECTION_STRING=postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison \
  work/path-repeat/inspect-after evidence/preparation-followup/reach-input-check.cypher work/path-repeat/input-after
CONNECTION_STRING=postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison \
  work/path-repeat/inspect-after examples/esc1-cypher/membership_reach.cypher work/path-repeat/membership-after membership
```

Repeat the second command with unique output prefixes: one warmup per build, then three alternating measured pairs. It writes the SQL, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` and complete answers. Compare answers as unordered rows of complete values. These instrumented times include measurement overhead and are separate from the complete-run observations.

## Correctness checks

Use the separate disposable correctness fixtures, not the measurement graph. The new shared template has 33 written-answer cases, including `SET` and `CREATE`. `TestPostgreSQLPathInputsWithoutOptimizations` repeats those expectations with PostgreSQL optimizations disabled; normal template execution checks the default setting and Neo4j. These checks also vary a reused parameter between nonempty, empty and null inputs.

From the BloodHound root, run the full DAWGS command once for each database:

```sh
CONNECTION_STRING=postgresql://bloodhound:experiment-only@127.0.0.1:25432/esc1_poc_test \
  POC_MODULE_CACHE=/home/cthompson/go/pkg/mod CGO_ENABLED=1 \
  make -C tools/DAWGS test_all \
  GO_CMD="$PWD/tools/esc1-poc/scripts/go.sh -C ../DAWGS" \
  METRICS_DIR="$PWD/tools/esc1-poc/work/path-repeat/coverage-pg"
# Repeat with CONNECTION_STRING=neo4j://neo4j:experiment-only@127.0.0.1:27687/neo4j
# and a separate coverage-neo4j directory.
```

The full DAWGS suite leaves several empty named graphs in PostgreSQL. The pilot requires exactly one empty graph named `default`. Before the pilot checks, use a fresh disposable fixture or inspect and remove only empty catalog entries owned by the DAWGS tests. This run removed nine verified empty entries; the guard remained enabled. Neo4j must also be empty. The retained validation record distinguishes two setup rejections from the final calculation checks.

The affected pilot checks use its direct adapters' address format. From `tools/esc1-poc`:

```sh
export POC_MODULE_CACHE=/home/cthompson/go/pkg/mod
export NEO4J_TEST_URI=bolt://127.0.0.1:27687
export ESC1_POC_POSTGRES_TEST_URI=postgresql://127.0.0.1:25432/esc1_poc_test
./scripts/go.sh test -race -count=1 ./internal/runner ./internal/graphdb
./scripts/go.sh test -race -tags integration -count=1 -timeout 30m \
  -run '^Test(CypherAlternatives|CypherAlternativesKeepSavedResults|NativeLatePublicationFailureRestoresPropertiesAndIDs)$' \
  ./internal/graphdb
```

These cover the complete ordinary and advanced alternatives against written expectations, plus failures that must preserve previously saved results. The test-suite timeout does not change analysis limits. The unrelated write-limit issue (#10), general undirected variable-length support and the earlier depth-limit review remain separate work. No commit or deployment is part of this task; `just prepare-for-codereview` remains required before a manual commit.
