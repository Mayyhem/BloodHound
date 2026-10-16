# Ordinary Cypher example in the test app — 14 October 2026

The user requested the ordinary Cypher ESC1 example alongside the advanced example and confirmed a separate namespace.

Open [OpenGraph Analysis](http://127.0.0.1:28080/ui/administration/opengraph-analysis), refresh, and filter or search for **PoC_Cypher**. Its **13 files** are active and enabled. The workspace now has **29 files** in total.

The source is the existing `examples/esc1-cypher/` example. The lab copy changes its namespace from `PoC` to `PoC_Cypher`, its schema name to `esc1_poc_cypher`, its display name, and its own `PoC_` output-kind/object-prefix names to `PoC_Cypher_`. Ordinary Cypher logic and native collected inputs remain the same. This permits distinct results beside the advanced example; it adds no author requirement. The shipped examples were not edited.

## Verification and preserved state

The adapted example passed the local plan command. The first attempt used the script's empty local module cache and failed because downloads are disabled; using the already populated cache succeeded. No dependency downloads were needed.

The schema was added through the native schema endpoint. A native rule preview verified no deletions or overwrites, exact preservation of the existing files and author choices, and addition of all 13 files. Only that exact reviewed revision was activated. The existing inactive draft was preserved. [Results](results.json) record versions and source checksums; private before/after snapshots and the full preview remain under ignored `work/ordinary-cypher-example/`.

[Normal-login browser checks](browser-results.json) passed: 29 rows overall, 13 ordinary-example rows after filtering, an entity panel, and an exact original-file download. No page errors occurred. No manual Run Analysis request was made. Installing a schema can queue the app's normal analysis. These checks do not claim successful calculation or publication.

The advanced example retains its prior enabled/disabled choices. A normal analysis includes all enabled files, so its separate recorded `domain_helpers` answer-checking failure can still prevent a complete run. Both examples share that helper-query logic; this content addition does not establish or fix the underlying failure.

The app remains `esc1-poc-bloodhound-workspace:20261014-edge-labels`. No containers were replaced for this addition. The previous edge-label check's full `just test` failed in shared UI and is recorded separately; no code was changed for adding this data. Nothing was staged or committed.

## Create analysis file discussion

No editor feature has been implemented. The user explicitly requested agreement on its behavior first. Decisions and open questions are recorded in the workspace plan and handoff. Adding this live example is separate from that design discussion.
