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

// depends-on: query:trusted_cas
// depends-on: query:eligible_templates
// depends-on: query:membership
// depends-on: node:Base, node:CertTemplate, node:EnterpriseCA
// depends-on: relationship:PublishedTo, relationship:Enroll, relationship:GenericAll
// depends-on: relationship:AllExtendedRights
// group-overlap: {"memberships":"membership"}
// batch-on: trusted_cas

// Template enrollment is the left side; CA enrollment is the right side.
// The shared group-overlap operation preserves native direct-ID order and
// shortcut rules using the extension's membership flags and reachable sets.
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
RETURN left, right, trusted.domain AS target, 'PoC_ADCSESC1' AS kind
