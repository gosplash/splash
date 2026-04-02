package ast

import (
	"fmt"

	"gosplash.dev/splash/internal/token"
)

// IntLiteral is an integer literal expression.
type IntLiteral struct {
	Value    int64
	Position token.Position
}

func (n *IntLiteral) Pos() token.Position { return n.Position }
func (n *IntLiteral) String() string      { return fmt.Sprintf("%d", n.Value) }
func (n *IntLiteral) exprNode()           {}

// FloatLiteral is a floating-point literal expression.
type FloatLiteral struct {
	Value    float64
	Position token.Position
}

func (n *FloatLiteral) Pos() token.Position { return n.Position }
func (n *FloatLiteral) String() string      { return fmt.Sprintf("%g", n.Value) }
func (n *FloatLiteral) exprNode()           {}

// StringLiteral is a string literal expression.
type StringLiteral struct {
	Value    string
	Position token.Position
}

func (n *StringLiteral) Pos() token.Position { return n.Position }
func (n *StringLiteral) String() string      { return fmt.Sprintf("%q", n.Value) }
func (n *StringLiteral) exprNode()           {}

// BoolLiteral is a boolean literal expression.
type BoolLiteral struct {
	Value    bool
	Position token.Position
}

func (n *BoolLiteral) Pos() token.Position { return n.Position }
func (n *BoolLiteral) String() string      { return fmt.Sprintf("%t", n.Value) }
func (n *BoolLiteral) exprNode()           {}

// NoneLiteral is the none/null literal.
type NoneLiteral struct {
	Position token.Position
}

func (n *NoneLiteral) Pos() token.Position { return n.Position }
func (n *NoneLiteral) String() string      { return "none" }
func (n *NoneLiteral) exprNode()           {}

// Ident is an identifier reference.
type Ident struct {
	Name     string
	Position token.Position
}

func (n *Ident) Pos() token.Position { return n.Position }
func (n *Ident) String() string      { return n.Name }
func (n *Ident) exprNode()           {}

// BinaryExpr is a binary operator expression.
type BinaryExpr struct {
	Left     Expr
	Op       token.Kind
	Right    Expr
	Position token.Position
}

func (n *BinaryExpr) Pos() token.Position { return n.Position }
func (n *BinaryExpr) String() string      { return "BinaryExpr" }
func (n *BinaryExpr) exprNode()           {}

// UnaryExpr is a unary operator expression.
type UnaryExpr struct {
	Op       token.Kind
	Operand  Expr
	Position token.Position
}

func (n *UnaryExpr) Pos() token.Position { return n.Position }
func (n *UnaryExpr) String() string      { return "UnaryExpr" }
func (n *UnaryExpr) exprNode()           {}

// CallExpr is a function call expression.
type CallExpr struct {
	Callee   Expr
	Args     []Expr
	TypeArgs []TypeExpr
	Position token.Position
}

func (n *CallExpr) Pos() token.Position { return n.Position }
func (n *CallExpr) String() string      { return "CallExpr" }
func (n *CallExpr) exprNode()           {}

// MemberExpr is a member access expression (obj.member or obj?.member).
type MemberExpr struct {
	Object   Expr
	Member   string
	Optional bool
	Position token.Position
}

func (n *MemberExpr) Pos() token.Position { return n.Position }
func (n *MemberExpr) String() string      { return fmt.Sprintf(".%s", n.Member) }
func (n *MemberExpr) exprNode()           {}

// IndexExpr is an index/subscript expression (obj[idx]).
type IndexExpr struct {
	Object   Expr
	Index    Expr
	Position token.Position
}

func (n *IndexExpr) Pos() token.Position { return n.Position }
func (n *IndexExpr) String() string      { return "IndexExpr" }
func (n *IndexExpr) exprNode()           {}

// NullCoalesceExpr is the ?? operator (left ?? right).
type NullCoalesceExpr struct {
	Left     Expr
	Right    Expr
	Position token.Position
}

func (n *NullCoalesceExpr) Pos() token.Position { return n.Position }
func (n *NullCoalesceExpr) String() string      { return "NullCoalesceExpr" }
func (n *NullCoalesceExpr) exprNode()           {}

// StructField is a single named field value in a struct literal.
type StructField struct {
	Name  string
	Value Expr
	Pos   token.Position
}

// StructLiteral is a struct literal expression.
type StructLiteral struct {
	TypeName string
	Fields   []StructField
	Position token.Position
}

func (n *StructLiteral) Pos() token.Position { return n.Position }
func (n *StructLiteral) String() string      { return fmt.Sprintf("%s{...}", n.TypeName) }
func (n *StructLiteral) exprNode()           {}

// ListLiteral is a list/array literal expression.
type ListLiteral struct {
	Elements []Expr
	Position token.Position
}

func (n *ListLiteral) Pos() token.Position { return n.Position }
func (n *ListLiteral) String() string      { return "ListLiteral" }
func (n *ListLiteral) exprNode()           {}

// MatchArm is one arm (pattern + body) of a match expression.
type MatchArm struct {
	Pattern Expr
	Body    Expr
	Pos     token.Position
}

// MatchExpr is a pattern-matching expression.
type MatchExpr struct {
	Subject  Expr
	Arms     []MatchArm
	Position token.Position
}

func (n *MatchExpr) Pos() token.Position { return n.Position }
func (n *MatchExpr) String() string      { return "MatchExpr" }
func (n *MatchExpr) exprNode()           {}

// ClosureExpr is an anonymous function (lambda) expression.
type ClosureExpr struct {
	Params   []Param
	Body     Expr
	Position token.Position
}

func (n *ClosureExpr) Pos() token.Position { return n.Position }
func (n *ClosureExpr) String() string      { return "ClosureExpr" }
func (n *ClosureExpr) exprNode()           {}
