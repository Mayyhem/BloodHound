# Executive summary

Making BloodHound extensions easier to build could reduce development effort, expand security coverage, and help customers get useful results sooner. Researchers should be able to collect information and describe the dangerous connections to look for. BloodHound should handle the order of the work and keep the results current.

This project lets an extension author describe those connections in readable `.cypher` rule files. For example: **“If an account in this vault has the same directory identifier as an Active Directory user, connect the vault to that user.”** This factual association appears in BloodHound for inspection. It does not establish an attack path, so pathfinding cannot follow it.

Complex analyses can also use ordinary Cypher: the [complete ESC1 example](examples/esc1-cypher/README.md) works without special processing instructions. Experienced authors can choose [optional advanced controls](ADVANCED_CONTROLS.md). The [comparison report](CYPHER_COMPARISON.md) checks exact answers and directly compares each choice on both databases through one million objects. The instructions remain optional, with measured differences in speed and input capacity.

You install two parts once: an extra container that stores the rules, and a modified BloodHound application that runs them. Later, you can upload new rules without rebuilding or restarting BloodHound. This is an experimental Community Edition feature; it still needs review and testing before production use.

## What using it looks like

1. Upload schemas through BloodHound’s OpenGraph Management page. On the local rule page, inspect, review, and activate their `.cypher` files. The supplied Compose setup needs no pasted browser token.
2. Upload collection data to BloodHound as usual, or click **Run Analysis** to check data already stored. The button requires a one-time server connection setup. Both ordinary BloodHound and OpenGraph collections are supported.
3. After BloodHound successfully finishes its own analysis, it runs the affected rules and saves their calculated connections.
4. Inspect current rules and completed reports in **Administration → OpenGraph Analysis**, then explore calculated connections in BloodHound’s normal interface.

Later collections can change the answer. A vault account collected before its matching directory user can be connected after that user arrives, without recollecting the vault. The engine also removes calculated connections that are no longer justified by stored facts. Simply leaving an object out of a later collection does not establish that it was removed; stale collected evidence remains a separate problem.

Uploading or removing rules alone does not change the graph. Changes, including cleanup of retired connection scopes, apply after the next successful BloodHound analysis. One table lists rules from every extension, with sortable columns for order, name, stage, date added, and extension. Filter by extension, expand a rule for its query and dependencies, or select rules to delete, disable, or enable. Disabling preserves files and existing results while pausing other producers of the same relationship type and dependent rules. [The usage guide](USAGE.md#change-the-active-rules) explains these controls.

## What changes in BloodHound

**BloodHound gains a step that runs your rules after its own analysis.** It tracks changed object and connection types to choose affected rules and reuse their shared calculations. Native BloodHound analysis still runs normally. It also makes other changes to the graph wait while both sets of calculations finish. This keeps the information being checked from changing halfway through the work.

People can still search and view BloodHound. Upload processing, deletion, and other graph changes may wait; searches may slow down when the database is busy. The rule container alone cannot arrange this, which is why this version requires a modified BloodHound application.

Existing settings give BloodHound the rule container's address and access secret. Leaving the address empty disables automatic rule processing. The [BloodHound changes explained](DESIGN.md#what-changed-in-bloodhound) and [review guide](REVIEW.md) identify the affected code and the separate changes to its database library, DAWGS.

## The main safeguards and limits

- The runner combines every producer’s complete answer before refreshing a relationship type within its declared endpoint scope. Existing edges in that scope participate regardless of their origin. A failed calculation before saving preserves the previous results. When the failure belongs to one step, that step and the steps tied to it are set aside (keeping their previous results) and everything else is saved in a new attempt; the run is reported as partial.
- File checks parse complete queries, suggest missing input declarations, and distinguish those checks from actual database execution.
- Execution reports show query failures, counts, timings, save outcomes, the last confirmed saved revision, and up to 20 recent attempts.
- Uploads are saved as inactive drafts. Missing schemas can be installed through OpenGraph Management later; the complete set must validate and be reviewed before activation.
- Each producing query names its edge with a literal `kind` column. Schemas describe types and directed `compatible_kinds` pairs; they need no `post_processing` blocks.
- Each query can choose whether a matching connection is replaced, has its supplied details updated, or is kept unchanged. Replacement is the default.
- Each file declares its extension namespace and its own version. Query dependencies support compatible version conditions and local aliases; different extensions can use identical filenames. Each extension has its own schema. Several queries can produce the same relationship type; their policies must agree. All uploaded schemas share one dependency order, and queries can pass calculated rows across extension boundaries. These settings are **proposed additions to OpenGraph**, not an accepted part of its official definition.
- Both Neo4j and PostgreSQL have laboratory test evidence. This coordinates one modified Community Edition application; other applications or direct database writers are outside its protection.
- Up to five ready queries can run together. A ready query can start without waiting for an unrelated slow query. Queries run one at a time after supporting objects are created, so they can see those new objects before saving.
- First runs, changed rules, restarts, and incomplete change tracking run all enabled rules. Tracking is bounded, so large uploads can also require a full run of enabled rules.
- Large-graph safeguards are implemented. The [Cypher comparison](CYPHER_COMPARISON.md) reports measured object and connection counts separately, along with remaining limits. Capacity at 100 million objects and Enterprise compatibility have not been demonstrated. The [review guide](REVIEW.md) includes the checks and unresolved work.
- **No BloodHound Enterprise analysis features.** This solution does not account for privileged zone (Tier Zero) rules, findings, exposure or impact analysis, or other BloodHound Enterprise capabilities. Calculated connections are ordinary graph edges; they are not evaluated against zone membership, and no findings or attack-path exposure are produced from them.

## Where to go next

| I want to… | Read |
| --- | --- |
| Use supplied extensions and understand what happens after uploads or rule changes | [End-user questions and answers](END_USERS.md) |
| Write my first extension or look up the authoring rules | [Developer guide and quick start](DEVELOPERS.md) |
| Extend the starter with shared answers, helpers, and properties | [Step-by-step examples](DEVELOPERS.md#continue-with-the-same-example) |
| Review cheaper preparation checks and the reachability investigation | [Focused follow-up and findings](PREPARATION_AND_REACHABILITY.md) |
| Review planned improvements to extension development | [Developer experience backlog](TODO.md) |
| Read the proposal and how the pilot addresses each problem | [OpenGraph analysis proposal](PROPOSAL.md) |
| Start it, upload rules, and find the results | [Usage guide](USAGE.md) |
| Understand the decisions and how it works | [Design guide and decision table](DESIGN.md) |
| Review the planned Cypher simplification and large-data comparison | [Cypher implementation plan](CYPHER_IMPLEMENTATION_PLAN.md) |
| Review the code, tests, or purpose of any file | [Review guide and complete file inventory](REVIEW.md) |
| Try two extensions sharing a calculation | [Synthetic Active Directory and CyberArk example](examples/cross-platform/README.md) |
| See the connections described by an example | [ESC1 model](model.html) or [vault model](examples/vault/model.html), opened in a browser |
| Continue the work or investigate an earlier experiment | [Handoff notes and historical evidence](HANDOFF.md) |

The native **OpenGraph Analysis** page uses the existing login for inspection, rule management, and the analysis file editor; [workspace instructions](USAGE.md#inspect-the-analysis-workspace) explain navigation, report matching, and downloads. The editor can test a file and its prerequisites without permanently changing the graph. The current setup keeps BloodHound running. The earlier standalone command that stopped the application is documented in the handoff notes for reproducing its experiments.

The native analysis workspace now includes the requested Explore-style canvas, resizable bottom rules/report panel, right entity panel, uploads and draft review, individual/multiple-rule enable/disable/delete, and Run Analysis. See [workspace usage](USAGE.md#inspect-the-analysis-workspace) for permissions and revision checks. Collapsed graph groups, historical diagrams, and live query progress remain later work. Temporary editor tests are available now. The older rules page remains available.

## 14 October 2026 — analysis file editor

The workspace now also has an analysis file editor. A signed-in person can save unfinished files as private working copies, resume them later, test one exact file without changing the graph permanently, inspect the returned rows as a table, review the proposed change, then activate only that file. The shared upload draft remains separate throughout.

Testing always recalculates only the needed query chain. A row-only file can now be reviewed and activated on its own. All graph writes, including temporary editor work, go through the same BloodHound process. The editor is production-built and deployed at <http://127.0.0.1:28080/ui/administration/opengraph-analysis>; real browser checks passed. See the [analysis workspace editor evidence](evidence/analysis-workspace-editor/README.md).
