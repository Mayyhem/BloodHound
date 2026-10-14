// namespace: PoC
// version: v1.0.0
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

// depends-on: node:Base, node:EnterpriseCA, node:Domain, relationship:HostsCAService

// Resolve host and CA domains once before traversing their forest components.
MATCH (host:Base)-[:HostsCAService]->(ca:EnterpriseCA)
WHERE host.enabled = true
OPTIONAL MATCH (ca_domain:Domain)
WHERE ca.domainsid IS NOT NULL AND ca.domainsid <> ''
  AND toUpper(ca_domain.objectid) = toUpper(ca.domainsid)
OPTIONAL MATCH (host_domain:Domain)
WHERE host.domainsid IS NOT NULL AND host.domainsid <> ''
  AND toUpper(host_domain.objectid) = toUpper(host.domainsid)
RETURN DISTINCT ca.objectid AS ca, id(ca) AS ca_graph_id, labels(ca) AS ca_kinds,
                ca_domain.objectid AS ca_domain, id(ca_domain) AS ca_domain_graph_id,
                labels(ca_domain) AS ca_domain_kinds, host_domain.objectid AS host_domain
