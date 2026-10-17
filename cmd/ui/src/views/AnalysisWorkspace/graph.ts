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
import { dependencyKey, extensionTitle, queryLabel } from './model';
import { Dependency, Workspace } from './types';

// One horizontal band of the graph holding every query of one extension.
// `top` and `bottom` are its edges in graph positions. Sigma draws a larger
// y higher on the screen, so `top` is the larger number.
export interface Lane {
    namespace: string;
    top: number;
    bottom: number;
}

// Lane spacing, in the same units as the layout's own spacing (it leaves 140
// between queries in one column and 200 between columns).
const laneRowSpacing = 160;
// Room above a lane's first row, for the lane title, and below its last row,
// for the query name and version drawn under each node. The labels shrink
// more slowly than the graph when zooming out, so the room below is larger.
const laneTopRoom = 125;
const laneBottomRoom = 150;

// laneOrder puts extensions that other extensions depend on first, so that
// arrows between extensions mostly point down the screen. Among extensions
// that are ready at the same time, the one whose title comes first
// alphabetically goes first. If the remaining extensions all wait on each
// other (one extension's query feeds the other and the other way round), the
// alphabetically first of them goes next, which breaks the loop.
// `namespaceOf` gives the extension of a query name.
export function laneOrder(
    namespaces: string[],
    dependencies: Dependency[],
    namespaceOf: (name: string) => string | undefined,
    title: (namespace: string) => string
) {
    const waitsOn = new Map(namespaces.map((namespace) => [namespace, new Set<string>()]));
    for (const dependency of dependencies) {
        const consumer = namespaceOf(dependency.consumer);
        const prerequisite = namespaceOf(dependency.prerequisite);
        if (consumer !== undefined && prerequisite !== undefined && consumer !== prerequisite)
            waitsOn.get(consumer)?.add(prerequisite);
    }
    const remaining = [...namespaces].sort(
        (first, second) => title(first).localeCompare(title(second)) || first.localeCompare(second)
    );
    const ordered: string[] = [];
    while (remaining.length) {
        const ready = remaining.find((namespace) =>
            [...waitsOn.get(namespace)!].every((prerequisite) => !remaining.includes(prerequisite))
        );
        const next = ready ?? remaining[0];
        ordered.push(next);
        remaining.splice(remaining.indexOf(next), 1);
    }
    return ordered;
}

// placeInLanes keeps every query's left-to-right position from the layout,
// so further left still means "runs earlier" across the whole graph, and moves
// it up or down into its extension's lane. Lanes are stacked from the top in
// laneOrder.
//
// Inside a lane, queries that share a column (the same left-to-right position)
// are stacked in the order the layout drew them, top first. A query alone in
// its column goes on the lane's first row, so a simple chain sits on one line.
// A lane is as tall as its busiest column. With no arrows at all there are no
// columns, so each lane lists its queries in rows of four, like the page does
// without lanes.
//
// With fewer than two extensions nothing moves and no lanes are returned.
function placeInLanes(graph: MultiDirectedGraph, workspace: Workspace): Lane[] {
    const queries = workspace.plan.queries.filter((query) => graph.hasNode(query.name));
    const namespaces = [...new Set(queries.map((query) => query.namespace))];
    if (namespaces.length < 2) return [];
    const namespaceOf = new Map(queries.map((query) => [query.name, query.namespace]));
    const ordered = laneOrder(
        namespaces,
        workspace.dependencies,
        (name) => namespaceOf.get(name),
        (namespace) => extensionTitle(workspace, namespace)
    );

    const lanes: Lane[] = [];
    // How far below the top of the graph the next lane starts.
    let distanceFromTop = 0;
    for (const namespace of ordered) {
        const members = queries.filter((query) => query.namespace === namespace).map((query) => query.name);
        // Each query's row inside the lane, counted from 0 at the top.
        const rows = new Map<string, number>();
        if (graph.size) {
            const columns = new Map<number, string[]>();
            for (const name of members) {
                const column = Math.round(graph.getNodeAttribute(name, 'x'));
                columns.set(column, [...(columns.get(column) ?? []), name]);
            }
            for (const column of columns.values()) {
                column
                    .sort((first, second) => graph.getNodeAttribute(second, 'y') - graph.getNodeAttribute(first, 'y'))
                    .forEach((name, row) => rows.set(name, row));
            }
        } else {
            members.forEach((name, index) => {
                graph.setNodeAttribute(name, 'x', (index % 4) * 300);
                rows.set(name, Math.floor(index / 4));
            });
        }
        const rowCount = Math.max(...rows.values()) + 1;
        for (const [name, row] of rows) {
            graph.setNodeAttribute(name, 'y', -(distanceFromTop + laneTopRoom + row * laneRowSpacing));
        }
        const height = laneTopRoom + (rowCount - 1) * laneRowSpacing + laneBottomRoom;
        lanes.push({ namespace, top: -distanceFromTop, bottom: -(distanceFromTop + height) });
        distanceFromTop += height;
    }
    return lanes;
}

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
    const lanes = placeInLanes(graph, workspace);
    return { graph, lanes };
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

// A rectangle on the screen, in pixels from the canvas's top-left corner.
export interface ScreenBox {
    left: number;
    top: number;
    right: number;
    bottom: number;
}

// The uncovered rectangle a ClearArea describes on a canvas of this size.
export const clearBox = (area: ClearArea, width: number, height: number): ScreenBox => ({
    left: area.left,
    top: area.top,
    right: width - area.right,
    bottom: height - area.bottom,
});

// placeLaneTitle decides where a lane title is drawn. Its natural place is at
// the top-left of its lane, so it moves with the graph like everything else.
// When that place is off screen or under a panel, the title slides just far
// enough to sit inside one of the uncovered rectangles, without leaving its
// own lane: it stops at the edge, like a spreadsheet's sticky header row.
// Sliding sideways is preferred, because it keeps the title level with the
// top of its lane, above the lane's queries. It slides down into the lane
// only when the lane's top is outside every uncovered rectangle (for example
// scrolled above the canvas). If no uncovered rectangle holds it inside its
// lane (the lane is off screen or completely under panels), it is hidden: the
// result is null.
//
// `natural` is the title's top-left corner, `size` its width and height, and
// `laneBottom` the screen position of the lane's bottom edge.
export function placeLaneTitle(
    natural: { left: number; top: number },
    size: { width: number; height: number },
    laneBottom: number,
    clearAreas: ScreenBox[]
): { left: number; top: number } | null {
    let best: { left: number; top: number } | null = null;
    let bestDown = Infinity;
    let bestSideways = Infinity;
    for (const area of clearAreas) {
        // Move down only if the top of the lane is above the uncovered area,
        // and never so far that the title would leave the lane or the area.
        const top = Math.max(natural.top, area.top);
        if (top + size.height > Math.min(laneBottom, area.bottom)) continue;
        // Move sideways only as far as needed to stay inside the area.
        const left = Math.min(Math.max(natural.left, area.left), area.right - size.width);
        if (left < area.left) continue;
        // Least movement down first, then least movement sideways.
        const down = top - natural.top;
        const sideways = Math.abs(left - natural.left);
        if (down < bestDown || (down === bestDown && sideways < bestSideways)) {
            best = { left, top };
            bestDown = down;
            bestSideways = sideways;
        }
    }
    return best;
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
//
// A panel in a corner leaves two clear rectangles: the strip below it and the
// strip beside it. Pass each as one choice; the queries go in whichever choice
// shows them largest.
export function cameraToFrame(
    sigma: FramingView,
    nodes: string[],
    choices: ClearArea | ClearArea[]
): CameraState | undefined {
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

    // How large the queries appear at zoom ratio 1. A higher ratio shrinks them.
    const atRatioOne: CameraState = { x: center.x, y: center.y, ratio: 1, angle: 0 };
    const corner = sigma.framedGraphToViewport({ x: minX, y: minY }, { cameraState: atRatioOne });
    const opposite = sigma.framedGraphToViewport({ x: maxX, y: maxY }, { cameraState: atRatioOne });

    // For each choice: the clear rectangle, and the zoom that fits the queries
    // in it. A choice too small to use (under 100 pixels) is ignored. If every
    // choice is too small, use the whole canvas.
    const fits = (Array.isArray(choices) ? choices : [choices])
        .map((covered) => ({
            left: covered.left,
            top: covered.top,
            width: width - covered.left - covered.right,
            height: height - covered.top - covered.bottom,
        }))
        .filter((area) => area.width >= 100 && area.height >= 100);
    if (!fits.length) fits.push({ left: 0, top: 0, width, height });
    const sized = fits.map((area) => ({
        area,
        ratio: Math.min(
            MAX_CAMERA_RATIO,
            Math.max(
                MIN_CAMERA_RATIO,
                (Math.abs(opposite.x - corner.x) + labelRoom.x) / area.width,
                (Math.abs(opposite.y - corner.y) + labelRoom.y) / area.height
            )
        ),
    }));
    // The smallest ratio draws the queries largest.
    const { area, ratio } = sized.reduce((best, next) => (next.ratio < best.ratio ? next : best));

    // Centering the camera on the queries would put them in the middle of the
    // whole canvas. Shift it so they sit in the middle of the chosen area:
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
