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

// depends-on: query:ad_users.cypher, node:PoC_CyberArk_Account, node:User

// Consume this run's directory summary rows across the schema boundary.
// The target remains the native User, rather than the summary helper.
UNWIND $ad_users AS directory_user
MATCH (account:PoC_CyberArk_Account)
WHERE account.user_sid = directory_user.properties.user_sid
MATCH (native_user:User)
WHERE native_user.objectid = directory_user.properties.user_sid
RETURN DISTINCT account.objectid AS source, native_user.objectid AS target, 'PoC_CyberArk_AccountFor' AS kind
