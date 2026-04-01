package codegen

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/token"
)

func (e *Emitter) emitExprStr(expr ast.Expr) string {
	if expr == nil {
		return "nil"
	}
	switch ex := expr.(type) {
	case *ast.IntLiteral:
		return fmt.Sprintf("%d", ex.Value)
	case *ast.FloatLiteral:
		return fmt.Sprintf("%g", ex.Value)
	case *ast.StringLiteral:
		return fmt.Sprintf("%q", ex.Value)
	case *ast.BoolLiteral:
		if ex.Value {
			return "true"
		}
		return "false"
	case *ast.NoneLiteral:
		return "nil"
	case *ast.Ident:
		return ex.Name
	case *ast.BinaryExpr:
		return e.emitBinaryExpr(ex)
	case *ast.UnaryExpr:
		return e.emitUnaryExpr(ex)
	case *ast.CallExpr:
		return e.emitCallExpr(ex)
	case *ast.MemberExpr:
		return fmt.Sprintf("%s.%s", e.emitExprStr(ex.Object), ex.Member)
	case *ast.IndexExpr:
		return fmt.Sprintf("%s[%s]", e.emitExprStr(ex.Object), e.emitExprStr(ex.Index))
	case *ast.NullCoalesceExpr:
		e.needsCoalesce = true
		return fmt.Sprintf("splashCoalesce(%s, %s)", e.emitExprStr(ex.Left), e.emitExprStr(ex.Right))
	case *ast.StructLiteral:
		return e.emitStructLiteral(ex)
	case *ast.ListLiteral:
		return e.emitListLiteral(ex)
	case *ast.ClosureExpr:
		return e.emitClosureExpr(ex)
	case *ast.MatchExpr:
		// v0.1: match not supported in codegen
		return `(func() any { panic("match: not supported in v0.1 codegen") })()`
	}
	return "nil"
}

var binaryOpMap = map[token.Kind]string{
	token.PLUS:    "+",
	token.MINUS:   "-",
	token.STAR:    "*",
	token.SLASH:   "/",
	token.PERCENT: "%",
	token.EQ:      "==",
	token.NEQ:     "!=",
	token.LT:      "<",
	token.LTE:     "<=",
	token.GT:      ">",
	token.GTE:     ">=",
	token.AND_AND: "&&",
	token.OR_OR:   "||",
}

func (e *Emitter) emitBinaryExpr(ex *ast.BinaryExpr) string {
	op, ok := binaryOpMap[ex.Op]
	if !ok {
		op = "/* unknown op */"
	}
	return fmt.Sprintf("(%s %s %s)", e.emitExprStr(ex.Left), op, e.emitExprStr(ex.Right))
}

func (e *Emitter) emitUnaryExpr(ex *ast.UnaryExpr) string {
	switch ex.Op {
	case token.MINUS:
		return fmt.Sprintf("(-%s)", e.emitExprStr(ex.Operand))
	case token.BANG:
		return fmt.Sprintf("(!%s)", e.emitExprStr(ex.Operand))
	}
	return e.emitExprStr(ex.Operand)
}

func (e *Emitter) emitCallExpr(ex *ast.CallExpr) string {
	var args []string
	for _, arg := range ex.Args {
		args = append(args, e.emitExprStr(arg))
	}
	return fmt.Sprintf("%s(%s)", e.emitExprStr(ex.Callee), strings.Join(args, ", "))
}

func (e *Emitter) emitStructLiteral(ex *ast.StructLiteral) string {
	var fields []string
	for _, f := range ex.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", f.Name, e.emitExprStr(f.Value)))
	}
	return fmt.Sprintf("%s{%s}", ex.TypeName, strings.Join(fields, ", "))
}

func (e *Emitter) emitListLiteral(ex *ast.ListLiteral) string {
	var elems []string
	for _, el := range ex.Elements {
		elems = append(elems, e.emitExprStr(el))
	}
	return fmt.Sprintf("[]any{%s}", strings.Join(elems, ", "))
}

func (e *Emitter) emitClosureExpr(ex *ast.ClosureExpr) string {
	var params []string
	for _, p := range ex.Params {
		params = append(params, fmt.Sprintf("%s %s", p.Name, e.emitTypeName(p.Type)))
	}
	body := e.emitExprStr(ex.Body)
	return fmt.Sprintf("func(%s) any { return %s }", strings.Join(params, ", "), body)
}
