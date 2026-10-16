// namespace: PoC
// version: v1.0.0
// depends-on: query:usable_cas
// depends-on: relationship:IssuedSignedBy
// depends-on: relationship:EnterpriseCAFor, relationship:RootCAFor
// reachability: {"direction":"outgoing","relationships":["IssuedSignedBy","EnterpriseCAFor","RootCAFor"],"include_start":false,"expand_results":false,"reject_cycles":true}

// All three certificate relationships are permitted at every depth.
UNWIND $usable_cas AS ca
RETURN DISTINCT ca.ca AS id, ca.graph_id AS graph_id, ca.kinds AS kinds
