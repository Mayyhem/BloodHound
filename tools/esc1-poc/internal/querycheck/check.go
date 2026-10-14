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
	"regexp"
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
	Inputs       []Input
	Columns      []string
	ColumnsKnown bool
	SharedFields []SharedField
	// A query without named kinds can read more than its declarations tell us.
	// Recalculate it after every graph change instead of trusting an incomplete list.
	BroadRead bool
}

type SharedField struct {
	Parameter string
	Column    string
	Line      int
}

type inputs struct {
	*parser.BaseCypherListener
	result      Result
	rows        map[string]string
	queryDepth  int
	returnCount int
	localDepth  int
}

var simpleAnswerName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (s *inputs) EnterOC_SingleQuery(*parser.OC_SingleQueryContext) { s.queryDepth++ }
func (s *inputs) ExitOC_SingleQuery(*parser.OC_SingleQueryContext)  { s.queryDepth--; s.rows = nil }

// Follow only a direct UNWIND of a shared parameter. WITH and local list
// expressions can change names or values; those cases still need execution
// tests. Skipping an uncertain inference is safer than rejecting valid Cypher.
func (s *inputs) EnterOC_Unwind(value *parser.OC_UnwindContext) {
	if s.queryDepth != 1 {
		return
	}
	if s.rows == nil {
		s.rows = map[string]string{}
	}
	name := unquote(value.OC_Variable().GetText())
	delete(s.rows, name)
	expression := strings.TrimSpace(value.OC_Expression().GetText())
	if strings.HasPrefix(expression, "$") && simpleAnswerName.MatchString(expression[1:]) {
		s.rows[name] = expression[1:]
	}
}
func (s *inputs) EnterOC_With(*parser.OC_WithContext)                                 { s.rows = nil }
func (s *inputs) EnterOC_ListComprehension(*parser.OC_ListComprehensionContext)       { s.localDepth++ }
func (s *inputs) ExitOC_ListComprehension(*parser.OC_ListComprehensionContext)        { s.localDepth-- }
func (s *inputs) EnterOC_Quantifier(*parser.OC_QuantifierContext)                     { s.localDepth++ }
func (s *inputs) ExitOC_Quantifier(*parser.OC_QuantifierContext)                      { s.localDepth-- }
func (s *inputs) EnterOC_LegacyListExpression(*parser.OC_LegacyListExpressionContext) { s.localDepth++ }
func (s *inputs) ExitOC_LegacyListExpression(*parser.OC_LegacyListExpressionContext)  { s.localDepth-- }

func (s *inputs) EnterOC_NonArithmeticOperatorExpression(value *parser.OC_NonArithmeticOperatorExpressionContext) {
	if s.queryDepth != 1 || s.localDepth != 0 || len(value.AllOC_PropertyLookup()) == 0 || len(value.AllOC_ListOperatorExpression()) != 0 {
		return
	}
	variable := value.OC_Atom().OC_Variable()
	if variable == nil {
		return
	}
	parameter := s.rows[unquote(variable.GetText())]
	if parameter == "" {
		return
	}
	property := value.OC_PropertyLookup(0)
	s.result.SharedFields = append(s.result.SharedFields, SharedField{Parameter: parameter, Column: unquote(property.OC_PropertyKeyName().GetText()), Line: property.GetStart().GetLine()})
}

func (s *inputs) EnterOC_Return(value *parser.OC_ReturnContext) {
	if s.queryDepth != 1 {
		return
	}
	s.returnCount++
	s.result.Columns = nil
	s.result.ColumnsKnown = s.returnCount == 1
	projection := value.OC_ProjectionBody().OC_ProjectionItems()
	if strings.HasPrefix(projection.GetText(), "*") {
		s.result.ColumnsKnown = false
	}
	for _, item := range projection.AllOC_ProjectionItem() {
		name := item.OC_Expression().GetText()
		if item.OC_Variable() != nil {
			name = unquote(item.OC_Variable().GetText())
		}
		if !simpleAnswerName.MatchString(name) {
			s.result.ColumnsKnown = false
		}
		s.result.Columns = append(s.result.Columns, name)
	}
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
