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
package modelview_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/modelview"
)

const fixture = `{
 "schema":{"name":"example","display_name":"Example","namespace":"PoC","version":"v1"},
 "node_kinds":[{"name":"PoC_A"},{"name":"PoC_B"},{"name":"PoC_Isolated"}],
 "relationship_kinds":[
  {"name":"PoC_CanReach","is_traversable":true,"compatible_kinds":[{"source_kind":"PoC_A","target_kind":"PoC_B"},{"source_kind":"PoC_B","target_kind":"User"}]},
  {"name":"PoC_AlsoReaches","is_traversable":true,"compatible_kinds":[{"source_kind":"PoC_A","target_kind":"PoC_B"}]},
  {"name":"PoC_SelfSupport","is_traversable":false,"compatible_kinds":[{"source_kind":"PoC_A","target_kind":"PoC_A"}]}
 ]
}`

func TestRenderEscapesEmbeddedJSON(t *testing.T) {
	var (
		definition  = strings.Replace(fixture, `"example"`, `"</script><script>window.injected=true</script>&"`, 1)
		html, err   = modelview.Render([]byte(definition))
		page        = string(html)
		startMarker = `<script id="model-definition" type="application/json">`
		start       = strings.Index(page, startMarker)
		decoded     []map[string]any
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "<script>window.injected") || !strings.Contains(page, `\u003c/script\u003e`) {
		t.Fatal("embedded labels must not terminate the JSON script element")
	}
	if start < 0 {
		t.Fatal("missing embedded definition")
	}
	start += len(startMarker)
	if err = json.Unmarshal([]byte(page[start:start+strings.Index(page[start:], "</script>")]), &decoded); err != nil {
		t.Fatalf("embedded definition is not valid JSON: %v", err)
	}
	if decoded[0]["schema"].(map[string]any)["name"] != "</script><script>window.injected=true</script>&" {
		t.Fatal("embedding changed the definition's label")
	}
	for _, forbidden := range []string{"<script src=", "<link ", "__MODEL_JSON__", "__NATIVE_NODE_KINDS__"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("standalone page contains %q", forbidden)
		}
	}
}

func TestRenderRejectsInvalidModels(t *testing.T) {
	var cases = map[string]string{
		"invalid JSON":            "{",
		"null":                    "null",
		"missing arrays":          `{"schema":{"name":"test"}}`,
		"invalid schema name":     strings.Replace(fixture, `"example"`, `12`, 1),
		"missing endpoints":       strings.Replace(fixture, `"compatible_kinds":[{"source_kind":"PoC_A","target_kind":"PoC_B"},{"source_kind":"PoC_B","target_kind":"User"}]`, `"other":[]`, 1),
		"empty endpoints":         strings.Replace(fixture, `"compatible_kinds":[{"source_kind":"PoC_A","target_kind":"PoC_B"},{"source_kind":"PoC_B","target_kind":"User"}]`, `"compatible_kinds":[]`, 1),
		"unknown endpoint":        strings.Replace(fixture, `"target_kind":"PoC_B"`, `"target_kind":"Unknown"`, 1),
		"duplicate relationship":  strings.Replace(fixture, `"PoC_AlsoReaches"`, `"PoC_CanReach"`, 1),
		"duplicate endpoint pair": strings.Replace(fixture, `{"source_kind":"PoC_B","target_kind":"User"}`, `{ "source_kind":"PoC_A", "target_kind":"PoC_B" }`, 1),
		"nonboolean traversal":    strings.Replace(fixture, `"is_traversable":true`, `"is_traversable":"true"`, 1),
		"duplicate local kind":    strings.Replace(fixture, `{"name":"PoC_Isolated"}`, `{"name":"PoC_A"}`, 1),
	}
	for name, definition := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := modelview.Render([]byte(definition)); err == nil {
				t.Fatal("expected invalid model to be rejected")
			}
		})
	}
}

// Exercise the actual browser parser without needing a network or a browser.
// This fixture deliberately includes an isolated kind, an external reference,
// parallel relationships, and a self-loop, and forbids an inferred A→User pair.
func TestBrowserParserPreservesExactEndpointPairs(t *testing.T) {
	var (
		nodePath, lookupError = exec.LookPath("node")
		html, renderError     = modelview.Render([]byte(fixture))
		page                  = string(html)
		start                 = strings.Index(page, "const nativeNodeKinds =")
		end                   = strings.Index(page, "const $ =")
		script                string
	)
	if lookupError != nil {
		t.Skip("Node.js is unavailable; browser parser test requires Node.js")
	}
	if renderError != nil {
		t.Fatal(renderError)
	}
	if start < 0 || end < start {
		t.Fatal("could not locate browser parser")
	}
	script = page[start:end] + `
const assert = require('node:assert/strict');
const definition = ` + fixture + `;
const model = parseModel(definition);
assert.deepEqual(model.nodes.map(node=>node.name), ['PoC_A','PoC_B','PoC_Isolated','User']);
assert.equal(model.byName.get('User').external, true);
assert.equal(model.byName.get('PoC_Isolated').external, false);
assert.deepEqual(model.edges.map(edge=>[edge.source,edge.name,edge.target,edge.traversable]), [
 ['PoC_A','PoC_CanReach','PoC_B',true], ['PoC_B','PoC_CanReach','User',true],
 ['PoC_A','PoC_AlsoReaches','PoC_B',true], ['PoC_A','PoC_SelfSupport','PoC_A',false]
]);
assert.equal(model.edges.some(edge=>edge.source==='PoC_A'&&edge.target==='User'), false);
assert.equal(model.edges.every(edge=>edge.existingEdgePolicy==='Defined by the producing query'), true);
for (const policy of ['replace-existing', 'merge-properties', 'keep-existing']) {
 const explicit = JSON.parse(JSON.stringify(definition));
 explicit.relationship_kinds[0].existing_edge_policy = policy;
 assert.equal(parseModel(explicit).edges[0].existingEdgePolicy, policy);
}
for (const policy of [null, '', false, 1, {}, [], 'unknown']) {
 const invalid = JSON.parse(JSON.stringify(definition));
 invalid.relationship_kinds[0].existing_edge_policy = policy;
 assert.throws(()=>parseModel(invalid), /existing_edge_policy/);
}
for (const endpoints of [undefined, [], [{source_kind:'PoC_A', target_kind:'Unknown'}]]) {
 const invalid = JSON.parse(JSON.stringify(definition));
 invalid.relationship_kinds[0].compatible_kinds = endpoints;
 assert.throws(()=>parseModel(invalid), /compatible_kinds|unknown node kind/);
}
const optionalTraversal = JSON.parse(JSON.stringify(definition));
delete optionalTraversal.relationship_kinds[0].is_traversable;
assert.equal(parseModel(optionalTraversal).edges[0].traversable, false);
for (const change of [
 value => value.relationship_kinds[1].name = value.relationship_kinds[0].name,
 value => value.relationship_kinds[0].compatible_kinds.push({...value.relationship_kinds[0].compatible_kinds[0]}),
 value => value.relationship_kinds[0].is_traversable = null,
]) {
 const invalid = JSON.parse(JSON.stringify(definition));
 change(invalid);
 assert.throws(()=>parseModel(invalid));
}
const noEdges = {...definition, relationship_kinds:[]};
assert.equal(parseModel(noEdges).nodes.length, 3);
assert.equal(parseModel(noEdges).edges.length, 0);
const consumer = JSON.parse(JSON.stringify(definition));
consumer.relationship_kinds[0].compatible_kinds[0].target_kind = 'PoC_Other_Node';
const supplier = {
 schema: {name:'other', display_name:'Other extension', namespace:'PoC_Other', version:'v1'},
 node_kinds:[{name:'PoC_Other_Node',description:'From the defining schema'}],
 relationship_kinds:[{name:'PoC_Other_Back', compatible_kinds:[{source_kind:'PoC_Other_Node',target_kind:'PoC_A'}]}]
};
assert.throws(()=>parseModel(consumer), /open the schema/);
for (const definitions of [[consumer,supplier],[supplier,consumer]]) {
 const combined = parseModel(definitions);
 assert.equal(combined.byName.get('PoC_Other_Node').description, 'From the defining schema');
 assert.equal(combined.edges.some(edge=>edge.source==='PoC_A' && edge.target==='PoC_Other_Node'), true);
 assert.equal(combined.edges.some(edge=>edge.source==='PoC_Other_Node' && edge.target==='PoC_A'), true);
}

`
	if output, err := exec.Command(nodePath, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("browser parser failed: %v\n%s", err, output)
	}
}

// The standard schema defaults an omitted traversal flag to false.
func TestRenderAcceptsOptionalTraversal(t *testing.T) {
	var definition = strings.Replace(fixture, `"is_traversable":true,`, "", 1)
	if _, err := modelview.Render([]byte(definition)); err != nil {
		t.Fatalf("omitted traversal flag should be accepted: %v", err)
	}
}

func TestRenderExistingEdgePolicies(t *testing.T) {
	for _, policy := range []string{`"replace-existing"`, `"merge-properties"`, `"keep-existing"`, `null`, `""`, `false`, `12`, `[]`, `{}`, `"unknown"`} {
		t.Run(policy, func(t *testing.T) {
			definition := strings.Replace(fixture, `"name":"PoC_CanReach",`, `"name":"PoC_CanReach","existing_edge_policy":`+policy+`,`, 1)
			html, err := modelview.Render([]byte(definition))
			valid := policy == `"replace-existing"` || policy == `"merge-properties"` || policy == `"keep-existing"`
			if valid && err != nil {
				t.Fatal(err)
			}
			if !valid && err == nil {
				t.Fatal("invalid explicit policy accepted")
			}
			if valid && !strings.Contains(string(html), "Existing edge policy") {
				t.Fatal("viewer does not show the policy")
			}
		})
	}
}

func TestRenderUsesAccompanyingSchemaForCustomKinds(t *testing.T) {
	var (
		consumer = strings.Replace(fixture, `"target_kind":"PoC_B"`, `"target_kind":"PoC_Other_Node"`, 1)
		supplier = `{"schema":{"name":"other","display_name":"Other","namespace":"PoC_Other","version":"v1"},"node_kinds":[{"name":"PoC_Other_Node"}],"relationship_kinds":[{"name":"PoC_Other_Back","compatible_kinds":[{"source_kind":"PoC_Other_Node","target_kind":"PoC_A"}]}]}`
	)
	if _, err := modelview.Render([]byte(consumer)); err == nil || !strings.Contains(err.Error(), "include the schema") {
		t.Fatal("missing schema not explained", err)
	}
	for _, definitions := range [][][]byte{{[]byte(consumer), []byte(supplier)}, {[]byte(supplier), []byte(consumer)}} {
		if _, err := modelview.Render(definitions...); err != nil {
			t.Fatal(err)
		}
	}
}
