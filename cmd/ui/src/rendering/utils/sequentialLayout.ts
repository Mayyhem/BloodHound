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

import { MultiDirectedGraph } from 'graphology';
import { RankDirection, setDagreLayout } from './dagre';

// Keep the same left-to-right layout for collected graphs and analysis plans.
// Layout changes only drawing coordinates; it never contacts the database.
export const sequentialLayout = (graph: MultiDirectedGraph, rankSeparation = 500) => {
    const { assign } = setDagreLayout({ graph: { rankdir: RankDirection.LEFT_RIGHT, ranksep: rankSeparation } }, graph);
    assign();
};
