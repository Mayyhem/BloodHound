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
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func registryDefinitions() []extension.Definition {
	return []extension.Definition{
		{
			Schema:            extension.Metadata{Name: "active_directory", DisplayName: "Active Directory", Namespace: "PoC_AD", Version: "v1.0.0"},
			PostProcessing:    &extension.ProcessingSettings{ReferenceCommit: strings.Repeat("a", 40)},
			NodeKinds:         []extension.NodeKind{{Name: "PoC_AD_User", PostProcessing: &extension.NodeProcessing{Query: "PoC_AD/ad_users.cypher", IDPrefix: "PoC_AD_User_"}}},
			RelationshipKinds: []extension.RelationshipKind{},
		},
		{
			Schema:            extension.Metadata{Name: "cyberark", DisplayName: "CyberArk", Namespace: "PoC_CA", Version: "v1.0.0"},
			PostProcessing:    &extension.ProcessingSettings{ReferenceCommit: strings.Repeat("b", 40)},
			NodeKinds:         []extension.NodeKind{{Name: "PoC_CA_Account"}},
			RelationshipKinds: []extension.RelationshipKind{{Name: "PoC_CA_Admin", CompatibleKinds: []extension.Endpoint{{SourceKind: "PoC_AD_User", TargetKind: "PoC_CA_Account"}}, PostProcessing: &extension.RelationshipProcessing{Query: "PoC_CA/ca_admin.cypher"}}},
		},
	}
}

func writeRegistry(t *testing.T, definitions []extension.Definition, queries map[string]string) string {
	t.Helper()
	var (
		directory = t.TempDir()
		filenames = []string{"active_directory.schema.json", "cyberark.schema.json"}
	)
	for index, definition := range definitions {
		contents, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, filenames[index]), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for filename, query := range queries {
		namespace := definitions[0].Schema.Namespace
		if strings.HasPrefix(filename, "PoC_CA/") {
			namespace = definitions[1].Schema.Namespace
		}
		query = "// namespace: " + namespace + "\n// version: v1.0.0\n" + query
		if err := os.MkdirAll(filepath.Dir(filepath.Join(directory, filename)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, filename), []byte(query), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func registryQueries() map[string]string {
	return map[string]string{
		"PoC_AD/shared.cypher":      "// depends-on: node:User\nRETURN 1 AS value",
		"PoC_AD/ad_users.cypher":    "// depends-on: query:shared\nRETURN $shared",
		"PoC_CA/ca_accounts.cypher": "// depends-on: node:PoC_CA_Account\nRETURN 1 AS value",
		"PoC_CA/ca_admin.cypher":    "// depends-on: query:PoC_AD/ad_users\n// depends-on: query:ca_accounts\nRETURN $ad_users, $ca_accounts",
	}
}

func TestLoadRegistryOrdersCrossSchemaQueries(t *testing.T) {
	var (
		definitions  = registryDefinitions()
		plan, err    = extension.Load(writeRegistry(t, definitions, registryQueries()))
		names        []string
		attributions = make(map[string]string)
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range plan.Queries {
		names = append(names, query.Name)
		attributions[query.Name] = query.Extension
	}
	if expected := []string{"PoC_AD/shared.cypher", "PoC_AD/ad_users.cypher", "PoC_CA/ca_accounts.cypher", "PoC_CA/ca_admin.cypher"}; !reflect.DeepEqual(names, expected) {
		t.Fatalf("order %v; expected %v", names, expected)
	}
	if expected := map[string]string{"PoC_AD/shared.cypher": "active_directory", "PoC_AD/ad_users.cypher": "active_directory", "PoC_CA/ca_accounts.cypher": "cyberark", "PoC_CA/ca_admin.cypher": "cyberark"}; !reflect.DeepEqual(attributions, expected) {
		t.Fatalf("attributions %v; expected %v", attributions, expected)
	}
	if plan.Queries[0].ExtensionName != "Active Directory" || plan.Queries[1].ExtensionName != "Active Directory" {
		t.Fatalf("incorrect display names: %+v", plan.Queries)
	}
	if plan.Schema.Name != "registered_extensions" || plan.Schema.ReferenceCommit != definitions[0].PostProcessing.ReferenceCommit {
		t.Fatalf("incorrect combined schema %+v", plan.Schema)
	}
	if len(plan.Definitions) != 2 || plan.Definition.Schema.Name != "active_directory" || plan.SchemaFiles["cyberark"] != "cyberark.schema.json" {
		t.Fatalf("incorrect definition metadata %+v", plan)
	}
	if len(plan.Schema.Nodes) != 1 {
		t.Fatal("collected CyberArk account was claimed as output")
	}
}

func TestLoadRegistryRejectsGlobalConflictsAndInvalidDependencies(t *testing.T) {
	var cases = []struct {
		name   string
		change func([]extension.Definition, map[string]string)
		want   string
	}{
		{"duplicate name", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].Schema.Name = definitions[0].Schema.Name
		}, "duplicate schema name"},
		{"duplicate namespace", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].Schema.Namespace = "PoC_AD"
			definitions[1].NodeKinds[0].Name = "PoC_AD_Account"
			definitions[1].RelationshipKinds[0].Name = "PoC_AD_Admin"
			definitions[1].RelationshipKinds[0].CompatibleKinds[0].TargetKind = "PoC_AD_Account"
		}, "duplicate schema namespace"},
		{"duplicate kind across nested namespaces", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].Schema.Namespace = "PoC"
			definitions[1].NodeKinds = []extension.NodeKind{{Name: "PoC_AD_User"}, {Name: "PoC_CA_Account"}}
		}, "duplicate declared kind"},
		{"duplicate output", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].RelationshipKinds[0].PostProcessing.Query = "PoC_AD/ad_users.cypher"
		}, "multiple output bindings"},
		{"duplicate prefix", func(definitions []extension.Definition, queries map[string]string) {
			definitions[1].NodeKinds[0].PostProcessing = &extension.NodeProcessing{Query: "PoC_CA/ca_accounts.cypher", IDPrefix: definitions[0].NodeKinds[0].PostProcessing.IDPrefix}
			queries["PoC_CA/ca_admin.cypher"] = "// depends-on: query:PoC_AD/ad_users\n// depends-on: query:ca_accounts\nRETURN 1"
		}, "duplicate helper ID prefix"},
		{"missing cross-schema prerequisite", func(_ []extension.Definition, queries map[string]string) {
			queries["PoC_CA/ca_admin.cypher"] = "// depends-on: query:missing_ad\nRETURN 1"
		}, "missing prerequisite"},
		{"cross-schema cycle", func(_ []extension.Definition, queries map[string]string) {
			queries["PoC_AD/ad_users.cypher"] = "// depends-on: query:PoC_CA/ca_admin\n// depends-on: query:shared\nRETURN 1"
		}, "dependency cycle"},
		{"invalid secondary settings", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].PostProcessing.ReferenceCommit = "invalid"
		}, "reference_commit"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				definitions = registryDefinitions()
				queries     = registryQueries()
			)
			testCase.change(definitions, queries)
			_, err := extension.Load(writeRegistry(t, definitions, queries))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %v; expected %q", err, testCase.want)
			}
		})
	}
}

func TestRegistryAllowsStandaloneSchema(t *testing.T) {
	var (
		definitions = registryDefinitions()
		directory   string
	)
	directory = writeRegistry(t, definitions, registryQueries())
	if err := os.Rename(filepath.Join(directory, "active_directory.schema.json"), filepath.Join(directory, "schema.json")); err != nil {
		t.Fatal(err)
	}
	plan, err := extension.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Definitions[0].Schema.Name != "cyberark" || plan.SchemaFiles["active_directory"] != "schema.json" {
		t.Fatal("definitions are not in filename order")
	}
}

func TestRegistryCombinesNativeComparisonSettings(t *testing.T) {
	var (
		definitions = registryDefinitions()
		queries     = registryQueries()
	)
	definitions[0].Schema.Namespace = "PoC"
	definitions[0].PostProcessing.CompareNativeESC1 = true
	definitions[0].RelationshipKinds = []extension.RelationshipKind{{Name: "PoC_ADCSESC1", CompatibleKinds: []extension.Endpoint{{SourceKind: "PoC_AD_User", TargetKind: "PoC_AD_User"}}, PostProcessing: &extension.RelationshipProcessing{Query: "PoC_AD/ad_esc1.cypher"}}}
	queries["PoC_AD/ad_esc1.cypher"] = "// depends-on: query:PoC_AD/ad_users\nRETURN $ad_users"
	for name, text := range queries {
		delete(queries, name)
		queries[strings.Replace(name, "PoC_AD/", "PoC/", 1)] = strings.ReplaceAll(text, "query:PoC_AD/", "query:PoC/")
	}
	definitions[0].NodeKinds[0].PostProcessing.Query = "PoC/ad_users.cypher"
	definitions[0].RelationshipKinds[0].PostProcessing.Query = "PoC/ad_esc1.cypher"

	plan, err := extension.Load(writeRegistry(t, definitions, queries))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Schema.CompareNativeESC1 {
		t.Fatal("combined plan lost native comparison request")
	}
}

func TestIsSchemaFileRequiresSafeSnakeCaseBasename(t *testing.T) {
	var cases = []struct {
		name string
		want bool
	}{
		{"schema.json", true},
		{"active_directory.schema.json", true},
		{"cyberark2.schema.json", true},
		{"../x.schema.json", false},
		{"nested/x.schema.json", false},
		{`nested\x.schema.json`, false},
		{"/tmp/x.schema.json", false},
		{`.schema.json`, false},
		{".hidden.schema.json", false},
		{"ActiveDirectory.schema.json", false},
		{"", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := extension.IsSchemaFile(testCase.name); actual != testCase.want {
				t.Fatalf("IsSchemaFile(%q) = %v; expected %v", testCase.name, actual, testCase.want)
			}
		})
	}
}

// Kind references are checked after all definitions have been read. They do
// not create a query execution dependency by themselves.
func TestMutualKindReferencesAndNativeKinds(t *testing.T) {
	var definitions = registryDefinitions()
	definitions[0].RelationshipKinds = []extension.RelationshipKind{{Name: "PoC_AD_CollectedLink", CompatibleKinds: []extension.Endpoint{{SourceKind: "PoC_CA_Account", TargetKind: "PoC_AD_User"}}}}
	for _, native := range []string{"User", "Base", "ADLocalGroup", "ADLocalUser", "AZUser", "AZBase", "Zone"} {
		definitions[0].RelationshipKinds[0].CompatibleKinds = append(definitions[0].RelationshipKinds[0].CompatibleKinds, extension.Endpoint{SourceKind: native, TargetKind: "PoC_AD_User"})
	}
	for _, order := range [][]extension.Definition{definitions, {definitions[1], definitions[0]}} {
		if err := extension.ValidateDefinitions(order); err != nil {
			t.Fatal(err)
		}
	}
	for _, missing := range []string{"PoC_Missing_Node", "PoC_CA_Admin", "MemberOf", "LocalGroup"} {
		definitions[0].RelationshipKinds[0].CompatibleKinds[0].SourceKind = missing
		err := extension.ValidateDefinitions(definitions)
		if err == nil || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "include the schema") {
			t.Fatal("missing node definition not explained", err)
		}
	}
}

func TestDefinitionNoLongerAcceptsRepeatedExternalKindsOrOldFieldName(t *testing.T) {
	var (
		definition    = modelDefinition()
		contents, err = json.Marshal(definition)
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		strings.Replace(string(contents), `"compatible_kinds"`, `"endpoints"`, 1),
		strings.Replace(string(contents), `"node_kinds":`, `"external_node_kinds":["User"],"node_kinds":`, 1),
	} {
		if _, err := extension.ParseDefinition([]byte(invalid)); err == nil {
			t.Fatal("old field silently accepted")
		}
	}
}
