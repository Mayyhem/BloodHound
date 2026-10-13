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

// depends-on: query:enabled_cas.cypher, node:Domain,
// relationship:SameForestTrust

// A certificate authority's own domain counts, as do domains that can reach
// it through forest trust. Every object along the route must be a Domain.
UNWIND $enabled_cas AS host
MATCH (root:Domain)
WHERE id(root) = host.ca_domain_graph_id
WITH DISTINCT root
MATCH route = (member:Domain)-[:SameForestTrust*0..]->(root)
WHERE all(domain IN nodes(route) WHERE domain:Domain)
RETURN root.objectid AS principal, id(root) AS graph_id, labels(root) AS kinds,
       collect(DISTINCT member.objectid) AS reach, count(DISTINCT member) AS reach_size
