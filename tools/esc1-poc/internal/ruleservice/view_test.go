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

package ruleservice_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func TestBuildViewEmpty(t *testing.T) {
	var (
		view          = ruleservice.BuildView(ruleservice.Bundle{}, extension.Plan{})
		contents, err = json.Marshal(view)
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Threads) != 0 || string(contents) != `{"revision":"","name":"","owner":"","threads":[],"queries":[],"extensions":[]}` {
		t.Fatalf("empty plan should have an empty array: %s", contents)
	}
}

func TestBuildViewOrdersBranchesAndSeparatesIndependentGroups(t *testing.T) {
	var (
		plan = extension.Plan{Queries: []extension.Query{
			{Name: "seed.cypher", Parameter: "seed", Text: "RETURN 1"},
			{Name: "first.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "seed.cypher"}}},
			{Name: "second.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "seed.cypher"}}},
			{Name: "final.cypher", Dependencies: []extension.Dependency{{Type: "query", Name: "first.cypher"}, {Type: "query", Name: "second.cypher"}}},
			{Name: "independent.cypher"},
		}}
		view           = ruleservice.BuildView(ruleservice.Bundle{Revision: "example"}, plan)
		expectedStages = []int{1, 2, 2, 3}
	)
	if len(view.Threads) != 2 || view.Revision != "example" {
		t.Fatalf("unexpected view: %+v", view)
	}
	for index, query := range view.Threads[0].Queries {
		if query.Order != index+1 || query.Stage != expectedStages[index] {
			t.Fatalf("incorrect order or stage: %+v", query)
		}
	}
	if !reflect.DeepEqual(view.Threads[0].Queries[0].Dependents, []string{"first.cypher", "second.cypher"}) {
		t.Fatalf("missing downstream queries: %+v", view.Threads[0].Queries[0])
	}
	if prerequisite := view.Threads[0].Queries[3].Prerequisites[0]; prerequisite.Name != "first.cypher" || prerequisite.Parameter != "first" {
		t.Fatalf("incorrect prerequisite row parameter: %+v", prerequisite)
	}
	if query := view.Threads[1].Queries[0]; query.Order != 5 || query.Stage != 1 {
		t.Fatalf("independent query order changed: %+v", query)
	}
}

func TestBuildViewGroupsSharedInputsTransitivelyWithoutInventingExecutionEdges(t *testing.T) {
	var (
		plan = extension.Plan{Queries: []extension.Query{
			{Name: "first.cypher", Dependencies: []extension.Dependency{{Type: "node", Name: "User"}}},
			{Name: "bridge.cypher", Dependencies: []extension.Dependency{{Type: "node", Name: "User"}, {Type: "relationship", Name: "MemberOf"}}},
			{Name: "last.cypher", Dependencies: []extension.Dependency{{Type: "relationship", Name: "MemberOf"}}},
			{Name: "separate.cypher", Dependencies: []extension.Dependency{{Type: "node", Name: "MemberOf"}}},
		}}
		view           = ruleservice.BuildView(ruleservice.Bundle{}, plan)
		expectedInputs = []ruleservice.InputView{{Type: "node", Name: "User"}, {Type: "relationship", Name: "MemberOf"}}
	)
	if len(view.Threads) != 2 || len(view.Threads[0].Queries) != 3 {
		t.Fatalf("shared dependency chain split across groups: %+v", view)
	}
	if !reflect.DeepEqual(view.Threads[0].SharedInputs, expectedInputs) {
		t.Fatalf("unexpected shared inputs: %+v", view.Threads[0].SharedInputs)
	}
	for _, query := range view.Threads[0].Queries {
		if query.Stage != 1 || len(query.Prerequisites) != 0 {
			t.Fatalf("shared input became a scheduler edge: %+v", query)
		}
	}
}

func TestBuildViewJoinsManagedProducersAndPreservesPublicationDetails(t *testing.T) {
	var (
		text = "// depends-on: none\nRETURN '<script>alert(1)</script>' AS note\n"
		plan = extension.Plan{
			Schema: extension.Schema{Name: "test", Owner: "owner", Nodes: []extension.NodeBinding{{Kind: "PoC_Helper", Query: "helper.cypher", IDPrefix: "PoC_Helper_"}}, Relationships: []extension.RelationshipBinding{
				{Kind: "PoC_Default", Query: "consumer.cypher", Endpoints: []extension.Endpoint{{SourceKind: "PoC_Helper", TargetKind: "User"}}},
				{Kind: "PoC_Preserved", Query: "consumer.cypher", ExistingEdgePolicy: extension.KeepExisting},
			}},
			Queries: []extension.Query{
				{Name: "consumer.cypher", Text: text, Dependencies: []extension.Dependency{{Type: "node", Name: "PoC_Helper"}}},
				{Name: "helper.cypher", Parameter: "helper"},
			},
		}
		view = ruleservice.BuildView(ruleservice.Bundle{}, plan)
	)
	if len(view.Threads) != 1 {
		t.Fatalf("managed producer separated from consumer: %+v", view)
	}
	if query := view.Threads[0].Queries[0]; query.Text != text || query.Order != 1 || query.Stage != 1 || len(query.Prerequisites) != 0 {
		t.Fatalf("full text or actual scheduling changed: %+v", query)
	}
	if output := view.Threads[0].Queries[1].Outputs[0]; output.IDPrefix != "PoC_Helper_" || output.Type != "node" {
		t.Fatalf("helper details missing: %+v", output)
	}
	if outputs := view.Threads[0].Queries[0].Outputs; len(outputs) != 2 || outputs[0].ExistingEdgePolicy != extension.ReplaceExisting || outputs[1].ExistingEdgePolicy != extension.KeepExisting || outputs[0].Endpoints[0].SourceKind != "PoC_Helper" {
		t.Fatalf("relationship publication details missing: %+v", outputs)
	}
}

func TestBuildViewGroupsEndpointAndReachabilityKinds(t *testing.T) {
	var (
		plan = extension.Plan{
			Schema: extension.Schema{
				Nodes:         []extension.NodeBinding{{Kind: "PoC_Helper", Query: "helper.cypher"}},
				Relationships: []extension.RelationshipBinding{{Kind: "PoC_Link", Query: "endpoint.cypher", Endpoints: []extension.Endpoint{{SourceKind: "PoC_Helper", TargetKind: "User"}}}},
			},
			Queries: []extension.Query{
				{Name: "helper.cypher"},
				{Name: "endpoint.cypher", Dependencies: []extension.Dependency{{Type: "node", Name: "User"}}},
				{Name: "reach.cypher", Reach: &extension.Reachability{NodeKinds: []string{"User"}, Relationships: []string{"MemberOf"}}},
				{Name: "membership.cypher", Dependencies: []extension.Dependency{{Type: "relationship", Name: "MemberOf"}}},
				{Name: "separate.cypher"},
			},
		}
		view = ruleservice.BuildView(ruleservice.Bundle{}, plan)
	)
	if len(view.Threads) != 2 || len(view.Threads[0].Queries) != 4 {
		t.Fatalf("endpoint producer or traversal dependency split across groups: %+v", view)
	}
	if !reflect.DeepEqual(view.Threads[0].SharedInputs, []ruleservice.InputView{{Type: "node", Name: "User"}, {Type: "relationship", Name: "MemberOf"}}) {
		t.Fatalf("implicit kinds missing or duplicate declared/endpoint input counted twice: %+v", view.Threads[0].SharedInputs)
	}
	for _, query := range view.Threads[0].Queries {
		if query.Stage != 1 || len(query.Prerequisites) != 0 {
			t.Fatalf("implicit kind became an execution edge: %+v", query)
		}
	}
}

func TestBuildViewIncludesExtensionsAndPausedRules(t *testing.T) {
	var (
		addedAt = time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("offset", 3600))
		plan    = extension.Plan{
			Definitions: []extension.Definition{{Schema: extension.Metadata{Name: "cyberark", DisplayName: "CyberArk"}}, {Schema: extension.Metadata{Name: "active_directory", DisplayName: "Active Directory"}}},
			Queries: []extension.Query{
				{Name: "seed.cypher", Extension: "active_directory", ExtensionName: "Active Directory", DisabledReason: "Disabled by you."},
				{Name: "consumer.cypher", Extension: "cyberark", ExtensionName: "CyberArk", DisabledReason: "Waiting for seed.cypher to be enabled.", Dependencies: []extension.Dependency{{Type: "query", Name: "seed.cypher"}}},
			},
		}
		view = ruleservice.BuildView(ruleservice.Bundle{AddedAt: map[string]time.Time{"seed.cypher": addedAt}}, plan)
	)
	if len(view.Queries) != 2 || len(view.Extensions) != 2 || view.Extensions[0].DisplayName != "Active Directory" {
		t.Fatalf("missing flattened rules or extensions: %+v", view)
	}
	if query := view.Queries[0]; query.Enabled || query.AddedAt != "2026-10-11T11:00:00Z" || query.Extension != "active_directory" {
		t.Fatalf("incorrect paused rule metadata: %+v", query)
	}
	if query := view.Queries[1]; query.Stage != 2 || query.Order != 2 || query.Enabled || query.AddedAt != "" || query.DisabledReason == "" {
		t.Fatalf("paused dependent lost its place in the full plan: %+v", query)
	}
}
