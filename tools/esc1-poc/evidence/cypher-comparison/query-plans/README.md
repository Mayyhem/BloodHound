# PostgreSQL trust-query correction

These are read-only `EXPLAIN (FORMAT JSON)` observations on the generated million-object graph. They do **not** execute the calculation. Row counts in these plans are estimates, not measured output counts.

The earlier translation joined a fresh copy of the node table when a later path ended at an already selected object. That copy had no restriction. In `before.json`, the index scan named `n0_1` estimates 999,991 rows. The corrected translation reuses the object already present in the input row; that scan is absent from `after.json`.

The shared five-object regression test proves the answer error independently: one path counted five times before the fix, and once after it. Required/optional matches, saving the count, reversed/undirected links and a self-loop are checked on both databases. The million-object timing comparison measures the resulting effect on complete ESC1 runs.

The SQL files retain the exact rendered statements. Numeric kind and graph identifiers belong to this generated database; recreating it can assign different identifiers. The original query is `extension/trusted_cas.cypher`. Both planning observations use the same synthetic trust inputs and 32 MiB query-memory setting. Only the updated translation changes. This automatic correction benefits both authoring styles and is separate from the optional-instruction comparisons.
