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
    // Green and red are one shade paler than before so the black extension
    // number drawn on each node stays readable.
    enabled: { color: '#66bb6a', label: 'Enabled' },
    blocked: { color: '#ed8b00', label: 'Blocked' },
    failed: { color: '#ef5350', label: 'Failed' },
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

// BloodHound's own analysis queue and phase, read from its existing status endpoints.
export interface NativeStatus {
    request: string;
    status: string;
    // When BloodHound's current phase began (its datapipe status time).
    since?: string;
    // When the queued analysis was requested, and by what ("init" = startup).
    requestedAt?: string;
    requestedBy?: string;
    // When BloodHound's last analysis finished.
    lastAnalysisAt?: string;
}

// A BloodHound phase running longer than this gets a warning: analysis and
// editor tests are waiting behind it.
export const slowPhaseMinutes = 5;

export interface RunPhase {
    // Still working: the page refreshes itself until this becomes false.
    active: boolean;
    severity: 'info' | 'success' | 'warning' | 'error';
    text: string;
    // The step that failed, so the status line can link to it.
    failedQuery?: string;
    // BloodHound has been busy a long time, usually with a slow upload.
    // The status line then links to BloodHound's File Ingest page.
    slow?: boolean;
}

// "12 minutes", "1 minute", or "less than a minute".
function elapsed(since: string | undefined, now: number) {
    const minutes = since ? Math.floor((now - Date.parse(since)) / 60000) : NaN;
    if (!Number.isFinite(minutes) || minutes < 1) return 'less than a minute';
    return minutes === 1 ? '1 minute' : `${minutes} minutes`;
}

// Describes a queued analysis: when it was requested and by what.
function queuedRequest(native: NativeStatus) {
    const when = native.requestedAt ? observedTime(native.requestedAt) : 'recently';
    return native.requestedBy === 'init'
        ? `BloodHound requested it at startup (${when})`
        : `It was requested at ${when}`;
}

const nativePhaseWords: Record<string, string> = {
    analyzing: 'analyzing collected data',
    ingesting: 'ingesting uploaded data',
    purging: 'purging deleted data',
    pruning: 'pruning old data',
    starting: 'starting',
    optimizing: 'optimizing the database',
};

// runPhase answers "is analysis running, and how did the last run end?" in one
// sentence. It combines BloodHound's queue and phase with the extension report.
// A run goes: requested → queued → BloodHound analysis → extension rules → finished.
export function runPhase(
    native: NativeStatus | undefined,
    status: Workspace['status'],
    requestedAt?: number,
    now = Date.now()
): RunPhase {
    const reportTime = Date.parse(status.updated_at);
    if (status.state === 'running') return { active: true, severity: 'info', text: 'Running extension rules…' };
    // BloodHound's current phase comes first: a queued analysis waits until an
    // upload or purge in progress finishes, and saying so explains the wait.
    if (native && native.status !== 'idle') {
        const slow = !!native.since && now - Date.parse(native.since) > slowPhaseMinutes * 60000;
        const phase = nativePhaseWords[native.status] ?? native.status;
        const queued =
            native.request === 'analysis' ? ` An analysis is queued after it. ${queuedRequest(native)}.` : '';
        const waiting =
            native.status === 'analyzing'
                ? ' Extension rules run when it finishes.'
                : ' Analysis, extension rules, and editor tests wait until it finishes.';
        const warning = slow
            ? ` This is taking much longer than usual; a very large or slow upload can hold everything for a long time. Check its progress in File Ingest.${
                  native.lastAnalysisAt
                      ? ` The last BloodHound analysis finished at ${observedTime(native.lastAnalysisAt)}.`
                      : ''
              }`
            : '';
        return {
            active: true,
            severity: slow ? 'warning' : 'info',
            slow,
            text: `BloodHound has been ${phase} for ${elapsed(native.since, now)}.${queued}${waiting}${warning}`,
        };
    }
    if (native?.request === 'analysis')
        return {
            active: true,
            severity: 'info',
            text: `Queued. ${queuedRequest(native)}. BloodHound starts it on its next processing check, usually within a minute, when no upload or data purge is in progress. Extension rules run right after its analysis finishes.`,
        };
    if (native?.request === 'deletion') return { active: true, severity: 'info', text: 'Data deletion is queued.' };
    // Just clicked Run Analysis, but BloodHound has not picked it up yet. Give
    // up waiting after two minutes so a lost request cannot spin forever.
    if (requestedAt && now - requestedAt < 120000 && !(reportTime > requestedAt))
        return { active: true, severity: 'info', text: 'Analysis requested. Waiting for BloodHound to start it.' };
    const when = observedTime(status.updated_at);
    switch (status.state) {
        case 'success':
            return { active: false, severity: 'success', text: `Last run succeeded · ${when}. ${status.message}` };
        case 'failed':
            return {
                active: false,
                severity: 'error',
                text: `Last run failed · ${when}. ${status.message}`,
                failedQuery: status.execution?.failure?.query,
            };
        case 'uncertain':
            return {
                active: false,
                severity: 'warning',
                text: `Last run's save is unconfirmed · ${when}. ${status.message}`,
            };
        case 'waiting':
            return { active: false, severity: 'info', text: `Waiting · ${when}. ${status.message}` };
        case 'no-rules':
            return { active: false, severity: 'info', text: status.message };
        default:
            return { active: false, severity: 'info', text: 'No extension run has been reported yet.' };
    }
}

export interface StepTimes {
    // The latest run in which this step actually ran, and how it ended.
    lastRun?: { at: string; state: string };
    // The latest run in which this step succeeded and the run's results were saved.
    lastSuccess?: string;
}

// stepTimes looks through the latest report and the up to 20 kept before it.
// A step's calculation can succeed in a run that fails elsewhere; that result
// is not saved, so "last success" counts only runs that saved their results.
export function stepTimes(workspace: Workspace, name: string): StepTimes {
    const seen = new Set<string>();
    const records = [workspace.status, ...(workspace.status.history ?? [])]
        .filter((record) => {
            const key = `${record.run_id}|${record.updated_at}`;
            if (seen.has(key)) return false;
            seen.add(key);
            return true;
        })
        .sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at));
    const stateIn = (record: RunRecord) => record.execution?.queries.find((query) => query.name === name)?.state;
    const ran = records.find((record) => ['succeeded', 'failed'].includes(stateIn(record) ?? ''));
    const succeeded = records.find((record) => record.state === 'success' && stateIn(record) === 'succeeded');
    return {
        lastRun: ran ? { at: ran.updated_at, state: stateIn(ran)! } : undefined,
        lastSuccess: succeeded?.updated_at,
    };
}
