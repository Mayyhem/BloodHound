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

import { faPlus, faTable, faUpload } from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
    Alert,
    Box,
    Button,
    Checkbox,
    CircularProgress,
    FormControl,
    InputLabel,
    Link,
    MenuItem,
    Select,
    Stack,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableRow,
    TextField,
    Typography,
} from '@mui/material';
import { isAxiosError } from 'axios';
import { Permission, usePermissions } from 'bh-shared-ui';
import { IconButton } from 'doodle-ui';
import { CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from 'react-query';
import { Link as RouterLink } from 'react-router-dom';
import { ROUTE_ADMINISTRATION_OPENGRAPH_MANAGEMENT } from 'src/routes/constants';
import AnalysisFileEditor from './AnalysisFileEditor';
import { downloadQuery, downloadText, readWorkspace } from './api';
import BottomPanel from './BottomPanel';
import EntityPanel from './EntityPanel';
import ManagementControls from './ManagementControls';
import {
    availability,
    completedState,
    dependencyDepth,
    dependencyKey,
    dependencyReason,
    latestCompleted,
    numberedExtensions,
    queryColors,
    relatedQueries,
    reportMatches,
} from './model';
import QueryDetails from './QueryDetails';
import QueryGraph from './QueryGraph';
import RuleActions from './RuleActions';
import RunSummary from './RunSummary';
import { EditorWorkingCopy, QueryOutcome } from './types';
import UploadDialog from './UploadDialog';
import useRuleManagement from './useRuleManagement';

export default function AnalysisWorkspace() {
    const {
        data: activeWorkspace,
        isLoading,
        isFetching,
        error,
        refetch,
    } = useQuery(['analysis-workspace'], ({ signal }) => readWorkspace(signal), {
        retry: false,
        refetchOnWindowFocus: false,
        cacheTime: 0,
    });
    const [focused, setFocused] = useState<string | null>(null);
    const [search, setSearch] = useState('');
    const [extension, setExtension] = useState('');
    const [downloadError, setDownloadError] = useState('');
    const [downloading, setDownloading] = useState(false);
    const [panelWidth, setPanelWidth] = useState(400);
    const [tableHeight, setTableHeight] = useState(320);
    const [tableView, setTableView] = useState<'rules' | 'report' | null>('rules');
    const [checked, setChecked] = useState<Set<string>>(new Set());
    const [editorOpen, setEditorOpen] = useState(false);
    const [editorInitial, setEditorInitial] = useState<EditorWorkingCopy>();
    const [uploadOpen, setUploadOpen] = useState(false);
    const { checkPermission } = usePermissions();
    const canEdit = checkPermission(Permission.OPENGRAPH_WRITE);
    const canRun = checkPermission(Permission.APP_WRITE_APPLICATION_CONFIGURATION);
    const canReadStatus = checkPermission(Permission.GRAPH_DB_READ);
    const refreshWorkspace = useCallback(async () => !(await refetch()).isError, [refetch]);
    const management = useRuleManagement(activeWorkspace, refreshWorkspace);
    const workspace = useMemo(
        () =>
            activeWorkspace && management.pending
                ? {
                      ...activeWorkspace,
                      plan: management.pending.review.plan,
                      dependencies: management.pending.review.dependencies,
                  }
                : activeWorkspace,
        [activeWorkspace, management.pending]
    );
    const actionsDisabled = !canEdit || management.busy || management.locked || !!management.pending || !!error;
    useEffect(() => {
        setChecked(new Set());
    }, [activeWorkspace?.plan.revision, activeWorkspace?.plan.schema_revision]);
    const toggleChecked = (names: string[], selected: boolean) => {
        management.clearReview();
        setChecked((previous) => {
            const next = new Set(previous);
            names.forEach((name) => (selected ? next.add(name) : next.delete(name)));
            return next;
        });
    };
    const queries = workspace?.plan.queries ?? [];
    const filtered = queries.filter(
        (query) =>
            (!extension || query.namespace === extension) &&
            `${query.name} ${query.extension_name}`.toLowerCase().includes(search.toLowerCase())
    );
    const visible = new Set(filtered.map((query) => query.name));
    const currentNames = new Set(queries.map((query) => query.name));
    const related = relatedQueries(workspace?.dependencies ?? [], focused);
    const hiddenQueries = new Set([
        ...[...related.prerequisites, ...related.consumers].filter(
            (name) => currentNames.has(name) && !visible.has(name)
        ),
        ...(workspace?.dependencies ?? [])
            .filter((connection) => visible.has(connection.prerequisite) !== visible.has(connection.consumer))
            .map((connection) => (visible.has(connection.prerequisite) ? connection.consumer : connection.prerequisite))
            .filter((name) => currentNames.has(name)),
    ]);
    const selected = queries.find((query) => query.name === focused);
    const selectedDependencies =
        workspace?.dependencies.filter((dependency) => dependencyKey(dependency) === focused) ?? [];
    const outcomes = useMemo(() => {
        const report = workspace && latestCompleted(workspace);
        return new Map<string, QueryOutcome>(
            workspace && reportMatches(workspace, report)
                ? report!.execution!.queries.map((query) => [query.name, query])
                : []
        );
    }, [workspace]);
    const outcomeStates = useMemo(
        () => new Map([...outcomes].map(([name, outcome]) => [name, outcome.state])),
        [outcomes]
    );

    useEffect(() => {
        setFocused(null);
        setDownloadError('');
    }, [workspace?.plan.revision, workspace?.plan.schema_revision]);
    const clearFilters = () => {
        setSearch('');
        setExtension('');
    };
    const focusQuery = useCallback(
        (name: string | null) => {
            if (!name) {
                setFocused(null);
                return;
            }
            // A details link also reveals a query or connection hidden by filters.
            const query = workspace?.plan.queries.find((entry) => entry.name === name);
            const dependency = workspace?.dependencies.find((entry) => dependencyKey(entry) === name);
            if (!query && !dependency) return;
            const targets = query
                ? [query]
                : workspace!.plan.queries.filter(
                      (entry) => entry.name === dependency!.prerequisite || entry.name === dependency!.consumer
                  );
            if (
                targets.some(
                    (target) =>
                        (extension && target.namespace !== extension) ||
                        !`${target.name} ${target.extension_name}`.toLowerCase().includes(search.toLowerCase())
                )
            ) {
                setSearch('');
                setExtension('');
            }
            setFocused(name);
        },
        [workspace, extension, search]
    );
    const download = async () => {
        if (!workspace || !selected) return;
        setDownloadError('');
        setDownloading(true);
        try {
            if (management.pending) downloadText(selected.name, selected.text);
            else await downloadQuery(workspace, selected.name);
        } catch (error) {
            setDownloadError(
                isAxiosError(error) && error.response?.status === 409
                    ? 'Rules or schemas changed. Refresh before downloading this query.'
                    : 'The query could not be downloaded. Refresh and try again.'
            );
        } finally {
            setDownloading(false);
        }
    };
    const openEditor = (initial?: EditorWorkingCopy) => {
        setEditorInitial(initial);
        setEditorOpen(true);
    };

    const panelOpen = !!selected || selectedDependencies.length > 0;
    const hasExtensions = !!workspace && (workspace.installed_extensions ?? workspace.plan.extensions).length > 0;
    // The bottom-left buttons sit just above the results table when it is open.
    const canvasBottom = tableView ? tableHeight + 28 : 16;
    const closeUpload = useCallback(() => setUploadOpen(false), []);
    // How much of each canvas edge the panels cover right now, so Reset Graph
    // frames the queries in the part of the canvas that is still visible.
    const topPanel = useRef<HTMLDivElement>(null);
    const coveredEdges = () => ({
        top: (topPanel.current?.getBoundingClientRect().height ?? 0) + 32,
        right: panelOpen ? panelWidth + 32 : 16,
        bottom: canvasBottom + 48,
        left: 16,
    });
    // Upload, Create, and Rules table sit in the graph's bottom-left row,
    // beside Reset Graph. Each icon's name appears as its tooltip.
    const fileActions = (
        <Box role='group' aria-label='Analysis file actions' className='flex gap-1'>
            <IconButton
                aria-label='Upload analysis files'
                tooltip={
                    management.draft?.revision
                        ? 'Upload analysis files (inactive draft saved)'
                        : 'Upload analysis files'
                }
                onClick={() => setUploadOpen(true)}>
                <span className='relative'>
                    <FontAwesomeIcon icon={faUpload} />
                    {management.draft?.revision && (
                        <span className='absolute -top-1 -right-1 !size-2 rounded-full bg-primary' />
                    )}
                </span>
            </IconButton>
            <IconButton
                aria-label='Create analysis file'
                onClick={() => openEditor()}
                disabled={!canEdit || !hasExtensions}>
                <FontAwesomeIcon icon={faPlus} />
            </IconButton>
            <IconButton aria-label='Open rules table' tooltip='Rules table' onClick={() => setTableView('rules')}>
                <FontAwesomeIcon icon={faTable} />
            </IconButton>
        </Box>
    );
    return (
        <Box
            className='relative h-full w-full overflow-hidden'
            data-testid='analysis-workspace'
            style={{ '--analysis-panel-space': `${panelWidth + 40}px` } as CSSProperties}>
            {workspace && !error && queries.length > 0 && (
                <QueryGraph
                    key={`${workspace.plan.revision}:${workspace.plan.schema_revision}`}
                    workspace={workspace}
                    visible={visible}
                    focused={focused}
                    onFocus={focusQuery}
                    outcomes={outcomeStates}
                    coveredEdges={coveredEdges}
                    controlsBottom={canvasBottom}>
                    {fileActions}
                </QueryGraph>
            )}
            <Box
                ref={topPanel}
                className={`absolute top-4 left-4 z-20 max-w-[600px] ${panelOpen ? 'right-4 md:right-[var(--analysis-panel-space)]' : 'right-4'}`}>
                <Box
                    component='details'
                    open
                    className='bg-neutral-2 rounded-lg shadow-outer-1 border p-3 max-h-[60vh] overflow-auto'>
                    <Box component='summary' className='cursor-pointer font-bold'>
                        OpenGraph Analysis
                    </Box>
                    <Stack spacing={1} className='pt-2'>
                        {isLoading && <CircularProgress aria-label='Loading analysis workspace' />}
                        {!!error && (
                            <Alert severity='error'>
                                {isAxiosError(error) && [401, 403].includes(error.response?.status ?? 0)
                                    ? 'Sign in with OpenGraph read permission to inspect analysis rules.'
                                    : 'The analysis workspace is unavailable. Check that OpenGraph analysis and its rule service are configured, then refresh.'}
                            </Alert>
                        )}
                        {workspace && !error && (
                            <>
                                <ManagementControls
                                    management={management}
                                    canEdit={canEdit}
                                    canRun={canRun}
                                    canReadStatus={canReadStatus}
                                    refreshing={isFetching}
                                    onRefresh={() => {
                                        setChecked(new Set());
                                        void management.refresh();
                                    }}
                                    onReport={() => setTableView('report')}>
                                    <Typography variant='caption' className='[overflow-wrap:anywhere]'>
                                        <strong>Current rule revision:</strong>{' '}
                                        {activeWorkspace!.plan.revision || 'No activated rules'}
                                    </Typography>
                                    <Typography variant='caption' className='[overflow-wrap:anywhere]'>
                                        <strong>Schema revision:</strong>{' '}
                                        {workspace.plan.schema_revision || 'Unavailable'}
                                    </Typography>
                                    <Stack
                                        direction='row'
                                        spacing={1.5}
                                        flexWrap='wrap'
                                        useFlexGap
                                        aria-label='Graph color legend'>
                                        {Object.values(queryColors).map(({ color, label }) => (
                                            <Stack key={label} direction='row' alignItems='center' spacing={0.5}>
                                                <Box
                                                    component='span'
                                                    aria-hidden
                                                    className='inline-block w-3 h-3 rounded-full'
                                                    style={{ backgroundColor: color }}
                                                />
                                                <Typography variant='caption'>{label}</Typography>
                                            </Stack>
                                        ))}
                                    </Stack>
                                </ManagementControls>
                                <Link component={RouterLink} to={ROUTE_ADMINISTRATION_OPENGRAPH_MANAGEMENT}>
                                    OpenGraph Management
                                </Link>
                                {!hasExtensions && (
                                    <Alert severity='info'>
                                        Install an extension in OpenGraph Management before creating an analysis file.
                                    </Alert>
                                )}
                                {!queries.length && (
                                    <Alert severity='info'>
                                        No activated analysis rules. Upload files to save an inactive draft, then review
                                        and activate.
                                    </Alert>
                                )}
                            </>
                        )}
                        {!!error && (
                            <Button className='text-link' onClick={() => void refetch()}>
                                Refresh
                            </Button>
                        )}
                    </Stack>
                </Box>
            </Box>
            {/* Without a graph there is no Reset Graph button to sit beside, so the
                file buttons take the same corner on their own. */}
            {workspace && !error && !queries.length && (
                <Box className='absolute left-4 z-10' style={{ bottom: canvasBottom }}>
                    {fileActions}
                </Box>
            )}
            {workspace && !error && tableView && (
                <BottomPanel height={tableHeight} onHeight={setTableHeight} panelOpen={panelOpen}>
                    <Stack direction='row' alignItems='center' spacing={1} className='px-3 pb-2 shrink-0'>
                        <Button
                            className='text-link'
                            aria-pressed={tableView === 'rules'}
                            onClick={() => setTableView('rules')}>
                            {management.pending ? 'Proposed rules' : 'Rules'}
                        </Button>
                        <Button
                            className='text-link'
                            aria-pressed={tableView === 'report'}
                            onClick={() => setTableView('report')}>
                            Analysis report
                        </Button>
                        <Box className='flex-1' />
                        <Button
                            className='text-link'
                            onClick={() => setTableView(null)}
                            aria-label='Close results panel'>
                            Close
                        </Button>
                    </Stack>
                    <Box className='overflow-auto min-h-0 flex-1 px-3 pb-3'>
                        {tableView === 'report' ? (
                            <RunSummary workspace={workspace} onFocus={focusQuery} />
                        ) : (
                            <>
                                <Stack
                                    direction='row'
                                    spacing={1}
                                    flexWrap='wrap'
                                    alignItems='center'
                                    useFlexGap
                                    className='py-2'>
                                    <Typography>
                                        <strong>Rules:</strong> {filtered.length}
                                    </Typography>
                                    <TextField
                                        label='Search queries'
                                        value={search}
                                        onChange={(event) => setSearch(event.target.value)}
                                        size='small'
                                    />
                                    <FormControl size='small' className='min-w-[180px]'>
                                        <InputLabel id='analysis-extension-label'>Extension</InputLabel>
                                        <Select
                                            labelId='analysis-extension-label'
                                            label='Extension'
                                            value={extension}
                                            onChange={(event) => setExtension(event.target.value)}>
                                            <MenuItem value=''>All extensions</MenuItem>
                                            {numberedExtensions(queries).map(({ namespace, number }) => (
                                                <MenuItem key={namespace} value={namespace}>
                                                    {number}.{' '}
                                                    {workspace.plan.extensions.find(
                                                        (entry) => entry.namespace === namespace
                                                    )?.display_name || namespace}{' '}
                                                    ({namespace})
                                                </MenuItem>
                                            ))}
                                        </Select>
                                    </FormControl>
                                    <Button
                                        className='text-link'
                                        onClick={clearFilters}
                                        disabled={!search && !extension}>
                                        Clear filters
                                    </Button>
                                </Stack>
                                <Stack direction='row' spacing={1} flexWrap='wrap' alignItems='center' useFlexGap>
                                    <Typography>
                                        <strong>Selected for changes:</strong> {checked.size} (
                                        {[...checked].filter((name) => !visible.has(name)).length} hidden)
                                    </Typography>
                                    <RuleActions
                                        names={[...checked]}
                                        disabled={actionsDisabled}
                                        bulk
                                        onAction={(action, names) => void management.reviewAction(action, names)}
                                    />
                                    <Button
                                        className='text-link'
                                        disabled={!checked.size || management.busy}
                                        onClick={() => toggleChecked([...checked], false)}>
                                        Clear selection
                                    </Button>
                                </Stack>
                                {(hiddenQueries.size > 0 || (selected && !visible.has(selected.name))) && (
                                    <Alert severity='info'>
                                        {hiddenQueries.size} related queries are hidden by filters.{' '}
                                        {selected && !visible.has(selected.name)
                                            ? 'The focused query is also hidden.'
                                            : ''}
                                        <Button className='text-link' onClick={clearFilters}>
                                            Reveal hidden queries
                                        </Button>
                                    </Alert>
                                )}
                                {!filtered.length && <Alert severity='info'>No queries match these filters.</Alert>}
                                <Table size='small' stickyHeader aria-label='Current analysis rules'>
                                    <TableHead>
                                        <TableRow>
                                            <TableCell padding='checkbox'>
                                                <Checkbox
                                                    inputProps={{ 'aria-label': 'Select all visible rules' }}
                                                    checked={
                                                        filtered.length > 0 &&
                                                        filtered.every((query) => checked.has(query.name))
                                                    }
                                                    indeterminate={
                                                        filtered.some((query) => checked.has(query.name)) &&
                                                        !filtered.every((query) => checked.has(query.name))
                                                    }
                                                    disabled={actionsDisabled || !filtered.length}
                                                    onChange={(_, value) =>
                                                        toggleChecked(
                                                            filtered.map((query) => query.name),
                                                            value
                                                        )
                                                    }
                                                />
                                            </TableCell>
                                            <TableCell>Order / dependency depth</TableCell>
                                            <TableCell>Query</TableCell>
                                            <TableCell>Availability</TableCell>
                                            <TableCell>Relation to focus</TableCell>
                                            <TableCell>Completed outcome</TableCell>
                                        </TableRow>
                                    </TableHead>
                                    <TableBody>
                                        {filtered.map((query) => (
                                            <TableRow
                                                key={query.name}
                                                selected={query.name === focused}
                                                onClick={() => focusQuery(query.name)}
                                                className='cursor-pointer'>
                                                <TableCell
                                                    padding='checkbox'
                                                    onClick={(event) => event.stopPropagation()}>
                                                    <Checkbox
                                                        inputProps={{
                                                            'aria-label': `Select ${query.name} for changes`,
                                                        }}
                                                        checked={checked.has(query.name)}
                                                        disabled={actionsDisabled}
                                                        onChange={(_, value) => toggleChecked([query.name], value)}
                                                    />
                                                </TableCell>
                                                <TableCell>
                                                    {query.order} / {dependencyDepth(query)}
                                                </TableCell>
                                                <TableCell>
                                                    <Link
                                                        component='button'
                                                        onClick={() => focusQuery(query.name)}
                                                        aria-pressed={query.name === focused}
                                                        className='text-left [overflow-wrap:anywhere]'>
                                                        {query.name}
                                                    </Link>
                                                </TableCell>
                                                <TableCell>{availability(query)}</TableCell>
                                                <TableCell>
                                                    {query.name === focused
                                                        ? 'Focused'
                                                        : related.prerequisites.has(query.name)
                                                          ? 'Prerequisite'
                                                          : related.consumers.has(query.name)
                                                            ? 'Consumer'
                                                            : '—'}
                                                </TableCell>
                                                <TableCell>{completedState(outcomes.get(query.name)?.state)}</TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            </>
                        )}
                    </Box>
                </BottomPanel>
            )}
            {workspace && !error && panelOpen && (
                <EntityPanel width={panelWidth} onWidth={setPanelWidth} onClose={() => setFocused(null)}>
                    {downloadError && <Alert severity='error'>{downloadError}</Alert>}
                    {selected ? (
                        <>
                            <QueryDetails
                                actions={
                                    <RuleActions
                                        names={[selected.name]}
                                        authorEnabled={selected.author_enabled}
                                        disabled={actionsDisabled}
                                        onAction={(action, names) => void management.reviewAction(action, names)}
                                    />
                                }
                                query={selected}
                                workspace={workspace}
                                outcome={outcomes.get(selected.name)}
                                onFocus={focusQuery}
                                onDownload={() => void download()}
                                downloading={downloading}
                                canEdit={canEdit}
                                onEdit={() =>
                                    openEditor({
                                        name: selected.name,
                                        text: selected.text,
                                        base_revision: activeWorkspace!.plan.revision,
                                        revision: '',
                                    })
                                }
                            />
                        </>
                    ) : (
                        <Stack
                            spacing={1.5}
                            component='section'
                            aria-label='Dependency details'
                            className='p-4 [overflow-wrap:anywhere]'>
                            <Typography variant='h6' component='h2'>
                                Selected dependency
                            </Typography>
                            <Typography>
                                <strong>Prerequisite:</strong>
                            </Typography>
                            <Link
                                component='button'
                                onClick={() => focusQuery(selectedDependencies[0].prerequisite)}
                                className='text-left [overflow-wrap:anywhere]'>
                                {selectedDependencies[0].prerequisite}
                            </Link>
                            <Typography>
                                <strong>Consumer:</strong>
                            </Typography>
                            <Link
                                component='button'
                                onClick={() => focusQuery(selectedDependencies[0].consumer)}
                                className='text-left [overflow-wrap:anywhere]'>
                                {selectedDependencies[0].consumer}
                            </Link>
                            {selectedDependencies.map((dependency, index) => (
                                <Typography key={index}>{dependencyReason(dependency)}</Typography>
                            ))}
                            <Typography variant='body2'>
                                The path includes prerequisites before this arrow and consumers after it. Other branches
                                fade to gray.
                            </Typography>
                        </Stack>
                    )}
                </EntityPanel>
            )}
            {workspace && !error && (
                <UploadDialog
                    open={uploadOpen}
                    onClose={closeUpload}
                    workspace={activeWorkspace!}
                    management={management}
                    canEdit={canEdit}
                />
            )}
            {workspace && !error && (
                <AnalysisFileEditor
                    open={editorOpen}
                    workspace={workspace}
                    canEdit={canEdit}
                    initial={editorInitial}
                    onClose={() => setEditorOpen(false)}
                    onActivated={(name) => {
                        setEditorOpen(false);
                        void refreshWorkspace();
                        setFocused(name);
                    }}
                />
            )}
        </Box>
    );
}
