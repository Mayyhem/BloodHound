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

type WorkspaceChange = ruleservice.Change
type WorkspaceDraft = ruleservice.DraftView
type WorkspacePreview = ruleservice.Preview
type WorkspaceManagementError = ruleservice.ManagementError

const MaxWorkspaceChangeBytes = ruleservice.MaxBundleBytes

// Management uses the same fixed destination and connection checks as reads.
// Each method has a specific purpose; browser input never chooses a service URL.
type WorkspaceManager struct{ WorkspaceReader }

func (s WorkspaceManager) client() ruleservice.Client {
	return ruleservice.Client{BaseURL: s.URL, Token: s.Token, HTTP: s.HTTP}
}
func (s WorkspaceManager) Draft(ctx context.Context) (WorkspaceDraft, error) {
	if err := s.validate(); err != nil {
		return WorkspaceDraft{}, err
	}
	return s.client().ReadDraft(ctx)
}
func (s WorkspaceManager) SaveDraft(ctx context.Context, change WorkspaceChange) (WorkspaceDraft, error) {
	if err := s.validate(); err != nil {
		return WorkspaceDraft{}, err
	}
	return s.client().SaveDraft(ctx, change)
}
func (s WorkspaceManager) DiscardDraft(ctx context.Context, change WorkspaceChange) error {
	if err := s.validate(); err != nil {
		return err
	}
	return s.client().DiscardDraft(ctx, change)
}
func (s WorkspaceManager) Preview(ctx context.Context, change WorkspaceChange) (WorkspacePreview, error) {
	if err := s.validate(); err != nil {
		return WorkspacePreview{}, err
	}
	return s.client().Preview(ctx, change)
}
func (s WorkspaceManager) Apply(ctx context.Context, change WorkspaceChange) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	return s.client().Apply(ctx, change)
}

func DecodeWorkspaceChange(contents []byte) (WorkspaceChange, error) {
	return ruleservice.DecodeChange(contents)
}
