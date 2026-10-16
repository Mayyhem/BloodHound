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

import { deflateRawSync } from 'node:zlib';
import { readUploads } from './uploads';

function file(name: string, text: string) {
    const result = new File([text], name);
    Object.defineProperty(result, 'text', { value: async () => text });
    return result;
}

// Build a small .zip in memory. Each entry is stored either as-is or with the
// standard .zip compression, which is what common zip tools produce.
function zipFile(name: string, entries: { path: string; text: string; compress?: boolean }[]) {
    const parts: Uint8Array[] = [];
    const directory: Uint8Array[] = [];
    let offset = 0;
    for (const entry of entries) {
        const path = new TextEncoder().encode(entry.path);
        const original = new TextEncoder().encode(entry.text);
        const packed = entry.compress ? new Uint8Array(deflateRawSync(original)) : original;
        const local = new DataView(new ArrayBuffer(30));
        local.setUint32(0, 0x04034b50, true);
        local.setUint16(8, entry.compress ? 8 : 0, true);
        local.setUint32(18, packed.length, true);
        local.setUint32(22, original.length, true);
        local.setUint16(26, path.length, true);
        const central = new DataView(new ArrayBuffer(46));
        central.setUint32(0, 0x02014b50, true);
        central.setUint16(10, entry.compress ? 8 : 0, true);
        central.setUint32(20, packed.length, true);
        central.setUint32(24, original.length, true);
        central.setUint16(28, path.length, true);
        central.setUint32(42, offset, true);
        parts.push(new Uint8Array(local.buffer), path, packed);
        directory.push(new Uint8Array(central.buffer), path);
        offset += 30 + path.length + packed.length;
    }
    const directorySize = directory.reduce((total, part) => total + part.length, 0);
    const end = new DataView(new ArrayBuffer(22));
    end.setUint32(0, 0x06054b50, true);
    end.setUint16(10, entries.length, true);
    end.setUint32(12, directorySize, true);
    end.setUint32(16, offset, true);
    const bytes = new Uint8Array(offset + directorySize + 22);
    let position = 0;
    for (const part of [...parts, ...directory, new Uint8Array(end.buffer)]) {
        bytes.set(part, position);
        position += part.length;
    }
    const result = new File([bytes], name);
    Object.defineProperty(result, 'arrayBuffer', { value: async () => bytes.buffer });
    return result;
}

it('reads .cypher files inside a .zip alongside directly selected files', async () => {
    const zip = zipFile('rules.zip', [
        { path: 'rules/users.cypher', text: '// namespace: PoC_A\n// version: v1.0.0\nRETURN 1', compress: true },
        { path: 'rules/groups.cypher', text: '// namespace: PoC_A\n// version: v1.0.0\nRETURN 2' },
        { path: 'rules/README.md', text: 'Not an analysis file' },
        { path: '__MACOSX/rules/._users.cypher', text: 'macOS extra data' },
    ]);
    const direct = file('edges.cypher', '// namespace: PoC_B\n// version: v1.0.0\nRETURN 3');
    expect(await readUploads([zip, direct])).toEqual({
        'PoC_A/users.cypher': '// namespace: PoC_A\n// version: v1.0.0\nRETURN 1',
        'PoC_A/groups.cypher': '// namespace: PoC_A\n// version: v1.0.0\nRETURN 2',
        'PoC_B/edges.cypher': '// namespace: PoC_B\n// version: v1.0.0\nRETURN 3',
    });
});

it('rejects unreadable, empty, and oversized .zip contents', async () => {
    const broken = new File(['not a zip'], 'broken.zip');
    Object.defineProperty(broken, 'arrayBuffer', { value: async () => new TextEncoder().encode('not a zip').buffer });
    await expect(readUploads([broken])).rejects.toThrow('broken.zip is not a readable .zip file.');
    await expect(readUploads([zipFile('empty.zip', [{ path: 'README.md', text: 'none' }])])).rejects.toThrow(
        'Select between 1 and 32'
    );
    await expect(
        readUploads([zipFile('large.zip', [{ path: 'users.cypher', text: 'x'.repeat(1048577), compress: true }])])
    ).rejects.toThrow('1 MiB');
});

it('keeps same-named files in different namespaces distinct and rejects duplicate identities', async () => {
    const first = file('users.cypher', '// namespace: PoC_A\n// version: v1.0.0\nRETURN 1');
    const second = file('users.cypher', '// namespace: PoC_B\n// version: v1.0.0\nRETURN 1');
    expect(Object.keys(await readUploads([first, second]))).toEqual(['PoC_A/users.cypher', 'PoC_B/users.cypher']);
    await expect(readUploads([first, first])).rejects.toThrow('same namespace and filename');
});
it('rejects misleading namespace text, wrong file types, and excessive uploads before a request', async () => {
    await expect(readUploads([file('users.cypher', "RETURN '// namespace: PoC_A'")])).rejects.toThrow(
        'namespace comment'
    );
    await expect(readUploads([file('schema.json', '{}')])).rejects.toThrow('snake_case.cypher');
    await expect(readUploads(Array.from({ length: 33 }, () => file('users.cypher', '')))).rejects.toThrow(
        '32 analysis files'
    );
    await expect(readUploads([file('users.cypher', 'x'.repeat(1048577))])).rejects.toThrow('1 MiB');
});
