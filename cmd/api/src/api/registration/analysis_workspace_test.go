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

package registration_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/api/registration"
	"github.com/specterops/bloodhound/cmd/api/src/api/router"
	v2 "github.com/specterops/bloodhound/cmd/api/src/api/v2"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bhctx"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	dbmocks "github.com/specterops/bloodhound/cmd/api/src/database/mocks"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/model/appcfg"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Use the real route registration and permission checks with no graph service.
// An allowed reader reaches the unconfigured response; denied readers never do.
func TestAnalysisWorkspaceRoutesEnforceReadPermission(t *testing.T) {
	for _, route := range []string{
		"/api/v2/analysis-workspace",
		"/api/v2/analysis-workspace/query?query=PoC_A/users.cypher&revision=" + strings.Repeat("a", 64) + "&schema_revision=" + strings.Repeat("b", 64),
	} {
		for _, scenario := range []struct {
			name                            string
			authenticated, allowed, enabled bool
			method                          string
			expected                        int
		}{
			{name: "anonymous", enabled: true, method: "GET", expected: 401},
			{name: "no permission", authenticated: true, enabled: true, method: "GET", expected: 403},
			{name: "reader", authenticated: true, allowed: true, enabled: true, method: "GET", expected: 503},
			{name: "feature disabled", authenticated: true, allowed: true, method: "GET", expected: 404},
			{name: "write refused", authenticated: true, allowed: true, enabled: true, method: "POST", expected: 405},
		} {
			t.Run(route+"/"+scenario.name, func(t *testing.T) {
				var (
					controller = gomock.NewController(t)
					database   = dbmocks.NewMockDatabase(controller)
					authorizer = auth.NewCustomAuthorizer(database, func(auth.Context) (model.Permissions, bool) {
						if scenario.allowed {
							return model.Permissions{auth.Permissions().OpenGraphRead}, true
						}
						return nil, true
					})
					configuration  = config.Configuration{}
					nativeRouter   = router.NewRouter(configuration, authorizer, "")
					resources      = v2.Resources{DB: database, Config: configuration, Authorizer: authorizer}
					request        = httptest.NewRequest(scenario.method, route, nil)
					response       = httptest.NewRecorder()
					requestContext = &bhctx.Context{}
				)
				database.EXPECT().IsEnabled(gomock.Any(), appcfg.FeatureOpenGraphExtensionManagement).Return(scenario.enabled, nil).AnyTimes()
				database.EXPECT().GetConfigurationParameter(gomock.Any(), appcfg.TrustedProxiesConfig).Return(appcfg.Parameter{}, errors.New("not configured")).AnyTimes()
				if scenario.authenticated {
					requestContext.AuthCtx.Owner = model.User{}
				}
				request = request.WithContext(requestContext.ConstructGoContext())
				registration.NewV2API(resources, &nativeRouter)
				nativeRouter.Handler().ServeHTTP(response, request)
				require.Equal(t, scenario.expected, response.Code, response.Body.String())
				require.NotContains(t, response.Body.String(), "Bearer")
			})
		}
	}
}

func TestAnalysisManagementRoutesEnforceReadAndWritePermissions(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v2/analysis-workspace/draft"}, {"PUT", "/api/v2/analysis-workspace/draft"},
		{"DELETE", "/api/v2/analysis-workspace/draft"}, {"POST", "/api/v2/analysis-workspace/preview"},
		{"PUT", "/api/v2/analysis-workspace/rules"},
	} {
		for _, scenario := range []struct {
			name                                string
			authenticated, read, write, enabled bool
		}{
			{name: "anonymous", enabled: true}, {name: "no permission", authenticated: true, enabled: true},
			{name: "reader", authenticated: true, read: true, enabled: true},
			{name: "writer without read", authenticated: true, write: true, enabled: true},
			{name: "editor", authenticated: true, read: true, write: true, enabled: true},
			{name: "feature disabled", authenticated: true, read: true, write: true},
		} {
			t.Run(route.method+route.path+scenario.name, func(t *testing.T) {
				var (
					controller        = gomock.NewController(t)
					databaseInterface = dbmocks.NewMockDatabase(controller)
					authorizer        = auth.NewCustomAuthorizer(databaseInterface, func(auth.Context) (model.Permissions, bool) {
						result := model.Permissions{}
						if scenario.read {
							result = append(result, auth.Permissions().OpenGraphRead)
						}
						if scenario.write {
							result = append(result, auth.Permissions().OpenGraphWrite)
						}
						return result, true
					})
					configuration  = config.Configuration{}
					nativeRouter   = router.NewRouter(configuration, authorizer, "")
					resources      = v2.Resources{DB: databaseInterface, Config: configuration, Authorizer: authorizer}
					request        = httptest.NewRequest(route.method, route.path, strings.NewReader("{}"))
					response       = httptest.NewRecorder()
					requestContext = &bhctx.Context{}
					expected       = 503
				)
				databaseInterface.EXPECT().AppendAuditLog(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				databaseInterface.EXPECT().IsEnabled(gomock.Any(), appcfg.FeatureOpenGraphExtensionManagement).Return(scenario.enabled, nil).AnyTimes()
				databaseInterface.EXPECT().GetConfigurationParameter(gomock.Any(), appcfg.TrustedProxiesConfig).Return(appcfg.Parameter{}, errors.New("not configured")).AnyTimes()
				if scenario.authenticated {
					requestContext.AuthCtx.Owner = model.User{}
				}
				if !scenario.authenticated {
					expected = 401
				} else if !scenario.enabled {
					expected = 404
				} else if !scenario.read || route.method != "GET" && !scenario.write {
					expected = 403
				}
				request = request.WithContext(requestContext.ConstructGoContext())
				registration.NewV2API(resources, &nativeRouter)
				nativeRouter.Handler().ServeHTTP(response, request)
				require.Equal(t, expected, response.Code, response.Body.String())
			})
		}
	}
}
