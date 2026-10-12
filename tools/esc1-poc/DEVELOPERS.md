# Build an extension

An extension describes **which connections should exist in BloodHound**. You provide a schema and read-only Cypher queries. BloodHound runs the queries after its own analysis, checks their answers, and saves the connections.

In this guide, a **node** is an object, a **relationship** is a connection, a **kind** is its type, and a **schema** describes those types. An object's `objectid` is its stable identifier.

Start with a query that returns `source` and `target` object IDs and a literal edge name as `kind`. You do not need to write Go, create relationships yourself, or use ESC1's special processing instructions for an ordinary extension.

This guide covers the current experimental architecture in `tools/esc1-poc`. It requires the modified BloodHound application and rule service described in [USAGE.md](USAGE.md). The schema additions described here are a proposal, not features supplied by an unmodified BloodHound installation.

## Find what you need

| I want to… | Go to… |
| --- | --- |
| Make my first working rule | [Quick start](#quick-start) |
| Understand what runs where | [How a run works](#how-a-run-works) |
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
  "external_node_kinds": [
    "User",
    "Group"
  ],
  "relationship_kinds": [
    {
      "name": "PoC_Start_DirectMemberOf",
      "description": "Learning example: an existing direct user-to-group membership.",
      "is_traversable": false,
      "endpoints": [
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

The comment tells the runner when this rule needs to run. The query decides which connections should exist and names their kind. The schema describes that kind and its permitted endpoint types. The optional `existing-edge-policy` comment keeps an existing matching edge unchanged; omit it to use the default replacement policy.

### 2. Check the query and plan

Run the `MATCH` and `RETURN` in the lab's BloodHound query interface. Expect source and target IDs plus the same `kind` value for every direct user/group membership. No rows is a valid answer if your collection has none.

If you have the local Go tooling and dependencies from the setup guide, you can also check the saved files without connecting to a database. Run from `tools/esc1-poc`, substituting your folder's path:

```sh
./scripts/go.sh run . plan -extension /path/to/developer-start
```

This checks the schema, filenames, dependencies, output assignments, and basic query restrictions. It **does not execute the query or prove database compatibility**. The script uses locally cached dependencies and does not download missing packages. If your dependencies are in another cache, set `POC_MODULE_CACHE` to that directory as described in the setup guide.

### 3. Activate the two files

1. Open the lab's rule page at `http://127.0.0.1:18081/`.
2. Select the two files, then click **Review uploaded changes**.
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

The pairs should match the direct membership query. The original `MemberOf` relationships stay under BloodHound's control. Your extension owns the separate `PoC_Start_DirectMemberOf` kind.

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
| `external_node_kinds` | Existing endpoint kinds you refer to, such as native `User`, or helper kinds owned by another extension. This does not create or relabel objects. |
| Relationship `endpoints` | Exact allowed source/target kind pairs. Allowing `User → Domain` and `Group → Computer` does not allow `User → Computer`. |
| `is_traversable` | Whether pathfinding may follow the relationship. Use `false` for factual associations that do not establish an attack step. |

Current experiment restriction: **calculated kinds and helper ID prefixes must start with `PoC_`**. They must also follow their schema namespace rules. This separates experimental output from native output.

A declared kind with no producing query is descriptive or collected data; the runner does not take ownership of it. The runner registers the display schema separately from calculating results. The custom `external_node_kinds` and `endpoints` fields are interpreted by this architecture, not by an ordinary OpenGraph schema upload.

**Do not add `post_processing` blocks.** Output declarations live in Cypher. The platform supplies stable ownership, the reference build, and the shipped ESC1 experiment's native-agreement check. New extensions receive an owner based on `schema.name`; the registry saves that identity through updates and restarts. You do not choose an owner or reference commit in the schema.

Keep an activated schema's filename, name, and namespace stable when updating it. You can add producers and change their queries through reviewed uploads. See [USAGE.md — Change the active rules](USAGE.md#change-the-active-rules) for removal and reassignment.

## Dependencies and shared queries

Every query starts with a `depends-on` comment. Declare the node and relationship kinds it reads, plus any earlier query answers it consumes:

```cypher
// depends-on: node:User, node:Group, relationship:MemberOf,
// query:eligible_users.cypher
```

These declarations do two jobs: order queries and decide which rules need to run after data changes. Include kinds used in filters and traversals, not just those in the final output. If a query has no inputs, write `// depends-on: none`.

### Pass rows from one query to another

For example, `eligible_users.cypher` could return a reusable list of enabled users:

```cypher
// depends-on: node:User
MATCH (person:User)
WHERE person.enabled = true
RETURN person.objectid AS user_id
```

A relationship query can consume those rows:

```cypher
// depends-on: query:eligible_users.cypher,
// node:User, node:Group, relationship:MemberOf

UNWIND $eligible_users AS eligible
MATCH (person:User)-[:MemberOf]->(group:Group)
WHERE person.objectid = eligible.user_id
RETURN DISTINCT person.objectid AS source, group.objectid AS target,
       'PoC_Start_DirectMemberOf' AS kind
```

The filename without `.cypher` becomes the parameter: `$eligible_users` is a list of row maps. Query parameters currently come from declared query prerequisites; there is no separate arbitrary parameter/configuration file for authors.

Both files must be in the loaded bundle. The final query names its declared edge kind in `RETURN`; the prerequisite has no output declaration. Query dependencies can cross extension boundaries, and a shared prerequisite runs once for its consumers in that run. Answers are not cached between runs. Circular dependencies are rejected.

### Fresh answers versus existing graph data

**Use a query prerequisite when you need another rule's answer from the same run.** Matching that rule's calculated relationship kind in the graph can read its previous result: new relationships are saved after calculations finish.

Helper nodes are different. They can be staged inside the final transaction before dependent graph reads, provided the query declares the helper producer as a prerequisite. Reads after helper staging are serial. Passing the producer's rows directly is often simpler. See [the shared extension example](examples/cross-platform/README.md).

Changed kinds select whole queries and their consumers, not just individual changed objects. A selected producer must return its **complete current answer**. First runs, changed rules, restarts, and incomplete change tracking cause all enabled rules to run. A skipped rule keeps its previous output.

## What queries return

Queries are read-only. Return ordinary strings, numbers, booleans, lists, and maps; do not return database node, relationship, or path objects as your extension's result.

| Query purpose | Required columns |
| --- | --- |
| Shared prerequisite | Any useful plain-value columns, such as `user_id`; reserve `kind` for edge output declarations. |
| Calculated relationship | Exactly `source`, `target`, `kind`, and optionally `properties`. |
| Helper node | Exactly `id`, `kinds`, and `properties`. |

### Calculated relationships

`source` and `target` must be nonempty string `objectid` values. Do not use database-internal numeric IDs. Each endpoint must resolve to one object and match a permitted endpoint pair. Referenced helpers must exist or be created by their prerequisite producer in the run.

**The query supplies the edge name:** return a literal string as `kind`. That name must be declared in a schema's `relationship_kinds`. You can add properties:

```cypher
// depends-on: node:User, node:Group, relationship:MemberOf
MATCH (person:User)-[:MemberOf]->(group:Group)
RETURN DISTINCT person.objectid AS source, group.objectid AS target,
       'PoC_Start_DirectMemberOf' AS kind,
       {evidence: 'direct membership'} AS properties
```

Each query produces **one kind**. Use one final outer `RETURN`, explicit column names, and a literal such as `'PoC_Start_DirectMemberOf' AS kind`. A variable, parameter, expression, or `UNION` cannot declare the output. Use separate files for separate output kinds.

The runner reads the literal before execution, so an empty result still has a known output kind. That makes stale-edge cleanup possible without a second declaration in the schema. Every returned row must agree with the declared kind. A comment or a `WITH` alias cannot declare an output.

Repeated pairs collapse when their property maps agree. The same pair with conflicting properties fails the run. Combine supporting evidence into one agreed result rather than relying on row order.

### Properties

- Stored values may be text, booleans, signed 64-bit whole numbers, finite fractional numbers, or lists of one simple type.
- Stored properties cannot contain nested maps, nested lists, or lists containing nulls. Prerequisite row maps can be richer; they are not stored property maps.
- Omit the relationship `properties` column for an empty map. The column itself cannot be `null`.
- A relationship property value of `null` removes that key with `merge-properties`; on creation/replacement, the key stays absent.
- The runner owns `poc_owner`. Helper `objectid` and `poc_owner` are also reserved. Property names cannot be blank.

### Helper nodes

Use helpers only when you need calculated objects in the graph. A shared prerequisite is enough when you only need intermediate rows.

Declare a helper type in `node_kinds`, then return its literal name in the one-element `kinds` list. Its ID prefix is automatically the kind name followed by `_`. For a declared `PoC_Example_User` kind:

```cypher
// depends-on: node:User
MATCH (person:User)
RETURN 'PoC_Example_User_' + person.objectid AS id,
       ['PoC_Example_User'] AS kinds,
       {user_id: person.objectid} AS properties
```

The ID must have a nonempty suffix after the prefix and remain stable across runs. `kinds` must contain only the declared helper kind. Helpers have their own identity: they do not gain native `User` or `Base` labels. Their properties cannot contain nulls.

An existing object with the same ID but another owner or kind causes failure. Helpers missing from a later result are retained; automatic deletion currently applies to calculated relationships, not helper nodes.

## How saved results change

**Your query returns the connections that should exist now, not a list of changes to apply.** Do not use `CREATE`, `MERGE`, `SET`, or `DELETE` in a rule. The runner writes the results and records their owner.

To change how matching edges are updated, add one optional comment to the relationship query, for example `// existing-edge-policy: keep-existing`. There is no matching schema setting to maintain.

| Query policy | What happens to an existing matching owned relationship |
| --- | --- |
| `replace-existing` — default | Delete and recreate it with the returned properties. Its database ID changes. |
| `merge-properties` | Keep its ID, update supplied properties, remove keys explicitly set to null, and retain unmentioned properties. |
| `keep-existing` | Keep its ID and properties. Returned properties still apply to newly created relationships. |

**All three policies remove owned relationships absent from a complete successful answer.** An empty successful result removes that producer's previous relationships. `keep-existing` does not mean “keep obsolete connections.”

Do not add a debugging `LIMIT`, sample rows, or return only recently changed objects in an active producer. Those partial answers can incorrectly remove valid output. Test a limited read separately from the published rule.

Missing/foreign ownership or duplicate existing managed pairs cause failure rather than adoption. A failure before commit preserves the last committed output; it may now be stale. Schema registration and database metadata preparation happen separately and can remain after failure.

### Updating, disabling, and removing rules

| Action | Result |
| --- | --- |
| Upload/overwrite | Review and activate the complete proposed revision. It takes effect after a later successful native analysis. |
| Disable | Keep files and previous graph output. Dependent rules pause too; independent rules can continue. |
| Enable | Resume eligible rules on a later analysis. Other disabled prerequisites can still keep a rule paused. |
| Remove | Review the dependent files that will also be removed. Retired calculated relationships are cleaned up on a later successful run; helpers remain. |

None of these actions alone runs queries or changes graph output. An in-progress run keeps the revision it already fetched. The page's report shows the last reported attempt, not a live health check; compare its revision and time with your changes.

### Updating older bundles

Previously saved schemas with `post_processing` blocks still load. To migrate one, add a literal `kind` to each relationship query, use literal helper `kinds`, and remove the schema's processing blocks. Move any `existing_edge_policy` to the query's `existing-edge-policy` comment. Modern helper IDs must begin with their kind name plus `_`.

Upload these changes together to the **existing registry**. It retains the original owners in saved platform metadata, including after restart or rule removal. Keep that registry's saved state when upgrading. Loading a converted folder directly in the standalone runner has no registry history; it derives a new owner and will reject previously owned objects rather than adopt them. Leave the legacy settings in place for that standalone workflow until its state is migrated.

To rename a query, add the renamed file, update consumers' prerequisites/parameters, and explicitly remove the old file in the same reviewed change. Two active queries cannot publish the same kind. Changing an output kind requires declaring the new kind in the schema; the registry records the old kind for cleanup. Removing a rule keeps its display schema intact.

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
| File upload rejected | Filenames, size limits, valid schema fields, missing prerequisite files, unused queries, and a literal output kind and one producer per kind. |
| Output declaration rejected | Use a literal `kind`, declare that kind in the schema, and keep one producing file per kind. |
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
| [DESIGN.md](DESIGN.md) | Execution, ownership, transactions, helper visibility, limits, and backend details. |
| [Vault example](examples/vault/README.md) | Connect collected custom objects to native users. |
| [AD/CyberArk example](examples/cross-platform/README.md) | Separate extensions consuming shared query answers. |
| [ESC1 query notes](extension/QUERY_NOTES.md) | Exact ESC1 calculations and current Go-assisted operations. |
| [REVIEW.md](REVIEW.md) | Implementation map, recorded evidence, and unproven behavior. |
