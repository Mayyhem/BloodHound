# Build the OpenGraph analysis workspace

Status: read-only MVP and the requested Explore-style layout/rule management are implemented in the working tree, 14 October 2026. Management verification and deployment are recorded in the latest HANDOFF entry. The remaining features stay on the roadmap. See [verification and remaining checks](../tools/esc1-poc/evidence/analysis-workspace/README.md) before treating this as ready to merge. This develops [TODO 3](../tools/esc1-poc/TODO.md#3-build-an-interactive-analysis-workspace-using-bloodhounds-sequential-layout).

## Purpose and boundaries

Give extension authors one place to follow calculations, inspect answers, understand failures, test changes, and manage their analysis files. Use BloodHound's familiar sequential graph display, table, details panel, navigation, menus, styling, login, and permissions where they fit. The Ghidra/IDA comparison is superseded by this design.

**Query nodes, dependency arrows, and collapsed groups are display information only. Never store them in BloodHound's collected-data graph.** Build the diagram from the selected rule revision and execution plan. Attach execution information from the selected run. Actual relationships produced by a successful analysis still belong in BloodHound's normal graph and can be opened in Explore.

Keep ordinary Explore behavior and existing API contracts intact. Reuse useful display components without teaching normal graph-object lookup or attack-path search to interpret query identifiers. No new schema or Cypher declarations are required by this workspace.

Schema uploads remain on OpenGraph Management. The native workspace now provides analysis uploads, draft review/activation, individual and multiple-rule enable/disable/delete, and Run Analysis. The older rules page remains available. Reuse [TODO 6's extension association and version checks](opengraph-analysis-extension-integration.md); do not duplicate its storage or validation rules. Moving analysis records into BloodHound's application database is a separate decision, not a prerequisite for this page.

## Initial MVP: understand current rules and completed runs

The following records the first delivery boundary. The user subsequently requested the management and layout changes described above; this does not authorize the later testing/history phases.

The first useful release answers: **How do these rules fit together, and what happened when they ran?** It is a read-only view of the current activated rules. Completing this MVP does not complete all of TODO 3.

| Include in the MVP | Keep for later phases |
| --- | --- |
| Current activated rules in the sequential graph, with complete execution dependencies and shared prerequisites shown once | Draft execution diagrams and retained historical diagrams |
| Pan, zoom, fit, search, extension filtering, and keyboard-accessible navigation | Focused chain-only views, collapsed chains, and extension groups |
| One focused query shared by the graph, rule table, and details panel; highlight prerequisites and downstream consumers | Multiple action selection, bulk actions, and specialized table scopes |
| Existing query properties, full Cypher, individual download, and clickable prerequisite/consumer details | Intermediate answers and retained inspection data |
| Enabled/disabled choice, blocking explanations, and existing completed-run counts, durations, and failure reasons | Genuine live per-query progress and new progress reporting |
| Refresh and links to the current rules page for uploads, review, activation, enable/disable/delete, and Run Analysis | Moving those controls into the workspace |
| Existing login and read permissions, clear report revision/time, and honest missing or unavailable states | Isolated tests and proposed relationship comparisons |

For the MVP, the table lists current rules under the same search and extension filters as the graph, and marks the focused row. Selecting a row or node updates both views and the panel. Related highlighting does not select additional rows for actions. Filtered-out dependencies must still be explained with a way to reveal them; a simple count and Clear filters action is sufficient. No collapse or bulk-selection machinery is needed yet.

Show the latest completed attempt's per-query outcomes on the diagram only when its revision matches the current plan. If it belongs to an older revision, show that fact and its existing report separately; do not attach its outcomes to today's queries or reconstruct an old diagram. Identify the last confirmed saved revision separately from calculation success. Any existing overall running status stays at run level with its update time; do not infer live query states from dependency order or a completed report. Missing reports and no activated rules have explicit empty/unavailable views, with a link to the current rules page.

MVP work is mainly the new interface, authenticated read access to current plan/query/report data, and exposing complete dependencies from existing runner plan logic. Download returns the displayed revision, or reports that it changed and asks for refresh. It does not require new execution modes, intermediate-answer retention, historical-plan storage, a storage move, or additional DAWGS changes.

**MVP done when:** an author can open the current ESC1 or cross-extension plan, follow a query's prerequisites and consumers, inspect/download its file and properties, understand why it is disabled or blocked, and inspect the matching completed attempt's outcome or see why no matching report is available. Navigation writes no graph data, preserves existing permissions and Explore behavior, and provides a clear route to the existing management controls.

## Roadmap after the MVP

Deliver these in order, keeping the full feature set below:

1. **Rule management:** move uploads, draft review, activation, individual/bulk actions, and Run Analysis into the workspace. Include a usable invalid-draft view and explicit action revisions.
2. **Focused and collapsed views:** add chain-only views, extra table scopes, and nonoverlapping chain/extension groups with accurate hidden counts and connections.
3. **Execution inspection:** retain bounded historical plans and add genuine live progress in separate working changes. Historical diagrams remain read-only.
4. **Isolated testing and answers:** add repeatable test targets, whole-plan then selected-chain tests, complete proposed-change comparisons, and bounded intermediate-answer inspection.

The following sections describe the complete workspace. They retain the original TODO and review decisions; they are not additional MVP completion requirements. Their behavior applies as the corresponding phase is implemented.

## One workspace, three coordinated views

The graph explains connections; the rule table supports scanning and selecting many rules; the details panel explains one query or summarizes a multiple selection. Switching views preserves the chosen revision, run, filters, and selection. A split view may show the table beside or below the graph; narrow screens may switch between them.

Make the displayed context explicit: current rules, a proposed draft, or an inspected run. Historical runs are read-only; downloads use the displayed revision when its files were retained. Provide Open current rule before management actions, explaining if that rule changed or was removed. Draft controls explicitly target the saved draft. Changing revisions clears action selections and invalidates pending change reviews; switching graph/table views within one revision preserves them. Every action and review identifies its target revision, and the server rejects a stale target.

Incomplete or invalid drafts remain usable through the file table and details panel, with actionable validation explanations and upload, download, replacement, and discard controls. When no valid plan exists, explain why the graph is unavailable rather than inventing execution order. Test and Activate remain unavailable until their required checks pass. Keep existing active rules accessible.

| Interaction | Required behavior |
| --- | --- |
| Inspect a query | Clicking its graph node or table row makes it the focused query, highlights its row and connections, and opens its details. The panel can navigate to a prerequisite or consumer. |
| Highlight related queries | Use green for enabled, gray for disabled, red for a step that failed in the latest matching completed run, orange for blocked, and blue for the focused query. Fade everything outside the focused dependency path to gray. Selecting an arrow follows paths through that arrow, excluding side branches and shortcuts. Use BloodHound's default gray edges; identify prerequisites and consumers in words in the table. Highlighting is explanatory; it does not select those queries for enable, disable, or delete. |
| Select several queries | Use table checkboxes and an equivalent graph selection control. Show the count and a shared action toolbar. The focused query and the set chosen for actions are separate, visible concepts. |
| Restrict the table | Offer all rules, selected rules, prerequisites, downstream consumers, and the entire highlighted chain. These use the same dependency information as the graph. A table row identifies its relationship to the focused query. |
| Select all | Distinguish all filtered rules from all rules in the displayed revision, with counts. Include retained rules whose schema is missing, labeled with their former extension and blocking reason. Collapsing or filtering does not silently change action selections; show how many selected rules are hidden and provide Clear selection. |
| Focus a chain | Show the focused query and its prerequisites and consumers. Shared prerequisites appear once. Keep a visible way to return to the full plan. |
| Search and navigate | Reuse pan, zoom, fit, search, extension filters, and keyboard conventions. Search can reveal a match inside a collapsed group. Keep layout positions stable while progress changes. |

Keep query labels compact: name, namespace, and analysis version. Availability uses the agreed colors and remains written in the table and details. Put full Cypher and lengthy properties in the panel. Extension identity must distinguish same-named files. Do not invent another author-facing naming convention; use TODO 6's identity when available.

## Dependencies and collapsed groups

Lay out prerequisites before their consumers, following the actual execution plan. An arrow explains what the consumer needs, such as a supplied answer parameter. Dependencies through helper objects or calculated relationship types must also be explained when the runner waits for them; label these separately from directly supplied answers. Merely reading the same collected object type does not establish an execution dependency.

Supply one complete dependency list from the same plan logic the runner uses, with a reason for each connection. Use it for the graph, table, panel, and test selection. Today's plan view lists direct answer prerequisites but omits helper/relationship waits from those arrows; extend it before displaying the first diagram. Existing broad connected groups are not execution chains and must not be used to invent arrows.

Queries sharing an output may affect each other's availability without having an execution order between them; show that relationship in the explanation, not as an invented prerequisite arrow. When a query reads its own output type, explain the runner's use of previously stored graph data rather than drawing a dependency on itself.

Offer collapse/expand for chains and extension groups. A collapsed group shows its rule count, selected count, and a summary of failures, running work, disabled rules, and blocked rules. Preserve arrows entering and leaving the group, including cross-extension connections. A shared prerequisite remains one query, even when several chains use it. Collapsing a visual group changes neither the execution plan nor the action selection.

Use one nonoverlapping grouping arrangement at a time: extension groups or unbranched chain sections. Keep shared branch/join queries visible between collapsed chain sections. Each query contributes to exactly one visible node or group; arbitrary nested or overlapping custom groups are unnecessary. Group arrows may point both ways between extensions without implying a circular query dependency. Group controls offer Expand, Focus, and Select the contained rules with a count; bulk changes use the common selection toolbar.

Extension filters must not hide a prerequisite or affected consumer without explanation. Show a boundary indicator with the hidden count and a way to reveal those queries. Expanding a group or jumping to a failure should keep the user's place where possible. Query order and dependency depth describe dependencies; depth counts arrows along the longest prerequisite chain from 0. Positions are not a timeline or a promise that queries run simultaneously.

## Query details and rule actions

The query panel includes name, extension, available version/revision information, order, dependency depth, date added, enabled state and any blocking reason. Include full Cypher, graph inputs, supplied and received parameters, prerequisites and consumers, helper kinds and ID prefixes, relationship kinds, allowed endpoints, and existing-edge policy. Explain the effect of optional reachability, group-overlap, and batching instructions. Put query properties, answers, and run details in clearly named sections.

Provide Download, Test with prerequisites, Enable, Disable, and Delete where allowed by the displayed context. A query's right-click or visible menu always names and targets that one query. Use the shared toolbar for several selected rules instead of changing menu targets depending on a hidden selection. Keyboard users can reach every action.

The selection toolbar supports downloading, enabling, disabling, and deleting the explicitly selected rules, including an explicitly selected full set. Before a change, reuse the existing review to show requested changes and additional effects separately: dependent rules, other extensions, shared output producers, and pending relationship cleanup. Do not turn highlighting into permission to delete dependent files. Preserve the existing review and activation checks; identify any current deletion behavior that removes additional files in that review. This page does not introduce new deletion semantics.

During change review, highlight every affected rule in both graph and table, distinguishing requested changes from additional effects. Reveal affected collapsed groups and filtered extensions, with a way to restore the previous view. Impact highlights do not change the explicit action selection. Files without a valid graph position still appear in the review table.

Keep Upload analyses, Review changes, Activate, Refresh, and Run Analysis available in the workspace. Link to OpenGraph Management for schema tasks. Explain that enabling or activating files is separate from successfully applying their results. Run Analysis keeps its existing meaning: request ordinary BloodHound analysis followed by applicable extension processing. A Test action does not activate files or publish results.

## Show availability, execution, and saving separately

| Information | Examples and meaning |
| --- | --- |
| Rule availability | The author's Enabled/Disabled choice, plus eligible/blocked with a reason. A missing, disabled, or incompatible prerequisite, or a paused peer producing the same relationship type, may block an enabled rule. Link to the responsible rules. |
| Selected run | Idle when there is no current work; waiting, running, succeeded, failed, skipped, or canceled for a particular attempt. Include row counts, durations, and why it ran or was skipped when known. |
| Saved results | Last confirmed successful revision and run, unchanged results, confirmed failed save, uncertain save, or unavailable information. Query calculation success does not prove results were saved. |

Use short readable labels and symbols as well as colors. Keep the focused/selected outline separate from the execution indicator. A disabled rule may still have results from an earlier successful run; show both facts.

Clearly identify the proposed, activated, running, and last successfully applied revisions. Today's 20-attempt history retains reports and revision identifiers, not complete historical plans. Add one bounded retained plan per distinct revision, shared by its runs; reuse TODO 6's revision records where available. Retain query text only under rule-inspection permissions and treat answer retention separately. Cap total retained bytes as well as record count. An older report whose plan is unavailable remains readable as a report without a fabricated diagram. Never substitute today's files for missing historical contents. Expired or unavailable details are distinct from an empty answer.

The service currently reports overall running status and per-query outcomes after execution. Add bounded progress updates from the runner for genuine live query states. Begin with ordinary periodic status reads; add a more elaborate delivery method only if measurement shows it is needed. Include run identity and update time so delayed reports cannot overwrite a newer run. If updates stop, label progress as stale/unavailable rather than claiming that a query is still running. Reporting failure must not repeat a successful graph save.

Give progress delivery and optional answer retention separate bounded allowances. A failed destination or exhausted inspection allowance marks those details unavailable or incomplete; it must not fail, repeat, or indefinitely delay ordinary analysis. Keep required calculation limits and validation failures unchanged. The existing correctness-test answer observer can fail a run and must not become the ordinary display-capture path. Written test expectations may still fail a test.

## Debugging and safe tests

Selecting a failure opens the responsible query, its phase and useful explanation, available inputs and intermediate answers, and the queries that could not continue. A failure during shared setup or saving belongs to the run, not an arbitrarily blamed query. Provide Jump to failure and navigation back to the surrounding chain. Keep ordinary errors and reports free of secrets, complete queries, and raw collected identifiers; full query text and answer data belong in authorized inspection controls.

Use the production calculation and validation code for tests. Start with isolated test databases and small collection files plus written expected results, so extension authors need not write Go tests. The workspace records the rule revision, selected database, test data, requested queries, and actual included prerequisites for each attempt. Later use of current application data requires a separately verified consistent database view or isolated copy; it must not change the live graph or prolong its write pause while an author inspects answers.

Use server-configured, dedicated test targets and named datasets. Browser requests choose those names, never connection URLs or credentials. Reject the live database as a test target. Each attempt starts from a recorded baseline or an explicitly named continuing collection sequence; record the engine/backend versions and dataset state. Define reset, failed-attempt cleanup, and one-at-a-time ownership of each target before exposing Test. Use the existing lab setup, with easier startup owned by TODO 5, rather than building database provisioning into BloodHound.

Testing a selected query or chain includes every required prerequisite. A downstream query may also need another branch that was not highlighted from the original focus. When proposed removals or updates depend on shared relationship producers, include their complete required calculations or label that comparison unavailable. Never interpret the answer from one selected producer as the complete replacement for all producers.

Run tests in a mode that cannot publish to the live graph, including helper creation, calculated relationships used by later queries, index/kind preparation, and final writes. Preserve ordinary calculation protections and explain the test's database state. Skipping a final save alone is not adequate isolation. Review additions, removals, and property changes against the stated test baseline; do not describe them as current production changes.

Intermediate answers can be large. Reuse the runner's existing local work directory and answer-file handling for a small bounded inspection area outside each temporary run directory, keeping retained inspection data separate from files needed to complete calculations. Ordinary run cleanup must not remove retained inspection files. Set total byte limits and an expiry independent of report history, and clean up expired data. Do not introduce another storage service. Author inspection must not keep execution or a database transaction open. Show a limited display and its known complete count separately; label incomplete calculations and unavailable counts accurately. Paging or a sample must never become input to a complete replacement calculation. Inspect the effective answers consumed by later queries, including optional advanced processing, rather than displaying raw seed rows as the final answer.

Support written cases for empty answers, duplicate support, removal of one or the last support, property updates, failure preserving previous results, and collectors arriving in either order. Run the same cases on Neo4j and PostgreSQL. Testing a draft must not change its activation state.

## Keep implementation changes concentrated

| Area | Intended work |
| --- | --- |
| BloodHound interface | MVP: sequential graph, query table/details, single-query focus, existing properties and completed reports. Later: multiple selection, grouping, management, and testing controls. Normal Explore continues to describe collected objects and their relationships. |
| BloodHound server | MVP: authenticated read access for current plans, query details/downloads, and existing reports. Later: management, test requests, and answer inspection. Keep the sidecar secret on the server and use fixed destinations. Document and test each new route. |
| Existing runner inside BloodHound | MVP: expose complete dependencies from existing plan logic; preserve execution. Later: bounded live progress, answer capture, selected test calculation, and reliable isolation. Code under `tools/esc1-poc` can still be compiled into BloodHound and require a rebuild. |
| Sidecar | MVP: reuse current rule files and reports. Later: add bounded retained plans and reuse existing management checks. Keep sensitive answer inspection separate from ordinary status reports. |
| DAWGS | No additional change is expected for the interface or progress features. Require a demonstrated missing capability before proposing a database-library change for test isolation. Existing DAWGS corrections remain dependencies of the runner. |

Reuse the existing graph layout and drawing where practical. Do not couple the analysis page to Explore's collected-object lookup, pathfinding actions, or selected-item browser state. If extracting a small shared display piece is clearer than adding analysis-specific branches to Explore, do that and test existing Explore behavior. Do not replace the graph drawing library merely to obtain rectangular query cards.

Use existing BloodHound permissions for the corresponding operations. Inspection of collected values must respect access to that data, beyond permission to read rule definitions. Executing tests must require the appropriate execution permission even though it does not publish. Include these checks with the first reachable route; do not add them after exposing the feature.

## Small working changes

Each change includes its focused tests, generated files where needed, and documentation. Finish and assess the MVP before starting the later phases; those phases remain part of the complete TODO 3.

### MVP delivery

1. **Read the current plan safely.** Expose complete runner-derived dependencies and authenticated read access to current plan/query/report data. Cover helper/relationship waits, shared prerequisites, same-named rules, rejected reads, and changes to the active revision during a request. Do not introduce execution or storage features.
2. **Inspect and navigate.** Add the sequential graph, basic rule table, details panel, single-query focus, directional highlights, individual download, search, filters, and keyboard controls. Link to current management. Cover disconnected queries, filtered dependencies, empty state, revision changes during download, and unchanged Explore behavior. Prove that page use creates no graph objects.
3. **Explain completed outcomes.** Add existing completed-report counts, timings, failure explanations, and saving states. Cover matching and mismatched revisions, unavailable reports, confirmed older results, and honest overall running status. Check the ESC1 and cross-extension inspection workflows and update usage documentation. The MVP is complete at this point.

### Later delivery

1. **Move analysis management.** Reuse uploads, drafts, review, activation, enable/disable/delete, bulk downloads, and Run Analysis. Include explicit multiple selection, invalid-draft handling, full graph/table impact review, exact revision targets, stale-review rejection, and permission failures. Retire the current page only after its replacement covers the existing workflow.
2. **Navigate larger plans.** Add chain-only focus, extra table scopes, collapsed chains/groups, hidden selection counts, and reveal/restore behavior. Test shared junctions, cross-extension edges, hidden failures, and keyboard navigation.
3. **Retain historical plans, then report live progress.** Make these two separate changes. First add bounded retained plans and read-only historical inspection. Then add bounded runner updates and status reads, checking delayed/missing/out-of-order updates and independence from calculation and saving.
4. **Run isolated whole-plan tests.** Establish trusted targets, repeatable baselines, cleanup, small collections and written expected answers, including helper/preparation behavior and failure preservation on both databases. Expose Test only after isolation is proved.
5. **Test selected chains.** Add complete required-prerequisite selection and shared-output comparisons. Show requested versus included queries and test hidden branches, shared producers, removals, and property updates.
6. **Inspect retained answers.** Add bounded capture and authorized inspection without holding execution open. Check capture failures, missing/expired data, and labels for limited displays and complete calculations.
7. **Finish the complete author workflow.** Validate upload-to-test-to-review-to-activation and update the guides. Check usability with many rules under current limits; record observations rather than promising a capacity number.

For BloodHound implementation, run focused tests and `just test`. Run `just prepare-for-codereview` before the user's manual commit and report what it actually checked. Any justified DAWGS edit also requires `make format` and both database tests when query meaning changes. No agent stages or commits.

## Full TODO 3 completion

These are the final roadmap checks. The smaller MVP has its own completion criterion above; historical retention, management, collapse, live progress, and testing do not block that first delivery.

- An author can move between graph, table, and details without losing context or confusing related highlights with rules selected for modification.
- Invalid drafts and retained rules with missing schemas remain inspectable. A same-named query changed since a historical run cannot be modified through that run's view.
- A collapsed or filtered view still reveals failures, hidden selections, and dependency effects in other extensions.
- Every original TODO 3 property, action, revision distinction, testing behavior, and accessibility requirement remains available.
- Inspection, management, tests, and normal execution enforce the same validation and access rules, including rejected actions, interrupted execution, resource limits, and stale review data.
- Missing progress or optional inspection data cannot prevent an ordinary calculation/save, and cannot be mistaken for an empty answer. New historical diagrams use their retained plans; older incomplete reports remain identifiable.
- Test results state their source revision/database state and display completeness. Shared answers, previously saved results, and the live database remain correct after any failed test or update.
- The familiar interface contains only a view of the execution plan; query nodes and arrows never become collected graph data. Existing BloodHound features remain usable.

Useful starting points: [current rule page](../tools/esc1-poc/internal/ruleservice/upload.html), [plan details](../tools/esc1-poc/internal/ruleservice/view.go), [change review](../tools/esc1-poc/internal/ruleservice/changes.go), [runner](../tools/esc1-poc/internal/runner), [execution reports](../tools/esc1-poc/internal/runner/report.go), [service](../tools/esc1-poc/postprocessing/service.go), [sequential layout](../cmd/ui/src/views/Explore/utils.ts), [graph drawing](../cmd/ui/src/components/SigmaChart/SigmaChart.tsx), and [current highlighting](../cmd/ui/src/components/SigmaChart/utils.tsx).

## Create analysis file — discussion in progress, 14 October 2026

The user requested an editor for building an extension one step at a time in BloodHound and explicitly asked to reach a shared understanding before implementation. The complete workflow was approved in Q23. Implementation is now authorized; preserve the exact Q&A below.

Agreed behavior:

- A Create analysis file button opens an editor with query execution and result display.
- Existing files have an Edit analysis file action using the same editor, dependency selection, and test controls.
- The author selects existing query nodes to connect dependencies. The dialog shows a dependency list and arrow preview, adds declarations automatically, and offers editable parameter names, such as `$usable_cas`.
- Testing offers reuse of saved dependency answers or recalculation. Changes to the selected dependencies or collected data require recalculation before testing.
- Normal-run answer reuse requires a successful whole run with matching files and data. Successful individual steps from a failed run are not reusable.
- A complete successful editor test of the needed prerequisite chain can also supply reusable answers when its files and collected data still match.
- The editor chooses an installed extension and links to schema management. Creating or editing schemas inside this dialog is outside the initial scope.
- Execute shows results without publishing graph changes.
- Recalculation runs the needed prerequisite chain, not the entire analysis. Freshly calculated prerequisite graph outputs can be used temporarily; all test graph changes are discarded afterward.
- Results include a table and a graph when returned values identify objects or relationships.
- The result graph shows only objects and relationships identified by the results; it does not add existing connections that the query did not return.
- Existing identifier-based result formats are recognized for graph display. Unfamiliar result tables can ask the author which columns identify objects; usable_cas can show matching CA nodes from its existing identifiers without query changes.
- Testing is allowed before proposed output types have schema declarations. Activation requires valid declarations.
- Save shows a review, then activates the file. Testing and activation remain separate from normal analysis saving graph results.
- Saving activates only the editor's file and preserves unrelated pending uploads separately.
- Activation from the editor requires a successful test of the exact file contents being saved, plus ordinary schema and dependency validation.
- A separate Save working copy action preserves unfinished work for resuming later without activation.

The complete workflow below was approved in Q23. Architecture review then found an operating constraint: the current write gate cannot track direct database writers on either backend. Q26 asks the user to resolve that condition; reuse/freshness integration must wait for the answer. Independent working-copy, selection, and rollback work can proceed. The existing answer files are deleted after calculation; retained answers and trustworthy data-version checks require new work.

### Complete workflow for review

1. Open Create analysis file, or Edit analysis file from an existing file's entity panel. Choose an installed extension, filename, and version. The editor supplies the existing required file headers; editing preserves the current metadata until the author changes it. Schema management remains a separate linked page.
2. Select prerequisite query nodes. A list and preview arrows show their connections to the file. Declarations are added automatically, with editable parameter names. Removing a selection removes that generated declaration. Typed declaration changes must remain visible in the dependency view; incomplete or conflicting declarations show a problem rather than silently changing the code.
3. Choose Reuse answers or Recalculate. Reuse identifies the successful normal run or complete successful editor-test chain supplying the answers. Its relevant files, schemas, and collected data must still match. Missing, expired, incomplete, oversized, or unverifiably fresh answers make reuse unavailable and explain which prerequisites need recalculation. Never feed a shortened table preview to a downstream query as if it were a complete answer.
4. Execute the current file with only its needed prerequisite chain. Show the actual included prerequisites before execution, including any extra producers needed for a shared graph output. Tests can use newly calculated prerequisite graph outputs temporarily. Completion, failure, or cancellation leaves no saved test graph changes. Author inspection starts after the calculation ends, so inspecting results does not hold the calculation open.
5. Show returned rows in the table and identified objects/relationships in the results graph. Recognize existing identifier-based formats and offer column selection for unfamiliar ones. Only result-identified connections appear. Newly proposed objects or relationships are marked as test outputs, not saved graph data. Show the source run/test and whether the display is limited; calculation and reuse still require complete answers.
6. Save working copy keeps unfinished code and selections for later, without activation or overwriting the unrelated upload draft.
7. Save and activate requires a successful test of the exact current file and declarations, then ordinary schema/dependency validation and explicit review. Only that file is activated; unrelated pending uploads remain separate. File or dependency changes invalidate the previous test. The proposed rule is to require a fresh test if relevant schemas or collected data changed before activation; this is part of the final review, not a previously answered standalone question.
8. Normal Run Analysis remains the action that saves calculated graph results. Saving/activating an editor file does not run that calculation automatically.

Proposed supporting behavior for final review: provide cancellation and the existing calculation limits; retain the working copy on test failure; reject stale activation review rather than overwriting newer files; retain only complete reusable answers within documented storage limits, otherwise offer recalculation; prevent reuse if unchanged data cannot be established. Exact limits and how freshness is established must be measured and explained in the implementation plan. No new schema/Cypher author declarations are proposed. The successful-test requirement applies to this requested editor activation workflow; changing ordinary upload requirements would need separate approval.

Separately completed: the user confirmed the ordinary Cypher example should coexist with the advanced example. The local test app now has 13 ordinary files under `PoC_Cypher` and distinct output kinds, preserving the previous files and choices. [Data verification](../tools/esc1-poc/evidence/analysis-workspace-ordinary-example/README.md) records that addition. It is not an implementation of this editor.

> **Updated 15 October 2026 at the user's request** (this section is kept as the 14 October agreement): prerequisites are chosen in a filterable dropdown, and aliases are edited only in the Cypher text. Testing is the play button (**Test file**), which runs the prerequisite check itself. Results are a table of rows only, shown between the Cypher box and Dependencies; the result graph, identifier-column selection, and test-output marking were removed, and the server no longer looks up objects from rows. The private copy action is **Save private copy**, and a **Revert** button was added. Activation keeps the two steps **Review tested file** then **Activate reviewed file**. One left-to-right **Dependency graph** window replaced the dependency list and arrow previews. Each check warns, as advice only, about declarations the file never uses. See `tools/esc1-poc/DESIGN.md` (Analysis file editor) and `tools/esc1-poc/HANDOFF.md`.

## Full editor Q&A log — 14 October 2026

This records every question presented through the question tool in this editor discussion, all offered choices, and the user's exact answers. It includes the related ordinary-example decision. Round numbers show conversation order; individual timestamps are not inferred. Later answers and corrections must be appended here. The decisions above summarize this log; a pending question is not an agreement.

### Original user request

> Add the ordinary Cypher example to the data so I can test with it.
>
> I also want there to be a "Create analysis file" button as well that allows me to test a new .cypher by connecting it to its dependencies in the Analysis graph and making the most recent run's data available to it. This would allow OpenGraph developers to build their extension's logic in BloodHound one step at a time. In the creation dialog I should be able to execute the cypher I'm writing and display the results. Grill me on how this should work until we reach a shared understanding before implementing.

### Round 1 — example, dependency answers, and graph saving

**Q1.** Should the ordinary Cypher example appear alongside the advanced ESC1 example, or replace it? Both currently use the same namespace and filenames.

Offered choices:

- Add alongside under a separate namespace (Recommended)
- Replace the advanced ESC1 files

**User answer:**

> Add alongside under a separate namespace (Recommended)

**Q2.** When testing a new analysis file, what should happen with its selected dependencies? The app currently removes their returned rows after a run.

Offered choices:

- Reuse answers from their latest successful run (Recommended)
- Recalculate the selected dependencies before each test
- Let me choose reuse or recalculate

**User answer:**

> Let me choose reuse or recalculate

**Q3.** Should Execute in the creation dialog only show returned rows, or also save the file’s proposed objects and relationships to the graph?

Offered choices:

- Show results; save graph changes through normal analysis (Recommended)
- Allow an explicit separate action to save tested results

**User answer:**

> Show results; save graph changes through normal analysis (Recommended)

### Round 2 — declarations, stale answers, and activation

**Q4.** When you connect a dependency in the graph, should the dialog add its dependency declaration to the file and offer a parameter name such as $usable_cas?

Offered choices:

- Add the declaration automatically; let me rename the parameter (Recommended)
- Require me to write the declaration myself

**User answer:**

> Add the declaration automatically; let me rename the parameter (Recommended)

**Q5.** If a selected dependency or collected data has changed since its saved answers were produced, should the dialog still allow reuse?

Offered choices:

- Require recalculation before testing (Recommended)
- Allow reuse with a clear warning and run date

**User answer:**

> Require recalculation before testing (Recommended)

**Q6.** After testing, what should Save analysis file do?

Offered choices:

- Save an inactive draft for normal review and activation (Recommended)
- Activate it immediately after showing a review

**User answer:**

> Activate it immediately after showing a review

### Round 3 — test scope and results

**Q7.** When you choose Recalculate, should testing run only the selected dependencies and their prerequisites, or the entire analysis?

Offered choices:

- Run only the needed prerequisite chain (Recommended)
- Run the entire analysis

**User answer:**

> Run only the needed prerequisite chain (Recommended)

**Q8.** If a prerequisite creates graph objects or relationships, should the test be able to use its fresh outputs temporarily, then discard all graph changes afterward?

Offered choices:

- Use temporary outputs and discard them after the test (Recommended)
- Require those outputs to be saved by normal analysis first

**User answer:**

> Use temporary outputs and discard them after the test (Recommended)

**Q9.** How should the creation dialog display query results?

Offered choices:

- Table, plus graph view when results identify objects or relationships (Recommended)
- Table only initially

**User answer:**

> Table, plus graph view when results identify objects or relationships (Recommended)

### Round 4 — selecting dependencies, completed runs, and undeclared outputs

**Q10.** How should you select prerequisites while creating a file? I recommend selecting existing query nodes, listing them in the dialog, and previewing their arrows to the new node.

Offered choices:

- Select query nodes; show a dependency list and arrow preview (Recommended)
- Drag connections between nodes

**User answer:**

> Select query nodes; show a dependency list and arrow preview (Recommended)

**Q11.** Which completed runs should provide reusable answers? A failed run may contain successful individual steps, but its graph changes were not saved.

Offered choices:

- Only a successful whole run, with matching files and data (Recommended)
- Also allow successful steps from a failed run, clearly identified

**User answer:**

> Only a successful whole run, with matching files and data (Recommended)

**Q12.** Should you be able to test a file before its proposed object or relationship types are declared in the extension’s schema?

Offered choices:

- Allow testing; require valid schema declarations before activation (Recommended)
- Require valid schema declarations before testing too

**User answer:**

> Allow testing; require valid schema declarations before activation (Recommended)

### Round 5 — editor-test answers, schema management, and result connections

**Q13.** Should a successful editor test of a complete prerequisite chain provide reusable answers for the next file, provided its files and collected data still match?

Offered choices:

- Yes, complete successful editor tests count too (Recommended)
- No, reuse only answers from normal whole-analysis runs

**User answer:**

> Yes, complete successful editor tests count too (Recommended)

**Q14.** Should Create analysis file initially work within an installed extension, or also let you create and edit extension schemas in the same dialog?

Offered choices:

- Choose an installed extension; link to schema management (Recommended)
- Include extension and schema creation/editing here too

**User answer:**

> Choose an installed extension; link to schema management (Recommended)

**Q15.** What should appear in the result graph? Adding existing connections can show relationships the query did not return.

Offered choices:

- Only the objects and relationships identified by the results (Recommended)
- Also show existing connections between returned objects

**User answer:**

> Only the objects and relationships identified by the results (Recommended)

### Round 6 — editing, pending uploads, and identifier columns

**Q16.** Should existing files have an Edit analysis file action that opens the same editor, dependency selection, and test controls?

Offered choices:

- Support creating and editing files in the same editor (Recommended)
- Start with creating new files only

**User answer:**

> Support creating and editing files in the same editor (Recommended)

**Q17.** If uploads are already waiting in an inactive draft, what should saving from the editor activate?

Offered choices:

- Only the editor’s file; preserve the pending uploads separately (Recommended)
- Combine the editor’s file and pending uploads into one review

**User answer:**

> Only the editor’s file; preserve the pending uploads separately (Recommended)

**Q18.** For results such as usable_cas that return object identifiers, how should the graph view identify which objects to show?

Offered choices:

- Recognize existing result formats; offer column selection for other identifiers (Recommended)
- Show a graph only when Cypher returns whole objects, relationships, or paths

**User answer:**

> I don't understand this question. Please reframe it. Also make sure you're capturing a full log of this Q&A in the docs.

### Round 7 — clarification of identifier columns

Assistant explanation:

> usable_cas returns rows containing identifiers for matching certificate authorities. To draw those authorities, the page needs to know which column identifies each one. Another query might call that column something different.

**Q19.** Suppose your result table has columns called “Certificate authority,” “Domain,” and “Count.” To draw a graph, BloodHound needs to know which columns refer to graph objects. Should it recognize familiar columns automatically and let you choose the columns for unfamiliar results, or should you write the query to return the actual graph objects instead?

Offered choices:

- Recognize familiar results; let me choose object columns when needed (Recommended)
- Require the query to return actual graph objects for graph display

**User answer:**

> I still don't understand. What would the graph display if I choose #1?

### Round 8 — concrete graph display

Assistant explanation:

> Option 1 would show the **collected objects identified by your result rows** in a separate results graph. The analysis graph would continue to show files and dependencies.
>
> For example, if a row identifies certificate authority **CA01**, domain **LAB.LOCAL**, and a count of **3**, the results graph would show the CA and domain as two nodes. An edge between them would appear only if the results also identify that relationship. The count would stay in the table.
>
> For usable_cas, this would simply show the matching certificate-authority nodes. Choosing a column tells BloodHound which objects to draw; it doesn’t change the query.

**Q20.** For usable_cas, would you like the results graph to show its matching certificate-authority nodes directly from the identifiers it already returns, without rewriting that query? Other unfamiliar result tables could ask you which column identifies the objects.

Offered choices:

- Yes, show matching objects from returned identifiers (Recommended)
- Only draw objects when the query returns the whole objects

**User answer:**

> Yes, show matching objects from returned identifiers (Recommended)

### Round 9 — activation test requirement and unfinished work

**Q21.** Should activation from the editor require a successful test of the exact file contents being saved? Schema and dependency validation would always be required.

Offered choices:

- Require a successful test before activation (Recommended)
- Allow untested or failed files to activate; clearly show test status

**User answer:**

> Require a successful test before activation (Recommended)

**Q22.** Should there also be a Save working copy action for unfinished queries, separate from Save and activate, so you can close the editor and resume later?

Offered choices:

- Keep a resumable working copy without activation (Recommended)
- Warn before closing; otherwise discard unfinished changes

**User answer:**

> Keep a resumable working copy without activation (Recommended)

### Round 10 — complete workflow review

Assistant's review summary:

> The complete workflow is ready for your review:
>
> 1. Create or edit a file, choose its extension, and connect prerequisites.
> 2. Reuse matching complete answers or recalculate the needed chain.
> 3. Execute, then inspect the result table and graph. Test graph changes are discarded.
> 4. Save unfinished work separately, or review and activate a file that passed its current test.
> 5. Use normal Run Analysis to save graph results.
>
> I also propose requiring a fresh test if relevant files, schemas, or collected data change before activation. Missing or incomplete saved answers would make reuse unavailable and explain what needs recalculation.

**Q23.** Does the complete workflow match what you want me to build: create/edit files, select dependencies, reuse matching complete answers or recalculate the needed chain, inspect table/graph results without saved test changes, keep unfinished working copies, and review/activate only a successfully tested file? This also requires a fresh test if relevant files, schemas, or collected data change before activation; missing or incomplete saved answers require recalculation. The detailed workflow and full Q&A are recorded in _plans/opengraph-analysis-workspace.md.

Offered choices:

- Yes, proceed with this workflow
- Adjust the workflow before implementation

**User answer:**

> Yes, proceed with this workflow

### Architecture model selection

The repository instructions specify Astra for architecture, but that model is not available for delegation in this session.

**Q24.** Your AGENTS.md specifies Astra for architecture, but Astra is not available among this session’s agent models. Which available model should handle that review? I can continue inspecting the existing code while waiting.

Offered choices:

- Use gpt-6.1-sol for architecture
- Use gpt-5.6-terra for architecture

**User answer:**

> Use gpt-6.1-sol for architecture

### Additional display instructions

The user added the following instruction while implementation was beginning:

> Also:
>
> I can't see the node or edge text in dark mode.
>
> Instead of using environment_kind icons, number the extensions in the Filter dropdown and put the number on each node icon for that extension.

This supersedes the earlier environment-icon choice for the analysis graph. The extension filter and graph must share the same numbering. Ordinary Explore is unaffected.

### Round 11 — graph-write coordination

Architecture explanation:

> BloodHound can pause and track writes made through the app, but it cannot reliably track a script or console writing directly to Neo4j. Strict answer reuse therefore needs either a workspace where BloodHound controls graph writes or a separate isolated data copy.

**Q25.** For this developer workspace, can we require all graph writes to go through BloodHound while testing and reusing answers? Reading directly from Neo4j is fine. Supporting direct database writes at the same time would require a separate isolated data copy to meet the unchanged-data guarantee you chose.

Offered choices:

- Yes, BloodHound controls graph writes in this workspace (Recommended)
- Direct database writes must also be supported

**User answer:**

> Explain more about this decision -- does it impact PostgreSQL too?

### Round 12 — clarification for both databases

Assistant explanation:

> Yes—this affects **PostgreSQL too**.
>
> BloodHound can pause and track graph writes made through the app: uploads, normal analysis, and saved extension results. When those change the data, old answers become unavailable for reuse.
>
> A script writing directly to PostgreSQL, or a query writing directly through the Neo4j console, bypasses that tracking. That matters between tests too: a direct edit after one successful test could leave its saved answers stale without BloodHound knowing.
>
> Direct **reads** are fine on either database. The editor’s tests would also remain read-only from your perspective: their temporary graph changes are discarded.
>
> The decision is whether this workspace can use BloodHound as its only graph writer, or whether it must support direct database writes as well. The latter needs stronger coordination or an isolated data copy; our current setup doesn’t provide that.

**Q26.** With that distinction, can the first version require all graph writes on either PostgreSQL or Neo4j to go through BloodHound, including between tests, while allowing direct database reads? This lets the app invalidate saved answers and test results whenever graph data changes.

Offered choices:

- Yes, use BloodHound as the graph writer on both databases (Recommended)
- No, direct graph writes must work with this feature too

**User answer:**

> What has to be written to the BloodHound database for this?

### Round 13 — what is written and where

Assistant explanation:

> The rule doesn’t require adding a new record to the BloodHound database. It means making graph changes **through BloodHound’s uploads, APIs, and analysis actions**, so the app knows when the data changes.
>
> For this feature:
>
> - Execute may temporarily create prerequisite objects or relationships. Those graph changes are discarded when the test ends.
> - Run Analysis saves calculated graph results, as it does now.
> - Working copies and retained answers would live in private files under the existing postprocessing storage directories—not as graph nodes or relationships.
> - A server-side data revision would invalidate old answers after graph writes. A restart would also require fresh tests.
>
> Installed types or indexes may need preparation before a first test; those are database metadata, separate from the test graph results.
>
> The unresolved choice is simply whether scripts and consoles need to write directly to PostgreSQL or Neo4j while this workspace is in use.

The operating boundary was pending at this point. Writing or saving Cypher source is not itself a graph-data write. Graph-data writes mean creating, changing, or deleting stored objects, connections, or properties. Direct read-only MATCH/RETURN queries do not violate the proposed boundary.

### Round 14 — graph writer condition approved

**User instruction:**

> All graph database writes should happen through BloodHound

This approves BloodHound as the graph writer on both PostgreSQL and Neo4j, including between tests. Direct database reads remain allowed. Freshness is tracked by the single application process, and restarting it invalidates older retained answers and successful-test receipts. It does not claim to detect writes made outside BloodHound or coordinate multiple application instances.

## Approved editor implementation details — 14 October 2026

The later approved Create/Edit discussion supersedes the earlier separate-test-target proposal for this developer workspace. Tests borrow BloodHound's graph driver while its single-process write gate pauses other graph writers. Prerequisite objects, relationships, and property changes live in one transaction and are discarded before the author inspects detached results. The final candidate's outputs are validated and previewed without publication. PostgreSQL rollback and Neo4j transaction close must confirm discard; an uncertain discard stops graph writes and cannot create a successful-test receipt.

As explained in Round 13 before the user's graph-writer approval, installed kinds and endpoint indexes may be prepared before that transaction. These are persistent database metadata. Test-created graph objects, connections, properties, and PoC_Run records are discarded. This is the current metadata boundary; the earlier blanket isolation statement about kind/index preparation does not describe this implementation. No new extension-author declarations or schema requirements are introduced.

A successful-test receipt identifies the owner, exact file text, displayed rule/schema revisions, prerequisite files, and the application's graph revision. Receipts expire after 30 minutes; activation also requires the exact review revision. A graph write or restart invalidates old work. Schema/file changes are checked again before review and activation. Activation changes only the editor file, preserving the existing upload draft, and does not run analysis.

Complete answers are private files, distinct from the detached table preview. One complete successful analysis run or complete successful editor prerequisite chain becomes a source only after publication or discard is confirmed. A partial incremental run, failure, cancellation, missing answer, or archive failure cannot become a complete source. Reuse checks one source's entire requested prerequisite closure, exact file contents, installed schemas, graph revision, checksums, and current limits. An edited final candidate is deliberately excluded from prerequisite matching.

Retained answer sources expire after 24 hours. Keep at most four complete sources with 256 MiB of answer data combined; a single staged capture may use another 256 MiB. Source metadata is bounded separately to 4 MiB per source. Startup removes owned incomplete staging/corrupt sources, and expiry/eviction removes complete cache sources; preserved historical evidence folders are unrelated and remain untouched. Capture failures are optional for normal analysis: they cannot turn a confirmed graph save into a failed run.

Editor calculations use one worker, at most 1,000,000 result rows, 512 MiB temporary answer data, 100,000 changed objects, two minutes per query, and five minutes for the full chain. These are conservative limits rather than measured capacity promises. The detached display holds at most 500 rows and 1 MiB of table data. Complete cached answers, never that limited display, supply downstream calculations. Only result-identified objects and relationships appear in the graph; proposed final helpers/connections are marked as test outputs. Identifier recognition uses familiar graph/object identifier columns, with author-selected columns for unfamiliar tables.

The test/source/review/activation endpoints require an authenticated user with OpenGraph read/write permission and the existing extension-management feature. Tests and sources also require graph read permission. Users restricted to specific environments cannot use arbitrary scalar-table testing, because aggregate values cannot safely be filtered afterward; existing Explore and working-copy access rules continue as before. The browser cannot select a database URL, private answer path, owner, or graph revision.

> **Updated 15 October 2026 at the user's request** (this section is kept as the 14 October agreement): prerequisites are chosen in a filterable dropdown, and aliases are edited only in the Cypher text. Testing is the play button (**Test file**), which runs the prerequisite check itself. Results are a table of rows only, shown between the Cypher box and Dependencies; the result graph, identifier-column selection, and test-output marking were removed, and the server no longer looks up objects from rows. The private copy action is **Save private copy**, and a **Revert** button was added. Activation keeps the two steps **Review tested file** then **Activate reviewed file**. One left-to-right **Dependency graph** window replaced the dependency list and arrow previews. Each check warns, as advice only, about declarations the file never uses. See `tools/esc1-poc/DESIGN.md` (Analysis file editor) and `tools/esc1-poc/HANDOFF.md`.

### Round 15 — standalone intermediate files

**Q27.** The existing loader refuses to activate a file that only returns rows until another file uses those rows. For building an extension one step at a time, should these files be allowed to activate before they have a consumer? They would still need a successful test and review.

Offered choices:

- Allow activation before a consumer exists (Recommended)
- Keep the existing requirement for a consumer

**User answer:** Allow activation before a consumer exists (Recommended)

This explicitly approves removing the loader's unused-query rejection. Ordinary read-only, declaration, version, cycle and output validation remain required. An intermediate file can be active and run/retain answers before another file consumes them. Editor activation still requires its successful current test and review. No new author declaration is required.

### Reuse correctness follow-up

Review found that internal database IDs may refer to temporary helper nodes discarded after a test, or relationships removed/recreated during publication. The first implementation therefore requires recalculation when any needed cached prerequisite calls parsed `id()` anywhere, or uses reachability/group-overlap processing that handles internal IDs. It checks the complete prerequisite closure, so aliases and nested values cannot hide such references. These queries remain supported when recalculated; this adds no author declaration. Stable objectid/scalar chains remain reusable, including successful complete editor tests. The preview identifies prerequisites needing recalculation before Execute. More precise tracking/remapping of internal-ID origins is a useful future improvement; it must not silently treat arbitrary integers as graph IDs.

Additional architecture review: gpt-6.1-sol, replacing unavailable Astra as approved in Q24, reviewed only transaction closure, freshness, receipt activation, storage bounds, and cache identity correctness. Its bounded read-only role was chosen for the cross-database identity/rollback reasoning. Terra handled bounded implementation/review work; Luna handled small prerequisite and rendering/evidence tasks. The root addressed the findings and ran the actual service flow against both isolated backends.

### Editor delivery — 14 October 2026

The approved workflow is implemented and deployed at <http://127.0.0.1:28080/ui/administration/opengraph-analysis>. Create/edit, prerequisite selection and aliases, complete-chain test/reuse, table/graph results, private working copies, and exact tested-file review/activation are available. Standalone intermediate files may activate without consumers. The final startup safeguard keeps normal BloodHound analysis available if optional editor answer storage cannot be initialized.

The fresh [editor evidence](../tools/esc1-poc/evidence/analysis-workspace-editor/README.md) records successful focused interface/native/PoC checks, both real databases, production builds, a real browser check and final authenticated API check. Active rules, schemas, pending uploads and database container identities were preserved. The repository-wide test command failed in the shared interface package after 7m49s when a temporary coverage file disappeared. Repository preparation is being recorded separately; it is not a substitute for tests. Nothing was staged or committed.

Final preparation completed dependencies, module synchronization, generation/formatting and licenses, then failed building the custom code checker because an offline dependency was missing. The complete service package and final deployed browser/API checks passed after the optional-storage startup safeguard. The evidence distinguishes those focused passes from the incomplete broad checks. Nothing was staged or committed.

Standard Redocly OpenAPI generation completed during repository preparation; its verified generated bundle and eight reviewed import/header formatting fixes were adopted. Other unrelated generated changes remained isolated.

Final deployed release includes those generated/formatting changes; its authenticated test/review and state-preservation checks passed. All verification processes have ended.

> **Updated 15 October 2026 at the user's request** (this section is kept as the 14 October agreement): prerequisites are chosen in a filterable dropdown, and aliases are edited only in the Cypher text. Testing is the play button (**Test file**), which runs the prerequisite check itself. Results are a table of rows only, shown between the Cypher box and Dependencies; the result graph, identifier-column selection, and test-output marking were removed, and the server no longer looks up objects from rows. The private copy action is **Save private copy**, and a **Revert** button was added. Activation keeps the two steps **Review tested file** then **Activate reviewed file**. One left-to-right **Dependency graph** window replaced the dependency list and arrow previews. Each check warns, as advice only, about declarations the file never uses. See `tools/esc1-poc/DESIGN.md` (Analysis file editor) and `tools/esc1-poc/HANDOFF.md`.

> **Updated 15 October 2026 at the user's request:** reuse of saved answers was removed from the editor. Every test now recalculates the needed prerequisite chain, and normal analysis runs no longer keep answer copies for the editor. Recorded timings showed little benefit: 0.1–0.9 seconds for full lab runs, under 12 seconds at one million objects. The agreement above about choosing reuse or recalculation, retained sources, and the internal-ID reuse rule is superseded. See `tools/esc1-poc/DESIGN.md` (Analysis file editor).

> **Updated 16 October 2026 at the user's request:** a failing step no longer discards the whole run. The failed step, the steps that use its results, and the other producers of its output types are set aside and keep their previous results; everything else is saved in one save. The status line shows these runs in orange as **partial** ("Finished with N failures: X steps saved; Y kept their previous results"), set-aside steps are orange in the graph, and the single retry recalculates only the set-aside steps. Reset Graph also frames the queries in whichever clear area beside or below the top panel shows them largest. See `tools/esc1-poc/DESIGN.md` (When one step fails: setting steps aside).
