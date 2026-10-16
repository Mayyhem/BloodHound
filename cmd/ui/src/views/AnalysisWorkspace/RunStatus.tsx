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

import { Alert, CircularProgress, Link } from '@mui/material';
import { Link as RouterLink } from 'react-router-dom';
import { ROUTE_ADMINISTRATION_FILE_INGEST } from 'src/routes/constants';
import { RunPhase } from './model';

// One status line for the whole run, shown in the top panel. While anything is
// in progress it shows a spinner; when finished, the result in its color, with
// a link to the step that failed.
export default function RunStatus({ phase, onFocus }: { phase: RunPhase; onFocus: (name: string) => void }) {
    return (
        <Alert
            severity={phase.severity}
            aria-label='Analysis status'
            icon={phase.active ? <CircularProgress size={18} aria-label='Analysis in progress' /> : undefined}>
            {phase.failedQuery && (
                <>
                    Failed at{' '}
                    <Link component='button' onClick={() => onFocus(phase.failedQuery!)} className='align-baseline'>
                        {phase.failedQuery}
                    </Link>
                    .{' '}
                </>
            )}
            {phase.text}
            {phase.slow && (
                <>
                    {' '}
                    <Link component={RouterLink} to={ROUTE_ADMINISTRATION_FILE_INGEST}>
                        Open File Ingest
                    </Link>
                </>
            )}
        </Alert>
    );
}
