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

import { Box, Button, Stack, Typography } from '@mui/material';
import { ReactNode, useRef } from 'react';

export default function EntityPanel({
    width,
    onWidth,
    onClose,
    children,
}: {
    width: number;
    onWidth: (width: number) => void;
    onClose: () => void;
    children: ReactNode;
}) {
    const panel = useRef<HTMLElement>(null);
    const drag = useRef<{ x: number; width: number }>();
    const resize = (next: number) => {
        const maximum = Math.min(800, (panel.current?.parentElement?.clientWidth || 1200) - 64);
        onWidth(Math.max(Math.min(320, maximum), Math.min(maximum, next)));
    };
    return (
        <Box
            ref={panel}
            component='aside'
            aria-label='Analysis entity panel'
            className='absolute z-30 top-4 right-4 bottom-4 max-w-[calc(100%-32px)] bg-neutral-2 border rounded-lg shadow-outer-1 flex flex-col'
            style={{ width }}>
            <button
                type='button'
                // A focusable separator follows the ARIA window-splitter pattern.
                // eslint-disable-next-line jsx-a11y/no-interactive-element-to-noninteractive-role
                role='separator'
                aria-label='Resize entity panel'
                aria-orientation='vertical'
                aria-valuemin={320}
                aria-valuemax={800}
                aria-valuenow={Math.round(width)}
                className='absolute -left-2 top-0 bottom-0 w-3 cursor-ew-resize touch-none focus-visible:outline focus-visible:outline-2'
                onPointerDown={(event) => {
                    event.currentTarget.setPointerCapture(event.pointerId);
                    drag.current = { x: event.clientX, width: panel.current?.clientWidth || width };
                }}
                onPointerMove={(event) => {
                    if (drag.current) resize(drag.current.width + drag.current.x - event.clientX);
                }}
                onPointerUp={(event) => {
                    drag.current = undefined;
                    event.currentTarget.releasePointerCapture(event.pointerId);
                }}
                onLostPointerCapture={() => {
                    drag.current = undefined;
                }}
                onKeyDown={(event) => {
                    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
                        event.preventDefault();
                        resize(width + (event.key === 'ArrowLeft' ? 32 : -32));
                    }
                }}>
                <span className='block h-10 w-1 mx-auto rounded bg-neutral-4' />
            </button>
            <Stack direction='row' alignItems='center' className='px-3 py-2 shrink-0'>
                <Typography className='flex-1 font-bold'>Entity information</Typography>
                <Button className='text-link' aria-label='Close entity panel' onClick={onClose}>
                    Close
                </Button>
            </Stack>
            <Box className='overflow-auto min-h-0 flex-1'>{children}</Box>
        </Box>
    );
}
