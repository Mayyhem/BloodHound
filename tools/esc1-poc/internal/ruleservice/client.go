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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL, Token string
	HTTP           *http.Client
}

func (s Client) request(ctx context.Context, method, path string, contents []byte) (*http.Response, error) {
	parsed, err := url.Parse(s.BaseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("rule service URL must be an HTTP or HTTPS address without credentials, query, or fragment")
	}
	if strings.TrimSpace(s.Token) == "" {
		return nil, fmt.Errorf("rule service token is required")
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.BaseURL, "/")+path, bytes.NewReader(contents))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+s.Token)
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 30 * time.Second}
	if s.HTTP != nil {
		client = *s.HTTP
		if client.Timeout <= 0 || client.Timeout > 30*time.Second {
			client.Timeout = 30 * time.Second
		}
	}
	if client.Transport == nil {
		defaultTransport, valid := http.DefaultTransport.(*http.Transport)
		if !valid {
			return nil, fmt.Errorf("default HTTP transport must support disabling proxies")
		}
		transport := defaultTransport.Clone()
		// Docker and local addresses must not send the token through HTTP_PROXY.
		transport.Proxy = nil
		client.Transport = transport
	}
	// Never forward authentication to a redirect destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(request)
}

func (s Client) Fetch(ctx context.Context) (Bundle, error) {
	response, err := s.request(ctx, http.MethodGet, "/v1/rules", nil)
	if err != nil {
		return Bundle{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Bundle{}, ErrNoActive
	}
	if response.StatusCode != http.StatusOK {
		return Bundle{}, fmt.Errorf("rule service returned HTTP %d", response.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, MaxBundleBytes+1))
	if err != nil {
		return Bundle{}, err
	}
	if len(contents) > MaxBundleBytes {
		return Bundle{}, fmt.Errorf("rule response exceeds its byte limit")
	}
	var bundle Bundle
	if err := decodeJSON(contents, &bundle); err != nil {
		return Bundle{}, err
	}
	// Validate the complete saved state, including disabled rules, removed
	// connection types, and dates. The file text alone is not enough.
	if _, err := ValidateBundle(bundle); err != nil {
		return Bundle{}, err
	}

	return bundle, nil
}

func (s Client) Report(ctx context.Context, status Status) error {
	if err := validateStatus(status); err != nil {
		return err
	}
	contents, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if len(contents) > MaxStatusBytes {
		return fmt.Errorf("status exceeds its byte limit")
	}
	response, err := s.request(ctx, http.MethodPost, "/v1/status", contents)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("rule service returned HTTP %d", response.StatusCode)
	}
	return nil
}
