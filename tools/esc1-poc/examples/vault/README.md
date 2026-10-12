# Vault accounts joined to directory users

This synthetic example adds a second extension beside ESC1. A collector supplies a vault and its accounts through OpenGraph. The calculation connects that vault to an existing AD user when a contained account’s `user_sid` matches the user’s `objectid`. The result is an owned `PoC_Vault_HasAccountFor` relationship from `VAULT-DEMO` to baseline Alice.

The relationship records that the vault contains an account for the user. It does not establish credential access or an attack path, so it is not traversable. The rule uses the collected account identifier and containment relationship; it makes no exposure judgment and does not filter on the directory user’s enabled state.

For the current setup that runs rules after BloodHound’s analysis, start with the [usage guide](../../USAGE.md). Its upload page accepts separate schemas together. To add vault beside ESC1, save the vault definition locally as `vault.schema.json` before selecting it, so it does not overwrite ESC1’s `schema.json`. All filenames are shared across the bundle; each extension retains its own schema name and owner. The command-based demonstration below stops the dedicated BloodHound application.

Upload all three files (using the distinct schema filename above when another `schema.json` is active): [extension/schema.json](extension/schema.json), [vault_helpers.cypher](extension/vault_helpers.cypher), and [account_users.cypher](extension/account_users.cypher). The schema name is `vault_poc`. The platform assigns its owner; `account_users.cypher` declares `'PoC_Vault_HasAccountFor' AS kind` and requests `keep-existing`. The schema contains only type descriptions and endpoint rules. This extension has its own written expectations; it does not compare results with native ESC1.

The account remains a custom `PoC_Vault_Account`, with source kind `PoC_Vault_Base`. Alice remains the native `User` loaded from [the existing AD baseline](../../testdata/baseline/users.json). Neither collected vault objects nor generated helpers receive native `Base` or `User` labels. Account IDs such as `VAULT-ACCOUNT-1` differ from directory SIDs: `user_sid` supplies the join without merging these separate identities.

Keeping `user_sid` on the collected account is what makes either arrival order work. If Alice has not arrived yet, the account still retains the information needed to connect her later. The next successful run checks again, without recollecting the vault. This does not change BloodHound’s own rules for resolving relationships supplied directly in an upload.

## Fixtures and expected results

Upload the existing `tools/esc1-poc/testdata/baseline/` AD files and the vault fixtures through BloodHound’s normal collection upload process. For the command-based experiment, use the [dedicated lab setup](../../HANDOFF.md#build-and-start-the-dedicated-environment). Run the command from `tools/esc1-poc/` with `--extension examples/vault/extension`; the `plan` command with the same flag shows the helper before the final query.

| Collection | Contained accounts | Expected vault-to-Alice pairs after Alice arrives | Purpose |
| --- | --- | --- | --- |
| [baseline.json](collections/baseline.json) | Account 1 | One | Join OpenGraph evidence to the existing AD user. |
| [two-supports.json](collections/two-supports.json) | Accounts 1 and 2 | One | Two accounts for the same user produce one distinct relationship. |

[expected.json](expected.json) contains exact source/target pairs outside the upload directory. Every account refers to the artificial SID `S-1-5-21-111111111-222222222-333333333-1001`; there are no real credentials or private directory data.

The demonstrations cover both arrival orders, multiple matching accounts, repeat runs, and preservation after failure. If vault data arrives first, there is no user connection until Alice arrives. With directory data first, the next calculation after the vault upload creates the association. Adding the second account keeps one relationship and updates the helper’s account count from one to two. A repeat keeps the existing relationship identity.

Omitting an account or containment edge from an upload is not evidence that it was deleted. Ingestion can merge data. This example does not model vault remediation or solve stale collected evidence. The shared engine still removes owned calculated relationships when the complete answer over actual stored facts no longer includes them.

## Input and publication contracts

Each collection has `graph.nodes`, `graph.edges`, and `metadata.source_kind`. Nodes use a stable string `id`, custom `kinds`, and properties such as text and numbers. Ingestion derives `objectid` from `id`; the collection never sets `properties.objectid`. Containment edges use `start` and `end` objects with `match_by: "id"`, `value`, and a custom kind constraint. The display schema registers the source kind, both collected node kinds, the helper, and both input and output relationship kinds under the distinct `PoC_Vault` namespace.

The helper producer returns one `PoC_Vault_Helper_<vault-id>` node per vault and records its contained `account_count`, including zero. The final query declares that producer as a prerequisite and consumes its rows through `$vault_helpers` before inspecting accounts and matching directory users by SID. A helper never substitutes for a directory user.

Helper updates and calculated relationships are saved together. If the final query fails after preparing a helper change, the previous helper, connections, and successful-run record must all remain intact.

## Command-based demonstration and historical evidence

From `tools/esc1-poc`, after building the executable and preparing the images:

```sh
docker compose -f compose.yaml down
./scripts/demo-opengraph.sh
docker compose -f compose.yaml up -d
```

The script stops the dedicated application and runs uploads against temporary labs. It exercises OpenGraph JSON, an OpenGraph ZIP, a mixed OpenGraph/classic ZIP, both arrival orders, exact connections, unchanged connection identities, and multiple matching accounts. It also checks that a malformed upload and a failed final query preserve the saved experimental state. Native ESC1 comparison runs alongside the vault extension without changing its owned objects.

Each order gets fresh database files; the script retains them under ignored `work/opengraph-*/` after stopping those temporary containers. [opengraph-results.json](../../evidence/opengraph-results.json) and the [browser screenshot](../../evidence/bloodhound-opengraph.png) preserve earlier runs of a different vault rule. Those historical checks and images do not validate the current `PoC_Vault_HasAccountFor` rule. Consult [REVIEW.md](../../REVIEW.md) for the recorded checks and their scope.
