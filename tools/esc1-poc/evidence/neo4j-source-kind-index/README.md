# Neo4j import without an ID index for extension source kinds — 16 October 2026

Evidence for [TODO #16](../../TODO.md#16-index-extension-objects-by-id-on-neo4j-so-large-uploads-stay-fast). The measurements are in [results.json](results.json); saved command output is under [raw](raw); the scripts that recreate the test files and the direct Neo4j timings are under [scripts](scripts).

## What was found

When BloodHound imports an OpenGraph object, it looks for an existing object with the same ID and the same source kind. The source kind is the `source_kind` value in the upload's `metadata`, and it becomes a Neo4j label. The statement is written by the DAWGS Neo4j driver (`drivers/neo4j/cypher.go`), for example:

```
unwind $p as p merge (n:TestBase {objectid:p.objectid}) set n += p, n:TestBase, n:TestNode
```

Neo4j can only find the object quickly when that label has an ID (`objectid`) index. BloodHound asks DAWGS for ID indexes only on its own built-in object types. The DAWGS Neo4j driver (`drivers/neo4j/index.go`) creates one index per object type in that fixed list, such as `base_objectid_constraint` and `user_objectid_constraint`. Nothing creates one for an extension's source kind: registering the source kind during import (`RegisterSourceKind` in `cmd/api/src/services/graphify/tasks.go`) only records its name, and installing an extension schema adds no index.

Without the index, Neo4j checks every existing object with that label, one by one, for every object imported. The checks cost about 0.85 microseconds each in the lab, so an import of N new objects costs roughly N × N ÷ 2 checks. Doubling the upload roughly quadruples the time.

The PostgreSQL driver keeps one ID index across all object types (`drivers/pg/query/format.go`), so it should not be affected. PostgreSQL was not tested.

## Measurements

### Directly in Neo4j

One `MERGE` statement per run, each with a fresh label, in the lab's Neo4j 4.4.42. Every figure includes about 1.24 seconds of `cypher-shell` start-up. With an index, the timer starts after the index is ready.

| New objects | No index | With index |
|---|---|---|
| 1,000 | 1.55 s | 2.07 s |
| 2,000 | 2.76 s | |
| 4,000 | 5.50 s | |
| 8,000 | 20.83 s | 4.07 s |
| 16,000 | 95.12 s | |
| 20,000 | | 4.60 s |
| 50,000 | | 6.26 s |

A second test separated objects already saved from objects created in the same statement. With 50,000 objects created and saved first, 2,000 further `MERGE`s took 84.61 s for new IDs and 85.99 s for existing IDs, compared with 2.93 s on an empty label. Saved objects cost as much to check as new ones; the cost is simply the number of objects with the label.

### Through the lab BloodHound

Uploads with OpenHound's `graph upload`. Job time includes BloodHound's analysis after import; ingest time is BloodHound's own "Ingest run finished" figure from [the log](raw/lab-ingest-log-lines.txt). Job times are in [raw/lab-ingest-jobs.txt](raw/lab-ingest-jobs.txt).

| Job | Upload | Ingest | Whole job |
|---|---|---|---|
| 16 | 20,000 objects, source kind with a hand-made ID index | 0.61 s | 33 s |
| 17 | The same file, source kind without an index | 129.47 s | 157 s |
| 13 | 12,000 objects and 6,000 relationships in five parts, nearly empty label | | 62 s |
| 14 | 100,000 objects and 50,000 relationships in three parts | Part 1 (50,000 objects, label already holding 12,002): 2,201 s | Cancelled |

Job 14 is the upload that exposed the problem. BloodHound's container was redeployed one minute into the job by something outside this investigation, and the new container restarted its three parts at 13:45:43. Part 1 finished at 14:22:24. The job was then cancelled: its two remaining database transactions were terminated, its two jobs were marked cancelled in the application database, and its remaining tasks were deleted. Parts 2 and 3 were on course to take about one and two and a half hours respectively.

### Upstream BloodHound `main`

Upstream BloodHound `main` (`eaf9c78`, reporting itself as v9.8.0) was built from source with its own Dockerfile and run with its example Docker Compose file, whose default graph database is Neo4j 4.4.42. It ran on separate ports and storage from the lab. The only changes to the compose file were the locally built image and a fixed administrator password ([diff](raw/upstream-compose-changes.diff), [settings](raw/upstream-compose.env)).

| Check | Result |
|---|---|
| Indexes at start | 302, including 42 ID constraints, all for built-in object types |
| After installing an extension schema for `PX20k` | Still 302; none for `PX20k` or `PX20k_Node` |
| After importing the `PX` collections | No index for any `PX` label |
| Import 20,000 objects, index created by hand first (`PY20k`) | 5.85 s |
| Import 10,000 objects, no index (`PX10k`) | 48.66 s |
| Import 20,000 objects, no index, extension schema installed (`PX20k`) | 157.29 s |
| Query plan for the import statement, no index | `NodeByLabelScan` then `Filter` |
| Query plan with the index | `NodeIndexSeek` |

These upstream figures were transcribed from the session output, because the throwaway stack was deleted after the run. The lab's plans for the same statement are saved in [raw/lab-query-plans.txt](raw/lab-query-plans.txt): `NodeByLabelScan` plus `Filter` for `TestBase`, and `NodeUniqueIndexSeek` for the built-in `Base`.

## Why local changes are not the cause

- The DAWGS Neo4j driver folder is identical to the fork's upstream base (`1d7915c`) and to DAWGS `main` (`c107686`). The fork's two DAWGS commits change the PostgreSQL translator and driver, and add a database switch whose index check passes straight through to the real database.
- The fork's BloodHound commits do not change import (`services/graphify`), the startup index check (`migrations/graph.go`) or `packages/go/graphschema`.
- Upstream `main`, run without any fork code, gives the same result. The 20,000-object file without an index took 157 seconds of ingest upstream and 129 seconds in the lab.

## Workaround and its limits

`CREATE INDEX IF NOT EXISTS FOR (n:<SourceKind>) ON (n.objectid)`, run once per source kind directly in Neo4j. BloodHound's Cypher page cannot make changes by default. DAWGS's index check (`assertSchema` in `drivers/neo4j/index.go`) removes every index that is not in the list it is given, and BloodHound gives it only the built-in list at startup. The lab log shows it removing this project's own `poc_endpoint_*` indexes when it starts, so a hand-made index will not survive a restart. That removal is inferred from the code and that log line; a hand-made index was not restarted in this investigation.

## How to repeat

1. Create the files: `python3 scripts/make-test-files.py /some/empty/folder`. They have the same object counts, relationship counts and structure as the originals, but are not byte-for-byte copies: property text differs slightly, and the large file is about 55 MB instead of 56 MB.
2. Direct Neo4j timing: `scripts/neo4j-merge-timing.sh ZZPerfA8000 8000` and `scripts/neo4j-merge-timing.sh ZZPerfB8000 8000 index`. Delete the test labels and indexes afterwards, as the script's comments describe.
3. Through BloodHound: create the index for `PerfIdx` in Neo4j, then upload `PerfIdx.json` and `PerfNoIdx.json` with `openhound graph upload <file> --batch-size 0`, and compare the "Ingest run finished" durations in the BloodHound log.
4. Upstream: build `dockerfiles/bloodhound.Dockerfile` from a clean upstream checkout, start `examples/docker-compose/docker-compose.yml` with the image and ports changed, install `schema_PX20k.json` with `openhound schema upload`, create the index for `PY20k`, and upload the three `PX`/`PY` files.

## Database state afterwards

- The direct Neo4j test labels and indexes (`ZZPerf…` and `perfidx_objectid`) were deleted directly in Neo4j after measurement. They never passed through BloodHound.
- The lab test data that came in through BloodHound was deleted with BloodHound's own delete-by-source-kind request (`POST /api/v2/clear-database` with source kinds `TestBase`, `PerfIdx` and `PerfNoIdx`). It removed 102,002 objects in 19 seconds, took the three source kinds out of BloodHound's list, and started BloodHound's normal full analysis. `GH`, `Okta` and the built-in data were not touched.
- Jobs 14 and 15 remain in the file-ingest history as cancelled.
- The upstream stack, its storage, its source checkout and its image were deleted. Docker's shared build cache from the upstream build was left in place, because it is shared with other image builds.

Source versions are in [raw/versions.txt](raw/versions.txt).
