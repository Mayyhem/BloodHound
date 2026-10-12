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

// Package testexamples reads the actual author guide for automated tests.
// Keeping the query text in the guide avoids a second copy that could drift.
package testexamples

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

func Tutorial(path string) ([]map[string]string, error) {
	var (
		pattern = regexp.MustCompile("(?s)<!-- tested-example: ([1-4]) ([a-z_.]+) -->\\n```(?:cypher|json)\\n(.*?)\\n```")
		stages  = make([]map[string]string, 4)
	)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	matches := pattern.FindAllStringSubmatch(string(contents), -1)
	if len(matches) != 6 {
		return nil, fmt.Errorf("expected six marked tutorial file listings, found %d", len(matches))
	}
	for _, match := range matches {
		stage, _ := strconv.Atoi(match[1])
		if stages[stage-1] == nil {
			stages[stage-1] = map[string]string{}
		}
		if _, exists := stages[stage-1][match[2]]; exists {
			return nil, fmt.Errorf("duplicate tutorial file %s", match[2])
		}
		stages[stage-1][match[2]] = match[3] + "\n"
	}
	for stage := 1; stage < len(stages); stage++ {
		for name, source := range stages[stage-1] {
			if _, exists := stages[stage][name]; !exists {
				stages[stage][name] = source
			}
		}
	}
	return stages, nil
}
