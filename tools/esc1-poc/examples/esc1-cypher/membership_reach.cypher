// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// depends-on: query:enrollment_principals.cypher,
// relationship:MemberOf, relationship:MemberOfLocalGroup

// Include each direct enroller and every member beneath it. A zero-length
// match includes the enroller itself. Keep groups as well as users.
UNWIND $enrollment_principals AS seed
MATCH (principal)-[:MemberOf|MemberOfLocalGroup*0..]->(root)
WHERE id(root) = seed.graph_id
WITH DISTINCT principal
// Reject a loop in the relevant evidence instead of saving a partial answer.
// Each checked object can match itself at most once after DISTINCT counting.
// The divisor is one for valid evidence and zero for a loop; division by zero
// stops the query and the runner keeps all previously saved results.
OPTIONAL MATCH (principal)-[:MemberOf|MemberOfLocalGroup*1..]->(loop)
WHERE id(loop) = id(principal)
WITH principal, 1 / (1 - count(DISTINCT loop)) AS valid_evidence
// Check each principal once before expanding its members. A principal with no
// members must still have a row with an empty member list.
OPTIONAL MATCH (member)-[:MemberOf|MemberOfLocalGroup*1..]->(principal)
RETURN principal.objectid AS principal, id(principal) AS graph_id,
       labels(principal) AS kinds, collect(DISTINCT member.objectid) AS reach,
       count(DISTINCT member) / min(valid_evidence) AS reach_size
