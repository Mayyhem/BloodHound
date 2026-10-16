# Graph defaults and query details — 14 October 2026

Deployed at [OpenGraph Analysis](http://127.0.0.1:28080/ui/administration/opengraph-analysis).
Refresh the browser to load the updated assets.

## Requested changes

- Query-parameter edges read **As $name**, including any existing version condition. Helper-object and calculated-relationship arrows keep their distinct explanations.
- Nodes use Explore's combined renderer, normal size **25**, and its existing border. Edges share its normal thickness **4** and gray. Selecting a query does not enlarge it. Fit graph calls Explore's own reset, using its normal padding and camera limits.
- Query icons use the existing icon of one distinct environment kind. Missing, multiple, or unknown kinds use the analysis flask; several extensions can share an icon. Existing example schemas have no environments, so the live lab uses the fallback. Icon URLs remain stable across unchanged refreshes. Unknown names, including JavaScript object-property names, safely use the fallback.
- Drag the entity panel's left handle to change its width. Focus that handle and use Left/Right for keyboard control. The bottom table and top controls make room automatically. Inspection and resizing preserve the camera.
- The dependency-depth explanation is a tooltip. Broad-read notices explain in plain language why a query repeats after graph changes. Intermediate queries show that graph-output counts do not apply. Publishing queries distinguish missing counts, no save, and unconfirmed saving; their confirmed counts include existing or shared outputs rather than only newly added connections.
- Deletion review separates requested deletions from extra affected files and explains pruning. Deletion behavior and active query files are unchanged.

The optional environment-kind response field is derived from existing schema metadata. No schema or Cypher author requirement changed. DAWGS and calculation behavior are unchanged.

## Answers to the workflow questions

[Special Groups preview](special-groups-preview.json) was read without activation. Its answer feeds membership, which feeds ESC1. Deleting it therefore removes those consumers. Existing cleanup then removes supporting calculations that no longer feed any remaining output. In this lab, **12 ESC1 files** are listed; domain_helpers remains because it publishes its own helper output. Disable keeps the files. Graph cleanup follows the next successful analysis, not the preview.

Reachability became optional. The live lab was seeded with the advanced files under `extension/`. The ordinary Cypher example under `examples/esc1-cypher/` produces the same results without reachability, group-overlap, or batch-on directives. Replacing the active example requires a complete reviewed replacement; removing the comments alone would remove part of the calculation. This follow-up did not replace the lab's files.

A broad read does not name every object/connection type. The engine therefore cannot narrow which changes matter and repeats the query after graph changes to avoid stale answers. This is a precaution, not a validation failure.

`usable_cas` returns intermediate rows, not published graph objects or relationships. Its graph-output counts are not applicable. A publishing query's proposed count describes its checked output rows; a confirmed count requires successful saving and can include existing/shared results. A missing count never means zero.

Last-run intermediate answers are currently removed with temporary calculation files. Status reports retain counts and outcomes, not the matching objects. A useful next step is bounded retained answers with run/rule/schema identifiers and graph-read access, then a view of the matching collected objects. Resolving those identifiers against today's graph must be labeled as current properties; it cannot claim a historical graph snapshot. This remains in the answer-inspection roadmap.

## Verification

The focused interface run passed **40 tests**; the subsequent workspace rerun passed **20**, adding publishing-count and cascade-review cases to the prior 18 workspace cases. Together these cover **42 distinct focused cases**. Complete rule-service/postprocessing and native API/registration checks passed. Type, style, formatting, production UI, native app, and rule-service builds passed.

The full **just test passed**, including JavaScript, Go tests, and coverage reporting. Its command log is `work/workspace-graph-details/just-test.log`. The final one-line unknown-icon guard was checked separately by browser, style, and production build after that run began. Earlier management-run failures remain preserved in their separate evidence; they were not reproduced in this passing run. Preparation for code review is still required before a manual commit; no successful new preparation run is claimed.

[Browser results](browser-results.json) cover both themes, native renderer/border/size, thickness, short labels, environment/fallback icons, stable URLs on refresh, native fit defaults, pointer/keyboard width controls, tooltip, camera preservation, edge selection, and a narrow viewport. A browser attempt timed out while the full suite competed for resources; the later check passed after the suite finished.

Live checks use normal login and actual deployed responses. They inspect details and downloads, change display width, and discard a Special Groups deletion preview. They do not activate, upload, or request analysis. Final results and versions are in `live-browser-results.json`, `live-api-results.json`, and `deployment-state.json`.

## Review and rollback

Only the named app/rule-service containers were replaced. Database identities/start times and active rule-file hash are checked against the recorded pre-deployment state. Previous images and configuration remain available.

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-graph-details/compose-before.yaml \
  up -d --no-deps --no-build --pull never rule-service bloodhound
```

If retaining the rollback, restore the earlier image settings in the active Compose file too. The earlier live report contains a separate domain_helpers check-answers failure with saving not-started; these display changes do not establish or fix its cause.

Nothing was staged or committed. The changed-file list includes the whole uncommitted workspace feature. Review it and run preparation in the isolated checkout before a manual commit:

```bash
git diff --stat
cat tools/esc1-poc/evidence/analysis-workspace-graph-details/changed-files.txt
(cd tools/esc1-poc/work/workspace-graph-details/build-checkout && just prepare-for-codereview)
git add --pathspec-from-file=tools/esc1-poc/evidence/analysis-workspace-graph-details/changed-files.txt
git diff --cached --stat
git commit -m "feat: add OpenGraph analysis workspace"
```

These staging/commit commands are suggestions only. Logs, screenshots, browser libraries, and isolated build artifacts remain under ignored work directories. Historical evidence remains unchanged.
