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
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

//go:embed upload.html
var uploadPage string

// NewHandler exposes bundle storage and worker reports. The token is never
// placed in a URL, persisted by the page, or included in an error message.
func NewHandler(store *Store, token string) (http.Handler, error) {
	return NewHandlerWithAnalysis(store, token, nil)
}

// NewHandlerWithAnalysis also lets the page request BloodHound's normal analysis.
// The connection and its secret stay on this server, never in the browser.
func NewHandlerWithAnalysis(store *Store, token string, analysis AnalysisController) (http.Handler, error) {
	if store == nil || len(token) < 32 || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("a store and bearer token of at least 32 bytes are required")
	}
	var (
		expected         = sha256.Sum256([]byte(token))
		uploads          = make(chan struct{}, 4)
		analysisRequests = newAnalysisHandler(analysis)
	)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.URL.Path == "/" || request.URL.Path == "/health" {
			if request.Method != http.MethodGet {
				fail(writer, http.StatusMethodNotAllowed, "GET required")
				return
			}
			if request.URL.Path == "/health" {
				respond(writer, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
			_, _ = io.WriteString(writer, uploadPage)
			return
		}
		if request.URL.Path != "/v1/rules" && request.URL.Path != "/v1/status" && request.URL.Path != "/v1/plan" && request.URL.Path != "/v1/preview" && request.URL.Path != "/v1/analysis" && request.URL.Path != "/v1/draft" {
			fail(writer, http.StatusNotFound, "not found")
			return
		}
		authorization := request.Header.Get("Authorization")
		supplied := sha256.Sum256([]byte(strings.TrimPrefix(authorization, "Bearer ")))
		if !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			fail(writer, http.StatusUnauthorized, "bearer token required")
			return
		}
		if request.URL.Path == "/v1/analysis" {
			analysisRequests.ServeHTTP(writer, request)
			return
		}
		if request.Method == http.MethodGet {
			if request.URL.Path == "/v1/draft" {
				respond(writer, http.StatusOK, store.Draft())
				return
			}
			if request.URL.Path == "/v1/plan" {
				respond(writer, http.StatusOK, store.View())
				return
			}
			if request.URL.Path == "/v1/preview" {
				fail(writer, http.StatusMethodNotAllowed, "POST required")
				return
			}
			if request.URL.Path == "/v1/status" {
				respond(writer, http.StatusOK, store.LatestStatus())
				return
			}
			bundle, err := store.Active()
			if errors.Is(err, ErrNoActive) {
				fail(writer, http.StatusNotFound, ErrNoActive.Error())
				return
			}
			if err != nil {
				fail(writer, http.StatusInternalServerError, "cannot read rules")
				return
			}
			respond(writer, http.StatusOK, bundle)
			return
		}
		// GET requests were handled above. Only these specific changes remain.
		requiredMethod := http.MethodPost
		if request.URL.Path == "/v1/rules" || request.URL.Path == "/v1/draft" {
			requiredMethod = http.MethodPut
		}
		if request.URL.Path == "/v1/draft" && request.Method == http.MethodDelete {
			requiredMethod = http.MethodDelete
		}
		if request.URL.Path == "/v1/plan" {
			requiredMethod = http.MethodGet
		}
		if request.Method != requiredMethod {
			fail(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// Bound simultaneous request allocations as well as each individual body.
		select {
		case uploads <- struct{}{}:
			defer func() { <-uploads }()
		default:
			fail(writer, http.StatusServiceUnavailable, "too many uploads; retry later")
			return
		}
		limit := int64(MaxStatusBytes)
		if request.URL.Path == "/v1/rules" || request.URL.Path == "/v1/preview" || request.URL.Path == "/v1/draft" {
			limit = MaxBundleBytes
		}
		contents, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, limit))
		if err != nil {
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				fail(writer, http.StatusRequestEntityTooLarge, "request exceeds its byte limit")
			} else {
				fail(writer, http.StatusBadRequest, "cannot read request")
			}
			return
		}
		if request.URL.Path == "/v1/status" {
			var status Status
			if err := decodeJSON(contents, &status); err != nil {
				fail(writer, http.StatusBadRequest, err.Error())
				return
			}
			if err := store.Report(status); err != nil {
				fail(writer, http.StatusBadRequest, err.Error())
				return
			}
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		var upload Change
		if err := decodeJSON(contents, &upload); err != nil {
			fail(writer, http.StatusBadRequest, err.Error())
			return
		}
		if request.URL.Path == "/v1/draft" {
			if request.Method == http.MethodDelete {
				if err := store.DiscardDraft(upload.DraftRevision); err != nil {
					changeFailure(writer, err)
					return
				}
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			draft, err := store.SaveDraft(upload)
			if err != nil {
				changeFailure(writer, err)
				return
			}
			respond(writer, http.StatusOK, draft)
			return
		}
		if request.URL.Path == "/v1/preview" {
			preview, err := store.Preview(upload)
			if err != nil {
				changeFailure(writer, err)
				return
			}
			respond(writer, http.StatusOK, preview)
			return
		}
		bundle, err := store.Apply(upload)
		if err != nil {
			changeFailure(writer, err)
			return
		}
		respond(writer, http.StatusOK, bundle)
	}), nil
}

func changeFailure(writer http.ResponseWriter, err error) {
	if errors.Is(err, ErrConflict) {
		fail(writer, http.StatusConflict, err.Error())
		return
	}
	fail(writer, http.StatusBadRequest, err.Error())
}

func respond(writer http.ResponseWriter, code int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(code)
	_ = json.NewEncoder(writer).Encode(value)
}
func fail(writer http.ResponseWriter, code int, message string) {
	respond(writer, code, map[string]string{"error": message})
}

// Serve keeps slow headers, bodies, and idle connections from occupying the
// registry indefinitely. Cancellation stops accepting requests and drains work.
func Serve(ctx context.Context, address string, store *Store, token, localOrigin string, analysis AnalysisController) error {
	handler, err := NewHandlerWithAnalysis(store, token, analysis)
	if err != nil {
		return err
	}
	handler, err = NewBrowserHandler(handler, token, localOrigin)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	finished := make(chan error, 1)
	go func() { finished <- server.ListenAndServe() }()
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		err := <-finished
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
