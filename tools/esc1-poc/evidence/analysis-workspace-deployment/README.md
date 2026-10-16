# Local analysis workspace deployment — 14 October 2026

The user requested a running deployment for manual testing. Open
[OpenGraph Analysis](http://127.0.0.1:28080/ui/administration/opengraph-analysis).
The local lab login is `admin` / `experiment-only`. The
[rules page](http://127.0.0.1:18081/) manages uploads and Run Analysis.
The previously suggested `bloodhound.localhost` address was not serving this lab.

The Compose project is `esc1-workspace-manual`. Its files, private configuration,
database copies, rules, build checkout, logs, and browser screenshot are under
`tools/esc1-poc/work/workspace-deployment/`. All published ports bind to
127.0.0.1. BloodHound uses 28080, rules use 18081, and Neo4j uses 37474/37687.
All four services were running at verification; both database health checks passed.

## What was deployed

The UI and both programs were built from the current uncommitted workspace.
The native program was built in a separate checkout with the freshly built UI;
the existing generated assets in the main checkout were not replaced. Both
server programs are static Linux executables. New local Docker image names
preserve the older images:

- `esc1-poc-bloodhound-workspace:20261014`
- `esc1-poc-rules-workspace:20261014`

[Source state](source-state.json) records the base BloodHound and DAWGS commits,
old container image identifiers, and original rule-file hashes.
[Build source files](build-source-files.json) records the current files copied
over that base before compilation. Documentation added after the build is not
part of that source snapshot. [Deployment state](deployment-state.json) records
the new images, executable hashes, container state, and storage locations.

The original persistent Neo4j lab and its application database were stopped.
Their four storage directories were copied with the original directories
mounted read-only. Application updates therefore apply to the copies. The
old running rule container was stopped and retained; its saved files still
match their pre-deployment hashes.

The old rule store predates explicit query namespaces. Its files were not
converted or overwritten. Instead, the new store contains the current shipped
ESC1 and cross-platform examples: 16 queries across `PoC`, `PoC_AD`, and
`PoC_CyberArk`. Their three schemas were installed through the application's
normal upload endpoint. The queries were saved as a draft, reviewed, and then
activated through the rule service. The copied application database already
had OpenGraph extension management enabled. Existing collected graph data
comes from the copied lab; no new collected dataset was uploaded.

## Actual checks

- Production UI build and both program builds succeeded using cached dependencies.
- Authenticated live workspace returned HTTP 200, 16 queries, and 16 dependency links.
- Chromium used the real login form and real HTTP responses. It verified the
  visible graph canvas, all 16 table entries, selected details, dependency
  navigation, search/clear, refresh, and an original query download matching
  the shipped file byte-for-byte. No browser page errors occurred; workspace
  reads and download returned HTTP 200.
- Original rule files remained unchanged. Both database containers were healthy.

See [API results](live-api-results.json) and
[browser results](live-browser-results.json). The screenshot and full build/check
logs remain in the ignored deployment directory.

No extension analysis was requested after activating the examples. The startup
report was received while the new rule store was empty. A fresh completed-run
report requires **Run Analysis** on the rules page, then **Refresh** in the
workspace. This deployment check does not establish the correctness of a fresh
analysis over the copied graph. Earlier implementation checks and full-suite
limitations remain documented in
[analysis-workspace/README.md](../analysis-workspace/README.md).

Initial deployment-helper attempts found differences from the older lab script:
schema uploads use `PUT`, successful uploads may return 200 or 201, and an empty
rule store returns 404. The private helper was corrected and the final checks
passed. The older `scripts/check-persistent-service.py` still uses `POST` for
schema upload and should be updated before that older demonstration is reused.

## Restart and rollback

From the repository root, restart this deployment with:

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-deployment/compose.yaml up -d --no-build --pull never
```

To restore the previous lab, stop the new containers without deleting their
files, then start the retained originals:

```bash
docker compose --env-file tools/esc1-poc/work/workspace-deployment/deployment.env \
  -f tools/esc1-poc/work/workspace-deployment/compose.yaml stop
docker start esc1-poc-persistent-app-db-1 esc1-poc-persistent-graph-db-1
```

Wait until both original databases show `healthy` in `docker ps`, then:

```bash
docker start bloodhound-rules-rule-service-1 esc1-poc-persistent-bloodhound-1
```

Nothing was staged or committed. Keep the private environment file and all
database directories out of source control. After manual review, use the
implementation evidence's suggested commit commands and resolve its remaining
repository-wide checks first.
