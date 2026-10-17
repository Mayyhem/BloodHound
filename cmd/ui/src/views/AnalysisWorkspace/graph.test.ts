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
import { sequentialLayout } from 'src/rendering/utils/sequentialLayout';
import { buildQueryGraph, clearBox, laneOrder, nearestDependency, placeLaneTitle } from './graph';
import { queryFixture, workspaceFixture } from './testData';
import { Dependency, Workspace } from './types';

// The layout the graph used before lanes: the same queries and arrows, laid
// out left to right, with nothing moved afterwards.
function layoutWithoutLanes(workspace: Workspace) {
    const graph = new MultiDirectedGraph();
    workspace.plan.queries.forEach((query) => graph.addNode(query.name, { size: 25, x: 0, y: 0 }));
    workspace.dependencies.forEach((dependency) => graph.addDirectedEdge(dependency.prerequisite, dependency.consumer));
    sequentialLayout(graph, 200);
    return graph;
}

const queryArrow = (prerequisite: string, consumer: string): Dependency => ({
    prerequisite,
    consumer,
    type: 'query',
    name: prerequisite,
});

describe('extension lanes', () => {
    it('keeps every left-to-right position and puts each extension in its own lane', () => {
        const workspace = workspaceFixture();
        const { graph, lanes } = buildQueryGraph(workspace);
        const before = layoutWithoutLanes(workspace);
        for (const query of workspace.plan.queries)
            expect(graph.getNodeAttribute(query.name, 'x')).toBe(before.getNodeAttribute(query.name, 'x'));

        // Active Directory feeds Vault, so it is above it. Other is independent
        // and comes before Vault alphabetically.
        expect(lanes.map((lane) => lane.namespace)).toEqual(['PoC_AD', 'PoC_Other', 'PoC_Vault']);
        // Lanes are stacked from the top without overlapping. Larger y is
        // higher on the screen.
        lanes.forEach((lane, index) => {
            expect(lane.top).toBeGreaterThan(lane.bottom);
            if (index) expect(lane.top).toBeLessThanOrEqual(lanes[index - 1].bottom);
        });
        for (const query of workspace.plan.queries) {
            const lane = lanes.find((entry) => entry.namespace === query.namespace)!;
            const y = graph.getNodeAttribute(query.name, 'y');
            expect(y).toBeLessThan(lane.top);
            expect(y).toBeGreaterThan(lane.bottom);
        }
        // Vault's two queries are in different columns, so they share one row.
        expect(graph.getNodeAttribute('PoC_Vault/users.cypher', 'y')).toBe(
            graph.getNodeAttribute('PoC_Vault/edges.cypher', 'y')
        );
    });

    it('stacks queries that share a column in the order the layout drew them', () => {
        const workspace = workspaceFixture();
        workspace.plan.queries.push(queryFixture('PoC_Vault/groups.cypher', 5));
        workspace.dependencies.push(queryArrow('PoC_AD/users.cypher', 'PoC_Vault/groups.cypher'));
        const { graph, lanes } = buildQueryGraph(workspace);
        const before = layoutWithoutLanes(workspace);
        const sameColumn = ['PoC_Vault/users.cypher', 'PoC_Vault/groups.cypher'];
        expect(graph.getNodeAttribute(sameColumn[0], 'x')).toBe(graph.getNodeAttribute(sameColumn[1], 'x'));
        const [higherBefore] = [...sameColumn].sort(
            (first, second) => before.getNodeAttribute(second, 'y') - before.getNodeAttribute(first, 'y')
        );
        const [higherNow] = [...sameColumn].sort(
            (first, second) => graph.getNodeAttribute(second, 'y') - graph.getNodeAttribute(first, 'y')
        );
        expect(higherNow).toBe(higherBefore);
        // Two rows make the Vault lane taller than the one-row lanes.
        const height = (namespace: string) => {
            const lane = lanes.find((entry) => entry.namespace === namespace)!;
            return lane.top - lane.bottom;
        };
        expect(height('PoC_Vault')).toBeGreaterThan(height('PoC_AD'));
    });

    it('draws no lanes and moves nothing when there is one extension', () => {
        const workspace = workspaceFixture();
        workspace.plan.queries = workspace.plan.queries.filter((query) => query.namespace === 'PoC_Vault');
        workspace.dependencies = workspace.dependencies.filter((dependency) =>
            dependency.prerequisite.startsWith('PoC_Vault')
        );
        const { graph, lanes } = buildQueryGraph(workspace);
        const before = layoutWithoutLanes(workspace);
        expect(lanes).toEqual([]);
        for (const query of workspace.plan.queries) {
            expect(graph.getNodeAttribute(query.name, 'x')).toBe(before.getNodeAttribute(query.name, 'x'));
            expect(graph.getNodeAttribute(query.name, 'y')).toBe(before.getNodeAttribute(query.name, 'y'));
        }
    });

    it('lists queries in rows of four inside each lane when there are no arrows', () => {
        const workspace = workspaceFixture();
        workspace.dependencies = [];
        workspace.plan.queries = [
            ...['a', 'b', 'c', 'd', 'e'].map((name, index) => queryFixture(`PoC_AD/${name}.cypher`, index + 1)),
            queryFixture('PoC_Vault/only.cypher', 6),
        ];
        const { graph, lanes } = buildQueryGraph(workspace);
        expect(lanes.map((lane) => lane.namespace)).toEqual(['PoC_AD', 'PoC_Vault']);
        const position = (name: string) => graph.getNodeAttributes(`PoC_AD/${name}.cypher`);
        expect(['a', 'b', 'c', 'd'].map((name) => position(name).y)).toEqual(Array(4).fill(position('a').y));
        expect(['a', 'b', 'c', 'd'].map((name) => position(name).x)).toEqual([0, 300, 600, 900]);
        // The fifth query starts a second row, below the first.
        expect(position('e').x).toBe(0);
        expect(position('e').y).toBeLessThan(position('a').y);
        expect(graph.getNodeAttribute('PoC_Vault/only.cypher', 'y')).toBeLessThan(lanes[1].top);
    });

    it('orders extensions by what they depend on, alphabetically when tied, and survives loops', () => {
        const namespaceOf = (name: string) => name.split('/')[0];
        const title = (namespace: string) => namespace;
        // C feeds A, so C goes before A even though A is first alphabetically.
        expect(laneOrder(['A', 'B', 'C'], [queryArrow('C/x', 'A/y')], namespaceOf, title)).toEqual(['B', 'C', 'A']);
        // Arrows inside one extension are ignored.
        expect(laneOrder(['B', 'A'], [queryArrow('B/x', 'B/y')], namespaceOf, title)).toEqual(['A', 'B']);
        // A and B feed each other. C waits on nothing, so it goes first; then
        // the alphabetically first of A and B breaks the loop.
        expect(
            laneOrder(['B', 'A', 'C'], [queryArrow('A/x', 'B/y'), queryArrow('B/z', 'A/w')], namespaceOf, title)
        ).toEqual(['C', 'A', 'B']);
        // Titles, not namespaces, decide ties.
        expect(laneOrder(['A', 'B'], [], namespaceOf, (namespace) => (namespace === 'A' ? 'Zebra' : 'Apple'))).toEqual([
            'B',
            'A',
        ]);
    });
});

it('selects a thin arrow near the pointer, but leaves empty space and distant extensions unselected', () => {
    const edges = [
        { key: 'a', source: { x: 10, y: 10.45 }, target: { x: 100, y: 10.45 } },
        { key: 'b', source: { x: 10, y: 20 }, target: { x: 100, y: 20 } },
    ];
    expect(nearestDependency({ x: 50, y: 11 }, edges)).toBe('a');
    expect(nearestDependency({ x: 50, y: 19 }, edges)).toBe('b');
    expect(nearestDependency({ x: 120, y: 10 }, edges)).toBeNull();
    expect(nearestDependency({ x: 50, y: 40 }, edges)).toBeNull();
    expect(nearestDependency({ x: 50, y: 11 }, [])).toBeNull();
});

describe('lane title placement', () => {
    // A 1,000 × 800 canvas with a top-left panel 400 wide and 300 tall, an
    // entity panel 300 wide on the right, and a table 200 tall at the bottom.
    // That leaves two uncovered rectangles: below the top panel, and beside it.
    const covered = [
        { top: 316, right: 316, bottom: 248, left: 16 },
        { top: 16, right: 316, bottom: 248, left: 416 },
    ];
    const areas = covered.map((area) => clearBox(area, 1000, 800));
    const size = { width: 120, height: 22 };

    it('stays at its natural place, moving with the graph, while that place is uncovered', () => {
        expect(placeLaneTitle({ left: 450, top: 100 }, size, 300, areas)).toEqual({ left: 450, top: 100 });
        expect(placeLaneTitle({ left: 100, top: 400 }, size, 500, areas)).toEqual({ left: 100, top: 400 });
    });

    it('stops at the edge of the uncovered area instead of going under a panel or off screen', () => {
        // Under the top panel: slides right to just beside it.
        expect(placeLaneTitle({ left: 100, top: 100 }, size, 300, areas)).toEqual({ left: 416, top: 100 });
        // Just above the top panel's bottom edge: it still slides right, staying
        // level with its lane's top, instead of dropping onto the lane's queries.
        expect(placeLaneTitle({ left: 100, top: 300 }, size, 500, areas)).toEqual({ left: 416, top: 300 });
        // Panned off the left edge, below the top panel: stops at the left edge.
        expect(placeLaneTitle({ left: -500, top: 400 }, size, 500, areas)).toEqual({ left: 16, top: 400 });
        // Toward the entity panel: stops before it, title fully visible.
        expect(placeLaneTitle({ left: 650, top: 400 }, size, 500, areas)).toEqual({ left: 564, top: 400 });
        // The lane's top has scrolled above the canvas: the title stays at the
        // top of the part still shown.
        expect(placeLaneTitle({ left: 450, top: -200 }, size, 300, areas)).toEqual({ left: 450, top: 16 });
    });

    it('is hidden when its lane has no uncovered space', () => {
        // The lane is entirely above the canvas.
        expect(placeLaneTitle({ left: 450, top: -300 }, size, -100, areas)).toBeNull();
        // The lane is entirely behind the table.
        expect(placeLaneTitle({ left: 450, top: 600 }, size, 700, areas)).toBeNull();
        // The lane is too thin to hold its title.
        expect(placeLaneTitle({ left: 450, top: 100 }, size, 110, areas)).toBeNull();
        // The uncovered space is narrower than the title.
        expect(placeLaneTitle({ left: 450, top: 100 }, { width: 600, height: 22 }, 300, areas)).toBeNull();
    });
});
