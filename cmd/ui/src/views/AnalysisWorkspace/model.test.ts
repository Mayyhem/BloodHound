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

import { buildQueryGraph } from './graph';
import {
    availability,
    completedState,
    dependencyKey,
    dependencyReason,
    latestCompleted,
    numberedExtensions,
    queryColor,
    queryColors,
    relatedQueries,
    reportMatches,
} from './model';
import { workspaceFixture } from './testData';
import { Dependency } from './types';

describe('analysis plan and completed reports', () => {
    it('numbers all extensions consistently regardless of query execution order', () => {
        const queries = workspaceFixture().plan.queries;
        expect(numberedExtensions(queries)).toEqual([
            { namespace: 'PoC_AD', number: 1 },
            { namespace: 'PoC_Other', number: 2 },
            { namespace: 'PoC_Vault', number: 3 },
        ]);
        expect(numberedExtensions([...queries].reverse())).toEqual(numberedExtensions(queries));
    });
    it('keeps only paths through the selected query or edge, without following side branches or shortcuts', () => {
        const connections = [
            ['start', 'a'],
            ['a', 'b'],
            ['b', 'c'],
            ['c', 'end'],
            ['a', 'side'],
            ['other', 'c'],
            ['a', 'c'],
        ];
        const dependencies: Dependency[] = connections.map(([prerequisite, consumer]) => ({
            prerequisite,
            consumer,
            type: 'query',
            name: prerequisite,
        }));
        for (const focused of ['b', dependencyKey(dependencies[1])]) {
            const path = relatedQueries(dependencies, focused);
            expect(path.nodes).toEqual(new Set(['start', 'a', 'b', 'c', 'end']));
            expect(path.edges).toEqual(new Set(dependencies.slice(0, 4).map(dependencyKey)));
        }
        expect(relatedQueries(dependencies, null).nodes.size).toBe(0);
        expect(relatedQueries(dependencies, null).edges.size).toBe(0);
    });
    it('does not present unfinished states in a completed report as live progress', () => {
        expect(completedState('waiting')).toBe('Not reached');
        expect(completedState('running')).toBe('Outcome unavailable');
        expect(completedState('succeeded')).toBe('succeeded');
    });
    it('draws shared prerequisites once and keeps disconnected queries', () => {
        const workspace = workspaceFixture();
        const graph = buildQueryGraph(workspace);
        expect(graph.order).toBe(4);
        expect(graph.size).toBe(3);
        expect(graph.inDegree('PoC_Other/alone.cypher')).toBe(0);
        expect(graph.getNodeAttribute('PoC_AD/users.cypher', 'x')).toBeLessThan(
            graph.getNodeAttribute('PoC_Vault/edges.cypher', 'x')
        );
        // Arrows are unlabeled; the details panel explains each one instead.
        expect(graph.edges().map((edge) => graph.getEdgeAttribute(edge, 'label'))).toEqual([
            undefined,
            undefined,
            undefined,
        ]);
        expect(workspace.dependencies.map(dependencyReason)).toEqual(
            expect.arrayContaining([
                'As $directory_users',
                'Helper objects: PoC_Vault_Helper',
                'Calculated relationships: PoC_AD_Link',
            ])
        );
        const related = relatedQueries(workspace.dependencies, 'PoC_Vault/users.cypher');
        expect([...related.prerequisites]).toEqual(['PoC_AD/users.cypher']);
        expect([...related.consumers]).toEqual(['PoC_Vault/edges.cypher']);
    });
    it('does not invent ordering between shared output producers', () => {
        const workspace = workspaceFixture();
        workspace.dependencies = [];
        workspace.plan.queries[0].outputs = [
            { type: 'relationship', kind: 'PoC_Link', producers: ['PoC_AD/users.cypher', 'PoC_Vault/users.cypher'] },
        ];
        const graph = buildQueryGraph(workspace);
        expect(graph.size).toBe(0);
        const positions = graph.nodes().map((name) => {
            const { x, y } = graph.getNodeAttributes(name);
            return `${x},${y}`;
        });
        expect(new Set(positions).size).toBe(workspace.plan.queries.length);
    });
    it('requires both rule and schema revisions before displaying outcomes', () => {
        const workspace = workspaceFixture();
        expect(reportMatches(workspace, latestCompleted(workspace))).toBe(true);
        workspace.plan.schema_revision = 'c'.repeat(64);
        expect(reportMatches(workspace, latestCompleted(workspace))).toBe(false);
        workspace.plan.schema_revision = workspace.status.execution!.schema_revision;
        workspace.plan.revision = 'c'.repeat(64);
        expect(reportMatches(workspace, latestCompleted(workspace))).toBe(false);
        delete workspace.status.execution;
        expect(reportMatches(workspace, latestCompleted(workspace))).toBe(false);
    });
    it('chooses the latest completed attempt before checking whether it matches', () => {
        const workspace = workspaceFixture();
        const matching = structuredClone(workspace.status);
        workspace.status.history = [
            matching,
            {
                ...matching,
                run_id: 'attempt-2',
                state: 'failed',
                revision: 'c'.repeat(64),
                updated_at: '2026-10-14T12:02:00Z',
            },
        ];
        workspace.status.state = 'running';
        workspace.status.updated_at = '2026-10-14T12:03:00Z';
        expect(latestCompleted(workspace)?.run_id).toBe('attempt-2');
        expect(reportMatches(workspace, latestCompleted(workspace))).toBe(false);
    });
    it('does not treat running or unavailable reports as completed outcomes', () => {
        const workspace = workspaceFixture();
        workspace.status.state = 'running';
        expect(latestCompleted(workspace)).toBeUndefined();
        workspace.status.state = 'unavailable';
        expect(latestCompleted(workspace)).toBeUndefined();
    });
    it('distinguishes an author choice from a blocked enabled query', () => {
        const query = workspaceFixture().plan.queries[0];
        query.enabled = false;
        expect(availability(query)).toBe('Enabled · blocked');
        query.author_enabled = false;
        expect(availability(query)).toBe('Disabled by author');
    });
});

describe('graph node colors', () => {
    it('shows disabled queries gray, failed steps red, blocked queries orange, and the rest green', () => {
        const [query] = workspaceFixture().plan.queries;
        expect(queryColor(query)).toBe(queryColors.enabled.color);
        expect(queryColor(query, 'succeeded')).toBe(queryColors.enabled.color);
        expect(queryColor(query, 'failed')).toBe(queryColors.failed.color);
        expect(queryColor({ ...query, enabled: false })).toBe(queryColors.blocked.color);
        expect(queryColor({ ...query, enabled: false }, 'failed')).toBe(queryColors.failed.color);
        expect(queryColor({ ...query, author_enabled: false })).toBe(queryColors.disabled.color);
        expect(queryColor({ ...query, author_enabled: false }, 'failed')).toBe(queryColors.disabled.color);
        expect(queryColors.disabled.color).toBe('#9e9e9e');
        expect(queryColors.failed.color).toBe('#c62828');
    });
});
