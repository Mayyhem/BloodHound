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

func TestSelectPrerequisitesKeepsOnlySelectedBranches(t *testing.T) {
	var plan = extension.Plan{
		Queries: []extension.Query{
			{Name: "seed.cypher"},
			{Name: "target.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "seed.cypher"}}},
			{Name: "other.cypher"},
			{Name: "consumer.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "target.cypher"}}},
		},
	}

	actual, err := plan.SelectPrerequisites([]string{"target.cypher"})
	require.NoError(t, err)
	require.Equal(t, []string{"seed.cypher", "target.cypher"}, actual)
}

func TestSelectPrerequisitesMergesTargetsAndSharedPrerequisites(t *testing.T) {
	var plan = extension.Plan{Queries: []extension.Query{
		{Name: "base.cypher"},
		{Name: "left.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "base.cypher"}}},
		{Name: "right.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "base.cypher"}}},
		{Name: "unrelated.cypher"},
	}}

	actual, err := plan.SelectPrerequisites([]string{"left.cypher", "right.cypher"})
	require.NoError(t, err)
	require.Equal(t, []string{"base.cypher", "left.cypher", "right.cypher"}, actual)
}

func TestSelectPrerequisitesIncludesEveryGraphProducerAndItsPrerequisites(t *testing.T) {
	var plan = extension.Plan{
		Queries: []extension.Query{
			{Name: "base.cypher"},
			{Name: "first.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "base.cypher"}}},
			{Name: "second.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "base.cypher"}}},
			{Name: "reader.cypher", Dependencies: []extension.Dependency{{Type: "relationship", Name: "PoC_Shared"}}},
			{Name: "other.cypher"},
		},
		Schema: extension.Schema{Relationships: []extension.RelationshipBinding{
			{Query: "first.cypher", Kind: "PoC_Shared"},
			{Query: "second.cypher", Kind: "PoC_Shared"},
		}},
	}

	actual, err := plan.SelectPrerequisites([]string{"reader.cypher"})
	require.NoError(t, err)
	require.Equal(t, []string{"base.cypher", "first.cypher", "second.cypher", "reader.cypher"}, actual)
}

func TestSelectPrerequisitesRejectsUnknownQueryAndMissingRequiredPrerequisite(t *testing.T) {
	var plan = extension.Plan{Queries: []extension.Query{
		{Name: "selected.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "missing.cypher"}}},
	}}

	actual, err := plan.SelectPrerequisites([]string{"selected.cypher"})
	require.ErrorContains(t, err, `required query "missing.cypher" is not in the execution plan`)
	require.Nil(t, actual)

	actual, err = plan.SelectPrerequisites([]string{"unknown.cypher"})
	require.Error(t, err)
	require.Nil(t, actual)

	plan.Queries[0].Dependencies = nil
	// A malformed dependency on a query outside the selected branch is ignored.
	plan.Queries = append(plan.Queries, extension.Query{Name: "unrelated.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "also_missing.cypher"}}})
	actual, err = plan.SelectPrerequisites([]string{"selected.cypher"})
	require.NoError(t, err)
	require.Equal(t, []string{"selected.cypher"}, actual)
}
