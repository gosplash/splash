package ast

import "gosplash.dev/splash/internal/token"

// BlockStmt is a sequence of statements enclosed in braces.
type BlockStmt struct {
	Stmts    []Stmt
	Position token.Position
}

func (s *BlockStmt) Pos() token.Position { return s.Position }
func (s *BlockStmt) String() string      { return "BlockStmt" }
func (s *BlockStmt) stmtNode()           {}

// LetStmt is a variable binding statement.
type LetStmt struct {
	Name     string
	Type     TypeExpr
	Value    Expr
	Position token.Position
}

func (s *LetStmt) Pos() token.Position { return s.Position }
func (s *LetStmt) String() string      { return "let " + s.Name }
func (s *LetStmt) stmtNode()           {}

// ReturnStmt returns a value from a function.
type ReturnStmt struct {
	Value    Expr
	Position token.Position
}

func (s *ReturnStmt) Pos() token.Position { return s.Position }
func (s *ReturnStmt) String() string      { return "return" }
func (s *ReturnStmt) stmtNode()           {}

// ExprStmt wraps an expression used as a statement.
type ExprStmt struct {
	Expr     Expr
	Position token.Position
}

func (s *ExprStmt) Pos() token.Position { return s.Position }
func (s *ExprStmt) String() string      { return "ExprStmt" }
func (s *ExprStmt) stmtNode()           {}

// IfStmt is a conditional statement with an optional else branch.
type IfStmt struct {
	Cond     Expr
	Then     *BlockStmt
	Else     Stmt
	Position token.Position
}

func (s *IfStmt) Pos() token.Position { return s.Position }
func (s *IfStmt) String() string      { return "if" }
func (s *IfStmt) stmtNode()           {}

// GuardStmt exits early if the condition is false.
type GuardStmt struct {
	Cond     Expr
	Else     *BlockStmt
	Position token.Position
}

func (s *GuardStmt) Pos() token.Position { return s.Position }
func (s *GuardStmt) String() string      { return "guard" }
func (s *GuardStmt) stmtNode()           {}

// ForStmt iterates over a sequence.
type ForStmt struct {
	Binding  string
	Iter     Expr
	Body     *BlockStmt
	Position token.Position
}

func (s *ForStmt) Pos() token.Position { return s.Position }
func (s *ForStmt) String() string      { return "for " + s.Binding }
func (s *ForStmt) stmtNode()           {}

// AssignStmt assigns a value to a target expression.
type AssignStmt struct {
	Target   Expr
	Value    Expr
	Position token.Position
}

func (s *AssignStmt) Pos() token.Position { return s.Position }
func (s *AssignStmt) String() string      { return "AssignStmt" }
func (s *AssignStmt) stmtNode()           {}
