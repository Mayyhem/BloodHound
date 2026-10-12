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
package datapipe

import (
	"context"
	"errors"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/require"
)

type extensionDeleteContextKey struct{}

type extensionGateDeleteDriver struct {
	fakeGraphDeleter
	beforeOperation func(context.Context, string)
	wipeCalls       int
	wipeError       error
}

func (s *extensionGateDeleteDriver) DeleteNodesByKinds(ctx context.Context, includeAny, excludeAny graph.Kinds) error {
	if s.beforeOperation != nil {
		s.beforeOperation(ctx, "nodes")
	}
	return s.fakeGraphDeleter.DeleteNodesByKinds(ctx, includeAny, excludeAny)
}
func (s *extensionGateDeleteDriver) DeleteRelationshipsByKinds(ctx context.Context, kinds graph.Kinds) error {
	if s.beforeOperation != nil {
		s.beforeOperation(ctx, "relationships")
	}
	return s.fakeGraphDeleter.DeleteRelationshipsByKinds(ctx, kinds)
}
func (s *extensionGateDeleteDriver) WipeGraph(ctx context.Context, retain graph.TransactionDelegate) error {
	if s.beforeOperation != nil {
		s.beforeOperation(ctx, "wipe")
	}
	s.wipeCalls++
	if s.wipeError != nil {
		return s.wipeError
	}
	return retain(&extensionGateDeleteTransaction{})
}
func (s *extensionGateDeleteDriver) ReadTransaction(ctx context.Context, callback graph.TransactionDelegate, options ...graph.TransactionOption) error {
	if s.beforeOperation != nil {
		s.beforeOperation(ctx, "migration read")
	}
	return callback(&extensionGateDeleteTransaction{})
}

type extensionGateDeleteTransaction struct{ graph.Transaction }
type extensionGateDeleteNodeQuery struct{ graph.NodeQuery }

func (s *extensionGateDeleteTransaction) Nodes() graph.NodeQuery {
	return &extensionGateDeleteNodeQuery{}
}
func (s *extensionGateDeleteTransaction) CreateNode(properties *graph.Properties, kinds ...graph.Kind) (*graph.Node, error) {
	return &graph.Node{Properties: properties, Kinds: kinds}, nil
}
func (s *extensionGateDeleteNodeQuery) Filterf(graph.CriteriaProvider) graph.NodeQuery { return s }
func (s *extensionGateDeleteNodeQuery) First() (*graph.Node, error) {
	return &graph.Node{Properties: graph.AsProperties(map[string]any{"Major": 1, "Minor": 2, "Patch": 3})}, nil
}

func extensionDeleteRequests() []struct {
	name    string
	request model.AnalysisRequest
} {
	return []struct {
		name    string
		request model.AnalysisRequest
	}{
		{"wipe", model.AnalysisRequest{DeleteAllGraph: true}},
		{"nodes", model.AnalysisRequest{DeleteSourceKinds: []string{"Base"}}},
		{"relationships", model.AnalysisRequest{DeleteRelationships: []string{"MemberOf"}}},
	}
}

func TestDeleteCollectedGraphDataRejectingExtensionHookStopsRawDeletes(t *testing.T) {
	var rejected = errors.New("graph writes stopped after uncertain commit")
	for _, testCase := range extensionDeleteRequests() {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				driver     = &extensionGateDeleteDriver{}
				database   = graph.NewDatabaseSwitch(t.Context(), driver)
				admissions int
				releases   int
			)
			database.SetWriteGate(func(ctx context.Context) (context.Context, func(), error) {
				admissions++
				return ctx, func() { releases++ }, rejected
			})
			err := DeleteCollectedGraphData(t.Context(), database, testCase.request, nil)
			require.ErrorIs(t, err, rejected)
			require.Equal(t, 1, admissions)
			require.Zero(t, releases)
			require.Zero(t, driver.wipeCalls)
			require.Empty(t, driver.nodeCalls)
			require.Empty(t, driver.relationshipCalls)
		})
	}
}

func TestDeleteCollectedGraphDataExtensionHookHoldsEntireRawOperation(t *testing.T) {
	for _, testCase := range extensionDeleteRequests() {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				driver     = &extensionGateDeleteDriver{}
				database   = graph.NewDatabaseSwitch(t.Context(), driver)
				admitted   bool
				releases   int
				operations []string
			)
			database.SetWriteGate(func(ctx context.Context) (context.Context, func(), error) {
				admitted = true
				return context.WithValue(ctx, extensionDeleteContextKey{}, "admitted"), func() { releases++ }, nil
			})
			driver.beforeOperation = func(ctx context.Context, operation string) {
				require.True(t, admitted)
				require.Zero(t, releases, "released before raw deletion completed")
				require.Equal(t, "admitted", ctx.Value(extensionDeleteContextKey{}))
				operations = append(operations, operation)
			}
			require.NoError(t, DeleteCollectedGraphData(t.Context(), database, testCase.request, nil))
			require.Equal(t, 1, releases)
			require.Contains(t, operations, testCase.name)
		})
	}
}

func TestDeleteCollectedGraphDataExtensionHookReleasesOnRawError(t *testing.T) {
	var failure = errors.New("raw deletion failed")
	for _, testCase := range extensionDeleteRequests() {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				driver   = &extensionGateDeleteDriver{fakeGraphDeleter: fakeGraphDeleter{nodeErr: failure, relationshipErr: failure}, wipeError: failure}
				database = graph.NewDatabaseSwitch(t.Context(), driver)
				releases int
			)
			database.SetWriteGate(func(ctx context.Context) (context.Context, func(), error) { return ctx, func() { releases++ }, nil })
			driver.beforeOperation = func(context.Context, string) { require.Zero(t, releases) }
			require.ErrorIs(t, DeleteCollectedGraphData(t.Context(), database, testCase.request, nil), failure)
			require.Equal(t, 1, releases)
		})
	}
}

func TestDeleteCollectedGraphDataWithoutExtensionHookKeepsRawDeletes(t *testing.T) {
	for _, testCase := range extensionDeleteRequests() {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				driver     = &extensionGateDeleteDriver{}
				database   = graph.NewDatabaseSwitch(t.Context(), driver)
				operations []string
			)
			driver.beforeOperation = func(ctx context.Context, operation string) {
				require.Nil(t, ctx.Value(extensionDeleteContextKey{}))
				operations = append(operations, operation)
			}
			require.NoError(t, DeleteCollectedGraphData(t.Context(), database, testCase.request, nil))
			require.Contains(t, operations, testCase.name)
		})
	}
}
