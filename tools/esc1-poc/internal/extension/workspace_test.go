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

package extension_test

import (
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/stretchr/testify/require"
)

func TestExecutionDependenciesExplainEveryWait(t *testing.T) {
	var (
		plan = extension.Plan{Schema: extension.Schema{
			Nodes: []extension.NodeBinding{{Query: "PoC_A/helper.cypher", Kind: "PoC_A_Helper"}},
			Relationships: []extension.RelationshipBinding{
				{Query: "PoC_A/edges.cypher", Kind: "PoC_A_Edge"},
				{Query: "PoC_B/edges.cypher", Kind: "PoC_A_Edge"},
			},
		}}
		query = extension.Query{Name: "PoC_C/read.cypher", Dependencies: []extension.Dependency{
			{Type: "query", Name: "PoC_A/helper.cypher", Parameter: "helpers", Conditions: ">=v1.0.0"},
			{Type: "node", Name: "PoC_A_Helper"},
			{Type: "relationship", Name: "PoC_A_Edge"},
			{Type: "node", Name: "User"},
		}}
	)
	require.Equal(t, []string{"PoC_A/edges.cypher", "PoC_A/helper.cypher", "PoC_B/edges.cypher"}, plan.Prerequisites(query))
	require.Equal(t, []extension.ExecutionDependency{
		{Prerequisite: "PoC_A/helper.cypher", Consumer: query.Name, Type: "query", Name: "PoC_A/helper.cypher", Parameter: "helpers", Conditions: ">=v1.0.0"},
		{Prerequisite: "PoC_A/helper.cypher", Consumer: query.Name, Type: "node", Name: "PoC_A_Helper"},
		{Prerequisite: "PoC_A/edges.cypher", Consumer: query.Name, Type: "relationship", Name: "PoC_A_Edge"},
		{Prerequisite: "PoC_B/edges.cypher", Consumer: query.Name, Type: "relationship", Name: "PoC_A_Edge"},
	}, plan.ExecutionDependencies(query))
	// A producer reads the previous saved answer of its own relationship type.
	query.Name = "PoC_A/edges.cypher"
	query.Dependencies = []extension.Dependency{{Type: "relationship", Name: "PoC_A_Edge"}}
	require.Empty(t, plan.ExecutionDependencies(query))
	require.Empty(t, plan.Prerequisites(query))
}
