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

package ruleservice

import (
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// NewBrowserHandler enables the standalone page at one explicit loopback origin.
// The container listens on its network interface, but its published host port must
// remain bound to loopback. Host and Origin checks protect browser requests; they
// cannot authenticate arbitrary clients on the container network. Production
// deployments should use a BloodHound login proxy instead of this local mode.
// Worker requests to /v1 retain the original bearer authentication.
func NewBrowserHandler(authenticated http.Handler, token, localOrigin string) (http.Handler, error) {
	var (
		originURL *url.URL
		err       error
	)
	if authenticated == nil || len(token) < 32 || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("an authenticated handler and private bearer token are required")
	}
	if localOrigin != "" {
		originURL, err = url.Parse(localOrigin)
		if err != nil || originURL.User != nil || originURL.RawQuery != "" || originURL.ForceQuery || strings.Contains(localOrigin, "#") || originURL.Path != "" || originURL.Opaque != "" || (originURL.Scheme != "http" && originURL.Scheme != "https") || originURL.Host == "" {
			return nil, fmt.Errorf("local UI origin must be an exact HTTP or HTTPS loopback origin without a path")
		}
		var address = net.ParseIP(originURL.Hostname())
		if originURL.Hostname() != "localhost" && (address == nil || !address.IsLoopback()) {
			return nil, fmt.Errorf("local UI origin must use a loopback host")
		}
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.URL.Path == "/v1/browser-mode" {
			if request.Method != http.MethodGet {
				fail(writer, http.StatusMethodNotAllowed, "GET required")
				return
			}
			respond(writer, http.StatusOK, map[string]bool{"local_ui": originURL != nil && request.Host == originURL.Host})
			return
		}
		if !strings.HasPrefix(request.URL.Path, "/ui/") {
			authenticated.ServeHTTP(writer, request)
			return
		}
		if originURL == nil {
			fail(writer, http.StatusNotFound, "local UI is disabled")
			return
		}
		if request.Host != originURL.Host || request.URL.User != nil || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.RawPath != "" {
			fail(writer, http.StatusForbidden, "local UI request does not match its configured origin")
			return
		}
		if len(request.Header.Values("Origin")) > 1 || len(request.Header.Values("Origin")) == 1 && request.Header.Get("Origin") != localOrigin || strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
			fail(writer, http.StatusForbidden, "local UI requires a same-origin request")
			return
		}
		var (
			machinePath string
			write       bool
		)
		switch request.URL.Path {
		case "/ui/plan", "/ui/status":
			if request.Method == http.MethodGet {
				machinePath = "/v1/" + strings.TrimPrefix(request.URL.Path, "/ui/")
			}
		case "/ui/rules":
			if request.Method == http.MethodGet || request.Method == http.MethodPut {
				machinePath = "/v1/rules"
				write = request.Method == http.MethodPut
			}
		case "/ui/draft":
			if request.Method == http.MethodGet || request.Method == http.MethodPut || request.Method == http.MethodDelete {
				machinePath = "/v1/draft"
				write = request.Method != http.MethodGet
			}
		case "/ui/preview":
			if request.Method == http.MethodPost {
				machinePath = "/v1/preview"
				write = true
			}
		case "/ui/analysis":
			if request.Method == http.MethodGet || request.Method == http.MethodPost {
				machinePath = "/v1/analysis"
				write = request.Method == http.MethodPost
			}
		default:
			fail(writer, http.StatusNotFound, "not found")
			return
		}
		if machinePath == "" {
			fail(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if write {
			if request.Header.Get("Origin") != localOrigin {
				fail(writer, http.StatusForbidden, "local UI writes require the configured Origin")
				return
			}
			var mediaType, _, mediaError = mime.ParseMediaType(request.Header.Get("Content-Type"))
			if mediaError != nil || mediaType != "application/json" {
				fail(writer, http.StatusUnsupportedMediaType, "application/json required")
				return
			}
		}
		var forwarded = request.Clone(request.Context())
		forwarded.URL.Path = machinePath
		forwarded.Header.Set("Authorization", "Bearer "+token)
		authenticated.ServeHTTP(writer, forwarded)
	}), nil
}
