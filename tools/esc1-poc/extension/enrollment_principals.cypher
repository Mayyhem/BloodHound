// namespace: PoC
// version: v1.0.0
// depends-on: query:trusted_cas
// depends-on: query:eligible_templates
// depends-on: node:Base, node:CertTemplate, node:EnterpriseCA, relationship:PublishedTo
// depends-on: relationship:Enroll, relationship:GenericAll, relationship:AllExtendedRights

// Only enrollment principals for eligible, published template/CA pairs seed
// membership work. Nested members still retain every native node kind.
UNWIND $trusted_cas AS trusted
UNWIND $eligible_templates AS eligible
MATCH (template:CertTemplate)-[:PublishedTo]->(ca:EnterpriseCA)
WHERE ca.objectid = trusted.ca
  AND template.objectid = eligible.template
OPTIONAL MATCH (template_principal:Base)-[:Enroll|GenericAll|AllExtendedRights]->(template)
WITH ca, collect(DISTINCT {id: template_principal.objectid,
                         graph_id: id(template_principal), kinds: labels(template_principal)}) AS template_direct
OPTIONAL MATCH (ca_principal:Base)-[:Enroll]->(ca)
WITH template_direct, collect(DISTINCT {id: ca_principal.objectid,
                                     graph_id: id(ca_principal), kinds: labels(ca_principal)}) AS ca_direct
UNWIND template_direct + ca_direct AS principal
WITH principal
WHERE principal.id IS NOT NULL
RETURN DISTINCT principal.id AS id, principal.graph_id AS graph_id, principal.kinds AS kinds
