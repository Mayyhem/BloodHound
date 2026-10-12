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

// depends-on: query:ca_root_reach.cypher, query:ca_auth_reach.cypher,
// node:Domain, relationship:TrustedForNTAuth, relationship:NTAuthStoreFor

// Native certificate reach permits all three root-chain kinds at every depth.
// The shared primitive returns sets, so repeated routes cannot multiply rows.
UNWIND $ca_root_reach AS chain
MATCH (domain:Domain)
WHERE domain.objectid IN chain.reach
UNWIND $ca_auth_reach AS auth
WITH chain, domain, auth
WHERE auth.principal = chain.principal
// The last auth edge is NTAuthStoreFor; for longer routes its predecessor
// is TrustedForNTAuth. Earlier edges permit either kind.
// Anchor the two-edge suffix at an already selected Domain so an unlabeled
// objectid lookup cannot scan the graph to rediscover every middle node.
OPTIONAL MATCH (middle)-[:TrustedForNTAuth]->(store)-[:NTAuthStoreFor]->(domain)
WHERE middle.objectid IN auth.reach
WITH chain, domain, count(store) AS indirect
OPTIONAL MATCH (ca:EnterpriseCA)-[direct:NTAuthStoreFor]->(domain)
WHERE ca.objectid = chain.principal
WITH chain, domain, indirect, count(direct) AS direct_count
WHERE indirect > 0 OR direct_count > 0
RETURN DISTINCT chain.principal AS ca, domain.objectid AS domain
