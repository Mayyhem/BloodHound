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

package toolapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/specterops/bloodhound/cmd/api/src/bootstrap"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

func TestExtensionModeBlocksToolGraphMutations(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			var configuration config.Configuration
			if enabled {
				configuration.PostProcessing.URL = "http://extension.invalid"
			}
			var daemon = NewDaemon(context.Background(), bootstrap.DatabaseConnections[database.Database, *graph.DatabaseSwitch]{}, configuration, graph.Schema{}, nil, func(router *chi.Mux) {
				// These probes share the same middleware as native maintenance handlers.
				router.Put("/pg-migration/probe", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusAccepted) })
				router.Put("/graph-db/switch/probe", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusAccepted) })
				router.Put("/unrelated/probe", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusAccepted) })
			})
			for _, path := range []string{"/pg-migration/probe", "/graph-db/switch/probe", "/unrelated/probe"} {
				var response = httptest.NewRecorder()
				daemon.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, path, nil))
				var expected = http.StatusAccepted
				if enabled && path != "/unrelated/probe" {
					expected = http.StatusConflict
				}
				require.Equal(t, expected, response.Code, path)
			}
			if enabled {
				for _, path := range []string{"/graph-db/switch/pg", "/graph-db/switch/neo4j", "/pg-migration/pg-to-neo", "/pg-migration/neo-to-pg", "/pg-migration/cancel"} {
					var response = httptest.NewRecorder()
					daemon.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, path, nil))
					require.Equal(t, http.StatusConflict, response.Code, path)
					require.Contains(t, response.Body.String(), "Disable extension processing")
				}
			}
			for _, path := range []string{"/health", "/pg-migration/status"} {
				var response = httptest.NewRecorder()
				daemon.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, response.Code, path)
			}
		})
	}
}
