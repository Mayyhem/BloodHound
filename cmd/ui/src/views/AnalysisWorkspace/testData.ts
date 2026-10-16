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
