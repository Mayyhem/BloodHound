# Active Directory and CyberArk rules sharing one plan

This is a synthetic demonstration of two separate schemas. It is not an official CyberArk collector or schema. The account data and directory SIDs are artificial, and the rules make no changes to production collection queries or data.

Upload the five files in [extension](extension) together, or add the CyberArk schema and its two queries after activating the Active Directory schema and its query. Filenames are unique across the bundle. The rule service keeps both schemas and builds one dependency order across them.

| Schema file | Display name | Namespace | Rules |
| --- | --- | --- | --- |
| [active_directory.schema.json](extension/active_directory.schema.json) | Active Directory | `PoC_AD` | `ad_users.cypher` |
| [cyberark.schema.json](extension/cyberark.schema.json) | CyberArk | `PoC_CyberArk` | `cyberark_accounts.cypher`, `cyberark_links.cypher` |

The schemas describe types and endpoints. They contain no `post_processing` blocks or reference commit. The platform supplies reference settings; neither extension compares its results with native ESC1. Outputs have no ownership settings. See [the developer guide](../../DEVELOPERS.md#updating-older-bundles) for updating older files.

| Query file | What it returns | Earlier answer it needs |
| --- | --- | --- |
| `ad_users.cypher` | Helper rows with `id`, literal `['PoC_AD_User'] AS kinds`, and `properties.user_sid`. | None; reads native users. |
| `cyberark_accounts.cypher` | Helper rows with `id`, literal `['PoC_CyberArk_AccountSummary'] AS kinds`, and account properties. | None; reads collected CyberArk accounts. |
| `cyberark_links.cypher` | `source`, `target`, and literal `'PoC_CyberArk_AccountFor' AS kind`. Uses `keep-existing`. | `ad_users.cypher`, supplied as `$ad_users`. |

Load all five files when checking the complete example. The link query cannot be tested by pasting it alone into the normal query interface without supplying its prerequisite rows. For a smaller introduction, start with [the single-extension walkthrough](../../DEVELOPERS.md#quick-start).

The directory rule reads native `User` nodes and returns `PoC_AD_User_<sid>` helper nodes with `properties.user_sid`. It preserves the native user's identity. The CyberArk account rule independently summarizes collected `PoC_CyberArk_Account` nodes. The CyberArk link rule declares `query:ad_users.cypher` as a prerequisite and consumes those result rows through `$ad_users`. It joins each account's `user_sid` to the native user's `objectid`. The prerequisite supplies fresh rows directly; it does not rely on a graph read seeing new calculated relationships before publication.

The final relationship, `PoC_CyberArk_AccountFor`, goes from the collected account to the native `User`. It records a factual account identity association. It is not traversable and makes no claim about credential access, risk, or an attack path. The helper nodes remain separate identities and use distinct kinds and ID prefixes.

For example, an artificial account `CYBERARK-ACCOUNT-1` with `user_sid` equal to `S-1-5-21-111111111-222222222-333333333-1001` associates with a native user having that `objectid`. The collected account ID remains `CYBERARK-ACCOUNT-1`. Directory users should be loaded through the normal AD collection format; do not label custom OpenGraph nodes as native `User` or `Base`.

The two helper rules have no query prerequisites and appear in stage 1. The link rule appears in stage 2 after `ad_users.cypher`, regardless of schema upload order. These stages describe dependencies; helper creation and later queries run one at a time in the same transaction. The account helper can still run when the directory rule is disabled. Disabling the directory rule also pauses its link consumer, preserving both files and their outputs. Re-enabling the directory rule allows the consumer to run after a later successful analysis. Removing the directory rule instead cascades to the link rule and retires its calculated relationship within its declared endpoint scope.

If account data arrives before its directory user, it keeps `user_sid` for a later match. A subsequent successful run after the directory user arrives publishes the association without recollecting the account. The calculation only interprets facts currently stored in the graph; an omitted collection record does not prove a deletion.

Use the [rule service setup](../../USAGE.md) for the upload page. The page reports each rule's schema, activation date, dependencies, and enabled state. Activation dates survive overwrites and service restarts; they describe rule activation, not collection time. Older rules without a saved date show **Not recorded**. A new file in a preview shows **On activation**.
