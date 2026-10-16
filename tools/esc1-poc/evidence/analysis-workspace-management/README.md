# Explore-style analysis workspace and rule management — 14 October 2026

The requested layout and management controls are running at
[OpenGraph Analysis](http://127.0.0.1:28080/ui/administration/opengraph-analysis).
The lab login remains `admin` / `experiment-only`. Refresh the browser to load the new assets.

## What changed

- The graph fills the page. The bottom rules/report panel has a drag handle and Up/Down keyboard resizing. Selecting a node, edge, or row opens the right entity panel and narrows the table. The camera stays put during inspection and resizing; Fit graph uses the visible space.
- Individual Enable, Disable, and Delete actions are in the entity panel. Table checkboxes support multiple-rule actions and show how many selected rules are hidden by filters. Inspection focus never checks additional rules.
- Collapsible controls at the top left provide uploads, inactive draft downloads/discard, review notices, activation, Run Analysis, and Refresh rules and status. The Latest status report button opens the report in the bottom panel.
- Metadata and legend labels are bold, with one item per line. Prior status colors, dependency depth, analysis versions, gray arrows, and faded unrelated paths remain. Thin arrows can be selected within six screen pixels when zoomed out.
- Native management reuses the existing rule service, exact revision checks, per-file overwrite choices, and complete cascade review. The proposed graph/table are shown during review. Invalid drafts stay inactive and downloadable. Generic connection failures and fixed draft guidance do not expose parser excerpts or secrets.
- Run Analysis uses the existing native action as the signed-in user. It does not borrow the service's stored BloodHound credentials, pin a diagram revision, or automatically retry an uncertain request.

No author-file requirements, query execution, DAWGS code, or database schema changed. The older rules page remains available. Collapsed graph groups, historical diagrams, live per-query progress, isolated tests, and answer inspection are still later work.

## Verification and limits

Focused interface checks passed: **41 tests in 7 files**, including existing Explore and Administration checks. Complete affected native API/route tests and complete rule-service/postprocessing tests passed. These cover permissions, duplicate-key and oversized requests, first upload through activation, stale schema review, safe errors, malformed replies, and redirect refusal. Type/style checks and production builds passed. The final notice correction has its own focused rerun in `results.json`.

[Browser checks](browser-results.json) passed real canvas rendering, edge clicks, dragging, table narrowing, report switching, bulk review, stale activation rejection, invalid-draft downloads/discard, explicit run requests, both themes, and a narrow viewport. These management replies were simulated and did not change the lab.

[Live browser checks](live-browser-results.json) used normal login and deployed responses: 16 queries, exact original download, dependency navigation, filters, drag resizing, table narrowing, reports, and a bulk preview that was discarded. No upload, activation, or analysis request was made during that check. Workspace, draft, preview, and download requests returned 200, without page errors.

The required full `just test` outcome is recorded separately in `results.json`; focused passing checks are not a claim that the full suite passes. Logs and screenshots are in ignored `tools/esc1-poc/work/workspace-management/`. OpenAPI source and bundled JSON were updated; every reference resolves and existing definitions are semantically unchanged. The normal generator remains unavailable offline as recorded in the initial workspace evidence. `just prepare-for-codereview` has not been rerun successfully for this follow-up and remains required before a manual commit.

The lab separately reported an existing `PoC/domain_helpers.cypher` failure during `check-answers`, with saving `not-started`. This task does not establish or repair its cause. See [the observed live state](live-api-results.json); interface checks do not prove successful analysis of the lab data.

## Deployment and review

Only the application and rule-service containers were replaced. Both database containers retained their identities/start times, and the active rule file hash was preserved, including the user's manual selections. Previous images and Compose configuration remain available. Final image/source details are in `deployment-state.json` and `build-source-files-final.json`.

To restore the deployment that preceded these management changes:

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-management/compose-before.yaml \
  up -d --no-deps --no-build --pull never rule-service bloodhound
```

If keeping that rollback, restore the earlier image settings in the active Compose file too.

Nothing was staged or committed. Review the whole uncommitted workspace feature using the supplied file list. Run preparation in an isolated checkout without ignored lab databases, as explained in the initial evidence. After resolving the recorded check limitations, the user can run:

```bash
git diff --stat
cat tools/esc1-poc/evidence/analysis-workspace-management/changed-files.txt
(cd tools/esc1-poc/work/workspace-management/build-checkout && just prepare-for-codereview)
git add --pathspec-from-file=tools/esc1-poc/evidence/analysis-workspace-management/changed-files.txt
git diff --cached --stat
git commit -m "feat: add OpenGraph analysis workspace"
```

The staging and commit commands are suggestions only and were not executed.
