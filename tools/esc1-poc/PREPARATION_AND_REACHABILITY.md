# Preparation costs and ordinary reachability

13 October 2026. This is the focused follow-up for TODOs #11 and #12, separate from the completed [large Cypher comparison](CYPHER_COMPARISON.md).

**#11 is implemented:** reports separate the preparation steps, and an equivalent identity check avoids unnecessary type-name lookups. **#12's investigation is complete, with automatic replacement deferred:** inspecting an ordinary query exposed a PostgreSQL input/result association error that should be corrected first. No new reachability shortcut, author instruction, or path-limit change was introduced.

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

A simple query asking for distinct members beneath one group used indexed lookups and completed its instrumented PostgreSQL execution in 2.298 ms. It expanded 166 paths and returned 124 distinct objects. That plan does not justify introducing a separate calculation for every distinct-reach query.

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

This points to how DAWGS reconnects an `UNWIND` input with an ordinary path result in `buildExpansionPatternRoot` in `translate/expansion.go`. A correction must preserve the association and legitimate repeated input rows, including empty input and zero-length paths. Simply deleting the input join could remove requested columns or lose repeated answers. This is recorded separately as TODO #13. No DAWGS files were changed in this follow-up; the existing uncommitted DAWGS work was preserved.

## Why an automatic replacement remains deferred

There is currently **no accepted or enabled new query form**. Ordinary queries continue through their existing execution path, with its existing behavior and limits. No enabled/disabled shortcut comparison is claimed because no shortcut was implemented.

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

The new native-adapter tests cover unrelated missing, null, empty, whitespace and duplicate IDs; objects with no type; valid unrelated objects; isolated migration records; mixed types; and incoming, outgoing and self connections. A rejected run must preserve all stored objects, relationships, properties and the prior success record. Separate runner tests cover a true skip and a failed identity read, and native tests check timing when file validation rejects a query.

The exact test commands, outcomes, source identities, individual observations, query plans and repeat instructions are retained in [the evidence directory](evidence/preparation-followup/README.md). No application deployment, staging, or commit was performed. The original database directories and their stopped states were not changed.
