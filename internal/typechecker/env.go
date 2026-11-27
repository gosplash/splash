package typechecker

import (
	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/types"
)

// LocalBinding identifies an exact declaration within one analyzed snapshot.
type LocalBinding struct {
	Param *ast.Param
	Let   *ast.LetStmt
}

type binding struct {
	typ       types.Type
	function  *ast.FunctionDecl
	imported  ast.Node
	ambiguous bool
	println   bool
	local     LocalBinding
}

// Env is a lexically-scoped symbol table.
type Env struct {
	parent  *Env
	symbols map[string]binding
}

func NewEnv(parent *Env) *Env {
	return &Env{parent: parent, symbols: make(map[string]binding)}
}

func (e *Env) Set(name string, t types.Type) {
	e.symbols[name] = binding{typ: t}
}

func (e *Env) Get(name string) (types.Type, bool) {
	b, ok := e.lookup(name)
	return b.typ, ok
}

func (e *Env) lookup(name string) (binding, bool) {
	if b, ok := e.symbols[name]; ok {
		return b, true
	}
	if e.parent != nil {
		return e.parent.lookup(name)
	}
	return binding{}, false
}
