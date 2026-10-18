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

export interface EditorDependency {
    name: string;
    alias: string;
    line: number;
    conditions: string;
}

function leadingLineNumbers(text: string) {
    const lines = text.split('\n');
    const result: number[] = [];
    let inBlock = false;
    for (let index = 0; index < lines.length; index++) {
        let line = lines[index].trim();
        if (inBlock) {
            const end = line.indexOf('*/');
            if (end < 0) continue;
            inBlock = false;
            line = line.slice(end + 2).trim();
        }
        if (line.startsWith('/*')) {
            const end = line.indexOf('*/', 2);
            if (end < 0) {
                inBlock = true;
                continue;
            }
            line = line.slice(end + 2).trim();
        }
        if (!line) continue;
        if (!line.startsWith('//')) break;
        result.push(index);
    }
    return result;
}

function normalizedName(namespace: string, reference: string) {
    const name = reference.trim().replace(/\.cypher$/, '');
    return `${name.includes('/') ? name : `${namespace}/${name}`}.cypher`;
}

export function readEditorDependencies(text: string, namespace: string): EditorDependency[] {
    const lines = text.split('\n');
    const result: EditorDependency[] = [];
    for (const lineNumber of leadingLineNumbers(text)) {
        const match = lines[lineNumber].trim().match(/^\/\/\s*depends-on:\s*query:([^\s<>=!~]+)(.*)$/);
        if (!match) continue;
        const remainder = match[2].trim();
        const aliasMatch = remainder.match(/(?:^|\s)as(?:\s+(.*))?$/);
        const conditions = (aliasMatch ? remainder.slice(0, aliasMatch.index) : remainder).trim();
        const shortName =
            match[1]
                .replace(/\.cypher$/, '')
                .split('/')
                .pop() || '';
        result.push({
            name: normalizedName(namespace, match[1]),
            alias: aliasMatch ? (aliasMatch[1] || '').trim() : shortName,
            line: lineNumber,
            conditions,
        });
    }
    return result;
}

export function editorDependencyProblem(dependencies: EditorDependency[]) {
    const names = new Set<string>();
    const aliases = new Set<string>();
    for (const dependency of dependencies) {
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(dependency.alias))
            return 'A query alias must be a valid Cypher parameter name.';
        if (names.has(dependency.name)) return 'A query can be declared only once.';
        if (aliases.has(dependency.alias)) return 'Each query must use a different alias.';
        names.add(dependency.name);
        aliases.add(dependency.alias);
    }
    return undefined;
}

export function setEditorDependency(
    text: string,
    namespace: string,
    fullQueryName: string,
    alias: string,
    selected: boolean
) {
    const lines = text.split('\n');
    const name = normalizedName(namespace, fullQueryName);
    const existing = readEditorDependencies(text, namespace).find((dependency) => dependency.name === name);
    if (existing) {
        // Removing the last prerequisite leaves no // depends-on line, which
        // means the file has no dependencies.
        if (!selected) lines.splice(existing.line, 1);
        else
            lines[existing.line] =
                `// depends-on: query:${name.replace(/\.cypher$/, '')}${existing.conditions ? ` ${existing.conditions}` : ''} as ${alias}`;
        return lines.join('\n');
    }
    if (!selected) return text;
    const leading = leadingLineNumbers(text);
    const firstStatement = leading.length ? leading[leading.length - 1] + 1 : 0;
    const none = leading.find((line) => /^\/\/\s*depends-on:\s*none\s*$/.test(lines[line]));
    if (none !== undefined) lines.splice(none, 1);
    const position = none === undefined ? firstStatement : none;
    lines.splice(position, 0, `// depends-on: query:${name.replace(/\.cypher$/, '')} as ${alias}`);
    return lines.join('\n');
}

export function setEditorHeader(text: string, key: 'namespace' | 'version', value: string) {
    const lines = text.split('\n');
    const existing = leadingLineNumbers(text).find((line) => new RegExp(`^\\s*//\\s*${key}:`).test(lines[line]));
    if (existing !== undefined) lines[existing] = `// ${key}: ${value}`;
    else {
        const leading = leadingLineNumbers(text);
        lines.splice(leading.length ? leading[leading.length - 1] + 1 : 0, 0, `// ${key}: ${value}`);
    }
    return lines.join('\n');
}

// Which typed keys may open the editor's suggestion list for a cursor at
// `position` (counted in characters from the start of `text`).
//
// The suggestion list is only useful where the editor can tell what kind of
// word belongs at the cursor. Tested against the editor itself, those places
// are:
//   $ anywhere                         -> the file's own $parameters
//   : straight after ( or [, or after  -> node or relationship types
//     ( or [ and a name: (n:  (:  -[r:
//   : after a name straight after      -> node types (a label check)
//     WHERE: WHERE n:
// Everywhere else (after . or {, a map key such as {id:, : after a space,
// or a label check after AND, OR or NOT, which the editor does not recognize)
// the editor falls back to listing every variable, parameter, built-in
// property name and keyword, which is noise. There the keys don't open the
// list; Ctrl+Space still opens it anywhere.
//
// Inside a // comment, such as a `// depends-on:` line, nothing opens it.
export function suggestionKeysAt(text: string, position: number): string[] {
    const before = text.slice(0, position);
    const line = before.slice(before.lastIndexOf('\n') + 1);
    // A // inside a quoted string also counts as a comment; Cypher rarely has
    // one, and the only effect is no automatic suggestions on that line.
    if (line.includes('//')) return [];
    const typesFollow = /[([]\s*([A-Za-z_]\w*)?\s*$/.test(line) || /\bWHERE\s+[A-Za-z_]\w*$/i.test(line);
    return typesFollow ? ['$', ':'] : ['$'];
}
