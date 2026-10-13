# Ordinary Cypher and optional advanced controls

## Executive summary

BloodHound extensions are intended to let authors add security analyses using **Cypher, the language used to describe which objects and connections to find**. Authors should be able to describe their analysis without learning BloodHound's internal code. This study asked whether a complex analysis could work entirely that way, while giving experienced authors, including SpecterOps, optional ways to improve efficiency. **The results support offering both approaches.**

The example was ESC1: finding who could abuse a certificate configuration to impersonate another account. That requires combining certificate permissions, trust relationships and group membership. A user may inherit permission through a group inside another group, with several routes leading to the same user. The analysis must also choose which groups or individual users should receive the resulting BloodHound connections. Expressing this in Cypher can require substantial repeated work or more input than the extension engine, the program that runs the analysis, permits in one query.

The three instructions give authors access to specific ways of handling that work:

- **`reachability` means “find everything connected through these kinds of links.”** For example, find everyone belonging to a group, including members of nested groups. The engine uses a dedicated calculation that reuses membership information without listing every possible route. This addresses repeated work when many routes lead to the same objects. The author specifies which links to follow and how to handle loops.
- **`group-overlap` means “combine these permission lists using BloodHound's group-selection rules.”** A user may get permission to request a type of certificate through one group and permission to use its issuing server through another. The calculation finds who qualifies through both, then decides which groups and individuals should receive connections. It handles nested groups, direct permissions and special groups such as Everyone. Authors can reuse that calculation instead of writing the decisions themselves in Cypher.
- **`batch-on` means “run this query on smaller pieces of this input, then combine the answers.”** Its purpose is to make independently divisible work fit within input limits. It can help a large calculation finish even when sending all its input at once would fail. The author is responsible for ensuring that separating the input does not change the answer.

In the advanced example, `reachability` and `group-overlap` perform part of the calculation. The ordinary example writes that work out in Cypher. Omitting `batch-on` already meant processing the whole input; this study tested the consequences of that choice.

**Automatic optimization is possible, but the engine must establish that a shortcut preserves what the query asks for.** Finding each reachable person once is different from counting every route to that person. Choosing a group instead of its members must follow the query's actual rules. Splitting a query that asks for the ten highest results into pieces can produce ten results per piece instead of ten overall. Recognizing a few familiar words in the query is insufficient.

We could teach the engine to recognize specific Cypher patterns and use the same efficient calculations automatically. That would require rules for recognizing equivalent calculations, checking their assumptions, and retaining ordinary execution when a shortcut does not apply. BloodHound's database options also behave differently: a method that helps PostgreSQL may offer little benefit on Neo4j. This work did not implement that automatic recognition or automatic splitting. Explicit instructions provide a direct way to request the defined operation today, while authors remain free to express the complete calculation themselves.

Automatic improvements should still benefit everyone. This study corrected shared PostgreSQL query handling that was repeating unnecessary work; both authoring styles benefited without another instruction. The next performance investigation should measure the checks performed before an analysis, then examine expensive ordinary queries. The [follow-up explanation](#follow-up-reduce-setup-work-and-improve-queries-automatically) and [TODOs #11 and #12](TODO.md#11-make-the-checks-before-an-analysis-cheaper) describe that work. Optional controls remain useful where authors can supply knowledge the engine does not yet infer reliably.

**The tests found that both styles produced the same answers, with different costs.** Reachability helped PostgreSQL; group selection helped both databases. Splitting input showed no consistent speed advantage on small inputs, but allowed a deliberately large input to complete where whole-input execution failed. On the million-object graph, typical complete recalculations took about 5.2 seconds with advanced instructions versus 11.7 seconds with ordinary Cypher on PostgreSQL, and 2.5 versus 3.9 seconds on Neo4j. BloodHound's built-in ESC1 calculation remained substantially faster than either extension calculation.

These results support the authoring choice, rather than establishing production capacity. Only about **0.1%** of that million-object graph formed the relevant membership and trust input, and some proposed performance targets were missed. Removing PostgreSQL's default fifteen-step search limit also needs explicit review before merging: it finds deeper relationships but may remove an intended safeguard against expensive searches. The tests do not settle that concern.

The implementation and comparisons are complete; nothing has been deployed. Future optional controls could help authors keep related input together when splitting work or limit how many queries run at once. Those remain proposals, alongside improvements to automatic execution.

For code review, the [actual Cypher file differences](#actual-cypher-file-differences) show the query changes line by line. The [detailed change breakdown](#detailed-change-breakdown) explains the BloodHound pilot and DAWGS changes, their reasons, implementation choices, checks and remaining concerns.

---

**ESC1 comparisons complete — 13 October 2026.** All eight final studies passed: 852 timed observations, with exact saved and intermediate answer checks on both databases. The separate batching-limit check also passed, and the original test databases and stopped states have been restored. At the user’s request, testing stops at **one million objects**; larger capacity studies are deferred.

Authors can now describe the complete ESC1 calculation with [ordinary Cypher](examples/esc1-cypher/README.md), without `reachability`, `batch-on`, or `group-overlap`. The [advanced example](extension/QUERY_NOTES.md) retains all three. Both use the same schema and saving protections. The [author guide](ADVANCED_CONTROLS.md) explains each choice and its responsibilities.

Optional does not mean equally fast. The complete ordinary example must be judged by its measured cost as well as whether it gives the right answer. This work does not make the proposed future instructions part of the authoring format.

The results support keeping both choices. Use ordinary Cypher when its clarity and measured cost suit the extension. For performance-sensitive ESC1 work, `reachability` helped PostgreSQL, and `group-overlap` helped both databases. Small differences from omitting `batch-on` were not consistent speed advantages; its more important role is making an independently divisible input fit within the parameter allowance. Omitting it sends the whole input and does not automatically divide it.

## What is being compared

The experiment changes one choice at a time on the same updated program:

| Name in the evidence | Reachability | Input pieces | Group selection |
| --- | --- | --- | --- |
| `advanced` | Instruction | `batch-on` | Instruction |
| `ordinary-reach` | Complete paths in Cypher | `batch-on` | Instruction |
| `no-batch` | Instruction | Whole input | Instruction |
| `ordinary-groups` | Instruction | `batch-on` | Complete Cypher |
| `ordinary-all` | Complete paths in Cypher | Whole input | Complete Cypher |

`starting` is the committed program and its advanced example. `native` invokes BloodHound's actual built-in `PostADCSESC1` calculation. Native preparation builds a shared ADCS cache used by other analyses too. Its result checking is timed separately, and it does **not** save the proposed connections. Compare its calculation separately; its total is not a replacement for a complete extension run.

The fixed starting commits are BloodHound `7d57d4d11f34038991c491983a9490a20cb7ac58` and DAWGS `5f16c574e48045472d15a447c2fa9707d394a4bd`. Original source archives, an original binary, and stopped database copies are preserved. Both measurement builds use the same measurement command and additional timing fields. [Reproduction instructions](scripts/comparison/README.md) include the exact patch applied to the separate starting checkout.

## Correctness and language changes

All five updated versions pass the shared Neo4j and PostgreSQL checks. Those checks compare exact saved connections, their properties, helper objects, and complete membership/trust answers. They cover direct and nested membership, local groups, deep paths, overlapping and nested groups, changed creation order, special groups, multiple authorities, empty answers, and unrelated objects. Failed calculations preserve connection identities, properties, helpers, and the successful-run record.

Relevant membership and certificate loops are rejected in both examples; forest-trust loops are allowed. The ordinary example makes the rejection explicit with a division-by-zero check. That is awkward for authors, but silently accepting incomplete evidence would change the answer. A clearer optional check is a separate future possibility.

The group query uses four commented steps. It relies on complete membership lists without loops: an outer group contains more members than its nested groups. It is not a general replacement for the advanced operation on arbitrary incomplete lists. The explanation received an independent review, and tests include nested groups that must both remain represented.

The exact intermediate comparison found a real mistake in an early ordinary example: `DISTINCT` combined two identical connections supported by different authorities. Saved connections still matched, but later queries would receive fewer supporting rows. The final query retains every supporting decision, and a permanent test checks that multiplicity. Measurements from the earlier `small-users/pg` study are diagnostic only and excluded from performance conclusions.

The large PostgreSQL trial also exposed an existing error when a later path ended at an object already selected earlier. The generated query unnecessarily read another copy of the entire object table. A five-object test counted one valid path five times. The correction reuses the selected object, preserves self-loops, and is tested for reading and saving a count on both databases. It benefits both authoring styles automatically. The earlier ESC1 output happened to agree because it only checked whether that count was positive.

The PostgreSQL changes cover ordinary paths after `WITH`, optional paths carrying earlier values, paths deeper than fifteen links, true/false values inside input rows, inputs whose names match returned columns, values carried into `SET`, and sorting by a returned column that reuses an object's name. Shared database tests and stored SQL examples cover these changes. Removing the default depth limit is a broader behavior decision with an unresolved safety concern; it should not be grouped with the straightforward query corrections. The [detailed change breakdown](#detailed-change-breakdown) explains that distinction. Explicit path bounds still apply. The pre-existing shortest-path limit is unchanged. Sequences that write, pass through `WITH`, and write again remain unsupported in some PostgreSQL forms.

## Measurement method and limits

Heavy experiments run one at a time on the same host. They retain the existing five-query concurrency limit, batching size of 500 rows, 15-minute query limit, two-hour whole-run limit, and 32 MiB setting for certain Go path calculations. That last setting is not a cap on database query memory; [actual server settings](evidence/cypher-comparison/database-query-settings.json) and measured process memory are recorded separately. Each study alternates version order and repeats each measured case at least three times. First calculations and full recalculations are separate. A first calculation is not a promise that operating-system or database caches were cold.

The full workload sequence includes a first calculation, complete recalculation, unchanged input that should skip, a certificate eligibility change, removal of one supporting authority, and removal of the final support. Input changes are real database updates/deletes. Every version starts from restored generated input. An additional untimed run compares complete intermediate rows, preserving repeated rows and sorting only lists whose meaning is a set.

The generator loads a single connected Active Directory/vault graph directly into the database. It includes a twenty-group chain, overlapping groups, local membership, shared certificate trust, directory users without enrollment rights, and vault accounts referring to directory users. It has one domain, one template, and 25 membership groups. Those fixed choices limit what the results establish. Vault data is present, but the timed analysis is ESC1; this is not a test of collection-file import or of running a CyberArk extension.

Report object counts separately from relationship counts. The relevant share counts the membership/trust inputs, but common identity checks may inspect unrelated objects too. A large total with a small relevant share does not establish capacity for an equally large membership calculation.

Application memory is the process's highest resident memory from `/usr/bin/time`, including setup and answer checking. Database memory is sampled about twice per second and includes its file cache. Adding those two numbers gives a conservative sum of separate observed peaks, not an exact simultaneous peak. Sampling can miss short peaks. Runner temporary-space accounting and sampled temporary files are reported separately. PostgreSQL's temporary bytes count writes, not peak space. Docker processor, network and block-I/O counters are retained where available; absent counters are not measured zeros.

Search studies separately exercise stable-ID, name-prefix and direct-membership queries while analysis runs. Writer requests wait through the real application write gate but do not alter graph input. These observations cover database searches and this extension's waiting interval; they do not cover HTTP requests, rendering, collection import, or the full native-plus-extension analysis interval. Observation counts accompany the 95th percentile.

## Completed small comparison

The final small study has 1,000 objects, 1,316 relationships, 100 relevant users, 490 membership pairs and two template/authority combinations. Every full answer has 100 ESC1 connections. All six change scenarios and the complete intermediate checks passed on both databases: **370 timed runs**, five repetitions per version and scenario. Each timing below is the middle of five full recalculations; brackets show fastest–slowest seconds.

The PostgreSQL database retained 688.2 MB of allocated space after the earlier million-object graph was deleted. This is a small live dataset in a previously larger database, not a freshly compacted database. Both versions use that same condition. Do not infer a size-scaling curve from these timings alone.

| Complete-run version | PostgreSQL seconds | Neo4j seconds |
| --- | ---: | ---: |
| Committed starting example | 1.755 [1.541–1.816] | 0.891 [0.720–0.973] |
| Updated advanced example | 0.692 [0.684–0.711] | 0.908 [0.777–1.032] |
| Only reachability replaced with Cypher | 1.814 [1.298–1.862] | 0.882 [0.821–1.046] |
| Only `batch-on` omitted | 0.712 [0.704–0.762] | 0.943 [0.816–2.470] |
| Only group selection replaced with Cypher | 0.778 [0.730–0.874] | 2.411 [2.165–4.108] |
| Complete ordinary example | 1.876 [1.771–1.879] | 2.202 [1.882–4.424] |

The direct feature comparisons use the **updated advanced example** as their reference. Positive means the Cypher counterpart took longer:

| Feature and counterpart | PostgreSQL change | Neo4j change | Interpretation |
| --- | ---: | ---: | --- |
| `reachability` → complete Cypher paths | +1.122 s / +162.1% | -0.026 s / -2.9% | The instruction saves substantial time on PostgreSQL. The small Neo4j difference is within overlapping ranges. |
| `batch-on` → the same query with whole input | +0.020 s / +2.9% | +0.035 s / +3.8% | Both inputs fit one piece. The small differences do not demonstrate a benefit from splitting large inputs. |
| `group-overlap` → complete Cypher decisions | +0.086 s / +12.4% | +1.503 s / +165.5% | The instruction helps on both databases, with a larger difference on Neo4j. |

The updated advanced example is 60.6% faster than the committed example on PostgreSQL and 2.0% slower on Neo4j. The PostgreSQL improvement includes the general path-counting correction; it is not a benefit created by choosing an instruction. The complete ordinary example is 6.9% slower than the committed example on PostgreSQL and 147.2% slower on Neo4j. Against the updated advanced example, it is 171.1% and 142.4% slower respectively.

Neo4j has wide ranges in several ordinary-query repetitions. Small percentage differences with overlapping ranges should not be treated as reliable wins. The group-selection difference remains substantial across the observed ranges. The three feature differences must not be added together to predict the complete result.

Individual first/full/unchanged/changed/support-removal timings, calculation and saving times, memory, temporary storage, processor use and exact answer fingerprints are in [the synthetic evidence](evidence/cypher-comparison/README.md). The studies below cover actual input splitting, simultaneous searches and one million objects.

## Splitting the input into real pieces

This study uses 2,000 objects, 20 relevant users and 51 authorities. The batch size is explicitly set to 50 for every version, so the advanced example uses two pieces. Omitting `batch-on` sends all 51 rows together. Five alternating repetitions and complete intermediate checks passed on both databases, including repeated support for the same saved connection. These inputs fit either way; the separate wide-input test examines the actual parameter-size limit.

| Complete-run version | PostgreSQL seconds | Neo4j seconds |
| --- | ---: | ---: |
| starting | 11.027 [10.759–11.382] | 0.928 [0.885–0.944] |
| advanced | 1.138 [1.090–1.738] | 0.900 [0.883–0.930] |
| ordinary-reach | 1.320 [1.227–1.492] | 0.846 [0.841–0.889] |
| no-batch | 1.252 [1.136–1.649] | 0.891 [0.718–0.905] |
| ordinary-groups | 1.420 [1.279–1.883] | 2.259 [2.157–2.453] |
| ordinary-all | 1.635 [1.493–2.092] | 2.233 [2.149–2.350] |

| Cypher counterpart versus the advanced example | PostgreSQL change | Neo4j change |
| --- | ---: | ---: |
| Complete Cypher reachability | +0.182 s / +16.0% | -0.054 s / -6.0% |
| Whole input without `batch-on` | +0.114 s / +10.0% | -0.009 s / -1.0% |
| Complete Cypher group decisions | +0.282 s / +24.7% | +1.359 s / +151.0% |

The PostgreSQL ranges overlap and early repetitions were slower. The small batching difference is not a reliable general speed advantage. Neo4j’s group-selection instruction has a much larger measured benefit. The dramatic PostgreSQL change from the committed example again includes the automatic path-counting correction, shared by every updated version.

## When batching makes the input fit

A separate rule tests the input allowance directly. It selects exactly 1,000 users and attaches 64 KiB of text to each input row; the consuming query checks the complete text. The text alone totals 62.5 MiB. Both versions have the same 32 MiB parameter allowance. `batch-on` sends two pieces of 500 rows, about 31.25 MiB of text each; omitting it sends the complete input.

| Database | Batched complete-run seconds, middle [fastest–slowest] | Whole input | Previous saved results after rejection |
| --- | ---: | --- | --- |
| PostgreSQL | 7.862 [7.761–8.408], 3/3 completed | 3/3 rejected at the parameter limit | All 1,000 connections, properties and successful-run record preserved |
| Neo4j | 3.899 [2.936–4.011], 3/3 completed | 3/3 rejected at the parameter limit | All 1,000 connections, properties and successful-run record preserved |

This establishes a capacity difference for this deliberately wide input, not a speed ratio: rejected runs have no completion time. The [separate JSON](evidence/cypher-comparison/batch-capacity.json) and [CSV](evidence/cypher-comparison/batch-capacity.csv) distinguish completed runs from failed attempts. Across these attempts, application peaks were 373.8 MiB on PostgreSQL and 219.2 MiB on Neo4j; sampled database peaks were 683.6 MiB and 5,888.2 MiB. Runner-counted temporary space peaked at 63.1 MiB. The inactive database was stopped for this separate check.

Use `batch-on` when the input would otherwise exceed the allowance and each piece can be calculated independently. It does not make an unsafe split correct: totals, ranking and group decisions may require related rows together. This is a separate rule demonstrating the parameter limit, not an ESC1 capacity claim. Setup interruptions and their corrections are documented below.

## Searches and waiting updates at 100,000 objects

The final search study uses 100,000 objects, 100,338 relationships, 300 relevant users, 960 membership pairs and two template/authority combinations. Its 21 saved connections point from groups. Relevant membership/trust inputs account for 0.332% of the objects. Five alternating repetitions of first and full calculations, plus complete intermediate checks, passed on both databases. PostgreSQL's row-count estimates were refreshed before all compared versions. These runs include simultaneous searches, so they are separate from the timing-only studies.

| Complete-run version | PostgreSQL full seconds | Neo4j full seconds |
| --- | ---: | ---: |
| starting | 3.098 [3.020–3.205] | 1.230 [1.209–1.272] |
| advanced | 1.709 [1.639–1.795] | 1.225 [1.152–1.327] |
| ordinary-reach | 2.879 [2.702–3.034] | 1.197 [1.153–1.282] |
| no-batch | 1.721 [1.643–1.809] | 1.224 [1.220–1.286] |
| ordinary-groups | 2.057 [1.651–2.313] | 2.721 [2.572–2.776] |
| ordinary-all | 3.066 [2.614–3.385] | 2.732 [2.606–3.927] |

| Cypher counterpart versus the advanced example | PostgreSQL change | Neo4j change |
| --- | ---: | ---: |
| Complete Cypher reachability | +1.170 s / +68.4% | -0.028 s / -2.3% |
| Whole input without `batch-on` | +0.011 s / +0.7% | under 0.001 s faster / under 0.1% faster |
| Complete Cypher group decisions | +0.347 s / +20.3% | +1.497 s / +122.2% |

The ordinary complete example is close to the committed example on PostgreSQL (-0.032 s / -1.0%), but slower than the updated advanced example (+1.356 s / +79.3%). On Neo4j it is +1.502 s / +122.1% slower than the committed example. The group-selection comparison explains most of that difference.

The following observations pool full recalculations across five repetitions. A 95th percentile means 95 out of 100 requests finished within that time. All rows have at least 100 search and writer observations, with **zero errors**. The detailed evidence includes counts for every version.

| Database and version | Search count | Search 95th percentile | Writer count | Update wait 95th percentile |
| --- | ---: | ---: | ---: | ---: |
| PostgreSQL starting | 1,390 | 17.391 ms | 1,000 | 3.012 s |
| PostgreSQL advanced | 742 | 18.204 ms | 810 | 1.637 s |
| PostgreSQL ordinary-all | 1,405 | 17.177 ms | 1,000 | 3.065 s |
| Neo4j starting | 584 | 11.675 ms | 595 | 1.190 s |
| Neo4j advanced | 591 | 11.422 ms | 585 | 1.170 s |
| Neo4j ordinary-all | 1,343 | 10.814 ms | 1,000 | 3.406 s |

The advanced example stays within the proposed 10% search and waiting-time targets in these full recalculations. Ordinary Cypher also stays within the search target, but Neo4j update waiting is 186.2% higher than the committed example. A slower analysis holds the write gate longer. These waiting requests measure that delay without changing the input; they do not establish a real ingestion throughput. The ordinary example's slightly lower search percentile does not establish that it helps searches: each version runs for a different duration and supplies a different mix of observed scheduling conditions.

Separate 150-search observations without analysis had 95th percentiles of 15.576 ms on PostgreSQL and 4.480 ms on Neo4j. Both authoring styles can therefore affect searches compared with idle conditions. The main review comparison remains against the committed program running the same analysis.

## One million objects

PostgreSQL: 1,000,000 objects; 1,003,106 relationships; 2,605 membership pairs; 2 template/authority combinations; 0.1032% relevant input share.
Neo4j: 1,000,000 objects; 1,003,106 relationships; 2,605 membership pairs; 2 template/authority combinations; 0.1032% relevant input share.

Three alternating repetitions covered all six workloads on each database. Each full answer has 1,000 ESC1 connections. Within each database, complete saved answers and supporting rows match across the authoring styles and the starting program. Saved answers also match between databases when compared using stable object identifiers. The built-in calculation also agrees on the connections.

| Complete-run version | PostgreSQL full seconds | Neo4j full seconds |
| --- | ---: | ---: |
| starting | 42.419 [42.391–78.249] | 3.071 [2.656–3.742] |
| advanced | 5.157 [4.934–5.170] | 2.518 [2.355–2.983] |
| ordinary-reach | 7.734 [7.709–8.101] | 2.369 [2.324–2.480] |
| no-batch | 5.064 [5.059–5.124] | 2.425 [2.408–2.694] |
| ordinary-groups | 8.116 [7.120–8.179] | 3.793 [3.766–4.281] |
| ordinary-all | 11.722 [10.763–12.128] | 3.895 [3.850–4.371] |

| Cypher counterpart versus the advanced example | PostgreSQL change | Neo4j change |
| --- | ---: | ---: |
| Complete Cypher reachability | +2.577 s / +50.0% | -0.149 s / -5.9% |
| Whole input without `batch-on` | -0.093 s / -1.8% | -0.093 s / -3.7% |
| Complete Cypher group decisions | +2.959 s / +57.4% | +1.275 s / +50.6% |

On PostgreSQL, the complete ordinary example is 6.565 seconds / 127.3% slower than the updated advanced example, but 30.697 seconds / 72.4% faster than the committed example. The automatic query correction contributes to both updated versions. On Neo4j, ordinary Cypher is 1.377 seconds / 54.7% slower than the updated advanced example and 0.824 seconds / 26.8% slower than the committed example. Small reachability and whole-input differences on Neo4j have overlapping ranges; they are not reliable speed advantages.

The largest completed dataset is one million objects. The user chose to stop here after these matched repetitions and the separate batching-limit check. This is not a discovered capacity ceiling. The relevant input is much smaller than the whole graph, and this does not establish capacity for a million relevant group members, many domains/templates, or larger graphs.

## Memory, storage and the built-in calculation

These are the largest observed values across the three million-object full recalculations. Database memory includes cache; the application value includes the measurement command and its answer checks. The counted temporary space belongs to the runner, not the database.

| Database and version | App peak MiB | Database sampled peak MiB | Sum of observed peaks MiB | Runner temporary peak MiB |
| --- | ---: | ---: | ---: | ---: |
| PostgreSQL starting | 46.223 | 3421.781 | 3468.004 | 1.007 |
| PostgreSQL advanced | 46.160 | 2364.949 | 2411.109 | 1.007 |
| PostgreSQL ordinary-all | 49.965 | 2449.508 | 2498.770 | 1.007 |
| Neo4j starting | 42.176 | 5374.383 | 5415.984 | 1.006 |
| Neo4j advanced | 42.473 | 5376.695 | 5419.035 | 1.006 |
| Neo4j ordinary-all | 45.859 | 5384.676 | 5430.367 | 1.006 |

Database allocation after loading was 714.4 MB on PostgreSQL and 1,945.3 MB for Neo4j data and logs. These include retained allocation from prior studies, so they are not estimates of minimum disk requirements.

The detailed evidence gives every version’s preparation, calculation, saving, processor time, database reads/writes, data transfer and temporary-space observations. Missing counters are left unmeasured. Those tables also cover first calculations, unchanged inputs, eligibility changes and removal of one or the final supporting authority.

The built-in reference performs the actual ESC1 calculation. Its shared preparation supports other ADCS analyses as well; its proposed connections are checked but not saved. The following middle calculation times therefore compare calculation only, not complete application runs.

| Objects / database | Built-in preparation s | Built-in ESC1 calculation s | Advanced calculation s | Ordinary calculation s |
| --- | ---: | ---: | ---: | ---: |
| 1,000 / PostgreSQL | 0.1320 | 0.0017 | 0.2267 | 1.4120 |
| 1,000 / Neo4j | 0.2154 | 0.0029 | 0.5553 | 1.8465 |
| 2,000 / PostgreSQL | 0.4010 | 0.0184 | 0.6521 | 1.1810 |
| 2,000 / Neo4j | 0.4231 | 0.0169 | 0.5691 | 1.8996 |
| 100,000 / PostgreSQL | 0.1456 | 0.0051 | 0.2648 | 1.7164 |
| 100,000 / Neo4j | 0.1745 | 0.0054 | 0.5815 | 2.1150 |
| 1,000,000 / PostgreSQL | 0.1055 | 0.0118 | 0.2990 | 5.7698 |
| 1,000,000 / Neo4j | 0.0853 | 0.0075 | 0.7304 | 2.1521 |

The built-in ESC1 calculation is substantially faster in these generated cases. Extension flexibility has a measurable cost even with the advanced instructions. On the million-object PostgreSQL graph, preparation accounts for about 4.8 seconds of the advanced example’s 5.2-second full run, while its calculation takes about 0.3 seconds. Making that preparation cheaper is a possible automatic improvement; authors should not have to bypass correctness checks to obtain it.

## Interrupted attempts and known limits

Earlier diagnostic trials used the code before the final path-counting correction. Their times are not mixed into the final comparison. A PostgreSQL attempt with 501 authorities took 695.815 seconds for its first committed-program run; 662.626 seconds belonged to shared certificate trust, before the final batched query. Repeating the matrix was projected to exceed eight hours, so the next repetition was cancelled. Its previous saved results remained intact. This was a budget decision, not a batching capacity failure or a timeout.

An early million-object trial was also stopped so the path-counting error could be investigated. The [query plans](evidence/cypher-comparison/query-plans/README.md) explain the automatic correction. Their cost numbers are database estimates, not measured elapsed times. The five-object test provides the direct proof of the incorrect count.

During setup, PostgreSQL ignored a limit placed before a delete. That cleanup was cancelled before the final timed studies began. The measurement command now reads a bounded list of object numbers and deletes only those objects. The general query-language issue remains [TODO #10](TODO.md); fixing it is separate work.

The Neo4j search study was interrupted when the resource sampler encountered a temporary directory that normal cleanup had just removed. The sampler now tolerates that disappearance. The interrupted attempt has no completion time and is excluded; 34 finished observations were retained, and only unfinished cases were resumed with unchanged program and query hashes. The continuation is recorded in the published study settings.

The separate wide-input batching check also encountered a setup problem: its first query matched all directory users before selecting 1,000 wide rows. The host reached the swap stop rule before any matched observation. The setup now restricts the intended 1,000 identifiers before returning the large text. During recovery, the monitor mistook small reads of previously swapped pages for renewed pressure even with over 29 GB available. It now stops on continuing **writes** to swap while retaining the same 20% memory reserve and all other limits. Test servers were restarted and the inactive backend stopped. Completed observations were retained; none of the 852 ESC1 measurements was rerun or changed. The [stopped setup record](evidence/cypher-comparison/batch-capacity-setup-stopped.json) and capacity continuation record make these conditions explicit.

## Candidates for future instructions

These recommendations concern later work. None is implemented by this work. Each must preserve answers, resource limits, and all-or-nothing saving. Ordinary authors must retain a working default when a preference is omitted or unavailable.

| Priority and recommendation | Intended use and evidence | Risks and safe behavior | Work on both databases |
| --- | --- | --- | --- |
| 1. [Make the checks before an analysis cheaper — TODO #11](TODO.md#11-make-the-checks-before-an-analysis-cheaper) | At one million objects, PostgreSQL preparation takes about 4.8 seconds of the advanced example's 5.2-second run. This is a larger remaining cost than its 0.3-second calculation. | Identity, schema and saving checks must still cover the same input. Any reuse must be invalidated by relevant changes; a stale checked answer must not permit unsafe saving. There is no author option to bypass the checks. | Medium to high: locate repeated work, retain the consistent input view, and repeat exact-answer and failure-preservation checks on both databases. No additional speedup is claimed. |
| 2. [Investigate automatic reachability improvements — TODO #12](TODO.md#12-improve-ordinary-queries-without-extra-instructions) | Avoid repeating path work when the query asks only for distinct reachable objects. PostgreSQL's direct reachability comparison adds 2.577 seconds at one million objects, identifying a cost to investigate. No new speedup has been measured. | A list of routes, path lengths, ordering, and duplicate rows can matter. Only substitute a shared calculation after proving the query asks for the same complete set. Otherwise execute the written query. | High: recognize a narrow safe query form, test paths and loops thoroughly, and retain ordinary execution. Neo4j may already plan the written form efficiently. |
| 3. Investigate splitting by a meaningful key | Keep every row for one domain or certificate decision together. This could make larger inputs easier to divide than a row-count-only `batch-on`. The 51-authority study confirms that pieces can retain exact supporting rows, and the wide-input check demonstrates the parameter-limit benefit of splitting. A benefit from grouping by key still needs its own experiment. | A key alone does not prove independence: totals or rankings can cross keys. Reject invalid keys and preserve complete groups. The author would still have to promise that groups are independent unless the engine can prove it. Omitted means whole input. | Medium: common grouping and temporary-file code, limit checks and identical answer tests on both databases; no database-specific query syntax should be necessary. |
| 4. Investigate an optional limit on simultaneous work | Let a memory-heavy query request fewer companion queries while retaining the existing overall ceiling of five. This needs a separate controlled concurrency experiment. | Can lengthen analysis and writer waiting. Treat it as a preference the engine may further restrict, never permission to exceed shared limits. Omitted keeps current scheduling. | Medium: shared scheduler accounting and cancellation tests; database-specific measurements remain necessary. |
| 5. Defer choosing whether a shared answer is kept or recomputed | Trade temporary storage for repeating a cheap query. Today's engine already calculates a shared prerequisite once, so a new setting must demonstrate an advantage beyond that reuse. | Recalculation costs time and must use the same input view. Do not repeat writes or nondeterministic work. The engine may decline the preference and retain current reuse. | High: lifetimes, repeated reads, limits, failure handling and meaningful tests on both databases. No benefit has yet been established. |
| 6. Defer choosing memory or temporary disk | Help an author whose large intermediate answer is cheap to read again, or whose small answer is repeatedly reused. Resource evidence must identify a real storage bottleneck first. | Memory preferences can exhaust the machine. Keep the same global budget, spill or reject safely, and allow the engine to decline. Omitted keeps bounded current storage. | Medium to high: shared storage code plus database/application memory accounting. Database temporary files are a separate concern. |
| 7. Defer a requested starting input | Ask a query to begin with a small eligible set. A small trial of two membership path orientations showed no clear benefit; query spelling is not evidence of the actual database plan. | Forcing a poor starting point can be slower. Prefer better automatic planning and observed plans. An optional preference should be declined when unsupported. | High: Neo4j and PostgreSQL have different planning controls; a portable meaning needs evidence and explicit fallback. |
| 8. Defer matching a shared answer by a declared key | A preference could ask for fast lookups before matching two shared answers. The slow trust query initially suggested this, but inspection exposed an unrelated whole-graph join. Correcting that automatically required no author instruction. A remaining lookup bottleneck must be demonstrated before proposing one. | Repeated keys must retain every matching row. Never assume uniqueness or discard requested ordering. Validate fields; the engine may decline the preference and execute ordinary Cypher. | High: input accounting and different database planning/storage behavior. Prefer automatic improvements first; no benefit from a new instruction has been established. |

A separate usability improvement is a readable way to reject invalid evidence with a useful message. It could replace the example's division-by-zero guard. That would help explain failures; it is not a demonstrated performance control and should not be presented as one.

## Follow-up: reduce setup work and improve queries automatically

**Follow-up completed 13 October 2026:** [the separate report](PREPARATION_AND_REACHABILITY.md) records the measured preparation change for TODO #11 and the decision to defer an automatic reachability replacement for #12. It also records a newly demonstrated PostgreSQL input/result association error, now #13, and the proposed Cypher `ANY` direction. The earlier comparison measurements above are unchanged. The original investigation rationale is retained below; it is not a claim that the proposed automatic replacement was implemented.

### Make the checks before an analysis cheaper

The earlier phrase **“shared preparation” means the setup and checks that both authoring styles perform before calculating connections**. In the current runner this includes preparing the declared object and connection types and their database lookups, opening the database transaction, deciding which rules need to run, and checking that object IDs are present and unique. It does not mean sharing BloodHound's built-in certificate-analysis cache with extensions, or retaining arbitrary query answers between runs.

The measured PostgreSQL preparation cost was about 4.8 seconds on the million-object advanced run. That tells us where to investigate, but the report's phase timer does not separate the individual checks. The code includes a check over object IDs throughout the graph; that is a candidate to measure, not an established explanation for the entire cost.

The proposed work is:

1. **Measure the individual steps.** Add separate timings around the existing setup, rule selection and identity checks. Include database preparation outside the main runner so time is not hidden by moving work between functions.
2. **Change the step that the measurements identify.** If a database query dominates, inspect how the database executes it and look for an equivalent query or supported lookup that does less work. If setup repeats a check against the same unchanged information, investigate doing that check once. These are alternatives to choose from the evidence, not changes already proven useful.
3. **Keep the guarantees.** Checking only the objects that happen to appear in ESC1 would narrow today's whole-graph identity check. An index alone also does not establish that missing, empty or duplicate IDs have all been checked. Preserve the same coverage and the existing handling of malformed data. A failed check must still leave previous results intact.
4. **Begin with reuse within one run, while the checked facts remain unchanged.** Reusing a check after an import or another write needs a reliable way to know that the checked facts have not changed. That is a larger proposal involving change tracking, rule revisions and restarts. Keep it out of the first change; #9 concerns improving change tracking and does not itself prove that every earlier check is still valid.
5. **Measure the actual benefit.** Report both preparation and complete-run time, alongside exact answers and failure preservation on both databases. Use focused cases and the existing evidence rather than automatically repeating all 852 observations. Record a deferral if no worthwhile safe change is found.

The starting code is [runner setup and identity validation](internal/runner/runner.go) and [database-specific preparation](internal/graphdb/native.go). Existing shared prerequisite answers are already calculated once per run; adding another mechanism to do that same work once would not address this recommendation.

### Improve ordinary queries without extra instructions

This has two possible approaches. First, correct unnecessary work in the database query itself. The already completed endpoint correction is an example: it stopped reading unrelated objects while matching an object selected earlier. Every affected query benefited automatically, with no new author instruction.

Second, investigate recognizing a narrowly defined calculation and choosing a more efficient implementation. A candidate is a query asking for **each distinct object reachable through specified membership links**. It may be possible to reuse the dedicated reachability calculation because the query does not need every route. The measured reachability difference identifies an opportunity to investigate; it does not establish that an automatic replacement is already correct or faster.

For that second approach, the engine would read the query's structure and operations, rather than match a few words in its text. It would check that the query has a supported form and verify the meaning of its direction, permitted links, starting objects, bounds, filters, loops and returned values. The implementation must also preserve empty answers and repeated rows wherever they matter. A query that needs route counts or lengths cannot be replaced merely because it contains the same membership links.

If the form is not supported, the engine executes the original query. For a supported form, the proposed replacement must be checked against written expected answers and ordinary execution, including cases deliberately outside the supported form. It must preserve the existing resource and saving limits. Resolving the depth-limit concern remains necessary; this proposal does not make unrestricted searches safe by itself.

The location of the change depends on what the investigation finds. A general PostgreSQL translation correction belongs in [DAWGS](../DAWGS/cypher/models/pgsql/translate). Selecting the pilot's existing [reachability calculation](internal/runner/reach.go) would require a checked connection between query interpretation and [query execution](internal/runner/calculate.go). A short design should identify that location and the supported forms before implementation. A PostgreSQL benefit does not require replacing an already efficient Neo4j query with the same mechanism.

Automatic group selection and automatic input splitting remain separate questions. Group selection must preserve BloodHound's exact decisions, while splitting must preserve results that depend on several input rows together. This first proposal does not silently extend recognition to those operations. Its deliverable is one justified automatic improvement, or a documented decision to defer it, rather than a promise to optimize arbitrary Cypher.

## Evidence and review

Private evidence is retained under `tools/esc1-poc/work/cypher-comparison/`, including original database copies, logs, per-run JSON, resource samples, and complete sorted answer files. Only synthetic measurements and safe settings are published under `evidence/cypher-comparison/`. The [summary script](scripts/comparison/summarize.py) exports only explicitly selected completed studies.

The proposed review targets are 10% for time, 20% for memory/temporary space, and 10% for the 95th-percentile search/writer observation against the committed program. [Every workload's checks](evidence/cypher-comparison/review-targets.json) retain absolute differences and percentages. They are review prompts, not an acceptance decision:

- Both complete authoring styles stay within the application-memory, sampled database-memory, combined-memory and runner-counted temporary-space targets across the completed studies.
- Ordinary Cypher exceeds the time target in several Neo4j workloads. Its million-object full run is 0.824 seconds / 26.8% slower than the committed example; its calculation alone is 1.266 seconds / 142.9% slower. PostgreSQL's small ordinary full calculation also exceeds the phase target by 0.160 seconds / 12.8%, although its complete-run increase is 6.9%.
- The advanced example has one complete-run timing miss: the small Neo4j one-support-removal case is 0.098 seconds / 11.1% higher. The first-calculation Neo4j search observation is 2.307 ms / 25.5% higher. Its full-calculation search observation stays within the target. These costs remain visible even though other workloads improve.
- Ordinary Neo4j waiting-update observations exceed the target for both first and full calculations. There were no search or waiting-writer errors.
- Sampling temporary files produces several percentage misses even when the runner's counted peak stays within target. Short-lived files and small reference values make these percentages unstable; the evidence includes both measurements. No small absolute allowance was agreed. Unchanged-input timing differences also include small absolute changes, such as 4.9 ms on million-object PostgreSQL.

Completion of the experiment does not mean the user accepted these performance costs. The report makes the tradeoffs reviewable before a manual commit. No application deployment is part of this work.

The original PostgreSQL and Neo4j database directories have been restored and compared successfully against the saved archives. Both dedicated test servers are stopped, matching their initial state. Original archives and generated test databases are retained privately. Neo4j logs were not in the original archive; the existing logs, including experiment logs, are retained. No application or rule-service deployment, staging, or commit was performed.

## Actual Cypher file differences

This is a direct comparison of the [advanced example](extension) and the [ordinary-Cypher example](examples/esc1-cypher), using the files in this checkout on 13 October 2026. **Five files change the calculation, one changes only a comment, and seven Cypher files are identical.** The schema is also identical. Both examples remain available.

The table links to each complete file. Expand the comparisons below to see every changed line: `-` means a line from the advanced example, `+` means its ordinary-Cypher replacement, and unmarked lines provide context. The `@@` lines identify the positions in each file. Unchanged portions are omitted; none of the changes are abbreviated.

| File | Advanced version | Ordinary version | What differs |
| --- | --- | --- | --- |
| `membership_reach.cypher` | [Open](extension/membership_reach.cypher) | [Open](examples/esc1-cypher/membership_reach.cypher) | Replaces the reachability instruction and starting-object rows with paths, loop checks, and complete membership lists. |
| `forest_reach.cypher` | [Open](extension/forest_reach.cypher) | [Open](examples/esc1-cypher/forest_reach.cypher) | Replaces the reachability instruction with domain-only trust paths and distinct reachable domains. |
| `ca_root_reach.cypher` | [Open](extension/ca_root_reach.cypher) | [Open](examples/esc1-cypher/ca_root_reach.cypher) | Replaces the reachability instruction with certificate paths, loop checks, and reachable-object lists; requires at least one link. |
| `ca_auth_reach.cypher` | [Open](extension/ca_auth_reach.cypher) | [Open](examples/esc1-cypher/ca_auth_reach.cypher) | Replaces the reachability instruction with authentication-store paths and loop checks; includes the starting authority. |
| `esc1.cypher` | [Open](extension/esc1.cypher) | [Open](examples/esc1-cypher/esc1.cypher) | Replaces group selection with the four Cypher steps and omits batching. The separate batching comparison below removes only its instruction. |
| `trusted_cas.cypher` | [Open](extension/trusted_cas.cypher) | [Open](examples/esc1-cypher/trusted_cas.cypher) | Changes one explanatory comment. The executable query is identical. |

### Reachability: four files

The advanced queries return starting objects and let the reachability instruction build the complete answers. The ordinary queries perform that work themselves: follow the permitted links, collect distinct objects, and return the same columns for later queries. Membership and certificate queries also check for loops. Forest trust permits loops and restricts the route to domains.

In these queries, `*0..` includes the starting object, while `*1..` requires at least one link. `DISTINCT` removes repeated objects reached by different routes. `OPTIONAL MATCH` keeps a starting row when a route has no match. These choices replace specific settings in the reachability instruction; removing the instruction alone would not perform the missing calculation.

<details>
<summary>membership_reach.cypher: complete difference</summary>

```diff
--- extension/membership_reach.cypher
+++ examples/esc1-cypher/membership_reach.cypher
@@ -14,11 +14,25 @@
 //
 // SPDX-License-Identifier: Apache-2.0
 
-// depends-on: query:enrollment_principals.cypher, relationship:MemberOf,
-// relationship:MemberOfLocalGroup
-// reachability: {"direction":"incoming","relationships":["MemberOf","MemberOfLocalGroup"],"include_start":false,"expand_results":true,"reject_cycles":true}
+// depends-on: query:enrollment_principals.cypher,
+// relationship:MemberOf, relationship:MemberOfLocalGroup
 
-// Shared sets cover direct enrollers and all nested candidates used by ESC1.
-// This helper rejects cycles only in relevant enrollment components.
-UNWIND $enrollment_principals AS principal
-RETURN principal.id AS id, principal.graph_id AS graph_id, principal.kinds AS kinds
+// Include each direct enroller and every member beneath it. A zero-length
+// match includes the enroller itself. Keep groups as well as users.
+UNWIND $enrollment_principals AS seed
+MATCH (principal)-[:MemberOf|MemberOfLocalGroup*0..]->(root)
+WHERE id(root) = seed.graph_id
+WITH DISTINCT principal
+// Reject a loop in the relevant evidence instead of saving a partial answer.
+// Each checked object can match itself at most once after DISTINCT counting.
+// The divisor is one for valid evidence and zero for a loop; division by zero
+// stops the query and the runner keeps all previously saved results.
+OPTIONAL MATCH (principal)-[:MemberOf|MemberOfLocalGroup*1..]->(loop)
+WHERE id(loop) = id(principal)
+WITH principal, 1 / (1 - count(DISTINCT loop)) AS valid_evidence
+// Check each principal once before expanding its members. A principal with no
+// members must still have a row with an empty member list.
+OPTIONAL MATCH (member)-[:MemberOf|MemberOfLocalGroup*1..]->(principal)
+RETURN principal.objectid AS principal, id(principal) AS graph_id,
+       labels(principal) AS kinds, collect(DISTINCT member.objectid) AS reach,
+       count(DISTINCT member) / min(valid_evidence) AS reach_size
```

</details>

<details>
<summary>forest_reach.cypher: complete difference</summary>

```diff
--- extension/forest_reach.cypher
+++ examples/esc1-cypher/forest_reach.cypher
@@ -14,12 +14,16 @@
 //
 // SPDX-License-Identifier: Apache-2.0
 
-// depends-on: query:enabled_cas.cypher, node:Domain, relationship:SameForestTrust
-// reachability: {"direction":"incoming","relationships":["SameForestTrust"],"include_start":true,"expand_results":false,"node_kinds":["Domain"],"reject_cycles":false}
+// depends-on: query:enabled_cas.cypher, node:Domain,
+// relationship:SameForestTrust
 
-// The reusable primitive visits each relevant Domain once and permits reciprocal
-// forest edges. Including the root preserves the zero-length same-domain case.
+// A certificate authority's own domain counts, as do domains that can reach
+// it through forest trust. Every object along the route must be a Domain.
 UNWIND $enabled_cas AS host
-WITH DISTINCT host.ca_domain AS id, host.ca_domain_graph_id AS graph_id, host.ca_domain_kinds AS kinds
-WHERE id IS NOT NULL
-RETURN id, graph_id, kinds
+MATCH (root:Domain)
+WHERE id(root) = host.ca_domain_graph_id
+WITH DISTINCT root
+MATCH route = (member:Domain)-[:SameForestTrust*0..]->(root)
+WHERE all(domain IN nodes(route) WHERE domain:Domain)
+RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
+       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) AS reach_size
```

</details>

<details>
<summary>ca_root_reach.cypher: complete difference</summary>

```diff
--- extension/ca_root_reach.cypher
+++ examples/esc1-cypher/ca_root_reach.cypher
@@ -14,10 +14,22 @@
 //
 // SPDX-License-Identifier: Apache-2.0
 
-// depends-on: query:usable_cas.cypher, relationship:IssuedSignedBy,
-// relationship:EnterpriseCAFor, relationship:RootCAFor
-// reachability: {"direction":"outgoing","relationships":["IssuedSignedBy","EnterpriseCAFor","RootCAFor"],"include_start":false,"expand_results":false,"reject_cycles":true}
+// depends-on: query:usable_cas.cypher, relationship:IssuedSignedBy, relationship:EnterpriseCAFor, relationship:RootCAFor
 
-// All three certificate relationships are permitted at every depth.
+// Follow the permitted certificate links. Keep a row even when no
+// object is reachable; later queries decide whether a domain qualifies.
 UNWIND $usable_cas AS ca
-RETURN DISTINCT ca.ca AS id, ca.graph_id AS graph_id, ca.kinds AS kinds
+MATCH (root)
+WHERE id(root) = ca.graph_id
+WITH DISTINCT root
+OPTIONAL MATCH (root)-[:IssuedSignedBy|EnterpriseCAFor|RootCAFor*1..]->(member)
+// Reject a loop in the relevant evidence instead of saving a partial answer.
+// Each checked object can match itself at most once after DISTINCT counting.
+// The divisor is one for valid evidence and zero for a loop; division by zero
+// stops the query and the runner keeps all previously saved results.
+WITH DISTINCT root, member
+OPTIONAL MATCH (member)-[:IssuedSignedBy|EnterpriseCAFor|RootCAFor*1..]->(loop)
+WHERE id(loop) = id(member)
+WITH root, member, 1 / (1 - count(DISTINCT loop)) AS valid_evidence
+RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
+       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) / min(valid_evidence) AS reach_size
```

</details>

<details>
<summary>ca_auth_reach.cypher: complete difference</summary>

```diff
--- extension/ca_auth_reach.cypher
+++ examples/esc1-cypher/ca_auth_reach.cypher
@@ -14,11 +14,22 @@
 //
 // SPDX-License-Identifier: Apache-2.0
 
-// depends-on: query:usable_cas.cypher, relationship:TrustedForNTAuth,
-// relationship:NTAuthStoreFor
-// reachability: {"direction":"outgoing","relationships":["TrustedForNTAuth","NTAuthStoreFor"],"include_start":true,"expand_results":false,"reject_cycles":true}
+// depends-on: query:usable_cas.cypher, relationship:TrustedForNTAuth, relationship:NTAuthStoreFor
 
-// The final trust edges are checked separately in trusted_cas. Identity reach
-// allows the CA itself to supply the two-edge suffix.
+// Follow the permitted certificate links. Keep a row even when no
+// object is reachable; later queries decide whether a domain qualifies.
 UNWIND $usable_cas AS ca
-RETURN DISTINCT ca.ca AS id, ca.graph_id AS graph_id, ca.kinds AS kinds
+MATCH (root)
+WHERE id(root) = ca.graph_id
+WITH DISTINCT root
+OPTIONAL MATCH (root)-[:TrustedForNTAuth|NTAuthStoreFor*0..]->(member)
+// Reject a loop in the relevant evidence instead of saving a partial answer.
+// Each checked object can match itself at most once after DISTINCT counting.
+// The divisor is one for valid evidence and zero for a loop; division by zero
+// stops the query and the runner keeps all previously saved results.
+WITH DISTINCT root, member
+OPTIONAL MATCH (member)-[:TrustedForNTAuth|NTAuthStoreFor*1..]->(loop)
+WHERE id(loop) = id(member)
+WITH root, member, 1 / (1 - count(DISTINCT loop)) AS valid_evidence
+RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
+       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) / min(valid_evidence) AS reach_size
```

</details>

### Group selection: esc1.cypher

The advanced file returns the two permission lists and lets `group-overlap` choose the groups and users. The ordinary file keeps the same initial permission collection, then adds the four commented steps that make those decisions explicitly. Its final `RETURN` produces a selected source directly instead of handing the two lists to the engine. The extra code preserves special groups, direct permissions, nested membership, processing order and repeated support from different authorities.

This comparison shows the two complete examples, so it also shows removal of `batch-on`. To measure group selection alone, the comparison tools put `batch-on` back into the ordinary file. That keeps the input splitting the same while changing only the group calculation.

<details>
<summary>esc1.cypher: complete difference</summary>

```diff
--- extension/esc1.cypher
+++ examples/esc1-cypher/esc1.cypher
@@ -18,12 +18,9 @@
 // query:membership.cypher, node:Base, node:CertTemplate, node:EnterpriseCA,
 // relationship:PublishedTo, relationship:Enroll, relationship:GenericAll,
 // relationship:AllExtendedRights
-// group-overlap: {"memberships":"membership.cypher"}
-// batch-on: trusted_cas.cypher
 
-// Template enrollment is the left side; CA enrollment is the right side.
-// The shared group-overlap operation preserves native direct-ID order and
-// shortcut rules using the extension's membership flags and reachable sets.
+// 1. Keep the template's permissions (left) separate from the authority's
+// permissions (right). Each template/authority/domain combination is one decision.
 UNWIND $trusted_cas AS trusted
 UNWIND $eligible_templates AS eligible
 MATCH (template:CertTemplate)-[:PublishedTo]->(ca:EnterpriseCA)
@@ -34,4 +31,53 @@
 WITH trusted, ca, template, collect(DISTINCT template_principal.objectid) AS left
 OPTIONAL MATCH (ca_principal:Base)-[:Enroll]->(ca)
 WITH trusted, ca, template, left, collect(DISTINCT ca_principal.objectid) AS right
-RETURN left, right, trusted.domain AS target, 'PoC_ADCSESC1' AS kind
+// 2. A special group makes that permission side unnecessary ("excluded").
+WITH trusted, ca, template, left, right,
+     any(candidate IN $membership WHERE candidate.principal IN left AND
+         candidate.is_group AND (candidate.is_special OR candidate.has_special_member)) AS left_excluded,
+     any(candidate IN $membership WHERE candidate.principal IN right AND
+         candidate.is_group AND (candidate.is_special OR candidate.has_special_member)) AS right_excluded
+// Record the direct permissions and the authority's permitted members.
+UNWIND $membership AS candidate
+WITH trusted, ca, template, left_excluded, right_excluded,
+     collect({principal:candidate.principal, graph_id:candidate.graph_id,
+              reach:candidate.reach, reach_size:candidate.reach_size,
+              left_direct:candidate.principal IN left,
+              right_direct:candidate.principal IN right,
+              right_matches:candidate.principal IN right OR
+                any(group IN $membership WHERE group.is_group AND group.principal IN right AND candidate.principal IN group.reach)}) AS checks
+// 3. A direct template principal already accepted by the authority stays direct.
+// Otherwise examine its members. BloodHound processes direct principals in
+// object-number order: a later principal can introduce an earlier one again.
+UNWIND checks AS candidate
+WITH trusted, ca, template, left_excluded, right_excluded,
+     collect({principal:candidate.principal, reach:candidate.reach,
+              reach_size:candidate.reach_size, left_direct:candidate.left_direct,
+              right_direct:candidate.right_direct, right_matches:candidate.right_matches,
+              remaining:any(direct IN checks WHERE
+                direct.left_direct AND NOT direct.right_matches AND candidate.principal IN direct.reach AND
+                (NOT (candidate.left_direct AND NOT candidate.right_matches) OR direct.graph_id > candidate.graph_id))}) AS choices
+// 4. Keep direct matches and qualifying groups. Keep an individual member only
+// if no selected group represents it. Membership loops have already been rejected.
+// In a complete membership graph without loops, an outer group has more members
+// than each nested group. The smaller-first calculation therefore considers
+// every qualifying group; no earlier group can remove a later candidate.
+UNWIND choices AS candidate
+WITH trusted, ca, template, left_excluded, right_excluded, choices, candidate
+WHERE
+  (left_excluded AND right_excluded AND (candidate.left_direct OR candidate.right_direct)) OR
+  (left_excluded AND NOT right_excluded AND candidate.right_direct) OR
+  (right_excluded AND NOT left_excluded AND candidate.left_direct) OR
+  (NOT left_excluded AND NOT right_excluded AND (
+    (candidate.left_direct AND candidate.right_matches) OR
+    (candidate.remaining AND candidate.right_matches AND (
+      candidate.reach_size > 0 OR
+      (candidate.reach_size = 0 AND NOT any(group IN choices WHERE
+        group.remaining AND group.right_matches AND group.reach_size > 0 AND
+        candidate.principal IN group.reach))
+    ))
+  ))
+// Keep one row for each supporting template/authority/domain decision. The
+// engine combines repeated connections when saving, just as it does for the
+// advanced instruction; later queries can still see every supporting row.
+RETURN candidate.principal AS source, trusted.domain AS target, 'PoC_ADCSESC1' AS kind
```

</details>

### Batching: the isolated one-line difference

The `no-batch` measurement uses the advanced query with exactly this line removed. It keeps `group-overlap` and the entire query body:

```diff
 // group-overlap: {"memberships":"membership.cypher"}
-// batch-on: trusted_cas.cypher
```

With that line present, the engine supplies pieces of `$trusted_cas` in separate executions and combines their answers. Without it, the engine supplies the whole input at once under the same limits. There is no replacement Cypher expression because the query's calculation is unchanged; the instruction controls how its input is supplied.

The [comparison script](scripts/comparison/run.py) constructs the measured versions as follows:

| Measured version | Exact file choice relative to the advanced example |
| --- | --- |
| `ordinary-reach` | Replace only the four reachability files shown above. |
| `no-batch` | Remove only the `batch-on` line from advanced `esc1.cypher`. |
| `ordinary-groups` | Use ordinary `esc1.cypher`, with `// batch-on: trusted_cas.cypher` added so batching remains enabled. Keep advanced reachability files. |
| `ordinary-all` | Replace the four reachability files and `esc1.cypher` with their ordinary versions. |

The script keeps the advanced `trusted_cas.cypher` comment in these measured copies. Its executable text is identical to the ordinary file, so the comment difference below does not change the comparison.

### Comment-only change and unchanged files

`trusted_cas.cypher` changes its explanation of where the reachability lists come from. Its calculation stays identical:

<details>
<summary>trusted_cas.cypher: complete difference</summary>

```diff
--- extension/trusted_cas.cypher
+++ examples/esc1-cypher/trusted_cas.cypher
@@ -18,7 +18,7 @@
 // node:EnterpriseCA, node:Domain, relationship:TrustedForNTAuth, relationship:NTAuthStoreFor
 
 // Native certificate reach permits all three root-chain kinds at every depth.
-// The shared primitive returns sets, so repeated routes cannot multiply rows.
+// Reach lists contain distinct objects, so repeated routes cannot multiply rows.
 UNWIND $ca_root_reach AS chain
 MATCH (domain:Domain)
 WHERE domain.objectid IN chain.reach
```

</details>

The seven byte-for-byte identical Cypher files are `domain_helpers.cypher`, `eligible_templates.cypher`, `enabled_cas.cypher`, `enrollment_principals.cypher`, `membership.cypher`, `special_groups.cypher`, `usable_cas.cypher`. `schema.json` is also byte-for-byte identical.

## Detailed change breakdown

### What this breakdown covers

This section describes the changes made for the ordinary-Cypher comparison, relative to the saved starting commits: BloodHound `7d57d4d11f34038991c491983a9490a20cb7ac58` and DAWGS `5f16c574e48045472d15a447c2fa9707d394a4bd`. It explains the reasoning behind the implementation; it does not imply that BloodHound's maintainers have approved every decision.

The changes in the BloodHound repository are under `tools/esc1-poc`: the extension pilot, examples, comparison command, tests and documentation. BloodHound's built-in ESC1 calculation was called as a reference; its algorithm was not rewritten. DAWGS is a separate repository under `tools/DAWGS`. Its PostgreSQL changes affect shared query handling and can therefore affect queries outside this extension when that DAWGS version is used. A BloodHound commit does not include the separate DAWGS changes.

Earlier work on schema declarations, inactive drafts, upload validation, execution reports and five-query scheduling was already in the starting version. Those features are not new results of this comparison.

| Area | Purpose of the change | Where its effects apply |
| --- | --- | --- |
| Ordinary ESC1 example | Express the complete calculation without the three advanced instructions | Authors who select this example |
| Pilot measurements | Separate preparation, calculation and saving; inspect complete answers | The pilot's run reports and optional correctness observer |
| Comparison tools | Generate data and compare fixed program versions consistently | Dedicated test databases and retained evidence |
| DAWGS query corrections | Make ordinary query expressions use the intended values and preserve correct counts | PostgreSQL queries using the affected forms |
| DAWGS depth-limit decision | Allow ordinary queries with no stated maximum to follow paths beyond fifteen links | Shared PostgreSQL query behavior; requires specific safety review |
| Tests and documentation | Explain the choices and check both databases against the same expected answers | Developer verification and review |

### BloodHound pilot: complete ordinary-Cypher examples

#### 1. Replace four reachability instructions with complete queries

**Problem and decision.** The advanced files ask Go code to finish part of the calculation. Simply deleting their `reachability` comments would leave that work undone. The ordinary example therefore supplies the missing calculation in Cypher while returning the same information to later queries.

**How it changed.** Four new example files describe the permitted paths directly:

- [membership_reach.cypher](examples/esc1-cypher/membership_reach.cypher) follows `MemberOf` and `MemberOfLocalGroup`, retaining relevant users, groups and other principals, then builds each principal's complete membership list.
- [forest_reach.cypher](examples/esc1-cypher/forest_reach.cypher) follows `SameForestTrust`, includes the starting domain, and checks that every object along a route is a domain.
- [ca_root_reach.cypher](examples/esc1-cypher/ca_root_reach.cypher) follows the permitted links from an authority toward root authorities and domains.
- [ca_auth_reach.cypher](examples/esc1-cypher/ca_auth_reach.cypher) follows the permitted authentication-store links and includes the starting authority.

`*0..` permits a starting object with no link traversed; `*1..` requires at least one link. Neither specifies a maximum. `DISTINCT` combines repeated routes to the same object where the answer is a set of reachable objects. `OPTIONAL MATCH` retains a row when a starting object has no matching members, so later queries receive an empty list instead of losing that starting object entirely. Returned identifiers, labels, lists and counts keep the advanced example's column names and meaning.

**Why this approach.** Later queries can use either implementation without changing their inputs. Several small files also keep membership, forest trust and certificate trust understandable as separate calculations. Adding a new custom function that called the existing Go operation would still require an extension-specific operation, rather than providing the requested complete Cypher alternative.

**Loops and the remaining tradeoff.** The advanced example rejects relevant membership and certificate loops but permits forest-trust loops. The ordinary example reproduces that distinction. It checks whether a relevant object can reach itself, then uses `1 / (1 - count(DISTINCT loop))`: valid evidence divides by one; a detected loop divides by zero and fails. The divisor is used in the returned calculation so the check participates in producing the answer. This preserves failure behavior using the currently supported language, but gives an awkward error. A readable validation instruction is a future usability proposal. The decision to remove PostgreSQL's default depth limit, needed for these unrestricted queries to find deeper objects, has a separate unresolved safety concern described below.

#### 2. Replace group selection with four explicit Cypher steps

**Problem and decision.** ESC1 needs the same choice of group and individual connections as BloodHound's existing calculation. Finding the same users through different group connections is not an exact match. A simple intersection of two permission lists would therefore be insufficient.

**How it changed.** [esc1.cypher](examples/esc1-cypher/esc1.cypher) now performs four commented steps:

1. Collect template and certificate-authority permissions separately for each template, authority and domain combination.
2. Apply the Everyone and Authenticated Users rules, and record direct permission matches and membership-based matches.
3. Preserve direct matches and identify the remaining candidates. Explicit comparisons of database object numbers reproduce the existing calculation's processing order where that order affects the answer.
4. Keep qualifying groups and retain individual members only when a selected group does not already represent them, including the special-group exceptions.

The final `RETURN` keeps one row per supporting decision. An early version used `DISTINCT` there and lost repeated support from different authorities even though the saved connections still looked correct. Removing that `DISTINCT` preserves every supporting row; the existing saving code still combines identical final connections.

**Why this approach.** The query expresses the decisions in ordinary Cypher and keeps each authority's evidence separate. It avoids a new authoring instruction or a change to BloodHound's built-in group-choice algorithm. Its shorter group selection relies on complete membership lists without loops: an outer group contains more reachable members than its nested groups. That property lets the query describe the selected groups without reproducing every step of the Go loop. It does not establish an equivalent replacement for arbitrary incomplete lists supplied to the generic `group-overlap` instruction.

The tests cover nested and overlapping groups, changed creation order, special groups, several independent authorities and repeated supporting rows. The four-step explanation also received the requested independent review.

#### 3. Exercise whole-input execution without adding an automatic splitter

**Problem and decision.** Authors should be able to omit `batch-on` when the complete input fits. That capability already existed in the starting engine; this work needed to demonstrate it with a complete ESC1 calculation.

**How it changed.** The ordinary example omits the `batch-on` comment. The engine sends the whole prerequisite input to the query under the existing limits. For the direct batching comparison, the tools remove only that comment from the advanced example. For the group-selection-only comparison, they add batching to the ordinary group query so group selection is the only changed choice.

**Why this approach.** Automatically splitting an arbitrary query can change totals, rankings or decisions that need several rows together. No general rule proving such a split safe was added. Authors who use `batch-on` still take responsibility for making each piece independent. Tests use several piece sizes and verify that the complete supporting answers remain the same. The separate wide-input test demonstrates the consequence of this choice: two smaller pieces fit the same allowance that rejects the whole input.

The advanced example and all three instructions remain available. The ordinary [schema.json](examples/esc1-cypher/schema.json) matches the advanced schema. Five query calculations differ; [trusted_cas.cypher](examples/esc1-cypher/trusted_cas.cypher) also has an explanatory comment adjusted to describe either source of reachability lists. The other shared calculations retain their existing behavior.

### BloodHound pilot: measurements and correctness checks

#### 4. Measure preparation, calculation and saving separately

**Problem and decision.** The existing total time and individual query times could not show where an implementation spent its time. Adding query durations would also be misleading because several queries can run simultaneously.

**How it changed.** [internal/runner/runner.go](internal/runner/runner.go) adds three durations to `Report`: `PreparationElapsed`, `CalculationElapsed` and `SavingElapsed`, exported as nanoseconds in JSON. A running timer changes phase when preparation finishes and when calculation finishes. A deferred update records time spent in the current phase even if that phase fails. A skipped run records its preparation without inventing calculation or saving time. Supporting objects and early writes needed by later queries remain part of calculation time.

[internal/graphdb/native.go](internal/graphdb/native.go) extends those measurements around the Neo4j transaction that surrounds `runner.Run`. Work before entering that callback contributes to preparation; completing the surrounding save or rollback contributes to saving. Total elapsed time is also recorded on failure. The existing rules for confirmed saving and uncertain commit acknowledgements remain in place.

**Why this approach.** It measures actual elapsed phases, including work done outside the main runner, without changing which queries execute or how their answers are saved. The same timing additions were applied to the isolated starting-source build, so the reference did not receive a different stopwatch. Despite its filename, this `native.go` change concerns the pilot's execution inside a native database transaction; it does not rewrite BloodHound's built-in ESC1 algorithm.

#### 5. Observe complete answers before later queries consume them

**Problem and decision.** Equal final connection counts can hide differences in the evidence passed between queries. Even exact final connections did not reveal the early mistake that discarded repeated authority support.

**How it changed.** `runner.Options` adds an optional `ObserveAnswer` callback. [internal/runner/store.go](internal/runner/store.go) carries it with the run's temporary answer storage. [internal/runner/calculate.go](internal/runner/calculate.go) reads each completed answer file through that callback after the query and any associated Go calculation finish, before processing its supporting outputs and dependent calculations. A recording error fails the run through the existing rollback path.

The comparison command writes those rows to separate evidence files. It sorts only membership and label lists whose meaning is a set, then compares complete rows while retaining duplicates. Its timed runs leave the callback unset. The small integration checks also record prerequisite inputs and count repeated final proposals explicitly.

**Why this approach.** Reading the completed answer observes the same result whether it came from Cypher or an advanced Go instruction. Streaming the evidence from existing answer files avoids introducing a second full in-memory copy. Keeping this work outside timed runs prevents evidence recording from being mistaken for calculation cost. [runner_test.go](internal/runner/runner_test.go) checks that the observer sees the complete row counts and that an observer failure cannot publish changes.

#### 6. Add an isolated comparison command and the real built-in reference

**Problem and decision.** Manual application runs could not reliably reset the same inputs, isolate one feature, preserve every observation or separate built-in calculation time from extension saving time.

**How it changed.** The new [cmd/cypher-comparison](cmd/cypher-comparison) command contains five parts:

| File | Responsibility and reason |
| --- | --- |
| [main.go](cmd/cypher-comparison/main.go) | Runs the selected operation, applies the existing runner limits, records results, and reads saved answers even after failure. It restricts connections to the dedicated local test ports and PostgreSQL database. |
| [load.go](cmd/cypher-comparison/load.go) | Generates a repeatable connected directory/vault graph and verifies loaded object and relationship counts. Direct loading makes the calculation comparison reproducible; it does not measure collection-file import. |
| [changes.go](cmd/cypher-comparison/changes.go) | Restores generated inputs, makes the recorded eligibility/support changes, and checks complete saved properties, helpers and successful-run records. It refuses to modify a graph containing objects outside the comparison's ownership rules. |
| [native.go](cmd/cypher-comparison/native.go) | Calls the actual local-group preparation, ADCS cache construction and `PostADCSESC1` calculation from BloodHound. Proposed relationships are written to bounded temporary files; sorting, stable-identifier lookup and answer checking are timed separately. It does not save native relationships. |
| [observe.go](cmd/cypher-comparison/observe.go) | Records intermediate answers and measures database searches and waiting writers through the existing application write gate. This exercises real query and waiting behavior without changing the input during a measured run. |

The native reference uses BloodHound's existing calculation to provide an independent result and timing reference. It does not supply a precomputed answer to either extension. Writing proposed native jobs to bounded temporary files preserves the streamed nature of that calculation instead of accumulating every job in memory. Because native saving is excluded and its preparation serves other ADCS analyses too, its total is not directly comparable with a complete extension run.

[go.mod](go.mod) and [go.sum](go.sum) add indirect dependencies needed to compile the imported BloodHound analysis packages. This is build support for using the real calculation, not a set of new extension features. The existing local replacements for BloodHound and DAWGS remain the means of building these checkouts together.

#### 7. Make comparisons repeatable and preserve interrupted attempts

**How and why.** [scripts/comparison/run.py](scripts/comparison/run.py) assembles the five updated variants, runs the committed reference separately, alternates their order, restores generated inputs, samples resources and retains query/source/build fingerprints. The first/full/change/removal scenarios distinguish faster calculation from skipping work. Complete intermediate checks run separately from timed observations.

The [reference timing patch](scripts/comparison/reference-instrumentation.patch) adds the same phase measurement and observation support to the preserved starting source. Both builds use the same measurement command. The reference retains its original DAWGS query behavior, including the subsequently discovered count error. This lets the report distinguish general query improvements from the direct feature comparisons made within the updated program.

[summarize.py](scripts/comparison/summarize.py) exports explicitly selected completed studies, calculates middle and fastest/slowest times, records absolute and percentage differences, compares saved answers across databases using stable identifiers, and evaluates the proposed review targets. Missing resource counters remain missing rather than being presented as zero. Stopped attempts remain separate from completed timings. [batch_capacity.py](scripts/comparison/batch_capacity.py) performs the separate wide-input comparison under the same parameter allowance for both choices.

Three corrections belong to these measurement tools, rather than the extension calculation:

- **Bounded cleanup:** PostgreSQL dropped the limit in the tested `WITH ... LIMIT ... DELETE` form. The command now reads at most 2,000 object numbers, then deletes only that explicit list. This protects test cleanup without claiming the general DAWGS issue is fixed; that issue is TODO #10.
- **Temporary-file sampling:** normal cleanup can remove a directory while the sampler is examining it. Sampling now tolerates disappearing files and directories while retaining other errors. Completed observations were kept when the interrupted study resumed.
- **Wide-input setup and memory monitoring:** the capacity producer now filters to the intended 1,000 user identifiers before returning large text values. The monitor counts continuing writes to swap, rather than confusing recovery reads with renewed memory pressure. The available-memory reserve and other limits remain in force. The initial setup failure and subsequent continuations are preserved in the evidence.

Original database directories were saved before the experiment and restored afterward. Their contents were compared against the archives with the test servers stopped. Generated databases and original archives remain private. Neo4j logs were not part of the original archive, so the existing logs, including experiment logs, were retained.

### DAWGS: shared PostgreSQL query changes

DAWGS translates Cypher into SQL for PostgreSQL. A *query part* below means the portion before or after a `WITH`; an *alias* is a name assigned to a returned value. These explanations separate the intended query correction from any broader consequences of the implementation.

#### 8. Carry the correct earlier values into paths after WITH

**Problem.** Readable ordinary queries select an object, pass it through `WITH`, and then follow another path. PostgreSQL could refer to the new query part's result before that result existed, instead of reading the selected object from the preceding part. Repeated optional paths also need to retain earlier objects when a later path is absent.

**Change.** [translate/expansion.go](../DAWGS/cypher/models/pgsql/translate/expansion.go) adds `expansionInputFrame`. It normally chooses the preceding result. If that result is the current, unfinished query part, it steps back once more. Both the first path and later path-building code use that selected input consistently: when choosing starting objects, bringing earlier conditions and `UNWIND` values into the query, and returning earlier values with the path result.

**Why this way.** One shared selection rule avoids making each ordinary query work around DAWGS's internal names. It also avoids always stepping backward, which would lose the immediately preceding path when several paths occur in the same query part. Existing distinctions between newly introduced objects, previously selected objects and self-loops remain relevant.

**Checks and scope.** Shared cases cover long paths after `WITH`, two optional paths, empty matches and two paths in the same part. This changes PostgreSQL handling of those query forms generally, not just the example files.

#### 9. Remove the default maximum depth from ordinary paths — unresolved safety decision

**Problem that motivated the change.** A query such as `[:MemberOf*1..]` specifies a minimum length but no maximum. The PostgreSQL translation nevertheless used a default maximum of fifteen. A twenty-link membership chain therefore exposed an incomplete answer compared with the unrestricted calculation required by the ordinary example.

**Exact change.** In `expansionConstraints` in [translate/expansion.go](../DAWGS/cypher/models/pgsql/translate/expansion.go), the previous rule always included `depth < MaxDepth.GetOr(15)`. The new rule always retains the existing cycle condition, but adds `depth < MaxDepth.Value` only when the query explicitly supplies a maximum. `*1..15` therefore remains limited; `*1..` does not receive that default limit. The constant itself remains because the separate shortest-path code still uses its existing default of fifteen.

[optimize/lowering_plan.go](../DAWGS/cypher/models/pgsql/optimize/lowering_plan.go) also changes `aggregateTraversalDepthBounds`. A specialized counting shortcut previously substituted fifteen when the query omitted a maximum. It now declines that shortcut unless the query provides an explicit upper bound. Otherwise the shortcut could have reintroduced incomplete answers even after the ordinary path translation changed. Existing tests of the shortcut now specify `*1..15`, and new tests check that unrestricted counts do not select it.

**Reason for this choice at implementation time.** The intent was to give a query with no stated maximum the complete answer requested by the example. Choosing a larger fixed default would merely move the point where deeper membership could be missed. The implementation kept explicit bounds and existing cycle checks and relied on the existing execution time limits for long-running work.

**What that reasoning did not establish.** A finite search can still contain an enormous number of different routes. Cycle checks and timeouts do not establish a hard limit on database memory or work. No replacement hard database-memory limit was introduced, and the comparison's host resource monitor is an experiment safeguard, not a new production safeguard.

The subsequent history review found the default in the June 2025 initial import (`c7ef8733`). Commit `1967dc97`, associated with BED-6499, later corrected how depth bounds were enforced. These records show existing, deliberately coded limiting behavior; they do not explain why fifteen was chosen or establish that its default use was accidental. Removing it may remove an intended protection.

**Scope and review consequence.** The change affects ordinary unrestricted PostgreSQL queries that use this DAWGS code, beyond ESC1. Declining the counting shortcut can also change their performance. The twenty-link checks establish the deeper answer, and the measured graphs establish their observed costs; neither establishes safety for densely connected graphs. This remains a behavior decision requiring explicit maintainer review before merging. It should not be approved merely as a routine correction to a hidden bug.

#### 10. Preserve true/false values inside input rows

**Problem.** The group query collects rows containing flags such as whether a permission directly matches. Expressions such as `row.a OR row.b` need actual true/false values. PostgreSQL translation could leave these map fields as text or fail to recognize the needed type, particularly when neither side supplied an obvious literal.

**Change.** [translate/hinting.go](../DAWGS/cypher/models/pgsql/translate/hinting.go) treats property lookups on either side of `AND` and `OR` as boolean values. [translate/expression.go](../DAWGS/cypher/models/pgsql/translate/expression.go) reads their JSON value with `->` and converts that value to boolean, rather than extracting text with `->>`. [translate/property.go](../DAWGS/cypher/models/pgsql/translate/property.go) looks through parentheses so `(row.a)` receives the same handling as `row.a`.

**Why this way.** Converting text would also accept the string `"true"`, even though it is not the boolean `true`. Preserving the JSON value retains that distinction and the meaning of null. The correction belongs where expressions are translated, so authors do not need database-specific conversions in every query.

**Checks.** Shared cases cover parameters, collected rows, parentheses, missing and null values, rejected wrong types, and saving a boolean expression as a property.

#### 11. Separate parameter names from names created inside a query

**Problem.** `$rows` means an input parameter, while `rows` can mean a column or one current row. These can legitimately share a spelling. The translator stored them in the same name lookup, allowing one to be confused with the other.

**Change.** [translate/translator.go](../DAWGS/cypher/models/pgsql/translate/translator.go) adds a separate `parameterBindings` map. When it encounters `$rows`, it looks up or creates the parameter's internal binding in that map. It no longer registers that name as an ordinary variable alias. Repeated uses of the same parameter still reuse its binding.

**Why this way.** It preserves the language's distinction at the point where names are resolved. Requiring authors to rename every input or column would expose an internal translation limitation and would not prevent the same problem in other queries.

**Checks.** Examples distinguish `WITH [1] AS rows` from a separate two-element `$rows`, reuse the original parameter after collecting a column with the same name, and use `UNWIND $rows AS rows`. Both databases must return the same values.

#### 12. Read earlier values correctly when setting properties

**Problem.** Shared checks save calculated answers and copy existing property values. PostgreSQL could refer to the update's returned columns while constructing that same update, although those columns do not exist until the update finishes.

**Change.** [translate/update.go](../DAWGS/cypher/models/pgsql/translate/update.go) rewrites property-assignment expressions using `rewriteCurrentFrameProjectionReferences` and the expressions available before the update. This happens after the ordinary frame-name rewrite and before assembling the final assignments.

**Why this way.** `SET a.copied = old` must read the value supplied by the earlier query part. Reusing the existing expression-rewrite helper fixes that reference without inventing a different author syntax or redesigning all write processing. It supports the related shared read-and-save tests; it does not claim support for every sequence of writes separated by `WITH`.

**Checks.** Shared cases save the count from a long path, copy and update earlier node properties, and do the equivalent work for relationship properties. More complex multi-write query forms remain outside the supported subset described in this report.

#### 13. Sort by the returned value when its name replaces an earlier object

**Problem.** A query can return `source.name AS source` and then `ORDER BY source LIMIT 1`. Here `source` means the returned name. PostgreSQL could instead sort using its earlier reference to the whole object, producing a different first result from Neo4j. An earlier tutorial check had already exposed this discrepancy.

**Change.** `rewriteOrderByProjectionAlias` in [translate/projection.go](../DAWGS/cypher/models/pgsql/translate/projection.go) previously recognized only a simple identifier. It now also recognizes an identifier that earlier translation has qualified with a table name. For that two-part form, it checks the column name against the returned aliases and rewrites the sort expression to the returned alias when it matches. Other expression forms are left alone.

**Why this way.** It gives the returned name priority at the final sorting step, where that meaning is required, without broadly replacing names elsewhere in the query. The correction supports predictable ordinary query results and limits; it is not a new group-selection rule.

**Checks.** Shared cases deliberately make object order and name order differ and compare reused-name and distinct-name forms. A translation test verifies the generated `ORDER BY`. The small change in `shortest_paths.sql` is an alias-sorting expectation; the shortest-path function's depth argument remains fifteen.

#### 14. Reuse an already selected path endpoint instead of multiplying matches

**Problem.** A later fixed-length path can end at an object selected earlier. In one form, PostgreSQL added another copy of the node table without restricting that copy to the selected object. Each valid match was then repeated for unrelated objects. A five-object check counted one path five times; the large comparison showed a substantial performance cost.

For example, the affected shape is:

```cypher
MATCH (target:Domain)
WITH target
OPTIONAL MATCH (source)-[:MemberOf]->(middle)-[:MemberOf]->(target)
RETURN count(source)
```

**Change.** In [translate/traversal.go](../DAWGS/cypher/models/pgsql/translate/traversal.go), `buildTraversalPatternStep` now uses the existing selected-endpoint builder when either its previous `UseExpandInto` condition applies or the right-hand object is already selected and an earlier result is available. That builder matches the link against the object carried in the earlier row, rather than adding an unrelated node-table read.

The helper previously applied an extra different-endpoints condition to undirected matches. Because it now serves additional ordinary path cases, that condition is restricted to its existing `UseExpandInto` use. This prevents the reuse from rejecting a valid ordinary self-loop merely because its two variable names differ.

**Why this way.** The selected object already contains the identity and values needed to match the link. Reusing the established builder avoids duplicating that logic while preserving property restrictions and optional-match behavior. The self-loop adjustment is necessary to keep that reuse from introducing a different answer. No new author instruction is needed.

**Evidence and interpretation.** Shared cases cover required and optional matches, absent routes, property restrictions, reversed and undirected links, a self-loop, and saving the resulting count. A translation check runs with query optimization both enabled and disabled. The retained query plans show the unwanted extra table read disappearing. This is an automatic correctness and performance correction, contributing to both updated authoring styles. Earlier ESC1 results happened to agree because the affected query only tested whether a count was positive; that did not make the extra work or incorrect count acceptable.

### Tests, recorded SQL and review documentation

**BloodHound pilot checks.** [cypher_comparison_integration_test.go](internal/graphdb/cypher_comparison_integration_test.go) constructs the five authoring choices from the same starting files and compares them with independently written expected answers on both databases. It also checks saved helpers, repeated support, relevant input errors and preservation of previous results. [cypher_answers_integration_test.go](internal/graphdb/cypher_answers_integration_test.go) supplies the small-test recording support, including complete reachable-object information passed between calculations. These additions check the meaning of the new ordinary example, rather than relying only on agreement between two implementations that might share a mistake.

**DAWGS checks.** The new [ordinary_cypher_shapes.json](../DAWGS/integration/testdata/templates/ordinary_cypher_shapes.json) defines shared query cases for paths carried through `WITH`, boolean maps, returned-name sorting and already selected endpoints. The cases include reads and writes and run against both databases with the same expected answers. The optimizer tests retain coverage of explicitly bounded count shortcuts and add rejection of that shortcut for unrestricted queries. Recorded expected SQL under [translation_cases](../DAWGS/cypher/models/pgsql/test/translation_cases) was updated for the changed translation, including removal of ordinary default-depth clauses and corrected returned aliases. These files describe expected generated queries; they are not new database migrations.

**Existing test correction.** [retriever_integration_test.go](../DAWGS/cmd/retriever/retriever_integration_test.go) assumed that database object numbers followed creation order. Neo4j can reuse deleted numbers. The test now copies and numerically sorts its expected node and relationship numbers before applying the expected skip/limit slice, matching the ordering requested by the actual query. It still requires exact returned numbers; it does not weaken the result comparison or change production retrieval.

**Verification already completed.** The complete DAWGS suites passed on both databases, and the pilot tests passed with race detection. The graph package needed a thirty-minute test allowance after its original invocation reached the default ten-minute package timer. That changed the test runner's allowance, not the program's query or analysis limits. Comparable unit coverage increased from 61.8852% to 61.9273%. These results support the covered behaviors; they do not resolve the unrestricted-path safety concern. Adding this documentation did not trigger another test or benchmark run.

**Documentation and evidence.** The ordinary example guide explains each calculation. [ADVANCED_CONTROLS.md](ADVANCED_CONTROLS.md) explains when the instructions help and what authors must guarantee. README, developer/end-user guides, REVIEW, TODO, the implementation plan and handoff notes link the completed work and its remaining limits. DAWGS's README describes the supported query forms and verification commands. This report and the published JSON/CSV preserve measured costs, failed targets, source identities and interruption notes so reviewers can assess the actual tradeoffs. Future efficiency controls remain proposals; the general limit-before-delete problem remains separate TODO #10.
