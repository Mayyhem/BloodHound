# Using extensions in BloodHound

Use this guide for extensions someone else has written. It assumes an administrator has enabled this experimental feature.

## What files do I upload, and where?

| File | What it provides | Where to upload it |
| --- | --- | --- |
| Schema (`schema.json` or `name.schema.json`) | Object and connection types, and how they appear. | The rule page supplied by your administrator. |
| Ruleset (`.cypher` files) | Instructions for finding connections in collected data. | The rule page, with its schema. |
| Collection payload (supported JSON or ZIP) | Facts gathered from your environment. | BloodHound's normal data upload page. |

The lab's rule page is at <http://127.0.0.1:18081/>. You do not need to understand the files' code.

## How do I start using an extension?

1. Select its schema and all supplied rule files. Click **Save upload and review**.
2. Check the proposed additions and replacements. Then click **Activate reviewed revision**. A revision identifies one saved version of the rules.
3. Upload collection data to BloodHound, or click **Run Analysis** to use data already stored.
4. Click **Refresh rules and status** and check **Latest analysis report** before exploring the connections in BloodHound.

**Activation saves the instructions; successful analysis updates the results.** This gives you a chance to review changes before they affect the graph.

## Can I install three extensions at different times?

Yes. Uploads add to the saved files. Activate each complete extension when ready. The engine combines all active rules into one plan, including connections between extensions.

Filenames must be unique across extensions. If another extension already uses `schema.json`, name the new schema something like `vault.schema.json` before its first upload. Keep that name for updates. Ask the supplier to resolve conflicting rule filenames, since other rules may refer to them.

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

**Order** and **Stage** describe this plan. Sorting or filtering only changes your view. If no workable order exists, activation is blocked; ask the supplier for corrected files.

## Why did some rules run while others were skipped?

The engine runs rules affected by changed types of data and their supporting rules. Unaffected results stay saved, avoiding unnecessary work.

First runs, rule changes, application restarts, or uncertainty about what changed run all enabled rules. Large uploads can also require this. Disabled rules stay paused.

## Can I run the rules without uploading more data?

Yes: click **Run Analysis**. After BloodHound's own checks succeed, it runs the affected enabled rules. This uses stored facts without collecting fresh information or forcing every rule to run.

A queued request is still waiting. Check the report for completion. If the button's connection is unavailable, ask your administrator to configure it; automatic processing after uploads can still work.

## How do I update an extension?

Upload just the changed or new files, keeping existing filenames. For a changed file, choose **Overwrite this file** or **Skip this file**, then review and activate. Overwrite replaces the whole file. Files omitted from the upload remain installed.

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
