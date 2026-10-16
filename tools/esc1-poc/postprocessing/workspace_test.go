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

package postprocessing_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
	"github.com/stretchr/testify/require"
)

type workspaceTransport func(*http.Request) (*http.Response, error)

func (s workspaceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return s(request)
}

func TestWorkspaceReaderDownloadsOnlyTheDisplayedRevision(t *testing.T) {
	var (
		revision       = strings.Repeat("a", 64)
		schemaRevision = strings.Repeat("b", 64)
		original       = "// original author comment\nRETURN 1"
		view           = ruleservice.WorkspaceView{
			Plan:   ruleservice.PlanView{Revision: revision, SchemaRevision: schemaRevision, Queries: []ruleservice.QueryView{{Name: "PoC_A/users.cypher", Text: original}, {Name: "PoC_B/users.cypher", Text: "RETURN 2"}}},
			Status: ruleservice.Status{State: "unavailable"}, ObservedAt: time.Now(),
		}
		reader = postprocessing.WorkspaceReader{URL: "http://rules", Token: strings.Repeat("t", 32)}
	)
	reader.HTTP = &http.Client{Transport: workspaceTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/v1/workspace", request.URL.RequestURI())
		require.Equal(t, "Bearer "+reader.Token, request.Header.Get("Authorization"))
		contents, err := json.Marshal(view)
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(contents)))}, nil
	})}
	contents, err := reader.Download(context.Background(), "PoC_A/users.cypher", revision, schemaRevision)
	require.NoError(t, err)
	require.Equal(t, original, contents)
	contents, err = reader.Download(context.Background(), "PoC_B/users.cypher", revision, schemaRevision)
	require.NoError(t, err)
	require.Equal(t, "RETURN 2", contents)
	_, err = reader.Download(context.Background(), "PoC_A/users.cypher", strings.Repeat("c", 64), schemaRevision)
	require.ErrorIs(t, err, postprocessing.ErrWorkspaceChanged)
	_, err = reader.Download(context.Background(), "PoC_A/users.cypher", revision, strings.Repeat("c", 64))
	require.ErrorIs(t, err, postprocessing.ErrWorkspaceChanged)
	_, err = reader.Download(context.Background(), "PoC_A/missing.cypher", revision, schemaRevision)
	require.ErrorIs(t, err, postprocessing.ErrWorkspaceQueryMissing)
}

func TestWorkspaceReaderRejectsInvalidAndUnsafeResponses(t *testing.T) {
	for _, response := range []struct {
		name           string
		status         int
		body           string
		transportError error
	}{
		{name: "redirect", status: 302, body: "private error"},
		{name: "unauthorized", status: 401, body: "private error"},
		{name: "invalid JSON", status: 200, body: "private error"},
		{name: "missing observation", status: 200, body: `{"status":{"state":"success"}}`},
		{name: "oversized", status: 200, body: strings.Repeat("x", ruleservice.MaxWorkspaceBytes+1)},
		{name: "connection failure", transportError: errors.New("secret connection text")},
	} {
		t.Run(response.name, func(t *testing.T) {
			var reader = postprocessing.WorkspaceReader{URL: "http://rules", Token: strings.Repeat("t", 32), HTTP: &http.Client{Transport: workspaceTransport(func(request *http.Request) (*http.Response, error) {
				if response.transportError != nil {
					return nil, response.transportError
				}
				return &http.Response{StatusCode: response.status, Header: http.Header{"Location": []string{"http://elsewhere"}}, Body: io.NopCloser(strings.NewReader(response.body))}, nil
			})}}
			_, err := reader.Read(context.Background())
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "secret")
		})
	}
	for _, address := range []string{"http://user:pass@rules", "http://rules/path", "http://rules?next=x", "file:///rules", "http://rules#fragment"} {
		_, err := (postprocessing.WorkspaceReader{URL: address, Token: strings.Repeat("t", 32)}).Read(context.Background())
		require.Error(t, err)
		require.NotContains(t, err.Error(), address)
	}
}
