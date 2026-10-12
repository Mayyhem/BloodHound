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
package graphdb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func quotedKind(kind string) string { return "`" + strings.ReplaceAll(kind, "`", "``") + "`" }

func (s *transaction) PutNode(ctx context.Context, kind string, properties map[string]any) error {
	var identifier, valid = properties["objectid"].(string)
	if !valid || identifier == "" {
		return fmt.Errorf("helper objectid is required")
	}
	rows, err := s.execute(ctx, "MERGE (node:"+quotedKind(kind)+" {objectid: $identifier}) SET node = $properties RETURN id(node) AS id", map[string]any{"identifier": identifier, "properties": properties})
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("helper identity changed during publication")
	}
	s.endpointIDs = nil
	return nil
}

func (s *transaction) EndpointKinds(ctx context.Context, identifiers []string) (map[string][]string, error) {
	var (
		kinds       = make(map[string][]string, len(identifiers))
		endpointIDs = make(map[string]int64, len(identifiers))
	)
	if len(identifiers) == 0 {
		s.endpointIDs = endpointIDs
		return kinds, nil
	}
	if len(s.database.endpointKinds) == 0 {
		return nil, fmt.Errorf("prepare indexed endpoint kinds before endpoint publication")
	}
	statements := make([]string, len(s.database.endpointKinds))
	for index, kind := range s.database.endpointKinds {
		statements[index] = "MATCH (node:" + quotedKind(kind) + ") USING INDEX node:" + quotedKind(kind) + "(objectid) WHERE node.objectid IN $identifiers RETURN node.objectid AS objectid, id(node) AS id, labels(node) AS kinds"
	}
	rows, err := s.Read(ctx, strings.Join(statements, " UNION "), map[string]any{"identifiers": identifiers})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		identifier, identifierValid := row["objectid"].(string)
		nodeID, idValid := row["id"].(int64)
		labels, labelsValid := row["kinds"].([]any)
		if !identifierValid || !idValid || !labelsValid {
			return nil, fmt.Errorf("database returned invalid endpoint metadata")
		}
		if _, found := endpointIDs[identifier]; found {
			return nil, fmt.Errorf("relationship endpoint %q identifies multiple nodes", identifier)
		}
		endpointIDs[identifier] = nodeID
		for _, label := range labels {
			kind, valid := label.(string)
			if !valid {
				return nil, fmt.Errorf("database returned an invalid endpoint kind")
			}
			kinds[identifier] = append(kinds[identifier], kind)
		}
	}
	for _, identifier := range identifiers {
		if _, found := endpointIDs[identifier]; !found {
			return nil, fmt.Errorf("relationship endpoint %q is missing", identifier)
		}
	}
	// Retain only this bounded batch, rather than all endpoints in a large run.
	s.endpointIDs = endpointIDs
	return kinds, nil
}

func (s *transaction) CreateRelationships(ctx context.Context, kind, owner string, relationships []runner.Relationship) (int, error) {
	var (
		identifiers = make([]string, 0, 2*len(relationships))
		creations   = make([]map[string]any, len(relationships))
		missing     bool
	)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(relationships) == 0 {
		return 0, nil
	}
	for index, relationship := range relationships {
		properties, err := relationshipCreationProperties(owner, relationship.Properties)
		if err != nil {
			return 0, err
		}
		creations[index] = map[string]any{"properties": properties}
		identifiers = append(identifiers, relationship.Source, relationship.Target)
		if _, found := s.endpointIDs[relationship.Source]; !found {
			missing = true
		}
		if _, found := s.endpointIDs[relationship.Target]; !found {
			missing = true
		}
	}
	if missing {
		if _, err := s.EndpointKinds(ctx, identifiers); err != nil {
			return 0, err
		}
	}
	for index, relationship := range relationships {
		creations[index]["source"] = s.endpointIDs[relationship.Source]
		creations[index]["target"] = s.endpointIDs[relationship.Target]
	}
	rows, err := s.execute(ctx, "UNWIND $rows AS row MATCH (source), (target) WHERE id(source) = row.source AND id(target) = row.target CREATE (source)-[edge:"+quotedKind(kind)+"]->(target) SET edge = row.properties RETURN id(edge) AS id", map[string]any{"rows": creations})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *transaction) UpdateRelationships(ctx context.Context, kind, owner string, updates []runner.RelationshipUpdate) (int, error) {
	var patches = make([]map[string]any, len(updates))
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := validateRelationshipUpdates(owner, updates); err != nil {
		return 0, err
	}
	if len(updates) == 0 {
		return 0, nil
	}
	for index, update := range updates {
		properties := update.Properties
		if properties == nil {
			properties = map[string]any{}
		}
		patches[index] = map[string]any{"id": update.ID, "properties": properties}
	}
	rows, err := s.execute(ctx, "UNWIND $rows AS row MATCH ()-[edge:"+quotedKind(kind)+"]->() WHERE id(edge) = row.id AND edge.poc_owner = $owner SET edge += row.properties RETURN id(edge) AS id", map[string]any{"rows": patches, "owner": owner})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *transaction) DeleteRelationships(ctx context.Context, kind, owner string, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	rows, err := s.execute(ctx, "UNWIND $ids AS identifier MATCH ()-[edge:"+quotedKind(kind)+"]->() WHERE id(edge) = identifier AND edge.poc_owner = $owner WITH edge, id(edge) AS old_id DELETE edge RETURN old_id", map[string]any{"ids": ids, "owner": owner})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// PrepareEndpoints installs objectid indexes for the plan's declared endpoint
// kinds before graph publication starts. Existing matching indexes are reused.
func (s *Database) PrepareEndpoints(ctx context.Context, kinds []string) error {
	var (
		identifier  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
		seen        = make(map[string]bool, len(kinds))
		prepared    []string
		session     = s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.name, AccessMode: neo4j.AccessModeWrite})
		waitSeconds = int64(15 * 60)
	)
	defer session.Close(ctx)
	for _, kind := range kinds {
		if !identifier.MatchString(kind) {
			return fmt.Errorf("invalid endpoint kind")
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		digest := sha256.Sum256([]byte(kind))
		indexName := fmt.Sprintf("poc_endpoint_%x", digest[:8])
		result, err := session.Run(ctx, "CREATE INDEX "+indexName+" IF NOT EXISTS FOR (node:"+quotedKind(kind)+") ON (node.objectid)", nil)
		if err != nil {
			return fmt.Errorf("prepare objectid index for %s: %w", kind, err)
		}
		if _, err = result.Consume(ctx); err != nil {
			return fmt.Errorf("prepare objectid index for %s: %w", kind, err)
		}
		prepared = append(prepared, kind)
	}
	if len(prepared) > 0 {
		if deadline, found := ctx.Deadline(); found {
			remaining := int64(time.Until(deadline).Seconds())
			if remaining < waitSeconds {
				waitSeconds = remaining
			}
		}
		if waitSeconds <= 0 {
			return fmt.Errorf("endpoint index preparation deadline expired")
		}
		result, err := session.Run(ctx, "CALL db.awaitIndexes($seconds)", map[string]any{"seconds": waitSeconds})
		if err != nil {
			return fmt.Errorf("wait for endpoint objectid indexes: %w", err)
		}
		if _, err = result.Consume(ctx); err != nil {
			return fmt.Errorf("wait for endpoint objectid indexes: %w", err)
		}
	}
	s.endpointKinds = prepared
	return nil
}
