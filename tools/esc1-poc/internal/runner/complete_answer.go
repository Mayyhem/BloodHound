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

package runner

import "context"

// CompleteAnswer gives the editor read access to one query's finished answer
// file. It is valid only while the editor's inspection function is running:
// the calculation deletes its temporary files when it finishes.
type CompleteAnswer struct{ file *answerFile }

// Rows reports how many rows the complete answer contains.
func (s CompleteAnswer) Rows() int64 {
	return s.file.rows
}

// Each visits every row and verifies the promised count and checksum. Display
// code may retain a small sample, but it must not stop this completeness check.
func (s CompleteAnswer) Each(ctx context.Context, consume func(map[string]any) error) error {
	return s.file.each(ctx, consume)
}
