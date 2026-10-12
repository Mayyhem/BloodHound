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

package datapipe

import (
	"context"
	"errors"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/specterops/bloodhound/cmd/api/src/database"
	"github.com/specterops/bloodhound/cmd/api/src/database/mocks"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/model/appcfg"
	"github.com/specterops/bloodhound/cmd/api/src/services/job"
	"github.com/specterops/bloodhound/packages/go/cache"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type extensionHook struct {
	withAnalysis func(context.Context, func(context.Context) error) error
	retry        func(context.Context) (bool, error)
	after        func(context.Context) (bool, error)
}

func (s extensionHook) WithAnalysis(ctx context.Context, action func(context.Context) error) error {
	if s.withAnalysis != nil {
		return s.withAnalysis(ctx, action)
	}
	return action(ctx)
}

func (s extensionHook) Retry(ctx context.Context) (bool, error)         { return s.retry(ctx) }
func (s extensionHook) AfterAnalysis(ctx context.Context) (bool, error) { return s.after(ctx) }

func TestRetryExtensionsCacheInvalidation(t *testing.T) {
	var hookError = errors.New("extension failed")
	for _, testCase := range []struct {
		name      string
		installed bool
		committed bool
		err       error
	}{
		{name: "disabled"},
		{name: "no graph change", installed: true},
		{name: "failed before commit", installed: true, err: hookError},
		{name: "committed", installed: true, committed: true},
		{name: "committed before reporting failure", installed: true, committed: true, err: hookError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var ctx = context.WithValue(context.Background(), extensionContextKey{}, "original context")
			answerCache, err := cache.NewCache(cache.Config{MaxSize: 2})
			require.NoError(t, err)
			_, _, err = answerCache.Set("answer", "stale answer")
			require.NoError(t, err)
			var pipeline = BHCEPipeline{cache: answerCache}
			if testCase.installed {
				pipeline.ExtensionAnalysis = extensionHook{retry: func(actual context.Context) (bool, error) {
					require.Equal(t, ctx, actual)
					return testCase.committed, testCase.err
				}}
			}
			require.ErrorIs(t, pipeline.retryExtensions(ctx), testCase.err)
			if testCase.committed {
				require.Zero(t, answerCache.Len())
			} else {
				require.Equal(t, 1, answerCache.Len())
			}
		})
	}
}

type extensionContextKey struct{}

type extensionEmptyDatabase struct {
	graph.Database
	err error
}

func (s extensionEmptyDatabase) ReadTransaction(_ context.Context, delegate graph.TransactionDelegate, _ ...graph.TransactionOption) error {
	if s.err != nil {
		return s.err
	}
	return delegate(extensionEmptyTransaction{})
}

type extensionEmptyTransaction struct{ graph.Transaction }

func (s extensionEmptyTransaction) Nodes() graph.NodeQuery { return extensionEmptyNodes{} }

type extensionEmptyNodes struct{ graph.NodeQuery }

func (s extensionEmptyNodes) Filterf(criteria graph.CriteriaProvider) graph.NodeQuery {
	criteria()
	return s
}
func (s extensionEmptyNodes) Fetch(delegate func(graph.Cursor[*graph.Node]) error, _ ...graph.Criteria) error {
	var nodes = make(chan *graph.Node)
	close(nodes)
	return delegate(extensionEmptyCursor{nodes: nodes})
}

type extensionEmptyCursor struct{ nodes chan *graph.Node }

func (s extensionEmptyCursor) Chan() chan *graph.Node { return s.nodes }
func (s extensionEmptyCursor) Error() error           { return nil }
func (s extensionEmptyCursor) Close()                 {}

func TestAnalyzeExtensionHookRequiresNativeSuccess(t *testing.T) {
	var failure = errors.New("native operation failed")
	for _, testCase := range []struct {
		name            string
		disabled        bool
		clearingError   error
		graphError      error
		completionError error
		extensionError  error
		wantHook        bool
		wantError       bool
	}{
		{name: "request clearing failed", clearingError: failure, wantError: true},
		{name: "analysis disabled", disabled: true, wantError: true},
		{name: "native graph analysis failed", graphError: failure, wantError: true},
		{name: "completion not recorded", completionError: failure, wantError: true},
		{name: "successful native analysis", wantHook: true},
		{name: "extension failure keeps native success", extensionError: failure, wantHook: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				ctx                                  = context.Background()
				nativeDB                             = mocks.NewMockDatabase(gomock.NewController(t))
				completed, jobsCompleted, hookCalled bool
			)
			answerCache, err := cache.NewCache(cache.Config{MaxSize: 2})
			require.NoError(t, err)
			_, _, err = answerCache.Set("answer", "old answer")
			require.NoError(t, err)
			nativeDB.EXPECT().DeleteAnalysisRequest(ctx).Return(testCase.clearingError)
			if testCase.clearingError == nil && !testCase.disabled {
				nativeDB.EXPECT().SetLastAnalysisStartTime(ctx).Return(nil)
				nativeDB.EXPECT().GetFlagByKey(ctx, appcfg.FeatureVariableAnalysisMode).Return(appcfg.FeatureFlag{Enabled: true}, nil)
				if testCase.graphError == nil {
					nativeDB.EXPECT().GetFlagByKey(ctx, appcfg.FeatureOpenGraphDataQuality).Return(appcfg.FeatureFlag{}, nil)
					nativeDB.EXPECT().UpdateLastAnalysisCompleteTime(ctx).DoAndReturn(func(context.Context) error {
						completed = testCase.completionError == nil
						return testCase.completionError
					})
					if testCase.completionError == nil {
						nativeDB.EXPECT().GetIngestJobsWithStatus(ctx, model.JobStatusAnalyzing).DoAndReturn(func(context.Context, model.JobStatus) ([]model.IngestJob, error) {
							jobsCompleted = true
							return nil, nil
						})
						nativeDB.EXPECT().GetFlagByKey(ctx, appcfg.FeatureEntityPanelCaching).Return(appcfg.FeatureFlag{}, nil)
					}
				} else {
					nativeDB.EXPECT().GetIngestJobsWithStatus(ctx, model.JobStatusAnalyzing).Return([]model.IngestJob{}, nil)
				}
			}
			var pipeline = BHCEPipeline{
				cfg: config.Configuration{DisableAnalysis: testCase.disabled}, db: nativeDB,
				graphdb: extensionEmptyDatabase{err: testCase.graphError}, cache: answerCache,
				jobService: job.NewJobService(ctx, nativeDB),
				ExtensionAnalysis: extensionHook{after: func(actual context.Context) (bool, error) {
					require.Equal(t, ctx, actual)
					require.True(t, completed, "native completion must be recorded first")
					require.True(t, jobsCompleted, "native ingestion jobs must be completed first")
					hookCalled = true
					return false, testCase.extensionError
				}},
			}
			err = pipeline.analyze(ctx, model.AnalysisSteps{})
			require.Equal(t, testCase.wantError, err != nil)
			require.Equal(t, testCase.wantHook, hookCalled)
			if testCase.wantHook {
				require.Zero(t, answerCache.Len())
			} else {
				require.Equal(t, 1, answerCache.Len())
			}
		})
	}
}

func TestAnalyzeCoordinatesOnlyQueuedWork(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		installed bool
		queued    bool
		reject    bool
	}{
		{name: "disabled hook queued work", queued: true},
		{name: "disabled hook idle"},
		{name: "queued work coordinated", installed: true, queued: true},
		{name: "idle retries without native turn", installed: true},
		{name: "admission failure leaves analysis request", installed: true, queued: true, reject: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				ctx              = context.Background()
				coordinated      = context.WithValue(ctx, extensionContextKey{}, "native turn")
				nativeDB         = mocks.NewMockDatabase(gomock.NewController(t))
				admissionError   = errors.New("writer admission expired")
				entered, retried bool
				pipeline         = BHCEPipeline{cfg: config.Configuration{DisableAnalysis: true}, db: nativeDB, jobService: job.NewJobService(ctx, nativeDB)}
			)
			nativeDB.EXPECT().GetIngestJobsWithStatus(ctx, model.JobStatusAnalyzing).Return([]model.IngestJob{}, nil)
			if testCase.queued {
				nativeDB.EXPECT().GetAnalysisRequest(ctx).Return(model.AnalysisRequest{AnalysisSteps: model.AnalysisStepsFull()}, nil)
				if !testCase.reject {
					var actionContext context.Context = ctx
					if testCase.installed {
						actionContext = coordinated
					}
					nativeDB.EXPECT().DeleteAnalysisRequest(actionContext).Return(nil)
				}
			} else {
				nativeDB.EXPECT().GetAnalysisRequest(ctx).Return(model.AnalysisRequest{}, database.ErrNotFound)
			}
			if testCase.installed {
				pipeline.ExtensionAnalysis = extensionHook{
					withAnalysis: func(actual context.Context, action func(context.Context) error) error {
						require.Equal(t, ctx, actual)
						entered = true
						if testCase.reject {
							return admissionError
						}
						return action(coordinated)
					},
					retry: func(actual context.Context) (bool, error) {
						require.Equal(t, ctx, actual)
						retried = true
						return false, nil
					},
				}
			}
			err := pipeline.Analyze(ctx)
			if testCase.reject {
				require.ErrorIs(t, err, admissionError)
			} else if testCase.queued {
				require.ErrorIs(t, err, ErrAnalysisDisabled)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.installed && testCase.queued, entered)
			require.Equal(t, testCase.installed && !testCase.queued, retried)
		})
	}
}
