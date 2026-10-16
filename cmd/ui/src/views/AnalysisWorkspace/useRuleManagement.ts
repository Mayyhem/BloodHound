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

import { isAxiosError } from 'axios';
import { useEffect, useState } from 'react';
import { applyChange, discardDraft, previewChange, readDraft, saveDraft } from './api';
import { Draft, Review, RuleAction, RuleChange, Workspace } from './types';

export function managementError(error: unknown): string {
    const status = isAxiosError(error) ? error.response?.status : undefined;
    if (status === 401 || status === 403) return 'Your account cannot make this change. Check your permissions.';
    if (status === 409) return 'Rules, schemas, or the saved draft changed. Refresh and review again.';
    if (status === 413) return 'Upload exceeds the 4 MiB limit.';
    if (status === 400)
        return 'The change is invalid. Check file declarations, prerequisites, installed schemas, and overwrite choices.';
    return 'The change could not be confirmed. Refresh rules and status before making another change.';
}

export default function useRuleManagement(workspace: Workspace | undefined, refreshWorkspace: () => Promise<boolean>) {
    const [draft, setDraft] = useState<Draft>();
    const [pending, setPending] = useState<{ change: RuleChange; review: Review }>();
    const [busy, setBusy] = useState(false);
    const [locked, setLocked] = useState(false);
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const [draftError, setDraftError] = useState('');
    const reloadDraft = async () => {
        try {
            setDraft(await readDraft());
            setDraftError('');
            return true;
        } catch {
            setDraft(undefined);
            setDraftError('The saved draft could not be read. Refresh before uploading files.');
            return false;
        }
    };
    const hasWorkspace = !!workspace;
    useEffect(() => {
        setPending(undefined);
        if (hasWorkspace) void reloadDraft();
    }, [hasWorkspace, workspace?.plan.revision, workspace?.plan.schema_revision]);

    const changeBase = (): RuleChange => ({
        base_revision: workspace?.plan.revision ?? '',
        schema_revision: workspace?.plan.schema_revision,
    });
    const perform = async (action: () => Promise<void>) => {
        if (busy || locked) return;
        setBusy(true);
        setError('');
        setMessage('');
        try {
            await action();
        } catch (failure) {
            setPending(undefined);
            setError(managementError(failure));
            // Even a lost reply may have saved files or activated the revision.
            // Require an explicit refresh before another write; never retry it.
            setLocked(true);
        } finally {
            setBusy(false);
        }
    };
    const review = async (change: RuleChange) => {
        const result = await previewChange(change);
        setPending({ change, review: result });
        setMessage('Review the proposed rules and every listed effect before activation.');
    };
    const refresh = async () => {
        if (busy) return;
        setBusy(true);
        setPending(undefined);
        setMessage('');
        const current = await refreshWorkspace();
        const saved = await reloadDraft();
        if (current && saved) {
            setLocked(false);
            setError('');
        }
        setBusy(false);
    };
    return {
        draft,
        pending,
        busy,
        locked,
        message,
        error,
        draftError,
        refresh,
        clearReview: () => {
            setPending(undefined);
            setMessage('');
        },
        reviewAction: (action: RuleAction, names: string[]) =>
            perform(async () => {
                if (
                    !workspace ||
                    !names.length ||
                    names.some((name) => !workspace.plan.queries.some((query) => query.name === name))
                )
                    return;
                await review({ ...changeBase(), [action]: names });
            }),
        upload: (files: Record<string, string>, overwrite: string[]) =>
            perform(async () => {
                if (!draft || !workspace) return;
                const saved = await saveDraft({ ...changeBase(), draft_revision: draft.revision, files, overwrite });
                setDraft(saved);
                if (saved.ready) await review({ ...changeBase(), draft_revision: saved.revision });
                else setMessage('Inactive draft saved. Correct the listed problems, then review again.');
            }),
        reviewDraft: () =>
            perform(async () => {
                if (draft?.ready) await review({ ...changeBase(), draft_revision: draft.revision });
            }),
        discard: () =>
            perform(async () => {
                if (!draft?.revision) return;
                await discardDraft(draft.revision);
                setPending(undefined);
                await reloadDraft();
                setMessage('Saved draft discarded. Active rules are unchanged.');
            }),
        activate: () =>
            perform(async () => {
                if (!pending) return;
                await applyChange({ ...pending.change, confirm_revision: pending.review.revision });
                setPending(undefined);
                let notice =
                    'Reviewed revision activated. New settings apply to the next analysis. Deleted connections are cleared after its next successful run.';
                if (pending.change.draft_revision) {
                    try {
                        await discardDraft(pending.change.draft_revision);
                    } catch {
                        notice +=
                            ' Activation succeeded, but the saved draft could not be discarded. Refresh before discarding it.';
                    }
                }
                const refreshed = await refreshWorkspace();
                await reloadDraft();
                if (!refreshed) {
                    setLocked(true);
                    notice += ' Refresh to read the current rules before another change.';
                }
                setMessage(notice);
            }),
    };
}
export type RuleManagement = ReturnType<typeof useRuleManagement>;
