# ESC1 query notes

`esc1.cypher` declares its edge name in the final projection: `RETURN left, right,
trusted.domain AS target, 'PoC_ADCSESC1' AS kind`. The runner preserves that name
when group-overlap turns the permission lists into source/target pairs. The
schema describes the kind and its allowed endpoints; it has no processing block.

Cypher selects certificate eligibility, trust evidence, and enrollment rights.
The runner supplies generic reachability and ordered group-overlap primitives
for shared graph and permission-set operations. Files with a `reachability`
JSON comment select seeds with `id`, `graph_id`, and `kinds` columns. The runner transforms them
into rows containing `principal`, `graph_id`, `kinds`, `reach`, and `reach_size`.
Running a seed file directly in a database returns seeds, not those transformed
rows. Consumers receive the transformed rows under the ordinary filename-stem
parameter, and dependency comments still determine execution order.

A reachability directive selects `incoming` or `outgoing` traversal, permitted
relationship kinds, optional permitted node kinds, whether to include the seed,
and whether to return all visited candidates or just seeds. `reject_cycles`
opts into acyclic evidence. Unknown fields, invalid kinds, missing dependency
declarations, and duplicate directives fail plan loading. The primitive makes
batched single-hop reads using graph-ID indexes, visits each relevant node
once, and reuses compressed
reachable sets. It never enumerates complete routes. Reciprocal forest trust is
handled through strongly connected components.

The query sequence is:

1. `enabled_cas.cypher` selects enabled Base hosts and resolves host and CA
   domains. `forest_reach.cypher` visits only the relevant Domain forest
   components, including the seed for the same-domain case. `usable_cas.cypher`
   applies native's unresolved-CA-domain exception.
2. `ca_root_reach.cypher` follows any combination of IssuedSignedBy,
   EnterpriseCAFor, and RootCAFor. `ca_auth_reach.cypher` follows either
   TrustedForNTAuth or NTAuthStoreFor. `trusted_cas.cypher` intersects root and
   auth evidence for each CA/domain, retaining the exact auth suffix rule: one
   NTAuthStoreFor edge, or a suffix of TrustedForNTAuth then NTAuthStoreFor.
3. `eligible_templates.cypher` retains all five native property conditions,
   including numeric types and the version/signature exception.
4. `enrollment_principals.cypher` selects Base principals for eligible,
   published template/CA pairs. Template enrollment unions Enroll, GenericAll,
   and AllExtendedRights; CA enrollment uses only Enroll.
5. `membership_reach.cypher` builds incoming MemberOf/MemberOfLocalGroup reach
   only for those direct principals and their nested candidates. No label
   filter applies to nested members. `special_groups.cypher` selects Group
   nodes ending in the exact SID suffixes `-S-1-1-0` or `-S-1-5-11`.
   `membership.cypher` derives Group/LocalGroup flags and nested special status.
6. `esc1.cypher` selects ordered direct permission sets for each target. Its
   `group-overlap` JSON comment names the membership prerequisite and invokes
   the reusable group-aware set calculation. Its `batch-on` declaration permits
   the runner to divide trusted CA/domain rows into bounded chunks; unioning
   the chunks preserves the complete relationship set. These seed files use
   ordinary Cypher without CASE, reduce, comprehensions, list indexing, or
   variable-length paths.

The permission cross-product keeps template principals first and CA principals
second. It skips an entire side when a direct or nested special Group is found,
uses ordered direct graph IDs, removes each expanded principal after union, and
processes nonempty-reach candidates from smallest reach to largest. Shortcut
removal uses set difference, not XOR. Selected groups stay groups rather than
being replaced by their users. `domain_helpers.cypher` remains a separate
prerequisite to demonstrate atomic helper and relationship publication.

Membership and certificate/auth cycles in the relevant components fail before
publication. Cycles in disconnected membership components no longer block an
unrelated calculation. Reciprocal Domain forest trust is permitted. Certificate
and auth reach are restricted to acyclic components because the generic set
composition has not established native equivalence for cyclic certificate
routes. This boundary is explicit rather than accepting an approximate result.

Repeated routes no longer cause exponential path work, but dense reachability
can still require a large transitive closure. The runner limits graph-cache
estimates, individual answers, prerequisite parameters, temporary disk, time,
and output cardinality. Reaching a limit fails the calculation and preserves
previous publication; it never authorizes deletion from a partial answer.
Encoded byte limits are not a guarantee about total Go or database memory.
There is no claim that this implementation has been tested on 100 million
objects, nor a claimed performance ratio against native Go. Native comparison
and representative measurements remain necessary before extending the tested
scope.
