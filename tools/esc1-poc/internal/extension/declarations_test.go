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
)

func TestAnalysisVersionRequirements(t *testing.T) {
	for _, testCase := range []struct {
		version, conditions string
		matches             bool
	}{
		{"v2.0.0", "", true},
		{"v1.10.0", ">=v1.9.0", true},
		{"v1.9.0", ">=v1.10.0", false},
		{"v1.1.0", ">=v1.0.3, <v2.0.0", true},
		{"v2.0.0", ">=v1.0.3,<v2.0.0", false},
		{"v2.0.0", ">=v1.0.3", true},
		{"v1.0.0", "==v1.0.0", true},
		{"v1.0.1", "==v1.0.0", false},
		{"v1.0.0", "!=v1.0.0", false},
		{"v1.0.0", ">v1.0.0", false},
		{"v1.0.0", "<=v1.0.0", true},
		{"v1.0.0", "<v1.0.0", false},
		{"v1.0.0", ">=v2.0.0,<v1.0.0", false},
		{"v1.2.0", ">=v1.0.0,<v2.0.0,!=v1.2.0", false},
		{"v1.0.9", "~=v1.0.3", true},
		{"v1.1.0", "~=v1.0.3", false},
		{"v1.9.0", "~=v1.0", true},
		{"v2.0.0", "~=v1.0", false},
		{"v1.9.0", "==v1.*", true},
		{"v1.2.9", "!=v1.2.*", false},
		{"v1.3.0", "!=v1.2.*", true},
		{"v1.0.0-rc.1", "", true},
		{"v1.0.0-rc.1", ">=v0.9.0", false},
		{"v1.0.0-rc.2", ">=v1.0.0-rc.1,<v1.0.0", true},
		{"v1.0.0+build.2", "==v1.0.0+build.1", true},
	} {
		t.Run(testCase.version+"/"+testCase.conditions, func(t *testing.T) {
			matches, err := extension.VersionMatches(testCase.version, testCase.conditions)
			if err != nil || matches != testCase.matches {
				t.Fatalf("matches=%t error=%v; want %t", matches, err, testCase.matches)
			}
		})
	}
	for _, conditions := range []string{"latest", "^v1.0.0", "~v1.0.0", "===v1.0.0", ">=1.0.0", ">=v1.0.0 <v2.0.0", ">=v1.0.0,", "==v1", "==v1.2", ">=v1.*", "~=v1", "==v01.*", "==v1.2.3.*", "~=v1.0.0 || ==v2.0.0"} {
		if _, err := extension.VersionMatches("v1.0.0", conditions); err == nil {
			t.Errorf("accepted malformed conditions %q", conditions)
		}
	}
}

func TestFileMetadataOnlyReadsLeadingComments(t *testing.T) {
	var header = "// namespace: PoC_AD\n// version: v1.0.0\n"
	metadata, err := extension.ReadQueryMetadata("/* license */\n" + header + "RETURN '// namespace: Other' AS text\n// version: v2.0.0")
	if err != nil || metadata.Namespace != "PoC_AD" || metadata.Version != "v1.0.0" {
		t.Fatalf("metadata=%+v error=%v", metadata, err)
	}
	for _, source := range []string{
		"RETURN 1\n" + header,
		"// namespace: PoC_AD\n// version: v1\nRETURN 1",
		header + "// namespace: PoC_AD\nRETURN 1",
		header + "// extension-versions:\nRETURN 1",
		header + "// extension-versions: ^v1.0.0\nRETURN 1",
	} {
		if _, err := extension.ReadQueryMetadata(source); err == nil {
			t.Error("accepted missing, repeated, or invalid metadata")
		}
	}
	for _, name := range []string{"../users.cypher", "PoC_Other/users.cypher", "PoC_AD/../users.cypher", "users\\other.cypher"} {
		if _, err := extension.QueryFileKey(name, header); err == nil {
			t.Errorf("accepted unsafe or mismatched file key %q", name)
		}
	}
}

func TestQueryRequirementsResolveIdentityAndAlias(t *testing.T) {
	var header = "// namespace: PoC_CA\n// version: v1.0.0\n"
	dependencies, err := extension.QueryDependencies(header + "// depends-on: query:PoC_AD/users>=v1.0.3, <v2.0.0 as directory_users\n// depends-on: query:users\n// depends-on: node:User, relationship:MemberOf\nRETURN 1")
	if err != nil || len(dependencies) != 4 {
		t.Fatalf("dependencies=%+v error=%v", dependencies, err)
	}
	if dependencies[0].Name != "PoC_AD/users.cypher" || dependencies[0].AnswerParameter() != "directory_users" || dependencies[0].Conditions != ">=v1.0.3, <v2.0.0" || dependencies[1].Name != "PoC_CA/users.cypher" {
		t.Fatalf("incorrect identity, conditions, or alias: %+v", dependencies)
	}
	for _, declarations := range []string{
		"query:users.cypher", "query:../users", "query:PoC_AD/users, node:User", "node:User, query:users",
		"query:users as bad-name", "query:users\n// depends-on: query:PoC_AD/users",
		"query:users\n// depends-on: query:users as other", "query:users, query:other",
	} {
		if _, err := extension.QueryDependencies(header + "// depends-on: " + declarations + "\nRETURN 1"); err == nil {
			t.Errorf("accepted invalid requirement %q", declarations)
		}
	}
	dependencies, err = extension.QueryDependencies(header + "// depends-on: none\nRETURN '// depends-on: query:missing' AS text\n// depends-on: query:missing")
	if err != nil || len(dependencies) != 0 {
		t.Fatalf("body text changed dependencies: %+v %v", dependencies, err)
	}
	if _, err := extension.QueryFileKey("users.cypher", strings.ReplaceAll(header, "PoC_CA", "PoC_AD")); err != nil {
		t.Fatal(err)
	}
}
