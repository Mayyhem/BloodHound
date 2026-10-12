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

// depends-on: query:membership_reach.cypher, query:special_groups.cypher

// Graph reach is generic and reused. These label and SID decisions remain
// extension rules, including native's Group-only special-principal rule.
UNWIND $membership_reach AS principal
RETURN principal.principal AS principal, principal.graph_id AS graph_id,
       'Group' IN principal.kinds OR 'LocalGroup' IN principal.kinds AS is_group,
       'Group' IN principal.kinds AND
         (principal.principal ENDS WITH '-S-1-1-0' OR
          principal.principal ENDS WITH '-S-1-5-11') AS is_special,
       principal.reach AS reach, principal.reach_size AS reach_size,
       any(special IN $special_groups WHERE special.principal IN principal.reach) AS has_special_member
