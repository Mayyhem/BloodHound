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

type overlapMember struct {
	id            string
	graphID       int64
	group         bool
	special       bool
	specialMember bool
	reach         *roaring.Bitmap
}

// groupOverlap shares one compact membership index across all seed batches.
// The extension supplies the meaning of group and special flags; this helper
// contains no graph kinds, SID rules, or certificate-specific predicates.
type groupOverlap struct {
	members []overlapMember
	byID    map[string]uint32
	limits  Limits
	bytes   int64
}

func (s *groupOverlap) reserve(bytes int64) error {
	if bytes > s.limits.ParameterBytes-s.bytes {
		return fmt.Errorf("group overlap cache exceeds the %d-byte parameter budget", s.limits.ParameterBytes)
	}
	s.bytes += bytes
	return nil
}

func overlapInteger(value any) (int64, bool) {
	switch value := value.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}

// newGroupOverlap consumes the configured membership parameter. Removing it
// from parameters keeps a potentially large lookup table out of the seed SQL;
// only the compact local index remains for subsequent batches.
func newGroupOverlap(ctx context.Context, query extension.Query, parameters map[string]any, limits Limits) (*groupOverlap, error) {
	var (
		result   = &groupOverlap{byID: map[string]uint32{}, limits: limits}
		reachIDs = map[string][]string{}
		graphIDs = map[int64]string{}
	)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if query.Overlap == nil {
		return nil, fmt.Errorf("group overlap configuration is missing")
	}
	parameter := query.Overlap.Memberships
	rows, valid := parameters[parameter].([]map[string]any)
	if !valid {
		return nil, fmt.Errorf("group overlap requires the %s membership table", parameter)
	}
	for name, value := range parameters {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if name != parameter {
			if err := result.reserve(encodedSize(value)); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var (
			id, idOK                       = row["principal"].(string)
			graphID, graphIDOK             = overlapInteger(row["graph_id"])
			group, groupOK                 = row["is_group"].(bool)
			special, specialOK             = row["is_special"].(bool)
			specialMember, specialMemberOK = row["has_special_member"].(bool)
			reach, reachOK                 = stringsFrom(row["reach"])
			reachSize, reachSizeOK         = overlapInteger(row["reach_size"])
		)
		if !idOK || strings.TrimSpace(id) == "" || !graphIDOK || !groupOK || !specialOK || !specialMemberOK || !reachOK || !reachSizeOK || reachSize != int64(len(reach)) {
			return nil, fmt.Errorf("group overlap membership requires principal, integer graph_id/reach_size, boolean flags, and a complete reach list")
		}
		if _, exists := reachIDs[id]; exists {
			return nil, fmt.Errorf("group overlap principal %s is repeated", id)
		}
		if other, exists := graphIDs[graphID]; exists && other != id {
			return nil, fmt.Errorf("group overlap graph_id identifies different principals")
		}
		if err := result.reserve(int64(320 + len(id)*2)); err != nil {
			return nil, err
		}
		reachIDs[id] = reach
		graphIDs[graphID] = id
		result.members = append(result.members, overlapMember{id: id, graphID: graphID, group: group, special: special, specialMember: specialMember, reach: roaring.New()})
	}
	if uint64(len(result.members)) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("group overlap principal index capacity exceeded")
	}
	// Dense indexes follow graph IDs, matching native bitmap iteration order.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result.members, func(left, right int) bool { return result.members[left].graphID < result.members[right].graphID })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for index, member := range result.members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result.byID[member.id] = uint32(index)
	}
	for index := range result.members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		member := &result.members[index]
		for _, id := range reachIDs[member.id] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			child, exists := result.byID[id]
			if !exists {
				return nil, fmt.Errorf("group overlap reach contains missing principal %s", id)
			}
			if id == member.id || member.reach.Contains(child) {
				return nil, fmt.Errorf("group overlap reach contains its own principal or duplicates")
			}
			member.reach.Add(child)
		}
		if err := result.reserve(int64(member.reach.GetSizeInBytes())); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	delete(parameters, parameter)
	return result, nil
}

func (s *groupOverlap) direct(ctx context.Context, value any) (*roaring.Bitmap, error) {
	var (
		values, valid = stringsFrom(value)
		result        = roaring.New()
	)
	if !valid {
		return nil, fmt.Errorf("group overlap left/right columns must be lists of principal IDs")
	}
	for _, id := range values {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index, exists := s.byID[id]
		if !exists {
			return nil, fmt.Errorf("group overlap seed contains missing principal %s", id)
		}
		result.Add(index)
	}
	return result, nil
}

func (s *groupOverlap) excluded(ctx context.Context, direct *roaring.Bitmap) (bool, error) {
	var iterator = direct.Iterator()
	for iterator.HasNext() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var member = s.members[iterator.Next()]
		if member.group && (member.special || member.specialMember) {
			return true, nil
		}
	}
	return false, nil
}

func (s *groupOverlap) intersect(ctx context.Context, left, right *roaring.Bitmap) (*roaring.Bitmap, error) {
	var (
		check      = right.Clone()
		remaining  = roaring.New()
		results    = roaring.New()
		candidates []uint32
	)
	leftExcluded, err := s.excluded(ctx, left)
	if err != nil {
		return nil, err
	}
	rightExcluded, err := s.excluded(ctx, right)
	if err != nil {
		return nil, err
	}
	if leftExcluded && rightExcluded {
		results.Or(left)
		results.Or(right)
		return results, nil
	}
	if leftExcluded {
		return right.Clone(), nil
	}
	if rightExcluded {
		return left.Clone(), nil
	}
	iterator := right.Iterator()
	for iterator.HasNext() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		member := s.members[iterator.Next()]
		if member.group {
			check.Or(member.reach)
		}
	}
	iterator = left.Iterator()
	for iterator.HasNext() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index := iterator.Next()
		if check.Contains(index) {
			results.Add(index)
		} else {
			remaining.Or(s.members[index].reach)
			// OrReach removes the current root after unioning its members,
			// including a copy introduced by an earlier direct principal.
			remaining.Remove(index)
		}
	}
	iterator = remaining.Iterator()
	for iterator.HasNext() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index := iterator.Next()
		if !s.members[index].reach.IsEmpty() {
			candidates = append(candidates, index)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftSize, rightSize := s.members[candidates[left]].reach.GetCardinality(), s.members[candidates[right]].reach.GetCardinality()
		if leftSize == rightSize {
			return s.members[candidates[left]].graphID < s.members[candidates[right]].graphID
		}
		return leftSize < rightSize
	})
	for _, index := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !remaining.Contains(index) {
			continue
		}
		remaining.Remove(index)
		if check.Contains(index) {
			results.Add(index)
			// Removal never toggles membership back on when groups overlap.
			// Results already selected remain selected, even if removed here.
			remaining.AndNot(s.members[index].reach)
		}
	}
	remaining.And(check)
	results.Or(remaining)
	return results, nil
}

func (s *groupOverlap) emit(ctx context.Context, row map[string]any, consume func(map[string]any) error) error {
	var (
		target, targetOK = row["target"].(string)
		output           *roaring.Bitmap
	)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(row) != 3 || !targetOK || strings.TrimSpace(target) == "" {
		return fmt.Errorf("group overlap seed must return only left, right, and a nonempty target")
	}
	if encodedSize(row) > s.limits.RowBytes {
		return fmt.Errorf("group overlap seed exceeds the %d-byte row limit", s.limits.RowBytes)
	}
	left, err := s.direct(ctx, row["left"])
	if err != nil {
		return err
	}
	right, err := s.direct(ctx, row["right"])
	if err != nil {
		return err
	}
	// The cache is immutable across batches. Account for worst-case temporary
	// sets and candidate indexes before doing any expansion for this seed.
	if int64(len(s.members))*20+s.bytes > s.limits.ParameterBytes {
		return fmt.Errorf("group overlap working sets exceed the parameter budget")
	}
	output, err = s.intersect(ctx, left, right)
	if err != nil {
		return err
	}
	iterator := output.Iterator()
	for iterator.HasNext() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consume(map[string]any{"source": s.members[iterator.Next()].id, "target": target}); err != nil {
			return err
		}
	}
	return nil
}
