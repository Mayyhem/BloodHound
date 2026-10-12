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

package extension

import (
	"errors"
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/specterops/dawgs/cypher/frontend"
	"github.com/specterops/dawgs/cypher/parser"
)

// BuildReferenceCommit belongs to the deployment, not the author's schema.
// Builds may replace it with -ldflags -X for another verified reference image.
var BuildReferenceCommit = "2cb6dbd37738ffe31e34e1729b3638f3887a93e9"

type queryOutput struct {
	kind   string
	node   bool
	policy ExistingEdgePolicy
}

// Read only the outer RETURN projection using the Cypher lexer/parser. A
// comment, quoted string, WITH alias, or nested RETURN cannot declare output.
// The loader separately checks the complete query with the shared language reader.
func parseQueryOutput(contents string) (*queryOutput, error) {
	var (
		errorContext = frontend.NewContext()
		lexer        = parser.NewCypherLexer(antlr.NewInputStream(contents))
		depth        int
		returnStart  = -1
		returnCount  int
		terminated   bool
		previous     string
		expressions  = map[string][]antlr.Token{}
		policy       ExistingEdgePolicy
	)
	for _, line := range strings.Split(contents, "\n") {
		comment := strings.TrimSpace(line)
		if !strings.HasPrefix(comment, "//") {
			continue
		}
		comment = strings.TrimSpace(strings.TrimPrefix(comment, "//"))
		if !strings.HasPrefix(comment, "existing-edge-policy:") {
			continue
		}
		if policy != "" {
			return nil, fmt.Errorf("existing-edge-policy may appear only once")
		}
		policy = ExistingEdgePolicy(strings.TrimSpace(strings.TrimPrefix(comment, "existing-edge-policy:")))
		if !policy.valid() {
			return nil, fmt.Errorf("invalid existing-edge-policy %q", policy)
		}
	}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errorContext)
	for _, token := range lexer.GetAllTokens() {
		if terminated && token.GetTokenType() != parser.CypherLexerSP {
			return nil, fmt.Errorf("a query file must contain one statement")
		}
		if depth == 0 && token.GetText() == ";" {
			terminated = true
		}
		if depth == 0 && previous != "." && token.GetTokenType() == parser.CypherLexerRETURN {
			returnStart = token.GetStart()
			returnCount++
		}
		switch token.GetText() {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if token.GetTokenType() != parser.CypherLexerSP {
			previous = token.GetText()
		}
	}
	if len(errorContext.Errors) > 0 {
		return nil, errors.Join(errorContext.Errors...)
	}
	if returnStart < 0 {
		return nil, fmt.Errorf("query requires a final RETURN")
	}
	lexer = parser.NewCypherLexer(antlr.NewInputStream(string([]rune(contents)[returnStart:])))
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errorContext)
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	queryParser := parser.NewCypherParser(tokens)
	queryParser.RemoveErrorListeners()
	queryParser.AddErrorListener(errorContext)
	projection := queryParser.OC_Return().OC_ProjectionBody().OC_ProjectionItems()
	if len(errorContext.Errors) > 0 {
		// Never infer an output from a recovered, invalid parse tree.
		return nil, errors.Join(errorContext.Errors...)
	}
	for _, item := range projection.AllOC_ProjectionItem() {
		var (
			expression = item.OC_Expression()
			name       = expression.GetText()
			values     []antlr.Token
		)
		if item.OC_Variable() != nil {
			name = strings.Trim(item.OC_Variable().GetText(), "`")
		}
		for index := expression.GetStart().GetTokenIndex(); index <= expression.GetStop().GetTokenIndex(); index++ {
			token := tokens.Get(index)
			if token.GetTokenType() != parser.CypherLexerSP {
				values = append(values, token)
			}
		}
		if _, exists := expressions[name]; exists {
			return nil, fmt.Errorf("duplicate RETURN column %q", name)
		}
		expressions[name] = values
	}
	var (
		relationship = expressions["kind"] != nil
		helper       = expressions["id"] != nil && expressions["kinds"] != nil && expressions["properties"] != nil
	)
	if !relationship && !helper {
		if policy != "" {
			return nil, fmt.Errorf("existing-edge-policy requires a relationship producer with a literal kind")
		}
		return nil, nil
	}
	if returnCount != 1 {
		return nil, fmt.Errorf("output queries require one outer RETURN; UNION output is not supported")
	}
	for tokens.LA(1) == parser.CypherLexerSP || tokens.LT(1).GetText() == ";" {
		tokens.Consume()
	}
	if tokens.LA(1) != antlr.TokenEOF || depth != 0 {
		return nil, fmt.Errorf("invalid final RETURN for output query")
	}
	if projection.GetStart().GetText() == "*" {
		return nil, fmt.Errorf("output queries must name their RETURN columns explicitly")
	}
	if relationship && helper {
		return nil, fmt.Errorf("a query cannot produce both nodes and relationships")
	}
	if helper && (expressions["id"] == nil || expressions["properties"] == nil) {
		return nil, fmt.Errorf("helper output requires id, kinds, and properties")
	}
	var allowed = map[string]bool{"id": helper, "kinds": helper, "properties": true, "source": relationship, "target": relationship, "kind": relationship}
	if relationship && expressions["source"] == nil {
		allowed["left"], allowed["right"] = true, true
	}
	for column := range expressions {
		if !allowed[column] {
			return nil, fmt.Errorf("unexpected output column %q; check the final RETURN", column)
		}
	}
	if relationship {
		if expressions["target"] == nil || (expressions["source"] == nil && (expressions["left"] == nil || expressions["right"] == nil)) {
			return nil, fmt.Errorf("relationship output requires source, target, and a literal kind (or left, right, target, kind for group-overlap)")
		}
		kind, err := outputLiteral(expressions["kind"])
		if err != nil {
			return nil, err
		}
		return &queryOutput{kind: kind, policy: policy}, nil
	}
	if policy != "" {
		return nil, fmt.Errorf("existing-edge-policy is only for relationships")
	}
	values := expressions["kinds"]
	if len(values) != 3 || values[0].GetText() != "[" || values[2].GetText() != "]" {
		return nil, fmt.Errorf("helper kinds must be a one-element list containing a literal kind")
	}
	kind, err := outputLiteral(values[1:2])
	if err != nil {
		return nil, err
	}
	return &queryOutput{kind: kind, node: true}, nil
}

func outputLiteral(tokens []antlr.Token) (string, error) {
	if len(tokens) != 1 || tokens[0].GetTokenType() != parser.CypherLexerStringLiteral {
		return "", fmt.Errorf("output kind must be a literal string in the final RETURN, not a variable, parameter, or expression")
	}
	var text = tokens[0].GetText()
	text = text[1 : len(text)-1]
	if !publishedIdentifier.MatchString(text) {
		return "", fmt.Errorf("output kind %q must be an unescaped PoC_ identifier", text)
	}
	return text, nil
}

func bindQueryOutput(definitions []Definition, query *Query, output *queryOutput) error {
	if output == nil {
		return nil
	}
	for index := range definitions {
		definition := &definitions[index]
		if output.node {
			for kindIndex := range definition.NodeKinds {
				kind := &definition.NodeKinds[kindIndex]
				if kind.Name != output.kind {
					continue
				}
				if kind.PostProcessing != nil && kind.PostProcessing.Query != query.Name {
					return fmt.Errorf("output kind %s has multiple producers", output.kind)
				}
				if kind.PostProcessing == nil {
					kind.PostProcessing = &NodeProcessing{Query: query.Name, IDPrefix: kind.Name + "_"}
				}
				query.OutputKind = output.kind
				return nil
			}
		} else {
			for kindIndex := range definition.RelationshipKinds {
				kind := &definition.RelationshipKinds[kindIndex]
				if kind.Name != output.kind {
					continue
				}
				policy := output.policy
				if policy == "" {
					policy = kind.ExistingEdgePolicy
				}
				if policy == "" {
					policy = ReplaceExisting
				}
				if kind.ExistingEdgePolicy != "" && kind.ExistingEdgePolicy != policy {
					return fmt.Errorf("query and schema disagree on existing-edge-policy for %s", kind.Name)
				}
				if kind.producerPolicy != "" && kind.producerPolicy != policy {
					return fmt.Errorf("producers for %s must agree on existing-edge-policy", kind.Name)
				}
				kind.producerPolicy = policy
				kind.producerQueries = append(kind.producerQueries, query.Name)
				if kind.PostProcessing == nil {
					kind.PostProcessing = &RelationshipProcessing{Query: query.Name}
				}
				query.OutputKind = output.kind
				return nil
			}
		}
	}
	return fmt.Errorf("output kind %s must be declared in a schema's %s", output.kind, map[bool]string{true: "node_kinds", false: "relationship_kinds"}[output.node])
}

// QueryOutputKind identifies a query's declared output without executing it.
// An empty kind denotes a prerequisite rather than a publisher.
func QueryOutputKind(contents string) (string, bool, error) {
	output, err := parseQueryOutput(contents)
	if err != nil || output == nil {
		return "", false, err
	}
	return output.kind, output.node, nil
}

// Output-shape errors refer to the final RETURN instead of the file header.
func finalReturnLine(contents string) int {
	var lexer = parser.NewCypherLexer(antlr.NewInputStream(contents))
	var line = 1
	lexer.RemoveErrorListeners()
	for _, token := range lexer.GetAllTokens() {
		if token.GetTokenType() == parser.CypherLexerRETURN {
			line = token.GetLine()
		}
	}
	return line
}

// Locate a forbidden executable word without counting text inside a comment
// or quoted value. This only runs when presenting an error to the author.
func tokenLine(contents, word string) int {
	var lexer = parser.NewCypherLexer(antlr.NewInputStream(contents))
	lexer.RemoveErrorListeners()
	for _, token := range lexer.GetAllTokens() {
		if strings.EqualFold(token.GetText(), word) {
			return token.GetLine()
		}
	}
	return 1
}

func textLine(contents, needle string) int {
	for index, line := range strings.Split(contents, "\n") {
		if strings.Contains(line, needle) {
			return index + 1
		}
	}
	return 1
}
