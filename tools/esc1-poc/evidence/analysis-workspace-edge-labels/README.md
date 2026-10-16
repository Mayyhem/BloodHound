# Edge labels follow zoom — 14 October 2026

Deployed at [OpenGraph Analysis](http://127.0.0.1:28080/ui/administration/opengraph-analysis).
Refresh the browser to load the new assets.

The workspace used Sigma's fixed-size edge labels while its node labels used Explore's zoom-aware drawing. `QueryGraph.tsx` now uses Explore's edge-label drawing and supplies the current camera zoom on each redraw. Edge text therefore scales with node text. Its background uses the canvas color so arrows do not show through the words. No query, execution, author-file, or deletion behavior changed.

## Verification

- All **31 focused workspace tests** passed. Type, style, formatting, production UI, and native app builds passed.
- [Actual canvas drawing](browser-results.json) was checked in both themes at camera ratios 1, 2, 0.5, then 1. Edge text measured 12, 8.485, 16.971, then 12 pixels, following Explore's existing scaling. There were no page errors.
- [Normal-login deployed checks](live-browser-results.json) used actual server responses and the zoom buttons. Edge text grew from 10.106 to 12.377 pixels, then shrank to 10.106 and 8.251 pixels. No rules were changed and no analysis was requested.
- Full `just test` failed after **12m19s** in `bh-shared-ui`. The captured workspace wrapper reports exit 1 without individual test failures; this run therefore does not establish their cause. Later dependent JavaScript workspaces and the Go phase were not reached. The preceding graph/details full run passed and remains preserved separately. Log: `work/workspace-edge-labels/just-test.log`.

The new app image is `esc1-poc-bloodhound-workspace:20261014-edge-labels`. [Deployment evidence](deployment-state.json) verifies that only the app container was replaced. The rule service and both database containers retained their identities and start times. The active rule file still matches the prior deployment's recorded checksum. A checksum of the entire rule directory differed; that directory also contains runtime status and the inactive draft, so its checksum is not used as the active-rule preservation check.

[API checks](live-api-results.json) confirm normal login and successful workspace/draft reads. The separate `domain_helpers` answer-checking failure remains in the existing report, before saving. This display fix does not establish or fix its cause.

[Source hashes](build-source-files.json) identify the working files copied into the isolated build checkout. [Source verification](source-verification.json) confirms compiled files match the snapshot and records BloodHound and DAWGS versions. Documentation written after that snapshot is outside the compiled source. Earlier evidence folders were preserved.

## Review and rollback

This follow-up changes `cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx`, explains it in `tools/esc1-poc/DESIGN.md`, and adds this evidence and a handoff note. The entire workspace feature remains uncommitted. Logs, screenshots, the browser check scripts, isolated build, and the exact comparison against the previous deployed source are under ignored `tools/esc1-poc/work/workspace-edge-labels/`.

To restore the previous app without restarting the databases or rule service:

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-edge-labels/compose-before.yaml \
  up -d --no-deps --no-build --pull never bloodhound
```

If keeping the rollback, also restore the previous app image in the active Compose file. Nothing was staged or committed. Before a manual commit, review all accumulated workspace changes and run the required preparation in the isolated checkout:

```bash
git diff --stat
cat tools/esc1-poc/evidence/analysis-workspace-edge-labels/changed-files.txt
(cd tools/esc1-poc/work/workspace-edge-labels/build-checkout && just prepare-for-codereview)
git add --pathspec-from-file=tools/esc1-poc/evidence/analysis-workspace-edge-labels/changed-files.txt
git diff --cached --stat
git commit -m "feat: add OpenGraph analysis workspace"
```

These are suggested commands only. Preparation has not been rerun successfully; the earlier offline tool limits remain documented in the original evidence. No successful preparation run is claimed.
