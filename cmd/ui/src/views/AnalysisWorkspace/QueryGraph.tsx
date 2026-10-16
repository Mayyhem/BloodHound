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

import { faCropAlt } from '@fortawesome/free-solid-svg-icons';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Box } from '@mui/material';
import { SigmaContainer, useRegisterEvents, useSigma } from '@react-sigma/core';
import '@react-sigma/core/lib/react-sigma.min.css';
import { useTheme } from 'bh-shared-ui';
import { IconButton } from 'doodle-ui';
import { KeyboardEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { MAX_CAMERA_RATIO, MIN_CAMERA_RATIO } from 'src/ducks/graph/utils';
import EdgeArrowProgram from 'src/rendering/programs/edge.arrow';
import drawHover from 'src/rendering/programs/node-hover';
import drawLabel from 'src/rendering/programs/node-label';
import getNodeCombinedProgram from 'src/rendering/programs/node.combined';
import {
    DEFAULT_GRAPH_EDGE_SIZE,
    DEFAULT_GRAPH_NODE_SIZE,
    blendHexColors,
    defaultEdgeColor,
} from 'src/rendering/utils/utils';
import { useAppSelector } from 'src/store';
import { ClearArea, Lane, buildQueryGraph, cameraToFrame, nearestDependency } from './graph';
import { laneTitles, queryColor, queryColors, queryLabel, queryRoles, relatedQueries } from './model';
import { roleIconSvg, roleKey } from './roleIcons';
import { Workspace } from './types';

interface Props {
    workspace: Workspace;
    controlsBottom?: number;
    visible: Set<string>;
    focused: string | null;
    // Without onFocus the graph is view-only: clicks change nothing and the
    // Clear focus button is hidden. The editor's dependency window uses this.
    onFocus?: (name: string | null) => void;
    // The extension whose lane was clicked. Its lane is highlighted and the
    // others fade; nothing is hidden. The page filters its table to it.
    highlightedLane?: string | null;
    // Called with an extension's namespace when its lane title is clicked.
    // Without it, lane titles are plain text (the editor's dependency window).
    onLane?: (namespace: string) => void;
    // Extra buttons shown in the same row, after Reset Graph.
    children?: ReactNode;
    // Each query's state in the latest completed run that matches these rules.
    outcomes?: Map<string, string>;
    // The clear parts of the canvas that panels currently leave, each given as
    // how much of every edge to avoid. Reset Graph frames the shown queries in
    // the choice that shows them largest. Without it, only the button row is
    // avoided.
    coveredEdges?: () => ClearArea[];
}

// Where one lane appears on the screen, in pixels from the canvas top-left.
interface LanePlacement {
    top: number;
    bottom: number;
}

function GraphControls({
    workspace,
    visible,
    focused,
    onFocus,
    controlsBottom = 16,
    highlightedLane = null,
    onLane,
    children,
    outcomes,
    coveredEdges,
    lanes,
    laneLayer,
}: Props & { lanes: Lane[]; laneLayer: HTMLDivElement | null }) {
    const sigma = useSigma();
    const registerEvents = useRegisterEvents();
    const theme = useTheme();
    const darkMode = useAppSelector((state) => state.global.view.darkMode);
    const camera = sigma.getCamera();
    const graph = sigma.getGraph();

    const fitted = useRef(false);
    // Read the latest panel sizes when the button is pressed, not when the
    // graph was first drawn.
    const covered = useRef(coveredEdges);
    covered.current = coveredEdges;
    // Reset Graph frames only the queries shown (filters hide the others) and
    // only in the part of the canvas that panels and buttons leave uncovered.
    const fit = useCallback(() => {
        const shown = graph.filterNodes((_node, attributes) => !attributes.hidden);
        const edges = covered.current?.() ?? { top: 16, right: 16, bottom: controlsBottom + 48, left: 16 };
        const state = cameraToFrame(sigma, shown, edges);
        if (state) void camera.animate(state, { duration: 250 });
    }, [sigma, graph, camera, controlsBottom]);
    useEffect(() => {
        // Frame once when the graph first appears. Opening details or resizing
        // the table must not move a camera the author has already positioned.
        if (fitted.current) return;
        // Wait one moment so filters and node positions are applied first.
        const timer = setTimeout(() => {
            fitted.current = true;
            fit();
        }, 50);
        return () => clearTimeout(timer);
    }, [fit]);
    // Each node shows icons for what its query produces. Queries with the same
    // roles share one image. An unchanged plan keeps the same images across
    // refreshes, focus changes, and theme switches.
    const iconKey = JSON.stringify(
        workspace.plan.queries.map((query) => [query.name, queryRoles(query, workspace.dependencies)])
    );
    useEffect(() => {
        const roles = JSON.parse(iconKey) as [string, ReturnType<typeof queryRoles>][];
        const urls = new Map<string, string>();
        for (const [name, queryRoleList] of roles) {
            const key = roleKey(queryRoleList);
            if (!urls.has(key))
                urls.set(key, URL.createObjectURL(new Blob([roleIconSvg(queryRoleList)], { type: 'image/svg+xml' })));
            if (graph.hasNode(name)) graph.setNodeAttribute(name, 'image', urls.get(key)!);
        }
        sigma.refresh();
        return () => urls.forEach((url) => URL.revokeObjectURL(url));
    }, [iconKey, graph, sigma]);

    // Lanes. A lane whose queries are all hidden by filters is hidden too; the
    // others stay where they are. With fewer than two lanes left there is
    // nothing to tell apart, so none are drawn.
    const shownLanes = useMemo(() => {
        const shown = lanes.filter((lane) =>
            workspace.plan.queries.some((query) => query.namespace === lane.namespace && visible.has(query.name))
        );
        return shown.length >= 2 ? shown : [];
    }, [lanes, workspace, visible]);
    const titles = useMemo(
        () =>
            laneTitles(
                workspace,
                lanes.map((lane) => lane.namespace)
            ),
        [workspace, lanes]
    );
    const background = theme.neutral.primary;
    const focusBlue = queryColors.focused.color;
    // Every other lane gets a faint shade of the text color, and the clicked
    // lane a faint shade of the focus blue. Shades come from the lane's place
    // in the full list, so hiding one lane does not recolor the rest. Node
    // labels are drawn on a patch of this same color so they blend in.
    const laneShade = useCallback(
        (namespace: string) => {
            if (namespace === highlightedLane) return blendHexColors(background, focusBlue, darkMode ? 0.22 : 0.1);
            const index = lanes.findIndex((lane) => lane.namespace === namespace);
            return index % 2 ? blendHexColors(background, theme.contrast, darkMode ? 0.07 : 0.04) : background;
        },
        [lanes, highlightedLane, background, focusBlue, theme.contrast, darkMode]
    );
    const laneDivider = blendHexColors(background, theme.contrast, darkMode ? 0.2 : 0.15);
    // A lane fades with its queries: when a query is focused and none of the
    // lane's queries is on its path, or when another lane was clicked.
    const related = useMemo(() => relatedQueries(workspace.dependencies, focused), [workspace, focused]);
    const laneFaded = (namespace: string) =>
        (!!highlightedLane && namespace !== highlightedLane) ||
        (!!focused &&
            !workspace.plan.queries.some((query) => query.namespace === namespace && related.nodes.has(query.name)));

    // Sigma redraws whenever the view pans, zooms, or resizes. After each
    // redraw, work out where every lane now appears on the screen. React only
    // redraws the lanes when one of them actually moved.
    const [placements, setPlacements] = useState<LanePlacement[]>([]);
    useEffect(() => {
        const measure = () => {
            const next = lanes.map((lane) => ({
                top: sigma.graphToViewport({ x: 0, y: lane.top }).y,
                bottom: sigma.graphToViewport({ x: 0, y: lane.bottom }).y,
            }));
            setPlacements((previous) => (JSON.stringify(previous) === JSON.stringify(next) ? previous : next));
        };
        measure();
        sigma.on('afterRender', measure);
        return () => {
            sigma.removeListener('afterRender', measure);
        };
    }, [sigma, lanes]);
    useEffect(() => {
        // Sigma keeps its initial settings. Update canvas text colors explicitly
        // when the user changes theme, just as Explore does.
        sigma.setSetting('labelColor', { color: theme.contrast });
    }, [sigma, theme.contrast]);
    useEffect(() => {
        sigma.setSetting('nodeReducer', (_name, data) => ({
            ...data,
            size: Number(data.size),
            inverseSqrtZoomRatio: 1 / Math.sqrt(camera.ratio),
        }));
        sigma.setSetting('edgeReducer', (_name, data) => ({ ...data, size: Number(data.size) }));
        const refresh = () => sigma.refresh();
        camera.on('updated', refresh);
        return () => {
            camera.off('updated', refresh);
        };
    }, [sigma, camera]);
    useEffect(() => {
        if (!onFocus) return;
        registerEvents({
            clickNode: ({ node }) => onFocus(node),
            clickEdge: ({ edge }) => onFocus(edge),
            clickStage: ({ event }) => {
                const edges = graph
                    .edges()
                    .filter((key) => !graph.getEdgeAttribute(key, 'hidden'))
                    .map((key) => ({
                        key,
                        source: sigma.graphToViewport(
                            graph.getNodeAttributes(graph.source(key)) as { x: number; y: number }
                        ),
                        target: sigma.graphToViewport(
                            graph.getNodeAttributes(graph.target(key)) as { x: number; y: number }
                        ),
                    }));
                onFocus(nearestDependency(event, edges));
            },
        });
    }, [registerEvents, onFocus, graph, sigma]);
    useEffect(() => {
        const edgeGray = defaultEdgeColor(darkMode);
        // Match Explore's fading strength. Unrelated nodes lose their status
        // color until focus is cleared; the selected path keeps its colors.
        // A clicked lane fades the queries in every other lane the same way.
        const fadedEdge = blendHexColors(edgeGray, background, darkMode ? 0.6 : 0.9);
        const namespaceOf = new Map(workspace.plan.queries.map((query) => [query.name, query.namespace]));
        const outsideLane = (name: string) => !!highlightedLane && namespaceOf.get(name) !== highlightedLane;
        const lanesDrawn = shownLanes.length > 0;
        for (const query of workspace.plan.queries) {
            const isDimmed = (!!focused && !related.nodes.has(query.name)) || outsideLane(query.name);
            const stateColor = queryColor(query, outcomes?.get(query.name));
            const labelBackground = lanesDrawn ? laneShade(query.namespace) : background;
            graph.mergeNodeAttributes(query.name, {
                hidden: !visible.has(query.name),
                color: query.name === focused ? focusBlue : isDimmed ? edgeGray : stateColor,
                size: DEFAULT_GRAPH_NODE_SIZE,
                borderColor: theme.contrast,
                graphBgColor: background,
                dimFactor: isDimmed ? (darkMode ? 0.5 : 0.2) : 1,
                highlighted: query.name === focused,
                forceLabel: query.name === focused,
                isDimmed,
                labelDimFactor: darkMode ? 0.3 : 0.1,
                backgroundColor: labelBackground,
                highlightedBackground: focusBlue,
                highlightedText: '#ffffff',
                label: queryLabel(query.name),
                // The lane names the extension, so only the version is shown
                // under the query name.
                sublabel: query.version || 'Version unavailable',
            });
        }
        graph.forEachEdge((edge, _attributes, source, target) => {
            // An arrow into or out of the clicked lane keeps its full color.
            const fadedByLane = outsideLane(source) && outsideLane(target);
            graph.mergeEdgeAttributes(edge, {
                hidden: !visible.has(source) || !visible.has(target),
                color: (focused && !related.edges.has(edge)) || fadedByLane ? fadedEdge : edgeGray,
                // Keep every edge gray, including the selected edge. Thickness
                // and its details identify the exact connection being inspected.
                size: edge === focused ? DEFAULT_GRAPH_EDGE_SIZE * 1.5 : DEFAULT_GRAPH_EDGE_SIZE,
                backgroundColor: background,
            });
        });
        sigma.refresh();
    }, [
        workspace,
        visible,
        focused,
        related,
        highlightedLane,
        shownLanes,
        laneShade,
        graph,
        sigma,
        theme,
        background,
        focusBlue,
        darkMode,
        outcomes,
    ]);

    const keyboard = (event: KeyboardEvent) => {
        const current = camera.getState();
        const step = current.ratio * 0.08;
        const actions: Record<string, () => void> = {
            ArrowLeft: () => camera.setState({ x: current.x - step }),
            ArrowRight: () => camera.setState({ x: current.x + step }),
            ArrowUp: () => camera.setState({ y: current.y + step }),
            ArrowDown: () => camera.setState({ y: current.y - step }),
            '+': () => camera.animatedZoom(),
            '=': () => camera.animatedZoom(),
            '-': () => camera.animatedUnzoom(),
            '0': fit,
            Escape: () => onFocus?.(null),
        };
        if (actions[event.key]) {
            event.preventDefault();
            actions[event.key]();
        }
    };
    // Lane bands are drawn in the layer behind Sigma's canvas, so nodes and
    // arrows stay on top of them.
    const bands = shownLanes.map((lane) => {
        const placement = placements[lanes.indexOf(lane)];
        if (!placement) return null;
        return (
            <Box
                key={lane.namespace}
                data-testid='extension-lane'
                className='absolute left-0 right-0'
                style={{
                    top: placement.top,
                    height: placement.bottom - placement.top,
                    backgroundColor: laneShade(lane.namespace),
                    borderTop: `1px solid ${laneDivider}`,
                    borderBottom: `1px solid ${laneDivider}`,
                }}
            />
        );
    });
    // Lane titles are anchored to the top middle of their lane. A lane spans
    // the whole canvas width, so the title is centered on the canvas and does
    // not move sideways when panning. It moves up and down with its lane; when
    // the top of a tall lane scrolls out of view, the title stays at the top
    // of the part still shown. A lane squeezed shorter than its title (zoomed
    // far out) hides its title.
    const { width: canvasWidth, height: canvasHeight } = sigma.getDimensions();
    // Space between the top edge of the lane and its title, in pixels.
    const titleMargin = 10;
    const laneTitleElements = shownLanes.map((lane) => {
        const placement = placements[lanes.indexOf(lane)];
        if (!placement || placement.bottom - placement.top < 18 + titleMargin) return null;
        if (placement.bottom < 0 || placement.top > canvasHeight) return null;
        const { title, namespace } = titles.get(lane.namespace)!;
        const top = Math.min(Math.max(placement.top, 0) + titleMargin, placement.bottom - 22);
        const left = canvasWidth / 2;
        const content = (
            <>
                <span className='font-bold'>{title}</span>
                {namespace && <span className='ml-1 text-xs opacity-80'>({namespace})</span>}
            </>
        );
        const style = {
            top,
            left,
            color: theme.contrast,
            backgroundColor: laneShade(lane.namespace),
            opacity: laneFaded(lane.namespace) ? 0.45 : 1,
            // Center the title on `left` rather than starting it there.
            transform: 'translateX(-50%)',
        };
        return onLane ? (
            <Box
                key={lane.namespace}
                component='button'
                type='button'
                aria-pressed={lane.namespace === highlightedLane}
                title={
                    lane.namespace === highlightedLane
                        ? 'Show every extension in the table again'
                        : 'Show only this extension in the table'
                }
                onClick={() => onLane(lane.namespace)}
                className='absolute z-[5] max-w-[240px] truncate rounded px-1 text-center text-sm hover:underline focus-visible:outline'
                style={style}>
                {content}
            </Box>
        ) : (
            <Box
                key={lane.namespace}
                className='absolute z-[5] max-w-[240px] truncate px-1 text-center text-sm'
                style={style}>
                {content}
            </Box>
        );
    });

    // Matches Explore's controls: plain icon buttons whose names appear as
    // tooltips. Zooming uses the mouse wheel or the keyboard keys below.
    return (
        <>
            {laneLayer && createPortal(bands, laneLayer)}
            {laneTitleElements}
            <Box
                tabIndex={0}
                onKeyDown={keyboard}
                aria-label='Graph keyboard controls: arrow keys pan, plus and minus zoom, zero resets the view, Escape clears focus. Use the query table to select a query.'
                className='absolute left-4 z-10 flex gap-1 rounded-full'
                // A backing in the canvas color keeps labels drawn under the
                // buttons from showing through, while looking like plain canvas.
                style={{ bottom: controlsBottom, backgroundColor: theme.neutral.primary }}>
                <IconButton aria-label='Reset Graph' onClick={fit}>
                    <FontAwesomeIcon icon={faCropAlt} />
                </IconButton>
                {children}
            </Box>
        </>
    );
}

export default function QueryGraph(props: Props) {
    const theme = useTheme();
    // Parent changes this component's key only when the rule/schema revision
    // changes. Selecting, filtering, and report refreshes preserve the camera.
    const [{ graph, lanes }] = useState(() => buildQueryGraph(props.workspace));
    const [nodeProgram] = useState(() => getNodeCombinedProgram());
    // The layer that holds the lane bands. It sits behind Sigma, whose own
    // background is see-through so the bands show.
    const [laneLayer, setLaneLayer] = useState<HTMLDivElement | null>(null);
    return (
        <Box className='absolute inset-0' style={{ background: theme.neutral.primary }}>
            <Box ref={setLaneLayer} aria-hidden className='absolute inset-0 pointer-events-none' />
            <SigmaContainer
                graph={graph}
                style={{ height: '100%', width: '100%', background: 'transparent' }}
                settings={{
                    nodeProgramClasses: { combined: nodeProgram },
                    edgeProgramClasses: { arrow: EdgeArrowProgram },
                    hoverRenderer: drawHover,
                    labelRenderer: drawLabel,
                    renderEdgeLabels: false,
                    enableEdgeClickEvents: true,
                    labelFont: 'Figtree',
                    labelSize: 12,
                    labelGridCellSize: 40,
                    labelRenderedSizeThreshold: 0,
                    labelColor: { color: theme.contrast },
                    minCameraRatio: MIN_CAMERA_RATIO,
                    maxCameraRatio: MAX_CAMERA_RATIO,
                }}>
                <GraphControls {...props} lanes={lanes} laneLayer={laneLayer} />
            </SigmaContainer>
        </Box>
    );
}
