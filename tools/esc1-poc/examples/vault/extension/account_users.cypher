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

// existing-edge-policy: keep-existing

// depends-on: query:vault_helpers.cypher,
// node:PoC_Vault_Container, node:PoC_Vault_Account, node:User,
// relationship:PoC_Vault_ContainsAccount

// Use this run's helper rows to choose vaults, then read the collected evidence.
// The account and directory user stay separate; the SID is the joining value.
UNWIND $vault_helpers AS helper
MATCH (vault:PoC_Vault_Container)-[:PoC_Vault_ContainsAccount]->(account:PoC_Vault_Account)
WHERE vault.objectid = helper.properties.vault_id
MATCH (directory_user:User)
WHERE directory_user.objectid = account.user_sid

// Several contained accounts can support the same single published connection.
RETURN DISTINCT vault.objectid AS source, directory_user.objectid AS target, 'PoC_Vault_HasAccountFor' AS kind
