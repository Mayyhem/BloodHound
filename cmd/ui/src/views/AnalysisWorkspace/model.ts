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

import { Dependency, Query, RunRecord, Workspace } from './types';

// Use the whole plan, not filtered rows. Sorting keeps each extension's number
// stable when execution order or the selected filter changes.
export const numberedExtensions = (queries: Query[]) =>
    [...new Set(queries.map((query) => query.namespace))]
        .sort()
        .map((namespace, index) => ({ namespace, number: index + 1 }));

export const queryLabel = (name: string) =>
    name
        .split('/')
        .pop()
        ?.replace(/\.cypher$/, '') ?? name;
export const duration = (nanoseconds: number) => `${(nanoseconds / 1e9).toFixed(3)} s`;
export const observedTime = (value?: string) =>
    !value || value.startsWith('0001-') ? 'Unavailable' : new Date(value).toLocaleString();
// Node colors in the graph. The legend in the top panel shows the same list.
export const queryColors = {
    enabled: { color: '#258a58', label: 'Enabled' },
    blocked: { color: '#ed8b00', label: 'Blocked' },
    failed: { color: '#c62828', label: 'Failed' },
    disabled: { color: '#9e9e9e', label: 'Disabled' },
    focused: { color: '#1976d2', label: 'Current focus' },
};
// A query the author disabled is gray. Otherwise a query whose step failed in
// the latest completed run of these exact rules is red, a blocked query is
// orange, and everything else is green. `outcome` is that run's state for the
// query; it is missing when no completed run matches the current rules.
export const queryColor = (query: Query, outcome?: string) =>
    !query.author_enabled
        ? queryColors.disabled.color
        : outcome === 'failed'
          ? queryColors.failed.color
          : !query.enabled
            ? queryColors.blocked.color
            : queryColors.enabled.color;
export const availability = (query: Query) =>
    !query.author_enabled ? 'Disabled by author' : query.enabled ? 'Enabled' : 'Enabled · blocked';
// The existing API counts stages from 1. The workspace counts dependency
// arrows from 0 without changing the API or previously saved run reports.
export const dependencyDepth = (query: Query) => query.stage - 1;
export const dependencyKey = (dependency: Pick<Dependency, 'prerequisite' | 'consumer'>) =>
    JSON.stringify([dependency.prerequisite, dependency.consumer]);
// These states came from a completed attempt. They are never live progress.
export const completedState = (state?: string) =>
    state === 'waiting' ? 'Not reached' : state === 'running' ? 'Outcome unavailable' : state ?? 'Unavailable';
export const dependencyReason = (dependency: Dependency) =>
    dependency.type === 'query'
        ? `As $${dependency.parameter}${dependency.conditions ? ` (${dependency.conditions})` : ''}`
        : `${dependency.type === 'node' ? 'Helper objects' : 'Calculated relationships'}: ${dependency.name}`;

// Choose the newest completed attempt first. Never search backward for a more
// flattering matching result. A running attempt is shown separately at run level.
export function latestCompleted(workspace: Workspace): RunRecord | undefined {
    const completed = (record: RunRecord) => ['success', 'failed', 'uncertain'].includes(record.state);
    const records = [...(workspace.status.history ?? [])];
    if (completed(workspace.status)) records.push(workspace.status);
    return records
        .filter(completed)
        .sort((first, second) => Date.parse(second.updated_at) - Date.parse(first.updated_at))[0];
}

export function reportMatches(workspace: Workspace, report?: RunRecord) {
    return (
        !!report?.execution &&
        !!workspace.plan.revision &&
        !!workspace.plan.schema_revision &&
        report.revision === workspace.plan.revision &&
        report.execution.schema_revision === workspace.plan.schema_revision
    );
}

// Follow arrows independently in each direction. For an edge, follow only
// prerequisites before its source and consumers after its target. Side branches
// and shortcuts that bypass the selected item are outside its focus path.
export function relatedQueries(dependencies: Dependency[], focused: string | null) {
    const edge = dependencies.find((dependency) => dependencyKey(dependency) === focused);
    const nodes = new Set<string>();
    const edges = new Set<string>();
    const walk = (start: string | null, upstream: boolean) => {
        const found = new Set<string>();
        const visited = new Set(start ? [start] : []);
        const pending = start ? [start] : [];
        while (pending.length) {
            const current = pending.pop();
            for (const dependency of dependencies) {
                if ((upstream ? dependency.consumer : dependency.prerequisite) !== current) continue;
                edges.add(dependencyKey(dependency));
                const next = upstream ? dependency.prerequisite : dependency.consumer;
                if (!visited.has(next)) {
                    visited.add(next);
                    found.add(next);
                    nodes.add(next);
                    pending.push(next);
                }
            }
        }
        return found;
    };
    const prerequisites = walk(edge?.prerequisite ?? focused, true);
    const consumers = walk(edge?.consumer ?? focused, false);
    if (edge) {
        prerequisites.add(edge.prerequisite);
        consumers.add(edge.consumer);
        nodes.add(edge.prerequisite);
        nodes.add(edge.consumer);
        edges.add(dependencyKey(edge));
    } else if (focused) nodes.add(focused);
    return { prerequisites, consumers, nodes, edges };
}
