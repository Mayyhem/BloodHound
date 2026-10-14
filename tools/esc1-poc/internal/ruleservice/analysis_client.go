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
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AnalysisController uses the native API so the ordinary pipeline also runs saved rules.
type AnalysisController interface {
	Status(context.Context) (AnalysisState, error)
	Run(context.Context) error
}

type AnalysisState struct {
	Available    bool   `json:"available"`
	Pending      bool   `json:"pending"`
	Running      bool   `json:"running"`
	NativeStatus string `json:"native_status"`
	Message      string `json:"message"`
}

var ErrAnalysisBusy = errors.New("native analysis is already queued, running, or waiting for graph deletion")

type analysisClient struct {
	origin   string
	tokenID  string
	tokenKey string
	http     *http.Client
}

// NewAnalysisClient keeps the API key on the server and accepts only an explicit origin.
func NewAnalysisClient(baseURL, tokenID, tokenKey string) (AnalysisController, error) {
	var (
		parsed, err                      = url.Parse(baseURL)
		defaultTransport, validTransport = http.DefaultTransport.(*http.Transport)
	)
	if baseURL == "" && tokenID == "" && tokenKey == "" {
		return nil, nil
	}
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(tokenID) == "" || strings.TrimSpace(tokenKey) == "" {
		return nil, fmt.Errorf("native analysis requires an API address, token ID, and token key")
	}
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(baseURL, "#") || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("native analysis API address must be an HTTP or HTTPS origin without credentials, path, query, or fragment")
	}
	// Token IDs are UUIDs; rejecting header characters also prevents accidental credential injection.
	if len(tokenID) != 36 {
		return nil, fmt.Errorf("native analysis token ID must be a UUID")
	}
	for index, character := range tokenID {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return nil, fmt.Errorf("native analysis token ID must be a UUID")
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return nil, fmt.Errorf("native analysis token ID must be a UUID")
		}
	}
	if !validTransport {
		return nil, fmt.Errorf("native analysis HTTP transport is unavailable")
	}
	var transport = defaultTransport.Clone()
	transport.Proxy = nil
	return &analysisClient{origin: parsed.String(), tokenID: tokenID, tokenKey: tokenKey, http: &http.Client{
		Transport: transport, Timeout: 6 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Sign the method, fixed path, current hour, and empty body using BloodHound's HMAC chain.
func (s *analysisClient) request(ctx context.Context, method, path string, expectedStatus int) ([]byte, error) {
	var (
		requestDate = time.Now().UTC().Format(time.RFC3339)
		operation   = hmac.New(sha256.New, []byte(s.tokenKey))
	)
	operation.Write([]byte(method + path))
	date := hmac.New(sha256.New, operation.Sum(nil))
	date.Write([]byte(requestDate[:13]))
	body := hmac.New(sha256.New, date.Sum(nil))
	request, err := http.NewRequestWithContext(ctx, method, s.origin+path, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create native analysis request")
	}
	request.Header.Set("Authorization", "bhesignature "+s.tokenID)
	request.Header.Set("RequestDate", requestDate)
	request.Header.Set("Signature", base64.StdEncoding.EncodeToString(body.Sum(nil)))
	response, err := s.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the native analysis API")
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("native analysis API rejected the configured token; check API token access and expiration")
		case http.StatusForbidden:
			return nil, fmt.Errorf("native analysis API token lacks the required permission")
		default:
			return nil, fmt.Errorf("native analysis API returned an unexpected HTTP status (%d)", response.StatusCode)
		}
	}
	limit := int64(64 * 1024)
	if path == "/api/v2/extensions/definitions" {
		limit = MaxBundleBytes
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the native analysis API response")
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("native analysis API response exceeds its size limit")
	}
	return contents, nil
}

func (s *analysisClient) Status(ctx context.Context) (AnalysisState, error) {
	var (
		pending struct {
			Data *struct {
				RequestType *string `json:"request_type"`
			} `json:"data"`
		}
		processing struct {
			Data *struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		state = AnalysisState{Available: true}
	)
	contents, err := s.request(ctx, http.MethodGet, "/api/v2/analysis/status", http.StatusOK)
	if err != nil {
		return AnalysisState{}, err
	}
	if json.Unmarshal(contents, &pending) != nil || pending.Data == nil || pending.Data.RequestType == nil {
		return AnalysisState{}, fmt.Errorf("native analysis API returned an invalid queue status")
	}
	switch *pending.Data.RequestType {
	case "":
	case "analysis":
		state.Pending = true
	case "deletion":
		state.Available = false
		state.Message = "Graph deletion is pending; wait before requesting analysis."
	default:
		return AnalysisState{}, fmt.Errorf("native analysis API returned an unknown queue status")
	}
	contents, err = s.request(ctx, http.MethodGet, "/api/v2/datapipe/status", http.StatusOK)
	if err != nil {
		return AnalysisState{}, err
	}
	if json.Unmarshal(contents, &processing) != nil || processing.Data == nil {
		return AnalysisState{}, fmt.Errorf("native analysis API returned an invalid processing status")
	}
	state.NativeStatus = processing.Data.Status
	switch state.NativeStatus {
	case "analyzing":
		state.Running = true
	case "idle", "ingesting", "purging", "pruning", "starting", "optimizing":
	default:
		return AnalysisState{}, fmt.Errorf("native analysis API returned an unknown processing status")
	}
	if state.Message == "" {
		switch {
		case state.Running:
			state.Message = "Native analysis is running."
		case state.Pending:
			state.Message = "Native analysis is queued."
		case state.NativeStatus != "idle":
			state.Message = "BloodHound is processing background work."
		default:
			state.Message = "Ready to request native analysis and run saved rules."
		}
	}
	return state, nil
}

func (s *analysisClient) Run(ctx context.Context) error {
	var state, err = s.Status(ctx)
	if err != nil {
		return err
	}
	if !state.Available || state.Pending || state.Running {
		return ErrAnalysisBusy
	}
	_, err = s.request(ctx, http.MethodPut, "/api/v2/analysis", http.StatusAccepted)
	return err
}

// The rules page reads definitions using the existing server-side credentials.
// Tokens and schema installation never pass through the analysis upload form.
func (s *analysisClient) Schemas(ctx context.Context) (SchemaSnapshot, error) {
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	contents, err := s.request(ctx, http.MethodGet, "/api/v2/extensions/definitions", http.StatusOK)
	if err != nil {
		return SchemaSnapshot{}, err
	}
	if json.Unmarshal(contents, &response) != nil {
		return SchemaSnapshot{}, fmt.Errorf("invalid installed schema response")
	}
	return ParseSchemas(response.Data)
}
