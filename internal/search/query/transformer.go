package query

import (
	"strings"
)

// Transformer functions for queries.

// LowercaseFieldNames performs strings.ToLower on every field name.
func LowercaseFieldNames(nodes []Node) []Node {
	return MapParameter(nodes, func(field, value string, negated bool) Node {
		return Parameter{Field: strings.ToLower(field), Value: value, Negated: negated}
	})
}

// RightAssociatePatterns rewrites a contiguous and/or pattern expression in a
// query such that operators are right-associative. For example, the following
// query without parentheses is interpreted as follows in the grammar:
//
// repo:foo a or b and c => (repo:foo a) or ((b) and (c))
//
// This function rewrites the above expression as follows:
//
// repo:foo a or b and c => repo:foo (a or b and c)
//
// Any number of field:value parameters may occur before and after the pattern
// expression, but the pattern expression must be contiguous. If there is more
// than one pattern, no rewrite is performed. In the latter case, we _do_ want
// the default interpretation which corresponds more naturally to groupings with
// field parameters, i.e.,
//
// repo:foo a or b or repo:bar c => (repo:foo a) or (b) or (repo:bar c)
func RightAssociatePatterns(nodes []Node) []Node {
	return MapParameter(nodes, func(field, value string, negated bool) Node {
		return Parameter{Field: strings.ToLower(field), Value: value, Negated: negated}
	})
}
