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
package postprocessing

import (
	"context"

	"github.com/specterops/dawgs/graph"
)

// Probe queries are rebuilt in an independent read transaction. Their filter
// functions have already been evaluated once by Filterf. Ignoring order, offset
// and limit deliberately observes a superset; the probe's own fixed limit bounds
// the work even when the native mutation's filter is broad.
type observedNodeQuery struct {
	graph.NodeQuery
	tracker *changeTracker
	context context.Context
	filters []graph.Criteria
}

func (s *observedNodeQuery) Filter(criteria graph.Criteria) graph.NodeQuery {
	s.filters = append(s.filters, criteria)
	s.NodeQuery = s.NodeQuery.Filter(criteria)
	return s
}
func (s *observedNodeQuery) Filterf(provider graph.CriteriaProvider) graph.NodeQuery {
	return s.Filter(provider())
}
func (s *observedNodeQuery) OrderBy(criteria ...graph.Criteria) graph.NodeQuery {
	s.NodeQuery = s.NodeQuery.OrderBy(criteria...)
	return s
}
func (s *observedNodeQuery) Offset(offset int) graph.NodeQuery {
	s.NodeQuery = s.NodeQuery.Offset(offset)
	return s
}
func (s *observedNodeQuery) Limit(limit int) graph.NodeQuery {
	s.NodeQuery = s.NodeQuery.Limit(limit)
	return s
}
func (s *observedNodeQuery) Update(properties *graph.Properties) error {
	if s.tracker.active(s.context) {
		s.tracker.nodeKinds(s.context, s.filters...)
	}
	return s.NodeQuery.Update(properties)
}
func (s *observedNodeQuery) Delete() error {
	s.tracker.Unknown(s.context)
	return s.NodeQuery.Delete()
}
func (s *observedNodeQuery) Query(delegate func(graph.Result) error, criteria ...graph.Criteria) error {
	// Final criteria can contain updating clauses, including label changes.
	s.tracker.Unknown(s.context)
	return s.NodeQuery.Query(delegate, criteria...)
}
func (s *observedNodeQuery) Fetch(delegate func(graph.Cursor[*graph.Node]) error, criteria ...graph.Criteria) error {
	if len(criteria) > 0 {
		s.tracker.Unknown(s.context)
	}
	return s.NodeQuery.Fetch(delegate, criteria...)
}

type observedRelationshipQuery struct {
	graph.RelationshipQuery
	tracker *changeTracker
	context context.Context
	filters []graph.Criteria
}

func (s *observedRelationshipQuery) Filter(criteria graph.Criteria) graph.RelationshipQuery {
	s.filters = append(s.filters, criteria)
	s.RelationshipQuery = s.RelationshipQuery.Filter(criteria)
	return s
}
func (s *observedRelationshipQuery) Filterf(provider graph.CriteriaProvider) graph.RelationshipQuery {
	return s.Filter(provider())
}
func (s *observedRelationshipQuery) OrderBy(criteria ...graph.Criteria) graph.RelationshipQuery {
	s.RelationshipQuery = s.RelationshipQuery.OrderBy(criteria...)
	return s
}
func (s *observedRelationshipQuery) Offset(offset int) graph.RelationshipQuery {
	s.RelationshipQuery = s.RelationshipQuery.Offset(offset)
	return s
}
func (s *observedRelationshipQuery) Limit(limit int) graph.RelationshipQuery {
	s.RelationshipQuery = s.RelationshipQuery.Limit(limit)
	return s
}
func (s *observedRelationshipQuery) Update(properties *graph.Properties) error {
	if s.tracker.active(s.context) {
		s.tracker.relationshipKinds(s.context, s.filters...)
	}
	return s.RelationshipQuery.Update(properties)
}
func (s *observedRelationshipQuery) Delete() error {
	if s.tracker.active(s.context) {
		s.tracker.relationshipKinds(s.context, s.filters...)
	}
	return s.RelationshipQuery.Delete()
}
func (s *observedRelationshipQuery) Query(delegate func(graph.Result) error, criteria ...graph.Criteria) error {
	s.tracker.Unknown(s.context)
	return s.RelationshipQuery.Query(delegate, criteria...)
}
