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

package ruleservice

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

var reportDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validateExecution(execution *runner.Execution) error {
	if execution == nil {
		return nil
	}
	if !slices.Contains([]string{"not-started", "running", "succeeded", "failed", "skipped"}, execution.Calculation) || !slices.Contains([]string{"not-started", "saving", "succeeded", "failed", "uncertain", "unchanged"}, execution.Save) {
		return fmt.Errorf("invalid calculation or save state")
	}
	if len(execution.Queries) > MaxFiles || len(execution.Phase) > 64 || execution.Elapsed < 0 {
		return fmt.Errorf("invalid execution report")
	}
	if execution.SchemaRevision != "" && !reportDigest.MatchString(execution.SchemaRevision) {
		return fmt.Errorf("invalid schema revision")
	}
	if execution.Save != "succeeded" && execution.Saved != nil {
		return fmt.Errorf("saved counts require a confirmed save")
	}
	if execution.Failure != nil {
		failure := execution.Failure
		if (failure.Query != "" && !queryName.MatchString(failure.Query)) || failure.Stage < 0 || failure.Stage > MaxFiles || len(failure.Phase) > 64 || len(failure.Message) > 2048 || !utf8.ValidString(failure.Message) {
			return fmt.Errorf("invalid failure details")
		}
	}
	var names = map[string]bool{}
	for _, query := range execution.Queries {
		if !queryName.MatchString(query.Name) || names[query.Name] || query.Stage < 1 || query.Stage > MaxFiles || len(query.Reason) > 2048 || !utf8.ValidString(query.Reason) || query.Elapsed < 0 {
			return fmt.Errorf("invalid query report")
		}
		for _, version := range []string{query.Version, query.SchemaVersion} {
			if version != "" {
				if len(version) > 128 {
					return fmt.Errorf("version exceeds its limit")
				}
				if _, err := extension.SchemaVersionMatches(version, ""); err != nil {
					return fmt.Errorf("invalid reported version")
				}
			}
		}
		if query.SchemaPaused && query.State != "disabled" {
			return fmt.Errorf("a schema pause must be reported as disabled")
		}
		names[query.Name] = true
		if !slices.Contains([]string{"waiting", "running", "succeeded", "failed", "skipped", "disabled", "canceled"}, query.State) {
			return fmt.Errorf("invalid query state")
		}
		for _, count := range []*int64{query.Rows, query.Proposed, query.Saved} {
			if count != nil && *count < 0 {
				return fmt.Errorf("query counts cannot be negative")
			}
		}
		if query.Saved != nil && execution.Save != "succeeded" {
			return fmt.Errorf("query saved counts require a confirmed save")
		}
	}
	for _, counts := range []*runner.ChangesCount{&execution.Proposed, execution.Saved} {
		if counts != nil && (counts.Added < 0 || counts.Removed < 0 || counts.Updated < 0 || counts.Unchanged < 0 || counts.Helpers < 0) {
			return fmt.Errorf("change counts cannot be negative")
		}
	}
	return nil
}
