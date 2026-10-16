// namespace: PoC
// version: v1.0.0
// depends-on: query:usable_cas
// depends-on: relationship:TrustedForNTAuth, relationship:NTAuthStoreFor

// Follow the permitted certificate links. Keep a row even when no
// object is reachable; later queries decide whether a domain qualifies.
UNWIND $usable_cas AS ca
MATCH (root)
WHERE id(root) = ca.graph_id
WITH DISTINCT root
OPTIONAL MATCH (root)-[:TrustedForNTAuth|NTAuthStoreFor*0..]->(member)
// Reject a loop in the relevant evidence instead of saving a partial answer.
// Each checked object can match itself at most once after DISTINCT counting.
// The divisor is one for valid evidence and zero for a loop; division by zero
// stops the query and the runner keeps all previously saved results.
WITH DISTINCT root, member
OPTIONAL MATCH (member)-[:TrustedForNTAuth|NTAuthStoreFor*1..]->(loop)
WHERE id(loop) = id(member)
WITH root, member, 1 / (1 - count(DISTINCT loop)) AS valid_evidence
RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) / min(valid_evidence) AS reach_size
