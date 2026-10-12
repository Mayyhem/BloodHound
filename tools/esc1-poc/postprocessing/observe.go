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
	"slices"
	"sync"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

const (
	observationLookupLimit = 256
	observationRowLimit    = 4096
	observationKindLimit   = 4096
)

type observationContextKey struct{}
type observationGraphKey struct{}

// withoutChangeObservation marks only the extension's own publication work.
// Native analysis uses the same exclusive turn but must remain observable.
func withoutChangeObservation(ctx context.Context) context.Context {
	return context.WithValue(ctx, observationContextKey{}, true)
}

// Remember what a write could change, even if it later reports an error: a
// batch can save some of its work before failing. After startup we do not know
// what changed earlier, so the first successful extension run must be complete.
type changeTracker struct {
	mutex         sync.Mutex
	known         bool
	nodes         map[string]struct{}
	relationships map[string]struct{}
	lookups       int
	rows          int
	read          func(context.Context, graph.TransactionDelegate) error
}

func (s *changeTracker) Snapshot() runner.Changes {
	var changes runner.Changes
	s.mutex.Lock()
	defer s.mutex.Unlock()
	changes.CoverageKnown = s.known
	for kind := range s.nodes {
		changes.Nodes = append(changes.Nodes, kind)
	}
	for kind := range s.relationships {
		changes.Relationships = append(changes.Relationships, kind)
	}
	slices.Sort(changes.Nodes)
	slices.Sort(changes.Relationships)
	return changes
}

func (s *changeTracker) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.known = true
	s.nodes = make(map[string]struct{})
	s.relationships = make(map[string]struct{})
	s.lookups = 0
	s.rows = 0
}

func (s *changeTracker) Unknown(ctx context.Context) {
	if ctx.Value(observationContextKey{}) == true {
		return
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.known = false
}

func (s *changeTracker) active(ctx context.Context) bool {
	if ctx.Value(observationContextKey{}) == true {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.known
}

func (s *changeTracker) add(ctx context.Context, nodes bool, kinds ...graph.Kind) {
	if ctx.Value(observationContextKey{}) == true {
		return
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !s.known {
		return
	}
	for _, kind := range kinds {
		if kind == nil {
			s.known = false
			return
		}
		if nodes {
			s.nodes[kind.String()] = struct{}{}
		} else {
			s.relationships[kind.String()] = struct{}{}
		}
		if len(s.nodes)+len(s.relationships) >= observationKindLimit {
			s.known = false
			return
		}
	}
}

func (s *changeTracker) lookup(ctx context.Context) bool {
	if ctx.Value(observationContextKey{}) == true {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !s.known {
		return false
	}
	if s.lookups >= observationLookupLimit || s.rows >= observationRowLimit {
		s.known = false
		return false
	}
	s.lookups++
	return true
}

func (s *changeTracker) row(ctx context.Context) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.rows++
	if s.rows > observationRowLimit {
		s.known = false
		return false
	}
	return s.known
}

// These small reads ask which types of objects or connections a write can
// change. If answering costs too much or fails, native work continues and the
// next extension run checks every rule instead of guessing which ones to skip.
func (s *changeTracker) readKinds(ctx context.Context, delegate graph.TransactionDelegate) error {
	var (
		readContext, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
		wrapped             = func(transaction graph.Transaction) error {
			if schema, ok := ctx.Value(observationGraphKey{}).(graph.Graph); ok {
				transaction = transaction.WithGraph(schema)
			}
			return delegate(transaction)
		}
		err error
	)
	defer cancel()
	if s.read != nil {
		err = s.read(readContext, wrapped)
	} else {
		err = graph.ReadForObservation(readContext, wrapped)
	}
	if readContext.Err() != nil {
		return readContext.Err()
	}
	return err
}

func (s *changeTracker) nodeKinds(ctx context.Context, criteria ...graph.Criteria) {
	if !s.lookup(ctx) {
		return
	}
	var err = s.readKinds(ctx, func(transaction graph.Transaction) error {
		var nodeQuery = transaction.Nodes()
		for _, filter := range criteria {
			nodeQuery = nodeQuery.Filter(filter)
		}
		return nodeQuery.Limit(observationRowLimit + 1).FetchKinds(func(cursor graph.Cursor[graph.KindsResult]) error {
			defer cursor.Close()
			for node := range cursor.Chan() {
				if !s.row(ctx) {
					return nil
				}
				s.add(ctx, true, node.Kinds...)
			}
			return cursor.Error()
		})
	})
	if err != nil {
		s.Unknown(ctx)
	}
}

func (s *changeTracker) relationshipKinds(ctx context.Context, criteria ...graph.Criteria) {
	if !s.lookup(ctx) {
		return
	}
	var err = s.readKinds(ctx, func(transaction graph.Transaction) error {
		var relationshipQuery = transaction.Relationships()
		for _, filter := range criteria {
			relationshipQuery = relationshipQuery.Filter(filter)
		}
		return relationshipQuery.Limit(observationRowLimit + 1).FetchKinds(func(cursor graph.Cursor[graph.RelationshipKindsResult]) error {
			defer cursor.Close()
			for relationship := range cursor.Chan() {
				if !s.row(ctx) {
					return nil
				}
				s.add(ctx, false, relationship.Kind)
			}
			return cursor.Error()
		})
	})
	if err != nil {
		s.Unknown(ctx)
	}
}

func (s *changeTracker) node(ctx context.Context, node *graph.Node) {
	if !s.active(ctx) {
		return
	}
	if node == nil {
		s.Unknown(ctx)
		return
	}
	s.nodeKinds(ctx, query.InIDs(query.Node(), node.ID))
	s.add(ctx, true, node.Kinds...)
	s.add(ctx, true, node.AddedKinds...)
	s.add(ctx, true, node.DeletedKinds...)
}

func (s *changeTracker) upsertNode(ctx context.Context, update graph.NodeUpdate) {
	if !s.active(ctx) {
		return
	}
	if update.Node == nil || update.Node.Properties == nil || update.IdentityKind == nil || len(update.IdentityProperties) == 0 {
		s.Unknown(ctx)
		return
	}
	var criteria = []graph.Criteria{query.Kind(query.Node(), update.IdentityKind)}
	for _, property := range update.IdentityProperties {
		criteria = append(criteria, query.Equals(query.NodeProperty(property), update.Node.Properties.Get(property).Any()))
	}
	s.nodeKinds(ctx, query.And(criteria...))
	s.add(ctx, true, update.IdentityKind)
	s.add(ctx, true, update.Node.Kinds...)
	s.add(ctx, true, update.Node.AddedKinds...)
	s.add(ctx, true, update.Node.DeletedKinds...)
}

func (s *changeTracker) WrapTransaction(ctx context.Context, transaction graph.Transaction) graph.Transaction {
	if ctx.Value(observationContextKey{}) == true {
		return transaction
	}
	return &observedTransaction{Transaction: transaction, tracker: s, context: ctx}
}

func (s *changeTracker) WrapBatch(ctx context.Context, batch graph.Batch) graph.Batch {
	if ctx.Value(observationContextKey{}) == true {
		return batch
	}
	var observed = &observedBatch{Batch: batch, tracker: s, context: ctx}
	if creator, ok := batch.(graph.NodeBatchCreator); ok {
		return &observedCreatingBatch{observedBatch: observed, creator: creator}
	}
	return observed
}

type observedTransaction struct {
	graph.Transaction
	tracker *changeTracker
	context context.Context
}

func (s *observedTransaction) WithGraph(schema graph.Graph) graph.Transaction {
	return s.tracker.WrapTransaction(context.WithValue(s.context, observationGraphKey{}, schema), s.Transaction.WithGraph(schema))
}
func (s *observedTransaction) CreateNode(properties *graph.Properties, kinds ...graph.Kind) (*graph.Node, error) {
	s.tracker.add(s.context, true, kinds...)
	return s.Transaction.CreateNode(properties, kinds...)
}
func (s *observedTransaction) UpdateNode(node *graph.Node) error {
	s.tracker.node(s.context, node)
	return s.Transaction.UpdateNode(node)
}
func (s *observedTransaction) CreateRelationshipByIDs(start, end graph.ID, kind graph.Kind, properties *graph.Properties) (*graph.Relationship, error) {
	s.tracker.add(s.context, false, kind)
	return s.Transaction.CreateRelationshipByIDs(start, end, kind, properties)
}
func (s *observedTransaction) UpdateRelationship(relationship *graph.Relationship) error {
	if s.tracker.active(s.context) {
		if relationship == nil {
			s.tracker.Unknown(s.context)
		} else {
			s.tracker.relationshipKinds(s.context, query.InIDs(query.Relationship(), relationship.ID))
			s.tracker.add(s.context, false, relationship.Kind)
		}
	}
	return s.Transaction.UpdateRelationship(relationship)
}
func (s *observedTransaction) Nodes() graph.NodeQuery {
	return &observedNodeQuery{NodeQuery: s.Transaction.Nodes(), tracker: s.tracker, context: s.context}
}
func (s *observedTransaction) Relationships() graph.RelationshipQuery {
	return &observedRelationshipQuery{RelationshipQuery: s.Transaction.Relationships(), tracker: s.tracker, context: s.context}
}
func (s *observedTransaction) Raw(statement string, parameters map[string]any) graph.Result {
	s.tracker.Unknown(s.context)
	return s.Transaction.Raw(statement, parameters)
}
func (s *observedTransaction) Query(statement string, parameters map[string]any) graph.Result {
	s.tracker.Unknown(s.context)
	return s.Transaction.Query(statement, parameters)
}

type observedBatch struct {
	graph.Batch
	tracker *changeTracker
	context context.Context
}

func (s *observedBatch) WithGraph(schema graph.Graph) graph.Batch {
	return s.tracker.WrapBatch(context.WithValue(s.context, observationGraphKey{}, schema), s.Batch.WithGraph(schema))
}
func (s *observedBatch) CreateNode(node *graph.Node) error {
	if node == nil {
		s.tracker.Unknown(s.context)
	} else {
		if node.ID != 0 && node.ID != graph.UnregisteredNodeID {
			s.tracker.node(s.context, node)
		}
		s.tracker.add(s.context, true, node.Kinds...)
	}
	return s.Batch.CreateNode(node)
}
func (s *observedBatch) UpdateNodeBy(update graph.NodeUpdate) error {
	s.tracker.upsertNode(s.context, update)
	return s.Batch.UpdateNodeBy(update)
}
func (s *observedBatch) UpdateNodes(nodes []*graph.Node) error {
	for _, node := range nodes {
		s.tracker.node(s.context, node)
	}
	return s.Batch.UpdateNodes(nodes)
}
func (s *observedBatch) DeleteNode(id graph.ID) error {
	// Cascading deletion also removes incident edges of unknown kinds.
	s.tracker.Unknown(s.context)
	return s.Batch.DeleteNode(id)
}
func (s *observedBatch) CreateRelationship(relationship *graph.Relationship) error {
	if relationship == nil {
		s.tracker.Unknown(s.context)
	} else {
		s.tracker.add(s.context, false, relationship.Kind)
	}
	return s.Batch.CreateRelationship(relationship)
}
func (s *observedBatch) CreateRelationshipByIDs(start, end graph.ID, kind graph.Kind, properties *graph.Properties) error {
	s.tracker.add(s.context, false, kind)
	return s.Batch.CreateRelationshipByIDs(start, end, kind, properties)
}
func (s *observedBatch) UpdateRelationshipBy(update graph.RelationshipUpdate) error {
	s.tracker.upsertNode(s.context, graph.NodeUpdate{Node: update.Start, IdentityKind: update.StartIdentityKind, IdentityProperties: update.StartIdentityProperties})
	s.tracker.upsertNode(s.context, graph.NodeUpdate{Node: update.End, IdentityKind: update.EndIdentityKind, IdentityProperties: update.EndIdentityProperties})
	if update.Relationship == nil {
		s.tracker.Unknown(s.context)
	} else {
		s.tracker.add(s.context, false, update.Relationship.Kind)
	}
	return s.Batch.UpdateRelationshipBy(update)
}
func (s *observedBatch) DeleteRelationship(id graph.ID) error {
	if s.tracker.active(s.context) {
		s.tracker.relationshipKinds(s.context, query.InIDs(query.Relationship(), id))
	}
	return s.Batch.DeleteRelationship(id)
}
func (s *observedBatch) Nodes() graph.NodeQuery {
	return &observedNodeQuery{NodeQuery: s.Batch.Nodes(), tracker: s.tracker, context: s.context}
}
func (s *observedBatch) Relationships() graph.RelationshipQuery {
	return &observedRelationshipQuery{RelationshipQuery: s.Batch.Relationships(), tracker: s.tracker, context: s.context}
}

type observedCreatingBatch struct {
	*observedBatch
	creator graph.NodeBatchCreator
}

func (s *observedCreatingBatch) CreateNodes(nodes []*graph.Node) ([]graph.ID, error) {
	for _, node := range nodes {
		if node == nil {
			s.tracker.Unknown(s.context)
		} else {
			s.tracker.add(s.context, true, node.Kinds...)
		}
	}
	return s.creator.CreateNodes(nodes)
}
