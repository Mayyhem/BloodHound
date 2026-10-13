# Focused preparation and reachability evidence

See [the report](../../PREPARATION_AND_REACHABILITY.md) for conclusions and limitations. This is a separate, small follow-up to the earlier comparison. All graph data here is synthetic.

These files retain the initial #11/#12 investigation, including the failing path-input query. The later #13 correction and completed #12 direct improvement are recorded separately in [path-input-followup](../path-input-followup/README.md); the older observations below are not measurements of the corrected code.

- `measurements.json`: complete reports, including separately marked warmups. Three timed repetitions per version/backend: 12 timed observations and four warmups. Versions alternate, backends run sequentially. Complete saved-answer hashes agree within and across databases.
- `pg-known-changes.json`, `neo4j-known-changes.json`: one affected-rule selection diagnostic per database after restart, with saved-answer checks; not part of the repeated timing table.
- `pg-settings.json`, `container-settings.txt`: actual PostgreSQL settings and container image/resource configuration, recorded after the study.
- `source.json`, `binaries.json`, `implementation.patch`: source starting points, pre-existing DAWGS-change fingerprint, fixture shape, binary hashes and production patch. DAWGS was not edited. The patch also contains the later reporting-only adjustment for rejected files and the stored elapsed field, absent from both timed binaries.
- `identity-before.cypher`, `identity-after.cypher`: the only calculation difference between the timed builds.
- `identity-*-plan.{sql,json}`: one matched later pair of actual PostgreSQL plans after loading finished. Earlier diagnostics that overlapped initial Neo4j loading were excluded.
- `reach.cypher`, `reach-plan.{sql,json}`: a simple distinct-member query and its measured execution plan.
- `membership-plan.{sql,json}`: the complete ordinary membership query with the fixture's 101 starting objects.
- `reach-input-check*`: the query, translated SQL and PostgreSQL/Neo4j answers demonstrating incorrect input/result association. Written expectation: `requested_root` must equal `actual_root` in every row. PostgreSQL violates that condition twice; Neo4j satisfies it.
- `reach-input-release-*`: separate checks of that same query using clean DAWGS v0.9.0 and v0.9.1. Both fail with a missing `s0` relation; they do not reproduce the modified checkout's wrong answers. The JSON record identifies source commits, method and limits; each SQL file is the unmodified translation emitted before execution failed.

Plans use `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` and execute the read query. Their rows and loops are observed, unlike the older comparison's estimated plans. They include measurement overhead and are not complete-run timing samples. Kind numbers and object IDs belong to this generated database. `inspect.go` reads its kind mapping before translating a query.

## Repeat the focused comparison

Use new storage for two isolated containers with the images and test ports in [compose.yaml](../../compose.yaml). Preserve existing database storage and stopped states. Do not reuse application data or run tests during timing. Run these commands from `tools/esc1-poc`; the Go wrapper keeps generated files under `work` and disables downloads.

Build the current version:

```sh
mkdir -p work/preparation-repeat
POC_MODULE_CACHE=/home/cthompson/go/pkg/mod ./scripts/go.sh build \
  -o work/preparation-repeat/after ./cmd/cypher-comparison
```

Build the reference with the same timers but the old identity query, using a Go overlay so the working source stays unchanged:

```sh
python3 - <<'PY'
import json
from pathlib import Path
source = Path('internal/runner/runner.go').resolve()
replacement = Path('work/preparation-repeat/reference-runner.go').resolve()
current = Path('evidence/preparation-followup/identity-after.cypher').read_text().strip()
previous = Path('evidence/preparation-followup/identity-before.cypher').read_text().strip()
contents = source.read_text()
assert contents.count(current) == 1
replacement.write_text(contents.replace(current, previous))
Path('work/preparation-repeat/overlay.json').write_text(json.dumps({'Replace': {str(source): str(replacement)}}))
PY
POC_MODULE_CACHE=/home/cthompson/go/pkg/mod ./scripts/go.sh build \
  -overlay work/preparation-repeat/overlay.json \
  -o work/preparation-repeat/before ./cmd/cypher-comparison
```

Create PostgreSQL database `cypher_comparison` in the dedicated new container. Load 100,000 objects into each empty comparison graph, with 100 relevant users, two authorities and the default `users` answer shape:

```sh
CONNECTION_STRING=postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison \
  work/preparation-repeat/before -action load -objects 100000 -members 100 -authorities 2
CONNECTION_STRING=neo4j://neo4j:experiment-only@127.0.0.1:27687/neo4j \
  work/preparation-repeat/before -action load -objects 100000 -members 100 -authorities 2
python3 evidence/preparation-followup/measure.py work/preparation-repeat
```

The script uses those fixed local connections and refuses to overwrite `measurements.json`. It checks 100 saved connections on every run and compares complete saved-output hashes. It saves both binaries' hashes. Its five-minute run allowance applies equally to both versions. This repeats the small comparison, not the earlier matrix.

Inspect PostgreSQL plans after measurements finish:

```sh
POC_MODULE_CACHE=/home/cthompson/go/pkg/mod ./scripts/go.sh build \
  -o work/preparation-repeat/inspect evidence/preparation-followup/inspect.go
CONNECTION_STRING=postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison \
  work/preparation-repeat/inspect evidence/preparation-followup/identity-after.cypher work/preparation-repeat/identity
CONNECTION_STRING=postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison \
  work/preparation-repeat/inspect examples/esc1-cypher/membership_reach.cypher work/preparation-repeat/membership membership
```

The last `membership` argument selects all generated enrollers as the complete input parameter. The inspector records SQL, the actual plan and complete answers under a two-minute deadline and a read-only transaction. Use the supplied read queries on the dedicated synthetic database.

## Correctness checks

Use separate empty fixture storage after measurement. The usual integration settings are:

```sh
export NEO4J_TEST_URI=bolt://127.0.0.1:27687
export ESC1_POC_POSTGRES_TEST_URI=postgresql://127.0.0.1:25432/esc1_poc_test
export POC_MODULE_CACHE=/home/cthompson/go/pkg/mod
./scripts/go.sh test -race ./internal/runner ./internal/graphdb
./scripts/go.sh test -race -tags integration -count=1 -timeout 10m ./internal/graphdb \
  -run '^(TestPreparationIdentityChecks|TestPostgresESC1IdentityGuard|TestPublicationGuardsPreserveGraph|TestIsolatedMigrationMetadataIdentity|TestNativeLatePublicationFailureRestoresPropertiesAndIDs)$'
./scripts/go.sh test -race -tags integration -count=1 -timeout 10m ./internal/graphdb \
  -run '^(TestCypherAlternatives|TestCypherAlternativesKeepSavedResults)$/^(baseline|no_template_permissions|parallel_membership_links|missing_identifier|blank_identifier|duplicate_identifier|membership_self_link)$'
./scripts/go.sh vet -tags integration ./internal/runner ./internal/graphdb
```

The first integration group covers the exact identity exception and early/late failures. The second checks the existing five authoring variants against written expected answers, including empty results, parallel links and failure preservation. This avoids repeating the large comparison or unchanged DAWGS suites. Outcomes and final container state are recorded in `validation.json`.

Original databases and application/rule-service containers are untouched. New measurement and fixture containers are stopped after completion; generated data remains private under `work/preparation-followup`. No stage, commit or deployment was performed. Before a manual commit, the repository still requires `just prepare-for-codereview`.
