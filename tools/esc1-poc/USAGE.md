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
3. For the ESC1 example, select [extension/schema.json](extension/schema.json) and **every `.cypher` file** in [extension](extension), including the membership and trust prerequisites. Select only the schema and query files, not `QUERY_NOTES.md`. For a smaller first extension, follow [DEVELOPERS.md](DEVELOPERS.md#quick-start).
4. Click **Review uploaded changes**, inspect the proposed plan, then click **Activate reviewed revision**. Choose explicitly whether to overwrite each existing file whose contents differ, including its declared dependencies. An accepted change shows its active revision; **uploading rules alone does not execute them**.
5. Open BloodHound at `http://127.0.0.1:28080/` and upload collection data through its normal upload process. It accepts the supported directory and OpenGraph JSON collections and ZIP archives.
6. After BloodHound imports the data and successfully finishes its native checks, it runs the affected active rules. Return to the rule page and click **Refresh rules and status** to inspect the execution report. Explore the resulting connections in BloodHound's normal browser.

Later collection uploads repeat this process using the rules active when that calculation starts. The service chooses queries by changed object and connection types, then runs their needed shared calculations once. It checks complete answers for selected outputs; skipped helpers and connections keep their saved state. Native checks still run normally. First runs, changed rules, restarts, and incomplete or over-budget change tracking run all enabled rules. A large upload can therefore require a full run of enabled rules. A rule change made during a calculation is kept for later work; it does not change the instructions halfway through the current calculation.

The ESC1 example creates `PoC_ADCSESC1` connections. BloodHound's own ESC1 connections are named `ADCSESC1`; the example checks its calculated answers against those native answers. The [vault example](examples/vault/README.md) connects a vault containing an account to the directory user whose `objectid` matches the account’s `user_sid`, through `PoC_Vault_HasAccountFor`. This association does not establish an attack path and is not traversable. Upload its three files: `schema.json`, `vault_helpers.cypher`, and `account_users.cypher`. Its rule files are under [examples/vault/extension](examples/vault/extension). To add this separate extension beside ESC1, save its `schema.json` locally as `vault.schema.json` before selecting it, so it does not overwrite ESC1’s `schema.json`. Its schema name remains unchanged. The combined demonstration below preserves an older single-definition setup.

The [synthetic Active Directory and CyberArk example](examples/cross-platform/README.md) supplies two separately named schemas and three query files. It shows a CyberArk rule consuming rows calculated by an Active Directory rule through an explicit prerequisite. It is not an official CyberArk collector or schema.

### Run analysis without another collection upload

Click **Run Analysis** beside Refresh. This requests BloodHound's normal full analysis of stored data. After it succeeds, BloodHound checks the enabled extension rules using the same affected-rule selection as automatic runs. A changed rule set runs all enabled rules; clicking the button does not otherwise force every extension query to run. Disabled rules and their existing results stay untouched.

The button shows when its request is being sent, queued, or running, and blocks repeated clicks during those states. **A queued request is not a successful result.** Read the separate extension report and check its time and active revision. The rules used are the saved rules when extension processing starts. This does not download fresh collector data or restart BloodHound. People can continue searching; other graph changes may wait while analysis finishes.

The button needs three settings in private `rules.env`: `BLOODHOUND_API_URL`, `BLOODHOUND_API_TOKEN_ID`, and `BLOODHOUND_API_TOKEN_KEY`. Use BloodHound's address inside the container network, for example `http://bloodhound:8080`, and an API token created through BloodHound's normal account controls. The token's account needs permission to run analysis (`AppWriteApplicationConfiguration`) and read the graph (`GraphDBRead`). API tokens must be enabled in BloodHound. Keep the key out of source control, logs, and screenshots. Recreate only the rule-service container using the same Compose settings used to start it after changing these values.

Leave all three empty to omit this connection; uploads and automatic analysis still work. The page then explains why Run Analysis is unavailable. The browser never receives the BloodHound key and needs no extra pasted token in the supplied local setup. The rule server uses the key only for the existing analysis request and status calls. No additional BloodHound code change is needed for this button. The earlier application changes are still required to run extension rules after native analysis.

## Change the active rules

The page uses one table for all uploaded extensions. Click a column heading to sort by **Order**, **Name**, **Stage**, **Date added**, or **Extension**, or use the extension filter to narrow the list. Expand a rule to read its full query, prerequisites, dependents, inputs, and outputs. The state column explains whether it is enabled, explicitly disabled, or paused because another rule is disabled. The analysis indicator and latest report describe BloodHound's last reported attempt.

Stage 1 rules have no earlier query to wait for. Later stages describe the longest chain of earlier answers a rule needs. They are not waiting groups: a rule can start as soon as its own required answers are ready and one of the five places is free. It does not wait for unrelated rules in an earlier stage. Once the runner creates supporting graph objects, the remaining queries run one at a time so they can see those objects before the complete result is saved.

**Date added** records the first activation of a query file. Overwriting it, disabling it, enabling it, and restarting the rule service preserve that date. Rules saved by an older version without a recorded date show **Not recorded**. New query files in a preview show **On activation**. Removing a file and later adding it again creates a new activation date.

Each extension keeps its own schema name, display name, and namespace. Authors do not supply reference settings. Relationship results combine across producing rules without ownership settings. Schema files may use `schema.json` or a name such as `active_directory.schema.json`. All schema and query filenames must be unique within the complete bundle; query names are global, even across different extensions. Choose distinct schema filenames when adding extensions. Keep an existing schema's filename and name stable through updates; these identify its declarations. The service registers each schema separately in BloodHound and creates one ordered query plan across all of them.

Later uploads can contain only changed or new files. The service combines them with the current files and validates the complete plan. For each existing file with different contents, choose **Overwrite this file** or **Skip this file**. An overwrite replaces the complete file, including its declared dependencies. Review the proposed plan and listed changes, then activate that exact revision. An older tab cannot overwrite newer work: refresh and review again if the active revision changed.

Use a rule's trash button to delete it, or select rows and choose **Delete Selected**. The confirmation lists deleted files, including rules that explicitly require a selected rule’s returned rows and supporting queries no surviving producer needs. It separately shows relationship types to refresh. Other producers of the same type and rules reading that type from the graph remain. Dependencies can cross extension boundaries. Confirming deletes those files while preserving the schema's kind names and display information. After the next successful rule run following BloodHound analysis, remaining producers rebuild the combined connections and their graph consumers recalculate. A connection remains while any surviving producer returns it. Deleting the last producer clears the type’s recorded endpoint scope during that run. A failed run preserves the previous committed results. Supporting helper objects are retained; they are not automatically deleted.

Use **Disable** on a rule, or select rows and choose **Disable Selected**, to pause calculation without deleting anything. Review and confirm the selected rules and the dependents that will pause. Their files, output declarations, dates, and existing graph results stay saved. Other producers of the same relationship type pause too, followed by consumers needing their query rows, calculated graph kinds, or helper endpoint kinds. This keeps the whole shared output unchanged while disabled. Independent rules continue running. Preserved results can become out of date while the rules are disabled.

Use **Enable** or **Enable Selected** to remove the selected rules' explicit disabled setting. Eligible dependents resume too; a rule remains paused if another prerequisite is still disabled, or if the rule itself was explicitly disabled. These settings apply to the next analysis. The selection checkbox in the table heading applies only to rows visible under the current extension filter. Sorting and filtering never change calculation order.

Each relationship query declares its output with a literal `kind` in its final `RETURN`; helper queries use a literal one-element `kinds` list. Schemas describe types and endpoints and need no `post_processing` blocks. The registry builds one plan containing every producer of each relationship type. See [the developer guide](DEVELOPERS.md#what-queries-return) for the row contract and migration from older bundles.

To pass a fresh calculation between schemas, declare the earlier file as `query:ad_users.cypher` and consume its returned rows through `$ad_users`. A query filename determines the parameter name. To read freshly calculated edges from the graph instead, declare that relationship type under `depends-on`. The runner waits for all its producers, stages their combined answer, then runs the reader in the same transaction. An explicit query prerequisite supplies the returned rows directly. The [cross-platform example](examples/cross-platform/README.md) demonstrates this distinction.

Upload validation checks filenames, schema declarations, declared prerequisites, literal output declarations, and forbidden writes. It does not establish that every graph dependency is declared or that all query syntax is supported. Limits are 32 files, 1 MiB per file, and 4 MiB for both an upload and the complete saved bundle. Queries must ask read-only questions; shared code saves the validated results. The full application query check, database execution, and returned-value checks happen later, so an accepted upload can still fail at runtime. Review is a preview of files and the plan, not of calculated graph changes.

Uploading, overwriting, deleting, disabling, or enabling rules alone makes no graph changes. Click **Run Analysis** if its connection is configured, or upload a subsequent collection to BloodHound, and wait for successful native analysis to use the updated plan. A failed calculation before publication preserves previous saved results, which may now be out of date. Helpers absent from a later answer are not automatically deleted; see [DESIGN.md](DESIGN.md) for shared-result and publication details.

## Local editing and an eventual BloodHound page

`compose.rules.yaml` publishes only `127.0.0.1:18081` and passes `-local-ui-origin http://127.0.0.1:18081`. The service checks the exact Host, any supplied Origin, and browser cross-site headers; writes require the configured Origin and JSON. It supplies the secret internally only for allowed browser routes. Machine routes remain protected by the original bearer secret. Without an explicit local origin, the standalone page falls back to manual bearer entry.

This local convenience trusts the loopback host-port boundary. Host and Origin do not authenticate arbitrary clients on the container network. A remote production page should use BloodHound's existing login and a server proxy. The [minimal native integration](DESIGN.md#a-native-bloodhound-page) is an explanation of future work, not an implemented navigation item.

## Read the results and status

The rule page shows the latest execution report received, including its revision, event, and reporting time. A success message gives the number of queries executed out of the total, or says that no rules depend on the changed data. Compare its revision with the active revision before assuming an updated rule has run. A successful collection import does not prove that the extension succeeded: extension failures have their own report and appear in BloodHound's application log.

The status page is not a heartbeat. If the containers cannot communicate, its last report can be stale even while BloodHound continues running. A failed attempt may retry on an idle processing tick only if no application graph-write attempt has started since its successful native analysis. Even an attempted write that later fails prevents that retry. Otherwise it waits for another successful analysis after a later collection upload. Retries keep the original files: uploading a corrected rule requires a new successful native analysis before the replacement can run.

Successful rules save their calculated connections, helper changes, and successful-run record together. Tracked changes remain pending after a failure, so a later successful analysis includes those changes as well as newer ones. They are cleared only after confirmed publication or a successful skip. Failures before that save leave the last committed output available. Schema registration and database metadata preparation happen separately and can remain after a failed calculation.

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
```

These commands need the installed Go tooling and cached dependencies described above; viewing the saved HTML needs neither Go nor a running database. You can also choose a local definition inside the page. Regenerating a page after a schema edit does not require rebuilding the container.

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
