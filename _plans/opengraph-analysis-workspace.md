# Build the OpenGraph analysis workspace

Status: proposed implementation plan, 13 October 2026. Following three reviews of the complete workspace design, the user approved a smaller read-only MVP and keeping the remaining features as a roadmap. Implementation has not been requested. This develops [TODO 3](../tools/esc1-poc/TODO.md#3-build-an-interactive-analysis-workspace-using-bloodhounds-sequential-layout).

## Purpose and boundaries

Give extension authors one place to follow calculations, inspect answers, understand failures, test changes, and manage their analysis files. Use BloodHound's familiar sequential graph display, table, details panel, navigation, menus, styling, login, and permissions where they fit. The Ghidra/IDA comparison is superseded by this design.

**Query nodes, dependency arrows, and collapsed groups are display information only. Never store them in BloodHound's collected-data graph.** Build the diagram from the selected rule revision and execution plan. Attach execution information from the selected run. Actual relationships produced by a successful analysis still belong in BloodHound's normal graph and can be opened in Explore.

Keep ordinary Explore behavior and existing API contracts intact. Reuse useful display components without teaching normal graph-object lookup or attack-path search to interpret query identifiers. No new schema or Cypher declarations are required by this workspace.

Schema uploads remain on OpenGraph Management. Analysis uploads and controls stay on the current rules page until this workspace provides their replacement. Reuse [TODO 6's extension association and version checks](opengraph-analysis-extension-integration.md); do not duplicate its storage or validation rules. Moving analysis records into BloodHound's application database is a separate decision, not a prerequisite for this page.

## MVP: understand current rules and completed runs

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
| Highlight related queries | Show prerequisites and downstream consumers in distinguishable ways, with a legend. Unrelated queries can be dimmed. Highlighting is explanatory; it does not select those queries for enable, disable, or delete. |
| Select several queries | Use table checkboxes and an equivalent graph selection control. Show the count and a shared action toolbar. The focused query and the set chosen for actions are separate, visible concepts. |
| Restrict the table | Offer all rules, selected rules, prerequisites, downstream consumers, and the entire highlighted chain. These use the same dependency information as the graph. A table row identifies its relationship to the focused query. |
| Select all | Distinguish all filtered rules from all rules in the displayed revision, with counts. Include retained rules whose schema is missing, labeled with their former extension and blocking reason. Collapsing or filtering does not silently change action selections; show how many selected rules are hidden and provide Clear selection. |
| Focus a chain | Show the focused query and its prerequisites and consumers. Shared prerequisites appear once. Keep a visible way to return to the full plan. |
| Search and navigate | Reuse pan, zoom, fit, search, extension filters, and keyboard conventions. Search can reveal a match inside a collapsed group. Keep layout positions stable while progress changes. |

Keep query labels compact: name, extension where needed, and short state indicators. Put full Cypher and lengthy properties in the panel. Extension identity must distinguish same-named files. Do not invent another author-facing naming convention; use TODO 6's identity when available.

## Dependencies and collapsed groups

Lay out prerequisites before their consumers, following the actual execution plan. An arrow explains what the consumer needs, such as a supplied answer parameter. Dependencies through helper objects or calculated relationship types must also be explained when the runner waits for them; label these separately from directly supplied answers. Merely reading the same collected object type does not establish an execution dependency.

Supply one complete dependency list from the same plan logic the runner uses, with a reason for each connection. Use it for the graph, table, panel, and test selection. Today's plan view lists direct answer prerequisites but omits helper/relationship waits from those arrows; extend it before displaying the first diagram. Existing broad connected groups are not execution chains and must not be used to invent arrows.

Queries sharing an output may affect each other's availability without having an execution order between them; show that relationship in the explanation, not as an invented prerequisite arrow. When a query reads its own output type, explain the runner's use of previously stored graph data rather than drawing a dependency on itself.

Offer collapse/expand for chains and extension groups. A collapsed group shows its rule count, selected count, and a summary of failures, running work, disabled rules, and blocked rules. Preserve arrows entering and leaving the group, including cross-extension connections. A shared prerequisite remains one query, even when several chains use it. Collapsing a visual group changes neither the execution plan nor the action selection.

Use one nonoverlapping grouping arrangement at a time: extension groups or unbranched chain sections. Keep shared branch/join queries visible between collapsed chain sections. Each query contributes to exactly one visible node or group; arbitrary nested or overlapping custom groups are unnecessary. Group arrows may point both ways between extensions without implying a circular query dependency. Group controls offer Expand, Focus, and Select the contained rules with a count; bulk changes use the common selection toolbar.

Extension filters must not hide a prerequisite or affected consumer without explanation. Show a boundary indicator with the hidden count and a way to reveal those queries. Expanding a group or jumping to a failure should keep the user's place where possible. Query order and stage describe dependencies; positions are not a timeline or a promise that queries run simultaneously.

## Query details and rule actions

The query panel includes name, extension, available version/revision information, order, stage, date added, enabled state and any blocking reason. Include full Cypher, graph inputs, supplied and received parameters, prerequisites and consumers, helper kinds and ID prefixes, relationship kinds, allowed endpoints, and existing-edge policy. Explain the effect of optional reachability, group-overlap, and batching instructions. Put query properties, answers, and run details in clearly named sections.

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
