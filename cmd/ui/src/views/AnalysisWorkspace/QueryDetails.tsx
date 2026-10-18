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

import { faCircleInfo } from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Alert, Box, Button, IconButton, Link, Stack, Tab, Tabs, Tooltip, Typography } from '@mui/material';
import { ReactNode } from 'react';
import CypherText from './CypherText';
import {
    availability,
    completedState,
    dependencyDepth,
    dependencyKey,
    dependencyReason,
    duration,
    latestCompleted,
    observedTime,
    queryColor,
    stepTimes,
} from './model';
import { Dependency, Query, QueryOutcome, Workspace } from './types';

interface Props {
    query: Query;
    workspace: Workspace;
    outcome?: QueryOutcome;
    onFocus: (name: string) => void;
    onDownload: () => void;
    downloading: boolean;
    canEdit: boolean;
    onEdit: () => void;
    // Enable, Disable, and Delete, shown in the header's action row.
    actions?: ReactNode;
    // The open tab. The page keeps it so a click in the analysis report can
    // open a step straight on its Last run tab.
    section: DetailsSection;
    onSection: (section: DetailsSection) => void;
}

export type DetailsSection = 'overview' | 'cypher' | 'run';

// The details panel shows each fact once. Anything written in the file's
// header lines (namespace, version, allowed schema versions, depends-on, and
// optional instructions) appears only in the Cypher tab. Overview keeps what
// the file cannot tell you: status, installed schema, order, connections to
// other files, and outputs. Last run shows the latest matching outcome.
export default function QueryDetails({
    query,
    workspace,
    outcome,
    onFocus,
    onDownload,
    downloading,
    canEdit,
    onEdit,
    actions,
    section,
    onSection,
}: Props) {
    const prerequisites = workspace.dependencies.filter((connection) => connection.consumer === query.name);
    const consumers = workspace.dependencies.filter((connection) => connection.prerequisite === query.name);
    const peerNames = [...new Set(query.outputs.flatMap((output) => output.producers ?? []))].filter(
        (name) => name !== query.name
    );
    const ownInput = query.inputs.filter(
        (input) => input.type === 'relationship' && query.outputs.some((output) => output.kind === input.name)
    );
    const queryLink = (name: string) =>
        workspace.plan.queries.some((entry) => entry.name === name) ? (
            <Link component='button' onClick={() => onFocus(name)} className='text-left [overflow-wrap:anywhere]'>
                {name}
            </Link>
        ) : (
            <Typography component='span'>{name} (not in current rules)</Typography>
        );
    // One connection per line: an arrow, the other file, and what it passes
    // along. Clicking the explanation focuses that arrow in the graph.
    const connection = (dependency: Dependency, other: string, arrow: string, index: number) => (
        <Box key={index} className='flex flex-wrap gap-x-2 items-baseline'>
            <span aria-hidden>{arrow}</span>
            {queryLink(other)}
            <Link
                component='button'
                onClick={() => onFocus(dependencyKey(dependency))}
                aria-label={`Focus dependency path from ${dependency.prerequisite} to ${dependency.consumer}`}
                className='text-left [overflow-wrap:anywhere]'>
                {dependencyReason(dependency)}
            </Link>
        </Box>
    );
    // On the Cypher tab the file box fills the rest of the panel's height, so
    // it scrolls only when the file is longer than the space left. The chain of
    // boxes down to it must each fill the space too. A very short panel still
    // gives the file 200 pixels; the panel itself then scrolls.
    const fillPanel = section === 'cypher';
    return (
        <Box
            component='section'
            aria-label='Query details'
            className={`px-4 pb-4 [overflow-wrap:anywhere] ${fillPanel ? 'h-full flex flex-col' : ''}`}>
            <Stack spacing={1} className={fillPanel ? 'flex-1 min-h-0' : ''}>
                <Typography variant='h6' component='h2'>
                    {query.name}
                </Typography>
                <Stack direction='row' spacing={1} alignItems='center'>
                    <Box
                        component='span'
                        aria-hidden
                        className='inline-block w-3 h-3 rounded-full shrink-0'
                        style={{ backgroundColor: queryColor(query, outcome?.state) }}
                    />
                    <Typography variant='body2'>
                        {availability(query)}
                        {query.disabled_reason ? ` — ${query.disabled_reason}` : ''} ·{' '}
                        {query.extension_name || query.extension || query.namespace}
                    </Typography>
                </Stack>
                <Stack direction='row' flexWrap='wrap' useFlexGap spacing={1} alignItems='center'>
                    <Button className='text-link' onClick={onEdit} disabled={!canEdit}>
                        Edit/Test analysis file
                    </Button>
                    <Button className='text-link' onClick={onDownload} disabled={downloading}>
                        {downloading ? 'Downloading…' : 'Download query'}
                    </Button>
                    {actions}
                </Stack>
                <Tabs
                    value={section}
                    onChange={(_, value: DetailsSection) => onSection(value)}
                    aria-label='Query details sections'
                    variant='fullWidth'>
                    <Tab value='overview' label='Overview' />
                    <Tab value='cypher' label='Cypher' />
                    <Tab value='run' label='Last run' />
                </Tabs>
                {section === 'overview' && (
                    <Stack spacing={1.5} className='pt-1'>
                        <Box>
                            <Typography>
                                <strong>Order:</strong> {query.order}
                            </Typography>
                            <Typography>
                                <strong>Dependency depth:</strong> {dependencyDepth(query)}
                                <Tooltip title='Dependency depth counts arrows along the longest prerequisite chain, starting at 0. It does not indicate simultaneous execution.'>
                                    <IconButton size='small' aria-label='About dependency depth'>
                                        <FontAwesomeIcon icon={faCircleInfo} />
                                    </IconButton>
                                </Tooltip>
                            </Typography>
                            <Typography>
                                <strong>Installed schema:</strong> {query.schema_version || 'Unavailable'}
                            </Typography>
                            <Typography>
                                <strong>Added:</strong> {observedTime(query.added_at)}
                            </Typography>
                        </Box>
                        {query.input_warning && (
                            <Alert severity='info'>
                                This query reads graph data without naming every object or connection type. It runs
                                again whenever graph data changes to avoid keeping outdated results.
                            </Alert>
                        )}
                        <Box>
                            <Typography component='h3' variant='subtitle1'>
                                Needs
                            </Typography>
                            {!prerequisites.length && <Typography>No earlier query.</Typography>}
                            {prerequisites.map((dependency, index) =>
                                connection(dependency, dependency.prerequisite, '←', index)
                            )}
                        </Box>
                        <Box>
                            <Typography component='h3' variant='subtitle1'>
                                Used by
                            </Typography>
                            {!consumers.length && <Typography>No later query.</Typography>}
                            {consumers.map((dependency, index) =>
                                connection(dependency, dependency.consumer, '→', index)
                            )}
                        </Box>
                        {!!peerNames.length && (
                            <Box>
                                <Typography component='h3' variant='subtitle1'>
                                    Other producers of this relationship
                                </Typography>
                                <Typography variant='body2'>
                                    Their complete answers are combined. A paused producer can block the shared result;
                                    this does not create an execution arrow.
                                </Typography>
                                {peerNames.map((name) => (
                                    <Box key={name}>{queryLink(name)}</Box>
                                ))}
                            </Box>
                        )}
                        {ownInput.map((input) => (
                            <Typography key={input.name} variant='body2'>
                                Reading {input.name} uses previously saved graph data. This query does not wait for
                                itself or its fellow producers.
                            </Typography>
                        ))}
                        <Box>
                            <Typography component='h3' variant='subtitle1'>
                                Produces
                            </Typography>
                            {!query.outputs.length && <Typography>Answer rows for the queries that use it.</Typography>}
                            {query.outputs.map((output, index) => (
                                <Box key={index}>
                                    <Typography>
                                        <strong>{output.type}:</strong> {output.kind}
                                    </Typography>
                                    {output.id_prefix && (
                                        <Typography variant='body2'>
                                            Helper IDs start with {output.id_prefix}
                                        </Typography>
                                    )}
                                    {output.endpoints?.map((endpoint) => (
                                        <Typography
                                            variant='body2'
                                            key={`${endpoint.source_kind}:${endpoint.target_kind}`}>
                                            Allowed: {endpoint.source_kind} → {endpoint.target_kind}
                                        </Typography>
                                    ))}
                                </Box>
                            ))}
                        </Box>
                    </Stack>
                )}
                {section === 'cypher' && (
                    <Stack spacing={1} className='pt-1 flex-1 min-h-0'>
                        <Typography variant='body2'>
                            The comment lines at the top hold the namespace, version, declarations, and any optional
                            instructions.
                        </Typography>
                        <CypherText label='Original Cypher file' value={query.text} readOnly fill className='text-xs' />
                    </Stack>
                )}
                {section === 'run' && <LastRun query={query} workspace={workspace} outcome={outcome} />}
            </Stack>
        </Box>
    );
}

function LastRun({ query, workspace, outcome }: { query: Query; workspace: Workspace; outcome?: QueryOutcome }) {
    const times = stepTimes(workspace, query.name);
    const timeLines = (
        <Box className='pb-1'>
            <Typography>
                <strong>Last ran:</strong>{' '}
                {times.lastRun
                    ? `${observedTime(times.lastRun.at)} · ${completedState(times.lastRun.state)}`
                    : 'Not in the kept run history'}
            </Typography>
            <Typography>
                <strong>Last success:</strong>{' '}
                {times.lastSuccess ? observedTime(times.lastSuccess) : 'None in the kept run history'}
            </Typography>
        </Box>
    );
    if (!outcome)
        return (
            <Box className='pt-1'>
                {timeLines}
                <Typography>No matching completed outcome is available for this query.</Typography>
            </Box>
        );
    const execution = latestCompleted(workspace)?.execution;
    const saveState = execution?.save;
    // The run report stores why the run failed separately from each step's
    // outcome. Show it here when this step is one that failed. A partly saved
    // run can list several failures.
    const failure = [...(execution?.failures ?? []), ...(execution?.failure ? [execution.failure] : [])].find(
        (item) => item.query === query.name
    );
    const savedCount =
        outcome.saved_outputs ??
        (['failed', 'set-aside'].includes(outcome.state) && saveState === 'succeeded'
            ? 'Not saved in this run; its previous results were kept'
            : saveState === 'uncertain'
              ? 'Save unconfirmed'
              : ['not-started', 'failed', 'unchanged'].includes(saveState ?? '')
                ? 'No save in this attempt'
                : 'Not reported');
    return (
        <Box className='pt-1'>
            {timeLines}
            <Typography>
                <strong>Outcome:</strong> {completedState(outcome.state)}
            </Typography>
            {failure && (
                <Alert severity='error' aria-label='Why it failed' className='my-1'>
                    {failure.message}
                </Alert>
            )}
            {outcome.reason && (
                <Typography variant='body2'>
                    <strong>
                        {['skipped', 'set-aside', 'waiting', 'canceled', 'disabled'].includes(outcome.state)
                            ? 'Why it did not run:'
                            : 'Why it ran:'}
                    </strong>{' '}
                    {outcome.reason}
                </Typography>
            )}
            <Typography>
                <strong>Time:</strong> {duration(outcome.elapsed_nanoseconds)}
            </Typography>
            <Typography>
                <strong>Rows:</strong> {outcome.rows ?? 'Unavailable'}
            </Typography>
            {query.outputs.length ? (
                <>
                    <Typography>
                        <strong>Proposed graph output rows:</strong> {outcome.proposed_outputs ?? 'Not reported'}
                    </Typography>
                    <Typography>
                        <strong>Confirmed graph output rows:</strong> {savedCount}
                        <Tooltip title='This counts the output rows represented by this query after a successful save, including connections already present or shared with other queries. It is not the number of newly added connections.'>
                            <IconButton size='small' aria-label='About confirmed graph output rows'>
                                <FontAwesomeIcon icon={faCircleInfo} />
                            </IconButton>
                        </Tooltip>
                    </Typography>
                </>
            ) : (
                <Typography>
                    <strong>Graph outputs:</strong> Not applicable — this query returns rows for other queries.
                </Typography>
            )}
        </Box>
    );
}
