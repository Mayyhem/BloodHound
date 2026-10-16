// namespace: PoC
// version: v1.0.0
// depends-on: query:enabled_cas
// depends-on: node:Domain
// depends-on: relationship:SameForestTrust

// A certificate authority's own domain counts, as do domains that can reach
// it through forest trust. Every object along the route must be a Domain.
UNWIND $enabled_cas AS host
MATCH (root:Domain)
WHERE id(root) = host.ca_domain_graph_id
WITH DISTINCT root
MATCH route = (member:Domain)-[:SameForestTrust*0..]->(root)
WHERE all(domain IN nodes(route) WHERE domain:Domain)
RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) AS reach_size
