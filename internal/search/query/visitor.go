package query

type Visitor interface {
	VisitNodes(v Visitor, node []Node)
	VisitOperator(v Visitor, kind operatorKind, operands []Node)
	VisitParameter(v Visitor, field, value string, negated bool)
}

type BaseVisitor struct{}

func (_ *BaseVisitor) VisitNodes(visitor Visitor, nodes []Node) {
	for _, node := range nodes {
		switch v := node.(type) {
		case Parameter:
			visitor.VisitParameter(visitor, v.Field, v.Value, v.Negated)
		case Operator:
			visitor.VisitOperator(visitor, v.Kind, v.Operands)
		}
	}
}

func (_ *BaseVisitor) VisitOperator(visitor Visitor, kind operatorKind, operands []Node) {
	visitor.VisitNodes(visitor, operands)
}

func (_ *BaseVisitor) VisitParameter(visitor Visitor, field, value string, negated bool) {}

type OperatorVisitor struct {
	callback func(kind operatorKind, operands []Node)
	BaseVisitor
}

func (s *OperatorVisitor) VisitOperator(visitor Visitor, kind operatorKind, operands []Node) {
	s.callback(kind, operands)
	visitor.VisitNodes(visitor, operands)
}

// VisitOperator calls f on all operator nodes. f supplies the node's kind and operands.
func VisitOperator(nodes []Node, f func(kind operatorKind, operands []Node)) {
	visitor := &OperatorVisitor{callback: f}
	visitor.VisitNodes(visitor, nodes)
}

type ParameterVisitor struct {
	callback func(field, value string, negated bool)
	BaseVisitor
}

func (s *ParameterVisitor) VisitParameter(visitor Visitor, field, value string, negated bool) {
	s.callback(field, value, negated)
}

// VisitParameter calls f on all parameter nodes. f supplies the node's field,
// value, and whether the value is negated.
func VisitParameter(nodes []Node, f func(field, value string, negated bool)) {
	visitor := &ParameterVisitor{callback: f}
	visitor.VisitNodes(visitor, nodes)
}

type FieldVisitor struct {
	field    string
	callback func(value string, negated bool)
	BaseVisitor
}

func (s *FieldVisitor) VisitParameter(visitor Visitor, field, value string, negated bool) {
	if s.field == field {
		s.callback(value, negated)
	}
}

// VisitField calls f on all parameter nodes whose field matches the field
// argument. f supplies the node's value and whether the value is negated.
func VisitField(nodes []Node, field string, f func(value string, negated bool)) {
	visitor := &FieldVisitor{callback: f, field: field}
	visitor.VisitNodes(visitor, nodes)
}
