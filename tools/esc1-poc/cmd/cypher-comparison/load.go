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
	"fmt"
	"math/bits"
	"time"

	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/ops"
)

type datasetSettings struct {
	Objects     int
	Members     int
	Authorities int
	Shape       string
}

type datasetReport struct {
	Objects                int           `json:"objects"`
	Relationships          int64         `json:"relationships"`
	MembershipObjects      int           `json:"membership_objects"`
	MembershipPairs        int64         `json:"membership_pairs"`
	TemplateAuthorityPairs int           `json:"template_authority_pairs"`
	RelevantShare          float64       `json:"relevant_share"`
	DirectoryUsers         int           `json:"directory_users"`
	VaultAccounts          int           `json:"vault_accounts"`
	Shape                  string        `json:"answer_shape"`
	CreationOrderHash      string        `json:"creation_order_sha256"`
	Elapsed                time.Duration `json:"load_nanoseconds"`
}

// The generator makes one connected domain/vault graph. Its membership graph
// has twenty nested groups, four overlapping branches and a shared large group.
// Extra users remain in the same domain but have no certificate permission.
// Vault accounts link to directory users by the same SID field as the example.
// This is a direct database load, not a test of collection-file ingestion.
func loadDataset(ctx context.Context, database graph.Database, settings datasetSettings) (*datasetReport, error) {
	const groupCount = 25
	if settings.Objects < 1 || settings.Objects > 100_000_000 || settings.Members < 1 || settings.Members > settings.Objects || settings.Authorities < 1 || settings.Authorities > settings.Objects {
		return nil, fmt.Errorf("object counts must be positive, at most 100 million, and members/authorities cannot exceed the total")
	}
	var (
		started = time.Now()
		report  = &datasetReport{Objects: settings.Objects, MembershipObjects: settings.Members + groupCount, TemplateAuthorityPairs: settings.Authorities, Shape: settings.Shape}
		// Domain, host, root authority, authentication store, template, vault.
		fixedCount     = 6 + settings.Authorities + groupCount
		userCount      = settings.Members + (settings.Objects-fixedCount-settings.Members)/2
		accountCount   = settings.Objects - fixedCount - userCount
		identifiers    = make([]graph.ID, 0, settings.Objects)
		creationOrder  = sha256.New()
		groupAncestors [groupCount]uint64
		pendingNodes   []*graph.Node
		pendingKinds   string
	)
	defer func() { report.Elapsed = time.Since(started) }()
	if settings.Members < 1 || settings.Authorities < 1 || settings.Objects < fixedCount+settings.Members+2 || settings.Objects > 100_000_000 || (settings.Shape != "users" && settings.Shape != "groups") {
		return report, fmt.Errorf("choose 1–100 million total objects with room for the chosen members, authorities and fixed graph; answer shape is users or groups")
	}
	count, err := ops.CountNodes(ctx, database)
	if err != nil {
		return report, err
	}
	if count != 0 {
		return report, fmt.Errorf("refusing load: comparison graph contains %d objects", count)
	}
	report.DirectoryUsers, report.VaultAccounts = userCount, accountCount
	report.RelevantShare = float64(settings.Members+groupCount+5+settings.Authorities) / float64(settings.Objects)

	flushNodes := func() error {
		if len(pendingNodes) == 0 {
			return nil
		}
		err := database.BatchOperation(ctx, func(batch graph.Batch) error {
			creator, ok := batch.(graph.NodeBatchCreator)
			if !ok {
				return fmt.Errorf("database lacks bulk node creation")
			}
			created, err := creator.CreateNodes(pendingNodes)
			if err != nil {
				return err
			}
			for index, id := range created {
				// Preserve the observed creation order, since native group
				// choices can depend on the database's object numbers.
				fmt.Fprintf(creationOrder, "%s=%d\n", pendingNodes[index].Properties.Get("objectid").Any(), id)
			}
			identifiers = append(identifiers, created...)
			return nil
		})
		pendingNodes = nil
		return err
	}
	addNode := func(kinds []string, properties map[string]any) error {
		key := fmt.Sprint(kinds)
		if key != pendingKinds || len(pendingNodes) == 2000 {
			if err := flushNodes(); err != nil {
				return err
			}
		}
		pendingKinds = key
		properties["comparison_fixture"] = true
		pendingNodes = append(pendingNodes, graph.PrepareNode(graph.AsProperties(properties), graph.StringsToKinds(kinds)...))
		return nil
	}
	for _, node := range []struct {
		kinds      []string
		properties map[string]any
	}{
		{[]string{"Base", "Domain"}, map[string]any{"objectid": "Comparison_Domain", "name": "COMPARISON DOMAIN"}},
		{[]string{"Base", "Computer"}, map[string]any{"objectid": "Comparison_Host", "name": "CA HOST", "domainsid": "Comparison_Domain", "enabled": true}},
		{[]string{"Base", "RootCA"}, map[string]any{"objectid": "Comparison_Root", "name": "ROOT"}},
		{[]string{"Base", "NTAuthStore"}, map[string]any{"objectid": "Comparison_Store", "name": "AUTHENTICATION STORE"}},
		{[]string{"Base", "CertTemplate"}, map[string]any{"objectid": "Comparison_Template", "name": "ESC1 TEMPLATE", "requiresmanagerapproval": false, "authenticationenabled": true, "enrolleesuppliessubject": true, "schemaversion": 2, "authorizedsignatures": 0}},
		{[]string{"PoC_Vault_Base", "PoC_Vault_Container"}, map[string]any{"objectid": "Comparison_Vault", "name": "SHARED VAULT"}},
	} {
		if err := addNode(node.kinds, node.properties); err != nil {
			return report, err
		}
	}
	authorityStart := 6
	for index := 0; index < settings.Authorities; index++ {
		if err := addNode([]string{"Base", "EnterpriseCA"}, map[string]any{"objectid": fmt.Sprintf("Comparison_CA_%09d", index), "name": "AUTHORITY", "domainsid": "Comparison_Domain"}); err != nil {
			return report, err
		}
	}
	groupStart := 6 + settings.Authorities
	for index := 0; index < groupCount; index++ {
		kind := "Group"
		if index == 4 {
			kind = "ADLocalGroup"
		}
		if err := addNode([]string{"Base", kind}, map[string]any{"objectid": fmt.Sprintf("Comparison_Group_%09d", index), "name": "MEMBERSHIP GROUP"}); err != nil {
			return report, err
		}
	}
	userStart := groupStart + groupCount
	for index := 0; index < userCount; index++ {
		if err := addNode([]string{"Base", "User"}, map[string]any{"objectid": fmt.Sprintf("Comparison_User_%09d", index), "name": fmt.Sprintf("USER %09d", index), "domainsid": "Comparison_Domain", "enabled": true}); err != nil {
			return report, err
		}
	}
	accountStart := userStart + userCount
	for index := 0; index < accountCount; index++ {
		if err := addNode([]string{"PoC_Vault_Base", "PoC_Vault_Account"}, map[string]any{"objectid": fmt.Sprintf("Comparison_Account_%09d", index), "name": fmt.Sprintf("ACCOUNT %09d", index), "user_sid": fmt.Sprintf("Comparison_User_%09d", index%userCount)}); err != nil {
			return report, err
		}
	}
	if err := flushNodes(); err != nil {
		return report, err
	}

	// Edges are committed in bounded loading batches. A failed load is marked
	// as incomplete; no calculation may be measured until loading and counts
	// have both succeeded. The experiment retains the original database copy.
	type connection struct {
		from, to int
		kind     string
	}
	var pendingEdges []connection
	flushEdges := func() error {
		if len(pendingEdges) == 0 {
			return nil
		}
		err := database.BatchOperation(ctx, func(batch graph.Batch) error {
			for _, edge := range pendingEdges {
				if err := batch.CreateRelationshipByIDs(identifiers[edge.from], identifiers[edge.to], graph.StringKind(edge.kind), graph.NewProperties()); err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			report.Relationships += int64(len(pendingEdges))
		}
		pendingEdges = nil
		return err
	}
	addEdge := func(from, to int, kind string) error {
		pendingEdges = append(pendingEdges, connection{from, to, kind})
		if len(pendingEdges) == 2000 {
			return flushEdges()
		}
		return nil
	}
	for _, edge := range []connection{{2, 0, "RootCAFor"}, {3, 0, "NTAuthStoreFor"}, {groupStart, 4, "Enroll"}, {0, 1, "Contains"}, {5, 0, "PoC_ComparisonLink"}} {
		if err := addEdge(edge.from, edge.to, edge.kind); err != nil {
			return report, err
		}
	}
	for index := 0; index < settings.Authorities; index++ {
		authority := authorityStart + index
		for _, edge := range []connection{{1, authority, "HostsCAService"}, {authority, 2, "EnterpriseCAFor"}, {authority, 3, "TrustedForNTAuth"}, {4, authority, "PublishedTo"}} {
			if err := addEdge(edge.from, edge.to, edge.kind); err != nil {
				return report, err
			}
		}
		if settings.Shape == "users" {
			for member := 0; member < settings.Members; member++ {
				if err := addEdge(userStart+member, authority, "Enroll"); err != nil {
					return report, err
				}
			}
		} else if err := addEdge(groupStart+1, authority, "Enroll"); err != nil {
			return report, err
		}
	}
	// Membership flows from a member towards its parent group. Build ancestor
	// masks first to count the exact complete membership pairs without retaining
	// the much larger table of all those pairs in the loader.
	groupAncestors[1], groupAncestors[2], groupAncestors[3] = 1, 1, 1
	groupAncestors[4] = 1 | (1 << 3)
	for index := 24; index >= 5; index-- {
		parent := index + 1
		if index == 24 {
			parent = 1
		}
		groupAncestors[index] = groupAncestors[parent] | (1 << parent)
	}
	for index := 1; index < groupCount; index++ {
		parent := 0
		if index >= 5 {
			parent = index + 1
			if index == 24 {
				parent = 1
			}
		}
		if err := addEdge(groupStart+index, groupStart+parent, "MemberOf"); err != nil {
			return report, err
		}
		if index == 4 {
			if err := addEdge(groupStart+index, groupStart+3, "MemberOf"); err != nil {
				return report, err
			}
		}
		report.MembershipPairs += int64(bits.OnesCount64(groupAncestors[index]))
	}
	for member := 0; member < userCount; member++ {
		if err := addEdge(0, userStart+member, "Contains"); err != nil {
			return report, err
		}
		if member >= settings.Members {
			continue
		}
		parents := []int{1 + member%4}
		if member%10 == 0 {
			parents = append(parents, 1+(member+1)%4)
		}
		if member == 0 {
			parents = append(parents, 5)
		}
		var ancestors uint64
		for _, parent := range parents {
			kind := "MemberOf"
			if parent == 4 {
				kind = "MemberOfLocalGroup"
			}
			if err := addEdge(userStart+member, groupStart+parent, kind); err != nil {
				return report, err
			}
			ancestors |= groupAncestors[parent] | (1 << parent)
		}
		report.MembershipPairs += int64(bits.OnesCount64(ancestors))
	}
	for account := 0; account < accountCount; account++ {
		if err := addEdge(5, accountStart+account, "PoC_Vault_ContainsAccount"); err != nil {
			return report, err
		}
	}
	if err := flushEdges(); err != nil {
		return report, err
	}
	actual, err := ops.CountNodes(ctx, database)
	if err != nil {
		return report, err
	}
	if actual != int64(settings.Objects) {
		return report, fmt.Errorf("loaded %d objects; expected %d", actual, settings.Objects)
	}
	var actualEdges int64
	if err := database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
		rows := transaction.Query("MATCH ()-[r]->() RETURN count(r)", nil)
		defer rows.Close()
		if !rows.Next() {
			return fmt.Errorf("cannot count loaded connections")
		}
		if err := rows.Scan(&actualEdges); err != nil {
			return err
		}
		return rows.Error()
	}); err != nil {
		return report, err
	}
	if actualEdges != report.Relationships {
		return report, fmt.Errorf("loaded %d connections; expected %d", actualEdges, report.Relationships)
	}
	report.CreationOrderHash = hex.EncodeToString(creationOrder.Sum(nil))
	return report, nil
}
