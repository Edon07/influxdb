package storage

import (
	"errors"

	"regexp"

	"github.com/influxdata/influxdb/influxql"
)

func NodeToExpr(node *Node) (influxql.Expr, error) {
	var v nodeToExprVisitor
	WalkNode(&v, node)
	if err := v.Err(); err != nil {
		return nil, err
	}

	if len(v.exprs) != 1 {
		return nil, errors.New("invalid expression")
	}

	return v.exprs[0], nil
}

type nodeToExprVisitor struct {
	exprs []influxql.Expr
	err   error
}

func (v *nodeToExprVisitor) Err() error {
	return v.err
}

func (v *nodeToExprVisitor) pop() influxql.Expr {
	if len(v.exprs) == 0 {
		panic("stack empty")
	}

	var top influxql.Expr
	top, v.exprs = v.exprs[len(v.exprs)-1], v.exprs[:len(v.exprs)-1]
	return top
}

func (v *nodeToExprVisitor) pop2() (lhs, rhs influxql.Expr) {
	rhs = v.exprs[len(v.exprs)-1]
	lhs = v.exprs[len(v.exprs)-2]
	v.exprs = v.exprs[:len(v.exprs)-2]
	return lhs, rhs
}

func (v *nodeToExprVisitor) Visit(n *Node) NodeVisitor {
	if v.err != nil {
		return nil
	}

	switch n.NodeType {
	case NodeTypeGroupExpression:
		if len(n.Children) > 1 {
			op := influxql.AND
			if n.GetLogical() == LogicalOr {
				op = influxql.OR
			}

			WalkNode(v, n.Children[0])
			if v.err != nil {
				return nil
			}

			for i := 1; i < len(n.Children); i++ {
				WalkNode(v, n.Children[i])
				if v.err != nil {
					return nil
				}

				lhs, rhs := v.pop2()
				v.exprs = append(v.exprs, &influxql.BinaryExpr{LHS: lhs, Op: op, RHS: rhs})
			}
			v.exprs = append(v.exprs, &influxql.ParenExpr{Expr: v.pop()})
			return nil
		}

	case NodeTypeBooleanExpression:
		walkChildren(v, n)

		if len(v.exprs) < 2 {
			v.err = errors.New("BooleanExpression expects two children")
			return nil
		}

		lhs, rhs := v.pop2()
		be := &influxql.BinaryExpr{LHS: lhs, RHS: rhs}
		switch n.GetComparison() {
		case ComparisonEqual:
			be.Op = influxql.EQ
		case ComparisonNotEqual:
			be.Op = influxql.NEQ
		case ComparisonStartsWith:
			v.err = errors.New("startsWith not implemented")
			return nil
		case ComparisonRegex:
			be.Op = influxql.EQREGEX
		case ComparisonNotRegex:
			be.Op = influxql.NEQREGEX
		}

		v.exprs = append(v.exprs, be)

		return nil

	case NodeTypeRef:
		ref := n.GetRefValue()
		if ref == "_measurement" {
			ref = "_name"
		}

		v.exprs = append(v.exprs, &influxql.VarRef{Val: ref})
		return nil

	case NodeTypeLiteral:
		switch val := n.Value.(type) {
		case *Node_StringValue:
			v.exprs = append(v.exprs, &influxql.StringLiteral{Val: val.StringValue})

		case *Node_RegexValue:
			re, err := regexp.Compile(val.RegexValue)
			if err != nil {
				v.err = err
			}
			v.exprs = append(v.exprs, &influxql.RegexLiteral{Val: re})
			return nil

		case *Node_IntegerValue:
			v.exprs = append(v.exprs, &influxql.IntegerLiteral{Val: val.IntegerValue})

		case *Node_UnsignedValue:
			v.err = errors.New("UnsignedValue not implemented")
			return nil

		case *Node_FloatValue:
			v.exprs = append(v.exprs, &influxql.NumberLiteral{Val: val.FloatValue})

		case *Node_BooleanValue:
			v.exprs = append(v.exprs, &influxql.BooleanLiteral{Val: val.BooleanValue})

		default:
			v.err = errors.New("unexpected literal type")
			return nil
		}

		return nil

	default:
		return v
	}
	return nil
}
