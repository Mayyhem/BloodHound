# Build an extension

An extension describes **which connections should exist in BloodHound**. You provide a schema and read-only Cypher queries. BloodHound runs the queries after its own analysis, checks their answers, and saves the connections.

In this guide, a **node** is an object, a **relationship** is a connection, a **kind** is its type, and a **schema** describes those types. An object's `objectid` is its stable identifier.

Start with a query that returns `source` and `target` object IDs and a literal edge name as `kind`. You do not need to write Go, create relationships yourself, or use ESC1's special processing instructions for an ordinary extension.

This guide covers the current experimental architecture in `tools/esc1-poc`. It requires the modified BloodHound application and rule service described in [USAGE.md](USAGE.md). The schema additions described here are a proposal, not features supplied by an unmodified BloodHound installation.

## Find what you need

| I want to… | Go to… |
| --- | --- |
| Make my first working rule | [Quick start](#quick-start) |
| Extend that example one step at a time | [Continue with the same example](#continue-with-the-same-example) |
| Understand what runs where | [How a run works](#how-a-run-works) |
| See what I must choose and what BloodHound handles | [Author choices and automatic work](#author-choices-and-automatic-work) |
| Name and describe my extension | [Files and schema](#files-and-schema) |
| Reuse another query's answer | [Dependencies and shared queries](#dependencies-and-shared-queries) |
| Return edges, properties, or helper nodes | [What queries return](#what-queries-return) |
| Understand updates and deletion | [How saved results change](#how-saved-results-change) |
| Find out which Cypher I can use | [Cypher support and DAWGS](#cypher-support-and-dawgs) |
| Understand ESC1's comment instructions | [The current ESC1 exception](#the-current-esc1-exception) |
| Keep a large query practical | [Performance and limits](#performance-and-limits) |
| Test or debug a rule | [Testing and troubleshooting](#testing-and-troubleshooting) |

## Quick start

This example creates a separate, non-traversable connection for each **direct** `User → MemberOf → Group` relationship already in the graph. It is a learning example: it adds no new security finding and does not calculate nested membership.

You need a running development lab and AD collection data containing at least one direct user-to-group membership. If you need to build the lab first, follow [USAGE.md — Prepare a fresh lab](USAGE.md#prepare-a-fresh-lab), then return here. Writing and uploading rules does not require a local Go installation when someone has already provided the lab.

### 1. Create two files

Create a folder with these files. The schema describes the types; the query names the type it produces. There is no query filename or processing block in the schema.

```text
developer-start/
  getting_started.schema.json
  getting_started_memberships.cypher
```

Save this as **`getting_started.schema.json`**:

```json
{
  "schema": {
    "name": "getting_started",
    "display_name": "Getting started example",
    "version": "v1.0.0",
    "namespace": "PoC_Start"
  },
  "node_kinds": [
    {
      "name": "PoC_Start_Note",
      "display_name": "Example note",
      "description": "A custom type reserved for this example; the membership rule creates no nodes.",
      "is_display_kind": true,
      "icon": "file",
      "color": "#5965D8"
    }
  ],
  "relationship_kinds": [
    {
      "name": "PoC_Start_DirectMemberOf",
      "description": "Learning example: an existing direct user-to-group membership.",
      "is_traversable": false,
      "compatible_kinds": [
        {
          "source_kind": "User",
          "target_kind": "Group"
        }
      ]
    }
  ]
}
```

The `PoC_Start_Note` declaration is present because current BloodHound schema registration requires at least one custom node kind, even for a rule that only connects existing objects. Declaring this type creates no note objects. Your real extension can declare its collected or helper types instead.

Save this as **`getting_started_memberships.cypher`**:

```cypher
// depends-on: node:User, node:Group, relationship:MemberOf
// existing-edge-policy: keep-existing

MATCH (person:User)-[:MemberOf]->(group:Group)
RETURN DISTINCT person.objectid AS source, group.objectid AS target,
       'PoC_Start_DirectMemberOf' AS kind
```

The `depends-on` comment tells the runner when this rule needs to run. The query decides which connections should exist and names their kind. The schema describes that kind and its permitted endpoint types. The optional `existing-edge-policy` comment keeps an existing matching edge unchanged; omit it to use the default replacement policy. Every policy still removes in-scope edges absent from all producers’ complete successful answers.

### 2. Check the query and plan

Run the `MATCH` and `RETURN` in the lab's BloodHound query interface. Expect source and target IDs plus the same `kind` value for every direct user/group membership. No rows is a valid answer if your collection has none.

If you have the local Go tooling and dependencies from the setup guide, you can also check the saved files without connecting to a database. Run from `tools/esc1-poc`, substituting your folder's path:

```sh
./scripts/go.sh run . plan -extension /path/to/developer-start
```

Expect one planned query, `getting_started_memberships.cypher`, with role `relationship` and kind `PoC_Start_DirectMemberOf`.

This checks the schema, filenames, declared prerequisites, output declarations, and basic query restrictions. It **does not execute the query, validate all query syntax, or prove database compatibility**. It also does not establish that you declared every graph kind the query reads. The script uses locally cached dependencies and does not download missing packages. If your dependencies are in another cache, set `POC_MODULE_CACHE` to that directory as described in the setup guide.

### 3. Activate the two files

1. Open the lab's rule page at `http://127.0.0.1:18081/`.
2. Select the two files, then click **Save upload and review**.
3. Check the proposed rule and output kind. If replacing files, explicitly choose which files to overwrite.
4. Click **Activate reviewed revision**.

The supplied lab page needs no pasted token. Other installations may use a different address or require authentication; see [USAGE.md](USAGE.md).

**Activation saves the rules. It does not run them.**

### 4. Trigger a run and inspect the result

Upload a subsequent AD collection through BloodHound at `http://127.0.0.1:28080/`, or click **Run Analysis** on the rule page to check stored data if its server connection is configured. After successful native analysis, BloodHound runs the affected rules. [The usage guide](USAGE.md#run-analysis-without-another-collection-upload) explains the button and its setup.

On the rule page, click **Refresh rules and status**. Check that the report succeeded and names your active revision. A successful collection upload alone does not prove that your extension succeeded.

Inspect the saved connections in BloodHound:

```cypher
MATCH (person:User)-[:PoC_Start_DirectMemberOf]->(group:Group)
RETURN person.objectid AS source, group.objectid AS target
```

This is an inspection query, not a rule file to upload, so it needs no output declaration or dependency comment. The pairs should match the direct membership query. The original `MemberOf` relationships stay under BloodHound's control. Your extension calculates the separate `PoC_Start_DirectMemberOf` kind.

### Continue with the same example

Keep the `developer-start` folder and its schema. The following examples build on it in this order; each section names the file to add or replace.

| Step | Change | What to check after a successful run |
| --- | --- | --- |
| 1. [Quick start](#quick-start) | Create the schema and membership query above. | Every direct user-to-group membership has one example connection. |
| 2. [Shared answers](#pass-rows-from-one-query-to-another) | Add `getting_started_enabled_users.cypher` and replace the membership query. | Only users whose `enabled` property is `true` have example connections. |
| 3. [Helper objects](#helper-nodes) | Add `getting_started_notes.cypher`, using the already declared note kind. | Each native user has one separate note object; the membership rule is unchanged. |
| 4. [Relationship properties](#calculated-relationships) | Replace the membership query with the properties version. | The same enabled-user connections have an `evidence` property. |

These tutorial steps replace the earlier version of the same rule. Overwrite that file as directed. Several queries can intentionally produce the same relationship kind; their answers combine, so leaving an earlier tutorial rule active would keep its broader results. Check the updated folder with `plan`, then review and activate the changed files together. Activation still needs a later successful analysis before results change.

For a useful collected-data example, continue with the [vault extension](examples/vault/README.md). For two extensions sharing an answer, use the [AD/CyberArk example](examples/cross-platform/README.md). ESC1 is the advanced example, not the starting template for every rule.

## How a run works

```text
Collector uploads facts
    → BloodHound imports them and completes native analysis
    → BloodHound fetches one saved revision of the enabled rules
    → Queries run in dependency order
    → The runner checks the complete answers
    → Helpers, calculated connections, and the success record are saved together
```

| Part | Responsibility |
| --- | --- |
| Collector | Upload the facts: objects, properties, and observed relationships. |
| Extension author | Describe the model and write the calculation queries. |
| Rule service | Store schema/query files, review changes, and show the latest reported status. It does not run graph queries. |
| BloodHound | Run native analysis, execute extension queries, and save validated results. |
| DAWGS | Provide BloodHound's graph access and supported Cypher parsing/translation. |

The application holds other participating graph writers while native and extension analysis run. Searches remain available, although database load can make them slower. This coordinates one modified Community Edition process; separate applications and direct database writers are outside that coordination.

You can update a rule without rebuilding BloodHound. Adding a missing DAWGS language feature requires an application build and deployment before rules can use it.

### Author choices and automatic work

| You decide | BloodHound handles |
| --- | --- |
| The collected facts, their stable IDs, and the properties that connect different sources. | Importing those facts. Omission from a later upload does not automatically mean deletion. |
| Your schema's names, kinds, allowed endpoint pairs, presentation, and whether a connection is traversable. | Registering the display schema and checking calculated endpoints. |
| The complete answer your query should return, including its literal output kind and any properties. | Checking rows, combining identical answers, and saving combined results together. |
| All graph kinds read by the query and every prerequisite query it consumes. | Choosing affected queries, ordering their work, and sharing each prerequisite's answer within the run. |
| An optional `existing-edge-policy` comment on a relationship query. | Applying that policy and removing in-scope relationships no producer still returns. |
| Whether helper objects are needed, their declared kind, and a stable ID suffix for each object. | Deriving the ID prefix as the kind followed by `_`, checking object identity and type, and saving helpers with relationships. Helpers omitted later are retained. |
| Which changed files to review and activate. | Recording the revision and keeping an in-progress run on the revision it already fetched. |

For a new extension, you do not supply a reference commit or `post_processing` blocks. The platform supplies its reference settings. There is no graph ownership setting. It also sets execution limits and manages temporary answers. ESC1's special instructions are explained separately under [the current exception](#the-current-esc1-exception).

### Combining custom data with native data

Collect custom objects under your own kinds and IDs. Keep a linking property, such as an account's `user_sid`, and join it to the native user's `objectid` in your query. Do not give a custom OpenGraph object native `User` or `Base` labels to make it look like directory data.

If the account arrives before the directory user, a later run can connect them when both exist. Leaving an object out of a subsequent collection does not prove it was deleted: your query sees whatever facts remain in the graph. The [vault example](examples/vault/README.md) includes collections and queries showing the join.

## Files and schema

Use a distinct named schema, such as `my_extension.schema.json`, and descriptive `snake_case.cypher` filenames. `schema.json` is also accepted. The upload is a flat collection of files, not a directory tree.

**Query filenames are shared across all installed extensions.** Prefix them when necessary, for example `vault_account_users.cypher`. Each query must produce a declared output or be needed by another query; unused files are rejected.

| Field or rule | What you need to know |
| --- | --- |
| `schema.name` | Stable extension identity, unique in the bundle. |
| `schema.display_name` | Human-readable name shown to authors. |
| `schema.version` | A semantic version such as `v1.0.0`; this experiment requires the `v` prefix. |
| `schema.namespace` | Prefix for this schema's declared kinds. With `PoC_Start`, kinds must begin `PoC_Start_`. Namespaces must be unique. |
| `node_kinds` / `relationship_kinds` | Arrays describing your own kinds. Current BloodHound registration requires at least one local node kind; relationships may be `[]`. Do not redeclare another extension's kinds. |
| Relationship `compatible_kinds` | Exact allowed source/target kind pairs. Allowing `User → Domain` and `Group → Computer` does not allow `User → Computer`. |
| `is_traversable` | Whether pathfinding may follow the relationship. Use `false` for factual associations that do not establish an attack step. |

Current experiment restriction: **calculated kinds and helper ID prefixes must start with `PoC_`**. They must also follow their schema namespace rules. This separates experimental output from native output.

A declared kind with no producing query is descriptive or collected data; declaring it alone does not schedule graph changes. If its last producing rule was deleted, previously managed results are cleared on the next successful run. The runner registers the display schema separately from calculating results. The custom `compatible_kinds` field is interpreted by this architecture, not by an ordinary OpenGraph schema upload.

Reference native BloodHound kinds directly. For a kind defined by another extension, use its defining schema; do not repeat its declaration in your own file.

All schemas are read before references are checked, so they can refer to one another in either file order. Missing definitions keep an uploaded draft inactive; upload the remaining schemas and queries, review the complete result, then activate it. Mutual type references are allowed. Calculations that each require the other's new answer still have no valid execution order and cannot activate.

**Do not add `post_processing` blocks.** Output declarations live in Cypher. The platform supplies the reference build and the shipped ESC1 experiment’s native-agreement check. Declare each relationship kind once in a schema; several query files may return it.

Keep an activated schema's filename and name stable when updating it. You can add producers and change their queries through reviewed uploads. See [USAGE.md — Change the active rules](USAGE.md#change-the-active-rules) for removal and reassignment.

## Dependencies and shared queries

Every query starts with a `depends-on` comment. Declare the node and relationship kinds it reads, plus any earlier query answers it consumes. Declared graph inputs must be native kinds or have a defining schema in the complete set; a missing custom schema keeps the draft inactive:

```cypher
// depends-on: node:User, node:Group, relationship:MemberOf,
// query:getting_started_enabled_users.cypher
```

These declarations do two jobs: order queries and decide which rules need to run after data changes. Include kinds used in filters and traversals, not just those in the final output. If a query has no inputs, write `// depends-on: none`.

### Pass rows from one query to another

Continue from the quick start. Add **`getting_started_enabled_users.cypher`** beside the schema; it returns a reusable list of enabled users:

```cypher
// depends-on: node:User
MATCH (person:User)
WHERE person.enabled = true
RETURN person.objectid AS user_id
```

Replace **`getting_started_memberships.cypher`** with the following query. Keep its name and the existing schema; the output kind is already declared there.

```cypher
// depends-on: query:getting_started_enabled_users.cypher,
// node:User, node:Group, relationship:MemberOf
// existing-edge-policy: keep-existing

UNWIND $getting_started_enabled_users AS eligible
MATCH (person:User)-[:MemberOf]->(group:Group)
WHERE person.objectid = eligible.user_id
RETURN DISTINCT person.objectid AS source, group.objectid AS target,
       'PoC_Start_DirectMemberOf' AS kind
```

The filename without `.cypher` becomes the parameter: `$getting_started_enabled_users` is a list of rows, each with `user_id`. Query parameters currently come from declared query prerequisites; there is no separate arbitrary parameter/configuration file for authors.

Both files must be in the loaded bundle. The final query names its declared edge kind in `RETURN`; the prerequisite has no output declaration. Query dependencies can cross extension boundaries, and a shared prerequisite runs once for its consumers in that run. Answers are not cached between runs. Circular dependencies are rejected.

The plan now lists the enabled-user query with role `intermediate`, before the membership query. After a successful run, users with `enabled: true` keep their direct membership connections. Users with `false` or a missing `enabled` property have none of these calculated connections. Their native `MemberOf` relationships remain unchanged.

The membership query needs the earlier query's rows as a parameter. Pasting it alone into the normal query interface will not supply that parameter; use the complete extension in the lab to check both steps together.

### Fresh answers versus existing graph data

**Use a query prerequisite to receive another rule’s returned rows as a parameter.** To read calculated edges from the graph, declare their relationship kind under `depends-on`. The runner waits for every producer of that type and stages their combined answer before the graph reader runs.

Helper nodes are also staged before their declared graph readers. All reads after graph changes are staged run one at a time inside the same transaction; the changes become visible to other users only after the whole run succeeds. A rule reading its own output type sees its previous state, even when other rules also produce that type; it does not wait for those peers. Other dependencies forming a cycle between different rules are rejected. Passing the producer’s rows directly remains useful. See [the shared extension example](examples/cross-platform/README.md).

Changed kinds select whole queries, every producer sharing their output type, their prerequisites, and their consumers, continuing until all affected work is included. A selected producer must return its **complete current answer**. First runs, changed rules, restarts, and incomplete change tracking cause all enabled rules to run. A skipped rule keeps its previous output.

## What queries return

Queries are read-only. Return ordinary strings, numbers, booleans, lists, and maps; do not return database node, relationship, or path objects as your extension's result.

| Query purpose | Required columns |
| --- | --- |
| Shared prerequisite | Any useful plain-value columns, such as `user_id`; reserve `kind` for edge output declarations. |
| Calculated relationship | Exactly `source`, `target`, `kind`, and optionally `properties`. |
| Helper node | Exactly `id`, `kinds`, and `properties`. |

### Calculated relationships

`source` and `target` must be nonempty string `objectid` values. Do not use database-internal numeric IDs. Each endpoint must resolve to one object and match a permitted endpoint pair. Referenced helpers must exist or be created by their prerequisite producer in the run.

**The query supplies the edge name:** return a literal string as `kind`. That name must be declared in a schema's `relationship_kinds`.

To add properties after the [shared-answer example](#pass-rows-from-one-query-to-another), replace **`getting_started_memberships.cypher`** with the following. Keep the enabled-user query, schema, and optional helper query. The `merge-properties` policy adds the evidence to existing connections too; `keep-existing` would leave their old properties unchanged.

```cypher
// depends-on: query:getting_started_enabled_users.cypher,
// node:User, node:Group, relationship:MemberOf
// existing-edge-policy: merge-properties

UNWIND $getting_started_enabled_users AS eligible
MATCH (person:User)-[:MemberOf]->(group:Group)
WHERE person.objectid = eligible.user_id
RETURN DISTINCT person.objectid AS source, group.objectid AS target,
       'PoC_Start_DirectMemberOf' AS kind,
       {evidence: 'direct membership of an enabled user'} AS properties
```

After a successful run, the endpoint pairs should match the shared-answer example. Each connection should have `evidence` set to `direct membership of an enabled user`. Adding this property requires no new schema field.

Each query produces **one kind**. Use one final outer `RETURN`, explicit column names, and a literal such as `'PoC_Start_DirectMemberOf' AS kind`. A variable, parameter, expression, or `UNION` cannot declare the output. Use separate files for separate output kinds.

The runner reads the literal before execution, so an empty result still has a known output kind. That makes stale-edge cleanup possible without a second declaration in the schema. Every returned row must agree with the declared kind. A comment or a `WITH` alias cannot declare an output.

Within one query, repeated pairs collapse when their property maps agree; conflicting maps fail the run. Across different queries producing the same type, supplied properties merge in alphabetical filename order. Later filenames win overlapping keys, including explicit null removals. This order is fixed regardless of which query finishes first.

### Properties

- Stored values may be text, booleans, signed 64-bit whole numbers, finite fractional numbers, or lists of one simple type.
- Stored properties cannot contain nested maps, nested lists, or lists containing nulls. Prerequisite row maps can be richer; they are not stored property maps.
- Omit the relationship `properties` column for an empty map. The column itself cannot be `null`.
- A relationship property value of `null` removes that key with `merge-properties`; on creation/replacement, the key stays absent.
- Helper `objectid` is reserved because the `id` column supplies it. Property names cannot be blank. No property establishes graph ownership.

### Helper nodes

Use helpers only when you need calculated objects in the graph. A shared prerequisite is enough when you only need intermediate rows.

Declare a helper type in `node_kinds`, then return its literal name in the one-element `kinds` list. Its ID prefix is automatically the kind name followed by `_`.

The quick-start schema already declares `PoC_Start_Note`. To create one note per native user, add **`getting_started_notes.cypher`** beside the existing files. No schema change is needed:

```cypher
// depends-on: node:User
MATCH (person:User)
RETURN 'PoC_Start_Note_' + person.objectid AS id,
       ['PoC_Start_Note'] AS kinds,
       {user_id: person.objectid} AS properties
```

This query is independent of the enabled-user calculation. It creates notes for all native users, including disabled users, and does not change which membership connections qualify. Expect one note with ID `PoC_Start_Note_<user-objectid>` for each user. A schema declaration alone created no notes in the quick start; this query makes that kind a calculated output.

The ID must have a nonempty suffix after the prefix and remain stable across runs. `kinds` must contain only the declared helper kind. Helpers have their own identity: they do not gain native `User` or `Base` labels. Their properties cannot contain nulls.

An existing object with the same ID and exactly the declared helper kind can be updated regardless of its origin. A different kind or duplicate identity causes failure. Helpers missing from a later result are retained; automatic deletion currently applies to calculated relationships, not helper nodes.

## How saved results change

**Your query returns the connections that should exist now, not a list of changes to apply.** Do not use `CREATE`, `MERGE`, `SET`, or `DELETE` in a rule. The runner combines the producers’ results and writes the complete answer.

To change how matching edges are updated, add one optional comment to the relationship query, for example `// existing-edge-policy: keep-existing`. There is no matching schema setting to maintain.

| Query policy | What happens to an existing matching relationship |
| --- | --- |
| `replace-existing` — default | Delete and recreate it with the returned properties. Its database ID changes. |
| `merge-properties` | Keep its ID, update supplied properties, remove keys explicitly set to null, and retain unmentioned properties. |
| `keep-existing` | Keep its ID and properties. Returned properties still apply to newly created relationships. |

**All producers of one relationship type must use the same policy. All three policies remove in-scope relationships absent from every producer’s complete successful answer.** An empty answer from one rule keeps edges still returned by another. If all producers return no rows, the runner clears the declared endpoint scope. `keep-existing` does not mean “keep obsolete connections.”

Do not add a debugging `LIMIT`, sample rows, or return only recently changed objects in an active producer. Those partial answers can incorrectly remove valid output. Test a limited read separately from the published rule.

The scope is the relationship type and its declared source/target kind pairs. Existing edges within that scope participate regardless of who created them: manual edges can be updated or removed too. Edges outside those endpoint pairs remain untouched. Duplicate existing pairs within the scope cause failure. A failure before commit preserves the last committed output; it may now be stale. Schema registration and database metadata preparation happen separately and can remain after failure.

### Updating, disabling, and removing rules

| Action | Result |
| --- | --- |
| Upload/overwrite | Review and activate the complete proposed revision. It takes effect after a later successful native analysis. |
| Disable | Keep files and previous graph output. Pause every producer sharing the relationship type, then their query and graph consumers. Independent work continues. |
| Enable | Resume eligible rules on a later analysis. Other disabled prerequisites can still keep a rule paused. |
| Remove | Delete explicit query dependents and unused supporting queries. Keep other producers and graph readers. Refresh combined results from the remaining producers on a later successful run; deleting the last producer clears its old scope. Helpers remain. |

For example, A and B produce `CanAccess`, C reads that type, and D directly needs A’s returned rows. Disabling A also pauses B, C, and D so the previous shared graph answer stays intact. Deleting A removes D, keeps B and C, refreshes `CanAccess` from B, and then runs C against the refreshed graph. If B is also deleted, the scope is cleared and C can run with no matching edges. Enabling A removes its explicit disabled setting; other disabled dependencies can still keep related rules paused.

None of these actions alone runs queries or changes graph output. An in-progress run keeps the revision it already fetched. The page's report shows the last reported attempt, not a live health check; compare its revision and time with your changes.

### Updating older bundles

Use a literal `kind` in each relationship query and literal helper `kinds`. Remove old schema `post_processing` blocks, including any owner field. Move an `existing_edge_policy` setting to each producer’s `existing-edge-policy` comment, using the same value for all producers of that type. Helper IDs must begin with their kind name plus `_`.

Rename each relationship's `endpoints` field to `compatible_kinds`, keeping every `source_kind`/`target_kind` pair and its direction. Remove `external_node_kinds`; native kinds come from BloodHound, and custom kinds come from the accompanying schemas.

Preview and activate a complete set of current files together. Saved ownership metadata is no longer part of the format. Existing graph properties such as `poc_owner` do not grant or prevent updates.

To rename a query, add the renamed file, update consumers' prerequisites/parameters, and explicitly remove the old file in the same reviewed change. Two active relationship queries may publish the same kind, so leaving the old file in place keeps its contribution active. Changing an output kind requires declaring the new kind in the schema; the registry records the old kind for cleanup. Removing a rule keeps its display schema intact.

## Cypher support and DAWGS

**Prefer ordinary Cypher that returns final relationships.** Supported simple forms include the `MATCH`, `OPTIONAL MATCH`, `WHERE`, `WITH`, `UNWIND`, aggregation, and map values demonstrated by the shipped queries. Support for a keyword does not prove every combination works; test your actual query through the application.

There are three separate checks:

| Check | What it establishes |
| --- | --- |
| Upload/`plan` validation | File structure, dependencies, output assignments, and basic restrictions. |
| Application query parsing | Whether the installed DAWGS frontend can understand the query. Persistent mode uses this check for both graph backends. |
| Database execution | Whether the query executes correctly on the selected backend, including PostgreSQL translation and result handling. |

A query working in Neo4j Browser does not automatically work through this application. The current DAWGS implementation does not provide general `CASE`, `reduce`, list comprehension, list indexing, or `CALL` subquery support. The file loader permits read-only `CALL { ... }` syntax, but that is not a promise that runtime parsing supports it. Procedure calls, including APOC, and external loads are not available to extension rules. `EXPLAIN` and `PROFILE` are debugging tools to use separately where supported, not text to upload as a rule.

### When the language is missing something

**Adding DAWGS features is an acceptable way to make extension authoring simpler.** Keeping DAWGS changes small is not more important than keeping extension rules understandable.

The design direction is for Cypher to express which relationships qualify, while Go handles execution, validation, and publication. New language support should preserve that separation when it makes the authoring experience better and performs acceptably. This is a direction for further implementation; the ESC1 exception below still exists today.

When requesting or implementing a feature, provide:

1. The readable Cypher you want authors to write, with a small input example and exact expected rows.
2. The current limitation or workaround, and how the new feature simplifies the rule.
3. Tests of actual results on both supported backends, including empty inputs, nulls, duplicates, and relevant ordering cases.
4. Measurements on representative data: total runtime, the changed query's runtime, application and database memory, and intermediate result sizes. Include nested/overlapping groups if the feature handles membership.

There is no agreed numeric slowdown allowance yet. Evaluate measured costs against the authoring benefit; do not treat an unmeasured “several times slower” estimate as an established cost of Cypher. Adding syntax alone is not enough: parsing, translation, execution, and existing query behavior need checking. Maintainers should follow [DAWGS's contribution instructions](../DAWGS/AGENTS.md).

## The current ESC1 exception

**The shipped ESC1 extension is not entirely defined by Cypher.** Some files return inputs to Go calculations instead of their final answer. These comments are custom runner instructions; a database treats them as comments.

The choice was made to reuse compact membership/trust calculations, avoid repeatedly following every possible route, and work around missing DAWGS expressions. It makes the query files shorter, but some behavior can only be understood or changed in Go. That authoring cost is real. The speed and memory benefit over an equivalent optimized Cypher implementation has not been measured.

### `reachability`: find connected objects

Example from [membership_reach.cypher](extension/membership_reach.cypher):

```cypher
// reachability: {"direction":"incoming","relationships":["MemberOf","MemberOfLocalGroup"],"include_start":false,"expand_results":true,"reject_cycles":true}
```

The query returns starting objects as `id` (object ID), `graph_id` (internal numeric ID), and `kinds`. Go follows the declared relationship types and returns rows containing `principal`, `graph_id`, `kinds`, `reach` (reachable object IDs), and `reach_size`.

| Setting | Meaning |
| --- | --- |
| `direction` | Follow incoming or outgoing relationships. |
| `relationships` | Relationship types to follow. Declare each under `depends-on` too. |
| `include_start` | Include the starting object in its reachable set. |
| `expand_results` | Return rows for discovered objects as well as the supplied starting objects. |
| `node_kinds` | Optional restriction on permitted node kinds; also declare those dependencies. |
| `reject_cycles` | Fail if the relevant traversal contains a cycle. |

Running the seed query directly in a database returns only starting objects, not the Go-produced reachable sets. ESC1's membership and certificate traversals reject relevant cycles; its forest traversal permits reciprocal trust. Do not assume all graph cycles have the same rule.

### `group-overlap`: combine permission sides

```cypher
// group-overlap: {"memberships":"membership.cypher"}
```

This tells Go to use the declared membership prerequisite to process each `left`, `right`, `target`, `kind` row. In ESC1, `left` contains template enrollers, `right` contains CA enrollers, and `target` is a domain. Go emits the final `source`/`target` rows and preserves the literal `kind`. The final ESC1 projection is `RETURN left, right, trusted.domain AS target, 'PoC_ADCSESC1' AS kind`.

The membership rows describe principal IDs, internal graph IDs, group/special-group flags, reachable members, and reach sizes. The operation preserves selected groups, uses ordering rules, and applies the special-group exceptions. It is more than an ordinary intersection of user lists. See [membership.cypher](extension/membership.cypher), [the ESC1 notes](extension/QUERY_NOTES.md), and [the implementation](internal/runner/overlap.go) for the exact contract.

Only relationship producers can request it, and they cannot also request `reachability`. Most extensions need neither instruction. Rewriting this step in Cypher requires preserving the actual edge results, not just finding the same reachable users. Rewriting the entire ESC1 extension also requires replacing the upstream reachability calculations.

### `batch-on`: run independent input chunks

```cypher
// batch-on: trusted_cas.cypher
```

This permits the runner to split one declared prerequisite into smaller lists. It executes the query once per chunk and combines the answers before publication. Other prerequisites remain available for every chunk.

Use it only when **combining every chunk's answers gives exactly the same relationships and properties as processing the whole input**. The author makes this promise; the runner cannot prove it. A global count, ranking, or limit that needs the complete input can break that promise.

Only relationship producers can use `batch-on`, and reachability seed queries cannot use it. All chunks must finish successfully before output is published. The directive controls execution; it does not itself decide who gets an edge.

## Performance and limits

Reading output declarations happens when rules load. Returning `kind` adds one short string column per relationship row; it adds no graph traversal or additional database query. This change preserves the existing streaming, batching, and overlap algorithms. Its runtime overhead has not been benchmarked.

Keep repeated work in shared prerequisite queries. Return only the columns consumers need. Keep joins tied to relevant kinds and object IDs, and avoid collecting every possible path when you only need to know which objects are connected.

Moving a rule into Cypher does not inherently require giving up controlled batching. Any optimization must preserve complete answers, including properties and special cases. A simple list-based rewrite of a bitmap calculation can be expensive, but that does not establish the cost of an optimized Cypher implementation.

| Current limit | Default |
| --- | ---: |
| Files in the complete uploaded bundle | 32 |
| One uploaded file | 1 MiB |
| Complete saved/uploaded bundle | 4 MiB, including its encoded structure |
| One result row | 1 MiB encoded |
| Combined prerequisite parameters | 32 MiB encoded budget |
| Explicit input/write batch | Up to 500 rows; input chunks may be smaller to fit the byte budget |
| One query, including its input chunks | 15 minutes |
| Complete run | 2 hours |
| Independent query readers | 2 |

The full runtime defaults, including disk and output limits, are in [DESIGN.md — Current defaults](DESIGN.md#current-defaults). Persistent mode uses these defaults; standalone command flags do not configure the persistent service.

Large prerequisite answers are kept in temporary files, then loaded as bounded parameters when consumed. A single giant collected list can exceed the row limit even when the number of result rows is small. Database memory is additional to the runner's byte budgets. Several smaller write batches still belong to one final transaction.

Hitting a limit fails the calculation and preserves previous output; it does not authorize a truncated replacement. Production-scale capacity and performance at 100 million edges have not been established.

## Testing and troubleshooting

### Before sharing an extension

Check behavior against written expectations, not just whether the query runs:

- A supported fact creates the expected pair; an unrelated fact does not.
- Repeated supporting facts produce one pair with consistent properties.
- Removing one of several supports keeps the pair; removing the last support removes it after a complete successful run.
- Empty input, missing properties, and relevant cycles behave as intended.
- Repeating the same input follows your selected edge policy.
- A failed query preserves the previous committed output.
- Separate collectors arriving in either order eventually produce the same answer.
- The complete extension works through the intended application on each backend you support.

For shared prerequisites, test with their actual row parameters. For ESC1-style directives, testing just the Cypher text bypasses Go transformations and is not an end-to-end test. Keep a small exact-answer fixture and a separate representative performance dataset.

The local `plan` command is a useful first check. Use the rule page and a dedicated lab for execution checks. Do not run the historical standalone `run`/`compare` commands against the persistent lab without reading their deployment requirements; those modes can stop the application.

### Common problems

| Symptom | What to check |
| --- | --- |
| File upload rejected | Filenames, size limits, valid schema fields, missing prerequisite files, unused queries, and literal output kinds with matching policies for shared relationship producers. |
| Output declaration rejected | Use a literal `kind`, declare that kind once in the schema, and give all its producers the same update policy. |
| Query parameter rejected | Declare `query:filename.cypher`; use `$filename` without the extension. |
| Rule never runs after activation | Activation alone does not execute it. Trigger a later successful native analysis with a collection upload; check disabled prerequisites and report revision. |
| Rule does not rerun after relevant data changes | Check all node/relationship dependencies, including kinds used only in filters. |
| No output, but the report succeeded | Check the query's actual rows and current graph evidence. Empty successful results are allowed. |
| Endpoint validation fails | Return `objectid` strings, check unique identities and permitted type pairs, and declare any required helper producer. |
| Duplicate pair/property error | Return one agreed property map for each source/target pair. |
| Works in Neo4j Browser but fails in the application | Check installed DAWGS support and test through the native application path. Upload acceptance does not prove execution support. |
| New rule still appears to use old answers | Check the report's revision. An in-progress run and its retries keep their original files; corrected files require another successful native analysis. |
| Previously calculated edges disappeared | Check for an empty/partial successful answer, changed filters, or removal of their producer. `keep-existing` still deletes obsolete pairs. |
| Deleted helper still appears | Omitted or retired helpers are retained by design. |
| Budget or timeout error | Reduce repeated work and oversized intermediate rows; measure before changing operator limits. Do not truncate the published answer. |
| Status is old or absent | Check rule-service/application connectivity and application logs; the displayed report is not a heartbeat. |
| Graph writes refused after a lost commit reply | Ask the operator to inspect the saved state and recover the application. An error does not prove the database rolled back. |

See [USAGE.md — Troubleshooting](USAGE.md#troubleshooting) for container commands and log locations. [REVIEW.md](REVIEW.md) records the scope of existing checks; examples and small fixtures are not production performance measurements.

## Further reading

| Document | Purpose |
| --- | --- |
| [USAGE.md](USAGE.md) | Build/start the lab, activate rules, inspect status, and manage their lifecycle. |
| [DESIGN.md](DESIGN.md) | Execution, shared outputs, transactions, graph visibility, limits, and backend details. |
| [Vault example](examples/vault/README.md) | Connect collected custom objects to native users. |
| [AD/CyberArk example](examples/cross-platform/README.md) | Separate extensions consuming shared query answers. |
| [ESC1 query notes](extension/QUERY_NOTES.md) | Exact ESC1 calculations and current Go-assisted operations. |
| [REVIEW.md](REVIEW.md) | Implementation map, recorded evidence, and unproven behavior. |
