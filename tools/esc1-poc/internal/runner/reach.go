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

package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

type reachVertex struct {
	id        string
	graphID   int64
	kinds     []string
	neighbors *roaring.Bitmap
	reachable *roaring.Bitmap
	seed      bool
}

type reachGraph struct {
	vertices []reachVertex
	byID     map[string]uint32
	graphIDs map[int64]string
	bytes    int64
	limit    int64
}

func (s *reachGraph) reserve(bytes int64) error {
	if bytes > s.limit-s.bytes {
		return fmt.Errorf("reachability cache exceeds the %d-byte calculation budget", s.limit)
	}
	s.bytes += bytes
	return nil
}

func (s *reachGraph) vertex(row map[string]any) (uint32, bool, error) {
	var (
		id, idOK       = row["principal"].(string)
		kinds, kindsOK = stringsFrom(row["kinds"])
		graphID        int64
	)
	switch value := row["graph_id"].(type) {
	case int64:
		graphID = value
	case int:
		graphID = int64(value)
	default:
		return 0, false, fmt.Errorf("reachability graph_id must be an exact integer")
	}
	if !idOK || strings.TrimSpace(id) == "" || !kindsOK {
		return 0, false, fmt.Errorf("reachability requires objectids and node kinds")
	}
	if other, exists := s.graphIDs[graphID]; exists && other != id {
		return 0, false, fmt.Errorf("reachability graph_id identifies different nodes")
	}
	if index, exists := s.byID[id]; exists {
		if s.vertices[index].graphID != graphID {
			return 0, false, fmt.Errorf("reachability objectid identifies different nodes")
		}
		return index, false, nil
	}
	var cost = int64(256 + len(id)*2)
	for _, kind := range kinds {
		cost += int64(32 + len(kind))
	}
	if err := s.reserve(cost); err != nil {
		return 0, false, err
	}
	if len(s.vertices) >= int(^uint32(0)) {
		return 0, false, fmt.Errorf("reachability node index capacity exceeded")
	}
	var index = uint32(len(s.vertices))
	s.byID[id] = index
	s.graphIDs[graphID] = id
	s.vertices = append(s.vertices, reachVertex{id: id, graphID: graphID, kinds: append([]string(nil), kinds...), neighbors: roaring.New()})
	return index, true, nil
}

// The seed query and each frontier query stream their rows. Only the relevant
// component and compressed reachable sets stay in memory; limits fail the
// calculation rather than silently truncating a frontier or a reachable set.
func calculateReach(ctx context.Context, transaction Transaction, query extension.Query, parameters map[string]any, space *answerSpace) (*answerFile, error) {
	var (
		graph    = reachGraph{byID: map[string]uint32{}, graphIDs: map[int64]string{}, limit: space.limits.ParameterBytes}
		frontier []uint32
	)
	if query.Reach == nil {
		return nil, fmt.Errorf("reachability configuration is missing")
	}

	if err := stream(ctx, transaction, query.Text, parameters, func(row map[string]any) error {
		var id, ok = row["id"].(string)
		if len(row) != 3 || !ok || strings.TrimSpace(id) == "" {
			return fmt.Errorf("reachability seed query must return exactly id, graph_id, and kinds")
		}
		var metadata = map[string]any{"principal": id, "graph_id": row["graph_id"], "kinds": row["kinds"]}
		if len(query.Reach.NodeKinds) > 0 && !hasAllowedKind(row["kinds"], query.Reach.NodeKinds) {
			return fmt.Errorf("reachability seed has an excluded node kind")
		}
		var index, created, err = graph.vertex(metadata)
		if err != nil {
			return err
		}
		graph.vertices[index].seed = true
		if created {
			frontier = append(frontier, index)
		}
		return ctx.Err()
	}); err != nil {
		return nil, err
	}

	var traversal = reachTraversalQuery(query.Reach)
	// New vertices are appended exactly once, so converging routes and duplicate
	// edges do not schedule the same frontier repeatedly.
	for len(frontier) > 0 {
		var next []uint32
		for start := 0; start < len(frontier); start += space.limits.BatchRows {
			var end = min(start+space.limits.BatchRows, len(frontier))
			var ids = make([]int64, 0, end-start)
			var active = map[string]bool{}
			for _, index := range frontier[start:end] {
				ids = append(ids, graph.vertices[index].graphID)
				active[graph.vertices[index].id] = true
			}
			if err := stream(ctx, transaction, traversal, map[string]any{"_reach_frontier": ids}, func(row map[string]any) error {
				var source, ok = row["source"].(string)
				if !ok || !active[source] {
					return fmt.Errorf("reachability returned an unexpected frontier source")
				}
				var childRow = map[string]any{"principal": row["target"], "graph_id": row["graph_id"], "kinds": row["kinds"]}
				if len(query.Reach.NodeKinds) > 0 && !hasAllowedKind(row["kinds"], query.Reach.NodeKinds) {
					return fmt.Errorf("reachability returned an excluded node kind")
				}
				var child, created, err = graph.vertex(childRow)
				if err != nil {
					return err
				}
				var parent = graph.byID[source]
				if !graph.vertices[parent].neighbors.Contains(child) {
					if err := graph.reserve(32); err != nil {
						return err
					}
					graph.vertices[parent].neighbors.Add(child)
				}
				if created {
					next = append(next, child)
				}
				return ctx.Err()
			}); err != nil {
				return nil, err
			}
		}
		frontier = next
	}
	if err := graph.calculate(ctx, query.Reach.RejectCycles); err != nil {
		return nil, err
	}
	var writer, err = space.create()
	if err != nil {
		return nil, err
	}
	defer writer.file.Close()
	var order = make([]int, len(graph.vertices))
	for index := range order {
		order[index] = index
	}
	sort.Slice(order, func(left, right int) bool {
		return graph.vertices[order[left]].graphID < graph.vertices[order[right]].graphID
	})
	for _, index := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var vertex = &graph.vertices[index]
		if !query.Reach.ExpandResults && !vertex.seed {
			continue
		}
		var reach = make([]string, 0, vertex.reachable.GetCardinality()+1)
		var iterator = vertex.reachable.Iterator()
		for iterator.HasNext() {
			reach = append(reach, graph.vertices[iterator.Next()].id)
		}
		if query.Reach.IncludeStart {
			reach = append(reach, vertex.id)
		}
		sort.Strings(reach)
		if err := writer.append(map[string]any{"principal": vertex.id, "graph_id": vertex.graphID, "kinds": vertex.kinds, "reach": reach, "reach_size": int64(len(reach))}); err != nil {
			return nil, err
		}
	}
	return writer.finish()
}

func reachTraversalQuery(config *extension.Reachability) string {
	var (
		relationship = "[:" + strings.Join(config.Relationships, "|") + "]"
		pattern      = "(parent)-" + relationship + "->(child)"
		predicates   []string
	)
	if config.Direction == "incoming" {
		pattern = "(parent)<-" + relationship + "-(child)"
	}
	for _, kind := range config.NodeKinds {
		predicates = append(predicates, "child:"+kind)
	}
	var query = "UNWIND $_reach_frontier AS frontier_id MATCH " + pattern + " WHERE id(parent) = frontier_id"
	if len(predicates) > 0 {
		query += " AND (" + strings.Join(predicates, " OR ") + ")"
	}
	return query + " RETURN DISTINCT parent.objectid AS source, child.objectid AS target, id(child) AS graph_id, labels(child) AS kinds"
}

func (s *reachGraph) calculate(ctx context.Context, rejectCycles bool) error {
	var (
		incoming = make([]int, len(s.vertices))
		order    = make([]uint32, 0, len(s.vertices))
	)
	for _, vertex := range s.vertices {
		var iterator = vertex.neighbors.Iterator()
		for iterator.HasNext() {
			incoming[iterator.Next()]++
		}
	}
	for index, count := range incoming {
		if count == 0 {
			order = append(order, uint32(index))
		}
	}
	for position := 0; position < len(order); position++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var iterator = s.vertices[order[position]].neighbors.Iterator()
		for iterator.HasNext() {
			var child = iterator.Next()
			incoming[child]--
			if incoming[child] == 0 {
				order = append(order, child)
			}
		}
	}
	if len(order) != len(s.vertices) {
		if rejectCycles {
			return fmt.Errorf("circular membership or trust in the relevant reachability component; existing results were kept")
		}
		return s.calculateComponents(ctx)
	}
	// Children finish before parents. Each shared subtree's compact set is
	// computed once and reused by every ancestor instead of expanding its routes.
	for position := len(order) - 1; position >= 0; position-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		var vertex = &s.vertices[order[position]]
		vertex.reachable = roaring.New()
		if err := s.reserve(128); err != nil {
			return err
		}
		var iterator = vertex.neighbors.Iterator()
		for iterator.HasNext() {
			if err := ctx.Err(); err != nil {
				return err
			}
			var child = iterator.Next()
			var before = vertex.reachable.GetSizeInBytes()
			vertex.reachable.Or(s.vertices[child].reachable)
			vertex.reachable.Add(child)
			var after = vertex.reachable.GetSizeInBytes()
			if after > before {
				if err := s.reserve(int64(after - before)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Cyclic components share one closure. Forest trust often has reciprocal edges,
// so cycles there are valid; calculations requiring acyclic evidence opt out.
func (s *reachGraph) calculateComponents(ctx context.Context) error {
	type visit struct {
		index    uint32
		finished bool
	}
	var (
		reverse = make([][]uint32, len(s.vertices))
		visited = make([]bool, len(s.vertices))
		finish  []uint32
		stack   []visit
	)
	if err := s.reserve(int64(len(s.vertices)) * 64); err != nil {
		return err
	}
	for index, vertex := range s.vertices {
		if err := ctx.Err(); err != nil {
			return err
		}
		var iterator = vertex.neighbors.Iterator()
		for iterator.HasNext() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.reserve(32); err != nil {
				return err
			}
			var child = iterator.Next()
			reverse[child] = append(reverse[child], uint32(index))
		}
	}
	for root := range s.vertices {
		if visited[root] {
			continue
		}
		stack = append(stack, visit{index: uint32(root)})
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			var next = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if next.finished {
				finish = append(finish, next.index)
				continue
			}
			if visited[next.index] {
				continue
			}
			visited[next.index] = true
			stack = append(stack, visit{index: next.index, finished: true})
			var iterator = s.vertices[next.index].neighbors.Iterator()
			for iterator.HasNext() {
				var child = iterator.Next()
				if !visited[child] {
					stack = append(stack, visit{index: child})
				}
			}
		}
	}
	var componentIDs = make([]int, len(s.vertices))
	for index := range componentIDs {
		componentIDs[index] = -1
	}
	var components []reachVertex
	var members []*roaring.Bitmap
	var pending []uint32
	for position := len(finish) - 1; position >= 0; position-- {
		var root = finish[position]
		if componentIDs[root] >= 0 {
			continue
		}
		var component = len(components)
		components = append(components, reachVertex{neighbors: roaring.New()})
		members = append(members, roaring.New())
		pending = append(pending, root)
		for len(pending) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			var next = pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if componentIDs[next] >= 0 {
				continue
			}
			componentIDs[next] = component
			members[component].Add(next)
			for _, neighbor := range reverse[next] {
				if componentIDs[neighbor] < 0 {
					pending = append(pending, neighbor)
				}
			}
		}
	}
	for index, vertex := range s.vertices {
		var source = componentIDs[index]
		var iterator = vertex.neighbors.Iterator()
		for iterator.HasNext() {
			var target = componentIDs[iterator.Next()]
			if source != target {
				components[source].neighbors.Add(uint32(target))
			}
		}
	}
	var condensed = reachGraph{vertices: components, bytes: s.bytes, limit: s.limit}
	if err := condensed.reserve(int64(len(components)) * 256); err != nil {
		return err
	}
	if err := condensed.calculate(ctx, true); err != nil {
		return err
	}
	s.bytes = condensed.bytes
	for index := range s.vertices {
		if err := ctx.Err(); err != nil {
			return err
		}
		var component = componentIDs[index]
		var reachable = members[component].Clone()
		var iterator = condensed.vertices[component].reachable.Iterator()
		for iterator.HasNext() {
			reachable.Or(members[iterator.Next()])
		}
		reachable.Remove(uint32(index))
		if err := s.reserve(128 + int64(reachable.GetSizeInBytes())); err != nil {
			return err
		}
		s.vertices[index].reachable = reachable
	}
	return nil
}
