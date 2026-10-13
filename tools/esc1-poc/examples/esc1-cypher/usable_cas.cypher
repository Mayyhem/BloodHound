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

// depends-on: query:enabled_cas.cypher, query:forest_reach.cypher

// Native accepts any enabled host when the CA domain cannot be resolved.
UNWIND $enabled_cas AS host
WITH host
WHERE host.ca_domain IS NULL OR
      any(forest IN $forest_reach WHERE forest.principal = host.ca_domain
          AND host.host_domain IN forest.reach)
RETURN DISTINCT host.ca AS ca, host.ca_graph_id AS graph_id, host.ca_kinds AS kinds
