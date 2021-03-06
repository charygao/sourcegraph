package query

import (
	"errors"
)

// isPatternExpression returns true if every leaf node in a tree root at node is
// a search pattern.
func isPatternExpression(nodes []Node) bool {
	result := true
	VisitParameter(nodes, func(field, _ string, _ bool) {
		if field != "" && field != "content" {
			result = false
		}
	})
	return result
}

// processTopLevel processes the top level of a query. It validates that we can
// process the query with respect to and/or expressions on file content, but not
// otherwise for nested parameters.
func processTopLevel(nodes []Node) ([]Node, error) {
	if term, ok := nodes[0].(Operator); ok {
		if term.Kind == And && isPatternExpression([]Node{term}) {
			return nodes, nil
		} else if term.Kind == Or && isPatternExpression([]Node{term}) {
			return nodes, nil
		} else if term.Kind == And {
			return term.Operands, nil
		} else if term.Kind == Concat {
			return nodes, nil
		} else {
			return nil, errors.New("cannot evaluate: unable to partition pure search pattern")
		}
	}
	return nodes, nil
}

// PartitionSearchPattern partitions an and/or query into (1) a single search
// pattern expression and (2) other parameters that scope the evaluation of
// search patterns (e.g., to repos, files, etc.). It validates that a query
// contains at most one search pattern expression and that scope parameters do
// not contain nested expressions.
func PartitionSearchPattern(nodes []Node) (parameters []Node, pattern Node, err error) {
	if len(nodes) == 1 {
		nodes, err = processTopLevel(nodes)
		if err != nil {
			return nil, nil, err
		}
	}

	var patterns []Node
	for _, node := range nodes {
		if isPatternExpression([]Node{node}) {
			patterns = append(patterns, node)
		} else if term, ok := node.(Parameter); ok {
			parameters = append(parameters, term)
		} else {
			return nil, nil, errors.New("cannot evaluate: unable to partition pure search pattern")
		}
	}
	if len(patterns) > 1 {
		pattern = Operator{Kind: And, Operands: patterns}
	} else if len(patterns) == 1 {
		pattern = patterns[0]
	}

	return parameters, pattern, nil
}

// isPureSearchPattern implements a heuristic that returns true if buf, possibly
// containing whitespace or balanced parentheses, can be treated as a search
// pattern in the and/or grammar.
func isPureSearchPattern(buf []byte) bool {
	// Check if the balanced string we scanned is perhaps an and/or expression by parsing without the heuristic.
	try := &parser{buf: buf, heuristic: false}
	result, err := try.parseOr()
	if err != nil {
		// This is not an and/or expression, but it is balanced. It
		// could be, e.g., (foo or). Reject this sort of pattern for now.
		return false
	}
	if try.balanced != 0 {
		return false
	}
	if containsAndOrExpression(result) {
		// The balanced string is an and/or expression in our grammar,
		// so it cannot be interpreted as a search pattern.
		return false
	}
	if !isPatternExpression(newOperator(result, Concat)) {
		// The balanced string contains other parameters, like
		// "repo:foo", which are not search patterns.
		return false
	}
	return true
}
