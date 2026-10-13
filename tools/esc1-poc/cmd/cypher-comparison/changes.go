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
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/specterops/dawgs/graph"
)

// Only this command's generated graph may be reset. The dedicated database
// and this second ownership check protect the saved test fixtures.
func changeDataset(ctx context.Context, database graph.Database, action string) error {
	var unknown int64
	if err := database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
		rows := transaction.Query("MATCH (n) WHERE NOT coalesce(n.comparison_fixture, false) AND NOT n:PoC_DomainHelper AND NOT n:PoC_Run RETURN count(n)", nil)
		defer rows.Close()
		if !rows.Next() {
			return fmt.Errorf("cannot inspect graph ownership")
		}
		if err := rows.Scan(&unknown); err != nil {
			return err
		}
		return rows.Error()
	}); err != nil {
		return err
	}
	if unknown != 0 {
		return fmt.Errorf("refusing change: %d objects do not belong to this comparison", unknown)
	}
	execute := func(statement string) error {
		return database.WriteTransaction(ctx, func(transaction graph.Transaction) error {
			rows := transaction.Query(statement, nil)
			defer rows.Close()
			for rows.Next() {
			}
			return rows.Error()
		})
	}
	switch action {
	case "clear":
		// Select a small, explicit list before deleting. PostgreSQL currently
		// loses WITH ... LIMIT when it directly precedes DELETE, which would
		// otherwise turn this cleanup into one enormous transaction.
		for {
			var identifiers []int64
			if err := database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
				rows := transaction.Query("MATCH (n) RETURN id(n) AS identifier LIMIT 2000", nil)
				defer rows.Close()
				for rows.Next() {
					var identifier int64
					if err := rows.Scan(&identifier); err != nil {
						return err
					}
					identifiers = append(identifiers, identifier)
				}
				return rows.Error()
			}); err != nil {
				return err
			}
			if len(identifiers) == 0 {
				return nil
			}
			if err := database.WriteTransaction(ctx, func(transaction graph.Transaction) error {
				rows := transaction.Query("MATCH (n) WHERE id(n) IN $identifiers DETACH DELETE n", map[string]any{"identifiers": identifiers})
				defer rows.Close()
				for rows.Next() {
				}
				return rows.Error()
			}); err != nil {
				return err
			}
		}
	case "reset":
		for _, statement := range []string{
			"MATCH ()-[r:PoC_ADCSESC1]->() DELETE r",
			"MATCH (n) WHERE n:PoC_DomainHelper OR n:PoC_Run DETACH DELETE n",
			"MATCH (t:CertTemplate {objectid:'Comparison_Template'}) SET t.authorizedsignatures = 0",
			"MATCH (:CertTemplate)-[r:PublishedTo]->(:EnterpriseCA) DELETE r",
			"MATCH (t:CertTemplate {objectid:'Comparison_Template'}), (ca:EnterpriseCA) CREATE (t)-[:PublishedTo]->(ca)",
		} {
			if err := execute(statement); err != nil {
				return err
			}
		}
		return nil
	case "change":
		return execute("MATCH (t:CertTemplate {objectid:'Comparison_Template'}) SET t.authorizedsignatures = 1")
	case "remove-one":
		return execute("MATCH (:CertTemplate)-[r:PublishedTo]->(:EnterpriseCA {objectid:'Comparison_CA_000000000'}) DELETE r")
	case "remove-last":
		return execute("MATCH (:CertTemplate)-[r:PublishedTo]->(:EnterpriseCA) DELETE r")
	default:
		return fmt.Errorf("unknown graph change %q", action)
	}
}

// Every saved connection and its complete properties are hashed, together with
// every helper. The run marker has a separate hash: its timestamp and source
// hash deliberately differ between successful versions, but must not change
// after failure. Maps use Go's stable JSON key order.
func hashSavedOutputs(ctx context.Context, database graph.Database, result *measurement) error {
	var (
		outputs  = sha256.New()
		marker   = sha256.New()
		identity = sha256.New()
	)
	var content io.Writer = outputs
	if path := os.Getenv("COMPARISON_ANSWERS"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		content = io.MultiWriter(outputs, file)
	}
	for _, check := range []struct {
		statement string
		run       bool
	}{
		{"MATCH (s)-[r:PoC_ADCSESC1]->(d) RETURN s.objectid AS from_object, d.objectid AS to_object, properties(r) AS details, id(r) AS saved_id ORDER BY from_object, to_object", false},
		{"MATCH (n:PoC_DomainHelper) RETURN n.objectid AS helper_id, properties(n) AS details, id(n) AS saved_id ORDER BY helper_id", false},
		{"MATCH (n:PoC_Run) RETURN n.objectid AS run_id, properties(n) AS details, id(n) AS saved_id ORDER BY run_id", true},
	} {
		if err := database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
			rows := transaction.Query(check.statement, nil)
			defer rows.Close()
			for rows.Next() {
				fields := rows.Values()
				if len(fields) < 3 {
					return fmt.Errorf("incomplete saved-output row")
				}
				values := fields[:len(fields)-1]
				savedID := fields[len(fields)-1]

				digest := content
				if check.run {
					digest = marker
				}
				if err := json.NewEncoder(identity).Encode(append([]any{savedID}, values...)); err != nil {
					return err
				}
				if err := json.NewEncoder(digest).Encode(values); err != nil {
					return err
				}
			}
			return rows.Error()
		}); err != nil {
			return err
		}
	}
	result.IdentityHash = hex.EncodeToString(identity.Sum(nil))
	result.SavedHash = hex.EncodeToString(outputs.Sum(nil))
	result.RunRecordHash = hex.EncodeToString(marker.Sum(nil))
	return nil
}
