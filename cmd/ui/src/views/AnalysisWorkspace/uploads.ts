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

const maxFiles = 32;
const maxFileBytes = 1048576;

interface UploadFile {
    name: string;
    size: number;
    text: () => Promise<string>;
}

// Read the browser's files before saving. A .zip file is opened here and each
// .cypher file inside it is treated as if it had been selected directly. The
// service independently checks every declaration, file name, and limit; this
// early check gives useful upload feedback.
export async function readUploads(files: File[]): Promise<Record<string, string>> {
    const cypherFiles: UploadFile[] = [];
    for (const file of files) {
        if (file.name.toLowerCase().endsWith('.zip')) cypherFiles.push(...(await readZip(file)));
        else cypherFiles.push(file);
    }
    if (!cypherFiles.length || cypherFiles.length > maxFiles)
        throw Error('Select between 1 and 32 analysis files, including files inside .zip files.');
    const result: Record<string, string> = {};
    for (const file of cypherFiles) {
        if (!/^[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.cypher$/.test(file.name))
            throw Error(
                'Upload snake_case.cypher files or a .zip of them. Upload schemas through OpenGraph Management.'
            );
        if (file.size > maxFileBytes) throw Error('Each file must be at most 1 MiB.');
        const contents = await file.text();
        const leading = contents.match(/^(?:\s+|\/\/[^\n]*(?:\n|$)|\/\*[\s\S]*?\*\/)*/)?.[0] ?? '';
        const declarations = [...leading.matchAll(/^\s*\/\/\s*namespace:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$/gm)];
        if (declarations.length !== 1) throw Error('Each file needs one namespace comment before its Cypher.');
        const name = `${declarations[0][1]}/${file.name}`;
        if (Object.hasOwn(result, name)) throw Error('Two uploaded files have the same namespace and filename.');
        result[name] = contents;
    }
    if (new TextEncoder().encode(JSON.stringify(result)).length > 4 * maxFileBytes)
        throw Error('Upload exceeds 4 MiB.');
    return result;
}

// A .zip file ends with a table of contents listing every file inside it. Read
// that list, then unpack only the .cypher files. Folder names inside the .zip
// are ignored, so "rules/users.cypher" is read as "users.cypher". Other files,
// such as a README or the extra files macOS adds, are skipped.
async function readZip(zip: File): Promise<UploadFile[]> {
    const bytes = new Uint8Array(await zip.arrayBuffer());
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const unreadable = Error(`${zip.name} is not a readable .zip file.`);

    // The table of contents is announced by a short record at the very end.
    let end = -1;
    for (let position = bytes.length - 22; position >= Math.max(0, bytes.length - 65557); position--) {
        if (view.getUint32(position, true) === 0x06054b50) {
            end = position;
            break;
        }
    }
    if (end < 0) throw unreadable;
    const entryCount = view.getUint16(end + 10, true);
    let entry = view.getUint32(end + 16, true);

    const result: UploadFile[] = [];
    for (let index = 0; index < entryCount; index++) {
        if (entry + 46 > bytes.length || view.getUint32(entry, true) !== 0x02014b50) throw unreadable;
        const flags = view.getUint16(entry + 8, true);
        const method = view.getUint16(entry + 10, true);
        const packedSize = view.getUint32(entry + 20, true);
        const size = view.getUint32(entry + 24, true);
        const nameLength = view.getUint16(entry + 28, true);
        const extraLength = view.getUint16(entry + 30, true);
        const commentLength = view.getUint16(entry + 32, true);
        const fileStart = view.getUint32(entry + 42, true);
        const path = new TextDecoder().decode(bytes.subarray(entry + 46, entry + 46 + nameLength));
        entry += 46 + nameLength + extraLength + commentLength;

        const name = path.split('/').pop() ?? '';
        if (!name.endsWith('.cypher') || path.startsWith('__MACOSX/') || name.startsWith('._')) continue;
        if (flags & 1) throw Error(`${zip.name} is password protected. Upload an unprotected .zip file.`);
        // Check the size the .zip reports before unpacking, so an oversized
        // file is rejected without filling the browser's memory.
        if (size > maxFileBytes) throw Error('Each file must be at most 1 MiB.');
        if (method !== 0 && method !== 8) throw Error(`${zip.name} uses an unsupported compression type.`);

        // Each file has its own short header before its packed contents.
        if (fileStart + 30 > bytes.length || view.getUint32(fileStart, true) !== 0x04034b50) throw unreadable;
        const dataStart = fileStart + 30 + view.getUint16(fileStart + 26, true) + view.getUint16(fileStart + 28, true);
        const packed = bytes.subarray(dataStart, dataStart + packedSize);
        // Method 0 stores the file as-is. Method 8 is the standard .zip
        // compression, which every supported browser can unpack natively.
        const contents = method === 0 ? packed : await unpack(packed);
        if (contents.length !== size) throw unreadable;
        const text = new TextDecoder().decode(contents);
        result.push({ name, size, text: async () => text });
    }
    return result;
}

async function unpack(packed: Uint8Array<ArrayBuffer>) {
    const unpacked = new Response(packed).body!.pipeThrough(new DecompressionStream('deflate-raw'));
    return new Uint8Array(await new Response(unpacked).arrayBuffer());
}
