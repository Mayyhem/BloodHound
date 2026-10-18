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

import { fireEvent, render, screen } from 'src/test-utils';
import CypherText from './CypherText';

// The real editor needs a browser canvas. This stand-in has a typing area and,
// when its text contains "open", the suggestion list the real editor shows.
vi.mock('@neo4j-cypher/react-codemirror', () => ({
    CypherEditor: ({
        value,
        autofocus,
        autofocusProps,
    }: {
        value: string;
        autofocus?: boolean;
        autofocusProps?: string[];
    }) => (
        <div data-autofocus={String(autofocus)} data-autofocus-props={JSON.stringify(autofocusProps)}>
            <input aria-label='typing area' defaultValue={value} />
            {value.includes('open') && <ul className='cm-tooltip-autocomplete' />}
        </div>
    ),
}));

describe('CypherText', () => {
    it('does not take keyboard focus when its text changes', () => {
        // The editor's own default is to take focus on every text change, which
        // pulled focus away from the Prerequisites dropdown.
        render(<CypherText label='Cypher' value='RETURN 1' onChange={() => {}} />);
        expect(screen.getByLabelText('typing area').parentElement).toHaveAttribute('data-autofocus', 'false');
        expect(screen.getByLabelText('typing area').parentElement).toHaveAttribute('data-autofocus-props', '[]');
    });

    it('keeps Escape inside while the suggestion list is open, and passes it on otherwise', () => {
        const windowKeys = vi.fn();
        const { rerender } = render(
            <div onKeyDown={(event) => windowKeys(event.key)}>
                <CypherText label='Cypher' value='MATCH (n:open' onChange={() => {}} />
            </div>
        );
        // The list is open: Escape closes only the list, so the window around
        // the box never hears it.
        fireEvent.keyDown(screen.getByLabelText('typing area'), { key: 'Escape' });
        expect(windowKeys).not.toHaveBeenCalled();
        // Other keys still pass through.
        fireEvent.keyDown(screen.getByLabelText('typing area'), { key: 'a' });
        expect(windowKeys).toHaveBeenCalledWith('a');

        // No list: Escape reaches the window, which can close as before.
        rerender(
            <div onKeyDown={(event) => windowKeys(event.key)}>
                <CypherText label='Cypher' value='MATCH (n)' onChange={() => {}} />
            </div>
        );
        fireEvent.keyDown(screen.getByLabelText('typing area'), { key: 'Escape' });
        expect(windowKeys).toHaveBeenCalledWith('Escape');
    });
});
