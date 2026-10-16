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
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/querycheck"
	"github.com/stretchr/testify/require"
)

func editorDefinition() extension.Definition {
	return extension.Definition{
		Schema:         extension.Metadata{Name: "editor_example", DisplayName: "Editor example", Namespace: "PoC_Edit", Version: "v1.0.0"},
		PostProcessing: &extension.ProcessingSettings{ReferenceCommit: strings.Repeat("a", 40)},
		RelationshipKinds: []extension.RelationshipKind{
			{Name: "PoC_Edit_Declared", CompatibleKinds: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}},
		},
	}
}

func editorFile(namespace, body string) string {
	return "// namespace: " + namespace + "\n// version: v1.0.0\n" + body
}

func TestLoadEditorFilesAllowsOnlyItsUndeclaredOutput(t *testing.T) {
	var (
		definitions = []extension.Definition{editorDefinition()}
		files       = map[string]string{
			"PoC_Edit/prerequisite.cypher": editorFile("PoC_Edit", "// depends-on: none\nRETURN 'input' AS value"),
			"PoC_Edit/target.cypher":       editorFile("PoC_Edit", "// depends-on: query:prerequisite\nRETURN 'user' AS source, 'domain' AS target, 'PoC_Edit_Proposed' AS kind"),
		}
	)

	plan, err := extension.LoadEditorFilesWithDisabled(definitions, files, nil, "PoC_Edit/target.cypher")
	require.NoError(t, err)
	require.Len(t, plan.Queries, 2)
	require.Equal(t, "PoC_Edit/target.cypher", plan.EditorTarget)
	require.Equal(t, "PoC_Edit_Proposed", plan.Queries[1].OutputKind)
	require.Empty(t, plan.Schema.Relationships)

	plan, err = extension.LoadEditorFilesWithDisabled(definitions, files, []string{"PoC_Edit/target.cypher"}, "PoC_Edit/target.cypher")
	require.NoError(t, err)
	require.Empty(t, plan.Queries[1].DisabledReason)

	_, err = extension.LoadFiles(definitions, files)
	require.ErrorContains(t, err, "must be declared")
}

func TestLoadEditorFilesKeepsPrerequisitesAndDeclaredOutputsStrict(t *testing.T) {
	var definitions = []extension.Definition{editorDefinition()}

	t.Run("undeclared prerequisite output", func(t *testing.T) {
		files := map[string]string{
			"PoC_Edit/prerequisite.cypher": editorFile("PoC_Edit", "// depends-on: none\nRETURN 'user' AS source, 'domain' AS target, 'PoC_Edit_Proposed' AS kind"),
			"PoC_Edit/target.cypher":       editorFile("PoC_Edit", "// depends-on: query:prerequisite\nRETURN 1 AS result"),
		}
		_, err := extension.LoadEditorFilesWithDisabled(definitions, files, nil, "PoC_Edit/target.cypher")
		require.ErrorContains(t, err, "must be declared")
	})

	t.Run("disabled prerequisite still needs a matching extension version", func(t *testing.T) {
		files := map[string]string{
			"PoC_Edit/prerequisite.cypher": "// namespace: PoC_Edit\n// version: v1.0.0\n// extension-versions: >=v2.0.0\n// depends-on: none\nRETURN 'input' AS value",
			"PoC_Edit/target.cypher":       editorFile("PoC_Edit", "// depends-on: query:prerequisite\nRETURN 1 AS result"),
		}
		_, err := extension.LoadEditorFilesWithDisabled(definitions, files, []string{"PoC_Edit/prerequisite.cypher"}, "PoC_Edit/target.cypher")
		require.ErrorContains(t, err, "does not satisfy extension-versions")
	})

	t.Run("disabled prerequisite remains marked disabled", func(t *testing.T) {
		files := map[string]string{
			"PoC_Edit/prerequisite.cypher": editorFile("PoC_Edit", "// depends-on: none\nRETURN 'input' AS value"),
			"PoC_Edit/target.cypher":       editorFile("PoC_Edit", "// depends-on: query:prerequisite\nRETURN 1 AS result"),
		}
		plan, err := extension.LoadEditorFilesWithDisabled(definitions, files, []string{"PoC_Edit/prerequisite.cypher"}, "PoC_Edit/target.cypher")
		require.NoError(t, err)
		require.NotEmpty(t, plan.Queries[0].DisabledReason)
		require.Empty(t, plan.Queries[1].DisabledReason)
	})

	t.Run("declared output still needs endpoints", func(t *testing.T) {
		definitions := []extension.Definition{editorDefinition()}
		definitions[0].RelationshipKinds[0].CompatibleKinds = nil
		files := map[string]string{
			"PoC_Edit/target.cypher": editorFile("PoC_Edit", "// depends-on: none\nRETURN 'user' AS source, 'domain' AS target, 'PoC_Edit_Declared' AS kind"),
		}
		_, err := extension.LoadEditorFilesWithDisabled(definitions, files, nil, "PoC_Edit/target.cypher")
		require.ErrorContains(t, err, "needs endpoints")
	})
}

func TestLoadEditorFilesRejectsUnavailableTargets(t *testing.T) {
	var definitions = []extension.Definition{editorDefinition()}

	_, err := extension.LoadEditorFilesWithDisabled(definitions, nil, nil, "PoC_Edit/missing.cypher")
	require.ErrorContains(t, err, "not available for testing")

	unknownExtension := map[string]string{
		"PoC_Unknown/target.cypher": editorFile("PoC_Unknown", "// depends-on: none\nRETURN 1 AS result"),
	}
	_, err = extension.LoadEditorFilesWithDisabled(definitions, unknownExtension, nil, "PoC_Unknown/target.cypher")
	require.ErrorContains(t, err, "install the schema")
}

func TestLoadEditorFilesRetainsSafetyAndDependencyChecks(t *testing.T) {
	var definitions = []extension.Definition{editorDefinition()}
	for _, testCase := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "graph write",
			files: map[string]string{
				"PoC_Edit/target.cypher": editorFile("PoC_Edit", "// depends-on: none\nCREATE (node:User) RETURN node.objectid AS result"),
			},
			want: "CREATE",
		},
		{
			name: "cycle",
			files: map[string]string{
				"PoC_Edit/first.cypher":  editorFile("PoC_Edit", "// depends-on: query:target\nRETURN $target AS result"),
				"PoC_Edit/target.cypher": editorFile("PoC_Edit", "// depends-on: query:first\nRETURN $first AS result"),
			},
			want: "cycle",
		},
		{
			name: "invalid relationship columns",
			files: map[string]string{
				"PoC_Edit/target.cypher": editorFile("PoC_Edit", "// depends-on: none\nRETURN 'user' AS source, 'PoC_Edit_Proposed' AS kind"),
			},
			want: "relationship output requires",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := extension.LoadEditorFilesWithDisabled(definitions, testCase.files, nil, "PoC_Edit/target.cypher")
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestEditorChangingColumnsDoesNotValidateDownstreamConsumers(t *testing.T) {
	files := map[string]string{
		"PoC_Edit/target.cypher":   editorFile("PoC_Edit", "// depends-on: none\nRETURN 1 AS new_column"),
		"PoC_Edit/consumer.cypher": editorFile("PoC_Edit", "// depends-on: query:target\nUNWIND $target AS row RETURN row.old_column AS result"),
	}
	plan, err := extension.LoadEditorFilesWithDisabled([]extension.Definition{editorDefinition()}, files, nil, "PoC_Edit/target.cypher")
	require.NoError(t, err)
	require.Len(t, plan.Queries, 1)
	require.Equal(t, "PoC_Edit/target.cypher", plan.Queries[0].Name)
	_, err = extension.LoadFiles([]extension.Definition{editorDefinition()}, files)
	require.ErrorContains(t, err, "does not return column old_column")
}

func TestUnusedDependenciesListsOnlyDeclarationsTheFileNeverUses(t *testing.T) {
	var (
		definitions = []extension.Definition{editorDefinition()}
		target      = "PoC_Edit/target.cypher"
		files       = map[string]string{
			"PoC_Edit/users.cypher":  editorFile("PoC_Edit", "// depends-on: none\nRETURN 'u' AS value"),
			"PoC_Edit/groups.cypher": editorFile("PoC_Edit", "// depends-on: none\nRETURN 'g' AS value"),
			target: editorFile("PoC_Edit", "// depends-on: node:Domain, node:User\n// depends-on: query:users\n// depends-on: query:groups as unused_groups\n"+
				"MATCH (domain:Domain) UNWIND $users AS user RETURN user.value AS source, domain.objectid AS target, 'PoC_Edit_Declared' AS kind"),
		}
	)
	plan, err := extension.LoadEditorFilesWithDisabled(definitions, files, nil, target)
	require.NoError(t, err)
	for _, query := range plan.Queries {
		if query.Name != target {
			continue
		}
		checked, err := querycheck.Check(query.Text)
		require.NoError(t, err)
		require.Equal(t, []string{"node:User", "query:PoC_Edit/groups"}, extension.UnusedDependencies(query, checked))
	}

	// An unlabeled read may touch any kind, so kind declarations count as used.
	files[target] = editorFile("PoC_Edit", "// depends-on: node:User\nMATCH (n) RETURN n.objectid AS value")
	plan, err = extension.LoadEditorFilesWithDisabled(definitions, files, nil, target)
	require.NoError(t, err)
	for _, query := range plan.Queries {
		if query.Name == target {
			checked, err := querycheck.Check(query.Text)
			require.NoError(t, err)
			require.Empty(t, extension.UnusedDependencies(query, checked))
		}
	}
}
