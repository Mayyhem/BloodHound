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

import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from 'src/test-utils';
import AnalysisFileEditor from './AnalysisFileEditor';
import * as api from './api';
import { queryFixture, workspaceFixture } from './testData';
import { EditorTestResult, EditorWorkingCopy, Review, Workspace } from './types';

// The real dependency window draws on a WebGL canvas, which this test
// environment lacks. This stand-in lists the same nodes and arrows as text.
vi.mock('./QueryGraph', () => ({
    default: ({ workspace, focused }: { workspace: Workspace; focused: string | null }) => (
        <div data-testid='dependency-canvas' data-focused={focused}>
            {workspace.plan.queries.map((query) => (
                <span key={query.name} data-node-id={query.name} />
            ))}
            {workspace.dependencies.map((dependency, index) => (
                <span key={index} data-source-id={dependency.prerequisite} data-target-id={dependency.consumer} />
            ))}
        </div>
    ),
}));

// The real Cypher box draws highlighted code with CodeMirror. These tests
// check the editor's own behavior, so a plain text box stands in for it. It
// lists the $parameter names the box would suggest.
vi.mock('./CypherText', () => ({
    default: ({
        label,
        value,
        onChange,
        readOnly,
        suggestions,
    }: {
        label: string;
        value: string;
        onChange?: (text: string) => void;
        readOnly?: boolean;
        suggestions?: { parameters?: string[] };
    }) => (
        <textarea
            aria-label={label}
            value={value}
            readOnly={readOnly}
            data-parameters={suggestions?.parameters?.join(',')}
            onChange={(event) => onChange?.(event.target.value)}
        />
    ),
}));

// The editor asks BloodHound for its node and relationship types to suggest
// while typing. These tests do not need them.
vi.mock('bh-shared-ui', async (original) => ({
    ...(await original<typeof import('bh-shared-ui')>()),
    useCypherSchema: () => ({ labels: [], relationshipTypes: [], propertyKeys: [] }),
}));

vi.mock('./api', () => ({
    activateEditorTest: vi.fn(),
    listEditorWorkingCopies: vi.fn(),
    previewEditorPlan: vi.fn(),
    readEditorWorkingCopy: vi.fn(),
    reviewEditorTest: vi.fn(),
    saveEditorWorkingCopy: vi.fn(),
    testEditorFile: vi.fn(),
}));

const result: EditorTestResult = {
    test_id: 'c'.repeat(64),
    rows: [{ objectid: 'S-1-5-21-1', details: { kept: true } }],
    total_rows: 1,
    truncated: false,
    execution: {} as never,
};
const review: Review = {
    base_revision: 'a'.repeat(64),
    revision: 'e'.repeat(64),
    removed: [],
    overwritten: [],
    retired_relationships: [],
    recalculated_relationships: [],
    paused: [],
    disabled: [],
    enabled: ['PoC_AD/new_analysis.cypher'],
    plan: workspaceFixture().plan,
    dependencies: [],
};
const savedCopy: EditorWorkingCopy = {
    name: 'PoC_AD/saved.cypher',
    text: '// namespace: PoC_AD\n// version: v1.0.0\n// depends-on: query:users as local_users\nUNWIND $local_users AS row RETURN row.value',
    base_revision: 'old',
    revision: 'saved-revision',
};

function renderEditor(options: { canEdit?: boolean; initial?: EditorWorkingCopy; workspace?: Workspace } = {}) {
    const workspace = options.workspace || workspaceFixture();
    const onClose = vi.fn(),
        onActivated = vi.fn();
    const props = {
        open: true,
        workspace,
        canEdit: options.canEdit ?? true,
        initial: options.initial,
        onClose,
        onActivated,
    };
    return { ...render(<AnalysisFileEditor {...props} />), props, onClose, onActivated };
}
async function completeTest(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole('button', { name: 'Test file' }));
    await screen.findByRole('button', { name: 'Review tested file' });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Review tested file' })).toBeEnabled());
}

beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(api.activateEditorTest).mockResolvedValue({ revision: review.revision });
    vi.mocked(api.listEditorWorkingCopies).mockResolvedValue([savedCopy]);
    vi.mocked(api.readEditorWorkingCopy).mockResolvedValue(savedCopy);
    vi.mocked(api.saveEditorWorkingCopy).mockImplementation(async (copy) => ({ ...copy, revision: 'saved-revision' }));
    vi.mocked(api.testEditorFile).mockResolvedValue(result);
    vi.mocked(api.reviewEditorTest).mockResolvedValue(review);
    vi.mocked(api.previewEditorPlan).mockImplementation(async (request) => ({
        plan: { ...workspaceFixture().plan, queries: [queryFixture(request.name, 0)] },
        dependencies: [],
    }));
});

it("suggests each declared prerequisite's alias as a $parameter while typing", async () => {
    const user = userEvent.setup();
    renderEditor();
    expect(screen.getByLabelText('Cypher')).toHaveAttribute('data-parameters', '');
    await user.click(screen.getByRole('combobox', { name: 'Prerequisites' }));
    await user.click(screen.getByRole('option', { name: 'PoC_AD/users.cypher' }));
    await user.keyboard('{Escape}');
    expect(screen.getByLabelText('Cypher')).toHaveAttribute('data-parameters', 'users');
    // An alias renamed in the text is what gets suggested.
    fireEvent.change(screen.getByLabelText('Cypher'), {
        target: {
            value: '// namespace: PoC_AD\n// version: v1.0.0\n// depends-on: query:users as directory_users\nRETURN 1',
        },
    });
    expect(screen.getByLabelText('Cypher')).toHaveAttribute('data-parameters', 'directory_users');
});

it('adds a prerequisite from the dropdown before Cypher and removes the line again', async () => {
    const user = userEvent.setup();
    renderEditor();
    // A new file starts without a // depends-on line; none is not required.
    expect((screen.getByLabelText('Cypher') as HTMLTextAreaElement).value).not.toContain('depends-on');
    expect(screen.queryByLabelText('Dependency graph')).not.toBeInTheDocument();
    await user.click(screen.getByRole('combobox', { name: 'Prerequisites' }));
    await user.click(screen.getByRole('option', { name: 'PoC_AD/users.cypher' }));
    await user.keyboard('{Escape}');
    const editor = screen.getByLabelText('Cypher') as HTMLTextAreaElement;
    expect(editor.value.indexOf('depends-on: query:PoC_AD/users as users')).toBeLessThan(
        editor.value.indexOf('RETURN')
    );
    expect(editor.value).not.toContain('depends-on: none');
    // The graph shows only the chosen prerequisite and this file, not every active query.
    const graph = screen.getByLabelText('Dependency graph');
    expect(graph.querySelectorAll('[data-node-id]')).toHaveLength(2);
    expect(graph.querySelector('[data-source-id="PoC_AD/users.cypher"]')).toHaveAttribute(
        'data-target-id',
        'PoC_AD/new_analysis.cypher'
    );
    expect(screen.getByTestId('dependency-canvas')).toHaveAttribute('data-focused', 'PoC_AD/new_analysis.cypher');
    // Aliases are edited in the Cypher text; there is no separate alias box.
    expect(screen.queryByLabelText(/alias/)).not.toBeInTheDocument();
    await user.click(screen.getByRole('combobox', { name: 'Prerequisites' }));
    await user.click(screen.getByRole('option', { name: 'PoC_AD/users.cypher' }));
    expect(editor.value).not.toContain('depends-on');
    expect(screen.queryByLabelText('Dependency graph')).not.toBeInTheDocument();
});

it('filters the prerequisite dropdown by name', async () => {
    const user = userEvent.setup();
    renderEditor();
    await user.type(screen.getByRole('combobox', { name: 'Prerequisites' }), 'vault');
    const options = screen.getAllByRole('option').map((option) => option.textContent);
    expect(options).toEqual(['PoC_Vault/users.cypher', 'PoC_Vault/edges.cypher']);
});

it('shows only consumers that read the edited file, alongside its prerequisites', async () => {
    renderEditor({
        initial: {
            name: 'PoC_AD/users.cypher',
            text: '// namespace: PoC_AD\n// version: v1.0.0\n// depends-on: none\nRETURN 1',
            base_revision: '',
            revision: '',
        },
    });
    const graph = screen.getByLabelText('Dependency graph');
    const nodes = [...graph.querySelectorAll('[data-node-id]')].map((node) => node.getAttribute('data-node-id'));
    expect(nodes).toEqual(['PoC_AD/users.cypher', 'PoC_Vault/users.cypher', 'PoC_Vault/edges.cypher']);
    expect(nodes).not.toContain('PoC_Other/alone.cypher');
});

it('keeps typed local declarations and aliases visible and blocks conflicting aliases', async () => {
    const user = userEvent.setup();
    renderEditor({ initial: savedCopy });
    expect(screen.getByText('PoC_AD/users.cypher')).toBeVisible();
    fireEvent.change(screen.getByLabelText('Cypher'), {
        target: {
            value: '// namespace: PoC_AD\n// version: v1.0.0\n// depends-on: query:users as same\n// depends-on: query:PoC_Vault/users as same\nRETURN 1',
        },
    });
    expect(screen.getByText('Each query must use a different alias.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Review prerequisites' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Save private copy' }));
    await waitFor(() => expect(api.saveEditorWorkingCopy).toHaveBeenCalled());
});

it('resumes private text against current rules and invalidates an earlier dependency preview', async () => {
    const user = userEvent.setup();
    const { props } = renderEditor();
    await user.click(screen.getByRole('button', { name: 'Review prerequisites' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Test file' })).toBeEnabled());
    await user.click(screen.getByRole('combobox', { name: 'Resume saved copy' }));
    await user.click(screen.getByRole('option', { name: savedCopy.name }));
    await user.click(screen.getByRole('button', { name: 'Resume' }));
    await waitFor(() => expect(screen.getByLabelText('Cypher')).toHaveValue(savedCopy.text));
    expect(screen.getByText('PoC_AD/users.cypher')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Review prerequisites' }));
    await waitFor(() =>
        expect(api.previewEditorPlan).toHaveBeenLastCalledWith(
            expect.objectContaining({ text: savedCopy.text, base_revision: props.workspace.plan.revision }),
            expect.anything()
        )
    );
});

it('shows result rows and requires test then review then exact activation', async () => {
    const user = userEvent.setup();
    const { onActivated } = renderEditor();
    expect(screen.queryByRole('button', { name: 'Activate reviewed file' })).not.toBeInTheDocument();
    await completeTest(user);
    const rows = screen.getByRole('table', { name: 'Editor test result rows' });
    expect(within(rows).getByText('S-1-5-21-1')).toBeVisible();
    expect(within(rows).getByText('{"kept":true}')).toBeVisible();
    // Results sit between the Cypher box and the dependency section.
    const results = screen.getByRole('region', { name: 'Test results' });
    expect(
        screen.getByLabelText('Cypher').compareDocumentPosition(results) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    expect(
        results.compareDocumentPosition(screen.getByRole('region', { name: 'Dependencies' })) &
            Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Review tested file' }));
    await user.click(await screen.findByRole('button', { name: 'Activate reviewed file' }));
    await waitFor(() => expect(onActivated).toHaveBeenCalledWith('PoC_AD/new_analysis.cypher'));
    expect(api.activateEditorTest).toHaveBeenCalledWith(result.test_id, review.revision, expect.anything());
});

it('removes activation controls when tested file contents change', async () => {
    const user = userEvent.setup();
    renderEditor();
    await completeTest(user);
    await user.click(screen.getByRole('button', { name: 'Review tested file' }));
    await screen.findByRole('button', { name: 'Activate reviewed file' });
    fireEvent.change(screen.getByLabelText('Cypher'), {
        target: { value: '// namespace: PoC_AD\n// version: v1.0.0\n// depends-on: none\nRETURN 2 AS result' },
    });
    expect(screen.queryByRole('button', { name: 'Activate reviewed file' })).not.toBeInTheDocument();
    expect(api.activateEditorTest).not.toHaveBeenCalled();
});

it('cancels tests and ignores late success after closing and reopening', async () => {
    const user = userEvent.setup();
    let finish: ((value: EditorTestResult) => void) | undefined;
    vi.mocked(api.testEditorFile).mockImplementation(
        () =>
            new Promise((resolve) => {
                finish = resolve;
            })
    );
    const view = renderEditor();
    await user.click(screen.getByRole('button', { name: 'Review prerequisites' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Test file' })).toBeEnabled());
    await user.click(screen.getByRole('button', { name: 'Test file' }));
    await user.click(screen.getByRole('button', { name: 'Cancel test' }));
    expect(screen.getByText('Test canceled. Your editor contents were kept.')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Close' }));
    view.rerender(<AnalysisFileEditor {...view.props} open={false} />);
    view.rerender(<AnalysisFileEditor {...view.props} open />);
    await act(async () => {
        finish?.(result);
        await Promise.resolve();
    });
    expect(screen.queryByRole('button', { name: 'Review tested file' })).not.toBeInTheDocument();
});

it('prevents readonly edits and execution', async () => {
    renderEditor({ canEdit: false });
    expect(screen.getByRole('button', { name: 'Review prerequisites' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Save private copy' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Test file' })).toBeDisabled();
    expect(screen.getByRole('combobox', { name: 'Prerequisites' })).toBeDisabled();
    expect(api.testEditorFile).not.toHaveBeenCalled();
    expect(api.saveEditorWorkingCopy).not.toHaveBeenCalled();
});

it('shows empty values as null instead of a blank cell', async () => {
    const user = userEvent.setup();
    vi.mocked(api.testEditorFile).mockResolvedValueOnce({ ...result, rows: [{ 'n.objectid': null }] });
    renderEditor();
    await completeTest(user);
    expect(
        within(screen.getByRole('table', { name: 'Editor test result rows' })).getByRole('cell', { name: 'null' })
    ).toBeVisible();
});

it('says when a test returned no rows', async () => {
    const user = userEvent.setup();
    vi.mocked(api.testEditorFile).mockResolvedValueOnce({ ...result, rows: [], total_rows: 0 });
    renderEditor();
    await completeTest(user);
    expect(screen.getByText('The test returned no rows.')).toBeVisible();
    expect(screen.queryByRole('table', { name: 'Editor test result rows' })).not.toBeInTheDocument();
});

it('hides the dependency graph when the checked file has no prerequisites or consumers', async () => {
    const user = userEvent.setup();
    renderEditor();
    await completeTest(user);
    expect(screen.queryByLabelText('Needed prerequisites')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Dependency graph')).not.toBeInTheDocument();
});

it('shows the complete needed chain after the prerequisites are checked', async () => {
    const user = userEvent.setup();
    const users = queryFixture('PoC_AD/users.cypher', 1);
    const vault = queryFixture('PoC_Vault/users.cypher', 2);
    vi.mocked(api.previewEditorPlan).mockImplementation(async (request) => ({
        plan: { ...workspaceFixture().plan, queries: [users, vault, queryFixture(request.name, 3)] },
        dependencies: [
            { prerequisite: users.name, consumer: vault.name, type: 'query', name: users.name, parameter: 'users' },
            { prerequisite: vault.name, consumer: request.name, type: 'query', name: vault.name, parameter: 'users' },
        ],
    }));
    renderEditor();
    await user.click(screen.getByRole('combobox', { name: 'Prerequisites' }));
    await user.click(screen.getByRole('option', { name: 'PoC_Vault/users.cypher' }));
    await user.keyboard('{Escape}');
    expect(screen.getByLabelText('Dependency graph').querySelectorAll('[data-node-id]')).toHaveLength(2);
    await user.click(screen.getByRole('button', { name: 'Review prerequisites' }));
    await waitFor(() =>
        expect(screen.getByLabelText('Needed prerequisites')).toHaveTextContent(
            'Needed prerequisites: PoC_AD/users.cypher, PoC_Vault/users.cypher'
        )
    );
    expect(screen.getByLabelText('Dependency graph').querySelectorAll('[data-node-id]')).toHaveLength(3);
});

it('reverts to the opened file and then to the last saved copy', async () => {
    const user = userEvent.setup();
    renderEditor();
    const editor = screen.getByLabelText('Cypher') as HTMLTextAreaElement;
    const opened = editor.value;
    expect(screen.getByRole('button', { name: 'Revert' })).toBeDisabled();
    fireEvent.change(editor, { target: { value: opened.replace('RETURN 1', 'RETURN 2') } });
    await user.click(screen.getByRole('button', { name: 'Revert' }));
    expect(editor.value).toBe(opened);
    expect(screen.getByRole('button', { name: 'Revert' })).toBeDisabled();

    fireEvent.change(editor, { target: { value: opened.replace('RETURN 1', 'RETURN 3') } });
    await user.click(screen.getByRole('button', { name: 'Save private copy' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Revert' })).toBeDisabled());
    fireEvent.change(editor, { target: { value: opened.replace('RETURN 1', 'RETURN 4') } });
    await user.click(screen.getByRole('button', { name: 'Revert' }));
    expect(editor.value).toContain('RETURN 3');
});

it('checks prerequisites automatically when play is pressed first', async () => {
    const user = userEvent.setup();
    renderEditor();
    await user.click(screen.getByRole('button', { name: 'Test file' }));
    await screen.findByRole('button', { name: 'Review tested file' });
    expect(api.previewEditorPlan).toHaveBeenCalledTimes(1);
    expect(api.testEditorFile).toHaveBeenCalledTimes(1);
    // An unchanged file is not checked again before the next test.
    await user.click(screen.getByRole('button', { name: 'Test file' }));
    await waitFor(() => expect(api.testEditorFile).toHaveBeenCalledTimes(2));
    expect(api.previewEditorPlan).toHaveBeenCalledTimes(1);
});

it('shows the service explanation and skips the test when the automatic check fails', async () => {
    const user = userEvent.setup();
    const message =
        'Line 4 reads node:Domain, but the file does not declare it. Add it to a // depends-on: line, for example // depends-on: node:Domain.';
    vi.mocked(api.previewEditorPlan).mockRejectedValueOnce(
        Object.assign(new Error('Bad request'), {
            isAxiosError: true,
            response: { status: 400, data: { errors: [{ message }] } },
        })
    );
    renderEditor();
    await user.click(screen.getByRole('button', { name: 'Test file' }));
    expect(await screen.findByText(message)).toBeVisible();
    // Errors are red (MUI's error style); warnings are orange.
    expect(screen.getByText(message).closest('.MuiAlert-root')).toHaveClass('MuiAlert-standardError');
    expect(api.testEditorFile).not.toHaveBeenCalled();
});

it('warns about declarations the Cypher never uses', async () => {
    const user = userEvent.setup();
    vi.mocked(api.previewEditorPlan).mockImplementation(async (request) => ({
        plan: { ...workspaceFixture().plan, queries: [queryFixture(request.name, 0)] },
        dependencies: [],
        unused: ['node:Domain', 'query:PoC_AD/users'],
    }));
    renderEditor();
    await user.click(screen.getByRole('button', { name: 'Review prerequisites' }));
    const warning = await screen.findByLabelText('Unused dependencies');
    expect(warning).toHaveTextContent('Declared but never used: node:Domain, query:PoC_AD/users.');
    expect(warning).toHaveClass('MuiAlert-standardWarning');
});

it('follows the namespace line in the text when it names another installed extension', async () => {
    const workspace = workspaceFixture();
    workspace.installed_extensions = [
        { namespace: 'PoC_AD', name: 'ad', display_name: 'Active Directory' },
        { namespace: 'PoC_Vault', name: 'vault', display_name: 'Vault' },
    ];
    renderEditor({ workspace });
    expect(screen.getByRole('combobox', { name: 'Installed extension' })).toHaveTextContent('Active Directory');
    fireEvent.change(screen.getByLabelText('Cypher'), {
        target: { value: '// namespace: PoC_Vault\n// version: v1.0.0\n// depends-on: none\nRETURN 1 AS result' },
    });
    expect(screen.getByRole('combobox', { name: 'Installed extension' })).toHaveTextContent('Vault');
    await userEvent.click(screen.getByRole('button', { name: 'Test file' }));
    await waitFor(() =>
        expect(api.previewEditorPlan).toHaveBeenCalledWith(
            expect.objectContaining({ name: 'PoC_Vault/new_analysis.cypher' }),
            expect.anything()
        )
    );
});

it('describes what activation changes and places Activate after the review details', async () => {
    const user = userEvent.setup();
    renderEditor();
    await completeTest(user);
    await user.click(screen.getByRole('button', { name: 'Review tested file' }));
    const details = await screen.findByLabelText('Review details');
    expect(details).toHaveTextContent('Change: Adds this new file.');
    const activate = within(details).getByRole('button', { name: 'Activate reviewed file' });
    expect(
        activate.compareDocumentPosition(screen.getByRole('region', { name: 'Dependencies' })) &
            Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
});

it('says when the reviewed file matches or replaces the active copy', async () => {
    const user = userEvent.setup();
    const active = workspaceFixture().plan.queries[0];
    renderEditor({ initial: { name: active.name, text: active.text, base_revision: '', revision: '' } });
    await completeTest(user);
    await user.click(screen.getByRole('button', { name: 'Review tested file' }));
    expect(await screen.findByLabelText('Review details')).toHaveTextContent(
        'Change: Same text as the active copy; activating changes nothing.'
    );

    vi.mocked(api.reviewEditorTest).mockResolvedValueOnce({ ...review, overwritten: [active.name] });
    fireEvent.change(screen.getByLabelText('Cypher'), { target: { value: active.text + '\n' } });
    await completeTest(user);
    await user.click(screen.getByRole('button', { name: 'Review tested file' }));
    expect(await screen.findByLabelText('Review details')).toHaveTextContent(
        'Change: Replaces the active copy of this file.'
    );
});

it('always recalculates: no test mode or reusable source choice exists', async () => {
    const user = userEvent.setup();
    renderEditor();
    expect(screen.queryByRole('combobox', { name: 'Test mode' })).not.toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: 'Reusable source' })).not.toBeInTheDocument();
    await completeTest(user);
    const request = vi.mocked(api.testEditorFile).mock.calls[0][0];
    expect(Object.keys(request).sort()).toEqual(['base_revision', 'name', 'schema_revision', 'text']);
});

it('keeps what the author typed when the page refreshes the workspace', async () => {
    const view = renderEditor();
    const editor = screen.getByLabelText('Cypher') as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: editor.value.replace('RETURN 1', 'RETURN 7') } });
    const refreshed = {
        ...view.props.workspace,
        status: { ...view.props.workspace.status, updated_at: '2026-10-16T13:39:24Z' },
    };
    view.rerender(<AnalysisFileEditor {...view.props} workspace={refreshed} />);
    expect((screen.getByLabelText('Cypher') as HTMLTextAreaElement).value).toContain('RETURN 7');
});
