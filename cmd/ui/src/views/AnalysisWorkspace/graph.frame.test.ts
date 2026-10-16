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

import { cameraToFrame } from './graph';

// A stand-in for Sigma's coordinate conversion: one framed unit is 1,000
// screen pixels at zoom ratio 1, and the camera position appears at the
// canvas center.
const width = 1200;
const height = 800;
const positions: Record<string, { x: number; y: number }> = {
    first: { x: 0.3, y: 0.5 },
    second: { x: 0.7, y: 0.5 },
};
const view = {
    getDimensions: () => ({ width, height }),
    getNodeDisplayData: (node: unknown) => positions[node as string] as never,
    framedGraphToViewport: (
        point: { x: number; y: number },
        override?: { cameraState?: { x: number; y: number; ratio: number } }
    ) => {
        const camera = override!.cameraState!;
        return {
            x: ((point.x - camera.x) * 1000) / camera.ratio + width / 2,
            y: ((point.y - camera.y) * 1000) / camera.ratio + height / 2,
        };
    },
    viewportToFramedGraph: (
        point: { x: number; y: number },
        override?: { cameraState?: { x: number; y: number; ratio: number } }
    ) => {
        const camera = override!.cameraState!;
        return {
            x: ((point.x - width / 2) * camera.ratio) / 1000 + camera.x,
            y: ((point.y - height / 2) * camera.ratio) / 1000 + camera.y,
        };
    },
};

it('fits the shown queries and centers them in the uncovered part of the canvas', () => {
    const covered = { top: 200, right: 400, bottom: 100, left: 0 };
    const state = cameraToFrame(view as never, ['first', 'second'], covered)!;
    // 400 px of queries plus 160 px label room must fit 800 px of free width.
    expect(state.ratio).toBeCloseTo(560 / 800);
    // The middle of the queries lands in the middle of the uncovered area.
    const middle = view.framedGraphToViewport({ x: 0.5, y: 0.5 }, { cameraState: state });
    expect(middle.x).toBeCloseTo(400);
    expect(middle.y).toBeCloseTo(450);
});

it('uses the whole canvas when panels cover nearly all of it, and skips missing queries', () => {
    const state = cameraToFrame(view as never, ['first', 'missing'], { top: 790, right: 0, bottom: 0, left: 0 })!;
    const point = view.framedGraphToViewport(positions.first, { cameraState: state });
    expect(point.x).toBeCloseTo(width / 2);
    expect(point.y).toBeCloseTo(height / 2);
    expect(cameraToFrame(view as never, ['missing'], { top: 0, right: 0, bottom: 0, left: 0 })).toBeUndefined();
});
