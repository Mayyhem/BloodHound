// namespace: PoC
// version: v1.0.0
// depends-on: query:enabled_cas
// depends-on: query:forest_reach

// Native accepts any enabled host when the CA domain cannot be resolved.
UNWIND $enabled_cas AS host
WITH host
WHERE host.ca_domain IS NULL OR
      any(forest IN $forest_reach WHERE forest.principal = host.ca_domain
          AND host.host_domain IN forest.reach)
RETURN DISTINCT host.ca AS ca, host.ca_graph_id AS graph_id, host.ca_kinds AS kinds
