# Make extension development easier

Give extension authors a clear path from an idea to working rules: create an extension, check its files, explore and test its execution chains, review the proposed results, and activate the rules.

Open items describe proposed improvements; completed work is listed under [Done](#done). Keep the engine's existing shared calculations, scope and identity checks, complete-result validation, and all-or-nothing saving. (Each save is still all or nothing. Since 16 October 2026, a failing step is set aside with the steps tied to it, and the rest is saved in a new attempt; see DESIGN.md, "When one step fails".) The interactive execution graph below replaces the earlier suggestion for a separate development-mode execution preview.

Earlier validation and useful execution reports are implemented. The read-only analysis workspace MVP for current rules and completed runs is implemented; [verification and remaining checks](evidence/analysis-workspace/README.md) describe its review status. Management, collapsed views, live progress, and interactive testing follow in later phases. Keep the completed authoring documentation current as the engine changes. Coordinate the Cypher work with the existing [implementation plan](CYPHER_IMPLEMENTATION_PLAN.md).

## 3. Build an interactive analysis workspace using BloodHound's sequential layout

Follow the [analysis workspace plan](../../_plans/opengraph-analysis-workspace.md). Reuse BloodHound's familiar sequential graph display, navigation, styling, table, and details-panel patterns in a dedicated analysis page. Each node represents a query and each dependency arrow explains what its consumer needs. Show branches, shared prerequisites, and dependencies between extensions. This replaces the Ghidra/IDA presentation proposal.

**The execution diagram is display information only. Never store query nodes, dependency arrows, or collapsed groups in BloodHound's collected-data graph.** Build it from rule definitions, the execution plan, and the selected run's reports. Keep normal Explore behavior intact; actual relationships produced by analysis can still be opened there.

The MVP links to the current rules page for upload, activation, rule management, and Run Analysis. Move those controls into the workspace in a later phase, after their replacement is ready. Schema uploads stay on OpenGraph Management; TODO 6's extension association and version checks serve both pages.

### MVP: understand current rules and completed runs

- [x] Draw the current activated rules in sequential layout, using complete runner-derived dependencies and showing shared prerequisites once. No new execution modes, storage, or DAWGS changes are required by the MVP.
- [x] Reuse pan, zoom, fit, search, extension filters, and keyboard navigation. Highlight prerequisites and downstream consumers; explain filtered-out dependencies and let the author reveal them.
- [x] Coordinate one focused query between graph, basic rule table, and details panel. Show existing properties, full Cypher, enabled/disabled choice and blocking reasons; provide individual download tied to the displayed revision.
- [x] Show existing completed-run counts, timings, failures, and saving outcomes on the graph only when the report matches its revision. Explain older or unavailable reports separately. Existing overall running status must not imply live per-query progress.
- [x] Provide Refresh and links to existing management controls. Handle no active rules and missing reports clearly. Keep existing permissions and normal Explore behavior intact; loading or navigating the page writes no collected graph data.

**MVP done when:** an author can inspect the current ESC1 or cross-extension rules, follow prerequisites and consumers, inspect/download a query, understand its availability and matching completed outcome, and reach existing management controls. Full TODO 3 remains open after this milestone.

### Roadmap: complete TODO 3

After the MVP, add rule management, then focused/collapsed views, then historical diagrams/live progress, then isolated testing and answer inspection. The complete checklist below retains all original requirements across these phases; it is not the MVP release gate. The picture must always distinguish dependency order from observed execution timing.

- [x] Supply complete dependency connections from the runner's plan before drawing the graph. Show a shared prerequisite once, with arrows to all its consumers. Label supplied answers/parameters and dependencies through calculated graph data distinctly; shared-output availability does not itself establish execution order.
- [ ] Support pan, zoom, fit, search, extension filtering, and expanding or collapsing chains. Selecting a query should highlight its prerequisites and downstream consumers, with navigation in both directions.
- [ ] Coordinate graph focus, table rows, and query details. Let the table show selected rules, prerequisites, downstream consumers, or the whole highlighted chain. Keep explanatory highlighting separate from the explicit selection used for actions; preserve selection between views of the same revision and show hidden selection counts. Historical runs are read-only; changing revisions clears action selections.
- [ ] Keep invalid/incomplete drafts usable through a file table and validation explanations when no execution plan can be drawn. Retained rules whose schemas are missing remain visible and downloadable. Do not invent execution order or enable Test/Activate before their required checks pass.
- [x] Show each query's name, extension, order, stage, date added, and enabled state. Explain when a query is paused because a prerequisite is disabled.
- [x] Open a details panel from a query block with its full Cypher, graph inputs, prerequisite parameters, consumers, helper types and ID prefixes, relationship types, allowed endpoints, and existing-edge policy. Include any optional reachability, group-overlap, and batching instructions and explain their effect on the answer and execution.
- [ ] Provide query download, individual and multiple selection, enable, disable, and delete actions through the graph and table. Right-click is an optional shortcut to visible, keyboard-accessible controls. Distinguish all filtered rules from all rules in the displayed revision, with counts. Highlight requested changes and every additional effect in both graph and table, revealing affected groups and other extensions. Preserve the existing review and activation checks and show the target revision.
- [ ] Make upload review, activation, refresh, and Run Analysis available from the graph workspace. Clearly identify the proposed, activated, running, and last successfully applied revisions.
- [ ] Show enabled/disabled choice and any blocking reason separately from execution and saving: idle/waiting/running/succeeded/failed/skipped/canceled, and confirmed/uncertain/unavailable saved results. Add bounded live updates, counts, durations, and reasons when available. Identify stale updates and retain bounded historical plans rather than substituting today's files. Keep positions stable; queries at the same stage need not run together.
- [ ] Jump from a failure to its query, reveal it inside a collapsed group, and explain the phase, reason, and dependent work that could not proceed. Keep shared setup and saving failures at run level when no individual query is responsible.
- [ ] Let authors test a proposed revision or selected chain from this workspace, including all required prerequisites. Show intermediate answers, validation failures, and proposed relationship additions, removals, and property changes beside the relevant query.
- [ ] Use the production calculation and validation code for tests. Begin with configured isolated test targets and repeatable baselines, accounting for helper objects, preparation, cleanup, and final writes. Tests cannot publish to the live graph or activate drafts. Current-application-data testing requires a separately verified consistent view or isolated copy.
- [ ] Distinguish a limited display of results from a complete calculation. A sample must never be treated as a complete replacement for published relationships.
- [ ] Bound retained inspection data separately from calculation data, with expiry and access checks. Progress or optional inspection failures cannot fail, repeat, or indefinitely delay ordinary analysis.
- [ ] Support small collection files and written expected results, without requiring Go tests. Cover empty answers, duplicate support, removal of one or the last support, failure preserving previous results, and collectors arriving in either order. Run the same cases through each supported database.
- [ ] Keep the graph usable with many rules through focused chain views and nonoverlapping collapsed groups showing rule, selection, and state counts. Preserve boundary connections and explain hidden cross-extension dependencies. Provide keyboard navigation and readable state labels as well as colors.

Keep changes concentrated in the new page, authenticated server access, and our existing runner/service. Retain current rule storage. No additional DAWGS work is expected for display or reporting; propose it only for a demonstrated missing capability. Safe tests and answer inspection need runner work regardless of the display style, and must preserve complete shared-output calculations.

**Full TODO 3 done when:** an author can follow a chain, inspect its query and intermediate answers, understand a failure, manage the rules, and review the effects of a proposed change from the graph workspace. Activating rules remains a separate, explicit action.

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

**Status: open — code implemented; verification and review unfinished (14 October 2026).** The checked items below describe implemented code and documentation. Schemas upload through OpenGraph Management and analyses stay on the rules page. The experimental parser reads installed definitions. Namespaces separate filenames, analysis versions are independent of schema versions, and optional requirements-style conditions constrain shared queries. No condition means the latest activated local query. Shared answers use descriptive Cypher columns and ordinary documentation.

See the [approved integration plan](../../_plans/opengraph-analysis-extension-integration.md) and [verification record](evidence/extension-integration/README.md). The future deletion checkbox and TODO 3 management page are outside this implementation. The existing 32-file/4 MiB limits remain; no measurements justify increasing them yet.

- [x] Identify every schema and rule by both its extension and its filename. Allow different extensions to each contain `schema.json` and identically named rules, such as `users.cypher`.
- [x] Resolve analyses against schemas already installed through OpenGraph Management, preserving the definition information the engine needs. Avoid a second schema upload to the rules page, and prevent retained analyses from reinstalling a deleted schema.
- [x] Make uploads, drafts, overwrite choices, downloads, updates, and rule controls distinguish which extension a file belongs to. Updating one extension must not overwrite another extension's same-named file. Continue reviewing genuine effects on rules in other extensions before activation or deletion.
- [x] Keep references within an extension simple, and make references to another extension's rules explicit. Keep their calculated answers distinguishable even when the rule filenames match.
- [x] Provide explicit references to shared queries with optional requirements-style version conditions and local answer names. No version condition means latest. Keep an analysis's own version separate from the extension versions it supports, so compatible upgrades do not require editing unchanged files.
- [x] Describe shared answer columns, their meanings, and their value types, and catch incompatible changes before activation where possible.
- [x] Review limits and retain the current bounded file count, total storage and execution work; no analysis archive was introduced.
- [x] Publish small reusable examples for identity matching and membership calculations, with expected results and dependency explanations.
- [x] Update [END_USERS.md](END_USERS.md) to explain the separate upload pages and removal of the global filename restriction.
- [ ] Verify the complete workflow through the real BloodHound pages: install three extensions at different times with matching schema and rule filenames, upload and run their analyses, update one, and delete one rule. Cover either upload order and schema deletion while analysis files remain. Confirm that filenames alone never cause changes to another extension and that intended connections between their rules still work. Existing browser checks used a simulated schema reader; the complete real-app walkthrough is still outstanding.
- [ ] Investigate the UI failures from `CI=true just test`, rerun the checks, and record the outcome. Failures in unchanged files have not yet been established as unrelated baseline failures.
- [ ] Complete `just prepare-for-codereview` in a clean checkout without the ignored lab history. The previous run stopped during recursive formatting; later checks did not finish.
- [ ] Run `just gen-spec` with the required generator available and review its output. The affected OpenAPI JSON was updated locally, but standard generation failed because the tool was not cached.
- [ ] Review the complete patch and address findings before closing TODO 6. Nothing has been staged, committed, or deployed.

**Done when:** the real-app workflow and remaining checks above are completed and reviewed, showing that users can install and update supplied extensions without renaming files, and authors can reuse another extension's calculation without coordinating filenames or discovering changed answer shapes during execution. The plan's deferred deletion control and TODO 3 workspace move are separate future work.

Start in [the definition registry](internal/extension/registry.go), [saved-file validation](internal/ruleservice/validation.go), and [the cross-extension example](examples/cross-platform/README.md). The author-facing declarations and shared-answer approach were approved before implementation.

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

**Completed: 13 October 2026.** DAWGS now preserves `WITH` ordering, skipped rows and limits before supported writes. The complete DAWGS suites pass on PostgreSQL and Neo4j, including 38 shared selection cases and checks of the saved graph after failed or canceled writes. PostgreSQL also passes the same selection cases with optimizations disabled. See [the correction and evidence](evidence/write-limit-followup/README.md) for the explanation, source fingerprints, results and remaining unsupported forms.

The comparison cleanup exposed this separate DAWGS problem: `MATCH (n) WITH n LIMIT 2000 DETACH DELETE n` was translated without the limit, causing a much larger deletion than requested. The isolated test graph was preserved in its original snapshot, and the measurement tool selects an explicit list of IDs before deleting. That earlier workaround and its historical evidence remain unchanged; the general correction is this separate follow-up.

- [x] Add small matching tests on both databases for limits and ordering before writes, including deletion.
- [x] Correct PostgreSQL translation so the selected objects and their number are preserved.
- [x] Check other supported writes after `WITH` and document unsupported forms clearly.

**Done when:** a limited write affects exactly the objects selected by the preceding query on both databases. Keep this separate from the completed read-query corrections and the measurement-only cleanup workaround.

## 11. Make the checks before an analysis cheaper

**Completed: 13 October 2026.** Separate timers identified avoidable work in the whole-graph identity query. An equivalent query reduced PostgreSQL preparation by 24.4% in the targeted 100,000-object comparison, with identical saved answers and identity/failure checks on both databases. Neo4j timing ranges overlap. See [the follow-up report](PREPARATION_AND_REACHABILITY.md) and [retained evidence](evidence/preparation-followup/README.md).

The million-object PostgreSQL comparison spent about 4.8 seconds preparing an advanced run and 0.3 seconds calculating it. Both authoring styles perform this preparation. It includes database setup, selecting the rules to run, and checking object identities. Those earlier phase totals did not identify which part accounted for most of the time. This follow-up investigated that cost without removing checks or adding author instructions.

- [x] Measure the separate preparation steps before choosing a change: database type/index preparation, opening the transaction, selecting affected rules, and checking missing, empty or duplicate object IDs.
- [x] Use those measurements to identify unnecessary repetition or an expensive database query. Prefer an equivalent, cheaper check or removing proven duplicate work within one run while the checked information remains unchanged.
- [x] Preserve the full scope of identity checks, including unrelated objects and the narrow exception for isolated migration records. Do not make checks optional for authors.
- [x] Keep reuse across separate runs out of the first change. Any later proposal must explain how imports, writes, rule changes and restarts invalidate old checks; coordinate that proposal with #9.
- [x] Check exact results and failure preservation on both databases, then measure preparation and complete-run time separately with a small, targeted comparison. Reuse the existing evidence; do not repeat the full comparison matrix by default.

**Done when:** the cost of each preparation step is documented and a targeted change demonstrates lower cost with the same protections, or the investigation records why a proposed change should be deferred. Do not claim a speedup from the existing phase totals alone.

See [how this would work](CYPHER_COMPARISON.md#follow-up-reduce-setup-work-and-improve-queries-automatically). Start in [runner setup and identity checks](internal/runner/runner.go) and [database-specific preparation](internal/graphdb/native.go). This is a separate completed follow-up, not a revision of the earlier comparison measurements.

## 12. Improve ordinary queries without extra instructions

**Completed: 13 October 2026, after correcting #13.** The direct correction removes proven repeated work from the unchanged ordinary membership query: 267 rows before DISTINCT instead of 26,967, with identical complete answers. Instrumented PostgreSQL execution medians were 1.278 seconds before and 0.127 seconds after. Whole-analysis measurements were variable; no reliable whole-analysis percentage is claimed. The evidence does not justify a separate reachability implementation. No author files, calculation limits or syntax requirements changed. See [the report](PREPARATION_AND_REACHABILITY.md#correcting-the-input-association-then-measuring-ordinary-queries) and [evidence](evidence/path-input-followup/README.md).

Investigate whether the engine can perform a clearly recognized ordinary query more efficiently while returning exactly the answer it requests. Begin with distinct reachable objects, where the comparison identified a PostgreSQL cost. This does not assume that arbitrary Cypher can be replaced with the advanced calculations.

- [x] Inspect the database's execution plan for one expensive query form. First check for unnecessary reads or repeated work that can be corrected directly, as happened with the already selected endpoint during this study.
- [x] Evaluate whether a dedicated reachability calculation is justified. The direct correction is sufficient for the measured query; no additional calculation was added. It acts on parsed ordinary path operations, not text matching or guesses from graph data. The narrower replacement and modern `ANY` remain possible separate proposals.
- [x] Establish matching behavior for permitted links, direction, starting objects, path bounds, loops, filters, empty answers and repeated rows. Shared written-answer cases cover these conditions. Routes, lengths, counts and ordering retain ordinary path execution; there is no endpoint-only replacement.
- [x] Preserve existing time, memory, input and saving protections. Resolve any dependence on unrestricted path searches with the separately documented depth-limit review; this task does not authorize removing another limit.
- [x] Compare saved builds before and after the correction using the same unchanged query and complete answers on both databases. Test the correction with PostgreSQL optimizations enabled and disabled. Measure the single-query plans and both complete example styles; preserve all observed timing variation. The correctness correction is always applied, so there is no separate shortcut switch.
- [x] Document the direct improvement, its measured cost and limits, and why no separate reachability calculation was added. Leave automatic group selection and automatic input splitting for separate proposals unless their equivalence can also be established explicitly.

**Done when:** one supported form receives a verified automatic improvement without author-file changes, or the investigation records why it should remain ordinary Cypher. Other forms must continue to work with their existing meaning and limits. No general automatic optimizer or new speedup is promised by this TODO.

See [how this would work](CYPHER_COMPARISON.md#follow-up-reduce-setup-work-and-improve-queries-automatically). Start in [DAWGS query translation](../DAWGS/cypher/models/pgsql/translate), [query execution](internal/runner/calculate.go), and [the existing reachability calculation](internal/runner/reach.go). Within this performance follow-up, investigate #11 first. The previously listed authoring and interface work remains separate.

## 13. Preserve the starting input for ordinary path results

**Completed: 13 October 2026.** Each input row remains attached to its own ordinary path search, including repeated inputs and optional matches. The original two-group reproduction returns only the two correct pairs. The shared template contains 33 written-answer cases; the corrected membership query retains its complete answer while avoiding the extra combinations. See [the implementation and measurements](PREPARATION_AND_REACHABILITY.md#correcting-the-input-association-then-measuring-ordinary-queries).

The #12 investigation found that PostgreSQL could combine each `UNWIND` input with paths belonging to other input rows. An ordinary query retaining both the requested starting ID and actual starting object returned mismatched pairs, despite its `WHERE` requiring them to agree. ESC1's membership query hid those extra rows with `DISTINCT`, but still paid for the extra work.

- [x] Add matching, written-answer cases on both databases for several starting objects, repeated and empty inputs, zero-length paths, direction, and returned input columns.
- [x] Correct how DAWGS reconnects supplied input rows with path results. Preserve legitimate repeated answers; deleting the input join alone is not sufficient.
- [x] Check required and optional matches, filters, bounds, route/count results and saving queries before making broader performance claims.
- [x] Repeat the single membership-query plan and a small exact-answer comparison. Keep automatic reachability selection separate until its meaning and limits are agreed.

**Done when:** each result remains associated with the input that selected it, on both databases, and the unnecessary combinations are absent without losing requested repeated rows. See [the recorded query and answers](PREPARATION_AND_REACHABILITY.md#what-the-ordinary-reachability-plans-showed). The correction is in DAWGS `buildExpansionPatternRoot` and `buildExpansionPatternStep`, with the input handling in `cypher/models/pgsql/translate/expansion_input.go`. It was implemented following the user's subsequent request to fix #13 and then complete #12.

## 14. Create an OpenGraph Extension Designer page

Provide a no-code style canvas that lets extension authors write an OpenHound collector targeting a REST API, using familiar BloodHound graph navigation.

- [ ] Create the OpenGraph Extension Designer page with a visual canvas and familiar graph navigation, including pan, zoom, selection, and details panels.
- [ ] Let authors describe REST API requests and map the returned data to OpenGraph objects, properties, and relationships through the canvas.
- [ ] Let authors review and test the collection flow and produce the corresponding OpenHound collector.

**Done when:** an author can use the designer to create and test an OpenHound collector for a REST API while navigating its collection flow with familiar BloodHound controls.

## 15. Run a single analysis step from its entity panel

Requested 16 October 2026: a play button at the top of the entity panel for a selected step, which calculates and saves that step's results without a full Run Analysis. Today the only way to save results is **Run Analysis**, which runs every enabled rule that collected-data changes affect. The editor's play button tests a file but never saves. This is new engine work that changes saved graph data, so agree the design before coding.

- [ ] Decide what runs. The step needs its prerequisite chain, but those intermediate answers are not saved. It also needs every other producer of the same relationship or helper kind, because their answers are combined and saving one producer alone would retire the others' connections. The editor's plan selection already builds this chain.
- [ ] Decide what happens downstream. Queries that read this step's output would be out of date afterward. Either run them too (which approaches the normal affected-rules run) or mark their saved results as possibly stale until the next full run.
- [ ] Go through BloodHound's normal analysis request and the existing graph-write gate, so a single-step run cannot overlap ingestion, another analysis, or editor tests. Use the same permission as Run Analysis.
- [ ] Keep all-or-nothing saving, complete-result validation, and the existing protections for shared calculations and cleanup.
- [ ] Record it as a partial run in the status report. Steps that were not part of it show "not run in this attempt" rather than losing their earlier outcome.
- [ ] Add the play button at the top of the entity panel, disabled with a reason for disabled or blocked steps. Clicking it shows a confirmation listing what will be calculated and what will be saved.

**Done when:** an author can save results for one step from its entity panel, the saved graph matches what a full run would save for the same outputs, other steps' results are untouched or clearly marked stale, and the status report shows exactly what that run did.

## 16. Index extension objects by ID on Neo4j so large uploads stay fast

**Found 16 October 2026, while testing OpenHound's `graph upload`; confirmed on unmodified upstream BloodHound.** Neo4j is the default database in BloodHound's example setup, so every Community Edition user with a large extension collection is affected.

When BloodHound imports an object, it looks for an existing object with the same ID and the same source kind (the `source_kind` named in the upload's `metadata`). Neo4j can only find it quickly if that source kind has an ID index. BloodHound creates ID indexes only for its own built-in object types, such as `Base`, `User` and `Computer`. Nothing creates one for an extension's source kind: not registering the source kind during import, and not installing the extension's schema. Without the index, Neo4j checks every object of that kind, one by one, for every object imported. Doubling the upload size roughly quadruples the import time. PostgreSQL is not affected in the code, because it keeps one ID index covering all object types; this was not tested.

Measured import times for 20,000 objects of one new source kind, from BloodHound's own import log (not counting the analysis that follows):

| Where | With an ID index | Without |
|---|---|---|
| Upstream BloodHound `main` (`eaf9c78`, v9.8.0) on Neo4j 4.4, extension schema installed | 5.9 seconds | 157 seconds |
| This lab | 0.6 seconds | 129 seconds |

Full measurements, versions, query plans and the scripts to repeat them are in [the evidence](evidence/neo4j-source-kind-index/README.md).

In the lab, a 150,000-object upload had spent 37 minutes on its first 50,000 objects, and was on course to take about 4 hours in total. Neo4j's query plan shows the cause directly: without the index it reads every object of the kind (`NodeByLabelScan`), with it it looks the ID up (`NodeIndexSeek`). Neither DAWGS nor BloodHound changes in this repository cause it: the Neo4j driver code is identical to DAWGS `main`.

**Temporary workaround:** an administrator creates the index directly in Neo4j, once per source kind: `CREATE INDEX IF NOT EXISTS FOR (n:<SourceKind>) ON (n.objectid)`. This needs direct database access, because BloodHound's Cypher page cannot make changes by default. BloodHound removes the index again on its next restart, because its startup check deletes every index that is not in its built-in list (the lab log shows it removing our own `poc_endpoint_*` indexes at startup). So it must be created again after every restart, before the next large upload. OpenHound cannot do this for users: it only talks to BloodHound's web API, which cannot create indexes.

- [ ] Agree where the fix lives before coding. Recommended: in BloodHound, include every registered source kind (already listed in the `source_kinds` table) in the list of object types whose IDs are indexed, everywhere BloodHound checks its indexes. The alternative is a new DAWGS feature that only adds indexes and never removes any; it is tidier for callers but changes a shared interface both databases must support.
- [ ] Never ask DAWGS to check indexes with only the new source kind. Its check removes every index it is not told about, so that would delete all the built-in indexes. Always supply the complete list.
- [ ] Create the index when a source kind is first registered during import, before its objects are written, so the first large upload is fast.
- [ ] Keep the indexes through restarts by including the registered source kinds in the startup check.
- [ ] Handle two imports registering the same new source kind at the same time, and an index being built for a source kind that already holds many objects, without failing either import.
- [ ] Check that PostgreSQL behaviour is unchanged, and measure it with the same 10,000- and 20,000-object uploads to confirm it is not affected.
- [x] Record the measurements, versions and query plans under [`evidence/neo4j-source-kind-index`](evidence/neo4j-source-kind-index/README.md).
- [ ] Add a regression test that fails if an extension source kind has no ID index after import on Neo4j.
- [ ] Report it upstream to SpecterOps (BloodHound, and DAWGS if the fix is placed there) with a short reproduction on upstream `main`, the measurements, the workaround and the proposed fix. No existing public issue was found.
- [ ] Update [END_USERS.md](END_USERS.md) with the workaround until the fix is available.

**Done when:** on Neo4j, importing 20,000 objects of a brand-new extension source kind takes about as long as for a kind that already has an index, the index survives a BloodHound restart, and no built-in index is lost.

Start in [source kind registration](../../cmd/api/src/services/graphify/tasks.go) (`RegisterSourceKind`), [the startup index check](../../cmd/api/src/migrations/graph.go), and the [Neo4j index code](../DAWGS/drivers/neo4j/index.go) (`assertSchema`). This changes BloodHound, so per the project rules it needs approval before coding.

## 17. Accept extension names that look like true, false or a number

**Found 16 October 2026, while testing OpenHound's `schema upload` with an extension named `T`.** The upload fails with a 500 error, whether or not the extension already exists. `GH`, `ADCS` and `GitHub` work.

When an extension is uploaded, BloodHound looks for an existing one with the same name. That lookup does not say the name is text (`IsStringData` is not set), so the shared filter builder guesses the value's type. It tries a whole number, then a decimal number, then true/false, and only then treats it as text. Go reads `T` as true, so the database is asked for `name = true` and refuses to compare text with true/false.

Affected names are exactly `1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False`, any whole or decimal number (`12`, `1.5`, `1e5`, `0x10`), and `NaN`, `Inf` or `Infinity` in any capitalization. Mixed capitalization such as `tRUE` is unaffected. Two other name lookups, `GetEnvironmentKindName` and `GetSchemaFindingByName`, have the same omission but nothing calls them yet. The data-quality `environment_id` filter also omits it; real environment IDs (security identifiers, tenant GUIDs) never look like numbers or true/false, so it is left out of this fix.

**Temporary workaround:** give the extension a name that does not look like true/false or a number, such as `Test`.

- [ ] Decide where the fix goes. Recommended: a branch of its own from `experimental-postprocessing`, so it can also be offered upstream as a separate pull request. It is a general BloodHound bug, not post-processing work.
- [ ] Add `IsStringData: true` to the name filter in `findOrCreateExtension`.
- [ ] Add the same to `GetEnvironmentKindName` and `GetSchemaFindingByName` so they do not fail once something calls them.
- [ ] Add integration tests that upload extensions named `T`, `false`, `1`, `12` and `NaN`. Each must be created on the first upload and updated, not duplicated, on the second. Add a test that looks up a finding named `T`.
- [ ] Run the database integration tests, rebuild the lab, and repeat `schema upload` with the extension named `T`.
- [ ] Write a change note with the test evidence and a suggested commit message.
- [ ] Optionally report the type-guessing filter builder to SpecterOps as a separate issue. Making the builder know which columns hold text would prevent this everywhere, but it touches every filtered API and needs their review.

**Done when:** an extension named `T`, `1` or `NaN` can be uploaded, uploaded again as an update, and found by name, and the new tests cover these names.

Start in [extension lookup](../../cmd/api/src/database/upsert_schema_extension.go) (`findOrCreateExtension`), [the filter builder](../../cmd/api/src/database/helper.go) (`buildSQLFilter`), [the other two name lookups](../../cmd/api/src/database/graphschema.go), and [the extension upload tests](../../cmd/api/src/database/upsert_schema_extension_integration_test.go). This changes BloodHound, so per the project rules it needs approval before coding.

## 18. Profile and optimize OpenGraph Analysis at 20 million nodes and 200 million edges

**Planned follow-up, 16 October 2026.** Apply the supplied `bhe-performance-optimization` skill's measure-first workflow to **BloodHound Community Edition (BHCE)** and its OpenGraph Analysis feature. First test a generated graph containing **20 million nodes and 200 million edges**, counted separately; eventually repeat with a realistic dataset. The earlier comparison stopped at one million objects (#7); it does not establish performance at this new scale or on realistic data.

- [ ] Gather context: identify the analysis workload, rule revisions, slow phases, run frequency, and any existing logs. Read the relevant code and DAWGS queries to identify likely costs without choosing a fix from code inspection alone.
- [ ] Confirm the actual BHCE environment, graph database, container, profiling endpoints, hardware, and dataset state. Adapt the skill's Enterprise-specific paths and commands to this standalone `BloodHound` checkout; do not assume a BHE parent repository, `bhe-api-primary`, or Enterprise tooling is available. If investigating local DAWGS changes, confirm the BHCE build uses that checkout.
- [ ] Add useful timing and work-count logs or metrics as a separate reviewable change. Measure preparation, query execution and shared calculations, result validation, and saving, including time spent in the DAWGS database driver. Retain only measurements with lasting troubleshooting value; remove or gate noisy investigation logs.
- [ ] Ask the user to load the 20-million-node, 200-million-edge dataset, start Parca, and trigger the baseline run. Confirm the actual graph counts and record the data-generation settings, source versions, database settings, and machine resources so the experiment can be repeated. Dataset loading, starting Parca, and triggering runs remain the user's steps.
- [ ] Capture baseline logs plus Parca CPU and memory profiles; use pprof for focused investigation when useful. Record complete-run and per-phase durations, call counts, peak memory, allocations, database and temporary-storage costs, failures or limits reached, and effects on normal BloodHound use. Preserve the profiles and check the calculated answers, rather than judging speed alone.
- [ ] Propose and implement one measured optimization at a time. For each before/after comparison, ask the user for a fresh run against the same input and starting database state. Confirm equivalent answers, preserved failure protections, improvement in the target phase, and no regression in other phases. Keep each change and any needed tests separately reviewable; follow this project's rule that the user stages and commits changes.
- [ ] Eventually repeat the workflow against a realistic dataset supplied and loaded by the user. Record its actual node/edge counts and relevant graph structure; do not assume it has the generated graph's size or behavior. Confirm any dataset-loading tools, including anubis if available, are appropriate for the BHCE setup.
- [ ] Save a plain-English comparison with reproducible steps, source and dataset versions, profiles, correctness checks, remaining limits, and recommendations. Store new evidence separately and preserve the earlier studies unchanged.

**Done when:** the 20-million-node, 200-million-edge BHCE experiment and the later realistic-data experiment have reproducible baselines and measured follow-up results, with verified answers and documented resource limits. Capacity is demonstrated only for runs that actually complete; unfinished or failed runs remain explicit limitations.

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

The native analysis workspace now includes the requested Explore-style canvas, resizable bottom rules/report panel, right entity panel, uploads and draft review, individual/multiple-rule enable/disable/delete, and Run Analysis. See [workspace usage](USAGE.md#inspect-the-analysis-workspace) for permissions and revision checks. Collapsed graph groups, historical diagrams, and live query progress remain later work. The editor follow-up below adds temporary query tests. The older rules page remains available.

Requested collected-data inspection example: retain the completed `usable_cas` answer and open its matching collected objects in the workspace. This belongs to the bounded answer-inspection phase. Do not rerun today's query and label it the last run, or treat current graph properties as a historical snapshot. The active lab includes the optional advanced ESC1 bundle and the ordinary Cypher bundle in separate namespaces.

## 14 October 2026 — editor follow-up

The analysis file editor, its private working copies, temporary tests, exact-file review, and activation are implemented. The remaining display work is a standalone archived-answer viewer and a historical graph snapshot viewer. Those are different from the editor's current result preview and must preserve their source versions and graph state.
