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

import { editorDependencyProblem, readEditorDependencies, setEditorDependency, setEditorHeader } from './editorModel';

describe('editor declaration model', () => {
    it('reads local query aliases and conditions only from leading comments', () => {
        const text =
            '/* license */\n// namespace: PoC_AD\n// depends-on: query:users>=v1.0.0 as directory_users\nRETURN "// depends-on: query:late"';
        expect(readEditorDependencies(text, 'PoC_AD')).toEqual([
            { name: 'PoC_AD/users.cypher', alias: 'directory_users', line: 2, conditions: '>=v1.0.0' },
        ]);
    });
    it('inserts before Cypher, replaces its exact declaration, and preserves conditions', () => {
        const text = '// version: v1.0.0\nRETURN 1';
        const added = setEditorDependency(text, 'PoC_AD', 'users.cypher', 'users', true);
        expect(added.indexOf('depends-on')).toBeLessThan(added.indexOf('RETURN'));
        expect(
            setEditorDependency(
                '// depends-on: query:PoC_AD/users>=v1.0.0 as old\nRETURN 1',
                'PoC_AD',
                'PoC_AD/users.cypher',
                'new',
                true
            )
        ).toContain('>=v1.0.0 as new');
    });
    it('removes none only when adding and detects duplicate names and aliases', () => {
        expect(setEditorDependency('// depends-on: none\nRETURN 1', 'PoC_AD', 'users', 'users', true)).not.toContain(
            'none'
        );
        // Removing the last prerequisite leaves no line at all; that means no dependencies.
        expect(
            setEditorDependency('// depends-on: query:users as users\nRETURN 1', 'PoC_AD', 'users', 'users', false)
        ).toBe('RETURN 1');
        const dependencies = readEditorDependencies(
            '// depends-on: query:users as same\n// depends-on: query:groups as same\nRETURN 1',
            'PoC_AD'
        );
        expect(editorDependencyProblem(dependencies)).toBe('Each query must use a different alias.');
    });
    it('changes only leading headers', () => {
        expect(setEditorHeader('// namespace: old\nRETURN "// namespace: body"', 'namespace', 'new')).toBe(
            '// namespace: new\nRETURN "// namespace: body"'
        );
    });
});
