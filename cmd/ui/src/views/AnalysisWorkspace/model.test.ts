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
    extensionTitle,
    laneTitles,
    latestCompleted,
    queryColor,
    queryColors,
    queryRoles,
    relatedQueries,
    reportMatches,
    runPhase,
    stepTimes,
} from './model';
import { workspaceFixture } from './testData';
import { Dependency } from './types';

describe('analysis plan and completed reports', () => {
    it('names extensions by display name, then namespace, and adds the namespace only when names repeat', () => {
        const workspace = workspaceFixture();
        workspace.installed_extensions = [{ namespace: 'PoC_New', name: 'new', display_name: 'New extension' }];
        expect(extensionTitle(workspace, 'PoC_AD')).toBe('Active Directory');
        // An extension installed but not yet used by an active query.
        expect(extensionTitle(workspace, 'PoC_New')).toBe('New extension');
        expect(extensionTitle(workspace, 'PoC_Unknown')).toBe('PoC_Unknown');
        expect(extensionTitle(workspace, '')).toBe('No extension');

        workspace.plan.extensions[1].display_name = 'Active Directory';
        const titles = laneTitles(workspace, ['PoC_AD', 'PoC_Vault', 'PoC_Other']);
        expect(titles.get('PoC_AD')).toEqual({ title: 'Active Directory', namespace: 'PoC_AD' });
        expect(titles.get('PoC_Vault')).toEqual({ title: 'Active Directory', namespace: 'PoC_Vault' });
        expect(titles.get('PoC_Other')).toEqual({ title: 'Other', namespace: undefined });
    });
    it('reads what each query produces from its outputs and from the queries that read its rows', () => {
        const workspace = workspaceFixture();
        const [adUsers, vaultUsers, vaultEdges] = workspace.plan.queries;
        const roles = (query: typeof adUsers) => queryRoles(query, workspace.dependencies);
        // Vault's users query reads Active Directory's rows; the other two
        // arrows pass helper objects and relationships, not rows.
        expect(roles(adUsers)).toEqual(['query']);
        expect(roles(vaultUsers)).toEqual([]);
        expect(roles(vaultEdges)).toEqual([]);
        vaultEdges.outputs = [
            { type: 'relationship', kind: 'PoC_Link' },
            { type: 'relationship', kind: 'PoC_Other_Link' },
        ];
        // Each role once, however many outputs give it.
        expect(roles(vaultEdges)).toEqual(['relationship']);
        // Always in the same order: relationship, node, query.
        adUsers.outputs = [
            { type: 'node', kind: 'PoC_Helper' },
            { type: 'relationship', kind: 'PoC_Link' },
        ];
        expect(roles(adUsers)).toEqual(['relationship', 'node', 'query']);
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
        const { graph } = buildQueryGraph(workspace);
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
        const { graph } = buildQueryGraph(workspace);
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
        // A step set aside after another step failed kept its old results.
        expect(queryColor(query, 'set-aside')).toBe(queryColors.blocked.color);
        expect(queryColors.disabled.color).toBe('#9e9e9e');
        expect(queryColors.failed.color).toBe('#ef5350');
    });
});

describe('analysis run status line', () => {
    const idle = { request: '', status: 'idle' };
    it('walks through requested, queued, BloodHound analysis, and extension rules while active', () => {
        const status = { ...workspaceFixture().status, updated_at: '2026-10-16T12:00:00Z' };
        const requested = Date.parse('2026-10-16T12:00:30Z');
        expect(runPhase(idle, status, requested, requested + 1000)).toMatchObject({
            active: true,
            text: 'Analysis requested. Waiting for BloodHound to start it.',
        });
        const now = Date.parse('2026-10-16T14:10:00Z');
        const queued = runPhase(
            { request: 'analysis', status: 'idle', requestedAt: '2026-10-16T14:09:30Z', requestedBy: 'init' },
            status,
            undefined,
            now
        );
        expect(queued.active).toBe(true);
        expect(queued.text).toMatch(
            /^Queued\. BloodHound requested it at startup \(.+\)\. BloodHound starts it on its next processing check, usually within a minute, when no upload or data purge is in progress\. Extension rules run right after its analysis finishes\.$/
        );
        expect(
            runPhase({ request: '', status: 'purging', since: '2026-10-16T14:08:00Z' }, status, undefined, now).text
        ).toBe(
            'BloodHound has been purging deleted data for 2 minutes. Analysis, extension rules, and editor tests wait until it finishes.'
        );
        // An analysis queued behind a long upload says what it waits for, for how long, and warns.
        const slow = runPhase(
            {
                request: 'analysis',
                status: 'ingesting',
                since: '2026-10-16T13:45:43Z',
                requestedBy: 'init',
                requestedAt: '2026-10-16T13:45:43Z',
                lastAnalysisAt: '2026-10-16T13:38:23Z',
            },
            status,
            undefined,
            now
        );
        expect(slow).toMatchObject({ active: true, severity: 'warning', slow: true });
        expect(slow.text).toMatch(
            /^BloodHound has been ingesting uploaded data for 24 minutes\. An analysis is queued after it\. BloodHound requested it at startup/
        );
        expect(slow.text).toContain('Analysis, extension rules, and editor tests wait until it finishes.');
        expect(slow.text).toContain('This is taking much longer than usual');
        expect(slow.text).toContain('The last BloodHound analysis finished at');
        expect(runPhase(idle, { ...status, state: 'running' })).toMatchObject({
            active: true,
            text: 'Running extension rules…',
        });
        // A request that BloodHound never picked up stops spinning after two minutes.
        expect(runPhase(idle, status, requested, requested + 121000).active).toBe(false);
    });
    it('reports how the last run ended, naming the failed step', () => {
        const status = workspaceFixture().status;
        expect(runPhase(idle, status)).toMatchObject({ active: false, severity: 'success' });
        const failed = runPhase(idle, {
            ...status,
            state: 'failed',
            message: 'The name property is empty (null) for 5 helper objects.',
            execution: {
                ...status.execution!,
                failure: { query: 'PoC/domain_helpers.cypher', phase: 'check-answers', message: '' },
            },
        });
        expect(failed).toMatchObject({ active: false, severity: 'error', failedQuery: 'PoC/domain_helpers.cypher' });
        expect(failed.text).toContain('The name property is empty (null) for 5 helper objects.');
        expect(runPhase(idle, { ...status, state: 'waiting', message: 'New data arrived.' })).toMatchObject({
            active: false,
            severity: 'info',
        });
    });
    it('shows a partly saved run in orange, naming the first failed step', () => {
        const status = workspaceFixture().status;
        const partial = runPhase(idle, {
            ...status,
            state: 'partial',
            message: 'Finished with 1 failure: 24 steps saved; 5 kept their previous results.',
            execution: {
                ...status.execution!,
                calculation: 'partial',
                failure: { query: 'PoC/domain_helpers.cypher', phase: 'check-answers', message: '' },
            },
        });
        expect(partial).toMatchObject({
            active: false,
            severity: 'warning',
            failedQuery: 'PoC/domain_helpers.cypher',
        });
        expect(partial.text).toContain('24 steps saved; 5 kept their previous results.');
    });
});

describe('step run times', () => {
    it('counts a success only from a run that saved its results', () => {
        const workspace = workspaceFixture();
        const saved = { ...workspace.status, run_id: 'saved', updated_at: '2026-10-14T10:00:00Z' };
        const failedRun = {
            ...workspace.status,
            run_id: 'failed',
            state: 'failed',
            updated_at: '2026-10-14T13:00:00Z',
            execution: { ...workspace.status.execution!, save: 'not-started' },
        };
        workspace.status = { ...workspace.status, ...failedRun, history: [failedRun, saved] };
        const times = stepTimes(workspace, 'PoC_AD/users.cypher');
        // It ran (and calculated) in the failed run, but that run saved nothing.
        expect(times.lastRun).toEqual({ at: '2026-10-14T13:00:00Z', state: 'succeeded' });
        expect(times.lastSuccess).toBe('2026-10-14T10:00:00Z');
        expect(stepTimes(workspace, 'PoC_Other/missing.cypher')).toEqual({
            lastRun: undefined,
            lastSuccess: undefined,
        });
    });
    it('counts a success from a partly saved run, but not a set-aside step as having run', () => {
        const workspace = workspaceFixture();
        const execution = workspace.status.execution!;
        const partial = {
            ...workspace.status,
            run_id: 'partial',
            state: 'partial',
            updated_at: '2026-10-15T09:00:00Z',
            execution: {
                ...execution,
                calculation: 'partial',
                queries: execution.queries.map((query, index) =>
                    index === 0 ? query : { ...query, state: 'set-aside' }
                ),
            },
        };
        const earlier = { ...workspace.status, run_id: 'earlier', updated_at: '2026-10-14T10:00:00Z' };
        workspace.status = { ...workspace.status, ...partial, history: [partial, earlier] };
        const [first, second] = execution.queries;
        expect(stepTimes(workspace, first.name).lastSuccess).toBe('2026-10-15T09:00:00Z');
        expect(stepTimes(workspace, second.name)).toEqual({
            lastRun: { at: '2026-10-14T10:00:00Z', state: 'succeeded' },
            lastSuccess: '2026-10-14T10:00:00Z',
        });
    });
});
