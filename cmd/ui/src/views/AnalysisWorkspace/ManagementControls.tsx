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

import { Alert, Box, Button, Stack, Typography } from '@mui/material';
import { apiClient } from 'bh-shared-ui';
import { ReactNode, useState } from 'react';
import { useQuery } from 'react-query';
import { RuleManagement } from './useRuleManagement';

// The top panel's run and review controls. `children` (revisions and the
// color legend) appear just above the analysis status line.
export default function ManagementControls({
    management,
    canEdit,
    canRun,
    canReadStatus,
    onReport,
    onRefresh,
    refreshing,
    children,
}: {
    management: RuleManagement;
    canEdit: boolean;
    canRun: boolean;
    canReadStatus: boolean;
    onReport: () => void;
    onRefresh: () => void;
    refreshing: boolean;
    children?: ReactNode;
}) {
    const [starting, setStarting] = useState(false);
    const [runMessage, setRunMessage] = useState('');
    const [runUncertain, setRunUncertain] = useState(false);
    const {
        data: native,
        refetch: refreshNative,
        isFetching: checkingNative,
    } = useQuery(
        ['analysis-workspace-native-status'],
        async () => {
            const [queue, processing] = await Promise.all([
                apiClient.baseClient.get('/api/v2/analysis/status'),
                apiClient.baseClient.get('/api/v2/datapipe/status'),
            ]);
            const request = queue.data.data.request_type;
            const status = processing.data.data.status;
            if (
                !['', 'analysis', 'deletion'].includes(request) ||
                !['idle', 'analyzing', 'ingesting', 'purging', 'pruning', 'starting', 'optimizing'].includes(status)
            )
                throw Error('Unknown status');
            return { request, status };
        },
        { enabled: canReadStatus, retry: false, refetchInterval: 10000, refetchOnWindowFocus: false }
    );
    const unavailable = management.busy || management.locked || !canEdit;
    const review = management.pending?.review;
    const requestedRemoval = management.pending?.change.remove ?? [];
    const additionalRemoval = review?.removed.filter((name) => !requestedRemoval.includes(name)) ?? [];
    const showNames = (names?: string[]) => (names?.length ? names.join(', ') : 'None');
    const start = async () => {
        setStarting(true);
        setRunMessage('');
        try {
            // This is the existing native action, authorized as the signed-in user.
            await apiClient.requestAnalysis();
            setRunMessage('Analysis requested. It will use the active rules when it starts.');
        } catch {
            setRunUncertain(true);
            setRunMessage(
                'The analysis request could not be confirmed. Refresh its status before requesting it again.'
            );
        } finally {
            setStarting(false);
            void refreshNative();
        }
    };
    return (
        <Stack spacing={1}>
            <Stack direction='row' flexWrap='wrap' useFlexGap spacing={1}>
                <Button
                    className='text-link'
                    onClick={start}
                    disabled={
                        !canRun ||
                        !native ||
                        native.request !== '' ||
                        native.status === 'analyzing' ||
                        starting ||
                        runUncertain ||
                        !!review
                    }>
                    Run Analysis
                </Button>
                <Button
                    className='text-link'
                    disabled={refreshing || management.busy || starting || checkingNative}
                    onClick={async () => {
                        onRefresh();
                        const result = await refreshNative();
                        if (!result.isError) setRunUncertain(false);
                    }}>
                    Refresh rules and status
                </Button>
                <Button className='text-link' onClick={onReport}>
                    Latest status report
                </Button>
            </Stack>
            {runMessage && <Alert severity={runUncertain ? 'warning' : 'info'}>{runMessage}</Alert>}
            {!canRun && (
                <Typography variant='caption'>
                    Run Analysis requires permission to change application configuration.
                </Typography>
            )}
            {children}
            <Typography variant='caption'>
                <strong>Analysis:</strong>{' '}
                {!native
                    ? 'Status unavailable'
                    : native.request === 'analysis'
                      ? 'Queued'
                      : native.status === 'analyzing'
                        ? 'Running'
                        : native.request === 'deletion'
                          ? 'Deletion queued'
                          : native.status}
            </Typography>
            {management.error && <Alert severity='error'>{management.error}</Alert>}
            {management.message && <Alert severity='info'>{management.message}</Alert>}
            {review && (
                <Box
                    component='section'
                    aria-label='Review analysis changes'
                    className='border rounded p-2 [overflow-wrap:anywhere]'>
                    <Typography component='h2' variant='subtitle1'>
                        Review proposed changes
                    </Typography>
                    <Typography>
                        <strong>Based on rule revision:</strong> {review.base_revision || 'No active rules'}
                    </Typography>
                    <Typography>
                        <strong>Proposed revision:</strong> {review.revision}
                    </Typography>
                    <Typography>
                        <strong>Schema revision:</strong> {review.plan.schema_revision}
                    </Typography>
                    <Typography>
                        <strong>Overwritten files:</strong> {showNames(review.overwritten)}
                    </Typography>
                    {requestedRemoval.length > 0 && (
                        <>
                            <Typography>
                                <strong>Requested deletions:</strong> {showNames(requestedRemoval)}
                            </Typography>
                            <Typography>
                                <strong>Additional files removed:</strong> {showNames(additionalRemoval)}
                            </Typography>
                            {additionalRemoval.length > 0 && (
                                <Alert severity='warning'>
                                    Deleting a prerequisite also removes queries that need its answer rows. The existing
                                    cleanup then removes supporting queries that no longer feed a remaining graph
                                    output. Disable keeps the files.
                                </Alert>
                            )}
                        </>
                    )}
                    <Typography>
                        <strong>All deleted files:</strong> {showNames(review.removed)}
                    </Typography>
                    <Typography>
                        <strong>Disabled rules:</strong> {showNames(review.disabled)}
                    </Typography>
                    <Typography>
                        <strong>Enabled rules:</strong> {showNames(review.enabled)}
                    </Typography>
                    <Typography>
                        <strong>Disabled or blocked rules:</strong> {showNames(review.paused)}
                    </Typography>
                    <Typography>
                        <strong>Connection types to recalculate:</strong> {showNames(review.recalculated_relationships)}
                    </Typography>
                    <Typography>
                        <strong>Types with no remaining producer:</strong> {showNames(review.retired_relationships)}
                    </Typography>
                    <Typography variant='body2'>
                        The graph and rules table show the proposed rules. Deletion may include dependent files and
                        unused supporting files. Connections without a remaining producer are cleared after the next
                        successful analysis; supporting objects stay in the graph.
                    </Typography>
                    <Stack direction='row' spacing={1}>
                        <Button className='text-link' disabled={unavailable} onClick={management.activate}>
                            Activate reviewed revision
                        </Button>
                        <Button className='text-link' disabled={management.busy} onClick={management.clearReview}>
                            Discard preview
                        </Button>
                    </Stack>
                </Box>
            )}
        </Stack>
    );
}
