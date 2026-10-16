// namespace: PoC
// version: v1.0.0
// depends-on: query:membership_reach
// depends-on: query:special_groups

// Graph reach is generic and reused. These label and SID decisions remain
// extension rules, including native's Group-only special-principal rule.
UNWIND $membership_reach AS principal
RETURN principal.principal AS principal, principal.graph_id AS graph_id,
       'Group' IN principal.kinds OR 'ADLocalGroup' IN principal.kinds AS is_group,
       'Group' IN principal.kinds AND
         (principal.principal ENDS WITH '-S-1-1-0' OR
          principal.principal ENDS WITH '-S-1-5-11') AS is_special,
       principal.reach AS reach, principal.reach_size AS reach_size,
       any(special IN $special_groups WHERE special.principal IN principal.reach) AS has_special_member
