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

// Package querycheck checks query files without connecting to a database.
package querycheck

import (
	"errors"
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/specterops/dawgs/cypher/frontend"
	"github.com/specterops/dawgs/cypher/parser"
)

// Input records a graph kind or answer parameter that is written in the query.
// Its line points to the use, so the author can find a missing declaration.
type Input struct {
	Type string
	Name string
	Line int
}

type Result struct {
	Inputs []Input
	// A query without named kinds can read more than its declarations tell us.
	// Recalculate it after every graph change instead of trusting an incomplete list.
	BroadRead bool
}

type inputs struct {
	*parser.BaseCypherListener
	result Result
}

func (s *inputs) EnterOC_NodeLabel(value *parser.OC_NodeLabelContext) {
	s.result.Inputs = append(s.result.Inputs, Input{"node", unquote(value.OC_LabelName().GetText()), value.GetStart().GetLine()})
}
func (s *inputs) EnterOC_RelTypeName(value *parser.OC_RelTypeNameContext) {
	s.result.Inputs = append(s.result.Inputs, Input{"relationship", unquote(value.GetText()), value.GetStart().GetLine()})
}
func (s *inputs) EnterOC_Parameter(value *parser.OC_ParameterContext) {
	s.result.Inputs = append(s.result.Inputs, Input{"query", unquote(strings.TrimPrefix(value.GetText(), "$")) + ".cypher", value.GetStart().GetLine()})
}
func (s *inputs) EnterOC_NodePattern(value *parser.OC_NodePatternContext) {
	if value.OC_NodeLabels() == nil {
		s.result.BroadRead = true
	}
}
func (s *inputs) EnterOC_RelationshipPattern(value *parser.OC_RelationshipPatternContext) {
	if value.OC_RelationshipDetail() == nil || value.OC_RelationshipDetail().OC_RelationshipTypes() == nil {
		s.result.BroadRead = true
	}
}
func (s *inputs) EnterOC_FunctionInvocation(value *parser.OC_FunctionInvocationContext) {
	switch strings.ToLower(value.OC_FunctionName().GetText()) {
	case "labels", "type":
		s.result.BroadRead = true
	}
}
func unquote(value string) string { return strings.ReplaceAll(strings.Trim(value, "`"), "``", "`") }

// Check uses the same complete-query parser and read-only restrictions as
// execution. A separate grammar pass avoids handing a broken parse tree to
// the language reader. Passing this check does not execute or test a query.
func Check(source string) (Result, error) {
	var (
		problems = frontend.NewContext()
		lexer    = parser.NewCypherLexer(antlr.NewInputStream(source))
		reader   *parser.CypherParser
		found    = &inputs{BaseCypherListener: &parser.BaseCypherListener{}}
	)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(problems)
	reader = parser.NewCypherParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	reader.RemoveErrorListeners()
	reader.AddErrorListener(problems)
	tree := reader.OC_Cypher()
	if len(problems.Errors) != 0 {
		return Result{}, fmt.Errorf("file syntax: %w", errors.Join(problems.Errors...))
	}
	antlr.ParseTreeWalkerDefault.Walk(found, tree)
	// Walk the already checked tree with DAWGS's normal language reader and
	// read-only filters. Reusing the tree avoids parsing every query twice and
	// preserves the file's original line numbers, including leading blanks.
	language := frontend.NewContext(&frontend.UpdatingNotAllowedClauseFilter{}, &frontend.ExplicitProcedureInvocationFilter{}, &frontend.ImplicitProcedureInvocationFilter{})
	language.Enter(&frontend.QueryVisitor{})
	antlr.ParseTreeWalkerDefault.Walk(language, tree)
	if err := language.GetErrors(); err != nil {
		return Result{}, fmt.Errorf("language support (shared by Neo4j and PostgreSQL): %w", err)
	}
	return found.result, nil
}
