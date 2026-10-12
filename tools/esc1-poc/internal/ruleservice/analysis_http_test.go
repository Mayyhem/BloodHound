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

package ruleservice_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

type analysisController struct {
	state       ruleservice.AnalysisState
	statusError error
	runError    error
	starts      int
	entered     chan struct{}
	release     chan struct{}
}

func (s *analysisController) Status(context.Context) (ruleservice.AnalysisState, error) {
	return s.state, s.statusError
}

func (s *analysisController) Run(context.Context) error {
	s.starts++
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	return s.runError
}

func TestAnalysisRequestsRequireAccessAndEmptyObject(t *testing.T) {
	var (
		controller   = &analysisController{state: ruleservice.AnalysisState{Available: true}}
		token        = strings.Repeat("a", 32)
		handler, err = ruleservice.NewHandlerWithAnalysis(open(t), token, controller)
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, token, body string
		code                int
	}{
		{"GET", "", "", 401},
		{"POST", "wrong", "{}", 401},
		{"DELETE", token, "{}", 405},
		{"POST", token, "null", 400},
		{"POST", token, `{"path":"/api/v2/other"}`, 400},
		{"POST", token, "{} {}", 400},
		{"POST", token, strings.Repeat(" ", 1025), 400},
		{"GET", token, "", 200},
	} {
		response := exchange(handler, test.method, "/v1/analysis", test.token, []byte(test.body))
		if response.Code != test.code {
			t.Fatalf("%s got %d; want %d", test.method, response.Code, test.code)
		}
	}
	if controller.starts != 0 {
		t.Fatal("invalid request reached BloodHound")
	}
	response := exchange(handler, "POST", "/v1/analysis", token, []byte("{}"))
	if response.Code != 202 || !strings.Contains(response.Body.String(), `"pending":true`) || controller.starts != 1 {
		t.Fatal("valid request was not queued", response.Code, response.Body.String())
	}
}

func TestAnalysisUnavailableBusyAndErrors(t *testing.T) {
	var token = strings.Repeat("a", 32)
	handler, err := ruleservice.NewHandler(open(t), token)
	if err != nil {
		t.Fatal(err)
	}
	response := exchange(handler, "GET", "/v1/analysis", token, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"available":false`) {
		t.Fatal(response)
	}
	if response := exchange(handler, "POST", "/v1/analysis", token, []byte("{}")); response.Code != 503 {
		t.Fatal(response)
	}
	for _, test := range []struct {
		method  string
		failure error
		code    int
	}{
		{"POST", ruleservice.ErrAnalysisBusy, 409},
		{"POST", errors.New("private native response and key"), 502},
		{"GET", errors.New("private native response and key"), 502},
	} {
		controller := &analysisController{runError: test.failure, statusError: test.failure}
		handler, err := ruleservice.NewHandlerWithAnalysis(open(t), token, controller)
		if err != nil {
			t.Fatal(err)
		}
		response := exchange(handler, test.method, "/v1/analysis", token, []byte("{}"))
		if response.Code != test.code || strings.Contains(response.Body.String(), "private native") {
			t.Fatal("wrong error or leaked details", response.Code, response.Body.String())
		}
	}
}

func TestAnalysisRejectsOverlappingClicks(t *testing.T) {
	var (
		controller   = &analysisController{entered: make(chan struct{}), release: make(chan struct{})}
		token        = strings.Repeat("a", 32)
		finished     = make(chan int, 1)
		handler, err = ruleservice.NewHandlerWithAnalysis(open(t), token, controller)
	)
	if err != nil {
		t.Fatal(err)
	}
	go func() { finished <- exchange(handler, "POST", "/v1/analysis", token, []byte("{}")).Code }()
	<-controller.entered
	response := exchange(handler, "POST", "/v1/analysis", token, []byte("{}"))
	close(controller.release)
	if code := <-finished; code != http.StatusAccepted {
		t.Fatal(code)
	}
	if response.Code != http.StatusConflict || controller.starts != 1 {
		t.Fatal("overlapping start was not refused")
	}
}
