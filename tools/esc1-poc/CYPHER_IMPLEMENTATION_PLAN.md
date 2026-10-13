# Easier extension authoring, with optional advanced controls

We want extension authors to write understandable Cypher queries that describe the connections BloodHound should find. They should be able to build a complex analysis such as ESC1 without reading Go code or learning special performance instructions.

BloodHound's extension engine is the program that runs these queries and saves their answers.

Experienced authors, including SpecterOps, should also be able to choose advanced instructions when those instructions help. We will support both approaches, compare their results and performance, and explain when each is useful.

**Status: completed, 13 October 2026.** Both examples and the required PostgreSQL fixes are implemented. Correctness and failure-preservation checks pass on both databases, including the complete DAWGS suites and pilot tests with race detection. The [comparison report](CYPHER_COMPARISON.md) and [published evidence](evidence/cypher-comparison/README.md) contain eight final studies and 852 timed observations, plus the separate batching-limit check. The largest dataset has one million objects, the stopping point requested by the user. Original database contents and stopped states are restored. The report identifies performance costs for review; completing this plan does not mean those costs have been accepted. No deployment or commit was performed.

## 1. The choices we will give authors

The ordinary approach will describe the whole calculation in Cypher, using several short query files where that makes it easier to follow. The advanced approach may use these three instructions:

| Instruction | What it does today | What the ordinary Cypher alternative must do |
| --- | --- | --- |
| `reachability` | Starts with selected objects, then uses Go to find objects connected through membership or trust links. | Describe which connections to follow and which objects to return, without requiring this instruction. |
| `batch-on` | Splits a query's input into smaller pieces, runs the calculation for each piece, and combines the answers. | Run the complete query without asking the author how to split it. The engine may split work automatically when it can do so correctly. |
| `group-overlap` | Uses Go to combine two sets of permissions and decide which groups or users should get a connection. | Explain the same choices in Cypher, including when to keep a group instead of replacing it with its members. |

At the start of this work, the shipped ESC1 extension needed `reachability` and `group-overlap` to complete its calculation. Removing those comments would remove work that its Cypher does not perform. `batch-on` can already be omitted if the complete input fits within the limits.

We will retain all three as supported advanced options. The new ordinary example must work without any of them. Neither approach is assumed to be faster, and supporting both does not mean they will handle the same amount of data within the same limits.

Both approaches must produce the same connections and the same details saved on those connections. Giving the same users access through different group connections is not an exact match.

## 2. Prepare a reliable starting point

The committed source is already saved. The exact BloodHound and DAWGS versions are listed at the end of this plan. DAWGS is the separate code that helps BloodHound read and execute queries; both versions are needed to rebuild the current system.

Before changing calculations:

1. Build the saved version in a separate working copy and record how it was built.
2. Preserve copies of the test databases, uploaded rules, active rule versions, and expected answers. Record the program and database versions and the settings actually used.
3. Run the existing small tests with that build and confirm the expected answers. Keep the results so later changes have a trustworthy comparison.
4. Record the version used for BloodHound's built-in ESC1 calculation, which provides another check of the expected answers.

A source commit does not save database contents, private settings, built programs, or the test logs kept outside Git. It also does not prove which version the running application uses. Keep private data and credentials out of the published comparison materials.

Reuse the completed query checks, inactive drafts, execution reports, example tests, shared calculations, and five-query scheduling. These are already part of the starting version. The interactive query graph is separate work and does not need to be finished before this comparison.

## 3. Write down what the answers must mean

Before rewriting ESC1, make small examples with exact expected connections. Cover the cases that are easy to get wrong:

- Certificate eligibility, missing or incorrectly typed values, certificate trust, and which certificate authority publishes each template.
- Direct and nested membership, local groups, multiple routes to the same object, and nested objects without the usual labels.
- Everyone and Authenticated Users, overlapping groups, and the choice between returning a group or its individual members.
- Processing order, including equal-sized groups where the current built-in calculation may make an ambiguous choice.
- Loops in membership or trust links. Record which cases currently work and which are rejected.
- Empty answers, several rules supporting the same connection, changed connection details, and supporting objects created by queries.

Compare with the built-in ESC1 calculation and independently written expected answers. Record and resolve disagreements; do not change the built-in behavior merely to make a test pass.

There is also a known question about a query that sorts a returned column using the same name as an earlier object variable. Neo4j and PostgreSQL produced different ordering in one tutorial test. Investigate that case before relying on it to choose groups or take only the first few results.

## 4. Build the ordinary alternatives one at a time

Start with reachability, then input batching, then group selection. Keep a working advanced example throughout.

For each step, write a readable Cypher example first. Have someone who did not write it explain what it does. If that requires following Go code or untangling one enormous expression, improve the example before committing to the implementation.

Add only the query-language features needed to make these examples clear and workable on both databases. New features must pass the same checks during upload and execution. Keep existing requirements for schemas, declared inputs, and output columns unless a change is agreed separately.

The engine may share efficient calculations between both authoring styles. For example, an ordinary query may benefit automatically from the same membership calculation used by `reachability`. Only use such a shortcut when it preserves everything the query asks for, including routes or ordering when those matter.

Two cautions need explicit tests:

- Splitting input can change totals, rankings, limits, or decisions that need several related rows together. Today, `batch-on` relies on the author's promise that splitting is safe. Test different piece sizes and explain that responsibility in the advanced guide.
- `group-overlap` does more than find users common to two lists. Its Cypher alternative must preserve the exact choices of groups and users. Renaming the existing Go operation as a custom function would not provide the complete Cypher alternative.

Keep instruction support, tests, and documentation for advanced authors. Remove only code that is unused or duplicated. Do not mark the ordinary alternative complete just because it runs: review its readability and any measured performance cost. A slower version may be acceptable if that specific tradeoff is understood and agreed.

## 5. Preserve correctness and existing results

These rules apply throughout the work. Small tests of correct answers and failed calculations must pass on both databases before large comparisons begin.

Both authoring styles receive the same protection. All required calculations must finish and their complete answers must be checked before obsolete connections are removed. Supporting objects, connections, and the successful-run record must be saved together or discarded together.

Queries must use a consistent view of the input. Preserve existing rules for connection types, which objects they may connect, several queries producing the same connection, saved connection details, disabling rules, and removing rules. Keep unrelated data untouched and ordinary BloodHound functionality working on both databases.

Test errors, cancellation, exhausted limits, and failures late in saving. An incomplete calculation must keep the previous saved results. If a database may have saved changes but its confirmation was lost, report that uncertainty and do not blindly repeat the work. Reports must distinguish a completed calculation from a confirmed save and show when saved results belong to an older rule version.

Incomplete uploads remain inactive drafts. Neither ordinary queries nor advanced instructions may bypass validation or use a partial answer as a successful result.

## 6. Compare each instruction with its counterpart

The final report must contain a direct comparison for **each of `reachability`, `batch-on`, and `group-overlap`**, on **both Neo4j and PostgreSQL**.

Use the same updated engine for both sides of each comparison. Change one feature at a time so we can tell what caused the difference:

| Test version | Reachability | Batching | Group selection |
| --- | --- | --- | --- |
| Advanced reference | Instruction | Instruction | Instruction |
| Reachability comparison | Complete Cypher | Instruction | Instruction |
| Batching comparison | Instruction | No `batch-on` | Instruction |
| Group-selection comparison | Instruction | Instruction | Complete Cypher |
| Complete ordinary example | Complete Cypher | No `batch-on` | Complete Cypher |

For batching, the counterpart is the same complete query without the comment. Say whether the engine runs it all at once or splits it automatically. Include inputs small enough to fit both ways and larger inputs that expose the limits.

For reachability and group selection, compare the full replacement calculation with the instruction. Give both the same starting information. Check the complete answers passed to later queries as well as the final saved connections, including order when a later query depends on it.

Measure all the work each choice requires: preparing inputs, running the query, doing any Go calculation, combining answers, and saving results. Include any extra conversion needed to make an answer usable by the next query. If changing one feature also forces other work to change, explain that limitation rather than attributing every difference to one instruction.

For every feature, database, data size, and test case, report:

| Question | Required evidence |
| --- | --- |
| Did both versions answer correctly? | Exact comparison of connections, their saved details, relevant supporting objects, and answers passed between queries. Matching counts alone is insufficient. |
| How long did they take? | Calculation time and total time, including preparation and saving. Show seconds and percentage differences, clearly naming the reference. |
| What resources did they need? | Highest memory use for the application and database, combined memory, temporary disk space, processor use, and database reads and data transfer where measurable. Mark missing measurements. |
| What limited them? | Failures, timeouts, memory or storage limits, and the largest completed case. A failed run has no invented completion time or speed ratio. |
| What should an author choose? | Where the instruction helped, hurt, or made no measurable difference, and what the author must understand to use it correctly. |

Also compare the complete ordinary and advanced examples with each other and with the committed starting version. Measure the matching part of BloodHound's built-in ESC1 calculation separately. Do not compare its entire analysis time with only one extension query.

If both styles use the same efficient calculation internally, report that. Do not force ordinary Cypher to run poorly to manufacture an advantage for the instruction. Do not add the three individual time differences to predict the complete result; measure it. Add further combinations if the features affect one another.

## 7. Make the comparison fair and repeatable

Use restored copies of the same data, the same share of the machine's resources, database versions, settings, and limits. Record how objects were loaded, because their creation order can affect group choices. Compare stable object identifiers across databases, and check deliberately changed creation orders on small cases.

Run the heavy comparisons one at a time so they do not compete for the machine. Keep the existing maximum of five ready queries and the existing one-at-a-time behavior after queries create supporting objects. Do not count earlier scheduling improvements as benefits of the Cypher work.

Repeat each measured case at least three times, alternating which version runs first. Show the middle result and the fastest and slowest results; repeat more when results vary widely. Record the first run separately from later runs that may benefit from data already in memory.

Test the first calculation, full recalculation, unchanged data that should skip work, a small relevant change, and removal of one or the last reason for a connection. Record which queries actually ran. Skipping more work is different from calculating the same work faster. Use real supported changes to remove data; absence from a later collection does not itself mean deletion.

Test normal BloodHound searches and waiting graph updates during analysis as well. The comparison must show whether either style makes everyday use worse.

Existing reports provide a useful start, but they do not separately time every calculation or record all resource use. Add consistent measurements for both sides and record any changes made solely for measurement. Preserve the evidence outside the recent 20-attempt history. Query times can overlap, so adding them does not give total elapsed time.

## 8. Increase data size carefully

**Updated scope, 13 October 2026:** the user asked to finish after the queued comparisons, the million-object runs and the separate batching-limit check. **One million objects is the maximum for this work.** Tests at 10 million or toward 100 million are deferred; they would require a separate capacity investigation.

Begin with small examples whose answers we understand, then test **1 million objects**. An object is a node, such as a user, group, or computer. Count connections separately: 100 million connections is not 100 million objects.

Generate repeatable data with deep membership, overlapping groups, shared trust links, unusually large groups, unrelated records, and both small and large answer sets. Include Active Directory and CyberArk together. Do not create a large test merely by copying disconnected tiny examples.

Record how much of the graph each analysis actually examines, how many membership pairs and template/authority combinations it considers, and how many answers it produces. Vary the relevant share of the graph. If we reach 100 million objects, aim for at least 10 million relevant objects and report their connection count too. A private lab dataset can provide another correctness check, but cannot replace these scale tests or become part of a public data package.

At each attempted size, run the feature comparisons and the complete examples under the same limits. Compare complete answers, not samples. Record how to regenerate and load the data, and distinguish tests of importing collection files from tests that load a database directly.

Before advancing, use the measured storage, memory, and time to decide whether the next size is safe and useful. The proposed limits are:

| Area | When to stop |
| --- | --- |
| Storage | Allow twice the estimated space for the next size, including database copies, temporary files, and database logs. Do not start if that leaves less than 25% of the assigned storage free; stop an active run if it reaches that reserve. |
| Memory | Reserve 20% of the assigned memory. Do not advance if twice the projected highest use exceeds the remaining 80%. Stop on reaching that limit or continued reliance on disk because memory is full. Count both application and database use. |
| Time | Set a total experiment budget and a maximum of 24 hours for any load or measured run. The program's shorter query and run limits still apply. Do not advance unless the budget covers loading, repeated runs, answer checks, and cleanup. |
| Incorrect or unsafe behavior | Stop advancement on unexplained answer differences, crashes, uncontrolled resource growth, or exceeded limits. Investigate before retrying. |
| Enough evidence | After repeated matched tests answer the comparison clearly, stop if a larger size would not change the decision. Explain why. |

Record the last completed size, actual object and connection counts, failures, and the reason for stopping. Do not quietly raise a limit for one version; a separate higher-limit comparison must give both the same allowance. Report a capacity difference if only one version finishes.

A useful comparison can finish below 100 million objects. Claiming capacity at 100 million requires an actual dataset of that size and a complete successful calculation; estimates do not establish it.

## 9. Judge the benefits honestly

Correct answers, understandable ordinary queries, retained advanced options, and safe saving are required. The report must show each feature's performance and the complete examples separately. A fast advanced example cannot hide a slow ordinary example.

The following remain **proposed review targets**, to agree before the large comparison:

| Measure | Proposed target against the committed starting version |
| --- | --- |
| Calculation and complete-run time | No more than 10% slower in the middle result of repeated runs that both versions finish. |
| Memory and temporary disk space | No more than 20% higher peak use under matched conditions. |
| Everyday BloodHound use | No new search failures. No more than 10% increase in search time or graph-update waiting time at the point by which 95 out of 100 observations have finished. Collect enough observations to measure this. |

Apply the targets to both complete authoring examples and publish the results for each required workload. Agree small absolute allowances when the starting measurement is near zero, where percentages are misleading. If a target is missed, explain the cause and seek review of the specific tradeoff. Do not describe an unmeasured or accepted cost as no performance impact.

## 10. Recommend future advanced controls

The final deliverable must identify other optional instructions that could help experienced authors control efficiency. These are recommendations for later work, not features to implement as part of this plan.

Investigate possibilities such as:

| Possible instruction | What an author could ask for |
| --- | --- |
| Keep or recompute a shared answer | Keep an expensive result for later queries, or calculate it again to avoid retaining a large answer. Explain what this adds beyond today's automatic reuse. |
| Split by a meaningful key | Keep all rows for one domain or certificate decision together when dividing the work. Explain how this improves on `batch-on`. |
| Start from a chosen input | Begin with a small set of eligible objects before looking up their matches in a much larger set. |
| Choose where temporary answers live | Prefer memory or temporary disk storage within the same overall limits. |
| Limit simultaneous work | Run fewer other queries alongside a particularly expensive query to reduce competition for memory or database connections, within the existing overall limits. |

Rank the candidates using the measured sources of cost where possible. For each recommendation, explain the intended use, supporting evidence or missing experiment, possible downsides, how misuse would be caught, what happens when it is omitted or unavailable, and the work needed to build and maintain it on both databases. Say whether the engine can decline a preference or would have to trust an author's promise.

Recommend investigation, deferral, or rejection. Improving automatic behavior may be the better answer. Do not invent speedups for untested ideas. Future efficiency controls must preserve the answer, stay within resource limits, and keep the same saving protections; changing which connections exist is a different kind of feature.

## 11. Final deliverables

The work is complete when reviewers receive:

1. **Two working ESC1 examples and clear author instructions:** one using ordinary Cypher without the three instructions, and one demonstrating the advanced options.
2. **`CYPHER_COMPARISON.md`:** separate reachability, batching, and group-selection comparisons on both databases; complete ESC1 comparisons; exact answer checks; time and resource tables or charts; limits and failures; and plain-English advice on choosing an approach.
3. **Repeatable evidence:** the source versions, settings, data-generation and test commands, individual measurements in CSV or JSON, and exact differences found. Identify where private evidence is retained without publishing private data.
4. **Ranked recommendations for future instructions:** benefits, evidence gaps, risks, safe behavior without the instruction, and the effort required.
5. **Updated project documentation:** examples kept consistent with the working code, a short explanation of any query-language changes and remaining limits, a README summary, and links to the detailed results from the review guide.

The user will review and make commits. This plan does not authorize deployment or implementation of the proposed future instructions.

## Saved versions and supporting notes

These fixed commits preserve the starting source. Do not replace them with moving branch names or reset the current working checkout to use them.

| Repository | Starting commit |
| --- | --- |
| BloodHound | `7d57d4d11f34038991c491983a9490a20cb7ac58` |
| DAWGS, stored separately in `tools/DAWGS` | `5f16c574e48045472d15a447c2fa9707d394a4bd` |

Both matched their locally recorded upstream branches before the documentation updates. BloodHound does not include the DAWGS checkout in its own commit.

Engineers can use [the author guide](DEVELOPERS.md), [the exact ESC1 behavior notes](extension/QUERY_NOTES.md), [existing checks and recorded results](REVIEW.md), [current program limits](internal/runner/limits.go), and [handoff notes](HANDOFF.md) for the details supporting this plan. Follow the [pilot instructions](AGENTS.md) and [DAWGS instructions](../DAWGS/AGENTS.md) when implementing the work.
