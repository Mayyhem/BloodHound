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

import { Dependency, Query, QueryOutcome, RunRecord, Workspace } from './types';

// The name to show for an extension: its display name, or its namespace when
// it has none. A query with no namespace belongs to "No extension". The plan
// lists extensions that have active queries; the installed list also covers an
// extension whose first file is still being written in the editor.
export function extensionTitle(workspace: Workspace, namespace: string) {
    if (!namespace) return 'No extension';
    const extension = [...workspace.plan.extensions, ...(workspace.installed_extensions ?? [])].find(
        (entry) => entry.namespace === namespace
    );
    return extension?.display_name || namespace;
}

// Titles for the graph's extension lanes. When two extensions share a display
// name, `namespace` is filled in so the lane can show it in small text.
export function laneTitles(workspace: Workspace, namespaces: string[]) {
    const titles = new Map(namespaces.map((namespace) => [namespace, extensionTitle(workspace, namespace)]));
    const shared = (title: string) => [...titles.values()].filter((other) => other === title).length > 1;
    return new Map(
        [...titles].map(([namespace, title]) => [
            namespace,
            { title, namespace: shared(title) && namespace ? namespace : undefined },
        ])
    );
}

// What a query produces. A query can do more than one of these; the graph
// draws one icon for each.
//   relationship: it makes relationships in the collected graph (declared in
//     the extension's schema, listed in the query's outputs)
//   node: it makes helper objects (also listed in its outputs)
//   query: another query reads its rows (that query declares
//     `depends-on: query:...` on it, which the plan lists as a dependency)
export type QueryRole = 'relationship' | 'node' | 'query';
export function queryRoles(query: Query, dependencies: Dependency[]): QueryRole[] {
    const produces: Record<QueryRole, boolean> = {
        relationship: query.outputs.some((output) => output.type === 'relationship'),
        node: query.outputs.some((output) => output.type === 'node'),
        query: dependencies.some((dependency) => dependency.type === 'query' && dependency.prerequisite === query.name),
    };
    return (['relationship', 'node', 'query'] as const).filter((role) => produces[role]);
}

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
    // Green and red are one shade paler than before so the black role icons
    // drawn on each node stay readable.
    enabled: { color: '#66bb6a', label: 'Enabled' },
    blocked: { color: '#ed8b00', label: 'Blocked or set aside' },
    failed: { color: '#ef5350', label: 'Failed' },
    disabled: { color: '#9e9e9e', label: 'Disabled' },
    focused: { color: '#1976d2', label: 'Current focus' },
};
// A query the author disabled is gray. Otherwise a query whose step failed in
// the latest completed run of these exact rules is red. A blocked query is
// orange: either a prerequisite is disabled, or the latest run set the step
// aside because a step it is tied to failed. Everything else is green.
// `outcome` is that run's state for the query; it is missing when no completed
// run matches the current rules.
export const queryColor = (query: Query, outcome?: string) =>
    !query.author_enabled
        ? queryColors.disabled.color
        : outcome === 'failed'
          ? queryColors.failed.color
          : !query.enabled || outcome === 'set-aside'
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
    state === 'waiting'
        ? 'Not reached'
        : state === 'running'
          ? 'Outcome unavailable'
          : state === 'set-aside'
            ? 'Set aside (kept its previous results)'
            : state === 'up-to-date'
              ? 'Up to date (nothing it reads changed)'
              : state ?? 'Unavailable';
export const dependencyReason = (dependency: Dependency) =>
    dependency.type === 'query'
        ? `As $${dependency.parameter}${dependency.conditions ? ` (${dependency.conditions})` : ''}`
        : `${dependency.type === 'node' ? 'Helper objects' : 'Calculated relationships'}: ${dependency.name}`;

// Choose the newest completed attempt first. Never search backward for a more
// flattering matching result. A running attempt is shown separately at run level.
export function latestCompleted(workspace: Workspace): RunRecord | undefined {
    const completed = (record: RunRecord) => ['success', 'partial', 'failed', 'uncertain'].includes(record.state);
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

// One analysis can take several attempts: the run BloodHound triggered, then
// the automatic retries of the steps that were set aside after a failure.
// Every attempt of one analysis reports the same event time and rule
// revision, which tells it apart from an unrelated earlier run. The attempts
// come back oldest first, so the first one is the run that did the main work.
export function analysisAttempts(status: Workspace['status']): RunRecord[] {
    const completed = (record: RunRecord) => ['success', 'partial', 'failed', 'uncertain'].includes(record.state);
    const seen = new Set<string>();
    const records = [status, ...(status.history ?? [])].filter((record) => {
        const key = `${record.run_id}|${record.updated_at}`;
        if (seen.has(key) || !completed(record)) return false;
        seen.add(key);
        return true;
    });
    const latest = records.sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at))[0];
    if (!latest) return [];
    return records
        .filter((record) => record.event === latest.event && record.revision === latest.revision)
        .sort((left, right) => Date.parse(left.updated_at) - Date.parse(right.updated_at));
}
// A step's outcome, and the attempt it came from. A retry reruns only the
// steps that were set aside, and reports every other step as "up to date",
// which says nothing new about them. So each step shows its outcome from the
// newest attempt that actually decided something about it; a step that every
// attempt found up to date shows the newest. The attempt number counts from 1
// at the run BloodHound triggered.
export interface AttemptOutcome {
    outcome: QueryOutcome;
    attempt: RunRecord;
    attemptNumber: number;
    attempts: number;
}

export function combinedOutcomes(status: Workspace['status']): Map<string, AttemptOutcome> {
    const attempts = analysisAttempts(status);
    const result = new Map<string, AttemptOutcome>();
    const names = new Set(attempts.flatMap((attempt) => attempt.execution?.queries.map((query) => query.name) ?? []));
    for (const name of names) {
        let chosen: AttemptOutcome | undefined;
        for (let index = attempts.length - 1; index >= 0; index--) {
            const outcome = attempts[index].execution?.queries.find((query) => query.name === name);
            if (!outcome) continue;
            const entry = { outcome, attempt: attempts[index], attemptNumber: index + 1, attempts: attempts.length };
            chosen ??= entry;
            if (outcome.state !== 'up-to-date') {
                chosen = entry;
                break;
            }
        }
        if (chosen) result.set(name, chosen);
    }
    return result;
}

// The outcomes of the latest attempt alone, in the same shape, for the
// "show attempts separately" view.
export function latestAttemptOutcomes(status: Workspace['status']): Map<string, AttemptOutcome> {
    const attempts = analysisAttempts(status);
    const latest = attempts[attempts.length - 1];
    return new Map(
        (latest?.execution?.queries ?? []).map((outcome) => [
            outcome.name,
            { outcome, attempt: latest, attemptNumber: attempts.length, attempts: attempts.length },
        ])
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
    const attempts = analysisAttempts(status);
    switch (status.state) {
        case 'success':
            return { active: false, severity: 'success', text: `Last run succeeded · ${when}. ${status.message}` };
        case 'partial':
            // Some steps failed and were set aside; everything else was saved.
            // Describe the whole analysis, not just the latest retry, which
            // reruns only the set-aside steps and would read as "0 steps saved".
            return {
                active: false,
                severity: 'warning',
                text: analysisSummary(status, attempts),
                failedQuery: status.execution?.failure?.query,
            };
        case 'failed':
            return {
                active: false,
                severity: 'error',
                text:
                    attempts.length > 1
                        ? `Last run failed on each of its ${attempts.length} attempts · ${when}. ${status.message}`
                        : `Last run failed · ${when}. ${status.message}`,
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

// analysisSummary writes the partly saved status line from every attempt of
// the analysis: when it started, how many attempts it took, what was saved,
// what failed, and what the engine says about further retries.
export function analysisSummary(status: Workspace['status'], attempts = analysisAttempts(status)): string {
    const first = attempts[0] ?? status;
    const latest = attempts[attempts.length - 1] ?? status;
    const outcomes = [...combinedOutcomes(status).values()];
    const saved = outcomes.filter(
        (entry) => entry.outcome.state === 'succeeded' && entry.attempt.execution?.save === 'succeeded'
    ).length;
    const failed = outcomes.filter((entry) => entry.outcome.state === 'failed').map((entry) => entry.outcome.name);
    const setAside = outcomes.filter((entry) => entry.outcome.state === 'set-aside').length;
    const retries =
        attempts.length > 1
            ? ` The ${attempts.length === 2 ? 'retry' : `${attempts.length - 1} retries`} at ${observedTime(latest.updated_at)} reran only the set-aside steps, and ${
                  failed.length ? 'they failed again' : 'they succeeded'
              }.`
            : '';
    // The engine's own message ends with whether another automatic try is
    // coming; keep that sentence and drop the per-attempt counts before it.
    const retryNote = (latest.message.match(
        /(?:Automatic retries have stopped|The set-aside steps will be tried once more|It will be tried once more)[^]*$/
    ) ?? [''])[0];
    const kept = [
        failed.length ? `${failed.length} failed (${failed.join(', ')})` : '',
        setAside ? `${setAside} set aside` : '',
    ]
        .filter(Boolean)
        .join(' and ');
    return `Run started ${observedTime(first.updated_at)}${attempts.length > 1 ? `, ${attempts.length} attempts` : ''}: ${saved} ${saved === 1 ? 'step' : 'steps'} saved${kept ? `; ${kept} kept their previous results` : ''}.${retries}${retryNote ? ` ${retryNote}` : ''}`;
}

export interface StepTimes {
    // The latest run in which this step actually ran, and how it ended.
    lastRun?: { at: string; state: string };
    // The latest run in which this step succeeded and the run's results were saved.
    lastSuccess?: string;
}

// stepTimes looks through the latest report and the up to 20 kept before it.
// A step's calculation can succeed in a run that fails elsewhere; that result
// is not saved, so "last success" counts only runs that saved their results:
// fully successful runs, and partly saved runs (where every step marked
// succeeded was saved). A set-aside step did not run, so it is not "last ran".
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
    const succeeded = records.find(
        (record) => ['success', 'partial'].includes(record.state) && stateIn(record) === 'succeeded'
    );
    return {
        lastRun: ran ? { at: ran.updated_at, state: stateIn(ran)! } : undefined,
        lastSuccess: succeeded?.updated_at,
    };
}
