# Repeat the Cypher comparison

This tool is for the two isolated local test databases. It does not upload rules, deploy BloodHound, or contact a remote service. See [the comparison report](../../CYPHER_COMPARISON.md) for the actual measurements and limits.

Run commands from `tools/esc1-poc`. Use Go from the recorded version and the existing module cache:

```sh
POC_MODULE_CACHE=/home/cthompson/go/pkg/mod ./scripts/go.sh build \
  -o work/cypher-comparison/measure ./cmd/cypher-comparison
```

The command accepts only Neo4j on `127.0.0.1:27687` or PostgreSQL database `cypher_comparison` on `127.0.0.1:25432`. Those are the dedicated test containers, not the BloodHound application's databases. The runner script uses the benign test credentials from their existing configuration. Stop other test and benchmark processes before measuring. The configured 32 MiB DAWGS graph-query allowance applies to certain Go path calculations, not total database query memory. Record server settings separately; the original experiment used PostgreSQL work memory of 4 MiB and shared buffers of 128 MiB, and Neo4j page cache of 512 MiB with no explicit transaction-memory cap. Observed process memory and host reserves remain separate checks.

## Preserve and rebuild the starting version

Before changing either database, preserve its stopped data directory and record the container image, settings and original running/stopped state. The original experiment keeps these copies under private `work/cypher-comparison/saved-state/`. Also retain active/draft rules if the application is involved; this comparison does not change them.

The fixed source commits are:

- BloodHound: `7d57d4d11f34038991c491983a9490a20cb7ac58`
- Separate DAWGS checkout: `5f16c574e48045472d15a447c2fa9707d394a4bd`

Use `git archive` for those commits and extract BloodHound into a **new empty** `work/cypher-comparison/starting-source` directory. Extract DAWGS into its `tools/DAWGS` subdirectory. Keep the archives and their SHA-256 hashes; do not reset either working checkout.

Apply `reference-instrumentation.patch` with `patch -p1` at the saved BloodHound root. It adds phase timing and an optional answer observer; it does not change which answers are calculated. Copy this command's `cmd/cypher-comparison` directory and the pilot's `go.mod`/`go.sum` into the saved pilot. Those additional dependencies let the command call the actual built-in ESC1 routine. From the saved `tools/esc1-poc`, build:

```sh
POC_MODULE_CACHE=/home/cthompson/go/pkg/mod ./scripts/go.sh build \
  -o ../../../measure-reference ./cmd/cypher-comparison
```

The original archives and original uninstrumented binary remain untouched. Record hashes of both measurement binaries, the patch, and all compared query files.

Create `work/cypher-comparison/starting-manifest.json` with `created_utc` as an ISO timestamp and `experiment_budget_hours` set to the agreed total budget. The original experiment uses 24 hours, including preparation, checks, loading, measurements and cleanup. Reruns need their own recorded starting time and fresh output directories.

## Run a matched comparison

Prepare an empty PostgreSQL database named `cypher_comparison` in the dedicated test container. The ordinary test suite uses different PostgreSQL databases. Neo4j tests and comparisons share the dedicated test server, so they must run at different times.

```sh
python3 scripts/comparison/run.py --backend pg --label final-small \
  --objects 1000 --members 100 --authorities 2 --shape users --rounds 5
```

Repeat with `--backend neo4j`. `--replace` first clears a graph only after checking that every object belongs to this generator or is one of its saved helpers/run markers. It refuses unrelated fixtures. Cleanup first reads at most 2,000 object IDs, then deletes only those IDs. A direct `WITH ... LIMIT ... DELETE` lost its limit in the observed PostgreSQL translation, so the measurement tool does not rely on that form. Cleanup is outside the timed calculations, and both measurement builds use the same code. Preserve the original database before using it.

The dedicated Neo4j test suite can leave ordinary `poc_endpoint_*` indexes on an empty graph. The measurement command installs BloodHound's combined schema, whose uniqueness constraints cannot replace those indexes automatically. Inspect the empty test database and preserve its snapshot before removing those test-created indexes. This setup step is not a query-language change; never remove indexes from an application database to run this tool.

Use `--probes` for a separate study with concurrent searches and waiting writers. `--full-only` measures repeated first/full calculations while the complete workload sequence remains in the primary study. `--batch-rows 50` applies the same smaller piece size to every version, including the starting program. The `final-divided` study uses 51 authorities to exercise two pieces under that explicitly changed setting. Do not mix it with the default-500 study or imply the default limit was reached.

`--prepare-statistics` runs PostgreSQL `ANALYZE` after loading and before any measured version. Its time is recorded separately. This avoids changing row-count estimates during early repeats after a large load. The original 100,000-object probe study showed wide early variation, so the follow-up repeats five times with this condition recorded. It is ordinary database preparation shared by both program versions, not a speedup attributed to an instruction.

For a million objects or more, the script also requires `--projected-memory-bytes`, `--projected-storage-bytes`, `--projected-seconds`, and `--projection-note`. Derive these from the preceding smaller run. Include database copies, logs, temporary files, checks and cleanup. It records the decision and refuses advancement when twice the projected memory exceeds 80% of the assigned memory, twice the projected storage would leave less than 25% free, or the remaining total budget is insufficient. Active runs monitor these reserves and stop after new writes to swap in three successive samples. Reads that bring previously swapped pages back are recovery, not new pressure; the available-memory reserve is still checked every time. The completed ESC1 studies used the earlier, stricter check of either direction; none was stopped by it. The distinction was corrected only while completing the separate batching check. Do not raise a limit for just one version.

The generated graph contains one connected AD domain, shared certificate trust, one template, a configurable number of authorities, 25 groups including a local group and a 20-group chain, overlapping membership, unrelated directory users, and a shared vault with accounts referring to directory users. It loads the database directly; this does not test collection-file import. `--shape users` produces one connection per relevant user. `--shape groups` exercises compact group answers. The vault input is present, but the measured analysis here is ESC1; this is not a measurement of running a CyberArk extension.

Each version starts with restored generated inputs and no saved extension outputs. Three repeats alternate the version order. Each repeat measures the first calculation, full recalculation, unchanged input, an eligibility change, removal of one publishing authority, and removal of the final support. The latter mutations use actual database deletes. No missing collection record is treated as deletion.

An extra, untimed run retains every intermediate answer. Only the membership/kind lists known to be sets are sorted. Complete rows, including repeated rows, are compared. The saved-output comparison includes properties and helpers; the separate identity-sensitive hash also covers database object numbers and the successful-run marker. Sorted `.answers` and intermediate files make exact differences inspectable if hashes disagree.

## Interpret the evidence

`measurements.json` and `.csv` hold individual observations. Per-run JSON contains which queries actually ran, their phase times and outcomes. Resource files hold samples taken approximately twice per second; Docker calls can make the interval longer. Application peak RSS and CPU come from `/usr/bin/time`. Database memory includes its container's file cache; CPU, block I/O and network counters are retained where Docker exposes them. Samples are not proof of the true peak between observations. The sampler tolerates files and directories disappearing during normal cleanup. The final Neo4j search study was resumed after an earlier sampler version failed on a removed directory; its continuation note preserves the interrupted attempt and both script hashes. Only unfinished cases were resumed, using unchanged measurement binaries and queries.

Use `engine_ns` for the extension's complete calculation/saving time. `operation_ns` additionally includes opening the run and waiting for any in-flight search probe to finish. Answer checking has its own time. Do not add overlapping query durations. The first repeat is the first observed run after loading, not a claim that OS and database caches were empty.

Native reference preparation builds the shared ADCS cache, which also supports calculations beyond ESC1. Its calculation invokes the actual built-in ESC1 routine and writes proposed jobs to bounded temporary storage. Sorting, stable-ID resolution and hashing are timed separately. Native edges are not saved in this reference. Whole-process memory includes measurement/checking work, so it is not an isolated measurement of native algorithm memory.

The command opens the database and asserts its standard schema before the operation timer. This is measurement-tool setup; extension preparation, calculations, result combination and saving are included in the engine timer. Native calculation uses the overall run deadline; the extension's per-query deadline applies to its named rule queries.

The final studies use one fixed pair of measurement binaries. Earlier exploratory builds received a tighter native evidence-file allowance and earlier removal of consumed files. No native ESC1 calculation changed. The source note is retained in private `measurement-revisions/native-check-bounds/change.json`; binary hashes distinguish the studies. The original DAWGS code remains in the starting binary, including the path-counting error corrected in the updated binary. Do not replace binaries while a study is running.

Search probes exercise stable-ID, name-prefix and membership lookups directly against the database. Waiting writers use the same application write gate, without modifying inputs. They do not measure HTTP routing, browser rendering, collection import, or a full native-plus-extension analysis interval. Retain observation counts; do not claim a reliable 95th percentile from too few observations.

Keep private database copies and raw logs out of Git. Publish only synthetic measurements and the settings needed to interpret them. Restore the original saved database contents and container states when finished.

## Regenerate the public tables

Select completed studies explicitly. The summary command refuses incomplete studies. It calculates median/range, direct percentage differences, phase/resource tables, and probe counts/percentiles from retained individual observations:

```sh
python3 scripts/comparison/summarize.py \
  final-small/pg final-small/neo4j \
  final-divided/pg final-divided/neo4j \
  final-compact-probes/pg final-compact-probes/neo4j \
  final-million/pg final-million/neo4j \
  --attempts many-authorities/pg million-users/pg
```

Add later completed study paths to that command. A deliberately stopped study must have its own `stopping-decision.json`; it is exported separately and receives no failed-run speed ratio. In the 501-authority PostgreSQL case, the first starting run completed correctly, but repeating the shared trust calculation across the matrix projected over eight hours. The next repetition was cancelled, and its saved-state identity matched the previous successful run. This is a budget decision, not proof of a batching capacity limit.

## A separate check of the batching input limit

After both `final-million` studies finish and before replacing their generated graphs, run:

```sh
python3 scripts/comparison/batch_capacity.py
```

This creates a deliberately wide-input rule in private working files: 1,000 rows, each with 64 KiB of text that the next query checks. Explicit identifier bounds select those 1,000 users before returning the wide text. A prefix alone also selects hundreds of thousands of unrelated users and caused the initial setup to hit the host stop rule before any matched capacity observation. That attempt is preserved separately. It compares `batch-on` with the same complete query under the unchanged 32 MiB parameter allowance and 500-row batch size. It repeats in alternating order three times on each database, checks complete saved answers, and checks that a rejected whole-input calculation preserves the previous saved result and successful-run record. Unexpected answers or failures stop the check.

Its separate `batch-capacity.json` and CSV identify completed and failed attempts explicitly; failed elapsed time is not a completion time or a speed ratio. This is a controlled demonstration of the input-size limit, not a claim about ESC1 capacity with ordinary collected data. Original databases still require restoration after all experiments finish.

The final capacity check restarted the dedicated test servers after that setup pressure, kept the inactive backend stopped, and retained the first completed PostgreSQL observation when resuming the unfinished cases. A second monitoring correction distinguishes swap recovery reads from new writes to swap; the 20% memory reserve, 32 MiB input limit and all other limits remain unchanged. The stopped setup and continuation notes accompany the capacity evidence. No ESC1 measurement was repeated because of these measurement-tool corrections.
