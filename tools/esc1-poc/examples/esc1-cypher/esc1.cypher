// namespace: PoC
// version: v1.0.0
// depends-on: query:trusted_cas
// depends-on: query:eligible_templates
// depends-on: query:membership
// depends-on: node:Base, node:CertTemplate, node:EnterpriseCA
// depends-on: relationship:PublishedTo, relationship:Enroll, relationship:GenericAll
// depends-on: relationship:AllExtendedRights

// 1. Keep the template's permissions (left) separate from the authority's
// permissions (right). Each template/authority/domain combination is one decision.
UNWIND $trusted_cas AS trusted
UNWIND $eligible_templates AS eligible
MATCH (template:CertTemplate)-[:PublishedTo]->(ca:EnterpriseCA)
WHERE ca.objectid = trusted.ca AND template.objectid = eligible.template
OPTIONAL MATCH (template_principal:Base)-[:Enroll|GenericAll|AllExtendedRights]->(template)
// Keep each template and CA separate. Combining their permission lists can
// invent a group shortcut that no individual template/CA combination allows.
WITH trusted, ca, template, collect(DISTINCT template_principal.objectid) AS left
OPTIONAL MATCH (ca_principal:Base)-[:Enroll]->(ca)
WITH trusted, ca, template, left, collect(DISTINCT ca_principal.objectid) AS right
// 2. A special group makes that permission side unnecessary ("excluded").
WITH trusted, ca, template, left, right,
     any(candidate IN $membership WHERE candidate.principal IN left AND
         candidate.is_group AND (candidate.is_special OR candidate.has_special_member)) AS left_excluded,
     any(candidate IN $membership WHERE candidate.principal IN right AND
         candidate.is_group AND (candidate.is_special OR candidate.has_special_member)) AS right_excluded
// Record the direct permissions and the authority's permitted members.
UNWIND $membership AS candidate
WITH trusted, ca, template, left_excluded, right_excluded,
     collect({principal:candidate.principal, graph_id:candidate.graph_id,
              reach:candidate.reach, reach_size:candidate.reach_size,
              left_direct:candidate.principal IN left,
              right_direct:candidate.principal IN right,
              right_matches:candidate.principal IN right OR
                any(group IN $membership WHERE group.is_group AND group.principal IN right AND candidate.principal IN group.reach)}) AS checks
// 3. A direct template principal already accepted by the authority stays direct.
// Otherwise examine its members. BloodHound processes direct principals in
// object-number order: a later principal can introduce an earlier one again.
UNWIND checks AS candidate
WITH trusted, ca, template, left_excluded, right_excluded,
     collect({principal:candidate.principal, reach:candidate.reach,
              reach_size:candidate.reach_size, left_direct:candidate.left_direct,
              right_direct:candidate.right_direct, right_matches:candidate.right_matches,
              remaining:any(direct IN checks WHERE
                direct.left_direct AND NOT direct.right_matches AND candidate.principal IN direct.reach AND
                (NOT (candidate.left_direct AND NOT candidate.right_matches) OR direct.graph_id > candidate.graph_id))}) AS choices
// 4. Keep direct matches and qualifying groups. Keep an individual member only
// if no selected group represents it. Membership loops have already been rejected.
// In a complete membership graph without loops, an outer group has more members
// than each nested group. The smaller-first calculation therefore considers
// every qualifying group; no earlier group can remove a later candidate.
UNWIND choices AS candidate
WITH trusted, ca, template, left_excluded, right_excluded, choices, candidate
WHERE
  (left_excluded AND right_excluded AND (candidate.left_direct OR candidate.right_direct)) OR
  (left_excluded AND NOT right_excluded AND candidate.right_direct) OR
  (right_excluded AND NOT left_excluded AND candidate.left_direct) OR
  (NOT left_excluded AND NOT right_excluded AND (
    (candidate.left_direct AND candidate.right_matches) OR
    (candidate.remaining AND candidate.right_matches AND (
      candidate.reach_size > 0 OR
      (candidate.reach_size = 0 AND NOT any(group IN choices WHERE
        group.remaining AND group.right_matches AND group.reach_size > 0 AND
        candidate.principal IN group.reach))
    ))
  ))
// Keep one row for each supporting template/authority/domain decision. The
// engine combines repeated connections when saving, just as it does for the
// advanced instruction; later queries can still see every supporting row.
RETURN candidate.principal AS source, trusted.domain AS target, 'PoC_ADCSESC1' AS kind
