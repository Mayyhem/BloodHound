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

// depends-on: query:usable_cas.cypher, relationship:IssuedSignedBy, relationship:EnterpriseCAFor, relationship:RootCAFor

// Follow the permitted certificate links. Keep a row even when no
// object is reachable; later queries decide whether a domain qualifies.
UNWIND $usable_cas AS ca
MATCH (root)
WHERE id(root) = ca.graph_id
WITH DISTINCT root
OPTIONAL MATCH (root)-[:IssuedSignedBy|EnterpriseCAFor|RootCAFor*1..]->(member)
// Reject a loop in the relevant evidence instead of saving a partial answer.
// Each checked object can match itself at most once after DISTINCT counting.
// The divisor is one for valid evidence and zero for a loop; division by zero
// stops the query and the runner keeps all previously saved results.
WITH DISTINCT root, member
OPTIONAL MATCH (member)-[:IssuedSignedBy|EnterpriseCAFor|RootCAFor*1..]->(loop)
WHERE id(loop) = id(member)
WITH root, member, 1 / (1 - count(DISTINCT loop)) AS valid_evidence
RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) / min(valid_evidence) AS reach_size
