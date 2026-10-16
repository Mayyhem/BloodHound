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

import { faBackward, faFloppyDisk, faPlay } from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
    Alert,
    AlertColor,
    Autocomplete,
    Box,
    Button,
    Checkbox,
    CircularProgress,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    FormControl,
    IconButton,
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
    Tooltip,
    Typography,
} from '@mui/material';
import { isAxiosError } from 'axios';
import { useEffect, useMemo, useRef, useState } from 'react';

import {
    EditorPlanPreview,
    activateEditorTest,
    listEditorWorkingCopies,
    previewEditorPlan,
    readEditorWorkingCopy,
    reviewEditorTest,
    saveEditorWorkingCopy,
    testEditorFile,
} from './api';
import { editorDependencyProblem, readEditorDependencies, setEditorDependency, setEditorHeader } from './editorModel';
import { dependencyKey } from './model';
import QueryGraph from './QueryGraph';
import {
    Dependency,
    EditorTestResult,
    EditorWorkingCopy,
    EditorWorkingCopySummary,
    Query,
    Review,
    Workspace,
} from './types';

interface Props {
    open: boolean;
    workspace: Workspace;
    canEdit: boolean;
    initial?: EditorWorkingCopy;
    onClose: () => void;
    onActivated: (name: string) => void;
}

interface TestReceipt {
    name: string;
    text: string;
    result: EditorTestResult;
}

function defaultCopy(workspace: Workspace): EditorWorkingCopy {
    const namespace = (workspace.installed_extensions ?? workspace.plan.extensions)[0]?.namespace || 'analysis';
    const name = `${namespace}/new_analysis.cypher`;
    return {
        name,
        text: `// namespace: ${namespace}\n// version: v1.0.0\n\nRETURN 1 AS result\n`,
        base_revision: workspace.plan.revision,
        revision: '',
    };
}

function defaultAlias(name: string) {
    return (name.split('/').pop() || 'query').replace(/\.cypher$/, '').replace(/[^A-Za-z0-9_]/g, '_');
}

// An empty value is shown as the word null, so it is not mistaken for a
// missing row. Lists and maps are shown as JSON text.
function readableValue(value: unknown) {
    if (value === null || value === undefined) return 'null';
    return typeof value === 'object' ? JSON.stringify(value) : String(value);
}

// A new file is not in the workspace yet, so the dependency window draws it
// from this stand-in. Only its name, extension, and version are shown.
function editedFileQuery(name: string, version: string): Query {
    const namespace = name.split('/')[0];
    return {
        name,
        namespace,
        version,
        schema_version: '',
        extension: namespace,
        extension_name: namespace,
        added_at: '',
        enabled: true,
        author_enabled: true,
        disabled_reason: '',
        order: 0,
        stage: 0,
        parameter: '',
        text: '',
        inputs: [],
        outputs: [],
        processing: {},
    };
}

export function receiptMatches(receipt: TestReceipt | undefined, name: string, text: string) {
    return receipt?.name === name && receipt.text === text;
}

function safeEditorError(error: unknown, action: string) {
    const apiMessage =
        isAxiosError(error) &&
        (error.response?.data as { errors?: { message?: string }[] } | undefined)?.errors?.[0]?.message;
    if (apiMessage) return apiMessage;
    if (isAxiosError(error) && error.response?.status === 409) {
        return 'Rules or schemas changed. Refresh the workspace and test again.';
    }
    if (isAxiosError(error) && [401, 403].includes(error.response?.status ?? 0)) {
        return 'You do not have permission to change analysis files.';
    }
    return `The editor could not ${action}. Try again.`;
}

export default function AnalysisFileEditor({ open, workspace, canEdit, initial, onClose, onActivated }: Props) {
    const [workingCopy, setWorkingCopy] = useState<EditorWorkingCopy>(() => initial || defaultCopy(workspace));
    // Revert returns to this copy: the file as opened, resumed, or last saved.
    const [original, setOriginal] = useState<EditorWorkingCopy>(() => initial || defaultCopy(workspace));
    const [copies, setCopies] = useState<EditorWorkingCopySummary[]>([]);
    const [resumeName, setResumeName] = useState('');
    const [receipt, setReceipt] = useState<TestReceipt>();
    const [review, setReview] = useState<Review>();
    const [preview, setPreview] = useState<EditorPlanPreview>();
    const [busy, setBusy] = useState('');
    // One message at a time: red for errors, orange for warnings, blue for
    // information, and green when something was saved.
    const [message, setMessage] = useState<{ text: string; severity: AlertColor }>();
    const showError = (text: string) => setMessage({ text, severity: 'error' });
    const showWarning = (text: string) => setMessage({ text, severity: 'warning' });
    const abortController = useRef<AbortController>();
    // Every open editor is a separate session. Late responses from an earlier
    // session must never replace code or enable activation in the new one.
    const session = useRef(0);
    const beginRequest = () => {
        abortController.current?.abort();
        const controller = new AbortController();
        abortController.current = controller;
        const epoch = session.current;
        return { controller, current: () => session.current === epoch && !controller.signal.aborted };
    };

    const dependencies = useMemo(
        () => workspace.plan.queries.filter((query) => query.name !== workingCopy.name),
        [workspace.plan.queries, workingCopy.name]
    );
    const installedExtensions = workspace.installed_extensions || workspace.plan.extensions;
    const namespace = workingCopy.name.split('/')[0];
    const declarations = useMemo(
        () => readEditorDependencies(workingCopy.text, namespace),
        [workingCopy.text, namespace]
    );
    const selectedDependencies = new Set(declarations.map((dependency) => dependency.name));
    // A declared prerequisite that is not active still appears in the dropdown,
    // so it can be seen and removed; the warning below explains the problem.
    const dependencyChoices = [
        ...dependencies.map((query) => query.name),
        ...declarations
            .map((dependency) => dependency.name)
            .filter((name) => !dependencies.some((query) => query.name === name)),
    ];
    const aliases = Object.fromEntries(declarations.map((dependency) => [dependency.name, dependency.alias]));
    // Active queries that already read this file's answer (only when editing an existing file).
    const consumers = workspace.dependencies.filter((dependency) => dependency.prerequisite === workingCopy.name);
    const missing = declarations.find((dependency) => !dependencies.some((query) => query.name === dependency.name));
    const declarationProblem =
        editorDependencyProblem(declarations) ||
        (missing ? `The prerequisite ${missing.name} is not active in this workspace.` : undefined);
    const version = workingCopy.text.match(/^\/\/[ \t]*version:[ \t]*(.*)$/m)?.[1] ?? 'v1.0.0';
    const resultColumns = useMemo(
        () => (receipt?.result.rows[0] ? Object.keys(receipt.result.rows[0]) : []),
        [receipt]
    );
    const tested = receiptMatches(receipt, workingCopy.name, workingCopy.text);
    const hasRows = !!receipt?.result.rows.length;
    // What activating the reviewed file does to the active rules. The review
    // lists a file as overwritten only when its text differs from the active copy.
    const activeQuery = workspace.plan.queries.find((query) => query.name === workingCopy.name);
    const reviewChange = !review
        ? ''
        : !activeQuery
          ? 'Adds this new file.'
          : review.overwritten.includes(workingCopy.name)
            ? 'Replaces the active copy of this file.'
            : !activeQuery.author_enabled && review.enabled.includes(workingCopy.name)
              ? 'Same text as the active copy; activating enables it.'
              : 'Same text as the active copy; activating changes nothing.';
    const edited =
        workspace.plan.queries.find((query) => query.name === workingCopy.name) ??
        editedFileQuery(workingCopy.name, version);
    // The dependency window shows the declared prerequisites until the
    // prerequisites are checked, then the complete needed chain. Active queries
    // that already read this file's answer are added either way.
    const chain = preview
        ? { queries: preview.plan.queries, dependencies: preview.dependencies }
        : {
              queries: [...dependencies.filter((query) => selectedDependencies.has(query.name)), edited],
              dependencies: declarations
                  .filter((declaration) => dependencies.some((query) => query.name === declaration.name))
                  .map(
                      (declaration): Dependency => ({
                          prerequisite: declaration.name,
                          consumer: workingCopy.name,
                          type: 'query',
                          name: declaration.name,
                          parameter: declaration.alias,
                      })
                  ),
          };
    const consumerQueries = workspace.plan.queries.filter(
        (query) =>
            consumers.some((dependency) => dependency.consumer === query.name) &&
            !chain.queries.some((shown) => shown.name === query.name)
    );
    const graphWorkspace: Workspace = {
        ...workspace,
        plan: { ...workspace.plan, queries: [...chain.queries, ...consumerQueries] },
        dependencies: [...chain.dependencies, ...consumers],
    };
    const graphNames = graphWorkspace.plan.queries.map((query) => query.name);
    const neededPrerequisites = preview
        ? preview.plan.queries.filter((query) => query.name !== workingCopy.name).map((query) => query.name)
        : [];

    // The page refreshes the workspace while analysis runs, which gives this
    // component a new workspace object. Opening the editor reads the latest
    // one, but a refresh must not reset what the author is typing.
    const latestWorkspace = useRef(workspace);
    latestWorkspace.current = workspace;
    useEffect(() => {
        session.current++;
        abortController.current?.abort();
        if (!open) return;
        const controller = new AbortController();
        const epoch = session.current;
        const opened = latestWorkspace.current;
        setWorkingCopy({ ...(initial || defaultCopy(opened)), base_revision: opened.plan.revision });
        setOriginal(initial || defaultCopy(opened));
        setReceipt(undefined);
        setReview(undefined);
        setMessage(undefined);
        setBusy('');
        setPreview(undefined);
        setResumeName('');
        setCopies([]);
        listEditorWorkingCopies(controller.signal).then(
            (saved) => {
                if (!controller.signal.aborted && session.current === epoch) setCopies(saved);
            },
            () => {
                if (!controller.signal.aborted && session.current === epoch)
                    showWarning('Saved private copies are unavailable right now. You can still edit and test.');
            }
        );
        return () => {
            session.current++;
            controller.abort();
            abortController.current?.abort();
        };
    }, [open, initial, canEdit]);

    const updateCopy = (change: Partial<EditorWorkingCopy>) => {
        setWorkingCopy((previous) => {
            const next = { ...previous, ...change };
            // The text is the source of truth: when its // namespace: line
            // names another installed extension, the file moves to it, so the
            // Installed extension choice and the text never disagree.
            const textNamespace = next.text.match(/^\/\/[ \t]*namespace:[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*$/m)?.[1];
            const [fileNamespace, fileName = 'new_analysis.cypher'] = next.name.split('/');
            if (
                !change.name &&
                textNamespace &&
                textNamespace !== fileNamespace &&
                installedExtensions.some((extension) => extension.namespace === textNamespace)
            )
                next.name = `${textNamespace}/${fileName}`;
            if (next.name !== previous.name) next.revision = '';
            return next;
        });
        setPreview(undefined);
        setReceipt(undefined);
        setReview(undefined);
        setMessage(undefined);
    };
    // The dropdown reports the complete new list of prerequisites. Remove the
    // ones that were unchecked and add declarations for the newly checked ones.
    const changeDependencies = (next: string[]) => {
        if (!canEdit || busy) return;
        let text = workingCopy.text;
        const usedAliases = new Set(declarations.map((dependency) => dependency.alias));
        for (const name of selectedDependencies)
            if (!next.includes(name)) text = setEditorDependency(text, namespace, name, aliases[name], false);
        for (const name of next) {
            if (selectedDependencies.has(name)) continue;
            // Two prerequisites with the same file name in different
            // extensions get the extension name added to keep aliases unique.
            let alias = defaultAlias(name);
            if (usedAliases.has(alias)) alias = name.replace(/\.cypher$/, '').replace('/', '_');
            usedAliases.add(alias);
            text = setEditorDependency(text, namespace, name, alias, true);
        }
        updateCopy({ text });
    };
    const saveCopy = async () => {
        if (!canEdit || busy) return;
        const operation = beginRequest();
        setBusy('save');
        setMessage(undefined);
        try {
            const saved = await saveEditorWorkingCopy(workingCopy, operation.controller.signal);
            if (!operation.current()) return;
            setWorkingCopy(saved);
            setOriginal(saved);
            setCopies((previous) => [...previous.filter((copy) => copy.name !== saved.name), saved]);
            setResumeName(saved.name);
            setMessage({
                text: 'Private working copy saved. It does not change the upload draft.',
                severity: 'success',
            });
        } catch (error) {
            if (operation.current()) showError(safeEditorError(error, 'save this working copy'));
        } finally {
            if (operation.current()) setBusy('');
        }
    };
    const resumeCopy = async () => {
        if (!resumeName || busy) return;
        const operation = beginRequest();
        setBusy('resume');
        setMessage(undefined);
        try {
            const saved = await readEditorWorkingCopy(resumeName, operation.controller.signal);
            if (!operation.current()) return;
            // Keep the exact private source while testing against today's rules.
            setWorkingCopy({ ...saved, base_revision: workspace.plan.revision });
            setOriginal(saved);
            setReceipt(undefined);
            setReview(undefined);
            setPreview(undefined);
        } catch (error) {
            if (operation.current()) showError(safeEditorError(error, 'resume that working copy'));
        } finally {
            if (operation.current()) setBusy('');
        }
    };
    const testRequest = () => ({
        name: workingCopy.name,
        text: workingCopy.text,
        base_revision: workspace.plan.revision,
        schema_revision: workspace.plan.schema_revision,
    });
    const changedSinceOriginal = workingCopy.name !== original.name || workingCopy.text !== original.text;
    const revert = () => {
        if (!canEdit || busy) return;
        setWorkingCopy({ ...original, base_revision: workspace.plan.revision });
        setPreview(undefined);
        setReceipt(undefined);
        setReview(undefined);
        setMessage(undefined);
    };
    const test = async () => {
        if (!canEdit || busy || declarationProblem) return;
        const operation = beginRequest();
        setBusy('test');
        setMessage(undefined);
        setReceipt(undefined);
        setReview(undefined);
        try {
            // Pressing play checks the prerequisites first whenever the current
            // text has not been checked, so a separate review click is optional.
            let plan = preview;
            if (!plan) {
                plan = await previewEditorPlan(testRequest(), operation.controller.signal);
                if (!operation.current()) return;
                setPreview(plan);
            }
            // Every test recalculates the needed prerequisite chain against the
            // current graph, so results can never come from outdated answers.
            const result = await testEditorFile(testRequest(), operation.controller.signal);
            if (!operation.current()) return;
            setReceipt({ name: workingCopy.name, text: workingCopy.text, result });
        } catch (error) {
            if (operation.current()) showError(safeEditorError(error, 'test this file'));
        } finally {
            if (operation.current()) setBusy('');
        }
    };
    const cancelTest = () => {
        abortController.current?.abort();
        setBusy('');
        setReceipt(undefined);
        setReview(undefined);
        setMessage({ text: 'Test canceled. Your editor contents were kept.', severity: 'info' });
    };
    const reviewPrerequisites = async () => {
        if (!canEdit || busy || declarationProblem) return;
        const operation = beginRequest();
        setBusy('plan');
        setMessage(undefined);
        try {
            const plan = await previewEditorPlan(testRequest(), operation.controller.signal);
            if (operation.current()) setPreview(plan);
        } catch (error) {
            if (operation.current()) {
                setPreview(undefined);
                showError(safeEditorError(error, 'review prerequisites'));
            }
        } finally {
            if (operation.current()) setBusy('');
        }
    };
    const requestReview = async () => {
        if (!receipt || !tested || !canEdit || busy) return;
        const operation = beginRequest();
        setBusy('review');
        setMessage(undefined);
        try {
            const reviewed = await reviewEditorTest(receipt.result.test_id, operation.controller.signal);
            if (operation.current()) setReview(reviewed);
        } catch (error) {
            if (operation.current()) showError(safeEditorError(error, 'review this tested file'));
        } finally {
            if (operation.current()) setBusy('');
        }
    };
    const activate = async () => {
        if (!receipt || !review || !tested || !canEdit || busy) return;
        const operation = beginRequest();
        setBusy('activate');
        setMessage(undefined);
        try {
            await activateEditorTest(receipt.result.test_id, review.revision, operation.controller.signal);
            if (operation.current()) onActivated(workingCopy.name);
        } catch (error) {
            if (operation.current()) showError(safeEditorError(error, 'activate this reviewed file'));
        } finally {
            if (operation.current()) setBusy('');
        }
    };
    const close = () => {
        session.current++;
        abortController.current?.abort();
        setBusy('');
        onClose();
    };

    return (
        <Dialog open={open} onClose={close} fullWidth maxWidth='lg' aria-labelledby='analysis-file-editor-title'>
            <DialogTitle id='analysis-file-editor-title'>Analysis file editor</DialogTitle>
            <DialogContent>
                <Stack spacing={2} className='pt-1'>
                    {!canEdit && (
                        <Alert severity='info'>
                            You can inspect this file, but permission is required to test or save changes.
                        </Alert>
                    )}
                    {message && <Alert severity={message.severity}>{message.text}</Alert>}
                    <Alert severity='info'>Private working copies are separate from the pending upload draft.</Alert>
                    <Stack direction={{ xs: 'column', md: 'row' }} spacing={1}>
                        <FormControl className='min-w-[190px]'>
                            <InputLabel id='editor-extension-label'>Installed extension</InputLabel>
                            <Select
                                labelId='editor-extension-label'
                                label='Installed extension'
                                value={workingCopy.name.split('/')[0] || ''}
                                onChange={(event) => {
                                    const fileName = workingCopy.name.split('/').pop() || 'new_analysis.cypher';
                                    updateCopy({
                                        name: `${event.target.value}/${fileName}`,
                                        text: setEditorHeader(workingCopy.text, 'namespace', event.target.value),
                                    });
                                }}
                                disabled={!canEdit || !!busy}>
                                {installedExtensions.map((extension) => (
                                    <MenuItem key={extension.namespace} value={extension.namespace}>
                                        {extension.display_name || extension.namespace}
                                    </MenuItem>
                                ))}
                            </Select>
                        </FormControl>
                        <TextField
                            fullWidth
                            label='File name'
                            value={workingCopy.name.split('/').pop()}
                            onChange={(event) => updateCopy({ name: `${namespace}/${event.target.value}` })}
                            inputProps={{ 'aria-label': 'Analysis file name' }}
                            disabled={!canEdit || !!busy}
                        />
                        <TextField
                            label='Version'
                            value={version}
                            onChange={(event) =>
                                updateCopy({
                                    text: setEditorHeader(workingCopy.text, 'version', event.target.value),
                                })
                            }
                            disabled={!canEdit || !!busy}
                        />
                        <FormControl className='min-w-[250px]'>
                            <InputLabel id='resume-working-copy-label'>Resume saved copy</InputLabel>
                            <Select
                                labelId='resume-working-copy-label'
                                label='Resume saved copy'
                                value={resumeName}
                                onChange={(event) => setResumeName(event.target.value)}>
                                <MenuItem value=''>Choose a private copy</MenuItem>
                                {copies.map((copy) => (
                                    <MenuItem key={copy.name} value={copy.name}>
                                        {copy.name}
                                    </MenuItem>
                                ))}
                            </Select>
                        </FormControl>
                        <Button onClick={() => void resumeCopy()} disabled={!resumeName || busy === 'resume'}>
                            Resume
                        </Button>
                    </Stack>
                    <Stack
                        direction='row'
                        spacing={1}
                        alignItems='center'
                        flexWrap='wrap'
                        useFlexGap
                        aria-label='Test and save'>
                        {/* Disabled buttons cannot show tooltips, so each span shows it instead. */}
                        <Tooltip title='Test file'>
                            <span>
                                <IconButton
                                    aria-label='Test file'
                                    color='primary'
                                    onClick={() => void test()}
                                    disabled={!canEdit || !!busy || !!declarationProblem}>
                                    {busy === 'test' ? (
                                        <CircularProgress size={16} aria-label='Testing' />
                                    ) : (
                                        <FontAwesomeIcon icon={faPlay} size='sm' />
                                    )}
                                </IconButton>
                            </span>
                        </Tooltip>
                        {busy === 'test' && <Button onClick={cancelTest}>Cancel test</Button>}
                        <Tooltip title='Save private copy'>
                            <span>
                                <IconButton
                                    aria-label='Save private copy'
                                    onClick={() => void saveCopy()}
                                    disabled={!canEdit || !!busy}>
                                    {busy === 'save' ? (
                                        <CircularProgress size={16} aria-label='Saving' />
                                    ) : (
                                        <FontAwesomeIcon icon={faFloppyDisk} size='sm' />
                                    )}
                                </IconButton>
                            </span>
                        </Tooltip>
                        <Tooltip title='Revert'>
                            <span>
                                <IconButton
                                    aria-label='Revert'
                                    onClick={revert}
                                    disabled={!canEdit || !!busy || !changedSinceOriginal}>
                                    <FontAwesomeIcon icon={faBackward} size='sm' />
                                </IconButton>
                            </span>
                        </Tooltip>
                    </Stack>
                    <TextField
                        label='Cypher'
                        value={workingCopy.text}
                        onChange={(event) => updateCopy({ text: event.target.value })}
                        multiline
                        minRows={12}
                        fullWidth
                        InputProps={{ readOnly: !canEdit || !!busy }}
                        inputProps={{ 'aria-label': 'Cypher' }}
                    />
                    {receipt && tested && (
                        <Box component='section' aria-label='Test results'>
                            <Typography component='h2' variant='subtitle1'>
                                Test results
                            </Typography>
                            {!hasRows && <Typography>The test returned no rows.</Typography>}
                            {hasRows && (
                                <>
                                    <Typography variant='body2'>
                                        {receipt.result.truncated || receipt.result.total_rows > 500
                                            ? `Showing ${Math.min(receipt.result.rows.length, 500)} preview rows of ${receipt.result.total_rows}. The complete successful test can be reused.`
                                            : `${receipt.result.total_rows} result ${receipt.result.total_rows === 1 ? 'row' : 'rows'}.`}
                                    </Typography>
                                    <Box className='max-h-[300px] overflow-auto'>
                                        <Table size='small' stickyHeader aria-label='Editor test result rows'>
                                            <TableHead>
                                                <TableRow>
                                                    {resultColumns.map((column) => (
                                                        <TableCell key={column}>{column}</TableCell>
                                                    ))}
                                                </TableRow>
                                            </TableHead>
                                            <TableBody>
                                                {receipt.result.rows.slice(0, 500).map((row, rowIndex) => (
                                                    <TableRow key={rowIndex}>
                                                        {resultColumns.map((column) => (
                                                            <TableCell key={column}>
                                                                {readableValue(row[column])}
                                                            </TableCell>
                                                        ))}
                                                    </TableRow>
                                                ))}
                                            </TableBody>
                                        </Table>
                                    </Box>
                                </>
                            )}
                            <Box>
                                <Button onClick={() => void requestReview()} disabled={!canEdit || !!busy}>
                                    {busy === 'review' ? 'Reviewing…' : 'Review tested file'}
                                </Button>
                            </Box>
                            {review && (
                                <Stack spacing={0.5} aria-label='Review details'>
                                    <Typography variant='body2'>
                                        <strong>File:</strong> {workingCopy.name}
                                    </Typography>
                                    <Typography variant='body2'>
                                        <strong>Review revision:</strong> {review.revision}
                                    </Typography>
                                    <Typography variant='body2'>
                                        <strong>Change:</strong> {reviewChange}
                                    </Typography>
                                    <Typography variant='body2'>
                                        <strong>Removed:</strong> {review.removed.join(', ') || 'None'}
                                    </Typography>
                                    <Typography variant='body2'>
                                        <strong>Dependencies reviewed:</strong> {review.dependencies.length}
                                    </Typography>
                                    <Typography variant='body2'>
                                        <strong>Recalculated outputs:</strong>{' '}
                                        {review.recalculated_relationships.join(', ') || 'None'}
                                    </Typography>
                                    <Box className='pt-1'>
                                        <Button
                                            variant='contained'
                                            onClick={() => void activate()}
                                            disabled={!canEdit || !!busy}>
                                            {busy === 'activate' ? 'Activating…' : 'Activate reviewed file'}
                                        </Button>
                                    </Box>
                                </Stack>
                            )}
                        </Box>
                    )}
                    <Box component='section' aria-label='Dependencies'>
                        <Typography component='h2' variant='subtitle1'>
                            Dependencies
                        </Typography>
                        <Typography variant='body2'>
                            Select prerequisites here or write their declarations in the file. Each query prerequisite
                            adds a line such as // depends-on: query:PoC/users as users; use $users in your Cypher, and
                            change the name after as in the Cypher box if you want a different one.
                        </Typography>
                        <Autocomplete
                            multiple
                            disableCloseOnSelect
                            options={dependencyChoices}
                            value={declarations.map((dependency) => dependency.name)}
                            onChange={(_, next) => changeDependencies(next)}
                            disabled={!canEdit || !!busy}
                            renderOption={({ key, ...optionProps }, option, { selected }) => (
                                <li key={key} {...optionProps}>
                                    <Checkbox size='small' checked={selected} className='mr-1' />
                                    {option}
                                </li>
                            )}
                            renderInput={(params) => (
                                <TextField {...params} label='Prerequisites' placeholder='Filter by name' />
                            )}
                            className='pt-2'
                        />
                        {declarationProblem && <Alert severity='warning'>{declarationProblem}</Alert>}
                        <Button
                            onClick={() => void reviewPrerequisites()}
                            disabled={!canEdit || !!busy || !!declarationProblem}>
                            Review prerequisites
                        </Button>
                        {preview?.unused?.length ? (
                            <Alert severity='warning' aria-label='Unused dependencies'>
                                Declared but never used: {preview.unused.join(', ')}. Remove them from the // depends-on
                                lines, or use them in the Cypher.
                            </Alert>
                        ) : null}
                        {neededPrerequisites.length > 0 && (
                            <Typography variant='body2' aria-label='Needed prerequisites'>
                                <strong>Needed prerequisites:</strong> {neededPrerequisites.join(', ')}
                            </Typography>
                        )}
                        {graphWorkspace.dependencies.length > 0 && (
                            <Box
                                aria-label='Dependency graph'
                                className='relative h-[360px] my-2 rounded border overflow-hidden'>
                                <QueryGraph
                                    key={JSON.stringify([graphNames, graphWorkspace.dependencies.map(dependencyKey)])}
                                    workspace={graphWorkspace}
                                    visible={new Set(graphNames)}
                                    focused={workingCopy.name}
                                    numberingQueries={[...workspace.plan.queries, edited]}
                                    controlsBottom={8}
                                />
                            </Box>
                        )}
                        <Typography variant='body2'>
                            <strong>Schema:</strong> configure installed extensions in{' '}
                            <Link href='/ui/administration/opengraph-management'>OpenGraph Management</Link>.
                        </Typography>
                    </Box>
                </Stack>
            </DialogContent>
            <DialogActions>
                <Button onClick={close}>Close</Button>
            </DialogActions>
        </Dialog>
    );
}
