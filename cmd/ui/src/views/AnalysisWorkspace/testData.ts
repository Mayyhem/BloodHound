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

import { Query, Workspace } from './types';

export function queryFixture(name: string, order: number): Query {
    const namespace = name.split('/')[0];
    return {
        name,
        namespace,
        version: 'v1.0.0',
        schema_version: 'v1.0.0',
        extension: namespace,
        extension_name: namespace,
        added_at: '2026-10-14T12:00:00Z',
        enabled: true,
        author_enabled: true,
        disabled_reason: '',
        order,
        stage: order,
        parameter: name.split('/')[1].replace('.cypher', ''),
        text: `// namespace: ${namespace}\n// version: v1.0.0\nRETURN 1`,
        inputs: [],
        outputs: [],
        processing: {},
    };
}

export function workspaceFixture(): Workspace {
    const queries = [
        'PoC_AD/users.cypher',
        'PoC_Vault/users.cypher',
        'PoC_Vault/edges.cypher',
        'PoC_Other/alone.cypher',
    ].map((name, index) => queryFixture(name, index + 1));
    queries[3].stage = 1; // The disconnected query has no prerequisites.
    const revision = 'a'.repeat(64),
        schemaRevision = 'b'.repeat(64);
    const status: Workspace['status'] = {
        state: 'success',
        event: 'collection',
        run_id: 'attempt-1',
        revision,
        updated_at: '2026-10-14T12:01:00Z',
        message: 'Results saved.',
        saved_revision: revision,
        results_uncertain: false,
        results_stale: false,
        execution: {
            schema_revision: schemaRevision,
            calculation: 'succeeded',
            save: 'succeeded',
            phase: 'complete',
            elapsed_nanoseconds: 5e9,
            proposed: {
                relationships_added: 2,
                relationships_removed: 0,
                relationships_updated: 0,
                relationships_unchanged: 1,
                helpers_changed: 0,
            },
            saved: {
                relationships_added: 2,
                relationships_removed: 0,
                relationships_updated: 0,
                relationships_unchanged: 1,
                helpers_changed: 0,
            },
            queries: queries.map((query, index) => ({
                name: query.name,
                stage: query.stage,
                state: 'succeeded',
                reason: 'Complete answer.',
                rows: index,
                elapsed_nanoseconds: 1e9,
                started_at: `2026-10-14T12:01:${String(index).padStart(2, '0')}Z`,
                finished_at: `2026-10-14T12:01:${String(index + 1).padStart(2, '0')}Z`,
                proposed_outputs: index,
                saved_outputs: index,
            })),
        },
    };
    return {
        observed_at: '2026-10-14T12:02:00Z',
        plan: {
            revision,
            schema_revision: schemaRevision,
            name: 'Example rules',
            queries,
            extensions: [
                { name: 'ad', namespace: 'PoC_AD', display_name: 'Active Directory' },
                { name: 'vault', namespace: 'PoC_Vault', display_name: 'Vault' },
                { name: 'other', namespace: 'PoC_Other', display_name: 'Other' },
            ],
        },
        dependencies: [
            {
                prerequisite: queries[0].name,
                consumer: queries[1].name,
                type: 'query',
                name: queries[0].name,
                parameter: 'directory_users',
            },
            { prerequisite: queries[1].name, consumer: queries[2].name, type: 'node', name: 'PoC_Vault_Helper' },
            { prerequisite: queries[0].name, consumer: queries[2].name, type: 'relationship', name: 'PoC_AD_Link' },
        ],
        status,
    };
}

// A partly saved analysis that took two attempts: the run BloodHound
// triggered, where the first step failed and the rest saved, then the
// automatic retry a minute later, which reran only the failed step (and
// failed again) and reported every other step as up to date.
export function retriedAnalysisFixture(): Workspace {
    const workspace = workspaceFixture();
    const base = workspace.status;
    const execution = base.execution!;
    const [failing, ...others] = execution.queries;
    const failure = {
        query: failing.name,
        phase: 'check-answers',
        message: 'The name property is empty (null) for 5 helper objects.',
    };
    const first: Workspace['status'] = {
        ...base,
        state: 'partial',
        event: '2026-10-14T12:00:00Z',
        run_id: 'attempt-1',
        updated_at: '2026-10-14T12:01:00Z',
        message:
            'Finished with 1 failure: 3 steps saved; 1 kept their previous results. 2 connections added, 0 removed, 0 updated. The set-aside steps will be tried once more in about a minute.',
        history: undefined,
        execution: {
            ...execution,
            calculation: 'partial',
            failure,
            failures: [failure],
            queries: [
                { ...failing, state: 'failed', reason: 'The rules are new or changed.', rows: 25 },
                ...others.map((query) => ({ ...query, reason: 'The rules are new or changed.' })),
            ],
        },
    };
    const retry: Workspace['status'] = {
        ...first,
        run_id: 'attempt-2',
        updated_at: '2026-10-14T12:02:00Z',
        message:
            'Finished with 1 failure: 0 steps saved; 1 kept their previous results. 0 connections added, 0 removed, 0 updated. Automatic retries have stopped; the set-aside steps run again after the next BloodHound analysis (Run Analysis or a data upload).',
        execution: {
            ...first.execution!,
            elapsed_nanoseconds: 1e9,
            queries: [
                {
                    ...failing,
                    state: 'failed',
                    rows: 25,
                    reason: 'It ran in an earlier attempt of this run and failed, so it was set aside and kept its previous results.',
                    started_at: '2026-10-14T12:02:00Z',
                    finished_at: '2026-10-14T12:02:01Z',
                },
                ...others.map((query) => ({
                    name: query.name,
                    stage: query.stage,
                    state: 'up-to-date',
                    reason: 'No declared input or connected calculation was affected by the confirmed changes.',
                    elapsed_nanoseconds: 0,
                })),
            ],
        },
    };
    workspace.status = { ...retry, history: [first, retry] };
    return workspace;
}
