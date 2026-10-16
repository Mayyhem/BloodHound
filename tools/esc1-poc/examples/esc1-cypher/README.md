# ESC1 using ordinary Cypher

This example calculates ESC1 without `reachability`, `batch-on`, or `group-overlap`. The [advanced example](../../extension) makes the same decisions using those optional instructions. Both use the existing schema, declared inputs, output checks, and saving protections.

Exact-answer checks and repeated comparisons through one million objects pass on Neo4j and PostgreSQL. Ordinary Cypher has a measured cost, particularly for group selection. See the [comparison report](../../CYPHER_COMPARISON.md) for direct feature comparisons, input limits and advice on choosing the advanced options. The generated graph has a small relevant input share; these results do not establish capacity for a million relevant group members.

**Scope note:** this solution does not account for privileged zone rules, findings, exposure or impact analysis, or other BloodHound Enterprise features. It only calculates the ESC1 connections.

## Follow the calculation

The certificate eligibility, enabled-authority, enrollment-principal, and domain-helper queries are the same as the advanced example. Five files change the calculation, and `trusted_cas.cypher` changes only an explanatory comment. The report includes [complete file differences and the separate batching change](../../CYPHER_COMPARISON.md#actual-cypher-file-differences). The five calculation changes are:

| File | What it calculates |
| --- | --- |
| `membership_reach.cypher` | For each relevant principal, all objects beneath it through group membership. A principal can be a user, group, computer, or local group. |
| `forest_reach.cypher` | Domains connected by the allowed forest trust links, including the starting domain. |
| `ca_root_reach.cypher` | Objects reached through the permitted certificate links to a root authority and domain. |
| `ca_auth_reach.cypher` | Objects reached through the permitted certificate links to an authentication store and domain, including the starting authority. |
| `esc1.cypher` | Which exact groups or individuals have the required permissions on both a template and its certificate authority. |

In the path queries, `*1..` means follow one or more links; `*0..` also allows the starting object. `DISTINCT` prevents several routes to an object from counting it several times. `OPTIONAL MATCH` keeps a starting object with an empty answer when no route exists.

Membership and certificate loops are rejected by the advanced example. The ordinary queries make that check explicit: look for an object reachable from itself, count it once, and divide by `1 - count`. A valid object gives a divisor of one; a loop gives zero and stops the query. This arithmetic check is awkward, but it uses ordinary Cypher and prevents publishing a different answer. Forest trust loops are allowed. A failure leaves the previous saved results in place.

## Why the group query has four steps

The template's permissions are called **left** and the authority's permissions **right**. Each template, authority, and domain combination stays separate throughout the calculation.

1. Collect the direct permissions on each side.
2. Recognize Everyone and Authenticated Users. A side described as “excluded” no longer needs a separate permission check. Record whether each candidate directly matches and whether the right side also accepts it through a group.
3. Keep direct left-side matches. For the others, find the members that the built-in calculation would consider. The comparison of object numbers preserves that calculation's processing order.
4. Keep qualifying groups. Keep an individual member only when a selected group does not already represent it. Apply the special-group exceptions from step 2.

This is more than intersecting two lists of users: the answer must preserve BloodHound's choice of group connections too.

The shorter group calculation relies on complete membership lists without loops. An outer group then has more reachable objects than any group inside it. Processing smaller lists first cannot cause an earlier group to remove a later group candidate. This lets Cypher describe the chosen groups directly. It does **not** justify changing the generic `group-overlap` instruction for arbitrary, incomplete lists supplied by other extensions.

The membership and kind lists are used as sets. Their order is irrelevant here; object-number comparisons in the group query are explicit.

## Run and compare

From `tools/esc1-poc`, validate the files with:

```sh
./scripts/go.sh run . plan -extension examples/esc1-cypher
```

Use the same upload and execution workflow described in [the author guide](../../DEVELOPERS.md). This example and the advanced one describe the same schema and output type, so choose one version for a run.

Without `batch-on`, the engine sends the complete input to the query. It does not automatically split it. The same input, time, memory and temporary-storage, and saving limits still apply. An input that exceeds a limit fails rather than publishing a partial answer.
