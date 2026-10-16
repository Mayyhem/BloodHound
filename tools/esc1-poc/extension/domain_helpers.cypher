// namespace: PoC
// version: v1.0.0
// depends-on: node:Domain

// This helper demonstrates that helper-node and relationship publication
// share a transaction. ESC1 does not need the helper for its mathematics.
// Keeping it as a prerequisite lets rollback checks cover helper changes;
// the helper never substitutes for a real Domain.
MATCH (domain:Domain)
RETURN 'PoC_DomainHelper_' + domain.objectid AS id,
       ['PoC_DomainHelper'] AS kinds,
       {name: domain.name, domainid: domain.objectid} AS properties
