package ast

import (
	"fmt"

	"gosplash.dev/splash/internal/token"
)

// TypeExpr represents a type expression in the AST.
type TypeExpr interface {
	Node
	typeExprNode()
}

// NamedTypeExpr is a named type, optionally with type arguments (e.g. List<T>).
type NamedTypeExpr struct {
	Name     string
	TypeArgs []TypeExpr
	Position token.Position
}

func (n *NamedTypeExpr) Pos() token.Position { return n.Position }
func (n *NamedTypeExpr) String() string      { return n.Name }
func (n *NamedTypeExpr) typeExprNode()       {}

// OptionalTypeExpr represents a nullable/optional type (e.g. T?).
type OptionalTypeExpr struct {
	Inner    TypeExpr
	Position token.Position
}

func (o *OptionalTypeExpr) Pos() token.Position { return o.Position }
func (o *OptionalTypeExpr) String() string      { return o.Inner.String() + "?" }
func (o *OptionalTypeExpr) typeExprNode()       {}

// FnTypeExpr represents a function type (e.g. fn(A, B) -> C).
type FnTypeExpr struct {
	Params     []TypeExpr
	ReturnType TypeExpr
	Position   token.Position
}

func (f *FnTypeExpr) Pos() token.Position { return f.Position }
func (f *FnTypeExpr) String() string      { return "fn(...) -> ..." }
func (f *FnTypeExpr) typeExprNode()       {}

// TypeParam is a generic type parameter with optional constraints.
type TypeParam struct {
	Name        string
	Constraints []string
	Pos         token.Position
}

// Param is a function parameter.
type Param struct {
	Name     string
	Type     TypeExpr
	Default  Expr
	Variadic bool
	Pos      token.Position
}

// EffectExpr names an effect (e.g. "DB", "DB.read").
type EffectExpr struct {
	Name string
	Pos  token.Position
}

// ModuleDecl declares the module name for a file.
type ModuleDecl struct {
	Name        string
	Annotations []Annotation
	Position    token.Position
}

func (m *ModuleDecl) Pos() token.Position { return m.Position }
func (m *ModuleDecl) String() string      { return fmt.Sprintf("module %s", m.Name) }
func (m *ModuleDecl) declNode()           {}

// UseDecl is an import declaration.
type UseDecl struct {
	Path     string
	Alias    string
	Position token.Position
}

func (u *UseDecl) Pos() token.Position { return u.Position }
func (u *UseDecl) String() string      { return fmt.Sprintf("use %s", u.Path) }
func (u *UseDecl) declNode()           {}

// FunctionDecl declares a named function.
type FunctionDecl struct {
	Name        string
	TypeParams  []TypeParam
	Params      []Param
	ReturnType  TypeExpr
	Effects     []EffectExpr
	Body        *BlockStmt
	Annotations []Annotation
	IsAsync     bool
	Position    token.Position
}

func (f *FunctionDecl) Pos() token.Position { return f.Position }
func (f *FunctionDecl) String() string      { return fmt.Sprintf("fn %s", f.Name) }
func (f *FunctionDecl) declNode()           {}

// FieldDecl is a field within a type declaration.
type FieldDecl struct {
	Name        string
	Type        TypeExpr
	Default     Expr
	Annotations []Annotation
	Position    token.Position
}

func (f *FieldDecl) Pos() token.Position { return f.Position }
func (f *FieldDecl) String() string      { return fmt.Sprintf("field %s", f.Name) }

// TypeDecl declares a named record/struct type.
type TypeDecl struct {
	Name        string
	TypeParams  []TypeParam
	Fields      []FieldDecl
	Annotations []Annotation
	Position    token.Position
}

func (t *TypeDecl) Pos() token.Position { return t.Position }
func (t *TypeDecl) String() string      { return fmt.Sprintf("type %s", t.Name) }
func (t *TypeDecl) declNode()           {}

// EnumVariant is one variant of an enum, with an optional payload type.
type EnumVariant struct {
	Name    string
	Payload TypeExpr
	Pos     token.Position
}

// EnumDecl declares a named enum type.
type EnumDecl struct {
	Name        string
	Variants    []EnumVariant
	Annotations []Annotation
	Position    token.Position
}

func (e *EnumDecl) Pos() token.Position { return e.Position }
func (e *EnumDecl) String() string      { return fmt.Sprintf("enum %s", e.Name) }
func (e *EnumDecl) declNode()           {}

// ConstraintMethod describes one method signature in a constraint.
type ConstraintMethod struct {
	Name       string
	Params     []Param
	ReturnType TypeExpr
	IsStatic   bool
	Pos        token.Position
}

// ConstraintDecl declares a named constraint (interface/trait).
type ConstraintDecl struct {
	Name        string
	TypeParams  []TypeParam
	Methods     []ConstraintMethod
	Annotations []Annotation
	Position    token.Position
}

func (c *ConstraintDecl) Pos() token.Position { return c.Position }
func (c *ConstraintDecl) String() string      { return fmt.Sprintf("constraint %s", c.Name) }
func (c *ConstraintDecl) declNode()           {}
