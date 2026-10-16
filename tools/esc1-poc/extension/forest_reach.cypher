// namespace: PoC
// version: v1.0.0
// depends-on: query:enabled_cas
// depends-on: node:Domain, relationship:SameForestTrust
// reachability: {"direction":"incoming","relationships":["SameForestTrust"],"include_start":true,"expand_results":false,"node_kinds":["Domain"],"reject_cycles":false}

// The reusable primitive visits each relevant Domain once and permits reciprocal
// forest edges. Including the root preserves the zero-length same-domain case.
UNWIND $enabled_cas AS host
WITH DISTINCT host.ca_domain AS id, host.ca_domain_graph_id AS graph_id, host.ca_domain_kinds AS kinds
WHERE id IS NOT NULL
RETURN id, graph_id, kinds
