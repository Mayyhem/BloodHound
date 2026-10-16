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

import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { rest } from 'msw';
import { setupServer } from 'msw/node';
import { render } from 'src/test-utils';
import AnalysisWorkspace from './AnalysisWorkspace';
import { dependencyKey } from './model';
import { workspaceFixture } from './testData';
import { Workspace } from './types';

// These checks cover graph/table/details coordination. Actual canvas drawing
// needs a separate browser check because this test environment has no WebGL GPU.
vi.mock('./QueryGraph', () => ({
    default: ({
        workspace,
        visible,
        focused,
        onFocus,
        children,
        outcomes,
        highlightedLane,
        onLane,
    }: {
        workspace: Workspace;
        visible: Set<string>;
        focused: string | null;
        onFocus: (name: string | null) => void;
        children?: React.ReactNode;
        outcomes?: Map<string, string>;
        highlightedLane?: string | null;
        onLane?: (namespace: string) => void;
    }) => (
        <div aria-label='Query graph'>
            <button onClick={() => onFocus(null)}>Clear focus</button>
            {[...new Set(workspace.plan.queries.map((query) => query.namespace))].map((namespace) => (
                <button
                    key={namespace}
                    aria-pressed={namespace === highlightedLane}
                    onClick={() => onLane?.(namespace)}>{`Lane ${namespace}`}</button>
            ))}
            {workspace.plan.queries
                .filter((query) => visible.has(query.name))
                .map((query) => (
                    <button
                        key={query.name}
                        aria-pressed={query.name === focused}
                        data-outcome={outcomes?.get(query.name)}
                        onClick={() => onFocus(query.name)}>{`Graph ${query.name}`}</button>
                ))}
            {workspace.dependencies.map((dependency, index) => (
                <button key={index} onClick={() => onFocus(dependencyKey(dependency))}>
                    {`Graph edge ${dependency.prerequisite} to ${dependency.consumer}`}
                </button>
            ))}
            {children}
        </div>
    ),
}));

let canEdit = true;
vi.mock('bh-shared-ui', async (original) => {
    const actual = await original<typeof import('bh-shared-ui')>();
    return { ...actual, usePermissions: () => ({ checkPermission: () => canEdit }) };
});
let workspace = workspaceFixture();
const requests: { method: string; path: string }[] = [];
const server = setupServer(
    rest.get('/api/v2/analysis-workspace', (request, response, context) => response(context.json({ data: workspace }))),
    rest.get('/api/v2/analysis-workspace/draft', (_, response, context) =>
        response(
            context.json({ data: { revision: '', base_revision: workspace.plan.revision, files: {}, ready: false } })
        )
    ),
    rest.get('/api/v2/analysis/status', (_, response, context) =>
        response(context.json({ data: { request_type: '' } }))
    ),
    rest.get('/api/v2/datapipe/status', (_, response, context) => response(context.json({ data: { status: 'idle' } })))
);
beforeAll(() => {
    server.listen({ onUnhandledRequest: 'error' });
    server.events.on('request:start', (request) =>
        requests.push({ method: request.method, path: request.url.pathname })
    );
});
afterAll(() => server.close());
beforeEach(() => {
    canEdit = true;
    workspace = workspaceFixture();
    requests.length = 0;
});
afterEach(() => server.resetHandlers());

it('coordinates graph, keyboard-accessible table, and prerequisite links without graph requests', async () => {
    render(<AnalysisWorkspace />);
    const table = await screen.findByRole('table', { name: 'Current analysis rules' });
    const user = userEvent.setup();
    const choice = within(table).getByRole('button', { name: 'PoC_Vault/users.cypher' });
    choice.focus();
    await user.keyboard('{Enter}');
    const panel = screen.getByRole('region', { name: 'Query details' });
    expect(within(panel).getByRole('heading', { name: 'PoC_Vault/users.cypher' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' })).toHaveAttribute(
        'aria-pressed',
        'true'
    );
    await user.click(within(panel).getByRole('button', { name: 'PoC_AD/users.cypher' }));
    expect(within(table).getByRole('button', { name: 'PoC_AD/users.cypher' })).toHaveAttribute('aria-pressed', 'true');
    await user.click(within(panel).getByRole('tab', { name: 'Last run' }));
    expect(within(panel).getByText('Rows:').parentElement).toHaveTextContent('Rows: 0');
    await user.click(within(panel).getByRole('tab', { name: 'Cypher' }));
    expect(within(panel).getByLabelText('Original Cypher file')).toHaveTextContent('// namespace: PoC_AD');
    expect(requests.every((request) => request.method === 'GET')).toBe(true);
});

it('shows zero-based dependency depth in the table and query details', async () => {
    render(<AnalysisWorkspace />);
    const table = await screen.findByRole('table', { name: 'Current analysis rules' });
    expect(within(table).getByRole('columnheader', { name: 'Order / dependency depth' })).toBeVisible();
    expect(within(table).getByRole('cell', { name: '1 / 0' })).toBeVisible();
    expect(within(table).getByRole('cell', { name: '3 / 2' })).toBeVisible();
    expect(within(table).getByRole('cell', { name: '4 / 0' })).toBeVisible();
    await userEvent.click(within(table).getByRole('button', { name: 'PoC_AD/users.cypher' }));
    expect(
        within(screen.getByRole('region', { name: 'Query details' })).getByText('Dependency depth:').parentElement
    ).toHaveTextContent('Dependency depth: 0');
});

it('lists extensions in the filter by name, without numbers, and the filter hides other extensions on the graph', async () => {
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    const user = userEvent.setup();
    await user.click(screen.getByRole('combobox', { name: 'Extension' }));
    expect(screen.getByRole('option', { name: 'Active Directory (PoC_AD)' })).toBeVisible();
    expect(screen.getByRole('option', { name: 'Other (PoC_Other)' })).toBeVisible();
    await user.click(screen.getByRole('option', { name: 'Vault (PoC_Vault)' }));
    const graph = screen.getByLabelText('Query graph');
    expect(within(graph).queryByRole('button', { name: 'Graph PoC_AD/users.cypher' })).toBeNull();
    expect(within(graph).getByRole('button', { name: 'Graph PoC_Vault/users.cypher' })).toBeVisible();
});

it('filters only the table when a lane is clicked, and undoes it from the lane, the chip, or Clear filters', async () => {
    render(<AnalysisWorkspace />);
    const table = await screen.findByRole('table', { name: 'Current analysis rules' });
    const graph = screen.getByLabelText('Query graph');
    const user = userEvent.setup();
    const rows = () => within(table).getAllByRole('row').length - 1;
    expect(rows()).toBe(4);

    // Clicking the Vault lane: the table shows Vault's two queries, the lane is
    // marked as pressed, and the graph still has every query.
    await user.click(within(graph).getByRole('button', { name: 'Lane PoC_Vault' }));
    expect(rows()).toBe(2);
    expect(within(table).getByRole('button', { name: 'PoC_Vault/edges.cypher' })).toBeVisible();
    expect(within(graph).getByRole('button', { name: 'Lane PoC_Vault' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(graph).getByRole('button', { name: 'Graph PoC_AD/users.cypher' })).toBeVisible();
    expect(screen.getByText('Table: Vault')).toBeVisible();
    expect(screen.getByText('Rules:').parentElement).toHaveTextContent('Rules: 2');

    // Clicking the same lane again shows every extension again.
    await user.click(within(graph).getByRole('button', { name: 'Lane PoC_Vault' }));
    expect(rows()).toBe(4);
    expect(screen.queryByText('Table: Vault')).toBeNull();

    // The chip's remove button undoes it.
    await user.click(within(graph).getByRole('button', { name: 'Lane PoC_AD' }));
    expect(rows()).toBe(1);
    await user.click(
        within(screen.getByRole('button', { name: 'Table shows only Active Directory' })).getByTestId('CancelIcon')
    );
    expect(rows()).toBe(4);

    // Clear filters undoes it too.
    await user.click(within(graph).getByRole('button', { name: 'Lane PoC_Other' }));
    expect(rows()).toBe(1);
    await user.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(rows()).toBe(4);

    // Picking an extension in the Extension filter replaces a clicked lane.
    await user.click(within(graph).getByRole('button', { name: 'Lane PoC_Other' }));
    await user.click(screen.getByRole('combobox', { name: 'Extension' }));
    await user.click(screen.getByRole('option', { name: 'Vault (PoC_Vault)' }));
    expect(rows()).toBe(2);
    expect(screen.queryByText('Table: Other')).toBeNull();
});

it('clears a clicked lane when a details link focuses a query outside it', async () => {
    render(<AnalysisWorkspace />);
    const table = await screen.findByRole('table', { name: 'Current analysis rules' });
    const graph = screen.getByLabelText('Query graph');
    const user = userEvent.setup();
    await user.click(within(graph).getByRole('button', { name: 'Lane PoC_Other' }));
    expect(within(table).getAllByRole('row')).toHaveLength(2);
    await user.click(within(graph).getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    expect(within(table).getAllByRole('row')).toHaveLength(5);
    expect(screen.queryByText('Table: Other')).toBeNull();
});

it('coordinates edge focus with details, keyboard dependency links, filters, and clear focus', async () => {
    render(<AnalysisWorkspace />);
    const table = await screen.findByRole('table', { name: 'Current analysis rules' });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Graph edge PoC_AD/users.cypher to PoC_Vault/users.cypher' }));
    let panel = screen.getByRole('region', { name: 'Dependency details' });
    expect(within(panel).getByText('As $directory_users')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Reveal hidden queries' })).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Query details' })).not.toBeInTheDocument();
    await user.click(within(panel).getByRole('button', { name: 'PoC_Vault/users.cypher' }));
    fireEvent.change(screen.getByLabelText('Search queries'), { target: { value: 'PoC_Vault' } });
    const focusEdge = screen.getByRole('button', {
        name: 'Focus dependency path from PoC_AD/users.cypher to PoC_Vault/users.cypher',
    });
    focusEdge.focus();
    await user.keyboard('{Enter}');
    expect(screen.getByLabelText('Search queries')).toHaveValue('');
    panel = screen.getByRole('region', { name: 'Dependency details' });
    expect(panel).toBeVisible();
    expect(within(table).getAllByText('Prerequisite')).toHaveLength(1);
    expect(within(table).getAllByText('Consumer')).toHaveLength(2);
    await user.click(screen.getByRole('button', { name: 'Clear focus' }));
    expect(screen.queryByRole('region', { name: 'Dependency details' })).not.toBeInTheDocument();
    expect(within(table).queryByText('Prerequisite')).not.toBeInTheDocument();
});

it('explains filtered dependencies and reveals a hidden prerequisite from details', async () => {
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' }));
    fireEvent.change(screen.getByLabelText('Search queries'), { target: { value: 'PoC_Vault' } });
    expect(screen.getByText(/related queries are hidden by filters/)).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Graph PoC_AD/users.cypher' })).not.toBeInTheDocument();
    await userEvent.click(
        within(screen.getByRole('region', { name: 'Query details' })).getByRole('button', {
            name: 'PoC_AD/users.cypher',
        })
    );
    expect(screen.getByLabelText('Search queries')).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' })).toHaveAttribute('aria-pressed', 'true');
});

it('keeps a mismatched report separate and clears focus when the revision changes', async () => {
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    workspace = { ...workspace, plan: { ...workspace.plan, schema_revision: 'c'.repeat(64) } };
    await userEvent.click(screen.getByRole('button', { name: 'Refresh rules and status' }));
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Query details' })).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Latest status report' }));
    await screen.findByText(/latest completed attempt has a different rule or schema revision/);
    await userEvent.click(screen.getByRole('button', { name: 'Rules' }));
    expect(screen.queryByRole('region', { name: 'Query details' })).not.toBeInTheDocument();
    const table = screen.getByRole('table', { name: 'Current analysis rules' });
    expect(within(table).queryByText('succeeded')).not.toBeInTheDocument();
    expect(within(table).getAllByText('Unavailable')).toHaveLength(4);
});

it('explains an absent prerequisite without claiming that filters can reveal it', async () => {
    workspace.plan.queries = workspace.plan.queries.filter((query) => query.name !== 'PoC_AD/users.cypher');
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' }));
    expect(screen.getAllByText('PoC_AD/users.cypher (not in current rules)').length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: 'Reveal hidden queries' })).not.toBeInTheDocument();
});

it('rejects a stale download and sends only the displayed revisions and query name', async () => {
    let parameters: URLSearchParams | undefined;
    server.use(
        rest.get('/api/v2/analysis-workspace/query', (request, response, context) => {
            parameters = request.url.searchParams;
            return response(context.status(409), context.json({ errors: [{ message: 'changed' }] }));
        })
    );
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    await userEvent.click(screen.getByRole('button', { name: 'Download query' }));
    await screen.findByText('Rules or schemas changed. Refresh before downloading this query.');
    expect(Object.fromEntries(parameters!)).toEqual({
        query: 'PoC_AD/users.cypher',
        revision: workspace.plan.revision,
        schema_revision: workspace.plan.schema_revision,
    });
    expect(requests.every((request) => request.method === 'GET')).toBe(true);
});

it('shows no active rules and no completed report without fabricating a graph', async () => {
    workspace.plan.queries = [];
    workspace.plan.revision = '';
    workspace.dependencies = [];
    workspace.status.state = 'unavailable';
    delete workspace.status.execution;
    render(<AnalysisWorkspace />);
    await screen.findByText(/No activated analysis rules/);
    await userEvent.click(screen.getByRole('button', { name: 'Latest status report' }));
    expect(screen.getByText('No completed attempt report is available.')).toBeVisible();
    expect(screen.queryByLabelText('Query graph')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Upload analysis files' })).toBeEnabled();
});

it('opens uploads, the editor, and the rules table from the bottom-left buttons', async () => {
    const user = userEvent.setup();
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    const actions = screen.getByLabelText('Analysis file actions');
    await user.click(within(actions).getByRole('button', { name: 'Upload analysis files' }));
    const upload = screen.getByRole('dialog', { name: 'Upload analysis files' });
    expect(within(upload).getByLabelText('Analysis files')).toHaveAttribute('accept', '.cypher,.zip');
    await user.click(within(upload).getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: 'Close results panel' }));
    expect(screen.queryByRole('table', { name: 'Current analysis rules' })).not.toBeInTheDocument();
    await user.click(within(actions).getByRole('button', { name: 'Open rules table' }));
    expect(screen.getByRole('table', { name: 'Current analysis rules' })).toBeVisible();

    server.use(
        rest.get('/api/v2/analysis-workspace/working-copies', (_, response, context) =>
            response(context.json({ data: [] }))
        )
    );
    await user.click(within(actions).getByRole('button', { name: 'Create analysis file' }));
    expect(screen.getByRole('dialog', { name: 'Analysis file editor' })).toBeVisible();
});

it('shows revisions and a colored legend in the top panel', async () => {
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    expect(screen.getByText('Current rule revision:').parentElement).toHaveTextContent(workspace.plan.revision);
    const legend = screen.getByLabelText('Graph color legend');
    expect(within(legend).getByText('Blocked or set aside').previousElementSibling).toHaveStyle({
        backgroundColor: '#ed8b00',
    });
    expect(within(legend).getByText('Failed').previousElementSibling).toHaveStyle({ backgroundColor: '#ef5350' });
    expect(within(legend).getByText('Disabled').previousElementSibling).toHaveStyle({ backgroundColor: '#9e9e9e' });
    expect(screen.queryByText(/Click a node or arrow/)).not.toBeInTheDocument();
});

it('keeps run-level failures and uncertain saving separate from calculation success', async () => {
    workspace.status.state = 'uncertain';
    workspace.status.results_uncertain = true;
    workspace.status.execution!.save = 'uncertain';
    delete workspace.status.execution!.saved;
    workspace.status.execution!.failure = { phase: 'saving', message: 'The database did not confirm saving.' };
    render(<AnalysisWorkspace />);
    await userEvent.click(await screen.findByRole('button', { name: 'Latest status report' }));
    await screen.findByText(/latest save outcome is uncertain/);
    expect(screen.getByText(/Run-level failure/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Jump to failure' })).not.toBeInTheDocument();
    expect(screen.getByText('Calculation:').parentElement).toHaveTextContent('Calculation: succeeded');
    expect(screen.getByText('Saving:').parentElement).toHaveTextContent('Saving: uncertain');
});

it.each([401, 403, 502, 503])('explains rejected or unavailable reads (%s)', async (status) => {
    server.use(
        rest.get('/api/v2/analysis-workspace', (request, response, context) => response(context.status(status)))
    );
    render(<AnalysisWorkspace />);
    await waitFor(() => expect(screen.getByRole('alert')).toBeVisible());
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(requests.every((request) => request.method === 'GET')).toBe(true);
});

it('reserves room for the entity panel and supports keyboard resizing of the bottom panel', async () => {
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    const panel = screen.getByTestId('analysis-bottom-panel');
    expect(panel.className).not.toContain('md:right-[var(--analysis-panel-space)]');
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    expect(panel.className).toContain('md:right-[var(--analysis-panel-space)]');
    fireEvent.keyDown(screen.getByRole('separator', { name: 'Resize results panel' }), { key: 'ArrowUp' });
    expect(panel).toHaveStyle({ height: '352px' });
    await userEvent.click(screen.getByRole('button', { name: 'Close entity panel' }));
    expect(panel.className).not.toContain('md:right-[var(--analysis-panel-space)]');
});

it('keeps bulk selection separate from focus and reviews hidden selected rules before activation', async () => {
    const writes: { path: string; body: unknown }[] = [];
    server.use(
        rest.post('/api/v2/analysis-workspace/preview', async (request, response, context) => {
            const body = await request.json();
            writes.push({ path: request.url.pathname, body });
            return response(
                context.json({
                    data: {
                        base_revision: workspace.plan.revision,
                        revision: 'd'.repeat(64),
                        removed: [],
                        overwritten: [],
                        disabled: body.disable,
                        enabled: [],
                        paused: body.disable,
                        retired_relationships: [],
                        recalculated_relationships: [],
                        plan: { ...workspace.plan, revision: 'd'.repeat(64) },
                        dependencies: workspace.dependencies,
                    },
                })
            );
        })
    );
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('checkbox', { name: 'Select PoC_AD/users.cypher for changes' }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Select PoC_Vault/users.cypher for changes' }));
    expect(screen.queryByRole('region', { name: 'Query details' })).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Search queries'), { target: { value: 'PoC_Vault' } });
    expect(screen.getByText('Selected for changes:').parentElement).toHaveTextContent('2 (1 hidden)');
    await userEvent.click(screen.getByRole('button', { name: 'Disable selected' }));
    await screen.findByRole('region', { name: 'Review analysis changes' });
    expect(writes).toEqual([
        {
            path: '/api/v2/analysis-workspace/preview',
            body: {
                base_revision: workspaceFixture().plan.revision,
                schema_revision: workspaceFixture().plan.schema_revision,
                disable: ['PoC_AD/users.cypher', 'PoC_Vault/users.cypher'],
            },
        },
    ]);
    expect(screen.getByRole('button', { name: 'Activate reviewed revision' })).toBeEnabled();
    expect(requests.some((request) => request.method === 'PUT')).toBe(false);
    await userEvent.click(screen.getByRole('button', { name: 'Discard preview' }));
    expect(screen.queryByRole('region', { name: 'Review analysis changes' })).not.toBeInTheDocument();
    expect(
        screen.queryByText('Review the proposed rules and every listed effect before activation.')
    ).not.toBeInTheDocument();
});

it('disables management controls for readers', async () => {
    canEdit = false;
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    expect(screen.getByRole('checkbox', { name: 'Select all visible rules' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Run Analysis' })).toBeDisabled();
    expect(requests.every((request) => request.method === 'GET')).toBe(true);
});

it('rejects stale reviews and requires refresh before another change', async () => {
    server.use(
        rest.post('/api/v2/analysis-workspace/preview', (_, response, context) => response(context.status(409)))
    );
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    await userEvent.click(screen.getByRole('button', { name: 'Disable' }));
    await screen.findByText('Rules, schemas, or the saved draft changed. Refresh and review again.');
    expect(screen.getByRole('button', { name: 'Disable' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Activate reviewed revision' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Refresh rules and status' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Disable' })).toBeEnabled());
});

it('widens the entity panel with the keyboard and keeps intermediate counts out of graph-output counts', async () => {
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    fireEvent.keyDown(screen.getByRole('separator', { name: 'Resize entity panel' }), { key: 'ArrowLeft' });
    expect(screen.getByRole('complementary', { name: 'Analysis entity panel' })).toHaveStyle({ width: '512px' });
    expect(screen.getByTestId('analysis-workspace').style.getPropertyValue('--analysis-panel-space')).toBe('552px');
    const panel = screen.getByRole('region', { name: 'Query details' });
    await userEvent.click(within(panel).getByRole('tab', { name: 'Last run' }));
    expect(within(panel).getByText('Graph outputs:').parentElement).toHaveTextContent('Not applicable');
    expect(within(panel).queryByText('Confirmed graph output rows:')).not.toBeInTheDocument();
    await userEvent.click(within(panel).getByRole('tab', { name: 'Overview' }));
    await userEvent.hover(within(panel).getByRole('button', { name: 'About dependency depth' }));
    expect(await screen.findByRole('tooltip')).toHaveTextContent(
        'Dependency depth counts arrows along the longest prerequisite chain, starting at 0. It does not indicate simultaneous execution.'
    );
});

it('shows missing publishing counts as unreported or unsaved rather than zero', async () => {
    workspace.plan.queries[0].outputs = [{ type: 'relationship', kind: 'PoC_Connection' }];
    workspace.status.state = 'failed';
    workspace.status.execution!.save = 'not-started';
    delete workspace.status.execution!.queries[0].proposed_outputs;
    delete workspace.status.execution!.queries[0].saved_outputs;
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    const panel = screen.getByRole('region', { name: 'Query details' });
    await userEvent.click(within(panel).getByRole('tab', { name: 'Last run' }));
    expect(within(panel).getByText('Proposed graph output rows:').parentElement).toHaveTextContent('Not reported');
    expect(within(panel).getByText('Confirmed graph output rows:').parentElement).toHaveTextContent(
        'No save in this attempt'
    );
});

it('identifies requested and extra deletions before activation', async () => {
    server.use(
        rest.post('/api/v2/analysis-workspace/preview', async (request, response, context) => {
            const body = await request.json();
            expect(body.remove).toEqual(['PoC_AD/users.cypher']);
            return response(
                context.json({
                    data: {
                        base_revision: workspace.plan.revision,
                        revision: 'd'.repeat(64),
                        removed: ['PoC_AD/users.cypher', 'PoC_Vault/users.cypher', 'PoC_Vault/edges.cypher'],
                        overwritten: [],
                        disabled: [],
                        enabled: [],
                        paused: [],
                        retired_relationships: ['PoC_Connection'],
                        recalculated_relationships: ['PoC_Connection'],
                        plan: { ...workspace.plan, revision: 'd'.repeat(64), queries: [workspace.plan.queries[3]] },
                        dependencies: [],
                    },
                })
            );
        })
    );
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const review = await screen.findByRole('region', { name: 'Review analysis changes' });
    expect(within(review).getByText('Requested deletions:').parentElement).toHaveTextContent('PoC_AD/users.cypher');
    expect(within(review).getByText('Additional files removed:').parentElement).toHaveTextContent(
        'PoC_Vault/users.cypher, PoC_Vault/edges.cypher'
    );
    expect(within(review).getByText(/Disable keeps the files/)).toBeVisible();
    expect(requests.some((request) => request.method === 'PUT')).toBe(false);
});

it('gives the graph each query outcome from the matching completed run so failed steps can be drawn red', async () => {
    workspace.status.execution!.queries[0].state = 'failed';
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    expect(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' })).toHaveAttribute('data-outcome', 'failed');
    expect(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' })).toHaveAttribute(
        'data-outcome',
        'succeeded'
    );
});

it('shows each file fact once: header lines only in the Cypher tab', async () => {
    workspace.plan.queries[0].extension_versions = '>=v1.0.0';
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' }));
    const panel = screen.getByRole('region', { name: 'Query details' });
    // Actions share one row in the header, beside the status.
    expect(within(panel).getByRole('button', { name: 'Disable' })).toBeVisible();
    expect(within(panel).getByRole('button', { name: 'Edit/Test analysis file' })).toBeVisible();
    // Overview shows connections, not the declarations already in the file.
    expect(within(panel).getByText('Needs')).toBeVisible();
    expect(within(panel).getByRole('button', { name: 'PoC_AD/users.cypher' })).toBeVisible();
    for (const repeated of ['Namespace:', 'Analysis version:', 'Allowed schema versions:', 'Graph inputs']) {
        expect(within(panel).queryByText(repeated)).not.toBeInTheDocument();
    }
});

it('offers Disable for an enabled rule and Enable for a disabled one', async () => {
    workspace.plan.queries[1].author_enabled = false;
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    let panel = screen.getByRole('region', { name: 'Query details' });
    expect(within(panel).getByRole('button', { name: 'Disable' })).toBeVisible();
    expect(within(panel).queryByRole('button', { name: 'Enable' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' }));
    panel = screen.getByRole('region', { name: 'Query details' });
    expect(within(panel).getByRole('button', { name: 'Enable' })).toBeVisible();
    expect(within(panel).queryByRole('button', { name: 'Disable' })).not.toBeInTheDocument();
    // Bulk actions on a mixed selection still offer both.
    expect(screen.getByRole('button', { name: 'Enable selected' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Disable selected' })).toBeInTheDocument();
});

it('shows a failed run with a link to the failed step, and a spinner while rules run', async () => {
    workspace.status.state = 'failed';
    workspace.status.message = 'The name property is empty (null) for 5 helper objects.';
    workspace.status.execution!.failure = { query: 'PoC_AD/users.cypher', phase: 'check-answers', message: '' };
    render(<AnalysisWorkspace />);
    const line = await screen.findByLabelText('Analysis status');
    await waitFor(() => expect(line).toHaveTextContent('Failed at PoC_AD/users.cypher.'));
    expect(line).toHaveTextContent('The name property is empty (null) for 5 helper objects.');
    expect(line).toHaveClass('MuiAlert-standardError');
    await userEvent.click(within(line).getByRole('button', { name: 'PoC_AD/users.cypher' }));
    expect(
        within(screen.getByRole('region', { name: 'Query details' })).getByRole('heading', {
            name: 'PoC_AD/users.cypher',
        })
    ).toBeVisible();
});

it('shows a spinner and disables Run Analysis while extension rules run', async () => {
    workspace.status.state = 'running';
    render(<AnalysisWorkspace />);
    const line = await screen.findByLabelText('Analysis status');
    await waitFor(() => expect(line).toHaveTextContent('Running extension rules…'));
    expect(within(line).getByLabelText('Analysis in progress')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Run Analysis' })).toBeDisabled();
});

it('warns and links to File Ingest when BloodHound has been busy for a long time', async () => {
    server.use(
        rest.get('/api/v2/datapipe/status', (_, response, context) =>
            response(
                context.json({
                    data: {
                        status: 'ingesting',
                        updated_at: new Date(Date.now() - 20 * 60000).toISOString(),
                        last_complete_analysis_at: '2026-10-16T13:38:23Z',
                    },
                })
            )
        ),
        rest.get('/api/v2/analysis/status', (_, response, context) =>
            response(
                context.json({
                    data: { request_type: 'analysis', requested_by: 'init', requested_at: new Date().toISOString() },
                })
            )
        )
    );
    render(<AnalysisWorkspace />);
    const line = await screen.findByLabelText('Analysis status');
    await waitFor(() => expect(line).toHaveTextContent('BloodHound has been ingesting uploaded data for 20 minutes.'));
    expect(line).toHaveClass('MuiAlert-standardWarning');
    expect(line).toHaveTextContent('Analysis, extension rules, and editor tests wait until it finishes.');
    expect(within(line).getByRole('link', { name: 'Open File Ingest' })).toHaveAttribute(
        'href',
        '/administration/file-ingest'
    );
});

it('shows why the failed step failed in its Last run tab', async () => {
    workspace.status.state = 'failed';
    workspace.status.execution!.queries[0].state = 'failed';
    workspace.status.execution!.queries[0].reason = 'The rules are new or changed.';
    workspace.status.execution!.failure = {
        query: 'PoC_AD/users.cypher',
        phase: 'check-answers',
        message: 'The name property is empty (null) for 5 helper objects.',
    };
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_AD/users.cypher' }));
    const panel = screen.getByRole('region', { name: 'Query details' });
    await userEvent.click(within(panel).getByRole('tab', { name: 'Last run' }));
    expect(within(panel).getByLabelText('Why it failed')).toHaveTextContent(
        'The name property is empty (null) for 5 helper objects.'
    );
    expect(within(panel).getByText('Why it ran:').parentElement).toHaveTextContent('The rules are new or changed.');
    // Another step's panel does not show this step's failure.
    await userEvent.click(screen.getByRole('button', { name: 'Graph PoC_Vault/users.cypher' }));
    await userEvent.click(
        within(screen.getByRole('region', { name: 'Query details' })).getByRole('tab', { name: 'Last run' })
    );
    expect(screen.queryByLabelText('Why it failed')).not.toBeInTheDocument();
});

it('opens a step on its Last run tab from the analysis report, with last ran and last success times', async () => {
    const earlier = {
        ...workspace.status,
        run_id: 'attempt-0',
        updated_at: '2026-10-14T11:00:00Z',
        history: undefined,
    };
    workspace.status.history = [earlier];
    render(<AnalysisWorkspace />);
    await screen.findByRole('table', { name: 'Current analysis rules' });
    await userEvent.click(screen.getByRole('button', { name: 'Latest status report' }));
    const report = await screen.findByRole('table', { name: 'Completed query outcomes' });
    expect(within(report).getByRole('columnheader', { name: 'Last success' })).toBeVisible();
    await userEvent.click(within(report).getByRole('button', { name: 'PoC_Vault/users.cypher' }));
    const panel = screen.getByRole('region', { name: 'Query details' });
    expect(within(panel).getByRole('heading', { name: 'PoC_Vault/users.cypher' })).toBeVisible();
    expect(within(panel).getByRole('tab', { name: 'Last run' })).toHaveAttribute('aria-selected', 'true');
    // The newest successful run counts, not the older one kept in history.
    expect(within(panel).getByText('Last ran:').parentElement).toHaveTextContent(
        new Date('2026-10-14T12:01:00Z').toLocaleString()
    );
    expect(within(panel).getByText('Last success:').parentElement).toHaveTextContent(
        new Date('2026-10-14T12:01:00Z').toLocaleString()
    );
});
