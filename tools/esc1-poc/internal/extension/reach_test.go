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

package extension

import (
	"github.com/specterops/dawgs/cypher/frontend"
	"path/filepath"
	"testing"
)

func TestReachabilityDirective(t *testing.T) {
	var dependencies = []Dependency{{Type: "relationship", Name: "MemberOf"}, {Type: "node", Name: "Domain"}}
	var valid = `// reachability: {"direction":"incoming","relationships":["MemberOf"],"include_start":true,"expand_results":true,"reject_cycles":true,"node_kinds":["Domain"]}`
	var config, err = parseReachability(valid, dependencies)
	if err != nil || config == nil || !config.RejectCycles || !config.IncludeStart || !config.ExpandResults {
		t.Fatalf("configuration %+v error %v", config, err)
	}
	for _, text := range []string{
		`// reachability: {"direction":"both","relationships":["MemberOf"]}`,
		`// reachability: {"direction":"incoming","relationships":["Missing"]}`,
		`// reachability: {"direction":"incoming","relationships":["MemberOf","MemberOf"]}`,
		`// reachability: {"direction":"incoming","relationships":["MemberOf"],"unknown":true}`,
		`// reachability: {"direction":"incoming","relationships":["MemberOf"],"node_kinds":["User"]}`,
		`// reachability: {"direction":"incoming","relationships":[]}`,
		valid + "\n" + valid,
		valid + " {}",
	} {
		if _, err := parseReachability(text, dependencies); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}

func TestBatchDependencyDirective(t *testing.T) {
	var dependencies = []Dependency{{Type: "query", Name: "seeds.cypher"}}
	var name, err = parseBatchDependency("// batch-on: seeds.cypher", dependencies)
	if err != nil || name != "seeds.cypher" {
		t.Fatalf("name %q error %v", name, err)
	}
	for _, text := range []string{"// batch-on:", "// batch-on: absent.cypher", "// batch-on: seeds.cypher, other.cypher", "// batch-on: seeds.cypher\n// batch-on: seeds.cypher"} {
		if _, err := parseBatchDependency(text, dependencies); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}

func TestShippedQuerySyntaxIncludesReachSeeds(t *testing.T) {
	var plan, err = Load(filepath.Join("..", "..", "extension"))
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range plan.Queries {
		t.Run(query.Name, func(t *testing.T) {
			if _, err := frontend.ParseCypher(frontend.NewContext(), query.Text); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGroupOverlapDirective(t *testing.T) {
	var dependencies = []Dependency{{Type: "query", Name: "membership.cypher"}}
	var valid = `// group-overlap: {"memberships":"membership.cypher"}`
	var config, err = parseGroupOverlap(valid, dependencies)
	if err != nil || config.Memberships != "membership.cypher" {
		t.Fatalf("configuration %+v error %v", config, err)
	}
	for _, text := range []string{`// group-overlap: {"memberships":"absent.cypher"}`, `// group-overlap: {"memberships":"membership.cypher","unknown":true}`, `// group-overlap: {}`, valid + "\n" + valid, valid + " {}"} {
		if _, err := parseGroupOverlap(text, dependencies); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}
