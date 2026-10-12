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

// depends-on: query:usable_cas.cypher, relationship:TrustedForNTAuth,
// relationship:NTAuthStoreFor
// reachability: {"direction":"outgoing","relationships":["TrustedForNTAuth","NTAuthStoreFor"],"include_start":true,"expand_results":false,"reject_cycles":true}

// The final trust edges are checked separately in trusted_cas. Identity reach
// allows the CA itself to supply the two-edge suffix.
UNWIND $usable_cas AS ca
RETURN DISTINCT ca.ca AS id, ca.graph_id AS graph_id, ca.kinds AS kinds
