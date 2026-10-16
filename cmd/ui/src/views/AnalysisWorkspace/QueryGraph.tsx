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
import { KeyboardEvent, ReactNode, useCallback, useEffect, useRef, useState } from 'react';
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
import { ClearArea, buildQueryGraph, cameraToFrame, nearestDependency } from './graph';
import { numberedExtensions, queryColor, queryColors, queryLabel, relatedQueries } from './model';
import { Workspace } from './types';

interface Props {
    workspace: Workspace;
    controlsBottom?: number;
    visible: Set<string>;
    focused: string | null;
    // Without onFocus the graph is view-only: clicks change nothing and the
    // Clear focus button is hidden. The editor's dependency window uses this.
    onFocus?: (name: string | null) => void;
    // Extension numbers come from these queries. Defaults to the shown plan;
    // the editor passes the complete plan so numbers match the main page.
    numberingQueries?: Workspace['plan']['queries'];
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

function GraphControls({
    workspace,
    visible,
    focused,
    onFocus,
    controlsBottom = 16,
    numberingQueries,
    children,
    outcomes,
    coveredEdges,
}: Props) {
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
    // Share the filter's numbering. An unchanged plan keeps the same images
    // across refreshes, focus changes, and theme switches.
    const iconKey = JSON.stringify(numberedExtensions(numberingQueries ?? workspace.plan.queries));
    useEffect(() => {
        const extensions = JSON.parse(iconKey) as { namespace: string; number: number }[];
        const urls = new Map<string, string>();
        extensions.forEach(({ namespace, number }) => {
            // Fit longer numbers inside the existing node circle.
            const fontSize = number < 10 ? 64 : number < 100 ? 48 : 36;
            const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><text x="50" y="52" text-anchor="middle" dominant-baseline="central" font-family="Arial,sans-serif" font-size="${fontSize}" font-weight="700" fill="#000000">${number}</text></svg>`;
            urls.set(namespace, URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml' })));
        });
        graph.forEachNode((name) => graph.setNodeAttribute(name, 'image', urls.get(name.split('/')[0])!));
        sigma.refresh();
        return () => urls.forEach((url) => URL.revokeObjectURL(url));
    }, [iconKey, graph, sigma]);
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
        const related = relatedQueries(workspace.dependencies, focused);
        const edgeGray = defaultEdgeColor(darkMode);
        const background = theme.neutral.primary;
        const focusBlue = queryColors.focused.color;
        // Match Explore's fading strength. Unrelated nodes lose their status
        // color until focus is cleared; the selected path keeps its colors.
        const fadedEdge = blendHexColors(edgeGray, background, darkMode ? 0.6 : 0.9);
        for (const query of workspace.plan.queries) {
            const isDimmed = !!focused && !related.nodes.has(query.name);
            const stateColor = queryColor(query, outcomes?.get(query.name));
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
                backgroundColor: background,
                highlightedBackground: focusBlue,
                highlightedText: '#ffffff',
                label: queryLabel(query.name),
                source: query.namespace,
                kind: query.version || 'Version unavailable',
            });
        }
        graph.forEachEdge((edge, _attributes, source, target) => {
            graph.mergeEdgeAttributes(edge, {
                hidden: !visible.has(source) || !visible.has(target),
                color: focused && !related.edges.has(edge) ? fadedEdge : edgeGray,
                // Keep every edge gray, including the selected edge. Thickness
                // and its details identify the exact connection being inspected.
                size: edge === focused ? DEFAULT_GRAPH_EDGE_SIZE * 1.5 : DEFAULT_GRAPH_EDGE_SIZE,
                backgroundColor: background,
            });
        });
        sigma.refresh();
    }, [workspace, visible, focused, graph, sigma, theme, darkMode, outcomes]);

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
    // Matches Explore's controls: plain icon buttons whose names appear as
    // tooltips. Zooming uses the mouse wheel or the keyboard keys below.
    return (
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
    );
}

export default function QueryGraph(props: Props) {
    const theme = useTheme();
    // Parent changes this component's key only when the rule/schema revision
    // changes. Selecting, filtering, and report refreshes preserve the camera.
    const [graph] = useState(() => buildQueryGraph(props.workspace));
    const [nodeProgram] = useState(() => getNodeCombinedProgram());
    return (
        <Box className='absolute inset-0'>
            <SigmaContainer
                graph={graph}
                style={{ height: '100%', width: '100%', background: theme.neutral.primary }}
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
                <GraphControls {...props} />
            </SigmaContainer>
        </Box>
    );
}
