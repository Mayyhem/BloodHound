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

import { faCube, faLink, faTable } from '@fortawesome/free-solid-svg-icons';
import { roleIconSvg, roleKey } from './roleIcons';

// The outline of a Font Awesome icon, as it appears in the drawn picture.
const outline = (icon: typeof faLink) => icon.icon[4] as string;
const iconCount = (svg: string) => svg.split('<g ').length - 1;

describe('role icons', () => {
    it('draws an empty ring for a query that produces none of the three things', () => {
        const svg = roleIconSvg([]);
        expect(svg).toContain('<circle');
        expect(iconCount(svg)).toBe(0);
        expect(roleKey([])).toBe('none');
    });

    it('draws the matching icon for each single role', () => {
        expect(roleIconSvg(['relationship'])).toContain(outline(faLink));
        expect(roleIconSvg(['node'])).toContain(outline(faCube));
        expect(roleIconSvg(['query'])).toContain(outline(faTable));
        expect(iconCount(roleIconSvg(['query']))).toBe(1);
    });

    it('draws two icons side by side and three in a triangle', () => {
        const pair = roleIconSvg(['relationship', 'query']);
        expect(iconCount(pair)).toBe(2);
        expect(pair).toContain(outline(faLink));
        expect(pair).toContain(outline(faTable));
        // The middle of each drawn icon: where it starts plus half its scaled
        // height. Icons have different shapes, so their middles are compared.
        const middles = (svg: string, icons: (typeof faLink)[]) =>
            [...svg.matchAll(/translate\(([\d.-]+) ([\d.-]+)\) scale\(([\d.e-]+)\)/g)].map(
                (match, index) => Number(match[2]) + (icons[index].icon[1] * Number(match[3])) / 2
            );
        // Side by side: both icons are centered on the same line.
        const [first, second] = middles(pair, [faLink, faTable]);
        expect(first).toBeCloseTo(second);

        const triangle = roleIconSvg(['relationship', 'node', 'query']);
        expect(iconCount(triangle)).toBe(3);
        // Triangle: one icon above two that share a lower line.
        const [top, left, right] = middles(triangle, [faLink, faCube, faTable]);
        expect(left).toBeCloseTo(right);
        expect(top).toBeLessThan(left);
        expect(roleKey(['relationship', 'node', 'query'])).toBe('relationship+node+query');
    });
});
