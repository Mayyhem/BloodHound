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

package registration_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
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

func TestAnalysisEditorWorkingCopyRoutesEnforcePermissions(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{method: "GET", path: "/api/v2/analysis-workspace/working-copies"},
		{method: "PUT", path: "/api/v2/analysis-workspace/working-copies"},
		{method: "DELETE", path: "/api/v2/analysis-workspace/working-copies"},
	} {
		for _, scenario := range []struct {
			name                                string
			authenticated, read, write, enabled bool
		}{
			{name: "anonymous", enabled: true},
			{name: "no permission", authenticated: true, enabled: true},
			{name: "reader", authenticated: true, read: true, enabled: true},
			{name: "writer without read", authenticated: true, write: true, enabled: true},
			{name: "editor", authenticated: true, read: true, write: true, enabled: true},
			{name: "feature disabled", authenticated: true, read: true, write: true},
		} {
			t.Run(route.method+scenario.name, func(t *testing.T) {
				controller := gomock.NewController(t)
				databaseInterface := dbmocks.NewMockDatabase(controller)
				authorizer := auth.NewCustomAuthorizer(databaseInterface, func(auth.Context) (model.Permissions, bool) {
					permissions := model.Permissions{}
					if scenario.read {
						permissions = append(permissions, auth.Permissions().OpenGraphRead)
					}
					if scenario.write {
						permissions = append(permissions, auth.Permissions().OpenGraphWrite)
					}
					return permissions, true
				})
				configuration := config.Configuration{}
				nativeRouter := router.NewRouter(configuration, authorizer, "")
				resources := v2.Resources{DB: databaseInterface, Config: configuration, Authorizer: authorizer}
				request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
				response := httptest.NewRecorder()
				requestContext := &bhctx.Context{}
				databaseInterface.EXPECT().AppendAuditLog(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				databaseInterface.EXPECT().IsEnabled(gomock.Any(), appcfg.FeatureOpenGraphExtensionManagement).Return(scenario.enabled, nil).AnyTimes()
				databaseInterface.EXPECT().GetConfigurationParameter(gomock.Any(), appcfg.TrustedProxiesConfig).Return(appcfg.Parameter{}, errors.New("not configured")).AnyTimes()
				if scenario.authenticated {
					requestContext.AuthCtx.Owner = model.User{Unique: model.Unique{ID: uuid.Must(uuid.NewV4())}}
				}
				request = request.WithContext(requestContext.ConstructGoContext())
				registration.NewV2API(resources, &nativeRouter)
				nativeRouter.Handler().ServeHTTP(response, request)
				expected := 503
				if !scenario.authenticated {
					expected = 401
				} else if !scenario.enabled {
					expected = 404
				} else if !scenario.read || route.method != "GET" && !scenario.write {
					expected = 403
				}
				require.Equal(t, expected, response.Code, response.Body.String())
			})
		}
	}
}
