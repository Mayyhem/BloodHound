// Copyright 2023 Specter Ops, Inc.
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

import { Box, Button, Dialog, DialogActions, DialogContent } from '@mui/material';
import { useState } from 'react';
import FileDrop from '../FileDrop';
import FileStatusListItem from '../FileStatusListItem';
import { FileForIngest, FileStatus, FileUploadStep } from './types';
import { validateFile } from '../..';

const FileUploadDialog: React.FC<{
    files: FileForIngest[];
    open: boolean;
    submitDisabled: boolean;
    onClose: () => void;
    onAppendFiles: (files: FileForIngest[]) => void;
    onRemoveFile: (index: number) => void;
    onUpload: () => void;
    onExited?: () => void;
    uploadMessage?: string;
}> = ({
    files,
    open,
    submitDisabled,
    onClose,
    onAppendFiles,
    onRemoveFile,
    onUpload,
    onExited = () => {},
    uploadMessage = '',
}) => {
    const [fileUploadStep, setFileUploadStep] = useState<FileUploadStep>(FileUploadStep.ADD_FILES);

    const handleFileDrop = async (files: FileList | null) => {
        if (files && files.length > 0) {
            const validatedFiles: FileForIngest[] = await Promise.all(
                [...files].map(async (file) => {
                    const errors = await validateFile(file);
                    if ((errors?.length ?? 0) > 0) {
                        return { file, errors, status: FileStatus.READY };
                    } else {
                        return { file, status: FileStatus.READY };
                    }
                })
            );
            onAppendFiles(validatedFiles);
        }
    };

    const handleClose = () => {
        onClose();
    };

    const handleSubmit = () => {
        if (fileUploadStep === FileUploadStep.ADD_FILES) {
            setFileUploadStep(FileUploadStep.CONFIRMATION);
        } else if (fileUploadStep === FileUploadStep.CONFIRMATION) {
            setFileUploadStep(FileUploadStep.UPLOAD);
            onUpload();
        }
    };

    return (
        <Dialog
            open={open}
            fullWidth={true}
            maxWidth={'sm'}
            TransitionProps={{
                onExited: () => {
                    setFileUploadStep(FileUploadStep.ADD_FILES);
                    onExited();
                },
            }}>
            <DialogContent>
                <>
                    {fileUploadStep === FileUploadStep.ADD_FILES && (
                        <FileDrop onDrop={handleFileDrop} acceptedFileTypes={['json', 'zip']} />
                    )}
                    {(fileUploadStep === FileUploadStep.CONFIRMATION || fileUploadStep === FileUploadStep.UPLOAD) && (
                        <Box fontSize={20} marginBottom={5}>
                            {uploadMessage ||
                                'The following files will be uploaded and ingested into BloodHound. This cannot be undone.'}
                        </Box>
                    )}

                    {files.length > 0 && (
                        <Box sx={{ marginTop: 1, marginBottom: 1 }}>
                            <Box sx={{ backgroundColor: 'black', color: 'white', fontWeight: 'bold', padding: '4px' }}>
                                Files
                            </Box>
                            {files.map((file, index) => {
                                return (
                                    <FileStatusListItem file={file} key={index} onRemove={() => onRemoveFile(index)} />
                                );
                            })}
                        </Box>
                    )}

                    {fileUploadStep === FileUploadStep.CONFIRMATION && (
                        <Box fontSize={20} marginTop={3}>
                            Press "Upload" to continue.
                        </Box>
                    )}
                </>
            </DialogContent>
            <DialogActions>
                {(fileUploadStep === FileUploadStep.ADD_FILES || fileUploadStep === FileUploadStep.CONFIRMATION) && (
                    <>
                        <Button
                            autoFocus
                            color='inherit'
                            onClick={handleClose}
                            data-testid='confirmation-dialog_button-no'>
                            Cancel
                        </Button>
                        <Button
                            color='primary'
                            disabled={submitDisabled}
                            onClick={handleSubmit}
                            data-testid='confirmation-dialog_button-yes'>
                            Upload
                        </Button>
                    </>
                )}
                {fileUploadStep === FileUploadStep.UPLOAD && (
                    <Button
                        color='primary'
                        onClick={handleClose}
                        disabled={submitDisabled}
                        data-testid='confirmation-dialog_button-yes'>
                        {submitDisabled ? 'Uploading Files' : 'Close'}
                    </Button>
                )}
            </DialogActions>
        </Dialog>
    );
};

export default FileUploadDialog;
