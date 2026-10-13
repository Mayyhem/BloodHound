# Make extension development easier

Give extension authors a clear path from an idea to working rules: create an extension, check its files, explore and test its execution chains, review the proposed results, and activate the rules.

Open items describe proposed improvements; completed work is listed under [Done](#done). Keep the engine's existing shared calculations, scope and identity checks, complete-result validation, and all-or-nothing saving. The interactive execution graph below replaces the earlier suggestion for a separate development-mode execution preview.

Earlier validation and useful execution reports are implemented. Next, build the interactive graph and its testing controls. Starter tooling and easier sharing build on those capabilities. Keep the completed authoring documentation current as the engine changes. Coordinate the Cypher work with the existing [implementation plan](CYPHER_IMPLEMENTATION_PLAN.md).

## 3. Build an interactive execution graph, like Ghidra/IDA

Make a connected graph the main workspace for understanding and testing rules. Use the block-and-arrow navigation familiar from Ghidra and IDA: each block represents a query, and an arrow leads from a prerequisite to the query that consumes its answer. Show the chains, their branches, and where they join, including dependencies between extensions.

Bring the details and actions currently available in the rule table into this graph. The picture must follow the actual execution plan and distinguish dependency order from observed execution timing.

- [ ] Lay out query blocks in dependency order. Show a shared prerequisite once, with arrows to all its consumers. Label connections with the supplied answer or parameter where useful.
- [ ] Support pan, zoom, fit, search, extension filtering, and expanding or collapsing chains. Selecting a query should highlight its prerequisites and downstream consumers, with navigation in both directions.
- [ ] Show each query's name, extension, order, stage, date added, and enabled state. Explain when a query is paused because a prerequisite is disabled.
- [ ] Open a details panel from a query block with its full Cypher, graph inputs, prerequisite parameters, consumers, helper types and ID prefixes, relationship types, allowed endpoints, and existing-edge policy. Include any optional reachability, group-overlap, and batching instructions and explain their effect on the answer and execution.
- [ ] Provide query download, individual and multiple selection, enable, disable, and delete actions directly from the graph. Before a change, highlight every affected query, including consumers in other extensions. Preserve the existing review and activation checks.
- [ ] Make upload review, activation, refresh, and Run Analysis available from the graph workspace. Clearly identify the proposed, activated, running, and last successfully applied revisions.
- [ ] Overlay actual execution state on the graph: waiting, running, succeeded, failed, skipped, or disabled. Show row counts and durations when available, and explain why a rule ran, was skipped, or could not proceed. Do not imply that queries at the same stage necessarily run together.
- [ ] Let authors test a proposed revision or selected chain from this workspace, including all required prerequisites. Show intermediate answers, validation failures, and proposed relationship additions, removals, and property changes beside the relevant query.
- [ ] Use the production calculation and validation code for those tests, with isolated test data or a consistent database view that cannot publish preview changes. Account for helper objects and database preparation as well as final relationship writes. Testing a draft must not activate it.
- [ ] Distinguish a limited display of results from a complete calculation. A sample must never be treated as a complete replacement for published relationships.
- [ ] Support small collection files and written expected results, without requiring Go tests. Cover empty answers, duplicate support, removal of one or the last support, failure preserving previous results, and collectors arriving in either order. Run the same cases through each supported database.
- [ ] Keep the graph usable with many rules through focused chain views and collapsed groups. Provide keyboard navigation and readable state labels as well as colors.

**Done when:** an author can follow a chain, inspect its query and intermediate answers, understand a failure, manage the rules, and review the effects of a proposed change from the graph workspace. Activating rules remains a separate, explicit action.

Start in [the rule page](internal/ruleservice/upload.html), [the existing plan details](internal/ruleservice/view.go), [change review](internal/ruleservice/changes.go), and [the runner](internal/runner).

## 5. Provide starter files and a simple development lab

Authors should be able to start with a working example without first building BloodHound, gathering UI assets, or learning the order of several container commands.

- [ ] Provide a starter generator for a minimal schema, query, sample collection, and expected result.
- [ ] Provide an environment check that explains missing tools, dependencies, images, settings, ports, and connections in plain English.
- [ ] Provide a single command to start a prepared lab with sample data and links to BloodHound and the execution graph.
- [ ] Reuse that lab for the graph's testing controls, so editing a rule does not require rebuilding the application.
- [ ] Document a short first-success walkthrough separately from instructions for building and maintaining the engine.

Possible command names are `init`, `doctor`, and `dev`; these are proposals, not existing commands.

**Done when:** a new author can create, test, and inspect their first relationship using the walkthrough and prepared environment.

Start in [USAGE.md](USAGE.md), [the build scripts](scripts), and [the command entry point](main.go).

## 6. Make shared queries easier to distribute and change

Schema filenames, query filenames, and answer parameter names are currently shared across all installed extensions. Uploading another extension's `schema.json` therefore looks like replacing the existing file. Users should be able to install supplied extensions without renaming their files or coordinating names with other authors. The complete saved set also has a 32-file limit.

- [ ] Identify every schema and rule by both its extension and its filename. Allow different extensions to each contain `schema.json` and identically named rules, such as `users.cypher`.
- [ ] Make uploads, drafts, overwrite choices, downloads, updates, and rule controls distinguish which extension a file belongs to. Updating one extension must not overwrite another extension's same-named file. Continue reviewing genuine effects on rules in other extensions before activation or deletion.
- [ ] Keep references within an extension simple, and make references to another extension's rules explicit. Keep their calculated answers distinguishable even when the rule filenames match.
- [ ] Provide explicit references to shared queries, with a clear way to identify the version a consumer expects.
- [ ] Describe shared answer columns, their meanings, and their value types, and catch incompatible changes before activation where possible.
- [ ] Revisit limits for files and extension packages while keeping total storage and execution work bounded.
- [ ] Publish small reusable examples for identity matching and membership calculations, with expected results and dependency explanations.
- [ ] Check installing three extensions at different times with matching schema and rule filenames, updating one, and deleting one rule. Confirm that filenames alone never cause changes to another extension and that intended connections between their rules still work. Update [END_USERS.md](END_USERS.md) when the filename restriction is removed.

**Done when:** users can install and update supplied extensions without renaming files, and authors can reuse another extension's calculation without coordinating filenames or discovering changed answer shapes during execution.

Start in [the definition registry](internal/extension/registry.go), [saved-file validation](internal/ruleservice/validation.go), and [the cross-extension example](examples/cross-platform/README.md). Agree on the author-facing format before implementing it.

## 7. Implement the Cypher simplification plan

ESC1 currently uses comments that ask Go code to transform query results. Running the Cypher by itself does not reproduce those transformations. Follow the existing [Cypher implementation plan](CYPHER_IMPLEMENTATION_PLAN.md) to provide complete ordinary-Cypher alternatives while retaining the instructions as supported advanced options. Authors should be able to choose either style, with measured guidance on performance and limits.

**Status: complete, 13 October 2026.** Ordinary and advanced examples pass exact-answer and failure-preservation checks on both databases. Eight final studies contain 852 timed observations through one million objects, and the separate batching-limit check passed on both databases. Original database contents and stopped states are restored. The [comparison report](CYPHER_COMPARISON.md) and [evidence](evidence/cypher-comparison/README.md) include direct feature comparisons, measured costs, limitations and ranked future controls. Performance tradeoffs remain for the user's review; no deployment or commit was performed.

- [x] Record the committed starting source and shipped rules: BloodHound `7d57d4d11f34038991c491983a9490a20cb7ac58` and DAWGS `5f16c574e48045472d15a447c2fa9707d394a4bd`.
- [x] Preserve restorable database copies, saved rule revisions, build and database versions, actual settings, and expected results. Confirm small-case answers using the committed starting source before changing calculations.
- [x] Add complete readable Cypher alternatives to `reachability` and `group-overlap`, and support complete queries without manual `batch-on`. Retain the instructions, their tests, and an advanced ESC1 example. Let both styles share useful engine calculations and resource protections.
- [x] Add only the DAWGS features needed for understandable rules and acceptable performance. Check existing behavior through the complete DAWGS tests on both databases and the pilot tests.
- [x] Compare exact results on Neo4j and PostgreSQL, including groups, special cases, shared relationship results, property updates, and failures that must preserve previously saved results.
- [x] Produce a separate comparison of each instruction against its Cypher counterpart on the same engine and inputs, changing one feature at a time. Report exact answers, elapsed time, memory, temporary storage, data transfer, limits, and when each option helps. Also measure the complete ordinary and advanced examples; do not infer their combined performance by adding individual differences.
- [x] Compare current and revised implementations through **1 million objects**, the stopping point requested by the user on 13 October 2026. Count relationships separately. Follow the plan's checks for correctness, speed, memory, temporary storage, and impact on normal BloodHound use. Larger capacity investigations are deferred.
- [x] Include ranked recommendations for additional advanced efficiency controls in the final report. State each candidate's benefit, evidence or missing experiment, correctness conditions, behavior when omitted, and effort to build and maintain. These are proposals, not implementation work.
- [x] Deliver `CYPHER_COMPARISON.md`, runnable examples for both styles, repeatable measurement instructions, and retained results. Update the author guide and remove only unused or duplicated code; keep the supported advanced instructions.

**Done when:** authors can express and test the complete calculation through understandable Cypher, choose optional advanced instructions using direct performance evidence, and review justified proposals for further controls.

Displaying those complete answers in the interactive execution graph remains part of #3. The comparison plan explicitly treats that interface as separate work.

Start in [the Cypher implementation plan](CYPHER_IMPLEMENTATION_PLAN.md) and [the current ESC1 query notes](extension/QUERY_NOTES.md).

## 9. Keep change tracking useful during large uploads

The engine tracks changed object and connection types to decide which rules need to run. It currently makes extra database lookups for many individual updates. Large uploads can exhaust the allowance for those lookups, leaving an incomplete picture of what changed and forcing all enabled rules to run.

Collect that information as changes are saved, so a million updated users can be summarized as “User information changed.” Keep the work and memory needed for tracking bounded without requiring users or extension authors to change their files.

- [ ] Have the code that saves graph changes report the affected types. Cover imports, BloodHound's own analysis, and other supported graph updates, including changes to properties and object types.
- [ ] Where existing data must be checked, check groups of objects together and collect the distinct affected types. Include previous object types and the connection types removed when an object is deleted. Uploaded file contents alone are not a complete record of these changes.
- [ ] Preserve tracking across failed or partly completed work. Clear pending changes only after confirmed extension saving or a successful decision that no rules need to run.
- [ ] Keep the fallback to all enabled rules whenever a complete account of changes cannot be guaranteed. Keep existing full runs for first use, changed rules, and application restarts. Explain the reason for a full run in the execution report.
- [ ] Compare selected-rule results with running all enabled rules on Neo4j and PostgreSQL. Cover large imports, repeated uploads, property changes, type changes, deletions, and tracking failures. Verify that unrelated rules can be skipped without leaving outdated connections.
- [ ] Measure import time, extra database work, memory, and total analysis time before and after the change. Raising the current limits alone does not complete this work. Update [END_USERS.md](END_USERS.md) and [DESIGN.md](DESIGN.md) with the verified behavior and remaining limits.

**Done when:** large uploads with reliably known changes can run only affected rules and their required calculations, without exhausting a per-object lookup allowance. Their results match a full run, and incomplete tracking still triggers all enabled rules. Uploads that genuinely affect every rule may still run them all.

Start in [change tracking](postprocessing/observe.go), [tracking for updates and deletions](postprocessing/observe_query.go), [BloodHound's import code](../../cmd/api/src/services/graphify), and [rule selection](internal/runner/runner.go). Changes to the database-writing code may also be needed.

## 10. Preserve limits before PostgreSQL writes

The comparison cleanup exposed a separate DAWGS problem: `MATCH (n) WITH n LIMIT 2000 DETACH DELETE n` was translated without the limit, causing a much larger deletion than requested. The isolated test graph was preserved in its original snapshot, and the measurement tool now selects an explicit list of IDs before deleting. This workaround does not correct general query translation.

- [ ] Add small matching tests on both databases for limits and ordering before writes, including deletion.
- [ ] Correct PostgreSQL translation so the selected objects and their number are preserved.
- [ ] Check other supported writes after `WITH` and document unsupported forms clearly.

**Done when:** a limited write affects exactly the objects selected by the preceding query on both databases. Keep this separate from the completed read-query corrections and the measurement-only cleanup workaround.

## 11. Make the checks before an analysis cheaper

The million-object PostgreSQL comparison spent about 4.8 seconds preparing an advanced run and 0.3 seconds calculating it. Both authoring styles perform this preparation. It includes database setup, selecting the rules to run, and checking object identities. The current measurements do not identify which part accounts for most of the time. This item investigates that cost without removing checks or adding author instructions.

- [ ] Measure the separate preparation steps before choosing a change: database type/index preparation, opening the transaction, selecting affected rules, and checking missing, empty or duplicate object IDs.
- [ ] Use those measurements to identify unnecessary repetition or an expensive database query. Prefer an equivalent, cheaper check or removing proven duplicate work within one run while the checked information remains unchanged.
- [ ] Preserve the full scope of identity checks, including unrelated objects and the narrow exception for isolated migration records. Do not make checks optional for authors.
- [ ] Keep reuse across separate runs out of the first change. Any later proposal must explain how imports, writes, rule changes and restarts invalidate old checks; coordinate that proposal with #9.
- [ ] Check exact results and failure preservation on both databases, then measure preparation and complete-run time separately with a small, targeted comparison. Reuse the existing evidence; do not repeat the full comparison matrix by default.

**Done when:** the cost of each preparation step is documented and a targeted change demonstrates lower cost with the same protections, or the investigation records why a proposed change should be deferred. Do not claim a speedup from the existing phase totals alone.

See [how this would work](CYPHER_COMPARISON.md#follow-up-reduce-setup-work-and-improve-queries-automatically). Start in [runner setup and identity checks](internal/runner/runner.go) and [database-specific preparation](internal/graphdb/native.go). This is proposed follow-up work, not part of the completed comparison.

## 12. Improve ordinary queries without extra instructions

Investigate whether the engine can perform a clearly recognized ordinary query more efficiently while returning exactly the answer it requests. Begin with distinct reachable objects, where the comparison identified a PostgreSQL cost. This does not assume that arbitrary Cypher can be replaced with the advanced calculations.

- [ ] Inspect the database's execution plan for one expensive query form. First check for unnecessary reads or repeated work that can be corrected directly, as happened with the already selected endpoint during this study.
- [ ] If a dedicated reachability calculation is justified, describe one narrowly supported query form and its exact meaning. Recognize it by reading the query's structure and operations, not by matching a few words or guessing from the current data.
- [ ] Establish matching behavior for permitted links, direction, starting objects, path bounds, loops, filters, empty answers and repeated rows. Queries requesting routes, lengths, ordering or other unsupported behavior must retain ordinary execution.
- [ ] Preserve existing time, memory, input and saving protections. Resolve any dependence on unrestricted path searches with the separately documented depth-limit review; this task does not authorize removing another limit.
- [ ] Compare the same query with the change enabled and disabled, using written expected answers on both databases. Confirm that unsupported forms take the ordinary path. Measure a small relevant before/after set and enable a shortcut only where its measured cost justifies it.
- [ ] Document the accepted query forms, where the change runs, measured benefit and remaining limits. Leave automatic group selection and automatic input splitting for separate proposals unless their equivalence can also be established explicitly.

**Done when:** one supported form receives a verified automatic improvement without author-file changes, or the investigation records why it should remain ordinary Cypher. Other forms must continue to work with their existing meaning and limits. No general automatic optimizer or new speedup is promised by this TODO.

See [how this would work](CYPHER_COMPARISON.md#follow-up-reduce-setup-work-and-improve-queries-automatically). Start in [DAWGS query translation](../DAWGS/cypher/models/pgsql/translate), [query execution](internal/runner/calculate.go), and [the existing reachability calculation](internal/runner/reach.go). Within this performance follow-up, investigate #11 first. The previously listed authoring and interface work remains separate.

## Done

### 2. Catch mistakes before rules are activated

**Completed: 12 October 2026.** Full query checks and visible-input validation are shared with execution. Dependency declarations remain explicit; broad reads are recalculated conservatively. Tutorial stages and shipped examples have permanent checks on both databases.

Checking filenames, declarations, and the final `RETURN` is not enough to establish that the complete query can run. Dependencies also determine when results are recalculated, so missing one can leave old results in place.

- [x] Parse the complete query during file validation using the same language checks as execution. Distinguish file validation, language support, and actual database execution in the results.
- [x] Report the filename, line, and useful explanation for each problem. Include the selected database and unsupported query feature when relevant.
- [x] Find graph kinds and prerequisite parameters that are visible in the query and check them against declared dependencies. Suggest corrections; handle uncertain cases conservatively.
- [x] Investigate automatically supplying dependencies that can be established reliably, while keeping exceptional dependencies explicit. Agree on any change to author requirements before implementing it.
- [x] Add regression cases for a misspelled `MATCH`, a query that reads `MemberOf` without declaring it, missing prerequisites, unsupported syntax, and incorrect output columns.
- [x] Add permanent automated tests for the quick-start files and shipped examples, including extracting each tutorial stage from Markdown and checking its expected results on both databases. Cover disabled and missing-property users, helper identities, property updates, and complete empty answers. Moved from #1 and implemented under the user’s request to execute #2.

**Done when:** authors receive actionable feedback before activation, and a query cannot silently omit an input that validation can identify.

Start in [the loader](internal/extension/extension.go), [output detection](internal/extension/output.go), and [the native execution adapter](internal/graphdb/native.go).

### 4. Make execution reports useful to the author

**Completed: 12 October 2026.** Reports distinguish calculations, confirmed saves, uncertain saves, and unavailable reports. They include per-query details, retain 20 recent attempts, and preserve the last confirmed saved revision.

The runner already records useful counts, but the persistent service mainly exposes a summary. A failure should give authors enough information to investigate without first finding application logs. These reports will also supply the execution graph.

- [x] Report the failing query, stage, and useful error details, without exposing secrets.
- [x] Include query row counts, durations, proposed and saved output counts, and the reason each query ran or was skipped.
- [x] Distinguish a successful calculation from a successful save, and identify when the displayed results still belong to an older revision.
- [x] Provide bounded recent execution history so authors can compare a changed rule with its previous run. Keep intermediate graph data available only through the appropriate inspection controls.
- [x] Make an uncertain save outcome or an unavailable report visibly different from a confirmed failure or success.

**Done when:** the graph and report explain what ran, what happened, and which results are currently saved.

Start in [the runner report](internal/runner/runner.go), [persistent reporting](postprocessing/service.go), and [stored status](internal/ruleservice/store.go).

### 1. Keep the authoring instructions and examples consistent

**Completed: documentation work on 11 October 2026.** Permanent automated tutorial and example tests were completed with [#2](#2-catch-mistakes-before-rules-are-activated) on 12 October 2026.

- [x] Check the current guides and example documentation against the authoring rules. Remove obsolete instructions and unnecessary alternative ways to express the same thing; label historical formats as history.
- [x] Provide one minimal starting example, followed by examples for shared answers, helper objects, and relationships with properties.
- [x] Explain which settings describe the extension's meaning and which decisions BloodHound handles automatically.

The guide names each file to add or replace, reuses one schema, and states the expected result at each step. The recorded one-time check loaded all four tutorial stages and all three shipped example directories with the existing local `plan` command. Those checks did not execute queries or prove results on either database. See [REVIEW.md](REVIEW.md#authoring-documentation-check) for the scope of that review.

Maintain [DEVELOPERS.md](DEVELOPERS.md) and [the example documentation](examples) as the authoring rules change.

### 8. Simplify schema kind declarations

**Completed: 11 October 2026.** Missing schemas may be uploaded later through the inactive draft workflow requested during this work.

- [x] Remove `external_node_kinds`. Native kinds come from BloodHound's graph definitions; custom kinds come from the complete uploaded schema set. Check both compatible endpoint kinds and explicitly declared graph inputs. Resolve references after all schemas are read, so file order and mutual schema references are allowed.
- [x] Rename relationship `endpoints` to `compatible_kinds`, preserving explicit, directed `source_kind`/`target_kind` pairs.
- [x] Update validation, the offline model viewer, shipped schemas, examples, tests, and authoring documentation together.
- [x] Save incomplete uploads as inactive drafts, retain them across restarts, and accept additional files in later uploads. Keep active rules unchanged; require complete validation and explicit review before activation. Circular calculation dependencies still prevent activation.

The offline viewer opens accompanying schemas together. The native kind check also identified and corrected the ESC1 example's `LocalGroup` label to BloodHound's actual `ADLocalGroup` label. See [REVIEW.md](REVIEW.md#schema-kind-declarations-and-inactive-drafts) for validation and [USAGE.md](USAGE.md#change-the-active-rules) for draft handling.
