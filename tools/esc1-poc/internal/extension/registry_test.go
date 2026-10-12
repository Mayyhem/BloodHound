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
			Schema:            extension.Metadata{Name: "active_directory", DisplayName: "Active Directory", Namespace: "PoC_AD", Version: "v1"},
			PostProcessing:    &extension.ProcessingSettings{ReferenceCommit: strings.Repeat("a", 40)},
			NodeKinds:         []extension.NodeKind{{Name: "PoC_AD_User", PostProcessing: &extension.NodeProcessing{Query: "ad_users.cypher", IDPrefix: "PoC_AD_User_"}}},
			RelationshipKinds: []extension.RelationshipKind{},
		},
		{
			Schema:            extension.Metadata{Name: "cyberark", DisplayName: "CyberArk", Namespace: "PoC_CA", Version: "v1"},
			PostProcessing:    &extension.ProcessingSettings{ReferenceCommit: strings.Repeat("b", 40)},
			NodeKinds:         []extension.NodeKind{{Name: "PoC_CA_Account"}},
			ExternalNodeKinds: []string{"PoC_AD_User"},
			RelationshipKinds: []extension.RelationshipKind{{Name: "PoC_CA_Admin", Endpoints: []extension.Endpoint{{SourceKind: "PoC_AD_User", TargetKind: "PoC_CA_Account"}}, PostProcessing: &extension.RelationshipProcessing{Query: "ca_admin.cypher"}}},
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
		if err := os.WriteFile(filepath.Join(directory, filename), []byte(query), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func registryQueries() map[string]string {
	return map[string]string{
		"shared.cypher":      "// depends-on: node:User\nRETURN 1 AS value",
		"ad_users.cypher":    "// depends-on: query:shared.cypher\nRETURN $shared",
		"ca_accounts.cypher": "// depends-on: node:PoC_CA_Account\nRETURN 1 AS value",
		"ca_admin.cypher":    "// depends-on: query:ad_users.cypher, query:ca_accounts.cypher\nRETURN $ad_users, $ca_accounts",
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
	if expected := []string{"shared.cypher", "ad_users.cypher", "ca_accounts.cypher", "ca_admin.cypher"}; !reflect.DeepEqual(names, expected) {
		t.Fatalf("order %v; expected %v", names, expected)
	}
	if expected := map[string]string{"shared.cypher": "shared", "ad_users.cypher": "active_directory", "ca_accounts.cypher": "cyberark", "ca_admin.cypher": "cyberark"}; !reflect.DeepEqual(attributions, expected) {
		t.Fatalf("attributions %v; expected %v", attributions, expected)
	}
	if plan.Queries[0].ExtensionName != "Shared calculations" || plan.Queries[1].ExtensionName != "Active Directory" {
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
			definitions[1].RelationshipKinds[0].Endpoints[0].TargetKind = "PoC_AD_Account"
		}, "duplicate schema namespace"},
		{"duplicate kind across nested namespaces", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].Schema.Namespace = "PoC"
			definitions[1].NodeKinds = []extension.NodeKind{{Name: "PoC_AD_User"}, {Name: "PoC_CA_Account"}}
			definitions[1].ExternalNodeKinds = nil
		}, "duplicate declared kind"},
		{"duplicate output", func(definitions []extension.Definition, _ map[string]string) {
			definitions[1].RelationshipKinds[0].PostProcessing.Query = "ad_users.cypher"
		}, "multiple output bindings"},
		{"duplicate prefix", func(definitions []extension.Definition, queries map[string]string) {
			definitions[1].NodeKinds[0].PostProcessing = &extension.NodeProcessing{Query: "ca_accounts.cypher", IDPrefix: definitions[0].NodeKinds[0].PostProcessing.IDPrefix}
			queries["ca_admin.cypher"] = "// depends-on: query:ad_users.cypher, query:ca_accounts.cypher\nRETURN 1"
		}, "duplicate helper ID prefix"},
		{"missing cross-schema prerequisite", func(_ []extension.Definition, queries map[string]string) {
			queries["ca_admin.cypher"] = "// depends-on: query:missing_ad.cypher\nRETURN 1"
		}, "missing prerequisite"},
		{"cross-schema cycle", func(_ []extension.Definition, queries map[string]string) {
			queries["ad_users.cypher"] = "// depends-on: query:ca_admin.cypher, query:shared.cypher\nRETURN 1"
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
	definitions[0].RelationshipKinds = []extension.RelationshipKind{{Name: "PoC_ADCSESC1", Endpoints: []extension.Endpoint{{SourceKind: "PoC_AD_User", TargetKind: "PoC_AD_User"}}, PostProcessing: &extension.RelationshipProcessing{Query: "ad_esc1.cypher"}}}
	queries["ad_esc1.cypher"] = "// depends-on: query:ad_users.cypher\nRETURN $ad_users"
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
