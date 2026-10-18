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

import { Box } from '@mui/material';
import { CypherEditor } from '@neo4j-cypher/react-codemirror';
import { ComponentProps, KeyboardEvent, useRef, useState } from 'react';
import { useAppSelector } from 'src/store';
import { suggestionKeysAt } from './editorModel';

// The words the editor can suggest while typing: node and relationship types,
// property names, and $parameter names (written without the $).
export type CypherSuggestions = NonNullable<ComponentProps<typeof CypherEditor>['schema']>;

interface Props {
    // The accessible name, read out by screen readers and used by tests.
    label: string;
    value: string;
    // Called with the new text after every edit. Leave it out for read-only text.
    onChange?: (text: string) => void;
    readOnly?: boolean;
    // true: fill the height of the surrounding column and scroll inside when
    // the text is longer (the details panel's Cypher tab). false: grow with
    // the text, starting at about 16 lines (the editor).
    fill?: boolean;
    // Extra layout classes for the outer box, such as the text size.
    className?: string;
    // When given, the editor suggests these words while typing, as Explore's
    // Cypher search does. Without it, nothing is suggested.
    suggestions?: CypherSuggestions;
}

// CypherText shows a Cypher file with syntax highlighting and line numbers,
// using the same editor component as BloodHound's Explore Cypher search.
//
// Its error underlining (lint) is off. That checker uses an older Cypher
// grammar than Neo4j and DAWGS: it underlines valid, working analysis files,
// for example `all(domain IN nodes(route) WHERE domain:Domain)`. The editor's
// Test button gives the real check. Typing suggestions are on only when
// `suggestions` is given.
export default function CypherText({
    label,
    value,
    onChange,
    readOnly = false,
    fill = false,
    className = '',
    suggestions,
}: Props) {
    const darkMode = useAppSelector((state) => state.global.view.darkMode);
    // Escape while the suggestion list is open should close only the list.
    // Without this, the same key press also reaches the editor window around
    // this box, which closes the whole window and loses unsaved work. The list
    // is checked before the editor handles the key, because the editor
    // removes the list at once.
    const listWasOpen = useRef(false);
    const noteOpenList = (event: KeyboardEvent<HTMLElement>) => {
        listWasOpen.current = event.key === 'Escape' && !!event.currentTarget.querySelector('.cm-tooltip-autocomplete');
    };
    const keepEscapeInside = (event: KeyboardEvent<HTMLElement>) => {
        if (listWasOpen.current) event.stopPropagation();
        listWasOpen.current = false;
    };
    // Which typed keys may open the suggestion list right now: only where the
    // editor gives a specific list, and never in a // comment
    // (suggestionKeysAt in editorModel.ts explains the rule). The text is
    // tracked here because the page's copy can lag one key press behind.
    const latestText = useRef(value);
    latestText.current = value;
    const [suggestionKeys, setSuggestionKeys] = useState<string[]>(['$']);
    return (
        <Box
            aria-label={label}
            onKeyDownCapture={noteOpenList}
            onKeyDown={keepEscapeInside}
            className={`flex flex-col rounded border overflow-hidden ${fill ? 'flex-1 min-h-[200px]' : ''} ${className}`}>
            <CypherEditor
                className={
                    fill
                        ? 'flex flex-col flex-1 min-h-0 [&_.cm-editor]:flex-1 [&_.cm-editor]:min-h-0'
                        : '[&_.cm-scroller]:min-h-[16rem]'
                }
                value={value}
                onValueChanged={(text: string) => {
                    latestText.current = text;
                    onChange?.(text);
                }}
                onPositionChanged={({ position }: { position: number }) =>
                    setSuggestionKeys(suggestionKeysAt(latestText.current, position))
                }
                autocompleteTriggerStrings={suggestionKeys}
                readOnly={readOnly}
                // The editor normally takes keyboard focus when it first appears
                // and again whenever its text changes (its `autofocusProps`
                // list includes the text). Choosing a prerequisite changes the
                // text, so focus jumped here from the dropdown and a following
                // Escape closed the whole editor window. Focus moves only when
                // the box is clicked or reached with Tab.
                autofocus={false}
                autofocusProps={[]}
                theme={darkMode ? 'dark' : 'light'}
                lint={false}
                autocomplete={!!suggestions}
                schema={suggestions}
                lineNumbers
                tooltipAbsolute={false}
            />
        </Box>
    );
}
