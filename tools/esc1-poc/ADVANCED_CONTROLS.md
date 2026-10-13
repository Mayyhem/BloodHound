# Optional controls for experienced authors

Start with ordinary Cypher when it expresses your calculation clearly. The [ordinary ESC1 example](examples/esc1-cypher/README.md) performs the complete analysis without any of the three instructions below. The [advanced ESC1 example](extension/QUERY_NOTES.md) uses all three. They must produce the same exact connections, including the choice of groups instead of individual users.

These controls change how work is done. They do not relax schema checks, input declarations, result limits, or the rule that a failed calculation keeps the previous saved results. Missing schemas still leave uploads as inactive drafts.

| Control | What it asks the engine to do | Ordinary alternative | Author's responsibility |
| --- | --- | --- | --- |
| `reachability` | Find the complete set of objects reachable through the declared links, using the shared Go calculation. | Use paths such as `[:MemberOf*1..]`, retain empty answers with `OPTIONAL MATCH`, and remove repeated objects with `DISTINCT`. | Specify direction, allowed links, whether the starting object belongs in the answer, and how loops should behave. A set of reachable objects is different from a list of routes. |
| `batch-on` | Divide the named input into pieces, execute the query for each piece, then combine its answers. | Omit the comment and run the complete query with the complete input. | Every piece must be independent. Totals, rankings, limits and group decisions can change when related rows are separated. |
| `group-overlap` | Apply BloodHound's ordered choice of groups and individuals from the two permission lists. | Express the same choices in ordinary Cypher, as the four steps in `examples/esc1-cypher/esc1.cypher` do. | This is more than finding common users. Preserve direct permissions, special groups, nested groups and the choices made when groups overlap. |

Do not remove an instruction from an existing query unless the query already does the work that instruction supplied. For example, the advanced ESC1 files rely on the Go reachability calculation; deleting its comment alone would remove part of the analysis.

Without `batch-on`, the engine currently sends the complete input. It does not decide how to split it automatically. The same size and time limits apply. A query may fit in pieces but exceed the input limit when sent all at once. That is a capacity difference, not proof that batching always makes small inputs faster.

Use the [comparison report](CYPHER_COMPARISON.md) to judge measured costs on each database. In the completed ESC1 studies, `reachability` helped PostgreSQL and `group-overlap` helped both databases. `batch-on` let a deliberately wide input complete where the whole input exceeded the same parameter allowance. These are measured examples, not a promise that every query benefits. Query times can overlap because up to five ready queries may run together. Use the complete-run measurement to compare elapsed time; do not add individual query times.

## Check a choice before adopting it

Keep a complete ordinary version and compare exact answers on small cases, including empty results, overlapping groups and invalid evidence. Then change one instruction at a time and measure both versions under the same limits. For batching, try several piece sizes. If the answer changes with piece size, the split is unsafe.

The ordinary ESC1 group query relies on complete membership sets without loops. Its reasoning does not make arbitrary, incomplete lists equivalent to the generic `group-overlap` operation. Membership and certificate loops fail in both supplied examples. Forest-trust loops are allowed.

The examples check loops in ordinary Cypher by causing division by zero when a relevant object can reach itself. This is explicit but awkward. A failed run identifies the query and preserves saved results. Improving how authors express this check is a possible follow-up; it is not another required instruction.

## Query-language changes and remaining limits

The PostgreSQL implementation now preserves ordinary unbounded paths beyond fifteen links, carried inputs across `WITH` and optional path matches, Boolean map values, repeated parameters whose names also appear as columns, values used by a later `SET`, and ordering by a returned column that reuses an earlier object's name. The same examples and expected answers are tested on Neo4j and PostgreSQL.

It also reuses an already selected object when a later link ends there. The old translation could multiply a count and repeat work for every object in the graph. This correction benefits both authoring styles automatically; authors do not need an instruction to avoid that work.

An explicit upper path bound still applies. The existing default bound in shortest-path operations is unchanged; this work concerns ordinary path matches. PostgreSQL still does not support every Neo4j query form. For example, a sequence that writes, passes through `WITH`, and writes again can produce unsupported nested database writes. The supplied examples do not require that form. File validation checks the supported language and declarations, but actual database execution remains a separate check.

Future control ideas and their evidence belong in the comparison report. None is required for ordinary authors, and none has been implemented as part of this work.
