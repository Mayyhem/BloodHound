// namespace: PoC
// version: v1.0.0
// depends-on: query:ca_root_reach
// depends-on: query:ca_auth_reach
// depends-on: node:EnterpriseCA, node:Domain, relationship:TrustedForNTAuth, relationship:NTAuthStoreFor

// Native certificate reach permits all three root-chain kinds at every depth.
// The shared primitive returns sets, so repeated routes cannot multiply rows.
UNWIND $ca_root_reach AS chain
MATCH (domain:Domain)
WHERE domain.objectid IN chain.reach
UNWIND $ca_auth_reach AS auth
WITH chain, domain, auth
WHERE auth.principal = chain.principal
// The last auth edge is NTAuthStoreFor; for longer routes its predecessor
// is TrustedForNTAuth. Earlier edges permit either kind.
// Anchor the two-edge suffix at an already selected Domain so an unlabeled
// objectid lookup cannot scan the graph to rediscover every middle node.
OPTIONAL MATCH (middle)-[:TrustedForNTAuth]->(store)-[:NTAuthStoreFor]->(domain)
WHERE middle.objectid IN auth.reach
WITH chain, domain, count(store) AS indirect
OPTIONAL MATCH (ca:EnterpriseCA)-[direct:NTAuthStoreFor]->(domain)
WHERE ca.objectid = chain.principal
WITH chain, domain, indirect, count(direct) AS direct_count
WHERE indirect > 0 OR direct_count > 0
RETURN DISTINCT chain.principal AS ca, domain.objectid AS domain
