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

// These are the read-only fields returned by the analysis workspace API.
export interface Query {
    name: string;
    namespace: string;
    version: string;
    schema_version: string;
    extension_versions?: string;
    extension: string;
    extension_name: string;
    added_at: string;
    enabled: boolean;
    author_enabled: boolean;
    disabled_reason: string;
    order: number;
    stage: number;
    parameter: string;
    text: string;
    input_warning?: string;
    inputs: { type: string; name: string }[];
    outputs: {
        type: string;
        kind: string;
        producers?: string[];
        id_prefix?: string;
        endpoints?: { source_kind: string; target_kind: string }[];
        existing_edge_policy?: string;
    }[];
    processing: {
        reachability?: {
            direction: string;
            relationships: string[];
            include_start: boolean;
            expand_results: boolean;
            reject_cycles: boolean;
            node_kinds?: string[];
        };
        group_overlap?: { memberships: string };
        batch_dependency?: string;
    };
}

export interface Dependency {
    prerequisite: string;
    consumer: string;
    type: 'query' | 'node' | 'relationship';
    name: string;
    parameter?: string;
    conditions?: string;
}

export interface QueryOutcome {
    name: string;
    stage: number;
    state: string;
    reason: string;
    elapsed_nanoseconds: number;
    // Clock times around the step's read, absent for a step that never
    // started. Compared across steps they show which ones ran together.
    started_at?: string;
    finished_at?: string;
    rows?: number;
    proposed_outputs?: number;
    saved_outputs?: number;
}

export interface ChangesCount {
    relationships_added: number;
    relationships_removed: number;
    relationships_updated: number;
    relationships_unchanged: number;
    helpers_changed: number;
}

export interface Execution {
    schema_revision?: string;
    calculation: string;
    save: string;
    phase: string;
    elapsed_nanoseconds: number;
    queries: QueryOutcome[];
    proposed: ChangesCount;
    saved?: ChangesCount;
    cleanup_pending?: boolean;
    // The first failure. When failing steps were set aside and the rest was
    // saved (calculation "partial"), `failures` lists every failure.
    failure?: Failure;
    failures?: Failure[];
}

export interface Failure {
    query?: string;
    // The connection type being saved when the failure happened, if any.
    relationship?: string;
    stage?: number;
    phase: string;
    message: string;
}

export interface RunRecord {
    state: string;
    event: string;
    run_id?: string;
    revision: string;
    backend?: string;
    message: string;
    updated_at: string;
    execution?: Execution;
}

export interface Workspace {
    observed_at: string;
    plan: {
        revision: string;
        schema_revision?: string;
        name: string;
        queries: Query[];
        extensions: { namespace: string; name: string; display_name: string; environment_kind?: string }[];
    };
    installed_extensions?: { namespace: string; name: string; display_name: string; version?: string }[];
    dependencies: Dependency[];
    status: RunRecord & {
        saved_revision?: string;
        results_uncertain: boolean;
        results_stale: boolean;
        history?: RunRecord[];
    };
}

export interface RuleChange {
    base_revision: string;
    schema_revision?: string;
    draft_revision?: string;
    confirm_revision?: string;
    files?: Record<string, string>;
    overwrite?: string[];
    remove?: string[];
    disable?: string[];
    enable?: string[];
}
export interface Draft {
    base_revision: string;
    revision: string;
    files: Record<string, string>;
    ready: boolean;
    validation_error?: string;
}
export interface Review {
    base_revision: string;
    revision: string;
    removed: string[];
    overwritten: string[];
    retired_relationships: string[];
    recalculated_relationships: string[];
    paused: string[];
    disabled: string[];
    enabled: string[];
    plan: Workspace['plan'];
    dependencies: Dependency[];
}

// Editor working copies belong to the signed-in person. They are deliberately
// separate from the shared upload draft used by rule management.
export interface EditorWorkingCopy {
    name: string;
    text: string;
    base_revision: string;
    revision: string;
}

export interface EditorWorkingCopySummary {
    name: string;
    base_revision: string;
    revision: string;
}

export interface EditorTestResult {
    test_id: string;
    rows: Record<string, unknown>[];
    total_rows: number;
    truncated: boolean;
    execution: Execution;
}
export type RuleAction = 'enable' | 'disable' | 'remove';
