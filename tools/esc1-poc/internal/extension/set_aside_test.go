// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package extension_test

import (
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// The steps, and why each is or is not set aside when failed.cypher fails:
//
//	input.cypher      prerequisite of the failed step  kept
//	failed.cypher     failed                           set aside
//	rows.cypher       uses its rows                    set aside
//	peer.cypher       saves PoC_Link, like rows.cypher set aside
//	reader.cypher     reads PoC_Link from the graph    set aside
//	helper_a.cypher   makes PoC_Input for the failed   kept (a prerequisite)
//	helper_b.cypher   makes PoC_Helper, like helper_c  set aside
//	helper_c.cypher   makes PoC_Helper, uses failed    set aside
//	other.cypher      unrelated                        kept
func setAsidePlan() extension.Plan {
	return extension.Plan{
		Schema: extension.Schema{
			Name:  "set_aside",
			Nodes: []extension.NodeBinding{{Kind: "PoC_Helper", Query: "helper_b.cypher"}, {Kind: "PoC_Helper", Query: "helper_c.cypher"}, {Kind: "PoC_Input", Query: "helper_a.cypher"}},
			Relationships: []extension.RelationshipBinding{
				{Kind: "PoC_Link", Query: "rows.cypher"},
				{Kind: "PoC_Link", Query: "peer.cypher"},
				{Kind: "PoC_Other", Query: "other.cypher"},
			},
		},
		Queries: []extension.Query{
			{Name: "input.cypher", Parameter: "input"},
			{Name: "helper_a.cypher", Parameter: "helper_a"},
			{Name: "failed.cypher", Parameter: "failed", Dependencies: []extension.Dependency{{Type: "query", Name: "input.cypher"}, {Type: "node", Name: "PoC_Input"}}},
			{Name: "rows.cypher", Parameter: "rows", Dependencies: []extension.Dependency{{Type: "query", Name: "failed.cypher"}}},
			{Name: "peer.cypher", Parameter: "peer"},
			{Name: "reader.cypher", Parameter: "reader", Dependencies: []extension.Dependency{{Type: "relationship", Name: "PoC_Link"}}},
			{Name: "helper_b.cypher", Parameter: "helper_b"},
			{Name: "helper_c.cypher", Parameter: "helper_c", Dependencies: []extension.Dependency{{Type: "query", Name: "failed.cypher"}}},
			{Name: "other.cypher", Parameter: "other", Dependencies: []extension.Dependency{{Type: "query", Name: "input.cypher"}}},
		},
	}
}

func TestSetAsideFollowsUsersAndSharedTypesButNotInputs(t *testing.T) {
	var plan = setAsidePlan()
	expected := []string{"failed.cypher", "rows.cypher", "peer.cypher", "reader.cypher", "helper_b.cypher", "helper_c.cypher"}
	if actual := plan.SetAside([]string{"failed.cypher"}, nil); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("set aside %v; expected %v", actual, expected)
	}
}

func TestSetAsideForAFailedTypeIncludesEveryProducer(t *testing.T) {
	var plan = setAsidePlan()
	expected := []string{"rows.cypher", "peer.cypher", "reader.cypher"}
	if actual := plan.SetAside(nil, []string{"PoC_Link"}); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("set aside %v; expected %v", actual, expected)
	}
}
