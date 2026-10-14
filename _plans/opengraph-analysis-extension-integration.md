# Integrate analysis files with OpenGraph extensions

**Status: incomplete (14 October 2026).** Steps 1–5 are implemented in the working tree. Step 6's code and documentation are updated, but the complete real-app workflow still needs verification. Repository checks, standard OpenAPI generation, and final review also remain unfinished; see the [verification record](../tools/esc1-poc/evidence/extension-integration/README.md). [TODO 6](../tools/esc1-poc/TODO.md#6-make-shared-queries-easier-to-distribute-and-change) remains open. Steps 7–8 are explicitly deferred and are not implemented. Nothing has been staged, committed, or deployed.

Users should upload extension schemas through BloodHound's existing OpenGraph Management page and continue uploading and managing analyses through the current rules page. Eventually, analysis upload and management will move to the dedicated [analysis workspace](opengraph-analysis-workspace.md) planned in [TODO 3](../tools/esc1-poc/TODO.md#3-build-an-interactive-analysis-workspace-using-bloodhounds-sequential-layout). Reuse the extension's existing name, namespace, version, and schema definition. Associate each analysis file with that extension so independent authors can use the same filenames, share calculations deliberately, and understand what an update or deletion affects.

The user has requested namespace and version declarations in every `.cypher` file, dependency conditions in the style of Python's `requirements.txt`, and an eventual option to delete associated analyses when deleting an extension. A dependency without a version condition uses the latest activated version available locally. This plan uses query names without the `.cypher` suffix in dependency declarations. The implemented comments and shared-answer approach were approved during implementation. The eventual deletion dialog remains a separate proposal.

## Why this work is needed

The [OpenGraph definition schema](https://bloodhound.specterops.io/opengraph/developer/graph-definition) already provides the extension's identity through `schema.name`, its graph naming prefix through `schema.namespace`, and its version through `schema.version`. Matching schema names identify an extension update. Keep these meanings.

The experiment currently stores all schemas and queries together under their filenames. Two unrelated `schema.json` files therefore collide. Query names and answer parameter names are also shared across all extensions. This limitation belongs to the experimental storage and query loader; extension identity already exists in OpenGraph.

The existing schema does not describe our analysis dependencies or the intermediate answers passed between calculations. Those need additional declarations. An answer containing a text column called `user_id`, for example, can break a consumer if that column is renamed, even when every graph type remains unchanged.

The current server accepts JSON and ZIP extension uploads, but its ZIP reader recognizes only `schema.json`, `pz_rules.json`, and `saved_queries.json`. The current schema upload dialog accepts one JSON file. Keep these schema-upload capabilities separate from analysis upload. Saved queries already supplied with an extension remain saved queries; uploading one must not automatically turn it into an analysis that creates or removes relationships.

## Intended user experience

1. The user uploads an extension schema through OpenGraph Management using the existing schema workflow.
2. The user uploads its `.cypher` files through the current rules page. The rules service associates them with the installed schema using their namespace and checks their declared compatible versions.
3. The rules page identifies added, changed, retained, and removed analyses, plus any effects on other extensions. Incomplete analysis uploads remain inactive drafts with an explanation of what is missing. Analyses uploaded before their schema stay inactive until it is installed and the completed set is reviewed.
4. The user activates a complete, valid analysis revision on the rules page after review. Uploading a draft does not run it or change the graph.
5. Successful BloodHound analysis runs the applicable calculations and saves their complete results using the existing checks.
6. The rules page provides analysis controls and reports for now. TODO 3 first adds a read-only workspace for current rules and completed outcomes, linking back to those controls. Later phases move management into that workspace and add live progress and testing.
7. Eventually, deleting an extension through OpenGraph Management offers a choice to keep or delete its associated analysis files and explains the consequences before confirmation. This deletion option does not make that page the analysis upload interface.

| Task | Page used for this work | Eventual analysis interface |
| --- | --- | --- |
| Upload or update an extension schema | OpenGraph Management | The schema workflow stays separate |
| Upload, review, activate, and manage analyses | Current rules page, including during TODO 3's MVP | Later management phase of TODO 3 |
| Inspect query dependencies and completed outcomes | Current rules page and reports | Read-only workspace MVP from TODO 3 |
| Inspect live query progress and intermediate answers; run isolated tests | Later TODO 3 work | Later reporting and testing phases of TODO 3 |
| Choose whether extension deletion also removes its analyses | Planned addition to OpenGraph Management's deletion dialog | Analysis details remain available from the analysis page |

Ordinary schema uploads continue to work. Existing schema sections, permissions, built-in extension protections, and graph-data behavior remain supported. TODO 6 can be completed on the current rules page; moving its controls into the execution graph is part of TODO 3.

The analysis workspace reuses BloodHound's sequential layout and familiar graph, table, and details-panel interactions. Query nodes, dependency arrows, and collapsed groups remain display information and must never enter the collected-data graph. The read-only MVP and later management, grouping, live reporting, and testing phases are defined in the workspace plan. MVP completion does not move management controls or complete all of TODO 3.

## Associate every query with its extension

Required and optional comments at the beginning of every analysis file:

```cypher
// namespace: PoC_CyberArk
// version: v1.0.0
// extension-versions: >=v1.0.0,<v2.0.0
// depends-on: node:PoC_CyberArk_Account
```

Separate the analysis file's version from the extension versions it supports. Apply these declarations to final relationship queries, helper queries, and intermediate calculations, including queries used by several extensions. `extension-versions` is optional. Without it, use the associated extension's latest activated schema without an additional version restriction. The file's own `namespace` and `version` remain required; the latest-version default applies to requirements, not to identifying the file itself.

| Declaration | Meaning | What BloodHound checks |
| --- | --- | --- |
| `namespace: PoC_CyberArk` | The extension this file belongs to | Resolve the existing extension with that `schema.namespace` |
| `version: v1.0.0` | The version of this analysis file | Consumers' query version requirements are compared with this value |
| `extension-versions: >=v1.0.0,<v2.0.0` | Optional limits on the associated extension versions this analysis supports | Compare the installed `schema.version` with these conditions |

An analysis at version `v1.0.0` can therefore remain eligible after its extension changes to `v1.1.0`, provided any declared extension range allows that version and the other checks pass. The two version numbers need not match. Changing a schema's display information does not by itself require changing every analysis file's version.

Check these rules before activation:

- The required namespace and version headers occur exactly once and contain valid values. The optional extension compatibility header occurs at most once and cannot be empty when present. Recognize headers only as the designated leading comments, never as text inside the query. Use complete `v`-prefixed semantic versions for analysis files. Preserve existing BloodHound schema version acceptance, including `1.0.0` without the prefix; normalize only for comparisons and report the stored spelling.
- The namespace matches one installed extension definition, and its schema version satisfies any declared `extension-versions` conditions. Resolve this through BloodHound's installed schema records, not a second schema upload on the rules page.
- If the schema is missing or its version is outside the supported range, preserve a new file as an inactive draft and explain the mismatch. Pause an already installed analysis that becomes incompatible. Do not silently attach it to another extension or rewrite its declarations.
- When the user uploads files from an extension-specific analysis view, verify that each file's namespace matches the selected extension. In the general rules upload, group files by their declared extension before review.
- Use the resolved extension record and its `schema.name` as the saved association. Preserve its namespace, the analysis's own version, its extension compatibility conditions, and the actual schema version used for each run. This keeps downloads, retained files, and later reinstalls understandable.
- A query belongs to its declared extension even if another extension consumes its answer or it produces a relationship type declared elsewhere. Keep existing output-kind and endpoint checks. Sharing an answer does not transfer the file's association.

Use extension identity plus query name when addressing a saved rule. The query name is its filename without `.cypher`. Include the analysis version and saved revision when identifying a particular set of contents. Resolve queries to these identities throughout ordering, temporary answer storage, reports, downloads, overwrite choices, enable/disable controls, and deletion. A query name or answer alias alone must never select a query globally.

Keep one installed schema version per extension and one active version of each analysis, with inactive drafts for proposed changes. Different analyses within an extension may have different versions. Supporting several simultaneously active versions of the same query or schema is outside this plan.

## Reference other calculations explicitly

Use a two-part reference: namespace plus query name, for example `PoC_AD/users`. All query dependencies refer to `.cypher` files, so the suffix adds no information. `query:PoC_AD/users` identifies the file `users.cypher` associated with `PoC_AD`. Keep the suffix on actual files and downloads. The slash separates two names; it does not give the query permission to read a filesystem path.

OpenGraph's `Namespace_Name` convention continues to apply to graph kinds. Keeping the query namespace separate from its name avoids guessing where a namespace ends when both names contain underscores. The documented OpenGraph schema does not currently specify this analysis reference syntax.

| Reference | Meaning | Parameter received by the query |
| --- | --- | --- |
| `query:users` | The latest activated `users.cypher` in this extension | `$users` |
| `query:PoC_AD/users` | The latest activated Directory query, with no version restriction | `$users` |
| `query:PoC_AD/users>=v1.0.3` | The Directory query at analysis version `v1.0.3` or later | `$users` |
| `query:PoC_AD/users==v1.0.0` | Exactly analysis version `v1.0.0` of the Directory query | `$users` |
| `query:PoC_AD/users>=v1.0.3,<v2.0.0 as directory_users` | The Directory query within the specified analysis version range | `$directory_users` |
| `query:PoC_Vault/users>=v1.0.0 as vault_users` | A separate query with the same name | `$vault_users` |

For example, a CyberArk query could begin:

```cypher
// namespace: PoC_CyberArk
// version: v1.0.0
// extension-versions: >=v1.0.0,<v2.0.0
// depends-on: query:PoC_AD/users>=v1.0.3,<v2.0.0 as directory_users
// depends-on: node:PoC_CyberArk_Account, node:User

UNWIND $directory_users AS directory_user
```

This is a declaration example, not a complete producing query. Its body would continue with the author's matching logic and output.

Reference rules:

- A short reference always stays within the current extension. Never search other extensions as a fallback. Both local and external references may omit version conditions; omission means latest, not an implicit exact version or same-major-version restriction.
- A reference to another extension names its namespace. Optional version conditions are compared with the referenced file's own `version` header. Check any `extension-versions` conditions on that file separately against its associated schema.
- Use the query name as the default parameter, such as `$users`. An optional `as directory_users` changes the parameter to `$directory_users`. Namespaces and version comparisons never become part of a Cypher parameter name.
- Aliases must be valid Cypher parameter names and unique within the consuming query. If two dependencies would both supply `$users`, require the author to give them distinct aliases; do not invent names or overwrite an answer.
- Multiple `depends-on` lines add dependencies to the same list. Write one query requirement per line, following the one-requirement-per-line style. Commas on that line combine version conditions for that query. Keep comma-separated graph inputs such as `node:User, node:Group` supported on their own lines. Do not mix a query requirement and graph inputs on the same line; errors should show how to split them into separate lines.
- The producer runs once per analysis run, even if consumers choose different aliases or specify different ranges that all accept its active version. Its full identity identifies the stored answer; each consumer receives that answer under its own alias.
- Reject missing prerequisites, unsatisfied version conditions, malformed comparisons, ambiguous declarations, duplicate query entries, repeated conflicting aliases, self-dependencies, and dependency loops before activation. Separate example lines expressing alternatives must not be read as permission to install two versions of a query.
- Keep graph dependencies such as `node:User` and `relationship:MemberOf` unchanged. They describe graph types rather than another query's returned rows.
- Update optional reachability, overlap, and batching instructions to resolve the same query identities and aliases wherever they refer to prerequisite answers. Preserve their calculation behavior.

## Compare compatible versions

Follow the name-and-optional-conditions style shown in [pip's requirements file examples](https://pip.pypa.io/en/stable/reference/requirements-file-format/#example). Use commas between conditions, with optional surrounding spaces. All conditions must be true. Use the same notation for query requirements and the optional `extension-versions` header.

Support the common [Python version comparison forms](https://packaging.python.org/en/latest/specifications/version-specifiers/#version-specifiers): `==`, `!=`, `>`, `>=`, `<`, `<=`, `~=`, and trailing `.*` with `==` or `!=`. Retain OpenGraph's concrete version format. This adopts familiar requirement notation; it does not introduce Python package installation, environment markers, URLs, or pip command options into query comments.

| Condition | Meaning |
| --- | --- |
| Omitted | Use the latest activated version available locally |
| `>=v1.0.3` | Version `v1.0.3` or later, with no upper bound |
| `==v1.0.0` | Exactly version `v1.0.0` |
| `>v1.0.3` | Later than `v1.0.3` |
| `<=v1.4.0` | Version `v1.4.0` or earlier |
| `>=v1.0.3,<v2.0.0` | At least `v1.0.3` and earlier than `v2.0.0` |
| `>=v1.0.3,<v2.0.0,!=v1.2.0` | The same range, excluding `v1.2.0` |
| `~=v1.0.3` | At least `v1.0.3` and earlier than `v1.1.0` |
| `==v1.*` | A version in the `v1` series |
| `!=v1.2.*` | Exclude the `v1.2` series |

An unbounded condition such as `>=v1.0.3` also accepts a stable `v2.0.0`. Use an explicit upper bound when a future major release should require review. Do not silently add a bound the author did not write.

Compare versions by semantic version ordering, not alphabetically: `v1.10.0` is later than `v1.9.0`. Use a single tested version-comparison implementation and verify that its interpretation of `~=` and wildcard conditions matches the documented examples. With explicit conditions, prereleases require a prerelease in at least one condition. Omitted conditions accept the reviewed active version, including a prerelease. Build metadata does not affect ordering or equality. Shortened conditions are supported only for `~=` and the documented wildcards. Every concrete installed version still follows OpenGraph's version validation. Do not add `^`, bare `~`, arbitrary string equality `===`, or OR expressions in this change.

Here, latest means the latest activated version available in this BloodHound installation. Drafts, disabled queries, deleted queries, and retained historical copies are not alternative versions to select. A reviewed replacement becomes the version used by requirements without conditions on the next run; an already running analysis keeps the exact contents it started with. A deliberate rollback likewise changes the active version for future runs.

Conditions check the one active or proposed version of the referenced query. They do not download a dependency, activate an old file, or select a different provider version for each consumer. If no proposed set can satisfy all affected consumers, keep the candidate inactive and show which requirements conflict. A missing or disabled provider still prevents its consumers from running even when they have no version condition.

Every run records the exact query versions, schema versions, and saved revision it actually used. A range is a compatibility claim by the author, not proof that the calculation still returns the expected answer. Keep shared-answer validation and written expected-result tests.

### What happens during an upgrade

- **Dependency without a version condition:** `query:PoC_AD/users` follows a reviewed, activated replacement of Directory's query without editing the consumer. This includes a major-version update; other validation still applies. A newer uploaded draft alone has no effect.
- **Schema update within the supported range:** CyberArk changes from `v1.0.0` to `v1.1.0`. Its analysis remains at `v1.0.0` and supports `>=v1.0.0,<v2.0.0`. It stays eligible without editing its comments. A consumer requiring that analysis at `==v1.0.0` also remains satisfied, because the query itself did not change.
- **Analysis update within a consumer's range:** Directory's `users.cypher` changes from `v1.0.3` to `v1.1.0`. A consumer requiring `>=v1.0.3,<v2.0.0` remains eligible if the other checks pass. Review covers affected consumers, and normal change selection schedules their recalculation.
- **Analysis update outside a consumer's range:** The same consumer does not accept a proposed `v2.0.0` of `users.cypher`. Keep the incompatible candidate inactive until the consumer is updated or explicitly paused as part of a reviewed change. An exact `==v1.0.0` requirement likewise rejects every other analysis version.
- **Schema update outside an analysis's range:** A schema-only update to CyberArk `v2.0.0` can proceed through the existing workflow, but its incompatible analyses and dependent calculations pause. Keep their files and previous graph results, explain that results may be outdated, and require compatible declarations and review before resuming.

## Describe shared answers without duplicating the graph schema

Extend TODO 6's shared-answer work to describe the column names, meanings, value types, and missing-value rules promised to consumers. This describes temporary query results, not another declaration of node or relationship kinds.

Approved approach: use descriptive Cypher RETURN columns and ordinary documentation. Do not add type comments, mandatory `row`/`node`/`relationship` aliases, or separate description files. A dependency alias names the consumer parameter; ordinary UNWIND/WITH names values inside the consumer. Check direct column references when they can be established reliably. Value types, meaning, null handling and missing-value expectations still need prose and execution tests. Graph objects cannot be passed as shared answers; return plain IDs and values.

At activation, check accepted producer analysis versions and declared answer compatibility wherever possible. At execution, validate returned rows before handing them to consumers. Include empty results, null values, lists, and nested values in the design. An empty answer must still have a known declared shape when consumers depend on that declaration.

A changed column meaning cannot generally be proved safe from its name or type. Small written examples with expected answers remain necessary. Avoid promising that activation detects every incompatible query change.

## Upload schemas and analyses on their respective pages

Keep schema uploads on OpenGraph Management. Continue using the current rules page for individual or multiple `.cypher` files, associated through their declarations, including during TODO 3's read-only MVP. Move those analysis controls in the later workspace management phase, when replacement controls are ready. A combined schema-and-analysis ZIP and analysis uploads through OpenGraph Management are outside this plan.

The current experimental rules bundle includes schema files, and the worker can register those schemas. Change that path to obtain the installed schema definitions from BloodHound through an authenticated read. Users must not need to upload the same schema to both pages. Any local copy used for validation is an internal snapshot tied to the installed schema revision, not an independent installation decision.

Preserve the definition information that calculations need, including the existing experimental `compatible_kinds` declarations. Check how the schema upload stores these proposed fields and how the rules service can read them before replacing the current bundle path. Extend the definition storage/read path where necessary without discarding standard sections or inventing new required author fields. A missing required declaration must be reported; never replace it with a guessed endpoint scope or silently use an older uploaded copy.

Schema installation and analysis activation are separate actions. A query may arrive before its schema as an inactive draft. A file upload changes only the supplied analyses; show retained files explicitly and require an explicit removal choice for omitted analyses. A schema uploaded later makes a complete draft eligible for review, not automatic activation.

When the installed schema version changes, recheck any retained analyses' extension compatibility conditions. Files without conditions follow the latest installed schema; files with satisfied conditions remain eligible. Neither case requires matching the analysis version to the schema version or rewriting comments. Analyses with different own versions can coexist when their declared conditions are satisfied. An incompatible schema update pauses affected analyses and explains why on the rules page, while preserving the existing schema workflow.

Use the existing permissions for each page and its server routes. Analysis review and execution must verify the installed schema state again, so changing the schema after a preview cannot activate an outdated calculation plan. A failed or unavailable schema read must prevent new activation or calculation from guessing what is installed; preserve the previous saved graph results.

Bound analysis counts, file sizes, total saved bytes, and execution resources separately. Replace the current blanket 32-file limit only with measured and documented limits. An optional archive for sharing analyses can be designed later; it does not require extending the schema upload format. If archives are introduced, agree on their layout and apply decompressed-size and path checks in the analysis upload service.

On the analysis page, show the associated installed schema version, active analysis revision, drafts, and last successfully applied graph results separately. Review and activation must reject an outdated browser view or a dependency that changed after review.

## Delete an extension with an explicit analysis choice

For the eventual extension-deletion integration, extend OpenGraph Management's existing Delete Extension confirmation with an unchecked-by-default option:

**Also delete associated analyses**

Show the associated files with their analysis versions, compatible extension versions, and status, including active, disabled, and draft analyses. The choice applies to files associated with the selected extension, including retained versions, not just files whose names happen to match. Downloading them should be available before deletion.

| Choice | Associated files | Analyses in other extensions | Existing graph results |
| --- | --- | --- | --- |
| Keep analyses | Retain the files and previous association; pause them because the extension is absent | Keep files and pause calculations whose required answers or type definitions are missing | Preserve results while their calculations are paused |
| Also delete associated analyses | Delete the selected extension's saved analysis files and drafts | Keep their files; show and pause affected consumers | Apply eligible relationship cleanup after the next successful analysis |

These are proposed extension-deletion rules. Today's individual-rule deletion can also delete dependent files. The new extension option should not silently delete another extension's files. Show that difference in the review and documentation; any additional file deletion requires its own explicit selection and review.

Check all forms of dependency: directly consumed answers, downstream consumers, graph reads of calculated kinds, helper kinds, and graph-type definitions removed with the schema. Independent rules continue. Preserve the existing protection that pauses all producers sharing an output when refreshing only part of their combined answer could remove valid results.

Keeping analyses must not let the worker reinstall the deleted schema. The current worker registers schemas from its saved bundle before running. Replace that behavior when connecting the rules page to installed schemas: schema installation and deletion decisions come from BloodHound's schema workflow, while analysis activation comes from the rules page and eventually TODO 3's page. Refreshing, restarting, or retrying an old run must not recreate an extension. This protection is needed before the optional analysis-deletion control is added.

Retained files must remain visible and downloadable on the rules page, and later TODO 3's page, after their extension disappears. Preserve the former extension name and namespace, each analysis's version and compatibility conditions, and the last installed schema version even if its database record is deleted. Reinstalling the same extension at a compatible version may make those files eligible for review, but does not automatically reactivate them. Do not attach them automatically to an unrelated extension that later uses the same namespace.

Deleting analysis files is distinct from deleting collected graph objects or helper objects. Preserve BloodHound's existing extension deletion behavior and the engine's existing scoped relationship cleanup. Several producers may still support a relationship, and paused groups must retain their results. Cleanup must remain pending when its prerequisites cannot safely be satisfied.

Retain enough internal information about retired relationship kinds and allowed endpoint pairs to perform cleanup after the schema or last query has been removed. This information is cleanup history, not an installed extension. Support a cleanup run when no active queries remain. Save cleanup and the successful-run record together, and preserve previous graph results if calculation or saving fails.

## Keep installation and execution consistent

Keep the current rules service and file storage for this work. BloodHound's schema records decide which extensions are installed; the rules service stores analysis files, drafts, active revisions, paused state, and pending cleanup. Validate their association against the installed definitions before activation and execution. Changing the page used for analysis management does not itself require moving this storage.

For later application integration, storing analysis records in BloodHound's application database remains an option. It would allow extension deletion and its analysis-removal decisions to save together while users continue to use separate pages. Decide whether that move is necessary before implementing it; it is not a prerequisite for TODO 6 or the current rules-page workflow. If adopted, preserve the original experimental data and import files/settings explicitly through validation.

Association records must survive schema deletion when the user chooses to retain analyses. Avoid any automatic deletion rule that erases them regardless of that choice. Audit records may preserve the action and file identifiers without retaining deleted query text unnecessarily. Keep one source of truth for installed schemas and one for active analyses; internal snapshots cannot install schemas or activate files.

Coordinate activation and deletion with running analysis. Every run uses one fixed set of schemas, queries, and dependency versions. For deletion, wait for the current run to finish before completing the change, or cancel it and confirm that unfinished graph writes were discarded. Once deletion reports success, an old run must not publish results or register the removed extension. Recheck the reviewed state after waiting.

The eventual optional deletion control must safely coordinate schema deletion, analysis-file removal or retention, and pending cleanup. When they use separate stores, a crash between saves must leave a recorded, recoverable operation and keep affected calculations paused; it must not report complete success while leaving executable orphaned analyses. Settle and test that operation before exposing the control. Graph-kind refresh and graph calculation remain separate work. Report and retry failures honestly; never report that analysis results are current merely because files were saved. A failed refresh must not allow execution against outdated extension state.

Keep existing schema API requests and responses working. Read installed definitions and their versions through authenticated server routes; analysis uploads continue through the rules service. An existing extension deletion call without an analysis choice preserves files and makes affected calculations ineligible. The eventual optional deletion operation requires review confirmation before removing analysis files. All entry points must enforce the same membership and pause rules, including schema changes performed outside the browser.

## Small implementation changes

Each change must include its focused tests, necessary generated files, plain-English comments, and corresponding documentation. Namespace/version declarations, compatible comparisons, and descriptive Cypher columns with ordinary documentation are approved. The progress checklist below distinguishes implemented code from unfinished verification; it does not certify that every required check has passed.

- [x] Steps 1–5: implement file associations, separate filenames by extension, resolve dependencies and aliases, check shared columns, and read installed schemas. Focused tests pass.
- [x] Step 6: update the current rules-page code, maintained examples, and documentation.
- [ ] Step 6: verify the complete workflow through real OpenGraph Management uploads, the rules page, and analysis execution. Cover three independently installed extensions with matching filenames, either upload order, updates, rule deletion, and schema deletion with retained files. Current browser checks use a simulated schema reader; native persistence and graph execution were tested separately.
- [ ] Investigate the UI failures from `CI=true just test`, rerun the checks, and record whether any remaining failures also occur without this change.
- [ ] Complete `just prepare-for-codereview` in a clean checkout without ignored lab history. The prior run stopped during recursive formatting before later checks finished.
- [ ] Run `just gen-spec` with the required generator available and review its output. The locally updated OpenAPI JSON passes parsing and reference checks, but standard generation has not succeeded.
- [ ] Review the complete patch and address findings before closing TODO 6.
- [ ] Step 7 — deferred: implement the optional analysis-deletion control and its coordination and cleanup requirements.
- [ ] Step 8 — deferred: move analysis controls into the later TODO 3 workspace management phase.

1. **Read and validate file association.** Parse namespace, analysis version, and supported extension versions. Implement the shared version comparisons, associate every query with its defining extension, and update maintained examples and test-file generators. Replace the current practice of inferring a shared query's extension from its consumers. Do not edit preserved evidence or historical snapshots. Missing or conflicting metadata in a candidate must leave the previous active set untouched.
2. **Separate filenames by extension throughout the rules service.** Change stored file identities, uploads, drafts, revisions, dates, reports, downloads, and rule controls together. Three extensions may each supply `users.cypher`; schema filenames belong to the separate schema upload workflow. Convert the existing experimental set through an explicit reviewed step; refuse ambiguous associations instead of guessing.
3. **Resolve shared dependencies and local aliases.** Implement namespace/query references without `.cypher`, optional requirements-style version conditions, and optional aliases. Omitted conditions follow the latest activated provider. Accept repeated `depends-on` lines, one query requirement per line, and commas between that query's conditions. Keep graph dependency lists on separate lines. Use full identities throughout ordering and answer storage. Update advanced controls and the cross-extension example in the same change. Verify one shared calculation runs once and its consumers receive the correct answer.
4. **Check shared answer expectations.** Use the approved RETURN-column checks and execution row checks, with prose describing types and meanings. Publish identity-matching and membership examples with written expected results. Explain which compatibility questions still need execution or author review.
5. **Use installed schemas from the current rules page.** Provide the authenticated definition read, preserving the standard and experimental declaration information the engine needs. Connect schema changes to membership checks and safe pauses. Replace runtime schema reinstallation with use of installed state, and remove the need for a second schema upload to the rules page. Coordinate schema changes with running analysis before enabling this path. Keep the existing analysis store.
6. **Finish the current workflow and documentation.** Keep analysis upload, drafts, review, downloads, and activation on the rules page. Test three independent schema uploads through OpenGraph Management and their analysis uploads through the rules page. Cover either arrival order, version updates, and deleting a schema while its files remain. Document the removal of the global filename restriction and keep future features clearly marked.

The following work is deferred until the corresponding application integration is ready:

7. **Add the optional analysis deletion control.** First settle storage coordination and add cleanup-only execution. Then show the exact affected files, consumers, pause effects, and pending cleanup in OpenGraph Management's existing extension-deletion dialog. Verify both keep and delete choices through the real application routes and UI. This does not add analysis upload to that page.
8. **Move analysis controls after TODO 3's read-only MVP.** The MVP links to the current rules page. In the later management phase, reuse the working upload, draft, review, activation, download, and version checks in the workspace. Its grouping, live reporting, and testing follow in separate phases. Update walkthroughs and repeat the user-flow checks with schema uploads still separate. A move to application-database storage is a separate decision, not a prerequisite for moving the controls.

## Required checks

| Area | Cases that must work |
| --- | --- |
| Page responsibilities | Schema uploads use OpenGraph Management; analysis uploads and controls use the current rules page; no duplicate schema upload is required; the eventual TODO 3 page reuses those analysis controls |
| Installed schema reads | Authentication and permission failures; missing or unavailable schema; complete standard and experimental declarations; stale snapshots rejected after schema update/deletion; no runtime schema reinstallation |
| File association | Missing or invalid required namespace/version headers; omitted optional compatibility header uses the latest schema; duplicated, empty, malformed, or unsatisfied compatibility conditions; query and schema version numbers differ validly; intermediate queries associated with their author even when shared |
| Independent installation | Three extensions installed at different times with identical schema/query filenames; updating, disabling, downloading, or deleting one never selects another by filename |
| Shared answers | References without `.cypher`; local and external omission both mean latest; default parameters and optional aliases; repeated dependency lines; one query requirement per line; comma-separated version conditions with optional spaces; graph lists on separate lines; two same-named queries with distinct aliases; missing or disabled providers; dependency loops; duplicate entries and aliases; one calculation reused across overlapping accepted ranges |
| Version comparisons | All listed operators; inclusive/exclusive boundaries; comma-combined bounds; excluded versions; `~=` and wildcard examples; invalid and impossible conditions; numeric ordering such as `v1.10.0` versus `v1.9.0`; prereleases and build metadata; unbounded ranges accepting later major releases; conflicting consumers |
| Updates | No condition follows the next activated provider, including a major update; drafts and historical files are never selected as latest; compatible schema upgrade needs no query edits; compatible query upgrade needs no consumer edits; exact comparison rejects another query version; schema update on its own page pauses incompatible analyses; mixed analysis versions accepted when compatible; incompatible analysis upload remains inactive; omitted analyses retained; stale reviews rejected; old drafts cannot restore deleted files |
| Deletion | Schema deletion pauses affected calculations while files stay available on the rules page; no schema is recreated; eventual keep/delete choices cover active, disabled, and draft files, direct and indirect consumers, graph-type readers, shared producers, recoverable partial failures, retained downloads, and explicit review after reinstall |
| Execution during changes | Running analysis keeps its exact starting versions and saved revision even when ranges accept several versions; activation, deletion, restart, and retry cannot switch files mid-run, reinstall an extension, or publish deleted analysis after deletion completes |
| Graph results | Empty answers; removal of one or the last supporting producer; preserved results while paused; failure before saving; late save failure; cleanup with zero active queries; helper objects and collected data retained |
| Failure handling | Unauthorized uploads and deletion; failed schema read, file save, database save, or graph-kind refresh; oversized files or upload batches; invalid paths; interrupted conversion; safe errors without secrets, complete queries, or raw graph identifiers |
| Existing behavior | Schema-only JSON; currently supported ZIP components; saved queries and privilege-zone rules; existing API callers; built-in protections; unaffected native BloodHound analysis |

Run focused tests for each change and `just test` for BloodHound changes. Run `just prepare-for-codereview` before each manual commit and record what it actually ran. Test graph-result behavior on both Neo4j and PostgreSQL. No DAWGS changes are expected; if query meaning changes there, include both-database tests and generated SQL examples, and run `make format`.

Use isolated databases for migrations and graph comparisons. Record new results separately with source versions and database state. Preserve historical evidence exactly. Do not repeat earlier performance studies unless these changes introduce a specific performance question.

## Code and documentation to start from

| Responsibility | Starting files |
| --- | --- |
| File declarations, association, and query ordering | [extension.go](../tools/esc1-poc/internal/extension/extension.go), [registry.go](../tools/esc1-poc/internal/extension/registry.go), [definition.go](../tools/esc1-poc/internal/extension/definition.go) |
| Current files, limits, drafts, and reviewed changes | [validation.go](../tools/esc1-poc/internal/ruleservice/validation.go), [draft.go](../tools/esc1-poc/internal/ruleservice/draft.go), [changes.go](../tools/esc1-poc/internal/ruleservice/changes.go), [view.go](../tools/esc1-poc/internal/ruleservice/view.go) |
| Schema registration and analysis coordination | [postprocessing/service.go](../tools/esc1-poc/postprocessing/service.go), [postprocessing/gate.go](../tools/esc1-poc/postprocessing/gate.go), [runner](../tools/esc1-poc/internal/runner) |
| Existing schema upload and extension records | [opengraphschema.go](../cmd/api/src/api/v2/opengraphschema.go), [schema model](../cmd/api/src/model/graphschema.go), [extension service](../cmd/api/src/services/opengraphschema/extension.go), [schema database operations](../cmd/api/src/database/graphschema.go) |
| Current analysis interface and its future replacement | [rules page](../tools/esc1-poc/internal/ruleservice/upload.html), [workspace plan](opengraph-analysis-workspace.md), [TODO 3](../tools/esc1-poc/TODO.md#3-build-an-interactive-analysis-workspace-using-bloodhounds-sequential-layout) |
| Existing schema upload and extension deletion controls | [SchemaUploadDialog](../packages/javascript/bh-shared-ui/src/components/SchemaUploadDialog), [OpenGraphManagement](../packages/javascript/bh-shared-ui/src/views/OpenGraphManagement) |
| Existing example and author guidance | [cross-extension example](../tools/esc1-poc/examples/cross-platform/README.md), [DEVELOPERS.md](../tools/esc1-poc/DEVELOPERS.md), [END_USERS.md](../tools/esc1-poc/END_USERS.md), [USAGE.md](../tools/esc1-poc/USAGE.md) |
| Design and review record | [DESIGN.md](../tools/esc1-poc/DESIGN.md), [REVIEW.md](../tools/esc1-poc/REVIEW.md), [HANDOFF.md](../tools/esc1-poc/HANDOFF.md), [migration plan](../tools/esc1-poc/OPENGRAPH_ANALYSIS_MIGRATION_PLAN.md) |

Update DESIGN.md as the workflow is implemented: schemas use OpenGraph Management, analyses use the current rules page, and TODO 3 supplies their eventual execution graph page. Update the migration plan's application-integration steps when that move is scheduled; revise storage steps only if a storage change is chosen. Include API documentation and generated clients/mocks as required. Any necessary migrations must follow the repository's naming and goose rules; leave existing baseline and merged migrations intact.

## Decisions and remaining scope

- Implemented headers: required `namespace` and `version`; optional `extension-versions`. Dependencies use requirements-style conditions and latest active when omitted.
- Shared results use Cypher columns and ordinary documentation. Unknown or complicated projections are not presented as statically verified. Actual rows must contain plain values; known columns are checked before another query receives them. Types are not inferred or converted. A valid empty result can remove a selected output's previous relationships after a successful run.
- Keep the schema parser in `tools/esc1-poc` for this experiment. BloodHound preserves accepted definitions and supplies an authenticated read; it remains the authority for installed schemas. Moving analysis validation into its schema-management code comes later.
- Existing native uploads without analysis endpoint pairs remain valid. Only analyses that create relationships require those pairs. Existing installations without a preserved definition must re-upload the schema through OpenGraph Management before associating analyses.
- Before the eventual deletion integration, confirm defaults and storage coordination, including retention and pausing of other extensions' consumers. Analysis archives and moving storage remain separate optional decisions.

TODO 3 owns the future execution graph page and the move of analysis uploads and controls into it. It is not required to complete TODO 6 using the current rules page. Automatic dependency downloads, multiple simultaneously active versions of the same query or schema, changes to collected-data deletion, new Cypher language features, and new performance studies remain separate work.

## Completion criteria

TODO 6 remains open until the current-scope workflow verification, repository checks, generated documentation, and final review in the checklist above are finished. Passing focused tests alone does not complete these steps. Deferred steps 7–8 remain unfinished parts of this broader plan and are not prerequisites for closing the current TODO 6 scope.

For TODO 6, a user can upload three independently supplied schemas through OpenGraph Management, upload and manage their analyses through the current rules page, and update one extension's files without overwriting another's. Users do not rename files or upload schemas twice. Every analysis has an unambiguous extension, its own version, and optional extension compatibility conditions. Dependencies omit the `.cypher` suffix and use requirements-style version conditions; without conditions, they follow the latest activated provider. Compatible upgrades do not require editing unchanged queries or their consumers. Missing or incompatible schemas and dependencies pause affected work with a useful explanation, and failures preserve the previous saved graph answer. Ordinary OpenGraph schema workflows continue to work.

For the later user interface, TODO 3 first provides read-only inspection of current rules and matching completed reports. Analysis upload and management move into the workspace in the following phase using the same checks. The eventual OpenGraph Management deletion dialog offers the explicit choice about associated analysis files. These future controls are not required to finish the current rules-page work or TODO 3's read-only MVP.
