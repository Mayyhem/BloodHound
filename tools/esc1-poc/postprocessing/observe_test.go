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
	"errors"
	"reflect"
	"testing"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

type observationCursor[T any] struct{ values chan T }

func (s *observationCursor[T]) Chan() chan T { return s.values }
func (s *observationCursor[T]) Close()       {}
func (s *observationCursor[T]) Error() error { return nil }
func observationValues[T any](values ...T) *observationCursor[T] {
	var cursor = &observationCursor[T]{values: make(chan T, len(values))}
	for _, value := range values {
		cursor.values <- value
	}
	close(cursor.values)
	return cursor
}

type observationNodeQuery struct {
	graph.NodeQuery
	rows    []graph.KindsResult
	failure error
	filters int
	updates int
	deletes int
	queries int
	fetches int
	limit   int
}

func (s *observationNodeQuery) Filter(graph.Criteria) graph.NodeQuery { s.filters++; return s }
func (s *observationNodeQuery) Filterf(provider graph.CriteriaProvider) graph.NodeQuery {
	return s.Filter(provider())
}
func (s *observationNodeQuery) OrderBy(...graph.Criteria) graph.NodeQuery { return s }
func (s *observationNodeQuery) Offset(int) graph.NodeQuery                { return s }
func (s *observationNodeQuery) Limit(limit int) graph.NodeQuery           { s.limit = limit; return s }
func (s *observationNodeQuery) FetchKinds(delegate func(graph.Cursor[graph.KindsResult]) error) error {
	if s.failure != nil {
		return s.failure
	}
	return delegate(observationValues(s.rows...))
}
func (s *observationNodeQuery) Update(*graph.Properties) error { s.updates++; return s.failure }
func (s *observationNodeQuery) Delete() error                  { s.deletes++; return s.failure }
func (s *observationNodeQuery) Query(func(graph.Result) error, ...graph.Criteria) error {
	s.queries++
	return s.failure
}
func (s *observationNodeQuery) Fetch(func(graph.Cursor[*graph.Node]) error, ...graph.Criteria) error {
	s.fetches++
	return s.failure
}

type observationRelationshipQuery struct {
	graph.RelationshipQuery
	rows    []graph.RelationshipKindsResult
	failure error
	deletes int
	updates int
	queries int
}

func (s *observationRelationshipQuery) Filter(graph.Criteria) graph.RelationshipQuery { return s }
func (s *observationRelationshipQuery) Filterf(provider graph.CriteriaProvider) graph.RelationshipQuery {
	return s.Filter(provider())
}
func (s *observationRelationshipQuery) Limit(int) graph.RelationshipQuery                 { return s }
func (s *observationRelationshipQuery) Offset(int) graph.RelationshipQuery                { return s }
func (s *observationRelationshipQuery) OrderBy(...graph.Criteria) graph.RelationshipQuery { return s }
func (s *observationRelationshipQuery) FetchKinds(delegate func(graph.Cursor[graph.RelationshipKindsResult]) error) error {
	if s.failure != nil {
		return s.failure
	}
	return delegate(observationValues(s.rows...))
}
func (s *observationRelationshipQuery) Delete() error                  { s.deletes++; return s.failure }
func (s *observationRelationshipQuery) Update(*graph.Properties) error { s.updates++; return s.failure }
func (s *observationRelationshipQuery) Query(func(graph.Result) error, ...graph.Criteria) error {
	s.queries++
	return s.failure
}

type observationTransaction struct {
	graph.Transaction
	nodes         *observationNodeQuery
	relationships *observationRelationshipQuery
	failure       error
	updates       int
	creates       int
}

func (s *observationTransaction) Nodes() graph.NodeQuery                  { return s.nodes }
func (s *observationTransaction) Relationships() graph.RelationshipQuery  { return s.relationships }
func (s *observationTransaction) WithGraph(graph.Graph) graph.Transaction { return s }
func (s *observationTransaction) UpdateNode(*graph.Node) error            { s.updates++; return s.failure }
func (s *observationTransaction) CreateNode(properties *graph.Properties, kinds ...graph.Kind) (*graph.Node, error) {
	s.creates++
	return graph.NewNode(1, properties, kinds...), s.failure
}
func (s *observationTransaction) Raw(string, map[string]any) graph.Result {
	return graph.NewErrorResult(s.failure)
}
func (s *observationTransaction) Query(string, map[string]any) graph.Result {
	return graph.NewErrorResult(s.failure)
}
func (s *observationTransaction) CreateRelationshipByIDs(start, end graph.ID, kind graph.Kind, properties *graph.Properties) (*graph.Relationship, error) {
	s.creates++
	return &graph.Relationship{StartID: start, EndID: end, Kind: kind, Properties: properties}, s.failure
}
func (s *observationTransaction) UpdateRelationship(*graph.Relationship) error {
	s.updates++
	return s.failure
}

type observationBatch struct {
	graph.Batch
	deletes       int
	upserts       int
	nodeDeletes   int
	failure       error
	nodes         *observationNodeQuery
	relationships *observationRelationshipQuery
	creates       int
	updates       int
}

func (s *observationBatch) WithGraph(graph.Graph) graph.Batch   { return s }
func (s *observationBatch) DeleteRelationship(graph.ID) error   { s.deletes++; return s.failure }
func (s *observationBatch) DeleteNode(graph.ID) error           { s.nodeDeletes++; return s.failure }
func (s *observationBatch) UpdateNodeBy(graph.NodeUpdate) error { s.upserts++; return s.failure }
func (s *observationBatch) UpdateRelationshipBy(graph.RelationshipUpdate) error {
	s.upserts++
	return s.failure
}
func (s *observationBatch) Nodes() graph.NodeQuery                 { return s.nodes }
func (s *observationBatch) Relationships() graph.RelationshipQuery { return s.relationships }
func (s *observationBatch) CreateNode(*graph.Node) error           { s.creates++; return s.failure }
func (s *observationBatch) CreateRelationship(*graph.Relationship) error {
	s.creates++
	return s.failure
}
func (s *observationBatch) CreateRelationshipByIDs(graph.ID, graph.ID, graph.Kind, *graph.Properties) error {
	s.creates++
	return s.failure
}
func (s *observationBatch) UpdateNodes([]*graph.Node) error { s.updates++; return s.failure }

type observationCreatingBatch struct{ *observationBatch }

func (s *observationCreatingBatch) WithGraph(graph.Graph) graph.Batch { return s }

func (s *observationCreatingBatch) CreateNodes(nodes []*graph.Node) ([]graph.ID, error) {
	var ids = make([]graph.ID, len(nodes))
	for index := range nodes {
		ids[index] = graph.ID(index + 1)
	}
	return ids, s.failure
}

func observationTracker(nodes *observationNodeQuery, relationships *observationRelationshipQuery) *changeTracker {
	var tracker = &changeTracker{read: func(ctx context.Context, delegate graph.TransactionDelegate) error {
		if _, ok := ctx.Deadline(); !ok {
			return errors.New("metadata reader must have a deadline")
		}
		return delegate(&observationTransaction{nodes: nodes, relationships: relationships})
	}}
	tracker.Reset()
	return tracker
}

func TestChangeTrackerRequiresPublishedBaselineAndSuppressesExtension(t *testing.T) {
	var (
		tracker     changeTracker
		ctx         = context.Background()
		transaction = &observationTransaction{}
	)
	if tracker.Snapshot().CoverageKnown {
		t.Fatal("startup must require a complete run")
	}
	tracker.Reset()
	tracker.Unknown(withoutChangeObservation(ctx))
	if tracker.WrapTransaction(withoutChangeObservation(ctx), transaction) != transaction {
		t.Fatal("extension transaction was decorated")
	}
	if !tracker.Snapshot().CoverageKnown {
		t.Fatal("extension work invalidated native coverage")
	}
	tracker.Unknown(ctx)
	if tracker.Snapshot().CoverageKnown {
		t.Fatal("unknown writer must invalidate coverage")
	}
}

func TestNodeMutationIncludesExistingAddedAndRemovedKindsDespiteWriteFailure(t *testing.T) {
	var (
		ctx         = context.Background()
		tracker     = observationTracker(&observationNodeQuery{rows: []graph.KindsResult{{ID: 1, Kinds: graph.Kinds{graph.StringKind("Existing"), graph.StringKind("CoLabel")}}}}, nil)
		failure     = errors.New("native write failed after partial work")
		transaction = &observationTransaction{failure: failure}
		node        = graph.NewNode(1, graph.NewProperties(), graph.StringKind("Added"))
	)
	node.AddedKinds = graph.Kinds{graph.StringKind("Added")}
	node.DeletedKinds = graph.Kinds{graph.StringKind("Removed")}
	if err := tracker.WrapTransaction(ctx, transaction).WithGraph(graph.Graph{}).UpdateNode(node); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var changes = tracker.Snapshot()
	if !changes.CoverageKnown || !reflect.DeepEqual(changes.Nodes, []string{"Added", "CoLabel", "Existing", "Removed"}) {
		t.Fatalf("incomplete mutation coverage: %+v", changes)
	}
	if transaction.updates != 1 {
		t.Fatal("native write was not delegated")
	}
}

func TestUpsertObservesExistingCoLabelsAndRelationshipEndpointNodes(t *testing.T) {
	var (
		ctx     = context.Background()
		tracker = observationTracker(&observationNodeQuery{rows: []graph.KindsResult{{ID: 1, Kinds: graph.Kinds{graph.StringKind("CoLabel")}}}}, nil)
		batch   = &observationBatch{}
		start   = graph.NewNode(1, graph.AsProperties(map[string]any{"objectid": "start"}), graph.StringKind("Start"))
		end     = graph.NewNode(2, graph.AsProperties(map[string]any{"objectid": "end"}), graph.StringKind("End"))
	)
	if err := tracker.WrapBatch(ctx, batch).UpdateRelationshipBy(graph.RelationshipUpdate{
		Start: start, End: end, StartIdentityKind: graph.StringKind("Entity"), EndIdentityKind: graph.StringKind("Entity"),
		StartIdentityProperties: []string{"objectid"}, EndIdentityProperties: []string{"objectid"},
		Relationship: &graph.Relationship{Kind: graph.StringKind("Edge")},
	}); err != nil {
		t.Fatal(err)
	}
	var changes = tracker.Snapshot()
	if !changes.CoverageKnown || !reflect.DeepEqual(changes.Nodes, []string{"CoLabel", "End", "Entity", "Start"}) || !reflect.DeepEqual(changes.Relationships, []string{"Edge"}) {
		t.Fatalf("endpoint changes were lost: %+v", changes)
	}
}

func TestIDRelationshipDeletionTracksKindAndNodeDeletionFallsBack(t *testing.T) {
	var (
		ctx      = context.Background()
		tracker  = observationTracker(nil, &observationRelationshipQuery{rows: []graph.RelationshipKindsResult{{Kind: graph.StringKind("RemovedEdge")}}})
		batch    = &observationBatch{}
		observed = tracker.WrapBatch(ctx, batch).WithGraph(graph.Graph{})
	)
	if err := observed.DeleteRelationship(7); err != nil {
		t.Fatal(err)
	}
	var changes = tracker.Snapshot()
	if !changes.CoverageKnown || !reflect.DeepEqual(changes.Relationships, []string{"RemovedEdge"}) {
		t.Fatalf("deletion not covered: %+v", changes)
	}
	if err := observed.DeleteNode(1); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || batch.nodeDeletes != 1 || batch.deletes != 1 {
		t.Fatal("cascading deletion must safely fall back")
	}
}

func TestMetadataFailureNeverChangesNativeWriteResult(t *testing.T) {
	var (
		ctx          = context.Background()
		probeFailure = errors.New("metadata read failed")
		tracker      = observationTracker(&observationNodeQuery{failure: probeFailure}, nil)
		transaction  = &observationTransaction{}
	)
	if err := tracker.WrapTransaction(ctx, transaction).UpdateNode(graph.NewNode(1, graph.NewProperties())); err != nil {
		t.Fatalf("metadata failure escaped to native writer: %v", err)
	}
	if tracker.Snapshot().CoverageKnown || transaction.updates != 1 {
		t.Fatal("metadata failure must select all rules and still perform native write")
	}
}

func TestFluentMutationRebuildsProbeAndEvaluatesFilterOnce(t *testing.T) {
	var (
		ctx         = context.Background()
		probe       = &observationNodeQuery{rows: []graph.KindsResult{{Kinds: graph.Kinds{graph.StringKind("Matched")}}}}
		writer      = &observationNodeQuery{}
		tracker     = observationTracker(probe, nil)
		transaction = &observationTransaction{nodes: writer}
		filters     int
	)
	if err := tracker.WrapTransaction(ctx, transaction).Nodes().Filterf(func() graph.Criteria {
		filters++
		return query.InIDs(query.Node(), 1)
	}).OrderBy(query.NodeID()).Offset(0).Limit(1).Update(graph.NewProperties()); err != nil {
		t.Fatal(err)
	}
	if filters != 1 || writer.filters != 1 || writer.limit != 1 || writer.updates != 1 || probe.filters != 1 || probe.limit != observationRowLimit+1 {
		t.Fatal("wrapper escaped, filter was repeated, or native query was changed")
	}
	if !reflect.DeepEqual(tracker.Snapshot().Nodes, []string{"Matched"}) {
		t.Fatal("fluent mutation was not observed")
	}
}

func TestOptionalBulkCreationSurvivesWithGraph(t *testing.T) {
	var (
		ctx      = context.Background()
		tracker  = observationTracker(nil, nil)
		batch    = &observationCreatingBatch{observationBatch: &observationBatch{}}
		observed = tracker.WrapBatch(ctx, batch).WithGraph(graph.Graph{})
	)
	if _, ok := observed.(graph.NodeBatchCreator); !ok {
		t.Fatal("bulk creation capability was lost through WithGraph")
	}
	if _, ok := tracker.WrapBatch(ctx, batch.observationBatch).(graph.NodeBatchCreator); ok {
		t.Fatal("invented bulk capability on a batch that does not support it")
	}
	var creator, ok = tracker.WrapBatch(ctx, batch).(graph.NodeBatchCreator)
	if !ok {
		t.Fatal("bulk node creation capability was hidden")
	}
	var ids, err = creator.CreateNodes([]*graph.Node{graph.PrepareNode(graph.NewProperties(), graph.StringKind("Bulk"))})
	if err != nil || !reflect.DeepEqual(ids, []graph.ID{1}) || !reflect.DeepEqual(tracker.Snapshot().Nodes, []string{"Bulk"}) {
		t.Fatal("bulk creation result or observation changed")
	}
}

func TestObservationBudgetFallsBackWithoutStoppingWrites(t *testing.T) {
	var (
		ctx         = context.Background()
		reads       int
		tracker     changeTracker
		transaction = &observationTransaction{}
	)
	tracker.Reset()
	tracker.read = func(context.Context, graph.TransactionDelegate) error { reads++; return nil }
	for index := 0; index < observationLookupLimit+10; index++ {
		if err := tracker.WrapTransaction(ctx, transaction).UpdateNode(graph.NewNode(1, graph.NewProperties())); err != nil {
			t.Fatal(err)
		}
	}
	if reads != observationLookupLimit || tracker.Snapshot().CoverageKnown || transaction.updates != observationLookupLimit+10 {
		t.Fatal("metadata work was unbounded or native writes were stopped")
	}
}

func TestKindsCreatedEarlierInTransactionSurviveIndependentReadVisibility(t *testing.T) {
	var (
		ctx         = context.Background()
		tracker     = observationTracker(&observationNodeQuery{}, nil)
		transaction = &observationTransaction{}
		observed    = tracker.WrapTransaction(ctx, transaction)
	)
	// The independent reader sees no row yet, but the earlier creation's kind
	// remains accumulated when the native writer later updates that new row.
	if _, err := observed.CreateNode(graph.NewProperties(), graph.StringKind("Uncommitted")); err != nil {
		t.Fatal(err)
	}
	if err := observed.UpdateNode(graph.NewNode(1, graph.NewProperties())); err != nil {
		t.Fatal(err)
	}
	var changes = tracker.Snapshot()
	if !changes.CoverageKnown || !reflect.DeepEqual(changes.Nodes, []string{"Uncommitted"}) {
		t.Fatalf("uncommitted kind was lost: %+v", changes)
	}
}

func TestRowBudgetAndRawQueriesFallBack(t *testing.T) {
	var (
		ctx         = context.Background()
		rows        = make([]graph.KindsResult, observationRowLimit+1)
		tracker     = observationTracker(&observationNodeQuery{rows: rows}, nil)
		transaction = &observationTransaction{nodes: &observationNodeQuery{}}
	)
	if err := tracker.WrapTransaction(ctx, transaction).Nodes().Update(graph.NewProperties()); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || transaction.nodes.updates != 1 {
		t.Fatal("large mutation was not safely delegated with full fallback")
	}
	tracker.Reset()
	tracker.WrapTransaction(ctx, transaction).Raw("opaque query", nil)
	if tracker.Snapshot().CoverageKnown {
		t.Fatal("raw query must invalidate coverage")
	}
}

func TestRelationshipWritesAndBatchNodeWritesKeepCoverage(t *testing.T) {
	var (
		ctx           = context.Background()
		tracker       = observationTracker(&observationNodeQuery{rows: []graph.KindsResult{{Kinds: graph.Kinds{graph.StringKind("OldNode")}}}}, &observationRelationshipQuery{rows: []graph.RelationshipKindsResult{{Kind: graph.StringKind("OldEdge")}}})
		transaction   = &observationTransaction{}
		batch         = &observationBatch{}
		observed      = tracker.WrapTransaction(ctx, transaction)
		observedBatch = tracker.WrapBatch(ctx, batch)
		relationship  = &graph.Relationship{ID: 9, Kind: graph.StringKind("NewEdge")}
		node          = graph.NewNode(1, graph.NewProperties(), graph.StringKind("NewNode"))
	)
	if _, err := observed.CreateRelationshipByIDs(1, 2, relationship.Kind, graph.NewProperties()); err != nil {
		t.Fatal(err)
	}
	if err := observed.UpdateRelationship(relationship); err != nil {
		t.Fatal(err)
	}
	if err := observedBatch.CreateNode(node); err != nil {
		t.Fatal(err)
	}
	if err := observedBatch.CreateNode(graph.PrepareNode(graph.NewProperties(), graph.StringKind("Prepared"))); err != nil {
		t.Fatal(err)
	}
	if err := observedBatch.UpdateNodes([]*graph.Node{node}); err != nil {
		t.Fatal(err)
	}
	if err := observedBatch.CreateRelationship(relationship); err != nil {
		t.Fatal(err)
	}
	if err := observedBatch.CreateRelationshipByIDs(1, 2, graph.StringKind("BatchEdge"), graph.NewProperties()); err != nil {
		t.Fatal(err)
	}
	var changes = tracker.Snapshot()
	if !changes.CoverageKnown || !reflect.DeepEqual(changes.Nodes, []string{"NewNode", "OldNode", "Prepared"}) || !reflect.DeepEqual(changes.Relationships, []string{"BatchEdge", "NewEdge", "OldEdge"}) {
		t.Fatalf("write coverage missing: %+v", changes)
	}
	if batch.creates != 4 || batch.updates != 1 || transaction.creates != 1 || transaction.updates != 1 {
		t.Fatal("a wrapped write was not forwarded")
	}
}

func TestBatchFluentRelationshipWritesAndNodeQueries(t *testing.T) {
	var (
		ctx      = context.Background()
		tracker  = observationTracker(&observationNodeQuery{rows: []graph.KindsResult{{Kinds: graph.Kinds{graph.StringKind("Node")}}}}, &observationRelationshipQuery{rows: []graph.RelationshipKindsResult{{Kind: graph.StringKind("Edge")}}})
		writer   = &observationRelationshipQuery{}
		batch    = &observationBatch{relationships: writer, nodes: &observationNodeQuery{}}
		observed = tracker.WrapBatch(ctx, batch)
		filters  int
	)
	var relationships = observed.Relationships().Filterf(func() graph.Criteria { filters++; return query.InIDs(query.Relationship(), 1) }).OrderBy(query.RelationshipID()).Offset(0).Limit(1)
	if err := relationships.Update(graph.NewProperties()); err != nil {
		t.Fatal(err)
	}
	if err := relationships.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := observed.Nodes().Filter(query.InIDs(query.Node(), 1)).Update(graph.NewProperties()); err != nil {
		t.Fatal(err)
	}
	var changes = tracker.Snapshot()
	if filters != 1 || writer.deletes != 1 || writer.updates != 1 || !changes.CoverageKnown || !reflect.DeepEqual(changes.Nodes, []string{"Node"}) || !reflect.DeepEqual(changes.Relationships, []string{"Edge"}) {
		t.Fatal("fluent batch writer escaped observation")
	}
	tracker.Reset()
	if err := relationships.Query(nil); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || writer.queries != 1 {
		t.Fatal("unclassified query did not fall back and forward")
	}
}

func TestUnclassifiedTransactionAndNodeQueryForwarding(t *testing.T) {
	var (
		ctx         = context.Background()
		tracker     = observationTracker(nil, nil)
		writer      = &observationNodeQuery{}
		transaction = &observationTransaction{nodes: writer}
		observed    = tracker.WrapTransaction(ctx, transaction)
	)
	observed.Query("opaque", nil)
	if tracker.Snapshot().CoverageKnown {
		t.Fatal("transaction query was treated as read only")
	}
	tracker.Reset()
	if err := observed.Nodes().Query(nil); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || writer.queries != 1 {
		t.Fatal("node query was not forwarded conservatively")
	}
	tracker.Reset()
	if err := observed.Nodes().Fetch(nil); err != nil {
		t.Fatal(err)
	}
	if !tracker.Snapshot().CoverageKnown {
		t.Fatal("plain fetch invalidated coverage")
	}
	if err := observed.Nodes().Fetch(nil, query.AddKinds(query.Node(), graph.Kinds{graph.StringKind("Added")})); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || writer.fetches != 2 {
		t.Fatal("write-capable fetch was not forwarded conservatively")
	}
	tracker.Reset()
	if err := observed.Nodes().Delete(); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || writer.deletes != 1 {
		t.Fatal("node deletion did not cover cascading changes")
	}
}

func TestTimedOutMetadataReaderCannotClaimCompleteCoverage(t *testing.T) {
	var (
		ctx         = context.Background()
		tracker     changeTracker
		transaction = &observationTransaction{}
	)
	tracker.Reset()
	tracker.read = func(readContext context.Context, _ graph.TransactionDelegate) error {
		<-readContext.Done()
		// A reader that silently ends on cancellation still has incomplete
		// coverage. Its child timeout must not cancel the native writer.
		return nil
	}
	if err := tracker.WrapTransaction(ctx, transaction).UpdateNode(graph.NewNode(1, graph.NewProperties())); err != nil {
		t.Fatal(err)
	}
	if tracker.Snapshot().CoverageKnown || transaction.updates != 1 || ctx.Err() != nil {
		t.Fatal("timed-out observation claimed coverage or affected native work")
	}
}
