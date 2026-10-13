# OpenGraph Relationship Analysis Proposal

Simplifying OpenGraph relationship analysis would:

- lower development and maintenance costs
- rapidly expand security coverage
- deliver value to customers sooner
- increase community participation

Writing a developer-facing server-side analysis engine for OpenGraph would:

- reduce duplicated development and maintenance efforts
- reduce collector-side resource consumption
- simplify customer/user operations
- possibly reduce analysis duration

This document proposes that OpenGraph extension authors supply familiar graph queries and simple dependencies to create relationships, while BloodHound coordinates execution and keeps calculated relationships current as data arrives from different collectors.

# Background

Post-processing is the name for the server-side analysis that takes place after data is uploaded to BloodHound.

Post-processing and other terms such as “pre-processing” are often conflated, so this document uses the term “analysis” to decouple the term from the timing and system it’s performed on. 

What matters is that analysis is used to create relationships that rely on data that already exists in the graph or that rely on complex chains of relationships created during prior analysis (e.g., nested group memberships).

# Why establish this foundation now

OpenGraph relationship analysis is still early enough that a small number of representative extensions can expose weaknesses in the shared design. That makes this the least expensive time to establish how extensions declare dependencies, how they share calculations, and how their results are safely updated.

Once many extensions have been developed (for example, the 100 that Justin mentioned on our horizon), these choices become compatibility promises. Changing them would require updating independently maintained queries, preserving old and new behavior during customer upgrades, retesting relationships already stored in customer graphs, and coordinating extensions that consume one another's calculations.

The cost grows faster than the number of extensions. One piece of work might be reused by several extensions. Several extensions might also independently create the same connection in BloodHound.
That means changing one shared piece later could break many extensions—possibly extensions written by different people. Also, if Neo4j and PostgreSQL behave differently, extension authors may add special fixes to their rules. Once many extensions depend on those fixes, correcting the underlying database behavior could break them.

**In short: a small design problem today can spread into many extensions and become much harder to fix later.**

This does not mean every interface, optimization, or large-scale deployment question must be finished before another extension is written. It means the following contracts should be settled and tested before the extension catalog grows substantially:

- how each query tells BloodHound what stored information and earlier calculations it needs
- how queries share results with future queries, including across extensions
- how several queries can jointly support the same relationship
- how changing, disabling, or removing a query affects results already in the graph
- how a failed query leaves the last confirmed complete results in place
- how the same supported query produces the same answer on Neo4j and PostgreSQL

# The Problems

This design proposal addresses several practical problems we **must** address for OpenGraph relationship analysis to scale.

## Useful research takes too long to become product coverage

Today, OpenGraph extension developers spend significant time and effort preparing and connecting information instead of identifying security risks for customers and users. 

---

## SpecterOps is the bottleneck for graph expansion

Developing analysis logic in OpenHound has a steep learning curve, requiring advanced software engineering and database management knowledge and/or significant AI token spend with a real-world cost. This increases the effort, time, and cost for the community to contribute and makes SpecterOps extension support harder to scale.

Writing a bespoke collector in any programming language is simply easier for community members and researchers.

#### **Solution: Make our unified collection framework the most attractive option**

Executing server-side Cypher is a familiar concept to BloodHound users and makes it easier to:

- control when and how nodes and relationships get created, read, updated, or deleted
- create shared analysis and composition queries
- test and see immediate query results while developing extensions

This would enable research for the target technology using BloodHound throughout the development process.

Familiar query files and simple dependency declarations give researchers less to learn than a new workflow language or complex DuckDB operations. Analysis logic stays in the queries. The server handles ordering and execution. This also gives SpecterOps a smaller framework to build and maintain.

Further, it reduces the need for DuckDB ingest and analysis, reducing collector-side requirements, development time, and resource consumption.

---

## Collected data arrives at different times

A relationship can’t be created between a password stored in CyberArk and the corresponding Active Directory account every time matching Active Directory data is uploaded to BloodHound without conducting a subsequent CyberArk extension analysis.

#### **Solution: Define graph queries and their dependencies for an analysis engine on the BloodHound server to process**

This reduces collector complexity and duplicate development efforts needed to coordinate collection and analysis timing.

BloodHound should automatically rerun affected analyses when relevant data arrives, reuse shared calculations, and keep cross-system relationships current without rerunning each affected collector.

This is especially important if we want to enable new product features such as detection that can only be successful if data is graphed quickly and predictably in near real-time after collection from multiple sources.

---

## Remediations between uploads are not reflected in the graph

BloodHound cannot recognize remediation until new evidence is collected. Even after new data is uploaded, old attack paths can persist if supporting permissions remains stored or the relevant extension’s analysis is not rerun, even if the rerun extension’s analysis supports reconciliation (currently uncommon). 

This can make a completed remediation appear ineffective and cause teams to spend time investigating a risk they already addressed. 

The current solution for extension developers is to write BloodHound API and/or DuckDB queries to match and delete existing edges before new ones are created from the same logic. This is overly complex, increases development time, and adds risks associated with incomplete, poorly timed, or incorrect deletion of data.

#### **Solution: Identify the whether the scope of data collected is complete**

Each upload could identify the scope collected and whether collection was complete and successful. 

Partial uploads add or update evidence without treating omissions as removals. 

Complete uploads result in all previously collected graph data for that environment scope being deleted and recreated from the new data.

Explicit removals take effect immediately.

BloodHound then reruns affected analyses and removes calculated relationships that no longer have valid support, preserving the rest. 

An optional time to live marks data as expired or unverified after a specified period. 

---

## Small changes trigger full analysis

A limited update can trigger broad recalculation and rebuilding of unchanged results, consuming resources and delaying answers.

Uploading a change to one node can cause BloodHound to repeat calculations across unrelated parts of the environment and, for many calculated relationships, delete and recreate results that have not changed. As the graph grows, this consumes more processing power and memory, delays updated answers, and can temporarily leave some attack paths unavailable while they are rebuilt. 

#### **Solution: Use declared dependencies to rerun only the analyses that could be affected**

This would allow the server to perform shared calculations once and add or remove only relationships whose results have changed.

For example, changing a certificate template would update the certificate analyses that depend on it while leaving unrelated Active Directory account relationships intact.

---

## Risks span multiple sources

A serious risk can emerge from several settings that appear reasonable on their own. A collector examining only one part may miss that attack path.

The current solution involves each collector fetching the data from all relevant extensions and checking every possible condition for every relationship it is capable of creating during every run, all on the collector-side resources.

#### **Solution: Define analyses that evaluate the combined evidence already stored in BloodHound**

BloodHound can then run that analysis when relevant information arrives or changes, identify the resulting attack path, and remove it when updated evidence confirms the conditions have been corrected. 

With this approach, it is also possible to let extension developers define what happens when uploaded data matches data already in the graph:

- Delete existing edge and create one with the new data
- Merge the new data into the existing edge
- Do nothing, let the existing edge persist and discard the new data

---

## Results are temporarily unavailable during processing

When calculated relationships are deleted before their replacements are ready, attack paths that depend on them can disappear from the graph even though the underlying risk still exists. Users may see incomplete results or mistake a missing path for a successful remediation. If processing fails, that gap can persist. 

#### **Solution: Keep previous results available while calculating updates, then publish the changes together after completion**

Retain a usable graph throughout processing with a clear indication when results are awaiting an update or processing has failed.

---

## Different analyzers repeat the same queries

BloodHound’s current post-processing engine may perform the same work multiple times during execution. For example, ESC1 and ESC2 each calculate certificate trust separately. 

Similarly, different collectors may repeat the same queries independently of one another to analyze the same conditions required to create the same relationships. 

#### **Solution: Conduct each query once and share it between relationship analyses that depend on it**

This would reduce processing duration and resource consumption.

---

## Independently developed extensions can collide or silently affect one another

A design that works for one extension may not work once extensions are installed, updated, disabled, and removed independently.

One extension may use an answer calculated by another extension. If that answer changes—such as renaming a value or changing what it represents—the extension using it may break.

Also, two queries may independently find that the same connection should exist. Removing one query should not remove that connection if the other query still says it is valid.

These problems become harder to correct after extensions have been distributed. Changes to a shared answer can affect several downstream extensions, while relationships already stored in customer graphs may not contain enough information to reconstruct the assumptions used by an older rule set.

#### **Solution: Protect shared calculations and relationships**

Shared calculations should describe the columns, value types, meaning, and compatible versions of the answers they provide. Proposed updates should be checked against all affected consumers before activation.

When several rules produce the same relationship type, BloodHound should combine their complete answers before updating the graph. A relationship should be removed only when no participating producer still returns it within the declared endpoint scope.

---

## Different extensions rebuild the same capabilities

Each extension either needs to build its own foreign account resolution logic or build its own capability to fetch that data from the API and conduct collector-side analysis. 

#### **Solution: Keep shared capabilities server-side**

This will significantly reduce development and maintenance effort.

---

## Extensions are difficult to maintain after their authors leave

OpenHound implementations of relationship analysis logic varies wildly and is poorly understood because it primarily consists of SQL statements that are often AI-generated due to their complexity. 

#### **Solution: Use Cypher to define analysis**

Cypher can accomplish the same logic with far fewer lines of code, reducing maintenance costs and increasing readability.

Hosting the processing engine server-side makes this logic far easier to trace and debug, as the system produces an analysis plan in processing order that can be reviewed and can identify any specific query that fails. This query can be copied into BloodHound and refined to fix the issue.

Defining compatible source/target node kinds for each relationship kind in the OpenGraph schema would allow the entire model to be viewed in BloodHound without relying on external documentation/diagrams.

---

## Extensions need additional setup

Each customer must define the frequency at which each extension collector must pull data from the API and in which order to produce valid graph data at the desired pace. 

#### **Solution: Define all dependencies within the extension and have the server determine the most efficient processing order**

This will reduce support costs associated with repeated collector setup and maintenance.

# Recommended sequencing

Use a small set of deliberately different extensions to establish the shared contracts before expanding the catalog:

1. A complex analysis such as ESC1 should prove that the model can express multi-stage calculations and match independently written expected results.
2. A small OpenGraph extension should prove that custom collected data can be joined safely to native BloodHound data.
3. Two separate extensions should prove cross-extension dependencies, shared calculations, independent updates, and overlapping relationship producers.
4. Both supported databases should run the same written cases and return the same exact answers.

# Implementation

The pilot implements the core approach: **authors describe the connections they want, and BloodHound calculates and maintains them.** Authors supply Cypher files and a description of their model. Shared code handles the order of work, checks the answers, and saves the resulting relationships.

The pilot implements affected-rule selection when it has a complete account of changed graph types. First runs, changed rule sets, application restarts, unsupported writes, and incomplete or exhausted change tracking still run all enabled rules.

The pilot does not yet treat a new collection as a complete replacement for older data, automatically expire old evidence, or let an extension require a specific version and result format from another extension’s shared calculation.

## What runs where

| Part | Responsibility |
| --- | --- |
| Extension files | Each schema describes an extension's object and relationship types and exact directed endpoint pairs. `.cypher` files declare graph inputs, query prerequisites, calculations, literal output types, and optional existing-edge policies. |
| Rule container | Provides a page for uploading files, checks and stores them, and displays the latest analysis report. It does not need access to the graph database. |
| Modified BloodHound application | Runs the rules after its own analysis succeeds, using its existing database connection. It validates returned objects and relationships against the complete rule plan and saves the complete update. |

The customer installs the modified application and rule container once. Later rule uploads do not require rebuilding or restarting BloodHound. They take effect after the next successful BloodHound analysis; uploading rules alone does not run them.

The pilot works with Neo4j and PostgreSQL. Focused changes to DAWGS, BloodHound's database library, support the queries and saved results needed by the examples. The added schema settings are proposals for OpenGraph, not features already accepted into its official definition.

## How the pilot addresses each problem

| Problem | What the new code does | Current boundary |
| --- | --- | --- |
| **Useful research takes too long to become product coverage** | A researcher can add supported analysis by uploading query files and a schema. The shared engine handles ordering, checking, and saving results, so each rule does not need its own Go implementation. | The pilot demonstrates this workflow. It has not measured the reduction in development time or cost. |
| **SpecterOps is the bottleneck for graph expansion** | Authors describe the conditions for a connection and return the two objects to connect. The engine handles database changes and cleanup. The same schema generates an HTML model picture that helps explain and review the extension. | Authors still need to understand their data and write correct queries. New capabilities outside the supported query and calculation features may still require engine changes. |
| **Collected data arrives at different times** | After successful BloodHound analysis, the rules reconsider the information now stored in the graph. In the vault example, an account keeps its directory identifier, allowing a later run to connect it when the matching user arrives. | The vault collector does not need to run again solely because directory data arrived later. This depends on retaining the matching identifier; the engine cannot recover information that was never collected. |
| **Remediations between uploads are not reflected in the graph** | Once stored evidence changes, the engine recalculates which relationships remain justified. A relationship supported by several records remains while any support is present and is removed after explicit changes eliminate the final support. | Omission from an upload does not prove removal. Complete-collection replacement, a new explicit-removal message, and automatic expiry are not implemented. Earlier evidence that remains stored can still support an outdated result. |
| **Small changes trigger full analysis** | The application records changed object and relationship types. It selects affected rules, every producer sharing their relationship outputs, their downstream consumers, and the prerequisites needed to calculate them. | Selection operates on complete queries, not individual changed objects. Bounded tracking falls back to all enabled rules when it cannot prove complete coverage, including during sufficiently large uploads. |
| **Risks span multiple sources** | Queries can combine evidence already in BloodHound. The vault example joins a collected account's directory identifier to a native directory user without requiring the vault collector to fetch that user back from BloodHound. | The example demonstrates a factual cross-system association, not a general identity resolver or a claim that the association is an attack path. The engine can use only evidence actually stored in the graph. |
| **Results are temporarily unavailable during processing** | Previous extension relationships remain visible while the complete answer is calculated. New relationships, removals, updates, supporting helper objects, and the success record are saved together. A failure before saving leaves the previous extension answer available. | This protection covers this engine's results. It does not change how built-in BloodHound analysis replaces its own results. Old extension results may be out of date after a failure. |
| **Different analyzers repeat the same queries** | Several rules can name the same earlier query as a dependency. The engine calculates that shared answer once per run and passes it to each consumer. Independent queries can run together within a set limit. | Sharing is within the loaded rule set. Answers are not retained between runs, and this does not remove repeated work inside BloodHound's built-in ESC1 and ESC2 analyses. |
| **Independently developed extensions can collide or silently affect one another** | A query can use an answer calculated by another extension. When several queries support the same relationship, BloodHound combines their answers and keeps the relationship while any query still supports it. | Shared queries do not yet have formal versions or a declared result format. |
| **Different extensions rebuild the same capabilities** | The shared engine provides query ordering, reusable answers, result checking, and safe relationship updates for both databases. Several schemas participate in one dependency plan, and a query can consume another extension's answer. | A general library of independently versioned shared calculations and a general foreign-account resolver remain future work. |
| **Extensions are difficult to maintain after their authors leave** | Analysis stays in named text files. Dependencies produce an inspectable processing order, errors identify failed queries, and the model picture shows allowed connections. Another author can update the files without rebuilding BloodHound. | A query using answers from earlier queries needs those inputs when tested separately. Not every rule can be pasted unchanged into the query browser. |
| **Extensions need additional setup** | Once installed, BloodHound schedules analysis after its own checks. Customers do not have to arrange separate analysis schedules or manually order query files. Saved rules and their enabled states survive a container restart. | Customers still configure collection schedules and install the modified application, rule service, and access settings. Production still needs native authenticated rule management rather than the local pilot page. |

## What happens to a matching relationship

A producing query may use an `existing-edge-policy` comment to choose what happens when its answer matches an existing relationship. Every producer of the same relationship type must use the same policy.

Before updating that relationship type, the engine combines the complete answers from all participating producers. Existing relationships within the declared endpoint scope participate regardless of which extension or process originally created them. There is no extension ownership field stored on the relationship.

| Choice | Effect |
| --- | --- |
| `replace-existing` — default | Replace the old relationship with a new one containing the supplied details. Old details are discarded. |
| `merge-properties` | Keep the relationship and update only the supplied details. Unmentioned details remain; a supplied null value removes that detail. |
| `keep-existing` | Leave the matching relationship and its details unchanged. |

Every choice creates newly justified relationships and removes in-scope relationships that no participating producer still returns. “Keep existing” means to preserve the details of a relationship that remains justified; it does not mean to keep an unjustified relationship forever.

The update occurs only after all selected producers have returned complete, validated answers. A failed or incomplete calculation cannot authorize removal of an earlier result. Extra helper objects created for calculations are not automatically deleted when a later answer omits them.

## How BloodHound stays available

The application makes other graph changes wait while its own analysis and the extension rules run. That prevents the evidence from changing halfway through a calculation. **People can still search and view BloodHound**, although database load can slow their requests. Upload processing, deletion, and other changes may wait.

This required focused changes to BloodHound: settings for the rule service, a step after normal analysis, and a common waiting rule for graph updates. Database switching and migration require disabling the feature and restarting first. The protection covers one modified Community Edition application; a different application or a person writing directly to the database is outside it.

The rule page and application log report failures separately from collection success. A report may be old if the containers cannot communicate. If the database might have saved results but the confirmation was lost, graph changes remain blocked until the saved state is checked and BloodHound is restarted. Searches remain allowed.

An extension failure does not undo data already imported, BloodHound's completed analysis, or the separate work that registers the model and prepares the database.

## How the pilot prepares for larger graphs

The engine reuses shared calculations instead of following every possible path. It keeps large intermediate answers in temporary files, removes them when no longer needed, and reads and writes in smaller batches. Limits on time, answer size, disk use, and simultaneous queries stop excessive work.

Those smaller batches still belong to one complete update. Hitting a limit cannot turn a partial answer into permission to delete old results. These safeguards reduce avoidable work, but **performance at 100 million edges has not been demonstrated**. Memory estimates and one large final database update still need testing at representative scale.

## What the pilot has demonstrated

- **ESC1 agreement:** recorded automatic runs matched the same 11 expected connections as BloodHound's own analysis on both databases. Checks compare the actual connected objects, not just the number of relationships.
- **OpenGraph updates:** the vault example created a connection, removed it after explicit changes eliminated its support, and restored it when that support returned.
- **Failure protection and access:** recorded checks cover rule updates, failed calculations preserving earlier results, and searches during processing.

These are laboratory results, not a production or Enterprise release. One PostgreSQL demonstration still failed an upload file-count check, even though the expected connections appeared; later vault checks passed using separate uploads. The [review guide](REVIEW.md#evidence-recorded-on-8-october-2026) preserves that failure and the other limits of the evidence.

The [design guide](DESIGN.md) explains the mechanisms and identifies the BloodHound changes. The [usage guide](USAGE.md) explains how to run the pilot.
