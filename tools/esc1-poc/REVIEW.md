# Review guide

This is a proof of concept for one modified BloodHound Community Edition process. The separate rule registry keeps files; BloodHound runs them after its own successful analysis. This guide identifies the code and the evidence a reviewer should inspect. It does not establish production readiness.

Start with [README.md](README.md) for the proposal, [USAGE.md](USAGE.md) for operating instructions, and [DESIGN.md](DESIGN.md) for the rules the code follows and its failure behavior. [HANDOFF.md](HANDOFF.md) retains the dated work history, full historical commands, private log locations, and local running state.


## Schema kind declarations and inactive drafts

TODO #8 is implemented, including the user's follow-up choice to save incomplete uploads as inactive drafts. `external_node_kinds` is removed. Relationships use `compatible_kinds`, with exactly the same directed source/target combinations. Native kinds come from BloodHound's existing graph definitions; custom kinds come from the complete accompanying schema set. Declared node and relationship inputs are checked too. Missing schemas prevent activation, not draft saving. This work does not add full Cypher parsing or automatic dependency discovery; those remain under TODO #2.

Two schemas can refer to one another in either upload order. Save the first as a draft, add the second, review the full proposal, and explicitly activate it. A circular requirement for newly calculated answers still fails validation. Drafts persist separately from active rules, survive restart, and never reach the analysis worker. Stale active or draft revisions cannot replace newer work. The offline viewer accepts accompanying schemas together and displays native references without an extra list.

The native-kind check found an existing example error: BloodHound's local-group label is `ADLocalGroup`, not `LocalGroup`. The ESC1 schema, membership query, corresponding database fixture, and current notes were corrected together. The rename otherwise preserves endpoint pairs and their direction; no independent source/target combinations are introduced.

Checks and evidence:

- Full extension suite with race detection: `work/schema-kinds-final-unit.log`. Covers draft persistence, inactive incomplete uploads, both arrival orders, explicit activation, overwrite choices, stale revisions, failed draft saves, graph-input schema checks, and circular calculation rejection.
- Both dedicated graph databases: the complete Neo4j/PostgreSQL suite passed with race detection (`work/schema-kinds-final-databases.log`, 220.528 seconds). It covers exact endpoint pairs, shared producers, properties, empty answers, removal, and failure preservation. The earlier successful run is retained in `work/schema-kinds-databases.log`.
- BloodHound packages that use the service: `work/schema-kinds-final-root.log` covers `daemons/datapipe`, `daemons/api/toolapi`, and `services`. Static checks, including integration-tagged code: `work/schema-kinds-final-vet.log`.
- Real-browser draft flow through disposable services: `work/schema-kinds-final-draft-browser/checks.json`. Both upload orders, process restart, active-rule preservation, explicit activation, stale-draft rejection, and discard passed. No graph service was connected and no browser errors were recorded. `scripts/check-rule-drafts.py` reproduces this check with a new output directory.
- Offline model browser controls, exact pairs, safe rendering, and multiple mutually referencing schemas in either file order: `work/schema-kinds-model-multiple/checks.json`. No web requests or browser errors. Rule-table controls and activation confirmations: `work/schema-kinds-table.log`.
- All four tutorial stages and three shipped examples passed the local `plan` command (`work/schema-kinds-example-plans.json`). These are file/plan checks; tutorial queries were not executed. The permanent tutorial-result tests remain in TODO #2.

The first checks exposed an unused test import, the old local-group label, a remaining internal field reference, and one undeclared synthetic test input. Those were corrected; relevant intermediate logs remain under `work/schema-kinds-*`. Restricted-shell HTTP tests required a rerun with loopback networking enabled. Draft and viewer browser tests use only local temporary services or offline files.

Nothing was staged, committed, or deployed. Existing live saved rules were not converted. Before deploying this schema change, convert the old saved schema fields and preserve/recompute the complete bundle's revision and history; the new service intentionally does not accept the removed schema fields. The full `just prepare-for-codereview` command remains required before the user's manual commit. See [the handoff entry](HANDOFF.md#schema-kinds-and-inactive-drafts--11-october-2026) for those commands and deployment notes.

## Shared relationship results — 11 October 2026

The current implementation removes graph ownership settings, saved owners, owner properties added by the runner, and ownership checks. Several rules can produce one declared relationship type. The runner combines their complete pairs and merges supplied properties in alphabetical filename order; later filenames win overlapping keys. Every producer must agree on the existing-edge policy. Refreshing uses the declared type and endpoint pairs, including pre-existing or manually inserted edges in that scope. Helpers match by object ID and exact kind.

Review the complete lifecycle: selecting a producer includes its peers, inputs, and consumers; graph readers wait for the shared type’s complete staged answer. Disabling pauses shared producers and their downstream consumers while preserving results. Deletion removes explicit query dependents, retains graph readers and surviving producers, and refreshes their results. Removing the final producer records its endpoint scope for later cleanup. Every graph change still commits or rolls back with the whole run.

Validation of this change passed:

- Complete extension suite: `go test -race ./...` from `tools/esc1-poc`, including shared producers, policy agreement, graph dependencies, self-reads, Disable/Delete, saved cleanup scopes, and selective runs.
- Complete graph integration suite: `go test -race -tags integration -count=1 ./internal/graphdb` against the isolated Neo4j and PostgreSQL test databases. Passed in 103.668 seconds; log: `work/ownership-graphdb-live-race-final.log`. Checks include combined results visible to graph consumers, filename property precedence, preserved results while disabled, surviving and last-producer deletion, rollback after staged writes, legacy properties, and untouched connections outside the declared endpoints.
- BloodHound integration packages: `go test ./cmd/api/src/daemons/datapipe ./cmd/api/src/daemons/api/toolapi ./cmd/api/src/services` from the repository root.
- Updated example JSON, shell and Python syntax, demonstration-script checks, and the rules page's JavaScript syntax.

The full repository `just prepare-for-codereview` command has not been run. Nothing was staged, committed, or deployed. The historical test logs below describe earlier revisions, including now-removed ownership checks, and are not validation of this change. See the current [handoff entry](HANDOFF.md#ownership-removed-and-shared-relationship-producers--11-october-2026) for environment details.

## Authoring documentation check

The documentation portion of [TODO item 1](TODO.md#1-keep-the-authoring-instructions-and-examples-consistent) was completed on 11 October 2026. The authoring guide now builds shared answers, helpers, and relationship properties on the same starter schema, with explicit filenames and instructions to replace rather than duplicate a producer. It separates author choices from automatic platform work. The example guides, usage instructions, design explanation, and proposal's implementation description were checked against the current loader and runner. Historical handoff entries and recorded test evidence retain their original scope.

Only Markdown documentation was changed for this work. No application source, tests, scripts, schema files, or `.cypher` files were changed. No application was rebuilt, rules activated, or database calculation run.

The one-time file checks used the existing local executable at `work/extensions-build/registry/esc1-poc`. The tutorial's JSON and Cypher were copied from the Markdown into a temporary directory under `work/`, checked, and removed. All of the following loaded successfully, with each prerequisite before its consumers:

| Example | Queries in the plan | What was checked |
| --- | ---: | --- |
| Quick start | 1 | One membership producer with the declared relationship kind. |
| Shared answers | 2 | The enabled-user calculation has role `intermediate` and precedes the membership producer. |
| Helper objects | 3 | The added query produces the note kind already declared by the starter schema. |
| Relationship properties | 3 | The replacement query keeps its prerequisite and output kind; the complete folder loads. |
| Shipped ESC1 | 13 | Every shipped query is included in the plan. |
| Shipped vault | 2 | The helper precedes the relationship query. |
| Shipped Active Directory/CyberArk | 3 | The directory helper precedes its cross-extension consumer. |

This checks file loading and plan construction using an existing build. It is not a fresh source build, a full query-language check, or evidence that the new tutorial produces the expected graph results on either database.

**Still pending:** add permanent tests that extract the tutorial files from Markdown, check every stage and shipped example, and compare actual results with the documented expectations on both databases. Those tests should include disabled and missing-property users, helper identities, updating evidence on existing edges, and complete empty answers. This requires code changes and is awaiting the user's approval. Existing tests of shipped examples do not replace checks of the copied tutorial. This work is now tracked under [TODO item 2](TODO.md#2-catch-mistakes-before-rules-are-activated); item 1's documentation work has moved to Done.

## Five ready queries at a time

The scheduler now defaults to five simultaneous queries, with a permitted setting of one to five. A finished query releases its place after its answers are checked; another ready query can start without waiting for unrelated queries. Shared answers remain available until their last reader finishes. Creating supporting objects still requires later reads to use the same database operation, one at a time. Complete results still save together.

Validation on 11 October 2026:

| Check | Result |
| --- | --- |
| Pilot tests with race detection | Passed (`work/parallel-current-tests.log`). The final cancellation adjustment also passed all runner and automatic-service tests (`work/parallel-final-core-tests.log`). |
| Focused scheduling and failure tests | Nine cases passed ten repetitions with race detection: five-reader ceiling, immediate reuse, fast-chain progress, shared answers, helper visibility, and readers fully stopping before cleanup. A new test caught and verified the fix for a failure waiting for its connection to close before canceling peers. |
| Complete Neo4j and PostgreSQL integration suite | Passed with race detection in 224.628 seconds (`work/parallel-database-tests-final.log`). Covers both standalone and application connections, ESC1 answers, existing-edge policies, ownership, selective runs, removal, and failed-save preservation. |
| Static checks | `go vet -tags integration ./...` passed (`work/parallel-vet.log`). |
| Reloaded rules page | All 16 rules and three schemas preserved; desktop and narrow row alignment within 2px across eight columns (`work/parallel-deployed-page/`). |
| Reloaded BloodHound application | One browser Run Analysis request completed all 16 rules successfully. Saved rules remained identical, native processing returned to idle, and the button re-enabled (`work/parallel-manual-analysis/final-evidence.json`). |

The first database command used a PostgreSQL address format rejected by the test safety check; it did not establish PostgreSQL success. The corrected command above passed. An initial restricted test attempt could not open local HTTP ports; the authorized local-port run passed. No large dataset was loaded: the user asked to wait for the Cypher rewrite comparison, using the [one-million-object starting tier and stop rules](CYPHER_IMPLEMENTATION_PLAN.md). These checks establish scheduling behavior and small-fixture correctness, not a measured large-graph speedup.

## Cypher output declarations

Relationship queries now declare their edge name with a literal `kind` in the final `RETURN`. Helper queries declare a literal one-element `kinds` list. The shipped schemas contain no `post_processing` blocks; the platform supplies experiment defaults. [DEVELOPERS.md](DEVELOPERS.md) explains the contract, empty results, query policies, and legacy migration.

Validation for this change: the full POC unit suite passed with `-race`, and the complete `internal/graphdb` integration suite passed on the dedicated Neo4j and PostgreSQL databases. Checks cover ESC1 results, properties, empty results, disabled/removed rules, ownership, and rollback. New tests also cover literal discovery, rejected dynamic/duplicate output declarations, and saved-owner migration through registry restart. Logs: `work/cypher-output-race-tests.log` and `work/cypher-output-database-tests.log`. The regenerated offline model pages also passed browser checks, with no browser errors or web requests (`work/cypher-output-model-check/checks.json`). These fixture runs are not a production-scale performance benchmark or a deployment of the updated application.

## Review priorities

1. Follow the place where rules run after normal BloodHound analysis, and how other graph changes wait. Confirm every application graph writer participates, including tag renames and raw PostgreSQL deletion. Database switch and migration tools must refuse work while the optional integration is enabled. Other applications and direct database writers are outside this coordination.
2. Follow uploaded rules through checks, saved storage, and fetching. Confirm a calculation keeps the same saved copy of the rules throughout. Uploading alone does not run rules. Check combined output kind/query assignments, matching policies, saved retirement scopes, and helper ID prefixes. Check authentication, request limits, and rejected uploads.
3. Follow the runner from ordered query results to checks on the declared type and endpoint scope and the final saved changes. Helpers, calculated relationships, and the run record save together. Schema registration, kind entries, and index preparation are separate operations and may remain after failure. Helpers omitted by later rules are not automatically deleted.
4. Inspect database adapters and the separate DAWGS checkout. Review exact result numbers, checks that analysis queries cannot write, choices for updating existing connections, and how failed changes are discarded on both databases. A parent BloodHound commit does not capture DAWGS edits.
5. Compare the recorded outcomes and retained failures below with the claims. No complete repository checks or `just prepare-for-codereview` were run. Repository instructions require that command before committing; nothing was staged or committed in this work.

## Read the source in this order

BloodHound stores a graph: nodes are objects such as users and domains, and relationships connect them. A kind describes an object or connection type; `objectid` is an object's stable identifier. Cypher is the language used by the rule query files. Publishing saves calculated results. A transaction saves a group of changes together or discards them together; discarding them is a rollback. The independent ESC1 result is `PoC_ADCSESC1`, compared with BloodHound's native `ADCSESC1`.

Open [the ESC1 model](model.html) or [the vault model](examples/vault/model.html) in a browser first. Read [extension/schema.json](extension/schema.json) to see the definition that produces that picture and controls what the experiment may publish. Continue with [eligible_templates.cypher](extension/eligible_templates.cypher), [trusted_cas.cypher](extension/trusted_cas.cypher), and [membership.cypher](extension/membership.cypher). These answer the three shared questions: which templates permit ESC1, which certificate authorities can affect which domains, and which members belong to each group.

Read [esc1.cypher](extension/esc1.cypher) for the final calculation and [QUERY_NOTES.md](extension/QUERY_NOTES.md) for the native source references and unusual group rules. [domain_helpers.cypher](extension/domain_helpers.cypher) is a small helper-node example used to exercise publication and rollback.

For persistent mode, trace [entrypoint.go](../../cmd/api/src/services/entrypoint.go) into [postprocessing/service.go](postprocessing/service.go), then [pipeline.go](../../cmd/api/src/daemons/datapipe/pipeline.go) and [extensions.go](../../cmd/api/src/daemons/datapipe/extensions.go). These show how BloodHound starts the worker and hands it a completed native analysis. Read [postprocessing/gate.go](postprocessing/gate.go) alongside [DAWGS switch.go](../DAWGS/graph/switch.go) to see which graph changes wait. The registry starts in [service.go](service.go), with file validation and storage in [internal/ruleservice/store.go](internal/ruleservice/store.go).

The Go code separates its jobs into packages, which are folders of related source files:

| Folder | Responsibility | Good starting file |
| --- | --- | --- |
| `postprocessing` | Run the same saved rules after normal analysis, make other graph changes wait, and decide safe retries | [service.go](postprocessing/service.go), [gate.go](postprocessing/gate.go) |
| `internal/ruleservice` | Store validated uploads and report the latest execution status | [store.go](internal/ruleservice/store.go), [http.go](internal/ruleservice/http.go), [client.go](internal/ruleservice/client.go) |
| `internal/extension` | Read the schemas and query output declarations, check allowed combinations, prepare today's BloodHound upload, and order calculation queries | [definition.go](internal/extension/definition.go), [extension.go](internal/extension/extension.go) |
| `internal/modelview` | Turn the same definition into an offline interactive picture and complete connection table | [modelview.go](internal/modelview/modelview.go), [viewer.html](internal/modelview/viewer.html) |
| `internal/bloodhound` | Validate collection files, upload them, and wait for successful ingestion and native analysis | [client.go](internal/bloodhound/client.go) |
| `internal/runner` | Share graph calculations, bound saved answers, compare complete results, and publish combined changes together | [runner.go](internal/runner/runner.go), [calculate.go](internal/runner/calculate.go), [reach.go](internal/runner/reach.go), [overlap.go](internal/runner/overlap.go) |
| `internal/graphdb` | Connect to the selected graph database, check read-only queries, and apply validated changes | [native.go](internal/graphdb/native.go) for persistent mode; [neo4j.go](internal/graphdb/neo4j.go), [postgres.go](internal/graphdb/postgres.go) for standalone mode |
| Standalone command | Join those pieces together, run one batch at a time, and stop/restart the dedicated BloodHound service | [main.go](main.go), [deployment.go](deployment.go), [stack.go](stack.go), [container_stack.go](container_stack.go) |

In Go, a `struct` collects named fields into one value, and an `interface` describes operations another piece of code must provide. The runner's database interface is deliberately small: begin a transaction, read, write, commit, and roll back. That lets tests replace the database with a controlled implementation without changing the publication rules. A `map` associates names with values; each query row is a map such as `{"source": "user-id", "target": "domain-id"}`. A slice is an ordered list; prerequisite answers are slices of these row maps.

Other Go notation you will see:

| Notation | Plain-English meaning |
| --- | --- |
| `var (...)` | Set aside named values together at the start of a function. |
| `value := expression` | Create a local name for the answer to an expression. |
| `value, err := operation()` | An operation can return an answer and an error together. |
| `if err != nil { return ... }` | Stop this function if that operation failed. `nil` means there is no error or other value. |
| `[]Pair` | A list whose items are source/target pairs. |
| `map[string]any` | Values looked up by text names; their value types can differ. |
| `*Database` and `&Database{...}` | A reference to a database value, and creation of a value whose reference can be shared. |
| `func (s *Database) Close(...)` | A function attached to a database value; `s` names that value inside the function. |
| `defer cleanup()` | Arrange for cleanup to run when this function finishes, including after an error. |
| `ctx context.Context` | Carry a stop request and time limit through operations that may take time. |
| `` `json:"source"` `` | Use `source` as this field's name when writing or reading JSON. |
| `for _, item := range items` | Work through the items; `_` discards a position number we do not need. |

For example, [runner.go](internal/runner/runner.go) reads a query, checks its error, checks its returned values, and only then records those answers for later files. Follow that function from top to bottom first. [publish.go](internal/runner/publish.go) contains the detailed rules for changing graph data. The calculation coordinator runs a limited number of independent reads and keeps publication under one coordinator.

## Application integration files

Most of the new code lives in `tools/esc1-poc`. The files under `cmd/api` connect that code to BloodHound's normal work. DAWGS supplies the code BloodHound uses to work with the graph database. The table identifies each change so a reviewer can open the relevant file.

| File | What changed or what the test checks |
| --- | --- |
| [config.go](../../cmd/api/src/config/config.go) | Adds the rule container's address and secret access code. An empty address leaves automatic rule processing off. |
| [entrypoint.go](../../cmd/api/src/services/entrypoint.go) | When BloodHound starts, connects the new rule-running code to the database and model information BloodHound already uses. |
| [pipeline.go](../../cmd/api/src/daemons/datapipe/pipeline.go) | Runs rules after BloodHound's own analysis succeeds. When no new analysis is waiting, it also checks whether a failed attempt can retry. |
| [extensions.go](../../cmd/api/src/daemons/datapipe/extensions.go) | Provides the small link between BloodHound's normal work and the new rule processing. After a retry saves changes, it clears previously saved query answers. |
| [extensions_test.go](../../cmd/api/src/daemons/datapipe/extensions_test.go) | Checks that rules run at the right time, stay off when disabled, and clear saved answers when needed. |
| [delete.go](../../cmd/api/src/daemons/datapipe/delete.go) | Makes graph deletion follow the waiting rule, including PostgreSQL's fast deletion methods. |
| [delete_extension_gate_test.go](../../cmd/api/src/daemons/datapipe/delete_extension_gate_test.go) | Checks that a deletion cannot start while it is forbidden, and that other work is allowed to continue after deletion finishes or fails. |
| [assetgrouptags.go](../../cmd/api/src/api/v2/assetgrouptags.go) | Makes tag updates follow the waiting rule because they can rename types of objects in the graph. |
| [tool API](../../cmd/api/src/daemons/api/toolapi/api.go) | Refuses requests to switch graph databases or move graph data between databases while automatic rule processing is enabled. |
| [toolapi/extension_gate_test.go](../../cmd/api/src/daemons/api/toolapi/extension_gate_test.go) | Checks those maintenance restrictions and confirms that status checks and unrelated requests still work. |
| [root go.mod](../../go.mod) | Makes this BloodHound build include this project's rule code and edited DAWGS code. Both sets of source files need to accompany the review. |
| [DAWGS switch.go](../DAWGS/graph/switch.go) | Makes operations that change the database follow the waiting rule, while allowing searches to continue. |
| [DAWGS switch_gate_test.go](../DAWGS/graph/switch_gate_test.go) | Checks that changes wait or fail when required and that searches can continue. |
| [DAWGS switch_observer.go](../DAWGS/graph/switch_observer.go), [switch_observer_test.go](../DAWGS/graph/switch_observer_test.go) | Optionally records graph writes and provides separate reads to identify changed types. Checks that ordinary writes and reads retain their behavior when observation is disabled. |
| [DAWGS manager.go](../DAWGS/drivers/pg/manager.go), [driver.go](../DAWGS/drivers/pg/driver.go) | Protect PostgreSQL's lists of object and connection types when one operation updates them while another reads them. |
| [DAWGS manager_cache_test.go](../DAWGS/drivers/pg/manager_cache_test.go) | Checks simultaneous use of those lists, including failed updates and accidental changes to a returned copy. |
| [DAWGS explicit_json.go](../DAWGS/drivers/pg/explicit_json.go) | Reads extension result numbers precisely, including large whole numbers, without changing how other BloodHound queries read their results. |
| [DAWGS explicit_json_test.go](../DAWGS/drivers/pg/explicit_json_test.go) | Checks number accuracy, empty values, ordinary results, and errors. |
| [native integration tests](internal/graphdb/native_integration_test.go) | Uses real Neo4j and PostgreSQL databases to check saved results, searches during calculations, and preservation of earlier results after failed calculations. |
| [postprocessing/service.go](postprocessing/service.go) | Gets a saved set of rules, runs it after BloodHound's own analysis, and decides whether a failed attempt can safely retry. |
| [postprocessing/gate.go](postprocessing/gate.go) | Makes other graph changes wait while BloodHound's analysis and rules run, while letting people keep searching. |

## Separate DAWGS dependency changes

[tools/DAWGS](../DAWGS/README.md) is a separate Git checkout. Root [go.mod](../../go.mod) and the POC [go.mod](go.mod) select local code. Include both modified and new DAWGS files in its review. The application table above covers the persistent integration additions; this table groups the other dependency work by purpose. The complete local change set must be reviewed, including fixtures and documentation.

| DAWGS files | Review purpose |
| --- | --- |
| [frontend/context.go](../DAWGS/cypher/frontend/context.go), [unsupported_expression_test.go](../DAWGS/cypher/frontend/unsupported_expression_test.go) | Reject unsupported comprehensions and indexes rather than silently treating them as different expressions. |
| [functions.go](../DAWGS/cypher/models/cypher/functions.go), [translate/function.go](../DAWGS/cypher/models/pgsql/translate/function.go), [esc1_function_test.go](../DAWGS/cypher/models/pgsql/translate/esc1_function_test.go) | Limited guarded `toFloat`, `trim`, and `properties` support. |
| [pgtypes.go](../DAWGS/cypher/models/pgsql/pgtypes.go), [parameter_maps_test.go](../DAWGS/cypher/models/pgsql/parameter_maps_test.go), [map_expressions.go](../DAWGS/cypher/models/pgsql/translate/map_expressions.go), [map_expressions_test.go](../DAWGS/cypher/models/pgsql/translate/map_expressions_test.go) | Typed map values and row-map parameters, including nested values and empty arrays. |
| [expression.go](../DAWGS/cypher/models/pgsql/translate/expression.go), [projection.go](../DAWGS/cypher/models/pgsql/translate/projection.go), [quantifiers.go](../DAWGS/cypher/models/pgsql/translate/quantifiers.go) | Preserve JSON field types, collected maps, grouping, null and Boolean expressions, quantifier aliases, and expression scopes. |
| [match.go](../DAWGS/cypher/models/pgsql/translate/match.go), [translator.go](../DAWGS/cypher/models/pgsql/translate/translator.go), [traversal.go](../DAWGS/cypher/models/pgsql/translate/traversal.go), [unwind.go](../DAWGS/cypher/models/pgsql/translate/unwind.go) | Optional joins, leading UNWIND/WITH frames, and bound traversal roots for the simpler rule queries. |
| [format.go](../DAWGS/cypher/models/pgsql/format/format.go), [selectivity.go](../DAWGS/cypher/models/pgsql/optimize/selectivity.go) | Supporting SQL formatting and query selection behavior. |
| [result.go](../DAWGS/drivers/pg/result.go), [result_test.go](../DAWGS/drivers/pg/result_test.go) | PostgreSQL result handling and its regression checks. |
| [explicit_transaction.go](../DAWGS/drivers/pg/explicit_transaction.go), [unit tests](../DAWGS/drivers/pg/explicit_transaction_test.go), [integration tests](../DAWGS/drivers/pg/explicit_transaction_integration_test.go), [explicit_relationships.go](../DAWGS/drivers/pg/explicit_relationships.go), [relationship tests](../DAWGS/drivers/pg/explicit_relationships_test.go) | Explicit final transactions and generic per-row relationship batches, with identity guards and late rollback. |
| [nodes.sql](../DAWGS/cypher/models/pgsql/test/translation_cases/nodes.sql), [mutation_tests.json](../DAWGS/cypher/test/cases/mutation_tests.json), [negative_tests.json](../DAWGS/cypher/test/cases/negative_tests.json), [collection_constraint_shapes.json](../DAWGS/integration/testdata/templates/collection_constraint_shapes.json), [map_shapes.json](../DAWGS/integration/testdata/templates/map_shapes.json) | Expected SQL, rejected expressions, and live semantic fixtures. |
| [README.md](../DAWGS/README.md), [translator README](../DAWGS/cypher/models/pgsql/translate/README.md) | Dependency behavior and limitations. |

These changes do not add general CASE, reduce, list comprehension, list indexing, or CALL support. Rule calculations were rewritten around simpler queries and shared Go calculations; see [DESIGN.md](DESIGN.md).

## Checks for affected-query selection on 11 October 2026

The automatic service now selects extension queries using observed changes.
The runner includes needed earlier queries, keeps the full plan's saved identity,
and updates only selected output kinds. A missing answer for a skipped rule is
never treated as an empty answer requiring deletion.

| Check | Result and scope |
| --- | --- |
| Complete pilot unit and race checks | Passed in `work/affected-analyses-unit-live.log`. Selection tests cover dependencies, skipped output identities and properties, cleanup, late failure, unknown changes, and changed rules. Service tests check that failures and replacement jobs do not lose earlier changes. |
| Automatic service on both databases | Passed in `work/affected-analyses-selection-live-final.log`. Uses the real rule HTTP service and native database connections: five initial queries become three after a relevant change. A second type on the changed object is also detected. Skipped outputs retain their IDs and properties. |
| Selected publication on both databases | The same live checks verify replacement, removal after a complete empty answer, and rollback after helper and relationship writes have been prepared. |
| Existing ESC1 and publication behavior | Passed in `work/affected-analyses-regression-live-final.log`: 26 native ESC1 fixtures on each database, all existing-edge policies on both, and Neo4j OpenGraph publication and failure preservation. |
| Existing BloodHound integration checks | Focused writer-gate, pipeline, and maintenance tests passed in `work/affected-analyses-native.log`. No additional native analysis algorithms were changed. |
| Complete DAWGS unit and race checks | `make test` passed with and without the isolated PostgreSQL database enabled. With that database enabled, matching the earlier coverage setup, 21,578 of 34,160 statements are covered (63.16745%, previously 62.95135%). The complete fresh profile and log are under `work/affected-analyses-dawgs-coverage-pg/` and `work/affected-analyses-dawgs-unit-pg.log`. |
| Go source checks | `go vet ./...` passed for the pilot in `work/affected-analyses-vet.log`. |

The first local unit run could not open test-server sockets. The first live
selection fixtures used unchanged property values instead of requested updates;
the next attempt used a query variable reserved by the PostgreSQL parser. Those
test-fixture mistakes were corrected without changing database behavior. Their
failed logs are retained under `work/affected-analyses-*`.
The database-enabled DAWGS cleanup test left two empty graph records; the pilot
correctly rejected that setup in a later regression run. Only those test-created
records were removed after verifying the isolated database contained no nodes
or edges. The single-graph safeguard remains unchanged.

Tracking stops trying to narrow the work after 256 metadata lookups, 4,096
returned objects, or 4,096 distinct types between successful runs. It then
requires all rules. Metadata reads use a separate connection and a 100 ms
deadline; legacy Neo4j connection acquisition can still take its driver's
configured timeout. Large uploads and broad native changes may therefore still
require full analysis. No large-graph performance result or rebuilt application
container is claimed by these checks.

## Manual analysis button and page polish — 11 October 2026

The deployed page has three schemas and 16 rules, including the synthetic AD
and CyberArk examples. Earlier rule files were preserved. Confirmation lists
and aligned action buttons were checked at desktop and narrow widths.

The full pilot race suite passed (`work/manual-analysis-tests.log`), as did vet
including integration source (`work/manual-analysis-vet.log`). Tests cover the
native signed-request format, access checks, queue/running states, repeated
clicks, lost responses, unsupported responses, size limits, redirects, and
private error messages. The browser fixture exercises button requests and
status changes; the deployed browser check reads real saved rules without
changing them. Logs: `work/manual-analysis-browser-configured.log` and
`work/manual-analysis-live-browser`. A first browser launch lacked the installed
library path; the configured repeat passed.

The button uses existing BloodHound API calls and adds no native application or
DAWGS changes. After the user approved its server-held API key, a real browser
click returned 202 and completed all 16 queries successfully. The check required
a new extension event matching the active rules, native analysis returning to
idle, and unchanged saved rule files. It also verified that the button was
disabled while queued/running and enabled after completion. No browser errors
were observed. Evidence: `work/manual-analysis-enabled-browser/final-evidence.json`
and screenshots in that directory. An initial test counted schema filenames
incorrectly and stopped before clicking; only that test assertion needed fixing.

The key inherits its account's permissions, so its access matters beyond the
three fixed calls this client makes. It is stored only in private, ignored
`rules.env`. [USAGE](USAGE.md#run-analysis-without-another-collection-upload)
explains setup and the difference between a queued request and finished results.

## Multiple-schema table checks — 11 October 2026

The page now combines every extension in one sortable, filterable table. It shows
progress during status checks and supports confirmed deletion and reversible
disabling, individually or for selected rules. The synthetic AD/CyberArk example
tests a dependency that crosses the two schemas; it is not an official collector.

| Check | Result and scope |
| --- | --- |
| Pilot tests | Full race suite passed in `work/multiple-rules-race-tests.log`. Covers combined validation, ownership, saved dates, disabling, restart, complete client validation, and registration retries. |
| Source checks | Vet, including integration-test code, passed in `work/multiple-rules-vet.log` and `work/multiple-rules-integration-vet.log`. |
| AD/CyberArk on both databases | Passed in `work/multiple-schemas-database-tests.log`: add CyberArk after AD, preserve AD helpers, run cross-platform queries, preserve disabled results after facts change, keep independent work running, enable again, and delete under the original owner. Deliberate late failures preserve previous data. These are direct database fixtures, not collection uploads. |
| Broader database checks | The full graph-adapter suite completed with one outdated assertion in `TestChangeCoverageControlsPublication`; all other cases passed (`work/multiple-rules-all-database-tests.log`). That assertion expected all rules to rerun after an enrollment change. It now checks that the unrelated domain helper stays untouched; the corrected test passed separately (`work/multiple-rules-coverage-regression.log`). |
| Native BloodHound coordination | Focused analysis and writer-coordination tests passed in `work/multiple-rules-native-tests.log`. No production DAWGS change was needed for this update. |
| Actual browser and storage | Isolated two-schema uploads, activation, sorting, filtering, visible selection, single and bulk disable/enable, persistence, overwrites, stale-tab rejection, and deletion passed. Bearer fallback passed too. Reports are in `work/multiple-rules-final-browser` and `work/multiple-rules-browser-bearer`. Delayed-response checks also exercise progress indicators and cancelled-preview filter restoration. |
| Deployed development containers | Both were rebuilt and restarted; the registry was rebuilt again for the final browser-only fixes. Page and application health returned 200 and saved active rules stayed byte-for-byte unchanged. Build and health records: `work/multiple-rules-build.log`, `work/multiple-rules-registry-final-build.log`, `work/multiple-rules-deployed-health.json`. The deployed browser check made no saved changes. |

No private collection was re-imported, and these checks do not establish
100-million-edge performance or Enterprise compatibility. Dates missing from
older saved rules are displayed as **Not recorded** rather than invented.

## Rule management checks — 11 October 2026

The rules page now supports reviewed replacement and removal, schema reassignment,
and a complete view of independently grouped queries. The older upload-only page
checks below describe an earlier version.

| Check | Result |
| --- | --- |
| Pilot tests and source checks | Complete race suite and vet passed: `work/rules-management-unit-final.log`, `work/rules-management-vet-final.log`. Includes stale-tab protection, replacement decisions, dependency rebuilding, removal history, local access checks, and reopening saved rules without a writable system temporary directory. |
| Removal on both databases | Passed: `work/rules-removal-live.log`. Covers ownership protection, rollback after deletion starts, last-rule removal, preserved helpers, and changed rules forcing a full run. |
| Native integration | Focused pipeline, writer-gate, and maintenance tests passed in `work/rules-management-native.log`. This task adds no DAWGS or native analysis algorithm changes. |
| Browser behavior | Twelve isolated checks passed, plus bearer fallback and five deployed-page checks. No browser errors. Reports and screenshots are under `work/rules-management-browser`, `work/rules-management-bearer-browser`, and `work/rules-management-deployed-browser`. |
| Containers | Both images rebuilt and loaded into the persistent Neo4j lab. Page and application health checks passed; saved active rules were unchanged through restart. See `work/rules-management-build-final.log` and `work/rules-management-deployed-health.json`. |

The isolated browser check changes only disposable rule storage with no graph
connection. The deployed check uses previews and rejected uploads. Database tests
use only the separate test databases. This revision did not repeat collection
uploads or the private lab ZIP, or measure large-graph performance. Helper objects
remain preserved after their calculation is removed.

## Checks after removing the exposure field on 11 October 2026

The example now creates `PoC_Vault_HasAccountFor` from matching account identifiers.
It no longer asks a collector to supply an `exposed` flag. This association does
not establish an attack path and is not traversable. The proposal explains the
solution without relying on this example.

Focused unit and race checks for extension loading, rule storage, and model viewing
passed (`work/vault-without-exposure-unit.log`). The existing OpenGraph publication,
failure-preservation, and unrelated-membership-cycle tests passed against the
isolated Neo4j test database (`work/vault-without-exposure-neo4j-live.log`). These
tests also confirm that an account association remains when the directory user is
disabled. The changed scripts passed Python and shell syntax checks, and the
offline model was regenerated from the new schema.

PostgreSQL and the full collection-upload demonstrations were not rerun for this
revision. The older results below retain their original scope.

## Evidence recorded on 8 October 2026

The vault rule described by these older results was retired on 11 October. Its
invented `exposed` field and risk connection were removed. The current example
only matches account identifiers and creates a connection that cannot be used as
an attack-path step. Earlier vault results do not validate the changed rule.

The documentation split did not rerun code tests or container demonstrations. [persistent-results.json](evidence/persistent-results.json) is the current native integration summary. Its local logs under ignored `work/` are available in this workspace but will not accompany a fresh checkout.

| Check | Recorded result and limit |
| --- | --- |
| POC unit tests, race checks, and vet | Passed; `work/persistent-complete-final-unit.log` and `work/persistent-final-vet.log`. |
| Native hook, writer gate, maintenance, raw purge, and commit reporting | Focused checks passed; log locations are in the persistent evidence JSON. A broader API race run exposed an unresolved shared mock slice race in the unchanged `ListKinds` handler/tests (`work/native-delete-gate-tests.log`). |
| Native adapters on both databases | Race checks passed: 26 direct ESC1 fixtures per backend, all publication policies, exact numbers, read availability during coordination, and rollback (`work/native-adapter-final-live.log`). Direct fixtures are not uploaded-collection tests. |
| Fresh DAWGS unit/race suite | Passed, 21,479 of 34,120 statements covered (62.95135%); `work/native-dawgs-unit.log`. This supersedes the earlier combined coverage calculation for this unit/race scope, and does not imply every live integration suite was rerun at this milestone. |
| Upload page | Six browser checks passed; `work/rule-upload-browser/verification.json`. |
| Persistent Neo4j flow | Complete recorded flow passed: 18 uploaded scenarios and 11 exact pairs, rule updates, rejected writing rules, replacement, failed-rule preservation, restoration, and registry restart persistence. 633 read probes succeeded; 26 graph and 24 UI probes overlapped rule execution (`work/persistent-neo/check-final`). |
| Persistent PostgreSQL core flow | Six core phases passed with 11 expected ESC1 pairs (`work/persistent-pg/check-paths`). The combined script subsequently failed its multi-file job bookkeeping assertion: job 7 reported one file after two uploads. The extension produced 11 ESC1 connections and one vault connection, but that does not make the overall script pass or establish that bookkeeping was fixed. |
| Persistent PostgreSQL vault with separate uploads | Separate single-file jobs passed directory/baseline/no-support/restoration checks: vault pairs 1/0/1, 11 ESC1 pairs in each phase, and 208 successful read probes (`work/persistent-pg/opengraph-separate`). Another 375 probes succeeded during the core and first vault attempt. These are separate evidence from the failed combined job. |

The PostgreSQL API also rejected an inspection query that projected a scalar map because of a missing SQL alias. The recorded checks used ordinary path responses to verify complete IDs and properties. This inspection-query limitation remains distinct from the job file-count failure.

Earlier standalone evidence belongs to its own operating mode, which stopped BloodHound while processing. [current-results.json](evidence/current-results.json) records final standalone policy, database, browser, and container checks. [results.json](evidence/results.json), [container-results.json](evidence/container-results.json), [opengraph-results.json](evidence/opengraph-results.json), and [schema-model-results.json](evidence/schema-model-results.json) preserve earlier milestones. In that earlier work, 26 direct fixtures and 18 uploaded scenarios were checked on each backend; the uploaded scenarios matched 11 exact pairs. A private ZIP with 13 files and 397 records matched two pairs on each backend. The current persistent demonstration did not re-import that ZIP. Private identifiers remain in ignored work files.

No 100-million-edge benchmark, Enterprise integration, or real lost-commit-acknowledgement fault injection was performed. Bounded work settings are not a hard database or process memory guarantee. Reads remain admitted but can wait on database locks and load. The uncertain-commit design stops further graph writes until inspection and restart; controlled checks are not evidence of a real network fault during commit. Researcher authoring time and production readiness have not been measured.

## Relevant checks to repeat

Run these from `tools/esc1-poc`, with the installed Go tool and an already authorized dependency cache. The wrapper disables downloads and keeps build products under ignored `work/`:

```sh
./scripts/go.sh test -race -count=1 ./...
./scripts/go.sh vet -tags integration ./...
```

For live adapter checks, [compose.yaml](compose.yaml) defines `test-graph` and `test-pg-graph` under the `tests` profile. Start only those services, using the existing `esc1-poc` project name. The images must already be available locally; both services have `pull_policy: never`.

```sh
docker compose -p esc1-poc -f compose.yaml --profile tests \
  up -d --no-build test-graph test-pg-graph
```

Neo4j uses ports 27687/27474, user `neo4j`, password `experiment-only`, database `neo4j`, and storage `work/test-neo4j` plus `work/test-neo4j-logs`. PostgreSQL uses port 25432, user `bloodhound`, password `experiment-only`, database `esc1_poc_test`, and storage `work/test-postgres`. These are separate from application storage and the persistent lab directories. [compose.pg.yaml](compose.pg.yaml) defines the same PostgreSQL test service; do not start another copy in a different project against that shared test storage.

Wait until both of these checks succeed; repeat them if either service is still starting:

```sh
docker compose -p esc1-poc -f compose.yaml --profile tests \
  exec -T test-graph cypher-shell -u neo4j -p experiment-only 'RETURN 1'
docker compose -p esc1-poc -f compose.yaml --profile tests \
  exec -T test-pg-graph pg_isready -h 127.0.0.1 -U bloodhound -d esc1_poc_test
```

The tests supply these lab credentials in code; no private application settings are needed. PostgreSQL tests truncate fixture tables, so their address must never point to preserved application data. Neo4j tests require an empty fixture graph. Both variables below are needed for native adapter tests; tests can skip if a variable is absent. Run:

```sh
NEO4J_TEST_URI=bolt://127.0.0.1:27687 \
ESC1_POC_POSTGRES_TEST_URI=postgresql://127.0.0.1:25432/esc1_poc_test \
  ./scripts/go.sh test -race -tags integration -count=1 ./internal/graphdb
```

To exercise real uploads and native analysis, use [check-persistent-service.py](scripts/check-persistent-service.py) with a fresh persistent lab and the authenticated setup in [USAGE.md](USAGE.md). Its required arguments are `--collections` and `--output`; `--opengraph` adds vault changes and leaves a combined ESC1/vault bundle active. Existing producers cannot later be removed with a core-only upload. Keep the failed combined PostgreSQL count assertion in the review record when comparing a new run. Full historical commands and reports stay in [HANDOFF.md](HANDOFF.md).

Before a commit, run `just prepare-for-codereview` from the BloodHound repository root as required by [AGENTS.md](../../AGENTS.md). The focused POC and dependency checks above do not replace it.

## File inventory

Maintained files under this experiment are listed individually. The counts recorded in older evidence describe those earlier snapshots. Paths are relative to this folder. Integration tests require their named separate empty database. Ignored binaries, work files, environment secrets, and caches are excluded. The sibling DAWGS checkout has its own [README](../DAWGS/README.md) and tests.

### Command, environment, and documentation

| File | Purpose |
| --- | --- |
| [.dockerignore](.dockerignore) | Allows only the executable and Dockerfile into the build context. |
| [.gitignore](.gitignore) | Keeps generated runtime files, the executable, and local environment settings out of version control. |
| [Dockerfile.bloodhound-reference](Dockerfile.bloodhound-reference) | Packages the unchanged native API development build in a separate image with the original DAWGS dependency. |
| [Dockerfile](Dockerfile) | Packages only the static executable in a scratch image. |
| [HANDOFF.md](HANDOFF.md) | Dated local history, detailed commands, preserved failures, and continuation notes. |
| [README.md](README.md) | Executive overview of the proposal and a guide to the other documents. |
| [DEVELOPERS.md](DEVELOPERS.md) | Extension author quick start, schema and query reference, current Cypher limitations, performance guidance, and troubleshooting. |
| [TODO.md](TODO.md) | Prioritized authoring improvements, documentation work completed, and remaining work including the proposed interactive execution graph. |
| [PROPOSAL.md](PROPOSAL.md) | User's OpenGraph analysis proposal, with an Implementation section mapping each problem to working pilot features and remaining work. |
| [USAGE.md](USAGE.md) | Setup, rule upload, demonstrations, operation, and recovery instructions. |
| [DESIGN.md](DESIGN.md) | Rule contracts, calculation and publication behavior, coordination, and design limits. |
| [AGENTS.md](AGENTS.md) | Local instructions for plain-English explanations, readable code, delegated work, and leaving commits to the user. |
| [CYPHER_IMPLEMENTATION_PLAN.md](CYPHER_IMPLEMENTATION_PLAN.md) | Plain-English plan for replacing author-facing reachability, batching, and group-selection instructions, with exact-result checks and a 1-million → 10-million → 100-million-object test ladder that stops early when justified. |
| [REVIEW.md](REVIEW.md) | Source reading guide, change references, scoped evidence, checks, and complete maintained file inventory. |
| [compose.pg.yaml](compose.pg.yaml) | Dedicated PostgreSQL graph lab, separate from the Neo4j environment. |
| [compose.postprocessor.yaml](compose.postprocessor.yaml) | Runs a separate command on the existing external network with runtime mounts and a shared state directory. |
| [compose.yaml](compose.yaml) | Defines the dedicated application, its databases, and separate Neo4j and PostgreSQL test services. |
| [container_stack.go](container_stack.go) | Verifies the existing CE graph configuration and private addresses, records image IDs, and stops/restarts the selected application. |
| [container_stack_test.go](container_stack_test.go) | Checks existing-container verification, replacement guards, and stop/restart recovery with controlled Docker responses. |
| [deployment.go](deployment.go) | Selects host or container deployment while keeping the same processing sequence. |
| [docker.go](docker.go) | Inspects and controls containers through the local Docker socket without a Docker executable. |
| [docker_test.go](docker_test.go) | Checks Docker socket requests, response handling, and errors without printing inspected secrets. |
| [go.mod](go.mod) | Declares the standalone module, database libraries, and local DAWGS replacement. |
| [go.sum](go.sum) | Checksums used to verify the Go dependency archive. |
| [graph_configuration.go](graph_configuration.go) | Selects the graph driver and checks application connection settings. |
| [main.go](main.go) | Connects the plan, schema, model, run, and comparison commands to their readers, viewer, client, and runner. |
| [main_test.go](main_test.go) | Checks service identification, restart behavior, locking, and API readiness without real containers. |
| [model.html](model.html) | Generated offline interactive picture and complete combination table for the ESC1 definition. |
| [postgres.env.example](postgres.env.example) | Benign example settings for the PostgreSQL graph lab and its separate tests. |
| [postprocessor.env.example](postprocessor.env.example) | Documents configurable names, mounts, credentials, and Docker socket authority without supplying secrets. |
| [retention_test.go](retention_test.go) | Checks log rotation, oversized reports, orphan answer cleanup, and work/report locks. |
| [stack.go](stack.go) | Checks the pinned host-mode stack and ports, stops/restarts its application, and holds the shared local command lock. |
| [versions.json](versions.json) | Records the native reference and exact library and container versions. |

### Collection client and readiness checks

| File | Purpose |
| --- | --- |
| [internal/bloodhound/client.go](internal/bloodhound/client.go) | Uploads validated collections, waits for completed ingestion, and checks ingestion job history. |
| [internal/bloodhound/client_test.go](internal/bloodhound/client_test.go) | Exercises collection validation, upload, job-status, and schema requests with controlled HTTP servers. |
| [internal/bloodhound/container_test.go](internal/bloodhound/container_test.go) | Checks container address restrictions, credential handling, and proxy/redirect protections. |
| [internal/bloodhound/idle.go](internal/bloodhound/idle.go) | Waits for an empty processing queue and successful current native analysis. |
| [internal/bloodhound/idle_test.go](internal/bloodhound/idle_test.go) | Checks queued work, processing state, startup analysis, and readiness failures. |
| [internal/bloodhound/opengraph.go](internal/bloodhound/opengraph.go) | Checks OpenGraph node kinds, edge endpoints, metadata, and property values against the native input contract. |
| [internal/bloodhound/opengraph_test.go](internal/bloodhound/opengraph_test.go) | Checks accepted OpenGraph forms, rejected malformed or reserved values, ZIPs, and cross-upload references. |
| [internal/bloodhound/validation.go](internal/bloodhound/validation.go) | Recognizes collection formats, checks classic JSON and all ZIP members before upload, and registers display schemas. |

### Plan loading and shared calculations

| File | Purpose |
| --- | --- |
| [internal/extension/definition.go](internal/extension/definition.go) | Reads and checks the updated OpenGraph definition and removes only proposed fields for a compatible BloodHound upload. |
| [internal/extension/definition_test.go](internal/extension/definition_test.go) | Checks exact endpoint pairs, declared kinds, calculation settings, and preservation of standard fields during export. |
| [internal/extension/extension.go](internal/extension/extension.go) | Builds calculation plans from the OpenGraph definition, reads dependency comments, orders queries, and identifies affected queries. |
| [internal/extension/extension_test.go](internal/extension/extension_test.go) | Checks schema declarations, dependency ordering, parameters, and conservative change handling. |
| [internal/extension/native_check_test.go](internal/extension/native_check_test.go) | Checks that native comparison requires an ESC1 producer and is optional for other extension schemas. |
| [internal/extension/reach.go](internal/extension/reach.go) | Checks reachability, group-overlap, and batch declarations. |
| [internal/extension/reach_test.go](internal/extension/reach_test.go) | Checks helper declarations, rejected settings, and current query syntax. |
| [internal/extension/readonly_test.go](internal/extension/readonly_test.go) | Checks forbidden query text and allowed read-only query forms. |
| [internal/extension/registry.go](internal/extension/registry.go) | Loads separately named schemas, combines declarations, retains every relationship producer, and assigns queries to their extensions. |
| [internal/extension/registry_test.go](internal/extension/registry_test.go) | Checks global dependency order, schema identity, cross-schema references, shared producers, duplicate declarations, and helper-prefix collisions. |
| [internal/runner/batch_test.go](internal/runner/batch_test.go) | Checks late write-batch failures, complete rollback, sampled reports, and bounded input batches. |
| [internal/runner/calculate.go](internal/runner/calculate.go) | Schedules bounded ready reads, releases answers, and serializes helper-dependent work. |
| [internal/runner/compare.go](internal/runner/compare.go) | Compares exact pairs with written expectations or native ESC1 answers. |
| [internal/runner/compare_test.go](internal/runner/compare_test.go) | Checks comparison read/run deadlines, no writes, and temporary cleanup after timeout. |
| [internal/runner/edge_policy_test.go](internal/runner/edge_policy_test.go) | Checks replacement, property merging, preservation, invalid maps, and rollback policies. |
| [internal/runner/limits.go](internal/runner/limits.go) | Defines calculation allowances and streaming read behavior. |
| [internal/runner/helper_scale_test.go](internal/runner/helper_scale_test.go) | Checks one prefix read for many helpers, identity preservation, unmatched old helpers, collisions, and late stream rollback. |
| [internal/runner/helpers.go](internal/runner/helpers.go) | Scans each helper prefix once and compares complete sorted desired and existing helpers. |
| [internal/runner/overlap.go](internal/runner/overlap.go) | Applies reusable ordered group-aware permission overlap. |
| [internal/runner/overlap_test.go](internal/runner/overlap_test.go) | Checks group shortcuts, special sides, overlaps, and ordered remove-self rules. |
| [internal/runner/parallel_test.go](internal/runner/parallel_test.go) | Checks the five-query limit, immediate scheduling, shared answers, helper visibility, and cancellation before connection closing and file cleanup. |
| [internal/runner/publish.go](internal/runner/publish.go) | Validates graph endpoints and identity and maintains helpers and relationships. |
| [internal/runner/publish_file.go](internal/runner/publish_file.go) | Compares complete sorted sets and publishes validated endpoint batches. |
| [internal/runner/reach.go](internal/runner/reach.go) | Builds shared reachable sets from relevant single-hop graph reads. |
| [internal/runner/reach_test.go](internal/runner/reach_test.go) | Checks diamonds, cycles, cancellation, limits, and all small directed graph closures. |
| [internal/runner/relationship_properties.go](internal/runner/relationship_properties.go) | Validates requested relationship properties and rejects conflicting duplicate pairs. |
| [internal/runner/runner.go](internal/runner/runner.go) | Runs the ordered plan, checks results, saves successful-run state, and commits or rolls back the complete update. |
| [internal/runner/runner_more_test.go](internal/runner/runner_more_test.go) | Checks shared prerequisite answers, conservative skipping, helper collisions, and reserved fields. |
| [internal/runner/runner_test.go](internal/runner/runner_test.go) | Checks publication, unchanged identities, support removal, rollback, endpoint guards, and native comparison. |
| [internal/runner/scale_test.go](internal/runner/scale_test.go) | Provides opt-in storage/reconciliation and shared-reach benchmarks with measured counts, time, temporary bytes, and process memory. |
| [internal/runner/store.go](internal/runner/store.go) | Stores typed private answers and sorts complete sets in bounded parts. |
| [internal/runner/store_test.go](internal/runner/store_test.go) | Checks answer round trips, incomplete files, sorting, and storage allowances. |
| [internal/runner/values.go](internal/runner/values.go) | Validates and sorts returned pairs, helper fields, query values, and stored properties. |

### Database adapters and tests

| File | Purpose |
| --- | --- |
| [internal/graphdb/address_test.go](internal/graphdb/address_test.go) | Checks host and private container graph address restrictions. |
| [internal/graphdb/endpoints_integration_test.go](internal/graphdb/endpoints_integration_test.go) | Checks exact permitted endpoint combinations and preservation of previous state when a combination is rejected in real Neo4j. |
| [internal/graphdb/edge_policy_integration_test.go](internal/graphdb/edge_policy_integration_test.go) | Checks all publication policies and late property-update rollback on both real databases. |
| [internal/graphdb/guards_integration_test.go](internal/graphdb/guards_integration_test.go) | Checks identities, selective triggers, exact mismatches, and uncertain commit acknowledgements against real graph state. |
| [internal/graphdb/multiple_integration_test.go](internal/graphdb/multiple_integration_test.go) | Checks the public two-schema rules through the real rule-service client and both native database adapters, including selective disable, scope checks, rollback, and removal cleanup. |
| [internal/graphdb/neo4j.go](internal/graphdb/neo4j.go) | Adapts the official Neo4j driver to the runner and checks that analysis queries are read-only. |
| [internal/graphdb/neo4j_integration_test.go](internal/graphdb/neo4j_integration_test.go) | Exercises ESC1 queries, publication, rollback, read-only enforcement, and membership cycles in real Neo4j. |
| [internal/graphdb/neo4j_publish.go](internal/graphdb/neo4j_publish.go) | Prepares input/endpoint indexes and performs typed batched publication. |
| [internal/graphdb/neo4j_publish_integration_test.go](internal/graphdb/neo4j_publish_integration_test.go) | Checks indexed batched publication against the separate Neo4j test graph. |
| [internal/graphdb/opengraph_integration_test.go](internal/graphdb/opengraph_integration_test.go) | Exercises the vault queries in real Neo4j, checking preservation, unchanged identities, support changes, removal, directory changes, failures, and scoped membership-cycle checks. |
| [internal/graphdb/postgres.go](internal/graphdb/postgres.go) | Uses local DAWGS for PostgreSQL reads and explicit publication transactions. |
| [internal/graphdb/postgres_esc1_integration_test.go](internal/graphdb/postgres_esc1_integration_test.go) | Checks ESC1 fixtures and failure preservation in the separate PostgreSQL graph. |
| [internal/graphdb/postgres_integration_test.go](internal/graphdb/postgres_integration_test.go) | Checks streaming and atomic publication against the separate PostgreSQL test database. |
| [internal/graphdb/postgres_json.go](internal/graphdb/postgres_json.go) | Preserves exact integer values in PostgreSQL JSON answers without changing the shared driver. |
| [internal/graphdb/postgres_json_test.go](internal/graphdb/postgres_json_test.go) | Checks nested integer precision and rejects malformed or out-of-range JSON answers. |
| [internal/graphdb/postgres_test.go](internal/graphdb/postgres_test.go) | Checks PostgreSQL addresses and parsed read-only queries. |
| [internal/graphdb/relationship_properties.go](internal/graphdb/relationship_properties.go) | Prepares relationship creation and guarded property updates for both adapters. |
| [internal/graphdb/relationship_properties_test.go](internal/graphdb/relationship_properties_test.go) | Checks relationship identities and malformed property-update batches. |

### Offline model viewer

| File | Purpose |
| --- | --- |
| [internal/modelview/modelview.go](internal/modelview/modelview.go) | Creates a standalone HTML page containing the chosen definition without unsafe text insertion. |
| [internal/modelview/modelview_test.go](internal/modelview/modelview_test.go) | Checks rendered definition handling, rejected invalid input, and safe embedding. |
| [internal/modelview/viewer.html](internal/modelview/viewer.html) | Offline page design, diagram controls, local definition chooser, details, exact combination table, and SVG download. |

### ESC1 schema and queries

| File | Purpose |
| --- | --- |
| [extension/QUERY_NOTES.md](extension/QUERY_NOTES.md) | Explains the native source references, group shortcut rules, and remaining ESC1 coverage limits. |
| [extension/ca_auth_reach.cypher](extension/ca_auth_reach.cypher) | Selects auth-chain reach seeds before the exact suffix check. |
| [extension/ca_root_reach.cypher](extension/ca_root_reach.cypher) | Selects certificate root-chain reach seeds. |
| [extension/domain_helpers.cypher](extension/domain_helpers.cypher) | Creates deterministic, separate helper descriptions for native domains. |
| [extension/eligible_templates.cypher](extension/eligible_templates.cypher) | Finds certificate templates whose settings permit ESC1. |
| [extension/enabled_cas.cypher](extension/enabled_cas.cypher) | Selects enabled CA hosts and carries known node metadata. |
| [extension/enrollment_principals.cypher](extension/enrollment_principals.cypher) | Selects relevant Base enrollment principals with graph metadata. |
| [extension/esc1.cypher](extension/esc1.cypher) | Selects ordered enrollment sets and requests group overlap for each target. |
| [extension/forest_reach.cypher](extension/forest_reach.cypher) | Selects Domain forest reach seeds, permitting reciprocal trust. |
| [extension/membership.cypher](extension/membership.cypher) | Derives group and special-member flags from shared reach evidence. |
| [extension/membership_reach.cypher](extension/membership_reach.cypher) | Requests strict membership reach only for relevant enrollment components. |
| [extension/schema.json](extension/schema.json) | ESC1 type presentation and permitted endpoint pairs; output declarations are in the Cypher files. |
| [extension/special_groups.cypher](extension/special_groups.cypher) | Selects native special Group principals by exact SID suffixes. |
| [extension/trusted_cas.cypher](extension/trusted_cas.cypher) | Combines certificate reach and the exact auth terminal-edge rule. |
| [extension/usable_cas.cypher](extension/usable_cas.cypher) | Applies native host-forest and unresolved-domain rules. |

### Synthetic ESC1 collections

| File | Purpose |
| --- | --- |
| [testdata/README.md](testdata/README.md) | Explains the baseline and changed evidence, expected results, and synthetic identifiers. |
| [testdata/baseline/certtemplates.json](testdata/baseline/certtemplates.json) | Collects the baseline certificate template with dangerous enrollment settings. |
| [testdata/baseline/computers.json](testdata/baseline/computers.json) | Collects the computer hosting the certificate authority. |
| [testdata/baseline/domains.json](testdata/baseline/domains.json) | Collects the synthetic domain and its related evidence. |
| [testdata/baseline/enterprisecas.json](testdata/baseline/enterprisecas.json) | Collects the enterprise certificate authority and enrollment evidence. |
| [testdata/baseline/groups.json](testdata/baseline/groups.json) | Collects its group and permission membership evidence. |
| [testdata/baseline/ntauthstores.json](testdata/baseline/ntauthstores.json) | Collects NTAuth trust evidence for certificate authentication. |
| [testdata/baseline/rootcas.json](testdata/baseline/rootcas.json) | Collects the trusted root certificate authority. |
| [testdata/baseline/users.json](testdata/baseline/users.json) | Collects the synthetic user used by the baseline. |
| [testdata/expected.json](testdata/expected.json) | Written expected source/domain pairs for the baseline demo. |
| [testdata/variants/all-supports-invalid/certtemplates.json](testdata/variants/all-supports-invalid/certtemplates.json) | Invalidates both supports so the old relationship should be removed. |
| [testdata/variants/manager-approval/certtemplates.json](testdata/variants/manager-approval/certtemplates.json) | Requires manager approval, preventing the template from supporting ESC1. |
| [testdata/variants/one-support-remaining/certtemplates.json](testdata/variants/one-support-remaining/certtemplates.json) | Invalidates one supporting template while retaining the other. |
| [testdata/variants/two-templates/certtemplates.json](testdata/variants/two-templates/certtemplates.json) | Adds a second template supporting the same experimental pair. |
| [testdata/variants/two-templates/enterprisecas.json](testdata/variants/two-templates/enterprisecas.json) | Updates the certificate authority to publish both templates. |

### Vault OpenGraph example

| File | Purpose |
| --- | --- |
| [examples/vault/README.md](examples/vault/README.md) | Explains the custom vault-to-directory join, fixture sequence, commands, and evidence limits. |
| [examples/vault/collections/baseline.json](examples/vault/collections/baseline.json) | OpenGraph collection with one vault and an account identifier matching the synthetic AD user. |
| [examples/vault/collections/two-supports.json](examples/vault/collections/two-supports.json) | Adds a second account for the same user; the result remains one vault-to-user association. |
| [examples/vault/expected.json](examples/vault/expected.json) | Written exact expected vault-to-user pairs for baseline, support, restoration, and disabled-user scenarios. |
| [examples/vault/extension/account_users.cypher](examples/vault/extension/account_users.cypher) | Matches collected account identifiers to directory users without assessing exposure or account status. |
| [examples/vault/extension/schema.json](examples/vault/extension/schema.json) | Single proposed OpenGraph definition of collected vault kinds, containment, helper, and calculated vault-to-user connection with its exact permitted endpoints. |
| [examples/vault/extension/vault_helpers.cypher](examples/vault/extension/vault_helpers.cypher) | Returns one separately labeled helper per vault with its count of contained accounts. |
| [examples/vault/model.html](examples/vault/model.html) | Generated offline interactive picture and complete combination table for the vault definition. |

### Synthetic Active Directory and CyberArk example

| File | Purpose |
| --- | --- |
| [examples/cross-platform/README.md](examples/cross-platform/README.md) | Explains the synthetic two-schema example, separate helper kinds, explicit prerequisite row flow, and factual account association. |
| [examples/cross-platform/extension/active_directory.schema.json](examples/cross-platform/extension/active_directory.schema.json) | Declares the Active Directory helper kind, and native User reference. |
| [examples/cross-platform/extension/ad_users.cypher](examples/cross-platform/extension/ad_users.cypher) | Returns directory helper rows carrying each native user's SID. |
| [examples/cross-platform/extension/cyberark.schema.json](examples/cross-platform/extension/cyberark.schema.json) | Declares synthetic CyberArk account kinds, the independent helper, and a non-traversable account-to-user association. |
| [examples/cross-platform/extension/cyberark_accounts.cypher](examples/cross-platform/extension/cyberark_accounts.cypher) | Summarizes collected synthetic accounts independently of directory rules. |
| [examples/cross-platform/extension/cyberark_links.cypher](examples/cross-platform/extension/cyberark_links.cypher) | Consumes current directory prerequisite rows and joins account user_sid values to native User object IDs. |

### Scripts

| File | Purpose |
| --- | --- |
| [scripts/build-container.sh](scripts/build-container.sh) | Creates private local permission settings when absent, builds the static Linux runner offline, and stages the minimal Docker context before building its image. |
| [scripts/build-reference.sh](scripts/build-reference.sh) | Builds the static original native API offline, verifies the reference and dependency pin, and records binary/image identities. |
| [scripts/build-image.sh](scripts/build-image.sh) | Checks the native reference and builds its dedicated BloodHound image. |
| [scripts/check-model-viewer.py](scripts/check-model-viewer.py) | Checks the offline viewer in a browser, including declared combinations, filters, unusual connections, and invalid definitions. |
| [scripts/check-opengraph-demo.py](scripts/check-opengraph-demo.py) | Checks exact vault demo expectations, unchanged relationship identity, failure snapshots, and helper kinds. |
| [scripts/demo-lab.sh](scripts/demo-lab.sh) | Runs a supplied private classic collection archive in a fresh lab and retains private evidence under ignored work files. |
| [scripts/demo-native-cases.sh](scripts/demo-native-cases.sh) | Runs the 18 scenarios through native ingestion and exact pair comparison. |
| [scripts/demo-opengraph.sh](scripts/demo-opengraph.sh) | Runs the vault OpenGraph demonstration in fresh labs with both arrival orders and changed evidence. |
| [scripts/demo-order.sh](scripts/demo-order.sh) | Checks classic collection arrival orders using two fresh dedicated graphs. |
| [scripts/demo.sh](scripts/demo.sh) | Checks native ESC1 agreement, repeat runs, overlapping support, removal, and restoration. |
| [scripts/go.sh](scripts/go.sh) | Runs the installed Go tool with project-local caches and downloads disabled. |
| [scripts/login.py](scripts/login.py) | Waits for the local application and obtains a session token without printing credentials. |
| [scripts/make-native-cases.py](scripts/make-native-cases.py) | Generates the synthetic inputs and written expectations for 18 native-ingestion scenarios. |

### Evidence

| File | Purpose |
| --- | --- |
| [evidence/bloodhound-helper.png](evidence/bloodhound-helper.png) | Synthetic browser screenshot showing the separate helper kind and ownership. |
| [evidence/bloodhound-opengraph.png](evidence/bloodhound-opengraph.png) | Synthetic browser screenshot showing the published vault-to-native-user relationship. |
| [evidence/bloodhound-relationship.png](evidence/bloodhound-relationship.png) | Synthetic browser screenshot showing the experimental ESC1 relationship. |
| [evidence/current-results.json](evidence/current-results.json) | Current scaling, DAWGS, edge-policy, browser, and final container checks, with scope and limits. |
| [evidence/persistent-results.json](evidence/persistent-results.json) | Persistent-container results on both databases, OpenGraph support changes, query availability, saved failures, image identities, and limits. |
| [evidence/container-results.json](evidence/container-results.json) | Public summary of container-mode assertions, runtime image identities, final-image checks, and seven numeric run measurements. |
| [evidence/model-viewer.png](evidence/model-viewer.png) | Screenshot of the offline vault model showing its complete two-combination table and isolated and external types. |
| [evidence/opengraph-results.json](evidence/opengraph-results.json) | Public summary of the vault OpenGraph demonstration outcomes, measurements, and limits. |
| [evidence/results.json](evidence/results.json) | Public summary of pinned versions and confirmed ESC1 outcomes and measurements, without private identifiers. |
| [evidence/schema-model-results.json](evidence/schema-model-results.json) | Records tests of the combined definition, exact pairs, offline browser page, and unchanged publication through the final container image. |

### Ignored local files

`work/` contains private collections, database storage, logs, temporary answers,
caches, generated reports, and build products. `esc1-poc` is the compiled
executable. Local environment files can contain credentials. These do not belong
in the maintained inventory or source control. Public evidence above contains
synthetic data and clearly scoped current or historical checks; private identifiers remain under `work/`.

### Persistent service and additional maintained files

| File | Purpose |
| --- | --- |
| [Dockerfile.extensions](Dockerfile.extensions) | Packages the separate native image with optional extension processing and its embedded UI. |
| [compose.extensions.yaml](compose.extensions.yaml) | Enables rule fetching in the selected native application. |
| [compose.rules.yaml](compose.rules.yaml) | Runs the persistent registry with private storage and no Docker or graph credentials. |
| [internal/graphdb/native.go](internal/graphdb/native.go) | Runs the existing runner through the native application graph connection. |
| [internal/graphdb/native_integration_test.go](internal/graphdb/native_integration_test.go) | Checks native graph publication on the isolated databases. |
| [internal/graphdb/native_test.go](internal/graphdb/native_test.go) | Checks native adapter behavior without a running graph. |
| [internal/graphdb/selection_integration_test.go](internal/graphdb/selection_integration_test.go) | Checks selected publication, skipped output preservation, rollback, and actual automatic change tracking on both databases. |
| [internal/runner/selection_test.go](internal/runner/selection_test.go) | Checks which queries run, complete-answer cleanup, native comparison, saved-plan identity, and preservation of skipped outputs. |
| [internal/ruleservice/client.go](internal/ruleservice/client.go) | Fetches and validates complete snapshots, including revisions, disabled settings, dates, and retirement scopes, and reports status without redirects or environment proxies. |
| [internal/ruleservice/analysis_client.go](internal/ruleservice/analysis_client.go) | Uses a server-held BloodHound API key to request normal analysis and read its status, with fixed operations and bounded requests. |
| [internal/ruleservice/analysis_client_test.go](internal/ruleservice/analysis_client_test.go) | Checks signed requests, native status handling, rejected responses, permissions, time limits, redirects, and private error messages. |
| [internal/ruleservice/analysis_http.go](internal/ruleservice/analysis_http.go) | Connects the manual analysis button to BloodHound, refuses overlapping requests, and distinguishes accepted requests from completion. |
| [internal/ruleservice/analysis_http_test.go](internal/ruleservice/analysis_http_test.go) | Checks access requirements, empty request bodies, disabled setup, busy handling, overlapping clicks, and safe failure messages. |
| [internal/ruleservice/http.go](internal/ruleservice/http.go) | Provides authenticated plan, preview, confirmed activation, and status endpoints with size and time limits. |
| [internal/ruleservice/http_test.go](internal/ruleservice/http_test.go) | Checks authentication, strict bodies, client limits, redirects, and concurrent uploads. |
| [internal/ruleservice/store.go](internal/ruleservice/store.go) | Validates and atomically persists active snapshots and the latest status. |
| [internal/ruleservice/store_test.go](internal/ruleservice/store_test.go) | Checks persistence, independent snapshots, schema identity, reassignment, additions, removals, and rejected updates. |
| [internal/ruleservice/settings.go](internal/ruleservice/settings.go) | Saves explicit disabled settings, pauses shared producers and dependent rules, and checks activation dates. |
| [internal/ruleservice/multiple_test.go](internal/ruleservice/multiple_test.go) | Checks the two-schema plan, incremental uploads, persistent dates, selective disable and re-enable, removal scopes, restart metadata, and complete client validation. |
| [internal/ruleservice/upload.html](internal/ruleservice/upload.html) | Presents one sortable and filterable rules table, expandable query details, schema downloads, upload review, readable confirmation lists, aligned actions, Run Analysis, and separate native and extension status. |
| [internal/ruleservice/validation.go](internal/ruleservice/validation.go) | Bounds and validates schema/query files, complete bundle revisions, JSON, saved disabled settings, activation dates, and retirement metadata. |
| [postprocessing/gate_test.go](postprocessing/gate_test.go) | Checks writer exclusion, nested admission, cancellation, and read availability. |
| [postprocessing/service_test.go](postprocessing/service_test.go) | Checks scheduling, pinned revisions, retries, and failure preservation. |
| [postprocessing/multiple_test.go](postprocessing/multiple_test.go) | Checks separate schema registrations before combined execution, complete registration caching, retry after partial failure, and disabled state passed to the runner. |
| [postprocessing/observe.go](postprocessing/observe.go) | Records changed types, looks up existing types within fixed limits, and requests a full run when the change record is incomplete. |
| [postprocessing/observe_query.go](postprocessing/observe_query.go) | Follows native filtered updates and deletions without changing the original query. |
| [postprocessing/observe_test.go](postprocessing/observe_test.go) | Checks added and removed types, multiple types on one object, deletion, lookup failure, limits, and unchanged native write behavior. |
| [postprocessing/selection_test.go](postprocessing/selection_test.go) | Checks accumulation across failures, outages, and newer jobs; confirms successful skips settle changes and extension writes are excluded. |
| [rules.env.example](rules.env.example) | Documents nonsecret service settings; real rules.env remains ignored. |
| [service.go](service.go) | Connects the serve command to private registry storage and optional BloodHound API settings for the manual analysis button. |
| [postprocessing/gate.go](postprocessing/gate.go) | Coordinates native and extension writer turns while leaving graph reads admitted. |
| [postprocessing/service.go](postprocessing/service.go) | Fetches pinned bundles, registers every schema separately, and runs the combined eligible plan after native analysis through the existing graph. |
| [scripts/build-extensions.sh](scripts/build-extensions.sh) | Builds distinct images offline, preserves UI assets, and creates private token settings once. |
| [scripts/check-persistent-service.py](scripts/check-persistent-service.py) | Checks real ingests, rule updates, preserved results after failure, concurrent reads, and optional OpenGraph support changes through the dedicated lab APIs. |
| [scripts/check-rule-upload.py](scripts/check-rule-upload.py) | Checks rule-page reads without changing saved rules by default; isolated mode tests activation, overwrite decisions, stale tabs, and confirmed deletion in disposable storage. |
| [scripts/check-rules-table.py](scripts/check-rules-table.py) | Checks the single rules table in an isolated browser API fixture, including ordering, sorting, filtering, selection, dates, expansion, confirmation, cancellation, and status polling. |
| [compose.persistent-lab.yaml](compose.persistent-lab.yaml) | Uses fresh Neo4j lab storage and separate ports without replacing the preserved control data. |
| [compose.persistent-pg-lab.yaml](compose.persistent-pg-lab.yaml) | Uses fresh PostgreSQL lab storage and host port 35432 with the persistent application on port 28080. |
| [internal/ruleservice/changes.go](internal/ruleservice/changes.go) | Builds complete multi-schema previews, requires explicit overwrites and current revisions, cascades removal, saves disabled settings, and retains retired kinds with their endpoint scopes. |
| [internal/ruleservice/changes_test.go](internal/ruleservice/changes_test.go) | Checks preview isolation, overwrites, stale tabs, reassignment, removal, and persistence of removal history. |
| [internal/ruleservice/local.go](internal/ruleservice/local.go) | Enables explicitly configured local browser access while keeping the worker API secret protected. |
| [internal/ruleservice/local_test.go](internal/ruleservice/local_test.go) | Checks exact local origin and methods, blocked cross-site changes, and continued worker authentication. |
| [internal/ruleservice/view.go](internal/ruleservice/view.go) | Describes the complete ordered plan, per-query extension, stage, activation date, enabled state, dependency details, and shared groups for the table and API. |
| [internal/ruleservice/view_test.go](internal/ruleservice/view_test.go) | Checks independent groups, shared inputs, branching order, endpoint inputs, and complete query details. |
| [internal/graphdb/removal_integration_test.go](internal/graphdb/removal_integration_test.go) | Checks removal, late rollback, mixed-origin edges, and last-rule removal against both real graph databases. |
