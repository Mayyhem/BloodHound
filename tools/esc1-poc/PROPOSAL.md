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

---

## Extensions need additional setup

Each customer must define the frequency at which each extension collector must pull data from the API and in which order to produce valid graph data at the desired pace. 

#### **Solution: Define all dependencies within the extension and have the server determine the most efficient processing order**

This will reduce support costs associated with repeated collector setup and maintenance.

# Implementation

The pilot implements the core approach: **authors describe the connections they want, and BloodHound calculates and maintains them.** Authors supply Cypher files and a description of their model. Shared code handles the order of work, checks the answers, and saves the resulting relationships.

Some proposed capabilities remain future work. In particular, the pilot does **not** replace a complete collection, expire old evidence, or automatically limit each run to only the analyses affected by a change.

## What runs where

| Part | Responsibility |
| --- | --- |
| Extension files | `.cypher` files describe the analysis. `schema.json` describes object types, allowed connections, which query produces each result, and what happens to an existing matching relationship. |
| Rule container | Provides a page for uploading files, checks and stores them, and displays the latest analysis report. It does not need access to the graph database. |
| Modified BloodHound application | Runs the rules after its own analysis succeeds, using its existing database connection. It checks which results belong to the extension and saves the complete update. |

The customer installs the modified application and rule container once. Later rule uploads do not require rebuilding or restarting BloodHound. They take effect after the next successful BloodHound analysis; uploading rules alone does not run them.

The pilot works with Neo4j and PostgreSQL. Focused changes to DAWGS, BloodHound's database library, support the queries and saved results needed by the examples. The added schema settings are proposals for OpenGraph, not features already accepted into its official definition.

## How the pilot addresses each problem

| Problem | What the new code does | Current boundary |
| --- | --- | --- |
| **Useful research takes too long to become product coverage** | A researcher can add supported analysis by uploading query files and a schema. The shared engine handles ordering, checking, and saving results, so each rule does not need its own Go implementation. | The pilot demonstrates this workflow. It has not measured the reduction in development time or cost. |
| **SpecterOps is the bottleneck for graph expansion** | Authors describe the conditions for a connection and return the two objects to connect. The engine handles database changes and cleanup. The same schema generates an HTML model picture that helps explain and review the extension. | Authors still need to understand their data and write correct queries. New capabilities outside the supported query and calculation features may still require engine changes. |
| **Collected data arrives at different times** | After successful BloodHound analysis, the rules reconsider the information now stored in the graph. In the vault example, an account keeps its directory identifier, allowing a later run to connect it when the matching user arrives. | The vault collector does not need to run again solely because directory data arrived later. This depends on retaining the matching identifier; the engine cannot recover information that was never collected. |
| **Remediations between uploads are not reflected in the graph** | Once an upload changes the stored evidence, the engine calculates which relationships remain justified and removes obsolete results it owns. A vault-to-user connection stays while any exposed account supports it, and disappears when the last support is corrected. | Omission from an upload does not prove removal. Complete-collection replacement, a new explicit-removal message, and automatic expiry are not implemented. Earlier evidence that remains stored can still support an outdated result. |
| **Small changes trigger full analysis** | Declared dependencies establish the order of calculations. Authors can choose to preserve unchanged relationships instead of replacing them, avoiding unnecessary writes. | The automatic service still runs the full extension plan after successful native analysis. Selecting only affected analyses is future work. The default relationship policy still replaces matching results. |
| **Risks span multiple sources** | Queries can combine evidence already in BloodHound. The vault rule checks exposed accounts, their directory identifiers, and the matching user's enabled state before creating a connection. No collector needs to fetch those directory objects back out for this analysis. | The engine can use only the evidence present in the graph. This example is a vault-to-directory match, not a general solution for matching every identity across every system. |
| **Results are temporarily unavailable during processing** | Previous extension relationships remain visible while the complete answer is calculated. New relationships, removals, updates, supporting helper objects, and the success record are saved together. A failure before saving leaves the previous extension answer available. | This protection covers this engine's results. It does not change how built-in BloodHound analysis replaces its own results. Old extension results may be out of date after a failure. |
| **Different analyzers repeat the same queries** | Several rules can name the same earlier query as a dependency. The engine calculates that shared answer once per run and passes it to each consumer. Independent queries can run together within a set limit. | Sharing is within the loaded rule set. Answers are not retained between runs, and this does not remove repeated work inside BloodHound's built-in ESC1 and ESC2 analyses. |
| **Different extensions rebuild the same capabilities** | The shared engine provides query ordering, group and trust calculations, result checking, and safe relationship updates for both databases. Authors reuse those capabilities instead of writing them again in each collector. | The pilot accepts one active schema containing multiple analyses. A library shared across independently installed extensions, and a general foreign-account resolver, remain future work. |
| **Extensions are difficult to maintain after their authors leave** | Analysis stays in named text files. Dependencies produce an inspectable processing order, errors identify failed queries, and the model picture shows allowed connections. Another author can update the files without rebuilding BloodHound. | A query using answers from earlier queries needs those inputs when tested separately. Not every rule can be pasted unchanged into the query browser. |
| **Extensions need additional setup** | Once installed, BloodHound schedules analysis after its own checks. Customers do not have to arrange separate analysis schedules or manually order the extension's query files. Saved rules survive a container restart. | Customers still configure collection schedules and install the modified application, rule service, and access settings. Existing analysis declarations cannot currently be removed or reassigned through uploads. |

## What happens to a matching relationship

The schema's `existing_edge_policy` setting provides the three choices proposed above. It applies to **calculated relationships owned by the extension**, not to collection ingestion.

| Choice | Effect |
| --- | --- |
| `replace-existing` — default | Replace the old relationship with a new one containing the supplied details. Old details are discarded. |
| `merge-properties` | Keep the relationship and update only the supplied details. Unmentioned details remain; a supplied null value removes that detail. |
| `keep-existing` | Leave the matching relationship and its details unchanged. |

Every choice creates newly justified relationships and removes owned relationships that no longer have support. “Keep existing” does not mean “keep forever.” If an existing result has missing or conflicting ownership, the run stops instead of taking it over or deleting it.

Extra helper objects created for calculations are not automatically deleted when a later answer omits them.

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
