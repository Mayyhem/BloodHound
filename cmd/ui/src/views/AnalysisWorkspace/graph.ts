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

import { MultiDirectedGraph } from 'graphology';
import type Sigma from 'sigma';
import type { CameraState } from 'sigma/types';
import { MAX_CAMERA_RATIO, MIN_CAMERA_RATIO } from 'src/ducks/graph/utils';
import { sequentialLayout } from 'src/rendering/utils/sequentialLayout';
import { DEFAULT_GRAPH_EDGE_SIZE, DEFAULT_GRAPH_NODE_SIZE } from 'src/rendering/utils/utils';
import { dependencyKey, queryLabel } from './model';
import { Workspace } from './types';

export function buildQueryGraph(workspace: Workspace) {
    const graph = new MultiDirectedGraph();
    for (const query of workspace.plan.queries) {
        graph.addNode(query.name, {
            label: `${queryLabel(query.name)} · ${query.namespace}`,
            type: 'combined',
            size: DEFAULT_GRAPH_NODE_SIZE,
            x: 0,
            y: 0,
        });
    }
    for (const dependency of workspace.dependencies) {
        if (!graph.hasNode(dependency.prerequisite) || !graph.hasNode(dependency.consumer)) continue;
        // Arrows carry no labels: what each arrow passes along (for example
        // "As $usable_cas") is shown in the details panel when it is clicked.
        const key = dependencyKey(dependency);
        if (!graph.hasEdge(key))
            graph.addDirectedEdgeWithKey(key, dependency.prerequisite, dependency.consumer, {
                type: 'arrow',
                size: DEFAULT_GRAPH_EDGE_SIZE,
            });
    }
    if (graph.size) {
        // Query chains need less space between dependency levels than collected graphs.
        // This leaves room vertically for branches in the fixed-height panel.
        sequentialLayout(graph, 200);
    } else {
        // The shared layout needs an edge. Independent queries must still appear
        // separately; placing them in rows does not create an execution order.
        graph
            .nodes()
            .forEach((name, index) =>
                graph.mergeNodeAttributes(name, { x: (index % 4) * 300, y: Math.floor(index / 4) * 160 })
            );
    }
    return graph;
}

// At a distant zoom, an arrow can be thinner than one screen pixel. Accept a
// nearby click so selection does not depend on which pixel WebGL happened to fill.
export function nearestDependency(
    point: { x: number; y: number },
    edges: {
        key: string;
        source: { x: number; y: number };
        target: { x: number; y: number };
    }[],
    tolerance = 6
): string | null {
    let closest: string | null = null;
    let distance = tolerance;
    for (const edge of edges) {
        const dx = edge.target.x - edge.source.x;
        const dy = edge.target.y - edge.source.y;
        const length = dx * dx + dy * dy;
        if (!length) continue;
        const along = Math.max(
            0,
            Math.min(1, ((point.x - edge.source.x) * dx + (point.y - edge.source.y) * dy) / length)
        );
        const current = Math.hypot(point.x - edge.source.x - along * dx, point.y - edge.source.y - along * dy);
        if (current < distance) {
            closest = edge.key;
            distance = current;
        }
    }
    return closest;
}

// Space, in screen pixels, that panels and buttons cover along each canvas edge.
export interface ClearArea {
    top: number;
    right: number;
    bottom: number;
    left: number;
}

type FramingView = Pick<
    Sigma,
    'getDimensions' | 'getNodeDisplayData' | 'framedGraphToViewport' | 'viewportToFramedGraph'
>;

// Extra room around the outermost queries so their labels stay on screen.
const labelRoom = { x: 160, y: 90 };

// cameraToFrame works out where to point the camera so the given queries fill
// the part of the canvas that panels and buttons leave uncovered. Sigma keeps
// node positions in a "framed" space; it converts them to screen pixels for
// any trial camera, which lets this measure the queries at zoom 1 and scale.
export function cameraToFrame(sigma: FramingView, nodes: string[], covered: ClearArea): CameraState | undefined {
    const points = nodes.flatMap((node) => {
        const data = sigma.getNodeDisplayData(node);
        return data ? [data] : [];
    });
    if (!points.length) return undefined;
    const { width, height } = sigma.getDimensions();
    const minX = Math.min(...points.map((point) => point.x));
    const maxX = Math.max(...points.map((point) => point.x));
    const minY = Math.min(...points.map((point) => point.y));
    const maxY = Math.max(...points.map((point) => point.y));
    const center = { x: (minX + maxX) / 2, y: (minY + maxY) / 2 };

    // The uncovered area. If panels cover nearly everything, use the whole canvas.
    let area = {
        left: covered.left,
        top: covered.top,
        width: width - covered.left - covered.right,
        height: height - covered.top - covered.bottom,
    };
    if (area.width < 100 || area.height < 100) area = { left: 0, top: 0, width, height };

    // How large the queries appear at zoom ratio 1. A higher ratio shrinks them.
    const atRatioOne: CameraState = { x: center.x, y: center.y, ratio: 1, angle: 0 };
    const corner = sigma.framedGraphToViewport({ x: minX, y: minY }, { cameraState: atRatioOne });
    const opposite = sigma.framedGraphToViewport({ x: maxX, y: maxY }, { cameraState: atRatioOne });
    const ratio = Math.min(
        MAX_CAMERA_RATIO,
        Math.max(
            MIN_CAMERA_RATIO,
            (Math.abs(opposite.x - corner.x) + labelRoom.x) / area.width,
            (Math.abs(opposite.y - corner.y) + labelRoom.y) / area.height
        )
    );

    // Centering the camera on the queries would put them in the middle of the
    // whole canvas. Shift it so they sit in the middle of the uncovered area:
    // the camera moves to the point now shown that far from the canvas center
    // in the opposite direction.
    const centered: CameraState = { x: center.x, y: center.y, ratio, angle: 0 };
    const target = { x: area.left + area.width / 2, y: area.top + area.height / 2 };
    const camera = sigma.viewportToFramedGraph(
        { x: width - target.x, y: height - target.y },
        { cameraState: centered }
    );
    return { x: camera.x, y: camera.y, ratio, angle: 0 };
}
