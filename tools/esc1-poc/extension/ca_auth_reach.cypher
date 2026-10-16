// namespace: PoC
// version: v1.0.0
// depends-on: query:usable_cas
// depends-on: relationship:TrustedForNTAuth
// depends-on: relationship:NTAuthStoreFor
// reachability: {"direction":"outgoing","relationships":["TrustedForNTAuth","NTAuthStoreFor"],"include_start":true,"expand_results":false,"reject_cycles":true}

// The final trust edges are checked separately in trusted_cas. Identity reach
// allows the CA itself to supply the two-edge suffix.
UNWIND $usable_cas AS ca
RETURN DISTINCT ca.ca AS id, ca.graph_id AS graph_id, ca.kinds AS kinds
