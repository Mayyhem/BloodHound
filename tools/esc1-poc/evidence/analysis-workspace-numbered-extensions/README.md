# Analysis workspace contrast and extension numbers — 14 October 2026

This follow-up fixes graph text contrast in dark mode and numbers each
extension consistently in the Extension filter and its query-node icon. The
numbers follow namespace order from the complete plan, so filtering does not
renumber them. Environment-kind icons are not used. These are display changes;
they do not change rules or calculations.

The deployed application image is
`esc1-poc-bloodhound-workspace:20261014-numbered-extensions`. The isolated
build snapshot was made from base commit `27bf6a0ed`. Its source hash list is
[build-source-files.json](build-source-files.json). It records the files
copied into that frozen build; editor work in the current checkout was not
included in this build or deployment.

## Checks

- Focused UI checks passed: **33 tests across 4 files**. See the ignored
  [focused test log](../../work/workspace-numbered-extensions/focused-tests-final.log).
- The final type-check log contains no diagnostics. An earlier type-check log
  records a `setSettings` error from before it was corrected to `setSetting`;
  the final UI build and browser checks used the corrected source. The final
  log is [types-final.log](../../work/workspace-numbered-extensions/types-final.log).
- Prettier and the style check passed. See [format-test.log](../../work/workspace-numbered-extensions/format-test.log)
  and [style-final.log](../../work/workspace-numbered-extensions/style-final.log).
- The production UI build completed successfully. See
  [ui-build.log](../../work/workspace-numbered-extensions/ui-build.log).
- Synthetic Chromium checks passed in light and dark themes, including visible
  labels, zoom scaling, matching filter and node numbers, stable icon URLs
  after refresh, and switching back to light mode. Details are in
  [browser-results.json](browser-results.json). Source screenshots remain in
  the ignored work folder: [light theme](../../work/workspace-numbered-extensions/light-zoom-1.png),
  [dark theme](../../work/workspace-numbered-extensions/dark-zoom-1.png), and
  [dark theme at two zoom levels](../../work/workspace-numbered-extensions/dark-zoom-0.5.png),
  [dark theme at the other zoom level](../../work/workspace-numbered-extensions/dark-zoom-2.png).
- Live Chromium checks against the deployed UI passed with no browser errors.
  They confirmed numbered extension choices, matching node numbers, text
  contrast through theme changes, and zoom behavior. The browser reported 29
  queries. It did not request an analysis or change rules. See
  [live-browser-results.json](live-browser-results.json) and the retained
  [live screenshots](../../work/workspace-numbered-extensions/live-theme-0.png),
  [1](../../work/workspace-numbered-extensions/live-theme-1.png),
  [2](../../work/workspace-numbered-extensions/live-theme-2.png), and
  [zoom view](../../work/workspace-numbered-extensions/workspace-live-zoom.png).
- Deployment replaced only the BloodHound application container. The rule
  service and PostgreSQL and Neo4j containers stayed in place, and active and
  draft rule-file checksums were preserved. See
  [deployment-state.json](deployment-state.json).
- The full `just test` command failed in `bh-shared-ui` after 5 minutes 53
  seconds. Its command log contains no individual failing test details, and Go
  tests were not reached. This was not a passing full-suite run; see
  [just-test.log](../../work/workspace-numbered-extensions/just-test.log).

The separate live API snapshot reports that the existing manual lab's latest
analysis had failed during answer checking, with saving not started. This is
not evidence of a failure in the display change, and the browser check did not
request that analysis. No cause is inferred from the sanitized report. See
[live-api-results.json](live-api-results.json).

## Deployment and rollback

The previous application image was
`esc1-poc-bloodhound-workspace:20261014-edge-labels`. To restore it while
keeping the same lab data and other containers:

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-numbered-extensions/compose-before.yaml \
  up -d --no-deps --no-build --pull never bloodhound
```

This command was not run. The active Compose file still names the newer image,
so restore the earlier image tag there too if the rollback should survive a
later restart.

## Review

Review the five UI source files and the updated user-facing/design notes listed
in [changed-files.txt](changed-files.txt). The hashes in
[build-source-files.json](build-source-files.json) identify the isolated
compiled snapshot; they do not describe later editor work in the current
checkout. Build logs, source scripts, screenshots, and the isolated checkout
remain under the ignored `tools/esc1-poc/work/workspace-numbered-extensions/`
directory.

Nothing was staged or committed. After reviewing the working tree, stage only
the intended files and commit manually. The full repository preparation remains
required before a commit; this follow-up does not claim that it passed.
