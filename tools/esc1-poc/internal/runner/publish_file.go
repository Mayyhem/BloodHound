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

package runner

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// Publisher is the small set of graph changes the runner permits. PostgreSQL
// uses DAWGS's structured operations; Neo4j uses parameterized Cypher. Neither
// adapter may commit a batch: the coordinator commits the entire run once.
type EndpointReader interface {
	EndpointKinds(context.Context, []string) (map[string][]string, error)
}

type Publisher interface {
	PutNode(context.Context, string, map[string]any) error
	CreateRelationships(context.Context, string, []Relationship) (int, error)
	UpdateRelationships(context.Context, string, []RelationshipUpdate) (int, error)
	DeleteRelationships(context.Context, string, []int64) (int, error)
}

// Relationship is one requested connection and its ordinary properties.
type Relationship struct {
	Source     string
	Target     string
	Properties map[string]any
}

// RelationshipUpdate changes only the supplied keys on a connection.
// A nil value removes that key. Unmentioned properties stay as they were.
type RelationshipUpdate struct {
	ID         int64
	Properties map[string]any
}

func validateFileEndpoints(ctx context.Context, transaction Transaction, binding extension.RelationshipBinding, file *answerFile) error {
	return file.batches(ctx, 0, func(rows Rows) error {
		pairs := make([]Pair, len(rows))
		for index, row := range rows {
			pairs[index] = pairFromRow(row)
		}
		return validateEndpoints(ctx, transaction, binding, pairs)
	})
}

type relationshipChanges struct {
	binding   extension.RelationshipBinding
	additions *answerFile
	removals  *answerFile
	updates   *answerFile
	unchanged int
}

// prepareChanges compares two sorted files by advancing through them once.
// It never keeps the complete current or desired relationship set in memory.
func prepareChanges(ctx context.Context, transaction Transaction, binding extension.RelationshipBinding, desired *answerFile) (relationshipChanges, error) {
	var (
		result = relationshipChanges{binding: binding}
		space  = desired.space
		policy = binding.EffectiveExistingEdgePolicy()
	)
	if policy != extension.ReplaceExisting && policy != extension.MergeProperties && policy != extension.KeepExisting {
		return result, fmt.Errorf("%s has an unknown existing_edge_policy %q", binding.Kind, policy)
	}
	existingWriter, err := space.create()
	if err != nil {
		return result, err
	}
	defer existingWriter.file.Close()
	// Managed kinds have already passed the PoC_ identifier check. Use that
	// name directly so this read works with both query implementations.
	query := "MATCH (source)-[edge:" + binding.Kind + "]->(target)"
	if len(binding.Endpoints) > 0 {
		var pairs []string
		for _, endpoint := range binding.Endpoints {
			pairs = append(pairs, "(source:"+endpoint.SourceKind+" AND target:"+endpoint.TargetKind+")")
		}
		query += " WHERE " + strings.Join(pairs, " OR ")
	}
	query += " RETURN id(edge) AS id, source.objectid AS source, target.objectid AS target"
	if policy == extension.MergeProperties {
		query += ", properties(edge) AS properties"
	}
	if err := streamWithTimeout(ctx, transaction, space.limits.QueryTimeout, query, nil, func(row map[string]any) error {
		if _, err := readPairs(Rows{{"source": row["source"], "target": row["target"]}}); err != nil {
			return err
		}
		if _, ok := row["id"].(int64); !ok {
			return fmt.Errorf("database returned an invalid relationship identity")
		}
		if policy == extension.MergeProperties {
			if properties, ok := row["properties"].(map[string]any); !ok || properties == nil {
				return fmt.Errorf("database returned invalid existing relationship properties")
			}
		}
		return existingWriter.append(row)
	}); err != nil {
		return result, err
	}
	existing, err := existingWriter.finish()
	if err != nil {
		return result, err
	}
	existing, err = sortAnswers(ctx, existing, "source", "target")
	if err != nil {
		return result, err
	}
	currentReader, err := existing.open()
	if err != nil {
		return result, err
	}
	defer currentReader.file.Close()
	desiredReader, err := desired.open()
	if err != nil {
		return result, err
	}
	defer desiredReader.file.Close()
	additions, err := space.create()
	if err != nil {
		return result, err
	}
	defer additions.file.Close()
	removals, err := space.create()
	if err != nil {
		return result, err
	}
	defer removals.file.Close()
	updates, err := space.create()
	if err != nil {
		return result, err
	}
	defer updates.file.Close()
	var previous Pair
	nextCurrent := func() (map[string]any, error) {
		row, err := currentReader.next()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		pair := pairFromRow(row)
		if pair == previous {
			return nil, fmt.Errorf("%s contains duplicate managed relationships; inspect them before running again", binding.Kind)
		}
		previous = pair
		return row, nil
	}
	nextDesired := func() (map[string]any, error) {
		row, err := desiredReader.next()
		if err == io.EOF {
			return nil, nil
		}
		return row, err
	}
	current, err := nextCurrent()
	if err != nil {
		return result, err
	}
	wanted, err := nextDesired()
	if err != nil {
		return result, err
	}
	for current != nil || wanted != nil {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		switch {
		case current == nil || wanted != nil && pairLess(pairFromRow(wanted), pairFromRow(current)):
			if err := additions.append(wanted); err != nil {
				return result, err
			}
			wanted, err = nextDesired()
		case wanted == nil || pairLess(pairFromRow(current), pairFromRow(wanted)):
			if err := removals.append(map[string]any{"id": current["id"]}); err != nil {
				return result, err
			}
			current, err = nextCurrent()
		default:
			switch policy {
			case extension.ReplaceExisting:
				// Replacement is intentional even when properties match. Both
				// operations remain inside the same final transaction.
				if err := removals.append(map[string]any{"id": current["id"]}); err != nil {
					return result, err
				}
				if err := additions.append(wanted); err != nil {
					return result, err
				}
			case extension.MergeProperties:
				patch := changedProperties(current["properties"].(map[string]any), wanted["properties"].(map[string]any))
				if len(patch) == 0 {
					result.unchanged++
				} else if err := updates.append(map[string]any{"id": current["id"], "properties": patch}); err != nil {
					return result, err
				}
			case extension.KeepExisting:
				result.unchanged++
			}
			current, err = nextCurrent()
			if err == nil {
				wanted, err = nextDesired()
			}
		}
		if err != nil {
			return result, err
		}
		if additions.answer.rows+removals.answer.rows+updates.answer.rows > space.limits.ChangedObjects {
			return result, fmt.Errorf("relationship changes exceed the %d-change transaction budget", space.limits.ChangedObjects)
		}
	}
	result.additions, err = additions.finish()
	if err != nil {
		return result, err
	}
	result.removals, err = removals.finish()
	if err != nil {
		return result, err
	}
	result.updates, err = updates.finish()
	if err != nil {
		return result, err
	}
	if err := existing.remove(); err != nil {
		return result, err
	}
	return result, nil
}

func pairLess(left, right Pair) bool {
	return left.Source < right.Source || left.Source == right.Source && left.Target < right.Target
}

func applyChanges(ctx context.Context, transaction Transaction, changes relationshipChanges) error {
	var (
		publisher, batched = transaction.(Publisher)
		// Leave room for the database IDs added by the adapters.
		extraBytesPerRow = int64(128)
	)
	create := func(rows Rows) error {
		relationships := make([]Relationship, len(rows))
		for index, row := range rows {
			pair := pairFromRow(row)
			relationships[index] = Relationship{Source: pair.Source, Target: pair.Target, Properties: creationProperties(row["properties"].(map[string]any))}
		}
		if batched {
			count, err := publisher.CreateRelationships(ctx, changes.binding.Kind, relationships)
			if err != nil {
				return err
			}
			if count != len(relationships) {
				return fmt.Errorf("endpoint identities changed during publication; rolling back")
			}
		} else {
			// The basic interface remains useful for tiny in-memory tests.
			for _, relationship := range relationships {
				rows, err := transaction.Write(ctx, "MATCH (source {objectid: $source}), (target {objectid: $target}) CREATE (source)-[edge:`"+changes.binding.Kind+"`]->(target) SET edge = $properties RETURN id(edge) AS id", map[string]any{"source": relationship.Source, "target": relationship.Target, "properties": relationship.Properties})
				if err != nil {
					return err
				}
				if len(rows) != 1 {
					return fmt.Errorf("endpoint identities changed during publication; rolling back")
				}
			}
		}
		return nil
	}
	remove := func(rows Rows) error {
		identifiers := make([]int64, len(rows))
		for index, row := range rows {
			identifiers[index] = row["id"].(int64)
		}
		if batched {
			count, err := publisher.DeleteRelationships(ctx, changes.binding.Kind, identifiers)
			if err != nil {
				return err
			}
			if count != len(identifiers) {
				return fmt.Errorf("relationship identity changed during publication; rolling back")
			}
		} else {
			for _, identifier := range identifiers {
				rows, err := transaction.Write(ctx, "MATCH ()-[edge:`"+changes.binding.Kind+"`]->() WHERE id(edge) = $id WITH edge, id(edge) AS old_id DELETE edge RETURN old_id", map[string]any{"id": identifier})
				if err != nil {
					return err
				}
				if len(rows) != 1 {
					return fmt.Errorf("relationship identity changed during publication; rolling back")
				}
			}
		}
		return nil
	}
	update := func(rows Rows) error {
		updates := make([]RelationshipUpdate, len(rows))
		for index, row := range rows {
			updates[index] = RelationshipUpdate{ID: row["id"].(int64), Properties: row["properties"].(map[string]any)}
		}
		if batched {
			count, err := publisher.UpdateRelationships(ctx, changes.binding.Kind, updates)
			if err != nil {
				return err
			}
			if count != len(updates) {
				return fmt.Errorf("relationship identity changed during property update; rolling back")
			}
		} else {
			for _, update := range updates {
				rows, err := transaction.Write(ctx, "MATCH ()-[edge:`"+changes.binding.Kind+"`]->() WHERE id(edge) = $id SET edge += $properties RETURN id(edge) AS id", map[string]any{"id": update.ID, "properties": update.Properties})
				if err != nil {
					return err
				}
				if len(rows) != 1 {
					return fmt.Errorf("relationship identity changed during property update; rolling back")
				}
			}
		}
		return nil
	}
	// PostgreSQL allows one connection of each kind between the same endpoints.
	// Remove replacements first, then recreate them. No intermediate state is
	// committed, so a failure restores the old IDs and all their properties.
	if err := changes.removals.batches(ctx, extraBytesPerRow, remove); err != nil {
		return err
	}
	if err := changes.updates.batches(ctx, extraBytesPerRow, update); err != nil {
		return err
	}
	return changes.additions.batches(ctx, extraBytesPerRow, create)
}

func compareFiles(ctx context.Context, transaction Transaction, desired *answerFile) (Comparison, error) {
	var comparison = Comparison{Missing: []Pair{}, Extra: []Pair{}}
	var sampleBytes = maxReportSampleBytes
	writer, err := desired.space.create()
	if err != nil {
		return comparison, err
	}
	defer writer.file.Close()
	if err := streamWithTimeout(ctx, transaction, desired.space.limits.QueryTimeout, "MATCH (source)-[:ADCSESC1]->(target) RETURN DISTINCT source.objectid AS source, target.objectid AS target", nil, writer.append); err != nil {
		return comparison, err
	}
	raw, err := writer.finish()
	if err != nil {
		return comparison, err
	}
	expected, err := checkedPairs(ctx, raw)
	if err != nil {
		return comparison, err
	}
	if err := raw.remove(); err != nil {
		return comparison, err
	}
	expectedReader, err := expected.open()
	if err != nil {
		return comparison, err
	}
	defer expectedReader.file.Close()
	actualReader, err := desired.open()
	if err != nil {
		return comparison, err
	}
	defer actualReader.file.Close()
	next := func(reader *answerReader) (map[string]any, error) {
		row, err := reader.next()
		if err == io.EOF {
			return nil, nil
		}
		return row, err
	}
	left, err := next(expectedReader)
	if err != nil {
		return comparison, err
	}
	right, err := next(actualReader)
	if err != nil {
		return comparison, err
	}
	for left != nil || right != nil {
		if err := ctx.Err(); err != nil {
			return comparison, err
		}
		switch {
		case right == nil || left != nil && pairLess(pairFromRow(left), pairFromRow(right)):
			comparison.MissingCount++
			appendReportPair(&comparison.Missing, pairFromRow(left), desired.space.limits.ReportPairs, &sampleBytes)
			left, err = next(expectedReader)
		case left == nil || pairLess(pairFromRow(right), pairFromRow(left)):
			comparison.ExtraCount++
			appendReportPair(&comparison.Extra, pairFromRow(right), desired.space.limits.ReportPairs, &sampleBytes)
			right, err = next(actualReader)
		default:
			left, err = next(expectedReader)
			if err == nil {
				right, err = next(actualReader)
			}
		}
		if err != nil {
			return comparison, err
		}
	}
	comparison.Truncated = comparison.MissingCount > int64(len(comparison.Missing)) || comparison.ExtraCount > int64(len(comparison.Extra))
	if err := expected.remove(); err != nil {
		return comparison, err
	}
	return comparison, nil
}
