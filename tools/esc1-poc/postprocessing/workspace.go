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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

// WorkspaceView is public so BloodHound need not import the tool's private code.
type WorkspaceView = ruleservice.WorkspaceView

var ErrWorkspaceChanged = errors.New("the rules or schemas changed; refresh before downloading")
var ErrWorkspaceQueryMissing = errors.New("the query is not in the displayed revision")

// WorkspaceReader only reads a fixed rule-service address configured by the
// administrator. Browser input can never choose a destination or supply a secret.
type WorkspaceReader struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func (s WorkspaceReader) validate() error {
	var address, err = url.Parse(s.URL)
	if err != nil || address == nil || address.Host == "" || address.User != nil ||
		(address.Scheme != "http" && address.Scheme != "https") ||
		address.RawQuery != "" || address.ForceQuery || address.Fragment != "" ||
		(address.Path != "" && address.Path != "/") || len(strings.TrimSpace(s.Token)) < 32 {
		return fmt.Errorf("analysis workspace connection is not configured")
	}
	return nil
}

func (s WorkspaceReader) Read(ctx context.Context) (WorkspaceView, error) {
	if err := s.validate(); err != nil {
		return WorkspaceView{}, err
	}
	return (ruleservice.Client{BaseURL: s.URL, Token: s.Token, HTTP: s.HTTP}).Workspace(ctx)
}

// Download checks a fresh, consistent copy. It either returns the displayed
// file or asks the author to refresh; it never substitutes a newer query.
func (s WorkspaceReader) Download(ctx context.Context, name, revision, schemaRevision string) (string, error) {
	var view, err = s.Read(ctx)
	if err != nil {
		return "", err
	}
	if revision == "" || revision != view.Plan.Revision || schemaRevision != view.Plan.SchemaRevision {
		return "", ErrWorkspaceChanged
	}
	for _, query := range view.Plan.Queries {
		if query.Name == name {
			return query.Text, nil
		}
	}
	return "", ErrWorkspaceQueryMissing
}
