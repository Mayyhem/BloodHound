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

package ruleservice

import (
	"sort"
	"strings"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// PlanView describes declared dependencies and the runner's existing execution
// order. Threads are conservative connected groups, not running goroutines.
type PlanView struct {
	Revision   string          `json:"revision"`
	Name       string          `json:"name"`
	Threads    []ThreadView    `json:"threads"`
	Queries    []QueryView     `json:"queries"`
	Extensions []ExtensionView `json:"extensions"`
}

type ExtensionView struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type ThreadView struct {
	Number       int         `json:"number"`
	SharedInputs []InputView `json:"shared_inputs"`
	Queries      []QueryView `json:"queries"`
}

type InputView struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type PrerequisiteView struct {
	Name      string `json:"name"`
	Parameter string `json:"parameter"`
}

type OutputView struct {
	Type               string                       `json:"type"`
	Kind               string                       `json:"kind"`
	Producers          []string                     `json:"producers,omitempty"`
	IDPrefix           string                       `json:"id_prefix,omitempty"`
	Endpoints          []extension.Endpoint         `json:"endpoints,omitempty"`
	ExistingEdgePolicy extension.ExistingEdgePolicy `json:"existing_edge_policy,omitempty"`
}

type ProcessingView struct {
	Reachability    *extension.Reachability `json:"reachability,omitempty"`
	GroupOverlap    *extension.GroupOverlap `json:"group_overlap,omitempty"`
	BatchDependency string                  `json:"batch_dependency,omitempty"`
}

type QueryView struct {
	Name           string             `json:"name"`
	Extension      string             `json:"extension"`
	ExtensionName  string             `json:"extension_name"`
	AddedAt        string             `json:"added_at"`
	DisabledReason string             `json:"disabled_reason"`
	Enabled        bool               `json:"enabled"`
	Order          int                `json:"order"`
	Stage          int                `json:"stage"`
	Parameter      string             `json:"parameter"`
	Text           string             `json:"text"`
	Inputs         []InputView        `json:"inputs"`
	Prerequisites  []PrerequisiteView `json:"prerequisites"`
	Dependents     []string           `json:"dependents"`
	Outputs        []OutputView       `json:"outputs"`
	Processing     ProcessingView     `json:"processing"`
}

// BuildView keeps actual ordering from plan.Queries. Shared graph inputs, reachability kinds, endpoint kinds,
// and managed producers join groups without inventing scheduling edges.
func BuildView(bundle Bundle, plan extension.Plan) PlanView {
	var (
		view          = PlanView{Revision: bundle.Revision, Name: plan.Schema.Name, Threads: []ThreadView{}, Queries: []QueryView{}, Extensions: []ExtensionView{}}
		queries       = make([]QueryView, len(plan.Queries))
		indexes       = make(map[string]int)
		parents       = make([]int, len(plan.Queries))
		kindUsers     = make(map[InputView][]int)
		inputUsers    = make(map[InputView][]int)
		outputs       = make(map[string][]OutputView)
		producers     = make(map[string][]string)
		threadIndexes = make(map[int]int)
	)
	for _, definition := range plan.Definitions {
		view.Extensions = append(view.Extensions, ExtensionView{Name: definition.Schema.Name, DisplayName: definition.Schema.DisplayName})
	}
	sort.Slice(view.Extensions, func(first, second int) bool { return view.Extensions[first].Name < view.Extensions[second].Name })
	for _, binding := range plan.Schema.Nodes {
		outputs[binding.Query] = append(outputs[binding.Query], OutputView{Type: "node", Kind: binding.Kind, IDPrefix: binding.IDPrefix})
	}
	// Filenames set property precedence independently of completion order.
	// Later filenames win when producers supply different values for one key.
	for _, query := range plan.Queries {
		for _, binding := range plan.Schema.Relationships {
			if binding.Query == query.Name {
				producers[binding.Kind] = append(producers[binding.Kind], query.Name)
			}
		}
	}
	for kind := range producers {
		sort.Strings(producers[kind])
	}
	for _, binding := range plan.Schema.Relationships {
		outputs[binding.Query] = append(outputs[binding.Query], OutputView{Type: "relationship", Kind: binding.Kind, Producers: append([]string(nil), producers[binding.Kind]...), Endpoints: append([]extension.Endpoint(nil), binding.Endpoints...), ExistingEdgePolicy: binding.EffectiveExistingEdgePolicy()})
	}
	for index, query := range plan.Queries {
		var (
			kindReferences  = make(map[InputView]bool)
			inputReferences = make(map[InputView]bool)
		)
		parents[index] = index
		indexes[query.Name] = index
		queries[index] = QueryView{Name: query.Name, Extension: query.Extension, ExtensionName: query.ExtensionName, DisabledReason: query.DisabledReason, Enabled: query.DisabledReason == "", Order: index + 1, Stage: 1, Parameter: query.Parameter, Text: query.Text, Inputs: []InputView{}, Prerequisites: []PrerequisiteView{}, Dependents: []string{}, Outputs: outputs[query.Name], Processing: ProcessingView{Reachability: query.Reach, GroupOverlap: query.Overlap, BatchDependency: query.BatchDependency}}
		if addedAt := bundle.AddedAt[query.Name]; !addedAt.IsZero() {
			queries[index].AddedAt = addedAt.UTC().Format(time.RFC3339)
		}
		if queries[index].Outputs == nil {
			queries[index].Outputs = []OutputView{}
		}
		for _, output := range queries[index].Outputs {
			kindReferences[InputView{Type: output.Type, Name: output.Kind}] = true
			for _, endpoint := range output.Endpoints {
				inputReferences[InputView{Type: "node", Name: endpoint.SourceKind}] = true
				inputReferences[InputView{Type: "node", Name: endpoint.TargetKind}] = true
			}
		}
		for _, dependency := range query.Dependencies {
			if dependency.Type == "query" {
				queries[index].Prerequisites = append(queries[index].Prerequisites, PrerequisiteView{Name: dependency.Name, Parameter: strings.TrimSuffix(dependency.Name, ".cypher")})
			} else {
				input := InputView{Type: dependency.Type, Name: dependency.Name}
				queries[index].Inputs = append(queries[index].Inputs, input)
				inputReferences[input] = true
			}
		}
		if query.Reach != nil {
			for _, kind := range query.Reach.NodeKinds {
				inputReferences[InputView{Type: "node", Name: kind}] = true
			}
			for _, kind := range query.Reach.Relationships {
				inputReferences[InputView{Type: "relationship", Name: kind}] = true
			}
		}
		for input := range inputReferences {
			inputUsers[input] = append(inputUsers[input], index)
			kindReferences[input] = true
		}
		for reference := range kindReferences {
			kindUsers[reference] = append(kindUsers[reference], index)
		}
	}
	for index := range queries {
		for _, prerequisite := range queries[index].Prerequisites {
			if dependencyIndex, exists := indexes[prerequisite.Name]; exists {
				joinGroups(parents, index, dependencyIndex)
				queries[dependencyIndex].Dependents = append(queries[dependencyIndex].Dependents, queries[index].Name)
			}
		}
		// A graph reader also waits for every producer of the type it reads.
		// Keep those waits out of Prerequisites, which lists row parameters.
		for _, prerequisite := range plan.Prerequisites(plan.Queries[index]) {
			if dependencyIndex, exists := indexes[prerequisite]; exists {
				if stage := queries[dependencyIndex].Stage + 1; stage > queries[index].Stage {
					queries[index].Stage = stage
				}
			}
		}
	}
	for _, users := range kindUsers {
		for _, index := range users[1:] {
			joinGroups(parents, users[0], index)
		}
	}
	for index, query := range queries {
		root := groupRoot(parents, index)
		threadIndex, exists := threadIndexes[root]
		if !exists {
			threadIndex = len(view.Threads)
			threadIndexes[root] = threadIndex
			view.Threads = append(view.Threads, ThreadView{Number: threadIndex + 1, SharedInputs: []InputView{}, Queries: []QueryView{}})
		}
		view.Threads[threadIndex].Queries = append(view.Threads[threadIndex].Queries, query)
	}
	for input, users := range inputUsers {
		if len(users) > 1 {
			threadIndex := threadIndexes[groupRoot(parents, users[0])]
			view.Threads[threadIndex].SharedInputs = append(view.Threads[threadIndex].SharedInputs, input)
		}
	}
	for index := range view.Threads {
		sort.Slice(view.Threads[index].SharedInputs, func(first, second int) bool {
			firstInput, secondInput := view.Threads[index].SharedInputs[first], view.Threads[index].SharedInputs[second]
			if firstInput.Type != secondInput.Type {
				return firstInput.Type < secondInput.Type
			}
			return firstInput.Name < secondInput.Name
		})
	}
	view.Queries = queries
	return view
}

// Each query initially represents its own group. Joining groups makes one
// representative point to another; follow those links to find the shared group.
func groupRoot(parents []int, index int) int {
	for parents[index] != index {
		index = parents[index]
	}
	return index
}

func joinGroups(parents []int, first, second int) {
	var (
		firstRoot  = groupRoot(parents, first)
		secondRoot = groupRoot(parents, second)
	)
	if firstRoot < secondRoot {
		parents[secondRoot] = firstRoot
	} else {
		parents[firstRoot] = secondRoot
	}
}
