// namespace: PoC
// version: v1.0.0
// depends-on: query:enrollment_principals
// depends-on: relationship:MemberOf
// depends-on: relationship:MemberOfLocalGroup
// reachability: {"direction":"incoming","relationships":["MemberOf","MemberOfLocalGroup"],"include_start":false,"expand_results":true,"reject_cycles":true}

// Shared sets cover direct enrollers and all nested candidates used by ESC1.
// This helper rejects cycles only in relevant enrollment components.
UNWIND $enrollment_principals AS principal
RETURN principal.id AS id, principal.graph_id AS graph_id, principal.kinds AS kinds
