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

import { IconDefinition, faCube, faLink, faTable } from '@fortawesome/free-solid-svg-icons';
import { QueryRole } from './model';

// The icon drawn for each thing a query can produce.
const roleIcons: Record<QueryRole, IconDefinition> = {
    relationship: faLink,
    node: faCube,
    query: faTable,
};

// Where each icon sits inside the node's 100 × 100 drawing area, and how wide
// it is. One icon fills the middle; two sit side by side; three form a
// triangle, one on top and two below.
const placements: Record<number, { x: number; y: number; size: number }[]> = {
    1: [{ x: 50, y: 50, size: 56 }],
    2: [
        { x: 28, y: 50, size: 38 },
        { x: 72, y: 50, size: 38 },
    ],
    3: [
        { x: 50, y: 28, size: 34 },
        { x: 29, y: 68, size: 34 },
        { x: 71, y: 68, size: 34 },
    ],
};

// roleIconSvg draws the picture shown inside a query's node circle, in black
// like the extension numbers it replaces. A query that produces none of these
// (for example one that only changes existing data) shows an empty ring.
export function roleIconSvg(roles: QueryRole[]) {
    const open = '<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">';
    if (!roles.length)
        return `${open}<circle cx="50" cy="50" r="22" fill="none" stroke="#000000" stroke-width="8"/></svg>`;
    const icons = roles.map((role, index) => {
        const place = placements[roles.length][index];
        // Each Font Awesome icon has its own width and height and its outline.
        const [width, height, , , outline] = roleIcons[role].icon;
        const scale = place.size / Math.max(width, height);
        const paths = (Array.isArray(outline) ? outline : [outline])
            .map((path) => `<path d="${path}" fill="#000000"/>`)
            .join('');
        const left = place.x - (width * scale) / 2;
        const top = place.y - (height * scale) / 2;
        return `<g transform="translate(${left} ${top}) scale(${scale})">${paths}</g>`;
    });
    return `${open}${icons.join('')}</svg>`;
}

// The same set of roles always gives the same key, so queries with the same
// roles share one image.
export const roleKey = (roles: QueryRole[]) => roles.join('+') || 'none';
