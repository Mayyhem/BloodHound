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

import {
    Alert,
    Box,
    Button,
    FormControlLabel,
    Link,
    Stack,
    Switch,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableRow,
    Typography,
} from '@mui/material';
import {
    analysisAttempts,
    AttemptOutcome,
    completedState,
    duration,
    latestCompleted,
    observedTime,
    reportMatches,
    stepTimes,
} from './model';
import { Workspace } from './types';

export default function RunSummary({
    workspace,
    outcomes,
    separateAttempts,
    onSeparateAttempts,
    onFocus,
    onOpenRun,
}: {
    workspace: Workspace;
    // Each step's outcome and the attempt it came from, as the page shows them.
    outcomes: Map<string, AttemptOutcome>;
    // Show the latest attempt alone instead of the whole analysis.
    separateAttempts: boolean;
    onSeparateAttempts: (value: boolean) => void;
    onFocus: (name: string) => void;
    // Opens a step's details panel on its Last run tab.
    onOpenRun: (name: string) => void;
}) {
    const inRules = (name: string) => workspace.plan.queries.some((query) => query.name === name);
    const report = latestCompleted(workspace);
    const matching = reportMatches(workspace, report);
    const execution = report?.execution;
    const attempts = analysisAttempts(workspace.status);
    // Failures to explain: in the combined view, those of the attempt each
    // failed step's outcome came from; otherwise the latest attempt's. A
    // partly saved attempt lists every failure; other attempts have at most one.
    const failuresOf = (attempt: typeof report) =>
        attempt?.execution?.failures?.length
            ? attempt.execution.failures
            : attempt?.execution?.failure
              ? [attempt.execution.failure]
              : [];
    // A failure with no step name belongs to the run itself (for example an
    // unconfirmed save), so the latest attempt's run-level failures always show.
    const failures = separateAttempts
        ? failuresOf(report)
        : [
              ...[...outcomes.values()]
                  .filter((entry) => entry.outcome.state === 'failed')
                  .flatMap((entry) =>
                      failuresOf(entry.attempt).filter((failure) => failure.query === entry.outcome.name)
                  ),
              ...failuresOf(report).filter((failure) => !failure.query),
          ];
    const savedRun = [...(workspace.status.history ?? []), workspace.status]
        .filter(
            (attempt) => attempt.revision === workspace.status.saved_revision && attempt.execution?.save === 'succeeded'
        )
        .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))[0];
    return (
        <Stack spacing={1} component='section' aria-label='Run reports'>
            <Typography>
                <strong>Report received:</strong> {observedTime(workspace.status.updated_at)}
            </Typography>
            <Typography>
                <strong>Overall status:</strong> {workspace.status.state}
            </Typography>
            <Typography>
                <strong>Message:</strong> {workspace.status.message}
            </Typography>
            {workspace.status.state === 'running' && (
                <Alert severity='info'>
                    An overall running report was received at {observedTime(workspace.status.updated_at)}. Per-query
                    live progress is unavailable. The diagram shows only a matching completed attempt.
                </Alert>
            )}
            <Typography className='[overflow-wrap:anywhere]'>
                <strong>Last confirmed saved rule revision:</strong> {workspace.status.saved_revision || 'Unavailable'}
            </Typography>
            <Typography className='[overflow-wrap:anywhere]'>
                <strong>Last confirmed saved attempt:</strong>{' '}
                {savedRun
                    ? `${savedRun.run_id || savedRun.event} · ${observedTime(savedRun.updated_at)}`
                    : 'Unavailable in retained reports'}
            </Typography>
            {workspace.status.results_uncertain && (
                <Alert severity='warning'>
                    The latest save outcome is uncertain. The last confirmed saved revision does not establish what is
                    currently stored.
                </Alert>
            )}
            {workspace.status.results_stale && !workspace.status.results_uncertain && (
                <Alert severity='info'>
                    Existing results are not confirmed current for all active rules and schemas. Read the completed
                    report for calculation and saving details.
                </Alert>
            )}
            {!report && <Alert severity='info'>No completed attempt report is available.</Alert>}
            {report && !matching && (
                <Alert severity='info'>
                    The latest completed attempt has a different rule or schema revision, or lacks the information
                    needed to match it. Its report is available below; its outcomes are not attached to the current
                    diagram.
                </Alert>
            )}
            {attempts.length > 1 && (
                <Box component='section' aria-label='Attempts of the latest analysis'>
                    <Typography>
                        <strong>Attempts of the latest analysis:</strong> {attempts.length}. The first attempt is the
                        run BloodHound triggered; each later one is an automatic retry of the steps that were set aside,
                        and reports every other step as up to date.
                    </Typography>
                    <Box component='ol' className='list-decimal pl-6'>
                        {attempts.map((attempt) => (
                            <li key={`${attempt.run_id}|${attempt.updated_at}`}>
                                {observedTime(attempt.updated_at)} · {attempt.state} · {attempt.message}
                            </li>
                        ))}
                    </Box>
                    <FormControlLabel
                        control={
                            <Switch
                                checked={separateAttempts}
                                onChange={(event) => onSeparateAttempts(event.target.checked)}
                            />
                        }
                        label='Show the latest attempt on its own'
                    />
                </Box>
            )}
            {report && (
                <Box component='details' open className='p-2 border border-neutral-300 dark:border-neutral-600'>
                    <Box component='summary' className='cursor-pointer'>
                        {separateAttempts || attempts.length < 2 ? 'Latest completed attempt' : 'Latest analysis'}:{' '}
                        {report.state} · {observedTime(report.updated_at)}
                        {matching ? ' · Matches current plan' : ' · Separate report'}
                    </Box>
                    <Stack spacing={1} className='pt-2'>
                        <Typography className='[overflow-wrap:anywhere]'>
                            <strong>Attempt:</strong> {report.run_id || report.event}
                        </Typography>
                        <Typography className='[overflow-wrap:anywhere]'>
                            <strong>Rule revision:</strong> {report.revision || 'Unavailable'}
                        </Typography>
                        <Typography className='[overflow-wrap:anywhere]'>
                            <strong>Schema revision:</strong> {execution?.schema_revision || 'Unavailable'}
                        </Typography>
                        <Typography>
                            <strong>Message:</strong> {report.message}
                        </Typography>
                        {execution ? (
                            <>
                                <Typography>
                                    <strong>Calculation:</strong> {execution.calculation}
                                </Typography>
                                <Typography>
                                    <strong>Saving:</strong> {execution.save}
                                </Typography>
                                <Typography>
                                    <strong>Total time:</strong> {duration(execution.elapsed_nanoseconds)}
                                </Typography>
                                {execution.save === 'unchanged' && (
                                    <Typography>This attempt left saved results unchanged.</Typography>
                                )}
                                {execution.save === 'failed' && (
                                    <Typography>
                                        Saving failed. Read the failure explanation for confirmation of what was
                                        preserved.
                                    </Typography>
                                )}
                                {execution.save === 'uncertain' && (
                                    <Typography>
                                        Saving was not confirmed. Do not assume that a failed reply means the database
                                        discarded the changes.
                                    </Typography>
                                )}
                                {execution.calculation === 'partial' && (
                                    <Typography>
                                        Steps that failed were set aside, together with the steps that use their results
                                        or save the same types. Those kept their previous results; everything else was
                                        saved.
                                    </Typography>
                                )}
                                {execution.cleanup_pending && (
                                    <Typography>Relationship cleanup is still pending.</Typography>
                                )}
                                {execution.saved && (
                                    <>
                                        <Typography>
                                            <strong>Added:</strong> {execution.saved.relationships_added}
                                        </Typography>
                                        <Typography>
                                            <strong>Removed:</strong> {execution.saved.relationships_removed}
                                        </Typography>
                                        <Typography>
                                            <strong>Updated:</strong> {execution.saved.relationships_updated}
                                        </Typography>
                                        <Typography>
                                            <strong>Unchanged:</strong> {execution.saved.relationships_unchanged}
                                        </Typography>
                                        <Typography>
                                            <strong>Helpers changed:</strong> {execution.saved.helpers_changed}
                                        </Typography>
                                    </>
                                )}
                                {failures.map((failure, index) => (
                                    <Alert severity='error' key={index}>
                                        <strong>
                                            {failure.query
                                                ? `Query ${failure.query}`
                                                : failure.relationship
                                                  ? `Saving ${failure.relationship} connections`
                                                  : 'Run-level failure'}
                                        </strong>
                                        <br />
                                        <strong>Phase:</strong> {failure.phase}
                                        <br />
                                        <strong>Explanation:</strong> {failure.message}
                                        {matching &&
                                            failure.query &&
                                            workspace.plan.queries.some((query) => query.name === failure.query) && (
                                                <Button className='text-link' onClick={() => onFocus(failure.query!)}>
                                                    Jump to failure
                                                </Button>
                                            )}
                                    </Alert>
                                ))}
                                <Box className='overflow-x-auto'>
                                    <Table size='small' aria-label='Completed query outcomes'>
                                        <TableHead>
                                            <TableRow>
                                                <TableCell>Query</TableCell>
                                                <TableCell>Outcome</TableCell>
                                                {attempts.length > 1 && <TableCell>Attempt</TableCell>}
                                                <TableCell>Rows</TableCell>
                                                <TableCell>Time</TableCell>
                                                <TableCell>Last ran</TableCell>
                                                <TableCell>Last success</TableCell>
                                                <TableCell>Explanation</TableCell>
                                            </TableRow>
                                        </TableHead>
                                        <TableBody>
                                            {execution.queries.map((latestOutcome) => {
                                                // The page's outcome for this step, when the report
                                                // matches the current plan; otherwise this attempt's.
                                                const entry = outcomes.get(latestOutcome.name);
                                                const query = entry?.outcome ?? latestOutcome;
                                                const times = stepTimes(workspace, query.name);
                                                const open = inRules(query.name)
                                                    ? () => onOpenRun(query.name)
                                                    : undefined;
                                                return (
                                                    <TableRow
                                                        key={query.name}
                                                        hover={!!open}
                                                        onClick={open}
                                                        className={open ? 'cursor-pointer' : undefined}>
                                                        <TableCell>
                                                            {open ? (
                                                                <Link
                                                                    component='button'
                                                                    onClick={(event) => {
                                                                        event.stopPropagation();
                                                                        open();
                                                                    }}
                                                                    className='text-left [overflow-wrap:anywhere]'>
                                                                    {query.name}
                                                                </Link>
                                                            ) : (
                                                                `${query.name} (not in current rules)`
                                                            )}
                                                        </TableCell>
                                                        <TableCell>{completedState(query.state)}</TableCell>
                                                        {attempts.length > 1 && (
                                                            <TableCell>
                                                                {entry
                                                                    ? `${entry.attemptNumber}${entry.attemptNumber > 1 ? ' (retry)' : ''}`
                                                                    : attempts.length}
                                                            </TableCell>
                                                        )}
                                                        <TableCell>{query.rows ?? 'Unavailable'}</TableCell>
                                                        <TableCell>{duration(query.elapsed_nanoseconds)}</TableCell>
                                                        <TableCell>
                                                            {times.lastRun
                                                                ? observedTime(times.lastRun.at)
                                                                : 'Not kept'}
                                                        </TableCell>
                                                        <TableCell>
                                                            {times.lastSuccess
                                                                ? observedTime(times.lastSuccess)
                                                                : 'None kept'}
                                                        </TableCell>
                                                        <TableCell>{query.reason}</TableCell>
                                                    </TableRow>
                                                );
                                            })}
                                        </TableBody>
                                    </Table>
                                </Box>
                            </>
                        ) : (
                            <Typography>Per-query execution details are unavailable for this attempt.</Typography>
                        )}
                    </Stack>
                </Box>
            )}
        </Stack>
    );
}
