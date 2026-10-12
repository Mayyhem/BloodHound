# Make extension development easier

Give extension authors a clear path from an idea to working rules: create an extension, check its files, explore and test its execution chains, review the proposed results, and activate the rules.

Open items describe proposed improvements; completed work is listed under [Done](#done). Keep the engine's existing shared calculations, scope and identity checks, complete-result validation, and all-or-nothing saving. The interactive execution graph below replaces the earlier suggestion for a separate development-mode execution preview.

Start with earlier validation and useful execution reports. Then build the interactive graph and its testing controls. Starter tooling and easier sharing build on those capabilities. Keep the completed authoring documentation current as the engine changes. Coordinate the Cypher work with the existing [implementation plan](CYPHER_IMPLEMENTATION_PLAN.md).

## 2. Catch mistakes before rules are activated

Checking filenames, declarations, and the final `RETURN` is not enough to establish that the complete query can run. Dependencies also determine when results are recalculated, so missing one can leave old results in place.

- [ ] Parse the complete query during file validation using the same language checks as execution. Distinguish file validation, language support, and actual database execution in the results.
- [ ] Report the filename, line, and useful explanation for each problem. Include the selected database and unsupported query feature when relevant.
- [ ] Find graph kinds and prerequisite parameters that are visible in the query and check them against declared dependencies. Suggest corrections; handle uncertain cases conservatively.
- [ ] Investigate automatically supplying dependencies that can be established reliably, while keeping exceptional dependencies explicit. Agree on any change to author requirements before implementing it.
- [ ] Add regression cases for a misspelled `MATCH`, a query that reads `MemberOf` without declaring it, missing prerequisites, unsupported syntax, and incorrect output columns.
- [ ] Add permanent automated tests for the quick-start files and shipped examples, including extracting each tutorial stage from Markdown and checking its expected results on both databases. Cover disabled and missing-property users, helper identities, property updates, and complete empty answers. Moved from #1; this requires approval for code changes.

**Done when:** authors receive actionable feedback before activation, and a query cannot silently omit an input that validation can identify.

Start in [the loader](internal/extension/extension.go), [output detection](internal/extension/output.go), and [the native execution adapter](internal/graphdb/native.go).

## 3. Build an interactive execution graph, like Ghidra/IDA

Make a connected graph the main workspace for understanding and testing rules. Use the block-and-arrow navigation familiar from Ghidra and IDA: each block represents a query, and an arrow leads from a prerequisite to the query that consumes its answer. Show the chains, their branches, and where they join, including dependencies between extensions.

Bring the details and actions currently available in the rule table into this graph. The picture must follow the actual execution plan and distinguish dependency order from observed execution timing.

- [ ] Lay out query blocks in dependency order. Show a shared prerequisite once, with arrows to all its consumers. Label connections with the supplied answer or parameter where useful.
- [ ] Support pan, zoom, fit, search, extension filtering, and expanding or collapsing chains. Selecting a query should highlight its prerequisites and downstream consumers, with navigation in both directions.
- [ ] Show each query's name, extension, order, stage, date added, and enabled state. Explain when a query is paused because a prerequisite is disabled.
- [ ] Open a details panel from a query block with its full Cypher, graph inputs, prerequisite parameters, consumers, helper types and ID prefixes, relationship types, allowed endpoints, and existing-edge policy. Include any reachability, group-overlap, and batching instructions while those remain supported.
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

## 4. Make execution reports useful to the author

The runner already records useful counts, but the persistent service mainly exposes a summary. A failure should give authors enough information to investigate without first finding application logs. These reports will also supply the execution graph.

- [ ] Report the failing query, stage, and useful error details, without exposing secrets.
- [ ] Include query row counts, durations, proposed and saved output counts, and the reason each query ran or was skipped.
- [ ] Distinguish a successful calculation from a successful save, and identify when the displayed results still belong to an older revision.
- [ ] Provide bounded recent execution history so authors can compare a changed rule with its previous run. Keep intermediate graph data available only through the appropriate inspection controls.
- [ ] Make an uncertain save outcome or an unavailable report visibly different from a confirmed failure or success.

**Done when:** the graph and report explain what ran, what happened, and which results are currently saved.

Start in [the runner report](internal/runner/runner.go), [persistent reporting](postprocessing/service.go), and [stored status](internal/ruleservice/store.go).

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

Query filenames and answer parameter names are currently shared across all installed extensions. The complete saved set also has a 32-file limit. These constraints make independently developed extensions harder to combine.

- [ ] Design query identities that belong to an extension, so two extensions can use the same local filename without a collision.
- [ ] Provide explicit references to shared queries, with a clear way to identify the version a consumer expects.
- [ ] Describe shared answer columns, their meanings, and their value types, and catch incompatible changes before activation where possible.
- [ ] Revisit limits for files and extension packages while keeping total storage and execution work bounded.
- [ ] Publish small reusable examples for identity matching and membership calculations, with expected results and dependency explanations.

**Done when:** authors can install and reuse another extension's calculation without coordinating filenames or discovering changed answer shapes during execution.

Start in [the definition registry](internal/extension/registry.go), [saved-file validation](internal/ruleservice/validation.go), and [the cross-extension example](examples/cross-platform/README.md). Agree on the author-facing format before implementing it.

## 7. Implement the Cypher simplification plan

ESC1 currently uses comments that ask Go code to transform query results. Running the Cypher by itself does not reproduce those transformations. Follow the existing [Cypher implementation plan](CYPHER_IMPLEMENTATION_PLAN.md) to make the rules understandable through ordinary queries while the engine manages execution efficiently.

**Status:** Plan under review; implementation has not started. Large-dataset testing waits for the Cypher rewrite comparison. Five-query scheduling is already implemented and must be the same in both versions being compared.

- [ ] Preserve a reproducible copy of today's code, rules, settings, and expected results before starting the rewrite.
- [ ] Replace `reachability`, `batch-on`, and `group-overlap` instructions one at a time with readable Cypher. Let the engine reuse calculations, divide work safely, and control resource use.
- [ ] Add only the DAWGS features needed for understandable rules and acceptable performance. Check that existing BloodHound functionality still works.
- [ ] Compare exact results on Neo4j and PostgreSQL, including groups, special cases, shared relationship results, property updates, and failures that must preserve previously saved results.
- [ ] Compare current and revised implementations on **1 million objects, then 10 million, toward 100 million**. Count relationships separately. Stop before advancing if the machine cannot safely handle the next tier or the results already provide a clear answer. Follow the plan's checks for correctness, speed, memory, temporary storage, and impact on normal BloodHound use.
- [ ] Until the special instructions are removed, expose their transformed answers and explain their effect in the execution graph's details and test results.
- [ ] Document the measured results, simplify the author guide, and remove replaced code after the revised implementation passes review.

**Done when:** authors can understand and test the meaning of an advanced rule without following its calculation through Go source.

Start in [the Cypher implementation plan](CYPHER_IMPLEMENTATION_PLAN.md) and [the current ESC1 query notes](extension/QUERY_NOTES.md).

## Done

### 1. Keep the authoring instructions and examples consistent

**Completed: documentation work on 11 October 2026.** The remaining permanent automated tests are tracked under [#2](#2-catch-mistakes-before-rules-are-activated), pending approval for code changes.

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
