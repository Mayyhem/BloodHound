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

import { apiClient } from 'bh-shared-ui';
import {
    Dependency,
    Draft,
    EditorTestResult,
    EditorWorkingCopy,
    EditorWorkingCopySummary,
    Review,
    RuleChange,
    Workspace,
} from './types';

export async function readWorkspace(signal?: AbortSignal): Promise<Workspace> {
    const response = await apiClient.baseClient.get<{ data: Workspace }>('/api/v2/analysis-workspace', { signal });
    return response.data.data;
}

export async function downloadQuery(workspace: Workspace, name: string) {
    const response = await apiClient.baseClient.get<string>('/api/v2/analysis-workspace/query', {
        params: { query: name, revision: workspace.plan.revision, schema_revision: workspace.plan.schema_revision },
        responseType: 'text',
    });
    const address = URL.createObjectURL(new Blob([response.data], { type: 'text/plain;charset=utf-8' }));
    const link = document.createElement('a');
    link.href = address;
    link.download = name.split('/').pop() ?? 'analysis.cypher';
    link.click();
    URL.revokeObjectURL(address);
}

const managementPath = '/api/v2/analysis-workspace';
export async function readDraft(): Promise<Draft> {
    return (await apiClient.baseClient.get(`${managementPath}/draft`)).data.data;
}
export async function saveDraft(change: RuleChange): Promise<Draft> {
    return (await apiClient.baseClient.put(`${managementPath}/draft`, change)).data.data;
}
export async function discardDraft(revision: string) {
    await apiClient.baseClient.delete(`${managementPath}/draft`, {
        data: { base_revision: '', draft_revision: revision },
    });
}
export async function previewChange(change: RuleChange): Promise<Review> {
    return (await apiClient.baseClient.post(`${managementPath}/preview`, change)).data.data;
}
export async function applyChange(change: RuleChange): Promise<{ revision: string }> {
    return (await apiClient.baseClient.put(`${managementPath}/rules`, change)).data.data;
}
export function downloadText(name: string, text: string) {
    const address = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
    const link = document.createElement('a');
    link.href = address;
    link.download = name.split('/').pop() ?? 'analysis.cypher';
    link.click();
    URL.revokeObjectURL(address);
}

const editorPath = `${managementPath}/editor`;

export async function listEditorWorkingCopies(signal?: AbortSignal): Promise<EditorWorkingCopySummary[]> {
    return (
        await apiClient.baseClient.get<{ data: EditorWorkingCopySummary[] }>(`${managementPath}/working-copies`, {
            signal,
        })
    ).data.data;
}

export async function readEditorWorkingCopy(name: string, signal?: AbortSignal): Promise<EditorWorkingCopy> {
    return (
        await apiClient.baseClient.get<{ data: EditorWorkingCopy }>(`${managementPath}/working-copies`, {
            params: { name },
            signal,
        })
    ).data.data;
}

export async function saveEditorWorkingCopy(
    workingCopy: EditorWorkingCopy,
    signal?: AbortSignal
): Promise<EditorWorkingCopy> {
    return (
        await apiClient.baseClient.put<{ data: EditorWorkingCopy }>(`${managementPath}/working-copies`, workingCopy, {
            signal,
        })
    ).data.data;
}

export async function deleteEditorWorkingCopy(name: string, revision: string) {
    await apiClient.baseClient.delete(`${managementPath}/working-copies`, { data: { name, revision } });
}

export interface EditorTestRequest {
    name: string;
    text: string;
    base_revision: string;
    schema_revision?: string;
}

export async function testEditorFile(request: EditorTestRequest, signal: AbortSignal): Promise<EditorTestResult> {
    return (await apiClient.baseClient.post<{ data: EditorTestResult }>(`${editorPath}/test`, request, { signal })).data
        .data;
}

export interface EditorPlanPreview {
    // Declarations the edited file never uses, such as node:Domain.
    unused?: string[];
    plan: Workspace['plan'];
    dependencies: Dependency[];
}

export async function previewEditorPlan(request: EditorTestRequest, signal: AbortSignal): Promise<EditorPlanPreview> {
    return (await apiClient.baseClient.post<{ data: EditorPlanPreview }>(`${editorPath}/plan`, request, { signal }))
        .data.data;
}

export async function reviewEditorTest(testID: string, signal?: AbortSignal): Promise<Review> {
    return (await apiClient.baseClient.post<{ data: Review }>(`${editorPath}/review`, { test_id: testID }, { signal }))
        .data.data;
}

export async function activateEditorTest(
    testID: string,
    confirmRevision: string,
    signal?: AbortSignal
): Promise<{ revision: string }> {
    return (
        await apiClient.baseClient.put<{ data: { revision: string } }>(
            `${editorPath}/activate`,
            {
                test_id: testID,
                confirm_revision: confirmRevision,
            },
            { signal }
        )
    ).data.data;
}
