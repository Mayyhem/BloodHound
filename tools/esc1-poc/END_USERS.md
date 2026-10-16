# Using extensions in BloodHound

Use this guide for extensions someone else has written. It assumes an administrator has enabled this experimental feature.

## What files do I upload, and where?

| File | What it provides | Where to upload it |
| --- | --- | --- |
| Schema (`schema.json` or `name.schema.json`) | Object and connection types, and how they appear. | BloodHound’s OpenGraph Management page. |
| Ruleset (`.cypher` files, or a `.zip` of them) | Instructions for finding connections in collected data. | Administration → OpenGraph Analysis → Upload button in the graph's bottom-left corner. |
| Collection payload (supported JSON or ZIP) | Facts gathered from your environment. | BloodHound's normal data upload page. |

The native workspace is **Administration → OpenGraph Analysis**. The older lab rules page remains available at <http://127.0.0.1:18081/>. You do not need to understand the files' code.

## How do I start using an extension?

1. Upload its schema through **OpenGraph Management**. Click the **Upload** button in the bottom-left corner of the workspace graph, select its `.cypher` files or a `.zip` containing them, and click **Save upload and review**.
2. Check the proposed additions and replacements. Then click **Activate reviewed revision**. A revision identifies one saved version of the rules.
3. Upload collection data to BloodHound, or click **Run Analysis** to use data already stored.
4. Open **Administration → OpenGraph Analysis** and click **Refresh rules and status** to inspect current rules. **Latest status report** opens the completed report in the bottom panel. Check the report before exploring calculated connections in BloodHound.

**Activation saves the instructions; successful analysis updates the results.** This gives you a chance to review changes before they affect the graph.

The [workspace instructions](USAGE.md#inspect-the-analysis-workspace) explain query selection, filters, downloads, and the difference between calculation success and confirmed saving.

## Can I install three extensions at different times?

Yes. Uploads add to the saved files. Activate each complete extension when ready. The engine combines all active rules into one plan, including connections between extensions.

Different extensions may supply the same filenames. Their declared namespaces keep them separate. Upload schemas on OpenGraph Management, then review each extension’s analyses on the rule page. A file uploaded before its schema remains an inactive draft.

## What if I only have some of the required files?

Save them as a draft and add the missing files later, in either order. The page explains what is missing and prevents activation until the complete set passes its checks. Existing active rules keep working.

There is one shared draft, which survives refreshes and restarts. **Discard preview** keeps it; **Discard saved draft** removes it. Neither changes active rules. This keeps incomplete instructions out of analysis.

The complete set is limited to 32 files, 1 MiB per file, and 4 MiB total. If files are rejected, share the message with the supplier.

## What happens if data for three extensions arrives at different times?

Suppose directory data arrives Monday, vault data Tuesday, and cloud data Friday:

- Monday's analysis uses the facts available then.
- Tuesday's analysis can connect vault data to Monday's directory data.
- Friday's analysis can use all three sources together.

This requires rules describing those connections and matching facts in the data. **You do not need to re-upload earlier collections.** Rules consider all relevant stored data, so reversing the arrival order can produce the same final connections once the same facts are stored.

## Do I need to choose which extension runs first?

No. Authors' instructions tell the engine which calculations must finish before others can use their answers. Shared calculations run once per analysis; unrelated work can run together.

**Order** and **Dependency depth** describe this plan in the analysis workspace. Depth is the number of arrows in the longest prerequisite chain leading to a query, starting at 0. The older rules page uses **Stage**, starting at 1. Sorting or filtering only changes your view. If no workable order exists, activation is blocked; ask the supplier for corrected files.

## Why did some rules run while others were skipped?

The engine runs rules affected by changed types of data and their supporting rules. Unaffected results stay saved, avoiding unnecessary work.

First runs, rule changes, application restarts, or uncertainty about what changed run all enabled rules.

The engine limits the extra work spent tracking changes during imports. Large uploads can exceed those limits, leaving an incomplete picture of what changed. It then runs all enabled rules because skipping one could leave outdated results. File size alone does not decide this. Disabled rules stay paused.

## Can I run the rules without uploading more data?

Yes: click **Run Analysis**. After BloodHound's own checks succeed, it runs the affected enabled rules. This uses stored facts without collecting fresh information or forcing every rule to run.

A queued request is still waiting. Check the report for completion. If the button's connection is unavailable, ask your administrator to configure it; automatic processing after uploads can still work.

## How do I update an extension?

Upload schema updates through OpenGraph Management. On the rule page, upload just the changed or new analysis files, keeping their filenames. For a changed file, choose **Overwrite this file** or **Skip this file**, then review and activate. Overwrite replaces the whole file. Files omitted from the upload remain installed.

Review checks the files without running them against your data, so acceptance does not guarantee successful analysis. Updates need no application restart.

## What if rules change while analysis or review is underway?

An extension analysis keeps the version it started with. Changes apply to a later analysis, so one run never mixes old and new instructions.

An outdated review cannot replace newer work. Refresh and review again. If active rules changed underneath a draft, download its files, discard it, and upload again for a fresh review.

## What happens when I delete a specific rule?

Use its trash button or **Delete Selected**, then review the confirmation:

- Rules that directly need its answer are also deleted because they can no longer run. Unneeded supporting rules are removed too.
- Other rules producing the same connection type remain. Rules reading those connections from the graph remain and recalculate later.
- These effects can reach other extensions; check the full list before confirming.

Graph changes wait for successful analysis. Connections remain if another rule still finds them. Deleting the last rule for a connection type schedules cleanup of that type between the object types it managed.

Collected objects, schema definitions, and supporting objects created by rules remain. To select an extension's rules together, filter by extension and use the heading checkbox; review any effects on other extensions.

## How is disabling a rule different from deleting it?

**Disable** keeps files and existing results. It also pauses rules producing the same connection type and rules depending on those results, even across extensions. Recalculating only part of a shared answer could erase valid connections. Independent rules continue.

**Enable** lets rules resume on a later analysis unless they or another required rule remain disabled. Check the table's explanation. Preserved results can become outdated while paused.

## Will overlapping rules or repeated uploads create duplicate connections?

The engine combines matching connections of the same type and direction between the same objects. Rechecking the same stored facts does not add copies. A connection remains while any producing rule still finds it, because another justification still exists.

## Will a later upload remove connections that are no longer valid?

For connection types being checked, successful analysis removes connections no producing rule finds in the stored facts. An empty successful answer can therefore clear earlier results.

**Omission from a later collection does not prove deletion.** Old facts may still justify a connection. A real-world fix appears only when stored evidence reflects it. Supporting objects created by rules are not automatically removed when missing from later answers.

## Can analysis change a connection I edited or created manually?

Yes, if its connection type and object types are managed by a rule. The engine checks those connections regardless of who created them.

Authors choose whether still-valid connections are replaced, have details updated, or stay unchanged. Replacement is the default. Every option removes unjustified connections, so manual edits are not a permanent override.

## How do I know the results are current?

Open **Latest analysis report** and check:

- **Time and revision:** compare the report and last confirmed saved revision with your active rules.
- **Calculation and save outcomes:** finishing calculations alone does not confirm that results were saved.
- **Skipped or paused rules:** read their reasons before expecting new results.

Proposed counts describe intended changes; saved counts confirm them. Adding rule counts can overcount shared connections. The latest 20 attempts are retained. A successful collection upload alone does not prove extension success.

## Why are there no new connections after a successful run?

There may be no matching facts, answers may already be correct, or no rules may be affected. Check the report's reasons and counts. Success does not guarantee a finding or prove that all needed data was collected.

## What happens if one rule fails?

A failure before saving discards the run's extension changes, including other rules' changes. Previous results remain but may be outdated. This prevents partly updated answers. Imports and BloodHound's own completed analysis are not undone.

Share the failed rule name and report with its supplier or administrator. Activate corrected files and request new analysis. Automatic retries use the original files. If another graph update has been attempted, the engine waits for new successful analysis before trying again.

## What does an unavailable or uncertain report mean?

**Unavailable:** there is no reliable current completion report. An old report does not prove processing is healthy. Ask your administrator to check.

**Uncertain save:** results may have been saved but confirmation was lost. BloodHound blocks graph changes to avoid building on an unknown result; searches remain available. An administrator must check saved results before restarting.

## Can I keep using BloodHound during analysis?

Yes. Previous extension results remain available until replacements are saved together. Upload processing, deletion, and other graph changes may wait to keep facts stable during analysis. Searches remain available but may slow down.

## Will every new connection appear in attack paths?

No. Some connections describe associations without demonstrating an attack step. The schema decides which types pathfinding can follow. You can inspect other associations in the graph even when pathfinding does not use them.

For administrator setup and detailed controls, see [USAGE.md](USAGE.md).

## What happens when a schema is deleted or becomes incompatible?

The analysis files remain available on the rule page. Affected calculations and other rules depending on them pause; previous calculated results are kept and shown as potentially stale. Other independent extensions can continue. The worker cannot reinstall the deleted schema.

After correcting or reinstalling the schema, review **Enable** for the retained paused files. Changing the schema version does not require new Cypher files when the author’s compatibility conditions and the other checks still hold. Each report records the query and schema versions used.

For now, deleting an extension does not delete its analysis files. A future deletion option will offer that choice. The OpenGraph Analysis workspace manages analysis files; schema uploads stay in OpenGraph Management.

## How do I work with the graph, table, and entity panel?

Drag the handle at the top of the bottom table to resize it. Click a node or row to open its entity panel on the right; the table makes room. Close the panel to regain that space. **Rules** and **Analysis report** switch the bottom panel's contents.

For one rule, its entity panel offers **Disable** if the rule is enabled or **Enable** if it is disabled, plus **Delete**. For multiple rules, check their table boxes and use the selected action. Focused paths are for inspection and do not add checked rules. The count includes any checked rules hidden by filters. Every change opens a review listing all affected rules before activation. A review may include extra dependent files when deleting a prerequisite. Existing graph results change only after successful analysis.

Use the Upload button in the graph's bottom-left corner to add `.cypher` files or a `.zip` of them; its window keeps inactive draft downloads and checking advice. The top-left panel can be collapsed. If rules changed elsewhere or a response was lost, refresh and review again before activating. You need OpenGraph write permission to change rules; Run Analysis uses BloodHound's existing application-configuration permission.

## Graph styling and interpreting query details

Query nodes use Explore's normal node renderer, size, border, and camera reset. Dependency arrows use its normal thickness and gray. Arrows carry no labels on the graph, to keep it readable; click an arrow, or open a query's **Overview**, to see what it passes along (**As $name**, or the helper-object or calculated-relationship kind being waited for). The entity panel's left handle changes its width by dragging; keyboard users can focus it and use Left/Right. The bottom panel makes room as the width changes. The dependency-depth explanation is a tooltip next to the value.

Extensions are numbered in the filter dropdown, and each query node shows its extension's number inside the circle. Numbers follow namespace order across the complete plan; filtering, selecting, or refreshing an unchanged plan does not renumber them. Environment kinds no longer choose graph icons. Node and edge text colors update when switching between light and dark mode. These display choices add no author requirement and do not change calculations.

The broad-read notice means some graph reads do not name an object or connection type. The engine cannot narrow which changes affect them, so it repeats them after graph changes rather than keep outdated results. It is not a validation failure. An intermediate query returns rows for later queries; graph-output counts are not applicable. For a publishing query, proposed output rows describe its checked answer before saving. Confirmed output rows require successful saving and may include existing connections or overlap other producers; they are not counts of newly added connections. Missing counts are labeled unreported, unsaved, or unconfirmed rather than zero.

Deleting `special_groups.cypher` removes `membership.cypher`, then `esc1.cypher`, because they require its answer rows. The existing deletion cleanup then removes supporting queries that no longer feed any remaining graph output. In the current advanced lab, 12 ESC1 files are listed for removal; `domain_helpers.cypher` remains because it has its own helper output. The review now distinguishes requested deletions from additional files and explains this behavior. Disable keeps the files. The UI does not change the existing deletion policy.

Reachability is optional, not removed. The active lab contains the advanced example in `extension/`. `examples/esc1-cypher/` calculates the same result using ordinary Cypher without reachability, group-overlap, or batch-on directives. Removing a directive from an advanced seed query would remove part of its calculation; switching examples requires replacing the complete reviewed bundle, not deleting comments.

Every editor test recalculates the needed prerequisite chain against the current graph; saved answers are never reused, so a test cannot show stale results or refer to objects that no longer exist. The file being tested also runs fresh; its rows are read while temporary data exists, then displayed as a table after it is discarded. Run Analysis still calculates its inputs and publishes graph results normally.
