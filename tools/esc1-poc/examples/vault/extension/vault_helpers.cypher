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

// depends-on: node:PoC_Vault_Container, node:PoC_Vault_Account,
// relationship:PoC_Vault_ContainsAccount

// Keep one helper for each vault, even when it contains no collected accounts.
// A changing count makes helper rollback observable after a later failure.
MATCH (vault:PoC_Vault_Container)
OPTIONAL MATCH (vault)-[:PoC_Vault_ContainsAccount]->(account:PoC_Vault_Account)
WITH vault, count(DISTINCT account) AS account_count
RETURN 'PoC_Vault_Helper_' + vault.objectid AS id,
       ['PoC_Vault_Helper'] AS kinds,
       {name: vault.name + ' processing helper',
        vault_id: vault.objectid,
        account_count: account_count} AS properties
