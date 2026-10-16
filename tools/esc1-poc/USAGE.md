# Use the persistent rule service

This walkthrough starts a fresh lab where BloodHound runs uploaded rules after its own analysis. The extra rule container stores your files. The modified BloodHound application reads them, calculates the answers, and saves the resulting connections. Installing that application is a one-time change; later rule uploads need no application restart.

People can still search and view BloodHound while the calculations run. Other changes to graph data, including deletions and tag changes, wait until both native analysis and the rules finish. Searches can be slower when the database is busy. This coordination covers one modified Community Edition application, not other applications or direct database writers.

Read [DESIGN.md](DESIGN.md) for how this works and why BloodHound needed changes. [REVIEW.md](REVIEW.md) lists the files, checks, and recorded limitations. The earlier standalone command, which stops BloodHound during calculation, has its separate [walkthrough in HANDOFF.md](HANDOFF.md#earlier-standalone-walkthrough-and-evidence).

## Prepare a fresh lab

Run all commands below from `tools/esc1-poc`. Choose **one backend at a time**: Neo4j and PostgreSQL both use application port `28080` and rule-page port `18081`.

For either backend, the BloodHound lab account is `admin` with password `experiment-only`. These fixed credentials are for this dedicated lab.

Use fresh, dedicated lab data. The development application reports version `999.999.999` and may apply database migrations. Do not attach it to preserved control data or an existing installation. These overlays keep storage under `work/persistent-neo` or `work/persistent-pg`, separate from the original lab at port `18080`. Reusing those directories resumes their existing data and rules; it does not make a fresh lab.

Before building, have these requirements available:

- Linux, Python 3, Docker, and Docker Compose with support for `!override` in Compose files.
- An installed Go toolchain suitable for the modules' Go `1.27.1` requirement. The script defaults to `/snap/go/current/bin/go`; set `POC_GO` to another installed executable if needed.
- This BloodHound checkout, the rule code in this directory, and the edited DAWGS checkout at `tools/DAWGS`, with their dependencies already in the Go module cache. `POC_MODULE_CACHE` can select an existing cache. The build script disables downloads.
- Built BloodHound UI files, including `index.html` and `assets/`, in `work/ui-assets` or the directory named by `UI_ASSETS_DIR`. An existing complete generated UI in the application source also satisfies the script. It adds missing assets and refuses to replace conflicting existing files.
- The local base image `esc1-poc-bloodhound:2cb6dbd37738ffe31e34e1729b3638f3887a93e9` and the pinned database images named in [compose.yaml](compose.yaml) or [compose.pg.yaml](compose.pg.yaml). The build and startup commands below do not download them. [HANDOFF.md's environment setup](HANDOFF.md#build-and-start-the-dedicated-environment) records reference-image preparation; the older `build-image.sh` refuses tracked native changes in this checkout.

[scripts/build-extensions.sh](scripts/build-extensions.sh) builds two separate images: the modified BloodHound application and the rule service. It also creates private `rules.env` once, with a random access token and your user/group IDs. Existing settings are preserved. Keep that file private; do not commit it or put its token in logs.

```sh
./scripts/build-extensions.sh
```

The following commands use the script's default image tags. If you override those tags for the build, use the same values in `rules.env` for startup.

## Start Neo4j

Start the databases first, then the rule service, then BloodHound. The database startup creates the network that the rule service joins.

```sh
mkdir -p ./work/persistent-neo/rules
chmod 700 ./work/persistent-neo/rules
docker compose -p esc1-poc-persistent --env-file rules.env \
  -f compose.yaml -f compose.extensions.yaml -f compose.persistent-lab.yaml \
  up -d --no-build app-db graph-db
BH_DOCKER_NETWORK=esc1-poc-persistent_default \
  RULE_STATE_DIR=./work/persistent-neo/rules docker compose \
  --env-file rules.env -f compose.rules.yaml up -d
docker compose -p esc1-poc-persistent --env-file rules.env \
  -f compose.yaml -f compose.extensions.yaml -f compose.persistent-lab.yaml \
  up -d --no-build bloodhound
```

Neo4j's host ports are `37687` for database connections and `37474` for its browser. BloodHound is at `http://127.0.0.1:28080/`.

## Start PostgreSQL instead

Use this sequence instead of the Neo4j sequence. It uses PostgreSQL for the graph and application records, with dedicated storage under `work/persistent-pg/storage` and host port `35432`.

```sh
mkdir -p ./work/persistent-pg/rules
chmod 700 ./work/persistent-pg/rules
docker compose -p esc1-poc-persistent --env-file rules.env \
  -f compose.pg.yaml -f compose.extensions.yaml -f compose.persistent-pg-lab.yaml \
  up -d --no-build pg-db
BH_DOCKER_NETWORK=esc1-poc-persistent_default \
  RULE_STATE_DIR=./work/persistent-pg/rules docker compose \
  --env-file rules.env -f compose.rules.yaml up -d
docker compose -p esc1-poc-persistent --env-file rules.env \
  -f compose.pg.yaml -f compose.extensions.yaml -f compose.persistent-pg-lab.yaml \
  up -d --no-build bloodhound
```

BloodHound is again at `http://127.0.0.1:28080/`.

## Upload rules, then upload collection data

1. Open `http://127.0.0.1:18081/`. This is the rule page, separate from BloodHound.
2. The supplied Compose setup enables local editing at this exact address, so the page needs no pasted token. Keep `rules.env` private: BloodHound and programmatic clients still use its secret.
3. Upload [extension/schema.json](extension/schema.json) in BloodHound’s **OpenGraph Management** page. On the rules page select **every `.cypher` file** in [extension](extension), including the membership and trust prerequisites. Do not select the schema or `QUERY_NOTES.md` here. For a smaller first extension, follow [DEVELOPERS.md](DEVELOPERS.md#quick-start).
4. Click **Save upload and review**, inspect the proposed plan, then click **Activate reviewed revision**. Choose explicitly whether to overwrite each existing file whose contents differ, including its declared dependencies. An accepted change shows its active revision; **uploading rules alone does not execute them**.
5. Open BloodHound at `http://127.0.0.1:28080/` and upload collection data through its normal upload process. It accepts the supported directory and OpenGraph JSON collections and ZIP archives.
6. After BloodHound imports the data and successfully finishes its native checks, it runs the affected active rules. Return to the rule page and click **Refresh rules and status** to inspect the execution report. Explore the resulting connections in BloodHound's normal browser.

Later collection uploads repeat this process using the rules active when that calculation starts. The service chooses queries by changed object and connection types, then runs their needed shared calculations once. It checks complete answers for selected outputs; skipped helpers and connections keep their saved state. Native checks still run normally. First runs, changed rules, restarts, and incomplete or over-budget change tracking run all enabled rules. A large upload can therefore require a full run of enabled rules. A rule change made during a calculation is kept for later work; it does not change the instructions halfway through the current calculation.

The ESC1 example creates `PoC_ADCSESC1` connections. BloodHound's own ESC1 connections are named `ADCSESC1`; the example checks its calculated answers against those native answers. The [vault example](examples/vault/README.md) connects a vault containing an account to the directory user whose `objectid` matches the account’s `user_sid`, through `PoC_Vault_HasAccountFor`. This association does not establish an attack path and is not traversable. Upload `schema.json` through OpenGraph Management, then `vault_helpers.cypher` and `account_users.cypher` through the rules page. Its rule files are under [examples/vault/extension](examples/vault/extension). To add it beside ESC1, use the same two pages; no file renaming is needed. Its namespace keeps its analyses separate. The maintained combined demonstration also uploads these schemas separately.

The [synthetic Active Directory and CyberArk example](examples/cross-platform/README.md) supplies two separately named schemas and three query files. It shows a CyberArk rule consuming rows calculated by an Active Directory rule through an explicit prerequisite. It is not an official CyberArk collector or schema.

### Run analysis without another collection upload

Click **Run Analysis** beside Refresh. This requests BloodHound's normal full analysis of stored data. After it succeeds, BloodHound checks the enabled extension rules using the same affected-rule selection as automatic runs. A changed rule set runs all enabled rules; clicking the button does not otherwise force every extension query to run. Disabled rules and their existing results stay untouched.

The button shows when its request is being sent, queued, or running, and blocks repeated clicks during those states. **A queued request is not a successful result.** Read the separate extension report and check its time and active revision. The rules used are the saved rules when extension processing starts. This does not download fresh collector data or restart BloodHound. People can continue searching; other graph changes may wait while analysis finishes.

Reading installed schemas and the Run Analysis button need three settings in private `rules.env`: `BLOODHOUND_API_URL`, `BLOODHOUND_API_TOKEN_ID`, and `BLOODHOUND_API_TOKEN_KEY`. Use BloodHound's address inside the container network, for example `http://bloodhound:8080`, and an API token created through BloodHound's normal account controls. The token’s account needs OpenGraph read permission (`OpenGraphRead`) for schema reads. Run Analysis additionally needs `AppWriteApplicationConfiguration` and `GraphDBRead`. OpenGraph extension management must be enabled. API tokens must be enabled in BloodHound. Keep the key out of source control, logs, and screenshots. Recreate only the rule-service container using the same Compose settings used to start it after changing these values.

Without this connection, new uploads can be saved as inactive drafts but cannot be reviewed or activated against installed schemas. The browser never receives the BloodHound key and needs no extra pasted token in the supplied local setup. The rule server uses the key for installed definitions and analysis request/status calls. The native worker reads installed schemas directly inside BloodHound before each attempt. Rebuild BloodHound with the definition-preservation migration and read endpoint before using this workflow.

## Inspect the analysis workspace

Open **Administration → OpenGraph Analysis** in the rebuilt BloodHound interface. It requires OpenGraph read permission, the OpenGraph extension-management feature flag, and the existing post-processing connection to the updated rule service. The native page uses your BloodHound login; do not paste the service token into it.

1. Click a query node or its table row. Green means enabled, gray means disabled, red means the query's step failed in the latest completed run of the current rules, orange means blocked, and blue marks the focused query; the top panel shows this legend with matching colored dots. Node labels show the namespace and analysis version. Prerequisites and consumers keep their availability colors; everything outside the focused dependency path fades to gray. Click an arrow to follow paths through that connection and inspect its explanation. Other branches and shortcuts that bypass it remain faded. The table identifies prerequisites and consumers in words.
2. Search by query/extension name or filter by extension. **Reveal hidden queries** clears filters when they hide a dependency. Pan with the mouse, zoom with the mouse wheel, or click **Reset Graph** (the crop icon, as in Explore) to fit the whole graph. Clicking empty graph space restores all colors. Keyboard users can select table buttons and dependency explanations with Enter; focus the graph controls and use arrows to pan, `+`/`-` to zoom, `0` to fit, and Escape to clear focus.
3. The entity panel's header shows the file's status, its extension, and one row of actions (**Edit/Test analysis file**, **Download query**, **Disable** for an enabled file or **Enable** for a disabled one, and **Delete**). Three tabs follow. **Overview** shows only what the file itself cannot tell you: order, dependency depth, installed schema, when it was added, what it **Needs** and what it is **Used by**, and what it **Produces**. **Cypher** shows the file exactly as written; its comment lines at the top hold the namespace, version, declarations, and optional instructions, which are not repeated elsewhere. **Last run** shows when the step last ran and how it ended, when it last succeeded in a run that saved its results, and the latest matching outcome. For the step that failed, it also shows why it failed. Click a row in **Analysis report** to open that step's panel on **Last run**; the report also lists each step's last-ran and last-success times. **Download query** returns that displayed file. If rules or schemas changed, click **Refresh** first.
4. Click **Latest status report** at the top left to open counts, times, failures, and saving information in the bottom panel. **Rules** switches back to the rule table. Outcomes appear on current queries only when both rule and schema revisions match. A missing or older report is explained separately. An overall running report does not provide live per-query progress.

**Dependency depth** counts arrows along the longest prerequisite chain leading to a query. Queries with no prerequisite queries have depth 0; a query immediately depending on one has depth 1. This describes dependencies, not simultaneous execution. The older rules page still calls this **Stage**, counting from 1.

The last confirmed saved revision is separate from successful calculation. An uncertain save does not prove which results are stored. **Refresh** reads the latest received report; opening and navigating the workspace writes no collected graph data.

The graph fills the page. Drag the handle above the bottom panel up or down to change its height; keyboard users can focus the handle and use Up/Down. Selecting a node or row opens its entity panel on the right, and the bottom panel narrows to make room. On a narrow screen, close the entity panel to return to the table. The top-left **OpenGraph Analysis** panel is collapsible. Above its **Analysis:** status line it shows the current rule revision, the schema revision, and the color legend.

One row of icon buttons sits in the bottom-left corner of the graph, just above the bottom panel when it is open: **Reset Graph** (crop icon), **Upload** (arrow icon), **Create analysis file** (plus icon), and **Rules table** (table icon). They are the same buttons Explore uses; hover over one to see its name. A dot on the Upload button means an inactive draft is saved.

Click **Upload** to save `.cypher` files as an inactive draft. You can select several `.cypher` files, one or more `.zip` files containing them, or both. Only `.cypher` files inside a `.zip` are read; folders inside it are ignored, so `rules/users.cypher` uploads as `users.cypher`. Other files, such as a README, are skipped. The 32-file, 1 MiB-per-file, and 4 MiB-total limits count the files inside each `.zip`. Password-protected `.zip` files are rejected. Choose **Overwrite this file** or **Keep saved file** separately for each changed existing file. Invalid drafts remain available for download, correction, and discard. Install schemas through **OpenGraph Management**. Once a draft is ready, review it and click **Activate reviewed revision**. Review notices show every deleted, disabled, blocked, and affected relationship type; the graph and table show the proposed rules until you activate or discard the preview.

For one rule, use **Enable**, **Disable**, or **Delete** in its entity panel. For several, use table checkboxes and the corresponding **selected** action. Clicking a node, edge, or row only changes focus; it never checks more rules for changes. Select-all applies to visible rows. Filtered selections remain selected and their hidden count is shown. Clear selections before changing your intended targets. A new active rule or schema revision clears selections and pending reviews. Every action is reviewed before activation, including cascaded deletions. Activation saves rules; it does not run analysis. Relationship cleanup waits for the next successful analysis.

**Run Analysis** uses BloodHound's existing action as your signed-in account, requiring application-configuration write permission. Queue/status reads require graph read permission. It uses whichever active rules are current when processing starts. A failed or lost reply is not retried automatically; use **Refresh rules and status** before trying again. The top panel's status line follows the whole run: requested, queued, BloodHound's own phase (for example purging deleted data or analyzing collected data), running extension rules, and then the result. A spinner shows while anything is in progress. While BloodHound is busy, the line says what it is doing and for how long, for example "BloodHound has been ingesting uploaded data for 24 minutes. An analysis is queued after it. BloodHound requested it at startup (…)." BloodHound processes one thing at a time, so analysis, extension rules, and editor tests wait behind an upload or purge. After five minutes the line turns orange, says the wait is unusually long, gives the time of the last finished analysis, and links to **File Ingest** to check the upload. A queued analysis explains that BloodHound starts it on its next processing check, usually within a minute, once no upload or purge is in progress. The page refreshes the report, graph colors, and table by itself, every 4 seconds while a run is in progress and every 10 seconds otherwise, so automatic retries and changes made elsewhere also appear. An open analysis file editor keeps what you typed during these refreshes. A failed run names the failed step as a link and gives the reason. Upload and rule changes require both OpenGraph read and write permission. An uncertain activation or stale review also requires refresh and another review.

The older local rules page remains available at `http://127.0.0.1:18081/` for existing workflows. Rebuilding the UI, API, and rule service is required; source edits do not update running containers automatically.

## Read execution reports

Expand **Latest analysis report** after a run. It shows the database, the revision that ran, the last confirmed saved revision, and separate calculation and save outcomes. Each query has its dependency stage, reason for running or being skipped, answer count, read time, and proposed/saved output counts. A failure identifies its query and stage when it occurred inside a query. A failure while saving the combined result belongs to the save step instead.

Counts from an unfinished or failed run may be incomplete. Proposed counts describe checked answers and attempted changes; only a confirmed save produces saved counts. A shared connection can be proposed by several queries, so do not sum their output counts to estimate distinct saved connections. Intermediate answers, object IDs, query literals, and raw database errors are excluded from these stored reports. Known failure categories have useful explanations; an unrecognized database failure points to private application logs.

The service retains the latest **20 attempts**, including retries, across restarts. History shows each attempt's final received state and can be expanded for comparison. A lost report is retried without rerunning a successful calculation. If a running report has no completion after the two-hour run limit plus one minute, it becomes **unavailable**, with an unknown save outcome. A report that cannot be loaded and a database save that was not confirmed are visibly distinct from success and confirmed failure. These are last-known reports, not a live health check; an outage can still hide a newer attempt.

## Change the active rules

Use **Save upload and review** to save an inactive draft. A draft can be incomplete: install the missing schema in OpenGraph Management or add the missing query on the rules page, in either order. Its files survive refresh and service restart. The page explains what is missing and keeps **Review saved draft** unavailable until the complete proposal validates. Active rules and their saved graph results keep working while the draft is incomplete.

Review checks the complete query language, visible input declarations, and output columns without executing a query. Fix the filename/line errors shown for an incomplete draft.

Once ready, review the proposed files and use **Activate reviewed revision**. Saving a draft never activates it. **Discard preview** closes the review but keeps the saved draft; **Discard saved draft** removes only the draft. Draft files can be downloaded individually. Changed existing files still need explicit overwrite choices. There is one shared draft per rule service, subject to the same file and size limits as the active set.

If active rules change after a draft is saved, download the draft files, discard the old draft, and upload them again against the current active revision. This requires fresh overwrite choices and review instead of silently replacing newer work. Mutual schema references are allowed; a circular requirement for newly calculated query results still prevents activation.

The page uses one table for all uploaded extensions. Click a column heading to sort by **Order**, **Name**, **Stage**, **Date added**, or **Extension**, or use the extension filter to narrow the list. Expand a rule to read its full query, prerequisites, dependents, inputs, and outputs. The state column explains whether it is enabled, explicitly disabled, or paused because another rule is disabled. The analysis indicator and latest report describe BloodHound's last reported attempt.

Stage 1 rules have no earlier query to wait for. Later stages describe the longest chain of earlier answers a rule needs. They are not waiting groups: a rule can start as soon as its own required answers are ready and one of the five places is free. It does not wait for unrelated rules in an earlier stage. Once the runner creates supporting graph objects, the remaining queries run one at a time so they can see those objects before the complete result is saved.

**Date added** records the first activation of a query file. Overwriting it, disabling it, enabling it, and restarting the rule service preserve that date. Rules saved by an older version without a recorded date show **Not recorded**. New query files in a preview show **On activation**. Removing a file and later adding it again creates a new activation date.

Each extension keeps its installed name, display name, and namespace. Query filenames need only be unique within their declared namespace; the service stores `namespace/filename.cypher` and downloads the original filename. The rules service reads installed schemas using its server-held BloodHound credentials and OpenGraph read permission. Authors do not supply reference settings. Relationship results combine across producing rules without ownership settings.

Later uploads can contain only changed or new files. The service combines them with the current files and validates the complete plan. For each existing file with different contents, choose **Overwrite this file** or **Skip this file**. An overwrite replaces the complete file, including its declared dependencies. Review the proposed plan and listed changes, then activate that exact revision. An older tab cannot overwrite newer work: refresh and review again if the active revision changed.

Use a rule's trash button to delete it, or select rows and choose **Delete Selected**. The confirmation lists deleted files, including rules that explicitly require a selected rule’s returned rows and supporting queries no surviving producer needs. It separately shows relationship types to refresh. Other producers of the same type and rules reading that type from the graph remain. Dependencies can cross extension boundaries. Confirming deletes those files while preserving the schema's kind names and display information. After the next successful rule run following BloodHound analysis, remaining producers rebuild the combined connections and their graph consumers recalculate. A connection remains while any surviving producer returns it. Deleting the last producer clears the type’s recorded endpoint scope during that run. A failed run preserves the previous committed results. Supporting helper objects are retained; they are not automatically deleted.

Use **Disable** on a rule, or select rows and choose **Disable Selected**, to pause calculation without deleting anything. Review and confirm the selected rules and the dependents that will pause. Their files, output declarations, dates, and existing graph results stay saved. Other producers of the same relationship type pause too, followed by consumers needing their query rows, calculated graph kinds, or helper endpoint kinds. This keeps the whole shared output unchanged while disabled. Independent rules continue running. Preserved results can become out of date while the rules are disabled.

Use **Enable** or **Enable Selected** to remove the selected rules' explicit disabled setting. Eligible dependents resume too; a rule remains paused if another prerequisite is still disabled, or if the rule itself was explicitly disabled. These settings apply to the next analysis. The selection checkbox in the table heading applies only to rows visible under the current extension filter. Sorting and filtering never change calculation order.

Each relationship query declares its output with a literal `kind` in its final `RETURN`; helper queries use a literal one-element `kinds` list. Schemas describe types and endpoints and need no `post_processing` blocks. The registry builds one plan containing every producer of each relationship type. See [the developer guide](DEVELOPERS.md#what-queries-return) for the row contract and migration from older bundles.

To pass a fresh calculation between schemas, declare the earlier file as `query:PoC_AD/ad_users` and consume its returned rows through `$ad_users`. The filename supplies the default parameter name; `as directory_users` changes it to `$directory_users`. Optional requirements-style conditions limit the provider version; omission follows the latest activated local version. To read freshly calculated edges from the graph instead, declare that relationship type under `depends-on`. The runner waits for all its producers, stages their combined answer, then runs the reader in the same transaction. An explicit query prerequisite supplies the returned rows directly. The [cross-platform example](examples/cross-platform/README.md) demonstrates this distinction.

Upload review checks filenames, schema declarations, prerequisites, output columns, read-only restrictions, and complete query syntax with the same language reader used during execution. Visible graph kinds and answer parameters require matching declarations. Unnamed or dynamic graph reads are treated conservatively, because their full input requirements cannot be established from names alone. Limits are 32 files, 1 MiB per file, and 4 MiB for both an upload and the complete saved bundle. Queries must ask read-only questions; shared code saves the validated results. Database translation, execution, and returned-value checks still happen during a run, so a validated upload can still fail there. Review is a preview of files and the plan, not of calculated graph changes.

Uploading, overwriting, deleting, disabling, or enabling rules alone makes no graph changes. Click **Run Analysis** if its connection is configured, or upload a subsequent collection to BloodHound, and wait for successful native analysis to use the updated plan. A failed calculation before publication preserves previous saved results, which may now be out of date. Helpers absent from a later answer are not automatically deleted; see [DESIGN.md](DESIGN.md) for shared-result and publication details.

## Local editing and the BloodHound page

`compose.rules.yaml` publishes only `127.0.0.1:18081` and passes `-local-ui-origin http://127.0.0.1:18081`. The service checks the exact Host, any supplied Origin, and browser cross-site headers; writes require the configured Origin and JSON. It supplies the secret internally only for allowed browser routes. Machine routes remain protected by the original bearer secret. Without an explicit local origin, the standalone page falls back to manual bearer entry.

This local convenience trusts the loopback host-port boundary. Host and Origin do not authenticate arbitrary clients on the container network. The [native analysis workspace](DESIGN.md#a-native-bloodhound-page) uses BloodHound's login and server connection for inspection. The native workspace also provides upload, review, activation, individual/bulk rule changes, and the normal Run Analysis action using your signed-in account.

## Read the results and status

The rule page shows the latest execution report received, including its revision, event, and reporting time. A success message gives the number of queries executed out of the total, or says that no rules depend on the changed data. Compare its revision with the active revision before assuming an updated rule has run. A successful collection import does not prove that the extension succeeded: extension failures have their own report and appear in BloodHound's application log.

The status page is not a heartbeat. If the containers cannot communicate, its last report can be stale even while BloodHound continues running. A failed attempt is retried once, on an idle processing tick at least a minute later, and only if no application graph-write attempt has started since its successful native analysis. If the retry also fails, retries stop until the next successful analysis. If new data arrived first, the attempt is shown as **waiting** rather than failed. Even an attempted write that later fails prevents that retry. Otherwise it waits for another successful analysis after a later collection upload. Retries keep the original files: uploading a corrected rule requires a new successful native analysis before the replacement can run.

Successful rules save their calculated connections, helper changes, and successful-run record together. Tracked changes remain pending after a failure, so a later successful analysis includes those changes as well as newer ones. They are cleared only after confirmed publication or a successful skip. Failures before that save leave the last committed output available. Schema uploads happen separately through OpenGraph Management. Database metadata preparation can remain after a failed calculation; execution never installs a schema.

There is a separate uncertain-outcome case: the database may save the results but lose the reply confirming the save. This BloodHound build then refuses further graph changes until someone checks the saved state and restarts the application. Searches remain available. Do not assume an error in that case means nothing was saved.

## Repeat the recorded lab demonstration

These commands actively upload synthetic collections and change the lab's rules. Run them only against a fresh dedicated persistent lab started above. Python 3 generates the collections and checks the workflow through the local APIs.

```sh
python3 scripts/make-native-cases.py --output work/persistent-collections
python3 scripts/check-persistent-service.py \
  --collections work/persistent-collections --output work/persistent-check
```

Use a new output directory for each attempt. Add `--opengraph` to the second command to upload directory data first, then one vault account, then two accounts matching the same user, and repeat the two-account upload. It checks the expected association and preserves its identity across these runs. The [standalone vault demonstration](examples/vault/README.md#command-based-demonstration-and-historical-evidence) covers both arrival orders and vault failure preservation; the persistent script’s failure checks exercise ESC1. That historical option leaves both models in one schema; it does not exercise separate-schema registration. Removing those extra rules is now supported through preview and activation. Their old relationship scope is cleared during a later successful analysis; helper objects remain. Start a fresh lab for a complete repeat of the recorded demonstration.

The saved checks include ESC1 results on both databases, rule replacement, rejected writing queries, previous-result preservation after failure, recovery, and sampled searches during calculations. Earlier vault results used a different rule and do not validate the current account association; check the recorded rule and run in the evidence before drawing conclusions. They are small lab demonstrations, not a load test or proof of Enterprise support or 100-million-edge capacity. [REVIEW.md](REVIEW.md) and [evidence/persistent-results.json](evidence/persistent-results.json) retain the results and failed attempts.

## View the declared model offline

Open [model.html](model.html) or [examples/vault/model.html](examples/vault/model.html) in a browser. These pages show the types and allowed connections declared by each schema, not the current collected graph. They work offline and offer search, pan/zoom, connection details, pathfinding filters, a complete combination table, and SVG download. Green connections permit pathfinding; red connections do not.

To regenerate a page after changing a schema, build the local command and run:

```sh
./scripts/go.sh build -o esc1-poc .
./esc1-poc model -extension extension > model.html
./esc1-poc model -extension examples/vault/extension > examples/vault/model.html
./esc1-poc model -extension examples/cross-platform/extension > work/cross-platform-model.html
```

These commands need the installed Go tooling and cached dependencies described above; viewing the saved HTML needs neither Go nor a running database. You can also select several local schema files together inside the page. For cross-extension kinds, include their defining schemas; the viewer reads the whole set before checking references. Native kinds need no extra file. The `model` command likewise reads every `schema.json` and named `.schema.json` file in the directory, without requiring queries or a database. Regenerating a page after a schema edit does not require rebuilding the container.

## Troubleshooting

| What you see | What to check or do |
| --- | --- |
| Build reports missing modules or Go tooling | The build does not download dependencies or a toolchain. Check `POC_GO`, the module cache, and the local DAWGS checkout. |
| Build reports missing UI assets or refuses an existing asset | Supply the built UI root with `UI_ASSETS_DIR`. Conflicting generated assets need review; the script deliberately refuses to overwrite them. |
| Build or startup reports a missing image | Prepare the exact local base and database images listed above. `--no-build` and `pull_policy: never` expect them to exist already. |
| Compose rejects `!override` | Use a Docker Compose version supporting that tag. It is needed to replace the original lab's ports. |
| Port already in use | Run one persistent backend lab at a time. Both use `28080` and `18081`; stop the other lab before starting this one. |
| Rule service cannot join its network | Start the selected lab database first. Use the shown `BH_DOCKER_NETWORK=esc1-poc-persistent_default` override. |
| Rule service cannot write its state | Check the selected `RULE_STATE_DIR`, its `0700` permissions, and the user/group IDs in `rules.env`. Neo4j and PostgreSQL use different rule-state directories. |
| Local rule page rejects a request | Use exactly `http://127.0.0.1:18081/`, matching Compose's `-local-ui-origin`. Writes require this Origin and JSON. Manual mode still needs the matching secret from `rules.env`. |
| Draft cannot activate | Read the missing-schema or query explanation. Add the defining schemas and required query files to the same saved draft, then review it again. |
| Upload rejects a schema update | Include every required query, preserve each existing schema’s filename and name, and explicitly choose overwritten files. Shared relationship producers must agree on the update policy. Removal and reassignment are allowed after complete-plan validation. Refresh and preview again if another tab changed the revision. |
| Upload succeeded but no new connections appeared | Upload a subsequent collection, wait for native analysis, then compare the execution report's revision and time. Uploading rules alone only stores files. |
| Collection succeeded but extension failed | Read the separate rule report and BloodHound log. Correct the rule, upload its replacement, and upload a subsequent collection so successful native analysis can run the new files. Idle retries keep the failed attempt's original files. Earlier saved connections may remain and be out of date. |
| PostgreSQL rejects a query that worked on Neo4j | The database translators differ. Upload checks do not establish runtime compatibility. Use the supported forms in the examples; see [DESIGN.md](DESIGN.md) for the narrow DAWGS changes. |
| A mixed directory/OpenGraph job reports an unexpected file count | A recorded demonstration hit this discrepancy even though both files arrived. Keep the failed report and use separate one-file uploads to diagnose it; do not treat an expected connection as proof that every upload check passed. |
| Status stops updating | Inspect both containers' availability, token settings, and network connection. The displayed report can be old; it is not a health signal. |
| Graph changes wait or analysis is deferred | This is the writer coordination. Already-running writes have up to 30 seconds to finish before analysis begins; calculations themselves are not limited to 30 seconds. Searches remain possible. |
| Graph writes are refused after an uncertain database save | Check the actual saved output before restarting BloodHound. A lost confirmation can follow a successful save. |
| Database switching or graph migration is refused | Disable automatic rules by leaving `bhe_post_processing_url` empty and restart first. These maintenance operations use separate writer connections. |

For the Neo4j lab's application log:

```sh
docker compose -p esc1-poc-persistent --env-file rules.env \
  -f compose.yaml -f compose.extensions.yaml -f compose.persistent-lab.yaml \
  logs --tail 100 bloodhound
```

For PostgreSQL, substitute `compose.pg.yaml` and `compose.persistent-pg-lab.yaml`. The rule-service log uses its own Compose file:

```sh
docker compose --env-file rules.env -f compose.rules.yaml logs --tail 100 rule-service
```

Integrating this with another installation requires an appropriately reviewed application image matching that installation and a one-time restart to enable `bhe_post_processing_url` and `bhe_post_processing_token`. The fresh-lab overlays above are not an upgrade procedure for existing data.

## Convert an older experimental rules directory

Keep the original `active.json`, `draft.json`, and `status.json` unchanged in their old state directory. Use a new empty rules directory for this format; startup rejects old files instead of guessing associations. Do not point a new binary at historical comparison data.

1. Export the old schema and `.cypher` files. For every query, explicitly choose its owning schema namespace and its own complete version, for example `v1.0.0`. Shared queries belong to their author, not their consumers.
2. Add the required leading `namespace` and `version` comments. Optionally declare `extension-versions`. Change local dependencies to `query:name`; cross-extension references need `query:Namespace/name`. Omit `.cypher`; place query requirements on separate lines. Update optional batch/overlap references to the consumer’s parameter alias.
3. Upload schemas through OpenGraph Management. Retain experimental `compatible_kinds`; the modified upload now preserves them. Do not use the old standalone display-only export for this step.
4. Upload the converted analyses to the fresh rules page as a draft. Resolve missing dependencies, review the complete plan, then activate. Review Disable choices for any analyses that were previously disabled before requesting a run. Existing graph results remain until a later successful analysis.
5. Handle an old draft separately after reviewing the active files. Do not overwrite the original saved state or carry an old review token into the new service.

A failed or interrupted conversion leaves the original directory available. No automatic schema installation, namespace inference, or deletion of the old store occurs.

Deleting or incompatibly updating a schema pauses affected analyses and preserves their downloads and previous graph results. Fix the schema, refresh, then review **Enable** for retained pauses. Compatible schema upgrades need no query edits. A newer uploaded query remains a draft until activated. See [version and shared-column rules](DEVELOPERS.md#file-association-and-compatible-versions).

## Graph styling and interpreting query details

Query nodes use Explore's normal node renderer, size, border, and camera reset. Dependency arrows use its normal thickness and gray. Arrows carry no labels on the graph, to keep it readable; click an arrow, or open a query's **Overview**, to see what it passes along (**As $name**, or the helper-object or calculated-relationship kind being waited for). The entity panel's left handle changes its width by dragging; keyboard users can focus it and use Left/Right. The bottom panel makes room as the width changes. The dependency-depth explanation is a tooltip next to the value.

Extensions are numbered in the filter dropdown, and each query node shows its extension's number inside the circle. Numbers follow namespace order across the complete plan; filtering, selecting, or refreshing an unchanged plan does not renumber them. Environment kinds no longer choose graph icons. Node and edge text colors update when switching between light and dark mode. These display choices add no author requirement and do not change calculations.

The broad-read notice means some graph reads do not name an object or connection type. The engine cannot narrow which changes affect them, so it repeats them after graph changes rather than keep outdated results. It is not a validation failure. An intermediate query returns rows for later queries; graph-output counts are not applicable. For a publishing query, proposed output rows describe its checked answer before saving. Confirmed output rows require successful saving and may include existing connections or overlap other producers; they are not counts of newly added connections. Missing counts are labeled unreported, unsaved, or unconfirmed rather than zero.

Deleting `special_groups.cypher` removes `membership.cypher`, then `esc1.cypher`, because they require its answer rows. The existing deletion cleanup then removes supporting queries that no longer feed any remaining graph output. In the current advanced lab, 12 ESC1 files are listed for removal; `domain_helpers.cypher` remains because it has its own helper output. The review now distinguishes requested deletions from additional files and explains this behavior. Disable keeps the files. The UI does not change the existing deletion policy.

Reachability is optional, not removed. The active lab contains the advanced example in `extension/`. `examples/esc1-cypher/` calculates the same result using ordinary Cypher without reachability, group-overlap, or batch-on directives. Removing a directive from an advanced seed query would remove part of its calculation; switching examples requires replacing the complete reviewed bundle, not deleting comments.

Every editor test recalculates the needed prerequisite chain against the current graph; saved answers are never reused, so a test cannot show stale results or refer to objects that no longer exist. The file being tested also runs fresh; its rows are read while temporary data exists, then displayed as a table after it is discarded. Run Analysis still calculates its inputs and publishes graph results normally.

To try the deployed editor, open <http://127.0.0.1:28080/ui/administration/opengraph-analysis> and click the plus button (**Create analysis file**) in the bottom-left corner of the graph. Pick the installed extension, name the file, and write its query in the **Cypher** box. If the text's `// namespace:` line names a different installed extension, for example after pasting a whole file, the **Installed extension** choice follows the text. Choose prerequisites from the **Prerequisites** dropdown, which lists every active query with a checkbox; type part of a name to filter the list. You can also type the declarations directly into the file. Choosing a query prerequisite adds a line such as `// depends-on: query:PoC/users as users`; change the name after `as` in the Cypher box if you want a different `$parameter`. A **Dependency graph** window appears. It uses the same left-to-right layout and controls as the main page, with this file in blue. Before the prerequisites are checked it shows the prerequisites you chose; after a check it shows every needed prerequisite, listed above it as **Needed prerequisites**. Active queries that already read this file's answer appear after it. The window is hidden while the file has no connections. Above the Cypher box, click the play button (**Test file**). Every test recalculates the prerequisites the file needs against the current graph. Play checks the prerequisites first whenever the text changed since the last check, then runs the test. **Review prerequisites** runs only that check, to see the needed chain without testing. If the check or the test finds a common problem, the message names it in plain words, for example the line that reads `Domain` objects and the exact `node:Domain` to add to `// depends-on`, or a `RETURN n` that returns a whole graph object instead of values such as `n.objectid`. Other problems get general guidance, because detailed checker messages can repeat parts of the file. The floppy-disk button (**Save private copy**) keeps unfinished work to resume later. The rewind button (**Revert**) returns to the file as it was opened, resumed, or last saved. **Test results** appear between the Cypher box and Dependencies as a table of returned rows, with empty values shown as `null`; when there are none, the dialog says so. Messages are red for errors, orange for warnings, blue for information, and green when something was saved. A check also warns in orange about declarations the Cypher never uses ("Declared but never used: node:Domain"); that is advice only. When a test fails because of specific objects, the message names them: for example "The name property is empty (null) for 5 helper objects: PoC_DomainHelper_S-1-5-21-…, …", the column and row that returned a whole graph object, or the source or target object ID of a connection that matches no object. The shared analysis report names the property, column, or connection type and the count, but never lists object IDs. Use **Review tested file** to see what activation would do. Its **Change** line says whether the file is new, replaces the active copy, or has the same text as the active copy. **Activate reviewed file** sits below those details and activates exactly that tested source. It appears only after a successful test and a successful review, which rechecks the file against every active rule. To make a change that would break other files, such as renaming a column a consumer reads, change those files together with **Upload** (one reviewed change), or disable the consumers first. A fresh query with no inputs can begin with the supplied `RETURN 1 AS result`; a graph-reading query needs the appropriate collected-kind declarations.
