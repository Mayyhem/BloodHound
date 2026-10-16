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

package postprocessing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/querycheck"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

type editorReceipt struct {
	owner          string
	request        EditorTestRequest
	files          map[string]string
	graphVersion   GraphVersion
	createdAt      time.Time
	reviewRevision string
}

// analysisEditor remembers successful editor tests so the same author can
// review and activate exactly the file they tested. It keeps no query answers:
// every test recalculates everything it needs from the current graph.
type analysisEditor struct {
	mutex    sync.Mutex
	receipts map[string]editorReceipt
}

func newAnalysisEditor() *analysisEditor {
	return &analysisEditor{receipts: map[string]editorReceipt{}}
}

// TestAnalysisFile holds the application's graph turn from freshness checks
// through confirmed discard. It always calculates the edited file together with
// every earlier file it needs, then throws away all of those graph changes.
// The caller has already checked the user's access to all environments;
// arbitrary scalar answer tables cannot be filtered safely.
func (s *Service) TestAnalysisFile(ctx context.Context, owner string, request EditorTestRequest) (EditorTestResult, error) {
	var result = EditorTestResult{Rows: []map[string]any{}}
	if owner == "" || s.editor == nil || s.executeTest == nil {
		return result, &EditorError{http.StatusServiceUnavailable, "The analysis editor is unavailable."}
	}
	if err := request.validate(); err != nil {
		return result, err
	}
	err := s.gate.Alone(ctx, 30*time.Second, func(turnContext context.Context) error {
		// Only this exclusive context may suppress revisions for writes that
		// are guaranteed to be discarded. Ordinary writers cannot opt out.
		turnContext = withoutChangeObservation(TemporaryGraphChanges(turnContext))
		bundle, snapshot, err := s.editorInputs(turnContext)
		if err != nil {
			return err
		}
		if bundle.Revision != request.BaseRevision || snapshot.Revision() != request.SchemaRevision {
			return editorChanged()
		}
		plan, err := ruleservice.EditorPlan(bundle, snapshot, request.Name, request.Text)
		if err != nil {
			return &EditorError{http.StatusBadRequest, editorFileProblem(err, request.Name)}
		}
		files := editorPlanFiles(plan, bundle.Files, request.Name, request.Text)
		version := s.gate.Version()
		options := runner.Options{BatchID: "editor", IngestionComplete: true, WritersPaused: true, TestTarget: request.Name, TemporaryDirectory: s.configuration.Directory, Limits: editorLimits()}
		options.InspectTestAnswer = func(answerContext context.Context, _ runner.Transaction, _ string, answer runner.CompleteAnswer) error {
			return collectEditorResults(answerContext, answer, &result)
		}
		report, testErr := s.executeTest(turnContext, plan, options)
		if errors.Is(testErr, runner.ErrTestDiscardUncertain) {
			s.gate.BlockWrites(testErr)
			return &EditorError{http.StatusServiceUnavailable, "The database did not confirm discarding the test. Graph changes are stopped until this is checked."}
		}
		if testErr != nil {
			return &EditorError{http.StatusBadRequest, editorTestProblem(testErr)}
		}
		if !report.Tested || !report.Discarded || report.Committed {
			s.gate.BlockWrites(errors.New("editor test discard was not confirmed"))
			return &EditorError{http.StatusServiceUnavailable, "The database did not confirm discarding the test. Graph changes are stopped until this is checked."}
		}
		if version != s.gate.Version() {
			return editorChanged()
		}
		if ctx.Err() != nil {
			return &EditorError{http.StatusRequestTimeout, "The test was canceled. Its graph changes were discarded."}
		}
		token, err := newEditorToken()
		if err != nil {
			return &EditorError{http.StatusServiceUnavailable, "The successful test could not be recorded. Test again."}
		}
		s.editor.mutex.Lock()
		defer s.editor.mutex.Unlock()
		for id, receipt := range s.editor.receipts {
			if s.clock().Sub(receipt.createdAt) > 30*time.Minute {
				delete(s.editor.receipts, id)
			}
		}
		if len(s.editor.receipts) >= 64 {
			var oldestID string
			var oldest time.Time
			for id, receipt := range s.editor.receipts {
				if oldestID == "" || receipt.createdAt.Before(oldest) {
					oldestID, oldest = id, receipt.createdAt
				}
			}
			delete(s.editor.receipts, oldestID)
		}
		s.editor.receipts[token] = editorReceipt{owner: owner, request: request, files: files, graphVersion: version, createdAt: s.clock()}
		result.TestID, result.Execution = token, report.Execution
		return nil
	})
	if err != nil {
		if _, ok := err.(*EditorError); !ok {
			if ctx.Err() != nil {
				err = &EditorError{http.StatusRequestTimeout, "The test was canceled. No successful test was recorded."}
			} else {
				err = &EditorError{http.StatusServiceUnavailable, "BloodHound is still changing the graph, for example ingesting an upload or running analysis, so the test could not start. Tests wait for that to finish; try again when the status line on the analysis page shows it is done."}
			}
		}
		return EditorTestResult{}, err
	}
	return result, nil
}

func (s *Service) ReviewAnalysisFile(ctx context.Context, owner, testID string) (WorkspacePreview, error) {
	var result WorkspacePreview
	if s.editor == nil {
		return result, &EditorError{http.StatusServiceUnavailable, "The analysis editor is unavailable."}
	}
	err := s.gate.Alone(ctx, 30*time.Second, func(turnContext context.Context) error {
		receipt, err := s.currentEditorReceipt(turnContext, owner, testID)
		if err != nil {
			return err
		}
		result, err = s.editorManager().Preview(turnContext, editorReceiptChange(receipt))
		if err != nil {
			return &EditorError{http.StatusBadRequest, "The tested file cannot be activated yet. Check its installed output schema and review the current rules."}
		}
		s.editor.mutex.Lock()
		receipt.reviewRevision = result.Revision
		s.editor.receipts[testID] = receipt
		s.editor.mutex.Unlock()
		return nil
	})
	return result, err
}

func (s *Service) ActivateAnalysisFile(ctx context.Context, owner, testID, confirmRevision string) (string, error) {
	var revision string
	if s.editor == nil {
		return revision, &EditorError{http.StatusServiceUnavailable, "The analysis editor is unavailable."}
	}
	err := s.gate.Alone(ctx, 30*time.Second, func(turnContext context.Context) error {
		receipt, err := s.currentEditorReceipt(turnContext, owner, testID)
		if err != nil {
			return err
		}
		if confirmRevision == "" || confirmRevision != receipt.reviewRevision {
			return editorChanged()
		}
		change := editorReceiptChange(receipt)
		change.ConfirmRevision = confirmRevision
		revision, err = s.editorManager().Apply(turnContext, change)
		if err != nil {
			return editorChanged()
		}
		s.editor.mutex.Lock()
		delete(s.editor.receipts, testID)
		s.editor.mutex.Unlock()
		return nil
	})
	return revision, err
}

func (s *Service) currentEditorReceipt(ctx context.Context, owner, testID string) (editorReceipt, error) {
	s.editor.mutex.Lock()
	receipt, found := s.editor.receipts[testID]
	s.editor.mutex.Unlock()
	if !found || owner == "" || receipt.owner != owner || s.clock().Sub(receipt.createdAt) > 30*time.Minute || receipt.graphVersion != s.gate.Version() {
		return editorReceipt{}, editorChanged()
	}
	bundle, snapshot, err := s.editorInputs(ctx)
	if err != nil {
		return editorReceipt{}, err
	}
	if bundle.Revision != receipt.request.BaseRevision || snapshot.Revision() != receipt.request.SchemaRevision {
		return editorReceipt{}, editorChanged()
	}
	for name, text := range receipt.files {
		if name != receipt.request.Name && bundle.Files[name] != text {
			return editorReceipt{}, editorChanged()
		}
	}
	return receipt, nil
}
func (s *Service) editorInputs(ctx context.Context) (ruleservice.Bundle, ruleservice.SchemaSnapshot, error) {
	bundle, err := s.client.Fetch(ctx)
	if errors.Is(err, ruleservice.ErrNoActive) {
		bundle = ruleservice.Bundle{Files: map[string]string{}}
	} else if err != nil {
		return bundle, ruleservice.SchemaSnapshot{}, &EditorError{http.StatusBadGateway, "The current analysis files are unavailable. Refresh and try again."}
	}
	if s.configuration.ReadSchemas == nil {
		return bundle, ruleservice.SchemaSnapshot{}, &EditorError{http.StatusServiceUnavailable, "The installed schema reader is unavailable."}
	}
	contents, err := s.configuration.ReadSchemas(ctx)
	if err != nil {
		return bundle, ruleservice.SchemaSnapshot{}, &EditorError{http.StatusBadGateway, "Installed schemas are unavailable. Refresh and try again."}
	}
	snapshot, err := ruleservice.ParseSchemas(contents)
	if err != nil {
		return bundle, snapshot, &EditorError{http.StatusBadGateway, "Installed schemas could not be checked. Refresh and try again."}
	}
	return bundle, snapshot, nil
}
func (s *Service) editorManager() WorkspaceManager {
	return WorkspaceManager{WorkspaceReader: WorkspaceReader{URL: s.configuration.URL, Token: s.configuration.Token}}
}
func editorReceiptChange(receipt editorReceipt) WorkspaceChange {
	return WorkspaceChange{BaseRevision: receipt.request.BaseRevision, SchemaRevision: receipt.request.SchemaRevision, Files: map[string]string{receipt.request.Name: receipt.request.Text}, Overwrite: []string{receipt.request.Name}, Enable: []string{receipt.request.Name}}
}
func editorChanged() *EditorError {
	return &EditorError{http.StatusConflict, "The successful test or review is no longer current. Refresh and test the exact file again."}
}
func newEditorToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
func editorPlanFiles(plan extension.Plan, original map[string]string, target, text string) map[string]string {
	files := map[string]string{}
	for _, query := range plan.Queries {
		if query.DisabledReason == "" {
			files[query.Name] = original[query.Name]
			if query.Name == target {
				files[target] = text
			}
		}
	}
	return files
}
func editorLimits() runner.Limits {
	limits := runner.DefaultLimits()
	limits.Workers = 1
	limits.ResultRows = 1_000_000
	limits.DiskBytes = 512 << 20
	limits.ChangedObjects = 100_000
	limits.QueryTimeout = 2 * time.Minute
	limits.RunTimeout = 5 * time.Minute
	return limits
}

// PreviewAnalysisFile shows the actual selected chain, including additional
// producers of a shared graph type. It reads rules/schemas but runs no Cypher.
func (s *Service) PreviewAnalysisFile(ctx context.Context, owner string, request EditorTestRequest) (EditorPlanPreview, error) {
	var result EditorPlanPreview
	if owner == "" || s.editor == nil {
		return result, &EditorError{http.StatusServiceUnavailable, "The analysis editor is unavailable."}
	}
	if err := request.validate(); err != nil {
		return result, err
	}
	err := s.gate.Alone(ctx, 30*time.Second, func(turnContext context.Context) error {
		bundle, snapshot, err := s.editorInputs(turnContext)
		if err != nil {
			return err
		}
		if bundle.Revision != request.BaseRevision || snapshot.Revision() != request.SchemaRevision {
			return editorChanged()
		}
		plan, err := ruleservice.EditorPlan(bundle, snapshot, request.Name, request.Text)
		if err != nil {
			return &EditorError{http.StatusBadRequest, editorFileProblem(err, request.Name)}
		}
		bundle.Files = editorPlanFiles(plan, bundle.Files, request.Name, request.Text)
		result.Unused = []string{}
		for _, query := range plan.Queries {
			if query.Name != request.Name {
				continue
			}
			// The plan already checked this text, so this second check succeeds.
			if checked, err := querycheck.Check(query.Text); err == nil {
				result.Unused = append(result.Unused, extension.UnusedDependencies(query, checked)...)
			}
		}
		result.Plan = ruleservice.BuildView(bundle, plan)
		result.Plan.Threads = nil
		result.Dependencies = []extension.ExecutionDependency{}
		for _, query := range plan.Queries {
			result.Dependencies = append(result.Dependencies, plan.ExecutionDependencies(query)...)
		}
		return nil
	})
	return result, err
}
