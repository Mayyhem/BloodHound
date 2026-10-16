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

import { ReactNode, useRef } from 'react';

interface Props {
    height: number;
    onHeight: (height: number) => void;
    panelOpen: boolean;
    children: ReactNode;
}

// The graph keeps its full canvas. Like Explore, this panel sits over the
// bottom and leaves room for details on the right. Resizing changes only height.
export default function BottomPanel({ height, onHeight, panelOpen, children }: Props) {
    const panel = useRef<HTMLDivElement>(null);
    const drag = useRef<{ y: number; height: number }>();
    const resize = (next: number) => {
        const available = panel.current?.parentElement?.clientHeight || 800;
        onHeight(Math.max(160, Math.min(available - 100, next)));
    };
    return (
        <div
            ref={panel}
            data-testid='analysis-bottom-panel'
            className={`absolute z-10 bottom-4 left-4 bg-neutral-2 rounded-lg shadow-outer-1 border flex flex-col min-h-0 ${panelOpen ? 'right-4 md:right-[var(--analysis-panel-space)]' : 'right-4'}`}
            style={{ height, maxHeight: 'calc(100% - 100px)' }}>
            {/* A focusable separator is the accessible control for changing panel size. */}
            <button
                type='button'
                // A focusable separator follows the ARIA window-splitter pattern.
                // eslint-disable-next-line jsx-a11y/no-interactive-element-to-noninteractive-role
                role='separator'
                tabIndex={0}
                aria-label='Resize results panel'
                aria-orientation='horizontal'
                aria-valuemin={160}
                aria-valuemax={panel.current?.parentElement?.clientHeight || 800}
                aria-valuenow={Math.round(height)}
                className='h-3 shrink-0 cursor-ns-resize touch-none flex items-center justify-center focus-visible:outline focus-visible:outline-2'
                onPointerDown={(event) => {
                    event.currentTarget.setPointerCapture(event.pointerId);
                    drag.current = { y: event.clientY, height: panel.current?.clientHeight || height };
                }}
                onPointerMove={(event) => {
                    if (drag.current) resize(drag.current.height + drag.current.y - event.clientY);
                }}
                onPointerUp={(event) => {
                    drag.current = undefined;
                    event.currentTarget.releasePointerCapture(event.pointerId);
                }}
                onLostPointerCapture={() => {
                    drag.current = undefined;
                }}
                onKeyDown={(event) => {
                    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
                        event.preventDefault();
                        resize(height + (event.key === 'ArrowUp' ? 32 : -32));
                    }
                }}>
                <span className='h-1 w-10 rounded bg-neutral-4' />
            </button>
            {children}
        </div>
    );
}
