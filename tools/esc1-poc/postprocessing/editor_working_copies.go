/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package postprocessing

import (
	"context"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

type EditorWorkingCopy = ruleservice.EditorWorkingCopy
type EditorWorkingCopySummary = ruleservice.EditorWorkingCopySummary
type EditorWorkingCopyError = ruleservice.EditorWorkingCopyError
type EditorWorkingCopyDeletion = ruleservice.EditorWorkingCopyDeletion

// ListEditorWorkingCopies accepts the authenticated native user identity. A
// browser request never supplies this value to the rule service directly.
func (s WorkspaceManager) ListEditorWorkingCopies(ctx context.Context, owner string) ([]EditorWorkingCopySummary, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s.client().ListEditorWorkingCopies(ctx, owner)
}

func (s WorkspaceManager) ReadEditorWorkingCopy(ctx context.Context, owner, name string) (EditorWorkingCopy, error) {
	if err := s.validate(); err != nil {
		return EditorWorkingCopy{}, err
	}
	return s.client().ReadEditorWorkingCopy(ctx, owner, name)
}

func (s WorkspaceManager) SaveEditorWorkingCopy(ctx context.Context, owner string, copy EditorWorkingCopy) (EditorWorkingCopy, error) {
	if err := s.validate(); err != nil {
		return EditorWorkingCopy{}, err
	}
	return s.client().SaveEditorWorkingCopy(ctx, owner, copy)
}

func (s WorkspaceManager) DeleteEditorWorkingCopy(ctx context.Context, owner, name, revision string) error {
	if err := s.validate(); err != nil {
		return err
	}
	return s.client().DeleteEditorWorkingCopy(ctx, owner, name, revision)
}

func DecodeEditorWorkingCopy(contents []byte) (EditorWorkingCopy, error) {
	return ruleservice.DecodeEditorWorkingCopy(contents)
}

func DecodeEditorWorkingCopyDeletion(contents []byte) (EditorWorkingCopyDeletion, error) {
	return ruleservice.DecodeEditorWorkingCopyDeletion(contents)
}
