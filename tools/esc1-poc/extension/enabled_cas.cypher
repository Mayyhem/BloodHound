// namespace: PoC
// version: v1.0.0
// depends-on: node:Base, node:EnterpriseCA, node:Domain, relationship:HostsCAService

// Resolve host and CA domains once before traversing their forest components.
MATCH (host:Base)-[:HostsCAService]->(ca:EnterpriseCA)
WHERE host.enabled = true
OPTIONAL MATCH (ca_domain:Domain)
WHERE ca.domainsid IS NOT NULL AND ca.domainsid <> ''
  AND toUpper(ca_domain.objectid) = toUpper(ca.domainsid)
OPTIONAL MATCH (host_domain:Domain)
WHERE host.domainsid IS NOT NULL AND host.domainsid <> ''
  AND toUpper(host_domain.objectid) = toUpper(host.domainsid)
RETURN DISTINCT ca.objectid AS ca, id(ca) AS ca_graph_id, labels(ca) AS ca_kinds,
                ca_domain.objectid AS ca_domain, id(ca_domain) AS ca_domain_graph_id,
                labels(ca_domain) AS ca_domain_kinds, host_domain.objectid AS host_domain
