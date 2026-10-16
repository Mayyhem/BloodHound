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

import (
	"fmt"
	"regexp"
	"strings"
)

// These errors carry the details an author needs to find the bad object:
// which column, property, or connection end, how many objects are affected,
// and a few example identifiers. Each still prints the same plain message as
// before, so logs and existing checks are unchanged.
//
// Example identifiers are object IDs and helper IDs that the author's own
// query returned. The editor shows them to the author, who can already read
// the graph. The shared status report shows only names and counts.

// AnswerValueError reports an answer column holding something other than plain
// values, such as a whole node returned by RETURN n.
type AnswerValueError struct {
	Row    int64  // the first affected row, counting from 1
	Column string // the column name from the query's RETURN
	Cause  error
}

func (s *AnswerValueError) Error() string {
	return fmt.Sprintf("row %d, column %s: %v", s.Row, s.Column, s.Cause)
}
func (s *AnswerValueError) Unwrap() error { return s.Cause }

// HelperPropertyError reports a helper property that cannot be stored, such as
// a name that is empty (null) for some helper objects.
type HelperPropertyError struct {
	Property string
	Count    int      // how many helper objects have this problem
	Examples []string // up to three of their helper IDs
	Cause    error
}

func (s *HelperPropertyError) Error() string {
	return fmt.Sprintf("helper property %s: %v", s.Property, s.Cause)
}
func (s *HelperPropertyError) Unwrap() error { return s.Cause }

// EndpointError reports a proposed connection whose source or target object
// cannot be used: it was not found, several objects share its ID, or its kind
// is not allowed for this connection type by the schema.
type EndpointError struct {
	Kind      string // the connection type, such as PoC_ADCSESC1
	Side      string // "source" or "target"; empty when the pair as a whole is not allowed
	Object    string // the object ID the query returned for that end
	Found     int    // how many objects have that ID
	PairOther string // for a disallowed pair, the other end's object ID
}

func (s *EndpointError) Error() string {
	if s.Side == "" {
		return fmt.Sprintf("%s has an incompatible endpoint pair %q -> %q", s.Kind, s.Object, s.PairOther)
	}
	return fmt.Sprintf("%s endpoint %q must identify exactly one graph node; found %d", s.Kind, s.Object, s.Found)
}

// plainName accepts only simple names such as name, n.objectid, or
// PoC_ADCSESC1, so text copied into a message cannot carry other content.
var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]{0,99}$`)

// SafeName returns the name when it is a plain name, and otherwise a fallback
// such as "a property", so messages never repeat unusual text from a query.
func SafeName(name, fallback string) string {
	if plainName.MatchString(name) {
		return name
	}
	return fallback
}

// ExampleList joins up to three examples and says how many were left out.
func ExampleList(examples []string, total int) string {
	text := strings.Join(examples, ", ")
	if total > len(examples) {
		text += fmt.Sprintf(", and %d more", total-len(examples))
	}
	return text
}

// HelperObjects counts helper objects in words: "1 helper object" or
// "5 helper objects".
func HelperObjects(count int) string {
	if count == 1 {
		return "1 helper object"
	}
	return fmt.Sprintf("%d helper objects", count)
}
