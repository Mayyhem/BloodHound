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
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func sharedProducerDefinition() extension.Definition {
	return extension.Definition{
		Schema:    extension.Metadata{Name: "shared_results", DisplayName: "Shared results", Namespace: "PoC", Version: "v1"},
		NodeKinds: []extension.NodeKind{{Name: "PoC_Helper"}},
		RelationshipKinds: []extension.RelationshipKind{
			{Name: "PoC_First", CompatibleKinds: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}},
			{Name: "PoC_Second", CompatibleKinds: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}},
		},
	}
}

func connectionQuery(dependencies, kind string) string {
	return "// depends-on: " + dependencies + "\nRETURN 'u' AS source, 'd' AS target, '" + kind + "' AS kind"
}

func queryNames(plan extension.Plan) []string {
	var result []string
	for _, query := range plan.Queries {
		result = append(result, query.Name)
	}
	return result
}

func TestSharedGraphReadersWaitForEveryProducer(t *testing.T) {
	var (
		definition = sharedProducerDefinition()
		queries    = map[string]string{
			"a_reader.cypher":   connectionQuery("relationship:PoC_First, node:PoC_Helper", "PoC_Second"),
			"b_producer.cypher": connectionQuery("none", "PoC_First"),
			"c_producer.cypher": connectionQuery("none", "PoC_First"),
			"helper.cypher":     "// depends-on: none\nRETURN 'PoC_Helper_1' AS id, ['PoC_Helper'] AS kinds, {} AS properties",
		}
		plan, err = extension.Load(writeRegistry(t, []extension.Definition{definition}, queries))
	)
	if err != nil {
		t.Fatal(err)
	}
	if expected := []string{"b_producer.cypher", "c_producer.cypher", "helper.cypher", "a_reader.cypher"}; !reflect.DeepEqual(queryNames(plan), expected) {
		t.Fatalf("query order %v; expected %v", queryNames(plan), expected)
	}
	if len(plan.Schema.Relationships) != 3 || len(plan.Definition.RelationshipKinds) != 2 {
		t.Fatalf("shared outputs must retain one kind definition and one binding per producer: %+v", plan)
	}
	for _, query := range plan.Queries {
		if query.Extension != definition.Schema.Name {
			t.Fatalf("producer lost its display grouping: %+v", query)
		}
	}
}

func TestSharedProducersMustAgreeOnPolicy(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		first, second string
		wantError     bool
	}{
		{"both default", "", "", false},
		{"explicit default", "replace-existing", "", false},
		{"matching keep", "keep-existing", "keep-existing", false},
		{"different policies", "keep-existing", "merge-properties", true},
		{"default first", "", "keep-existing", true},
		{"default second", "keep-existing", "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				queries  = make(map[string]string)
				policies = map[string]string{"first.cypher": testCase.first, "second.cypher": testCase.second}
			)
			for name, policy := range policies {
				queries[name] = connectionQuery("none", "PoC_First")
				if policy != "" {
					queries[name] = "// existing-edge-policy: " + policy + "\n" + queries[name]
				}
			}
			_, err := extension.Load(writeRegistry(t, []extension.Definition{sharedProducerDefinition()}, queries))
			if testCase.wantError {
				if err == nil || !strings.Contains(err.Error(), "must agree on existing-edge-policy") {
					t.Fatalf("expected conflicting policy error; got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGraphDependenciesRejectCyclesAndPermitSelfReads(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		queries map[string]string
		cycle   bool
	}{
		{"self read", map[string]string{"first.cypher": connectionQuery("relationship:PoC_First", "PoC_First")}, false},
		{"no producer", map[string]string{"reader.cypher": connectionQuery("relationship:PoC_First", "PoC_Second")}, false},
		{"two graph kinds", map[string]string{
			"first.cypher":  connectionQuery("relationship:PoC_Second", "PoC_First"),
			"second.cypher": connectionQuery("relationship:PoC_First", "PoC_Second"),
		}, true},
		{"shared self readers", map[string]string{
			"first.cypher":  connectionQuery("relationship:PoC_First", "PoC_First"),
			"second.cypher": connectionQuery("relationship:PoC_First", "PoC_First"),
		}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := extension.Load(writeRegistry(t, []extension.Definition{sharedProducerDefinition()}, testCase.queries))
			if testCase.cycle {
				if err == nil || !strings.Contains(err.Error(), "dependency cycle") || !strings.Contains(err.Error(), "first.cypher") || !strings.Contains(err.Error(), "second.cypher") {
					t.Fatalf("expected both queries in cycle error; got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAffectedExpandsSharedProducersInputsAndConsumers(t *testing.T) {
	var (
		queries = map[string]string{
			"first.cypher":  connectionQuery("relationship:MemberOf", "PoC_First"),
			"second.cypher": connectionQuery("query:seed.cypher", "PoC_First"),
			"seed.cypher":   "// depends-on: none\nRETURN 1 AS value",
			"reader.cypher": connectionQuery("relationship:PoC_First", "PoC_Second"),
			"helper.cypher": "// depends-on: query:seed.cypher\nRETURN 'PoC_Helper_1' AS id, ['PoC_Helper'] AS kinds, {} AS properties",
		}
		plan, err = extension.Load(writeRegistry(t, []extension.Definition{sharedProducerDefinition()}, queries))
	)
	if err != nil {
		t.Fatal(err)
	}
	// A shared producer pulls in seed; refreshing seed also requires its other
	// consumer, even if that consumer appeared earlier in the ordered plan.
	if actual := plan.Affected(nil, []string{"MemberOf"}, true); !reflect.DeepEqual(actual, queryNames(plan)) {
		t.Fatalf("incomplete selection %v; expected %v", actual, queryNames(plan))
	}
	if actual := plan.ExpandSelection([]string{"reader.cypher"}); !reflect.DeepEqual(actual, queryNames(plan)) {
		t.Fatalf("manual selection skipped related work: %v", actual)
	}
}

func TestSharedSelfReadDoesNotWaitForPeerProducers(t *testing.T) {
	var (
		queries = map[string]string{
			"first.cypher":  connectionQuery("relationship:PoC_First, query:seed.cypher", "PoC_First"),
			"second.cypher": connectionQuery("relationship:PoC_First", "PoC_First"),
			"seed.cypher":   "// depends-on: none\nRETURN 1 AS value",
			"reader.cypher": connectionQuery("relationship:PoC_First", "PoC_Second"),
		}
		plan, err = extension.Load(writeRegistry(t, []extension.Definition{sharedProducerDefinition()}, queries))
		expected  = map[string][]string{
			"first.cypher":  {"seed.cypher"},
			"second.cypher": nil,
			"seed.cypher":   nil,
			"reader.cypher": {"first.cypher", "second.cypher"},
		}
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range plan.Queries {
		if actual := plan.Prerequisites(query); !reflect.DeepEqual(actual, expected[query.Name]) {
			t.Fatalf("%s prerequisites %v; expected %v", query.Name, actual, expected[query.Name])
		}
	}
	// Sharing an output still requires selecting every producer, even though
	// a self-reader does not wait for the new result of that output.
	if actual := plan.ExpandSelection([]string{"first.cypher"}); !reflect.DeepEqual(actual, queryNames(plan)) {
		t.Fatalf("shared self-read selection %v; expected %v", actual, queryNames(plan))
	}
}
