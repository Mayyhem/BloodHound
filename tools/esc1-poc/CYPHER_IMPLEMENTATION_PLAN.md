# Executive summary

Extension authors should describe **which connections BloodHound should find**, using readable Cypher. They should not have to learn separate instructions for walking the graph, dividing work into pieces, or managing memory. The analysis engine should choose how to carry out the work efficiently and save the complete results safely.

The proposed change moves the meaning of the ESC1 rules into Cypher while keeping reusable performance and safety work inside the engine. This does **not** require turning every efficient Go calculation into a large database query. It requires giving authors a normal query whose meaning is clear, then executing that query correctly on either Neo4j or PostgreSQL.

We will change one part at a time and compare it with a frozen copy of the current implementation. The planned scale ladder is **1 million, then 10 million, toward 100 million graph objects (nodes)**, with difficult connected structures and normal BloodHound activity during analysis. Count relationships separately: 100 million relationships is not 100 million objects. Stop before the next tier if hardware cannot safely handle it or repeated results already answer the comparison clearly. Report only the scale actually tested, including how much of that graph the rules examine and how many answers they produce.

**Status: future Cypher implementation plan, under user review.** The other conversation has finished, and the separate five-query scheduling change and page alignment have been implemented and reloaded locally. The user asked to wait for the Cypher rewrite comparison before loading the large datasets. Freeze the current source, rules, and settings as that comparison begins; both versions must use the same scheduler. No million-object test has run.

## What should change

| Current instruction | What authors should write instead | What the engine should handle | Main condition for replacing it |
| --- | --- | --- | --- |
| `reachability` — find objects connected through membership or trust links | A Cypher question describing the allowed links, direction, and required endpoints | Avoid repeatedly exploring the same routes; reuse shared answers; keep memory and temporary files bounded | Return the same objects without having to enumerate every possible route |
| `batch-on` — tell the runner which input it may split | The complete query, without a manual batching instruction | Read and save answers in manageable pieces; split calculations only when doing so preserves the complete answer | Never change a result because related input rows landed in different pieces |
| `group-overlap` — combine the two permission requirements for ESC1 | Clearly separated Cypher steps explaining who qualifies and when a group represents its members | Execute common set operations efficiently and reuse membership calculations | Preserve BloodHound's actual group and user edges, including its special cases and ordering |

The author still owns the security meaning of the rule. The engine owns execution order, reusable calculations, resource limits, shared result combination, and safe saving. Declaring dependencies and outputs follows the format agreed in the other conversation; this plan does not introduce another author-facing language or schema file.

## Step 1 Preserve a trustworthy starting point

Before changing calculation behavior:

1. Wait for the latest rule-format edits to finish. Reuse the completed baseline work, identify any checks those latest edits require, and record outstanding failures. This plan does not claim that those checks have run.
2. Preserve the current source, rule files, compiled programs, container images, database versions, and settings in a separate experiment directory. Use file checksums to identify the exact versions. **Do not stage or commit anything**; the user will make the eventual commit.
3. Preserve the working dataset and its expected results. Keep original lab storage and uploaded private collections separate from generated benchmark data.
4. Match the completed baseline evidence to the preserved source. Rebuild and confirm the answers if the finished rule-format changes are not covered by that evidence. A historical report or an existing image alone does not establish which current source it contains.
5. Record the current native BloodHound reference as well, including any local changes to its analysis code. A comparison must name the actual native version tested.

The comparison will have three clearly named versions:

| Version | Purpose |
| --- | --- |
| Current pilot | Establish what today's directives, shared Go calculations, and saving process actually cost |
| Revised pilot | Measure the same rules expressed through the proposed Cypher and engine changes |
| Native BloodHound | Check the expected native ESC1 results and compare the relevant native calculation cost |

Use separate database copies. “Side by side” means the same inputs and measurements, not allowing two versions to change the same graph or compete for the same machine resources during timing.

**Completion check:** the preserved current version can be reproduced, and the test identifies the exact source, rules, and database snapshot used.

## Step 2 Write down the rules before rewriting them

Turn the existing tests and native analysis into a short, plain-English behavior checklist. For each item, include a small graph and its exact expected edges.

| Area | Behavior that must be covered |
| --- | --- |
| Certificate templates | Required properties, missing and wrongly typed values, and the schema-version/signature exception |
| Certificate authorities | Enabled hosts, domain resolution, root trust, authentication trust, and the required final authentication links |
| Permission combinations | Keep each template and its publishing certificate authority together; permissions from unrelated combinations must not be mixed |
| Membership | Direct and nested members, local groups, repeated links, several routes to one member, and nested objects without the usual labels |
| Special groups | Everyone and Authenticated Users can make one whole permission side unnecessary; if both sides qualify, preserve the original direct sets |
| Group representation | Keep the correct groups as groups; preserve direct matches, earlier selected groups, and members shared by overlapping groups |
| Ordering | Preserve meaningful processing order, including the current use of database object numbers |
| Cycles | Distinguish allowed forest-trust loops from membership and certificate cases that the current pilot rejects |

Equal-sized overlapping groups need special attention. The inspected native code sorts candidates by membership count without an explicit rule for ties. Repeated native runs may therefore expose an ambiguous choice. We must resolve and document such cases before claiming exact agreement; replacing a group edge with equivalent user coverage is still a different result.

Do not change native behavior merely to make the comparison pass. Record the pinned version's observed behavior; any proposal to make its tie handling deterministic needs a separate, explicit behavior review.

Keep currently rejected cycle cases separate from successful cases. If the revised version can safely support more cyclic graphs, treat that as a separately reviewed extension of behavior. Ordinary Cypher must not silently lose its normal cycle behavior because an ESC1-specific check was hidden inside a general engine shortcut.

**Completion check:** current pilot, native reference, and independently written expectations agree on supported cases, or each disagreement has a documented explanation and decision.

## Step 3 Replace the reachability instruction

### Put the question in Cypher

Express membership and trust questions through ordinary relationship patterns. A simple example is “find the objects that can reach this group through one or more membership links.” The actual ESC1 queries must also preserve direction, which kinds may appear along the route, whether the starting object counts, and which discovered objects become later candidates.

Start from relevant enrollment principals and usable certificate authorities. Avoid calculating membership for the entire directory when only a small part can affect ESC1. Keep the eligibility checks early, and match actual published template/authority pairs before combining their permissions.

### Make the engine avoid repeated route work

Returning distinct endpoints does not necessarily stop a database from exploring a huge number of routes first. A graph can contain millions of different routes to the same few objects.

Add an internal execution choice for queries that only need reachable endpoints. For a proven-safe query shape, the engine can remember visited objects and share completed membership answers instead of constructing every route. The existing compact Go calculations are a useful starting point; they can become an engine implementation detail rather than an instruction the author must request.

For PostgreSQL, investigate improving DAWGS's translated database work first. If a shared engine calculation is materially better, it may perform bounded database reads and carry out the generic traversal itself. For Neo4j, measure its native query execution against the shared alternative. Both must implement the same author-visible query meaning.

The shortcut must **not** be applied blindly. A query that returns paths, counts routes, inspects links along a path, or requires particular path lengths can need information that endpoint-only traversal discards. Preserve zero-link matches, lower and upper length bounds, whether a relationship may be reused, intermediate-object filters, cycles, and duplicate results. Compare optimized and ordinary unoptimized Cypher on small generated graphs, including examples that must decline the shortcut. Unsupported shapes use the normal execution path and the same limits.

Develop the new execution choices behind an internal test setting, initially limited to extension analysis. Run ordinary BloodHound query tests with the setting off and on before considering wider use. Authors should not need an extra query comment to obtain safe execution. New language support still needs normal DAWGS parsing, query-rendering, and both-database execution checks.

Avoid eagerly storing every reachable pair for every object. Even without repeated route exploration, that answer can be enormous. Share relevant calculations within the run, release them after their last use, and use bounded temporary storage where needed. Do not introduce a cache that survives runs until its update rules have been designed and tested separately.

**Completion check:** reachability comments disappear from the revised ESC1 files; ordinary queries produce the expected results; difficult repeated-route graphs do not cause avoidable route explosion.

## Step 4 Remove manual batching from the author contract

There are two different jobs here:

| Job | Safe default |
| --- | --- |
| Reading a completed query's output and saving changes in smaller pieces | The engine handles this automatically, while checking that the complete output was received |
| Splitting the input and rerunning the query separately for each piece | Do this only when the engine can establish that combining those answers gives the same complete result |

A query asking for the largest value across all inputs cannot be answered by simply joining each piece's largest value. Similar problems arise with totals, sorting, limits, exclusions, and duplicate removal.

Prefer keeping large prerequisites inside the database or in engine-managed temporary storage, and joining related rows by their identifiers. Avoid repeatedly sending an entire directory membership table as one query parameter. Avoid offset-based paging that rescans growing amounts of data or loses rows when the view changes.

Implement safe input splitting first for clearly independent groups of work, such as complete authority/domain groups. Keep every permission list needed for a single template/authority decision together. If duplicate output can cross piece boundaries, perform complete duplicate removal before saving. A boundary through one large permission group must never create a partial answer; stream that group where possible or fail clearly within its limit.

When the engine cannot establish that splitting is safe, run the complete query with streaming and resource limits. Do not guess, return a sample, or require a replacement batching comment. If that query cannot fit, explain which allowance was exceeded and preserve its previous results.

Test different piece sizes, including one row, awkward boundaries, the normal size, and a size larger than the input. Empty input, duplicate inputs, a late database error, cancellation, and a failure in the final piece must all behave correctly.

**Completion check:** the revised ESC1 files need no `batch-on` instruction, and changing internal piece size does not change the final edges or properties.

## Step 5 Express group selection clearly

This is the most demanding rewrite. A simple intersection of expanded users is insufficient: native BloodHound sometimes returns a group to represent its members, and earlier choices affect later choices.

Break the rule into understandable steps:

1. Find direct template permissions and direct authority permissions for each valid template/authority/domain combination.
2. Apply the special-group rules separately to each permission side.
3. Preserve qualifying direct matches.
4. Examine the remaining nested groups in the required order, starting with the smaller membership sets.
5. Keep selected groups, remove the members they already represent from further consideration, and retain earlier selections.
6. Return the remaining qualifying principals and their target domains using the agreed output format.

Prototype this as several short Cypher steps before attempting one large query. Shared prerequisite results are acceptable when they make the rule easier to understand. Explain each step in comments written for someone who barely knows Go.

Have an extension author or reviewer who did not write the prototype explain it back in plain English. A long, difficult `reduce` expression is not an improvement merely because it removes a comment. Use that review to choose the necessary language features before building them.

Add DAWGS language features only where the prototype needs them. Likely candidates are `CASE` for choices, list filtering, and `reduce` for walking an ordered list while carrying forward the choices already made. These are candidates, not a commitment to use them all. There is no up-front requirement for `CALL`; add read-only subqueries only if they make the rules materially clearer or avoid unnecessary work. General procedure execution is not needed for this plan.

Every added feature needs normal language behavior on both supported backends, including empty values, missing values, ordering, duplicate handling, and mixed value types. Supporting a feature must not mean recognizing only one hard-coded ESC1 expression.

Measure the ordered-group calculation early. Repeatedly copying a large list or building one giant collected value may be much worse than today's compact sets. The engine may use a more efficient generic implementation when it can prove the result is unchanged. Keep SID tests, certificate rules, and other ESC1 meaning in the query, not in a hidden database function named after ESC1.

If a complete Cypher rewrite becomes harder to read or materially slower, keep the unresolved operation internal while documenting the remaining limitation. Do not disguise the old directive behind a new custom function and call the migration complete. Removing `group-overlap` is complete only when the rule's meaning is understandable from the query and its ordinary prerequisites.

**Completion check:** the revised output matches the exact expected group/user edges, including overlapping groups and tie cases, and authors no longer need the `group-overlap` instruction.

## Step 6 Keep saving and scheduling safe

Keep the existing scope and saving behavior unchanged during the first comparison. Cypher rules remain read-only; the common engine checks and saves their results.

- Keep the same coordinated view of the graph throughout prerequisite reads, shared calculations, and saving. Other graph changes still wait under the existing application protection. Do not combine answers read from different versions of the input.
- Finish all required calculations and validate their complete answers before removing obsolete connections.
- Preserve the chosen existing-edge policy, output properties, helper identities, and data outside the declared output scope.
- Keep helper changes, relationship changes, and the successful-run record in the same all-or-nothing save.
- A timeout, missing input, invalid answer, exhausted allowance, or damaged temporary file must not turn a partial answer into deletions.
- If the database may have saved changes but its reply was lost, preserve the existing uncertain-outcome handling; do not report a confirmed rollback or blindly retry.
- Retain affected-rule selection, disabled-rule preservation, removal cleanup, and dependency ordering across extensions.

The implemented scheduling change sets both the default and the maximum to **five ready queries running at once**. When one query finishes, start another ready query immediately if a slot is free; do not wait for all five to finish. A query becomes ready only after its required earlier queries finish successfully. Keep reads one at a time after helper objects are created, so later queries can see those objects through the existing transaction. This scheduling change is separate from the future Cypher migration. Slots are reused after the finished query’s answers have been checked; those checks and final saving still have one coordinator.

Use **the same scheduler and simultaneous-query setting in both the current and revised versions** for the main Cypher comparison, including the same switch to one-at-a-time reads. Freeze those settings with the experiment. If measuring the scheduling improvement itself, compare the old two-query groups against the new five-query scheduling using otherwise identical rules and engine behavior. Report that result separately; do not attribute it to the Cypher rewrite.

### Build resource protection into the revised engine

| Scaling problem | Planned protection |
| --- | --- |
| Several individually acceptable calculations exceed memory together | Budget the combined work, bound simultaneous database reads, and slow input processing when consumers cannot keep up |
| Shared answers are reused for a different question | Match the graph view, allowed links, direction, filters, and input parameters before reusing an answer; keep reuse within one run |
| Large joins or collected lists multiply temporary results | Match relevant identifiers early, keep full permission groups together, and stream or use bounded temporary files instead of collecting everything in memory |
| Sorting and duplicate removal need too much memory or too many open files | Reuse the existing bounded sorting approach and limit how many files are merged at once |
| The disk fills or a query exceeds its allowance | Detect the limit, cancel remaining work, remove unfinished temporary answers, and retain the last saved result |
| A large final save overwhelms database logs, locks, or recovery | Measure and limit the complete save, including replacement costs; do not solve it by committing incomplete pieces and weakening failure protection |

Check these limits under failure as well as normal operation. Include database and driver memory in measurements; a limit on encoded answer bytes is not a hard limit on total memory.

## Build a large dataset that exercises the hard work

Use a repeatable generator with a recorded starting number, so another engineer can regenerate identical input. Start with the existing synthetic collection generator and graph fixtures. Produce actual supported collection data for import checks and a faster direct database loader for isolated calculation measurements; clearly distinguish those two tests.

| Dataset | Purpose |
| --- | --- |
| Small written examples and randomized small graphs | Establish exact correctness, including cases too subtle to diagnose in a huge graph |
| 1 million objects (nodes) | Calibrate storage, load time, measurements, and practical limits; verify the actual object and relationship counts after loading |
| 10 million objects (nodes) | Proceed only if the stop gates below allow it; measure shared calculations, temporary files, saving, and complete results |
| 100 million objects (nodes) | Target comparison only if hardware and the remaining question justify advancing; verify actual object and relationship counts after loading |

At each substantial tier, vary both graph shape and how much is relevant to ESC1:

| Shape or variation | What it exposes |
| --- | --- |
| Many small independent domains | Per-domain overhead and available independent work |
| A few very large shared membership structures | Repeated work and memory needed for shared answers |
| Deep nesting, diamonds, and overlapping groups | Numerous routes to the same objects and exact group selection |
| A few extremely popular groups | Uneven workloads that average-size examples miss |
| Shared authority chains and many publication combinations | Repeated trust work and excessive combinations of unrelated rows |
| Many ineligible templates and unrelated graph records | Whether early filtering prevents wasted work |
| Approximately 1%, 10%, and 50% relevant relationships | Distinguish a huge background graph from a genuinely large analysis |
| Few results versus millions of results | Separate the cost of finding answers from the unavoidable cost of saving them |
| AD and CyberArk together | Shared dependencies across schemas and unrelated extension work |

The percentages are starting test targets, not a claim about real customer environments. Include both realistic-looking distributions and clearly labelled stress cases. Do not scale only by copying tiny disconnected labs, and do not make an all-to-all graph the only large test.

Count **input objects (nodes), input relationships, reachable membership pairs, template/authority combinations, and output relationships separately**. A graph with 100 million stored links can imply many more reachable pairs. An output that inherently grows too large must hit a documented limit safely rather than consume unbounded resources.

Give selected regions independently written expected answers. For small randomized graphs, compare against a simple, deliberately slow reference calculation that does not call either implementation's helpers. At large scale, compare the complete old/new edge sets using streamed, sorted stable object identifiers; hashes are useful evidence, but retain exact difference counts and examples too.

Use the already-authorized private lab archive as an additional realistic correctness check. It does not replace the generated large dataset, and it must not be copied into a public benchmark package.

## Run a fair comparison

### Match the starting conditions

Run current and revised versions against restored copies of the same input, one at a time on the same hardware allocation. Record CPU and memory limits, disk type and space, database settings, indexes, input loading order, timeouts, and all result/resource limits. Use the same database version for the old/new comparison on each backend.

Keep internal object numbering consistent within each old/new database comparison. Native group selection can depend on that ordering. Across Neo4j and PostgreSQL, preserve relative creation order where possible and compare stable object identifiers, not database-generated IDs. Also test deliberately shuffled creation orders; explain any native ordering ambiguity instead of hiding it by comparing counts or user coverage alone.

Run Neo4j and PostgreSQL as separate experiments. Two heavy database copies running simultaneously would compete for resources and distort the result. Make isolated copies for inspection, but time them under matched, controlled conditions. Pause competing writers during isolated calculation tests; keep the normal application coordination in the full application tests.

Use the one-million-object calibration to estimate room for inputs, indexes, preserved snapshots, temporary answers, and database change logs before creating the larger tiers. Check the actual allocation available to the experiment, including database and application memory. These checks do not authorize changing unrelated host settings.

### Stop gates before the next tier

Write down the available disk space, memory allocation, and time budget before generating data. The following are proposed starting gates for review, not claims about this machine's capacity. Freeze the chosen limits before timing the revised version and apply them equally to both versions.

| Gate | Concrete decision and evidence to retain |
| --- | --- |
| Storage | Estimate the next tier's total storage from measured bytes per object and relationship, including indexes, snapshots, temporary answers, and database logs. Allow twice the measured estimate for growth uncertainty. Stop before generation if that allowance would leave less than 25% of the experiment's storage allocation free. Cancel an active run if it reaches that reserve. Record estimates, actual peaks, and free space. |
| Memory | Reserve 20% of the experiment's memory allocation. Stop before the next tier if twice the measured peak for its projected workload would exceed the remaining 80%, including the application, database, and other required processes. Stop an active run at that threshold or on sustained swapping. Record the measured peak and which process consumed the memory. Object count alone is insufficient: account for growth in reachable pairs and output size. |
| Time | Set a maximum of 24 hours per load or complete measured run, and a total experiment time budget, before testing. Use measured phase times and known graph-shape changes to estimate the next tier. Stop if a load or run reaches its limit, or the remaining budget cannot cover loading, at least three repetitions for each version, comparison, and cleanup. Record completed phase times and the uncompleted work. |
| Failure or incorrect results | Stop advancement on an unexplained result difference, a database or process crash, unsafe resource growth, or a resource-limit failure. Preserve failure details and the previous saved results. Diagnose the cause before any retry; do not quietly increase only one version's allowance. A repeatable old-version failure can answer a capacity question at the tested tier without requiring a larger run. |
| Repeated results already answer the question | After at least three matched repetitions per version and each required workload at the current tier, stop if the result differences and resource measurements settle the review decision with room beyond ordinary measurement variation. Examples are three old-version limit failures while the revised version completes correctly, or a repeatable revised-version regression that exceeds the agreed acceptance gate. If timing ranges overlap or a larger shared structure could change the conclusion, the answer is not yet obvious. Record the question answered, measurements, and why another tier would not change that decision. |

At every stop, record the last verified object and relationship counts, graph shape, relevant analysis size, completed repetitions, limiting resource or decision, and resources needed to continue if known. Use estimates only to decide whether a run is safe to attempt. **An estimate or smaller-tier result does not demonstrate 100-million-object capacity.** If stopping early provides the requested comparison answer, say so while leaving the 100-million-object goal unverified. There is no mandatory 50-million tier and no tier beyond 100 million in this plan.

### Measure different kinds of runs separately

| Run | Required observation |
| --- | --- |
| First analysis with no extension outputs | Calculation and initial saving cost |
| Complete recalculation with unchanged facts | Actual calculation cost, with the same existing-edge policy in both versions |
| Ordinary unchanged automatic run | Whether the engine correctly skips unaffected work; this is not a calculation speedup |
| One changed domain or permission | Complete affected answers, with unrelated outputs preserved |
| Remove one of several supporting reasons | Keep the edge while another valid reason exists |
| Remove the final reason or change eligibility | Remove only the newly unsupported in-scope results |
| Late failure or interrupted save | Preservation or correctly reported uncertainty, never a claimed successful partial result |
| Analysis while people search | Search success, response times, and how long other graph changes wait |

Use explicit supported graph updates for removal tests. An object merely missing from a later collection is not evidence that the collector deleted it.

For each timed case, alternate which implementation goes first. Record first-start and warmed-up runs separately, with at least three measured repetitions and additional repetitions when results vary substantially. Report the median and range; three samples do not justify a meaningful 95th-percentile run time. A restarted database may still benefit from the operating system's disk cache, so do not label that a fully cold run.

Start with identical existing limits. If today's 32 MiB prerequisite allowance or another limit prevents a run, record that failure. A second experiment may use a larger, fixed resource allowance for **both** versions, chosen and recorded before the run. Never quietly raise a limit for only one implementation. If only the revised version finishes, report a demonstrated capacity difference, not an invented speed ratio.

### Record the measurements that matter

| Measurement | Why it matters |
| --- | --- |
| Preparation, graph traversal, group selection, comparison, saving, and total time | Identify where a gain or slowdown actually occurs |
| Highest application and database memory use, plus combined container/host usage | Avoid moving a memory problem from Go into the database and calling it solved |
| CPU use, database reads, rows exchanged, and temporary disk use | Explain wasted work and likely hardware requirements |
| Membership reuse, query count, and repeated route work | Establish whether the intended engine improvements are actually used |
| Saved output count, properties, endpoint scopes, and helper state | Verify correctness beyond matching totals |
| Database change-log growth and time spent saving or rolling back | Expose the cost of one large all-or-nothing save |
| Search response times and graph-write waiting time | Measure the effect on normal BloodHound use |
| Failures and the first limiting resource | Show the actual supported capacity boundary |

Do not add application memory twice when native and extension code run in the same process. Separate calculation-only measurements from the complete upload/native-analysis/extension cycle. Compare native ESC1 time with extension ESC1 time only when both measurements cover the same work; otherwise label the broader native measurement plainly.

The primary old/new comparison uses the same default edge policy. A separate policy experiment can explain replacement costs, but must not make the revised version look faster by changing what it saves.

## Acceptance criteria

These are proposed starting gates, to be fixed before the large measured comparison. They are not existing performance guarantees. Recheck the stated current defaults against the frozen source after the other conversation finishes.

List the required workloads before timing them, and judge each one separately; a fast easy case must not hide a slower difficult case. Use calibration repetitions to record normal measurement variation and agree absolute allowances for near-zero times, disk use, or waiting time before seeing the revised results. A percentage comparison against zero is not meaningful.

| Area | Required result |
| --- | --- |
| Correctness | No unexplained extra or missing edges, property differences, unexpected scope changes, or damage to unrelated graph data |
| Failure handling | Every tested incomplete calculation preserves previous saved output; uncertain saves are identified correctly |
| Author simplicity | Shipped revised ESC1 rules need no reachability or batching comment; group-overlap is removed only when its meaning is readable in ordinary queries |
| Existing BloodHound behavior | Normal query, analysis, ingestion, and saving tests continue to pass on both backends; new engine shortcuts leave unsupported query shapes alone |
| Large graph | Advance from 1 million to 10 million objects, then toward 100 million objects only while the stop gates permit it. Report verified object and relationship counts separately, with substantial shared connected structure and a measured relevant analysis portion. At 100 million objects, target at least 10 million relevant objects and report their relationship count too. Attempt the current version under the same conditions and report failures. A smaller-tier answer may complete the comparison review; the 100-million-object capacity goal remains unverified unless that actual dataset and complete revised run finish. |
| Time | No more than a 10% increase in median calculation or complete-run time on cases both versions finish under matched settings |
| Resources | No more than a 20% increase in peak combined memory or temporary disk on matched successful cases; no crashes from uncontrolled growth |
| User impact | No new search failures; no more than a 10% increase in measured 95th-percentile search latency or graph-write waiting time under the same test load |

Collect enough search samples to support the latency measure and publish the sample count. If a threshold is exceeded, identify the cause, improve the design, or document a specific tradeoff for review. A readability benefit may justify a measured cost, but that decision should be explicit rather than described as “no performance impact.”

## Work order and review responsibilities

| Order | Deliverable | Review before proceeding |
| --- | --- | --- |
| 1 | Frozen current version, written behavior checklist, and repeatable small comparison | Exact meanings and native ambiguities understood |
| 2 | Readable query prototypes and a short list of required DAWGS features | Authors can follow the rules without learning engine internals |
| 3 | Ordinary-Cypher reachability with tested engine execution choices | Correct answers and bounded work on repeated-route cases |
| 4 | Automatic handling of large inputs and removal of `batch-on` | Changing piece sizes preserves complete answers |
| 5 | Ordered group selection and necessary language support | Exact group/user output agreement and understandable queries |
| 6 | Full regression and failure checks on both databases | Native behavior and all-or-nothing saving preserved |
| 7 | One-million-object calibration, then 10 million, toward 100 million if justified | Check every stop gate before advancing; record actual counts and any early-stop reason |
| 8 | Reviewer report, simplified author guide, and removal of superseded code | New implementation meets the agreed gates |

For engineers locating the work: query definitions live in [extension](extension/); current special-instruction handling is in [internal/extension/reach.go](internal/extension/reach.go); execution and shared calculations are in [internal/runner](internal/runner/); database-specific execution is in [internal/graphdb](internal/graphdb/). DAWGS changes belong in its existing language-reading and PostgreSQL translation code, with their corresponding shared query tests. Follow [DAWGS's repository instructions](../DAWGS/AGENTS.md) and the [pilot review guide](REVIEW.md) when selecting the full checks. Keep data-loading, comparison, and measurement tools separate from the engine so the tests can catch engine mistakes.

I will own the architecture, scope and saving rules, comparison strategy, native-result disagreements, and final review. Focused Cypher prototypes, straightforward Go work, individual tests, dataset tooling, and documentation can be delegated to **Sol 6.1 Medium**, with clear file ownership so parallel agents do not overwrite one another.

Keep the old version available in the experiment snapshots while comparing. Once the new version passes, remove unused directive parsing and duplicated implementations rather than carrying permanent compatibility paths. Do not remove code merely because it has been moved out of the author's view; useful shared engine work should remain shared and well commented.

The final review package should contain readable old/new rules, reproducible dataset instructions, frozen settings, exact result differences, phase timings, resource charts, preserved failure reports, and a concise explanation of the DAWGS changes. Put summary findings in the README and detailed evidence in the review guide. Leave all commits to the user.

## Existing work to reuse

| Source | Purpose in this plan |
| --- | --- |
| [ESC1 query notes](extension/QUERY_NOTES.md) | Current eligibility, trust, membership, ordering, and cycle boundaries |
| [Reachability tests](internal/runner/reach_test.go) | Shared descendants, cycle behavior, cancellation, and reference traversal |
| [Group selection tests](internal/runner/overlap_test.go) | Exact special-group, shortcut, overlap, and ordering examples |
| [Batch tests](internal/runner/batch_test.go) and [edge-policy tests](internal/runner/edge_policy_test.go) | Complete-answer handling and preservation of saved results |
| [Neo4j ESC1 cases](internal/graphdb/neo4j_integration_test.go) and [PostgreSQL ESC1 cases](internal/graphdb/postgres_esc1_integration_test.go) | Real database checks and comparison with native answers |
| [Native integration tests](internal/graphdb/native_integration_test.go) | Application database connections, publication, rollback, and readers during work |
| [Native group selection](../../packages/go/analysis/ad/ad.go) and [native tests](../../packages/go/analysis/ad/post_integration_test.go) | Independent native expectations and ordering questions |
| [Synthetic collection generator](scripts/make-native-cases.py) | Existing small collected-data examples; its current options select the output directory, not a million-object scale. Extend or build a separate repeatable generator before claiming large-scale loading support. |
| [DAWGS graph benchmark tools](../DAWGS/cmd/graphbench/) | Reusable loading and timing infrastructure, after checking which operations it actually implements |
| [Current review evidence](REVIEW.md) | Existing results and their limits; historical reports do not establish 100-million-object capacity |
