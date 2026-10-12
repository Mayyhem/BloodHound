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

// depends-on: query:enabled_cas.cypher, node:Domain, relationship:SameForestTrust
// reachability: {"direction":"incoming","relationships":["SameForestTrust"],"include_start":true,"expand_results":false,"node_kinds":["Domain"],"reject_cycles":false}

// The reusable primitive visits each relevant Domain once and permits reciprocal
// forest edges. Including the root preserves the zero-length same-domain case.
UNWIND $enabled_cas AS host
WITH DISTINCT host.ca_domain AS id, host.ca_domain_graph_id AS graph_id, host.ca_domain_kinds AS kinds
WHERE id IS NOT NULL
RETURN id, graph_id, kinds
