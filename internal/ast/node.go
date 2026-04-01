package ast

import "gosplash.dev/splash/internal/token"

type Node interface {
	Pos() token.Position
	String() string
}

type Decl interface {
	Node
	declNode()
}

type Expr interface {
	Node
	exprNode()
}

type Stmt interface {
	Node
	stmtNode()
}

type File struct {
	Module       *ModuleDecl
	Exposes      []string
	Uses         []*UseDecl
	Declarations []Decl
	Position     token.Position
}

func (f *File) Pos() token.Position { return f.Position }
func (f *File) String() string      { return "File" }
