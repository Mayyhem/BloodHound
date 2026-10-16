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

import { Button, Stack } from '@mui/material';
import { RuleAction } from './types';

export default function RuleActions({
    names,
    disabled,
    onAction,
    bulk = false,
    authorEnabled,
}: {
    names: string[];
    disabled: boolean;
    onAction: (action: RuleAction, names: string[]) => void;
    bulk?: boolean;
    // For a single rule: whether its author switch is on. An enabled rule
    // offers only Disable and a disabled rule only Enable. Bulk actions on a
    // mixed selection leave this out and offer both.
    authorEnabled?: boolean;
}) {
    return (
        <Stack direction='row' spacing={1}>
            {authorEnabled !== true && (
                <Button
                    className='text-link'
                    disabled={disabled || !names.length}
                    onClick={() => onAction('enable', names)}>
                    Enable{bulk ? ' selected' : ''}
                </Button>
            )}
            {authorEnabled !== false && (
                <Button
                    className='text-link'
                    disabled={disabled || !names.length}
                    onClick={() => onAction('disable', names)}>
                    Disable{bulk ? ' selected' : ''}
                </Button>
            )}
            <Button color='error' disabled={disabled || !names.length} onClick={() => onAction('remove', names)}>
                Delete{bulk ? ' selected' : ''}
            </Button>
        </Stack>
    );
}
