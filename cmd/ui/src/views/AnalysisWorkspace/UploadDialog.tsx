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
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    FormControl,
    InputLabel,
    MenuItem,
    Select,
    Stack,
    Typography,
} from '@mui/material';
import { useEffect, useState } from 'react';
import { downloadText } from './api';
import { Workspace } from './types';
import { readUploads } from './uploads';
import { RuleManagement } from './useRuleManagement';

// Opened by the Upload button in the graph's bottom-left corner. Uploaded
// files become an inactive draft; nothing changes until the draft is reviewed
// and activated in the workspace's top panel.
export default function UploadDialog({
    open,
    onClose,
    workspace,
    management,
    canEdit,
}: {
    open: boolean;
    onClose: () => void;
    workspace: Workspace;
    management: RuleManagement;
    canEdit: boolean;
}) {
    const [uploads, setUploads] = useState<Record<string, string>>({});
    const [choices, setChoices] = useState<Record<string, string>>({});
    const [uploadError, setUploadError] = useState('');
    const [reading, setReading] = useState(false);
    const [submitted, setSubmitted] = useState(false);
    const unavailable = management.busy || management.locked || reading || !canEdit;
    const savedFiles = {
        ...Object.fromEntries(workspace.plan.queries.map((query) => [query.name, query.text])),
        ...management.draft?.files,
    };
    const changed = Object.keys(uploads).filter((name) => uploads[name] !== savedFiles[name]);
    const replacements = changed.filter((name) => Object.hasOwn(savedFiles, name));
    const chosen = changed.filter((name) => !replacements.includes(name) || choices[name] === 'overwrite');
    const review = management.pending?.review;
    // A ready upload opens its review in the top panel, so step out of the way.
    // An invalid draft keeps this dialog open to show its problems.
    useEffect(() => setSubmitted(false), [open]);
    useEffect(() => {
        if (submitted && review) {
            setSubmitted(false);
            onClose();
        }
    }, [submitted, review, onClose]);
    return (
        <Dialog open={open} onClose={onClose} fullWidth maxWidth='sm' aria-labelledby='analysis-upload-title'>
            <DialogTitle id='analysis-upload-title'>Upload analysis files</DialogTitle>
            <DialogContent>
                <Stack spacing={1}>
                    <Typography variant='body2'>
                        Choose .cypher files, or .zip files containing them. Each file needs namespace and version
                        comments. Maximum: 32 files, 1 MiB per file, 4 MiB total. Schemas belong in OpenGraph
                        Management.
                    </Typography>
                    {!canEdit && (
                        <Alert severity='info'>OpenGraph write permission is required to upload or change rules.</Alert>
                    )}
                    {review && (
                        <Alert severity='info'>
                            A review is open. Activate or discard it before uploading more files.
                        </Alert>
                    )}
                    {management.draftError && <Alert severity='error'>{management.draftError}</Alert>}
                    <input
                        aria-label='Analysis files'
                        type='file'
                        multiple
                        accept='.cypher,.zip'
                        disabled={unavailable || !management.draft || !!review}
                        onChange={async (event) => {
                            const files = Array.from(event.target.files ?? []);
                            setReading(true);
                            setUploadError('');
                            setChoices({});
                            setUploads({});
                            try {
                                setUploads(await readUploads(files));
                            } catch (failure) {
                                setUploadError((failure as Error).message);
                            } finally {
                                setReading(false);
                            }
                        }}
                    />
                    {uploadError && <Alert severity='error'>{uploadError}</Alert>}
                    {Object.keys(uploads).length > 0 && !changed.length && (
                        <Alert severity='info'>Every selected file matches the saved copy. Nothing to upload.</Alert>
                    )}
                    {changed
                        .filter((name) => !replacements.includes(name))
                        .map((name) => (
                            <Typography key={name}>
                                <strong>New file:</strong> {name}
                            </Typography>
                        ))}
                    {replacements.map((name) => (
                        <FormControl size='small' key={name}>
                            <InputLabel id={`overwrite-${name}`}>{name}</InputLabel>
                            <Select
                                labelId={`overwrite-${name}`}
                                label={name}
                                value={choices[name] || ''}
                                disabled={unavailable || !!review}
                                onChange={(event) => setChoices({ ...choices, [name]: event.target.value })}>
                                <MenuItem value='overwrite'>Overwrite this file</MenuItem>
                                <MenuItem value='skip'>Keep saved file</MenuItem>
                            </Select>
                        </FormControl>
                    ))}
                    <Button
                        variant='contained'
                        disabled={
                            unavailable || !!review || !chosen.length || replacements.some((name) => !choices[name])
                        }
                        onClick={async () => {
                            await management.upload(
                                Object.fromEntries(chosen.map((name) => [name, uploads[name]])),
                                replacements.filter((name) => choices[name] === 'overwrite')
                            );
                            setUploads({});
                            setChoices({});
                            setSubmitted(true);
                        }}>
                        Save upload and review
                    </Button>
                    {management.error && <Alert severity='error'>{management.error}</Alert>}
                    {management.draft?.revision && (
                        <>
                            <Typography className='[overflow-wrap:anywhere]'>
                                <strong>Draft revision:</strong> {management.draft.revision}
                            </Typography>
                            <Alert severity={management.draft.ready ? 'info' : 'warning'}>
                                {management.draft.ready
                                    ? 'Inactive draft is ready for review.'
                                    : management.draft.validation_error}
                            </Alert>
                            {Object.entries(management.draft.files).map(([name, text]) => (
                                <Button
                                    key={name}
                                    className='text-link justify-start'
                                    onClick={() => downloadText(name, text)}>
                                    Download draft {name}
                                </Button>
                            ))}
                            <Stack direction='row' spacing={1}>
                                <Button
                                    className='text-link'
                                    disabled={unavailable || !!review || !management.draft.ready}
                                    onClick={() => {
                                        void management.reviewDraft();
                                        onClose();
                                    }}>
                                    Review saved draft
                                </Button>
                                <Button color='error' disabled={unavailable || !!review} onClick={management.discard}>
                                    Discard saved draft
                                </Button>
                            </Stack>
                        </>
                    )}
                </Stack>
            </DialogContent>
            <DialogActions>
                <Button className='text-link' onClick={onClose}>
                    Close
                </Button>
            </DialogActions>
        </Dialog>
    );
}
