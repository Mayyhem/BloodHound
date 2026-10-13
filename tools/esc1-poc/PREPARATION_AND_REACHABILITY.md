# Preparation costs and ordinary reachability

13 October 2026. This is the focused follow-up for TODOs #11, #12 and #13, separate from the completed [large Cypher comparison](CYPHER_COMPARISON.md).

**#11 is implemented:** reports separate the preparation steps, and an equivalent identity check avoids unnecessary type-name lookups. **#13 now corrects path/input association. #12 is completed through its direct-correction approach:** the unchanged ordinary membership query does less unnecessary work, with identical complete answers. Its instrumented PostgreSQL execution median fell from 1.278 seconds to 0.127 seconds in the focused follow-up. No separate reachability algorithm, author instruction, or additional path-limit change was introduced.

## What changed before an analysis

The runner now reports time spent preparing database types and indexes, opening the transaction, selecting rules, and checking identities. The native adapter also counts query-language checks and Neo4j setup outside the runner. Failed steps retain their elapsed time; steps never attempted remain zero. These are elapsed times around program calls, not separate measurements of the database server's CPU work. A driver that opens its connection on its first query charges that waiting time to the query. Temporary-file setup and cleanup are included in overall preparation but not the named steps.

The starting identity check called `labels(item)` for every object to decide whether it was an isolated migration record. PostgreSQL translated that into a lookup of the names associated with each object's stored type numbers. On the synthetic graph, that inner lookup ran 100,002 times, including the saved helper and success record.

The revised condition first asks whether the object has the `MigrationData` type. Only a possible migration record needs the exact one-type check and the check for incident links:

```cypher
MATCH (item)
WHERE NOT (
  item:MigrationData AND size(labels(item)) = 1
  AND NOT (item)--()
)
WITH item.objectid AS objectid, count(*) AS matches
WHERE objectid IS NULL OR trim(objectid) = '' OR matches <> 1
RETURN objectid LIMIT 1
```

Counting rows is equivalent to counting objects here: this is a required `MATCH`, so every row contains an object. PostgreSQL can count without carrying the complete object into the count. On this generated graph, the revised type-name lookup never ran. Both recorded [identity plans](evidence/preparation-followup/README.md) still inspect the whole graph and group all nonexempt IDs.

The migration exception has exactly the same meaning: only an object whose sole type is `MigrationData` and which has no incoming, outgoing, or self connection is excluded. Unrelated objects, objects with no type, and mixed-type migration records remain checked. Missing, null, blank and duplicate identities still stop the run before calculation. There is no retained result between runs, no optional author check, and no change to scope validation, limits, or saving.

## Focused measurements

Both databases received the same generated graph: **100,000 input objects and 100,316 relationships**, including 125 membership objects, 490 membership pairs, two certificate-authority/template combinations, and 100 expected output connections. The relevant input share was 0.132%. The two measurement builds used identical code except for the internal identity query; both included the new timers. The advanced example was measured because both authoring styles use this same check.

One warmup per version was followed by three measured repetitions per version, alternating their order. Runs and backends ran sequentially. Original databases remained stopped and untouched; the experiment used new container storage. Server caches were not cleared between repetitions. Database schema installation and initial loading happened before these measurements. Each analysis still includes its own catalog, scope and index preparation. The five-minute whole-run allowance was the same for both versions; the existing fifteen-minute per-query allowance and size limits were unchanged. No manual statistics refresh, application-search load, or resource-capacity study was added.

Preparation-step medians, milliseconds:

| Step | PostgreSQL before | PostgreSQL after | Neo4j before | Neo4j after |
| --- | ---: | ---: | ---: | ---: |
| Query-language checks | 9.796 | 8.726 | 10.829 | 11.853 |
| Database types, scope and indexes | 39.239 | 39.083 | 187.572 | 179.543 |
| Open transaction | 30.632 | 26.675 | 0.017 | 0.018 |
| Select rules for this full run | 0.561 | 0.690 | 0.572 | 0.420 |
| Check identities | 1,825.982 | 1,366.815 | 241.119 | 223.927 |

Complete phase measurements, seconds, median [fastest–slowest]:

| Database and phase | Before | After |
| --- | ---: | ---: |
| PostgreSQL preparation | 1.912 [1.877–1.928] | 1.446 [1.276–1.672] |
| PostgreSQL complete run | 9.423 [9.320–11.320] | 8.332 [7.908–9.054] |
| Neo4j preparation | 0.447 [0.422–0.490] | 0.416 [0.391–0.458] |
| Neo4j complete run | 2.026 [1.664–2.195] | 1.890 [1.644–2.085] |

On PostgreSQL, the identity check's median fell **25.1%** and preparation fell **24.4%**. Complete-run medians differed by **11.6%**, but calculation and saving also varied; that whole difference cannot be attributed to identity checking. The Neo4j ranges overlap substantially, so these observations do not establish a Neo4j speedup. Medians of individual steps need not sum to the median of the whole run.

A separate diagnostic after restarting the measurement containers supplied a confirmed `User` change. Selection, including reading the previous saved plan and choosing affected rules, took **4.446 ms on PostgreSQL and 359.674 ms on Neo4j**. Those single observations include startup/cache effects and are kept separate from the repeated full-run table. Both retained the same complete saved answers. A true no-change skip is checked in the runner tests and does not claim an identity scan.

This establishes a useful smaller change. It does not explain every millisecond of the previous million-object study, establish a new million-object result, or demonstrate production capacity. The identity query still sorts/groups IDs across the whole graph. PostgreSQL also spends time preparing executable code for these queries; removing that cost was not attempted here.

Every measured run saved the same exact 100 connections and helper properties. The saved-output SHA-256 was `820300fae60fa274af78a6d210938073ec36be9ca6d594a165e96ae431537228` on both databases and both versions. Relationship IDs can legitimately change under replacement policy; success-record timestamps also change. Neither is confused with the stable saved-answer comparison.

## What the ordinary reachability plans showed

The initial investigation, before the input correction below, found that a simple query asking for distinct members beneath one group used indexed lookups and completed its instrumented PostgreSQL execution in 2.298 ms. It expanded 166 paths and returned 124 distinct objects. That plan does not justify introducing a separate calculation for every distinct-reach query.

The complete ordinary `membership_reach.cypher` query was more revealing. Its supplied input contained 101 starting objects. The first expansion produced 267 rows, then PostgreSQL combined every one with all 101 input rows: **26,967 rows before `DISTINCT` reduced them to 125 objects**. The full instrumented plan took 839.302 ms; approximately 747 ms was database code-generation work. Those single plan observations include measurement overhead and are diagnostics, not repeated speed comparisons.

The extra combinations also expose an answer error if the query retains its starting-object column. This small check compares two groups:

```cypher
UNWIND ['Comparison_Group_000000000',
        'Comparison_Group_000000001'] AS requested_root
MATCH (member)-[:MemberOf|MemberOfLocalGroup*1..]->(root)
WHERE root.objectid = requested_root
RETURN requested_root, root.objectid AS actual_root, count(*) AS routes
ORDER BY requested_root, actual_root
```

The `WHERE` requires both returned root names to agree. Neo4j returns those two matching pairs. PostgreSQL returns those pairs **and two mismatched pairs**. The [recorded answers](evidence/preparation-followup/reach-input-check.answers.json) and [Neo4j output](evidence/preparation-followup/reach-input-check-neo4j.txt) preserve the discrepancy. The supplied membership query discards the starting-object column and applies `DISTINCT`, which explains why the earlier exact ESC1 checks did not expose this error.

This points to how DAWGS reconnects an `UNWIND` input with an ordinary path result in `buildExpansionPatternRoot` in `translate/expansion.go`. A correction must preserve the association and legitimate repeated input rows, including empty input and zero-length paths. Simply deleting the input join could remove requested columns or lose repeated answers. This was recorded separately as TODO #13. That initial investigation did not edit DAWGS. The user subsequently requested “Fix 13, then do 12”; the implementation and measurements below supersede the earlier deferral.

## Correcting the input association, then measuring ordinary queries

DAWGS now performs each ordinary path search with access to the current incoming row. Previously, it could search using all supplied rows and then combine every path with every supplied row again. The new PostgreSQL query uses `LATERAL`, which lets an inner search read the current outer row. The path search itself still follows the authored links, direction, filters and bounds and retains all requested routes. This applies to the first variable-length step and later steps, including inputs carried through `WITH`.

An optional match needs an additional distinction: two identical input values can still represent two separate rows. DAWGS assigns each incoming row a temporary number and uses it when restoring unmatched rows. The number remains internal. Empty and null `UNWIND` inputs receive a valid SQL array type but produce no rows; comparisons against those absent elements no longer generate invalid SQL casts. An explicit zero upper bound now prevents taking even the first edge. The saving check also corrected `CREATE` after a completed `WITH` so it reads that completed input rather than referring to a missing result.

The original two-group reproduction now returns exactly the two expected pairs: group 0 with itself and 166 routes, and group 1 with itself and 46 routes. It no longer returns either mismatched pair. Repeated inputs retain their repeated routes. Required and optional matches, empty/null inputs, input-dependent endpoint filters, zero and positive bounds, both directed spellings, cycles and self-links, counts, path values, multiple path steps, and `SET`/`CREATE` values have written-answer cases shared by PostgreSQL and Neo4j. Saving cases use supported `WITH DISTINCT` inputs; this is not a claim to fix every possible write query.

For TODO #12, the preferred first step was to remove proven repeated work directly. That is sufficient here: the complete membership query still returns the same 125 objects and their complete collected values, but its first stage processes **267 rows before DISTINCT instead of 26,967**, a 99.0% reduction at that stage. The correction is part of ordinary translation of parsed path operations, without text matching, data-dependent guesses or new extension files. Correctness applies with PostgreSQL optimizations enabled or disabled. Routes, lengths, counts and ordering continue through ordinary execution; they are not replaced by a set of reachable objects.

The follow-up reused the same isolated 100,000-object / 100,316-relationship graph. Before and after builds share the #11 preparation change and all earlier local DAWGS work; only the input correction differs. Each sequence had one warmup and three alternating measured repetitions per version/backend. Tests, builds and measurements ran separately. All observations, including slow ones, were retained. Server caches were not cleared, and no statistics refresh or database setting change was made.

PostgreSQL membership query, instrumented execution in milliseconds, median [fastest–slowest]:

| Measurement | Before | After |
| --- | ---: | ---: |
| EXPLAIN ANALYZE execution | 1,277.593 [1,134.260–2,770.709] | 126.572 [124.381–140.799] |
| Query planning | 3.946 [3.884–4.866] | 4.299 [4.252–4.791] |
| Database code generation within execution | 1,170.082 [1,028.343–2,502.051] | 59.544 [57.062–67.961] |

The execution median is 90.1% lower in these observations. Much of that difference is PostgreSQL spending less time generating executable code for the changed plan; it is not all time saved by removing duplicate rows. Cached page accesses increased from 12,381 to 13,657, while physical reads and temporary blocks written were zero in both plans. This demonstrates less unnecessary row processing and lower measured execution cost, not uniformly lower resource use. It does not measure total server memory or network traffic.

Complete-run measurements in seconds, median [fastest–slowest]:

| Database and example | Before | After |
| --- | ---: | ---: |
| PostgreSQL ordinary Cypher | 17.055 [5.065–27.624] | 4.137 [3.946–20.915] |
| PostgreSQL advanced instructions | 7.757 [7.449–9.391] | 6.755 [6.536–8.744] |
| Neo4j ordinary Cypher | 3.313 [3.173–4.193] | 3.255 [3.054–3.459] |
| Neo4j advanced instructions | 2.597 [2.185–4.048] | 1.993 [1.752–6.519] |

Complete-run ranges overlap, and preparation and other queries also varied. These totals support exact-answer validation but do not establish those median differences as reliable whole-analysis speedups. No Neo4j speedup is claimed. Every run saved the same 100 connections and complete helper properties, with the same `820300fae60fa274af78a6d210938073ec36be9ca6d594a165e96ae431537228` saved-output hash on both databases. Reported temporary answer storage remained about 119 KB. The existing query/run timeouts, row, input, memory and disk allowances, scope checks and all-or-nothing saving were unchanged.

[Follow-up evidence](evidence/path-input-followup/README.md) retains the plans, exact answers, individual observations, source information and validation commands. These are small follow-up measurements, not replacements for the earlier million-object study or evidence of production capacity.

## Does the query problem affect released BloodHound?

A separate read-only check on 13 October 2026 built the existing inspector from clean DAWGS tags `v0.9.0` and `v0.9.1`, without our local DAWGS changes. Both translated the two-input query above into SQL that fails with `ERROR: relation "s0" does not exist (SQLSTATE 42P01)`. The generated query references a missing intermediate result. It returned no answers. This is a different observed symptom from the incorrectly paired answers in our modified checkout; do not describe those wrong answers as reproduced in a released version.

[BloodHound CE v9.8.0's dependency file](https://raw.githubusercontent.com/SpecterOps/BloodHound/v9.8.0/go.mod) selects DAWGS `v0.9.0`. That establishes a released-library failure for this particular custom Cypher pattern on PostgreSQL. It does not establish how many users submit such queries, an affected customer workload, or the behavior of the complete released application's request handling. The local BloodHound source allows custom Cypher through `GraphQuery.RawCypherQuery`; the inspected built-in queries did not contain this leading `UNWIND` pattern. Native ESC1 calculates from cached principal sets and does not run our extension's membership query. No evidence here establishes incorrect native ESC1 findings or corrupted saved graphs. The existing Neo4j check returned the expected answers.

The [release-check record](evidence/preparation-followup/reach-input-release-check.json) and adjacent generated SQL preserve the versions and failure. Both checks used the existing synthetic `cypher_comparison` database through a read-only transaction. Its isolated container was started briefly and returned to its original stopped state. No application data or DAWGS source was changed.

## Why no separate reachability replacement was added

TODO #12 is complete through the direct improvement above. There is **no new query form or separately enabled reachability shortcut**. The ordinary path translator preserves the requested answer while avoiding the proven input multiplication. The before/after comparison uses saved builds; the correctness fix cannot be switched off with the optimizer setting. Both optimizer modes are tested against the written expectations.

A dedicated calculation remains a possible later proposal. The simple one-root query already completed in a few milliseconds, and the more expensive membership query now benefits from the direct correction. These measurements do not justify another implementation with different loop or path behavior.

A reasonable first candidate for a later design would be a single starting object, specified link types and direction, zero-or-more links up to an explicit maximum, and `RETURN DISTINCT` of the endpoint ID. No route, route length, ordering, input-dependent path filter, optional match, or aggregation other than distinct endpoints would be accepted initially. Recognizing that would require the parsed query operations, not a text search.

Even this narrow candidate needs work beyond reusing the current advanced calculation:

| Question | Required behavior |
| --- | --- |
| Which links and direction? | Follow only the query's types and direction; a reverse edge is not automatically permitted. |
| Starting objects and repeated rows? | Preserve the original input-to-result association. Deduplication must occur only where the query requests it. |
| Path bounds? | Honor both bounds. Finding an object after one step does not settle whether a three-step route also exists. The existing advanced reach calculation has no maximum-depth option. |
| Loops? | Ordinary Cypher permits suitable paths through cyclic graphs. The advanced ESC1 membership and certificate instructions deliberately reject relevant loops. That rejection cannot be silently imposed on a generic query. |
| Filters? | Filtering endpoints differs from excluding intermediate objects. Filters applied before or after choosing a path can change the answer. |
| Empty answers? | A required match yields no row; an optional match can retain its starting row. Preserve the query's choice. |
| Routes, lengths and order? | Continue ordinary execution unless those requested values have an explicitly equivalent implementation. A set of objects is insufficient. |

The already documented removal of the ordinary PostgreSQL default depth limit remains an unresolved review decision. This task neither restores nor removes a limit. Cross-run reuse, automatic group choices and automatic input splitting also remain separate proposals.

## Could standard Cypher ANY replace the instruction?

Yes, it is a promising language-level route for the central traversal operation. Modern Neo4j's `ANY` path selector chooses one shortest matching path for each pair of endpoints; returning the endpoints can therefore express reachability. It is different from the lowercase `any(...)` list predicate already understood by DAWGS. See [Neo4j's ANY and partition semantics](https://neo4j.com/docs/cypher-manual/current/patterns/shortest-paths/#any).

The surrounding query would still collect IDs and counts, preserve empty answers, decide whether to include the start, calculate answers for discovered starting objects where `expand_results` requests that, and explicitly reject loops where the extension requires it. Efficiently sharing overlapping searches is an implementation choice, not a guarantee from accepting the syntax.

Full Neo4j parity would also require returning actual paths and matching filter, bound and repeated-input behavior. The current DAWGS grammar supports the older shortest-path forms and the list predicate, but not the new `ANY` selector. The test backend is Neo4j 4.4.42, so testing a new selector against Neo4j requires a newer backend or an explicitly verified translation. Adding the selector is a proposal, not implemented support or a new requirement for authors.

## Checks and retained evidence

For #11, the native-adapter tests cover unrelated missing, null, empty, whitespace and duplicate IDs; objects with no type; valid unrelated objects; isolated migration records; mixed types; and incoming, outgoing and self connections. A rejected run must preserve all stored objects, relationships, properties and the prior success record. Separate runner tests cover a true skip and a failed identity read, and native tests check timing when file validation rejects a query.

For #13/#12, both complete DAWGS `make test_all` suites passed. After the final adjustment to keep empty-input bookkeeping private, the complete unit suite, SQL translation cases and all 33 new cases on both databases passed again; PostgreSQL also passed with optimizations disabled. Comparable unit coverage increased from 21,161/34,182 statements (61.9069%) to 21,266/34,257 (62.0778%). Both profiles omit database connection settings. The higher database-enabled profile is not used for this comparison.

The exact test commands, outcomes, source identities, individual observations, query plans and repeat instructions are retained in the [preparation evidence](evidence/preparation-followup/README.md) and [path-input evidence](evidence/path-input-followup/README.md). No application deployment, staging, or commit was performed. The original database directories and their stopped states were not changed.
