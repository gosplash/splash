package typechecker

import "gosplash.dev/splash/internal/types"

// Env is a lexically-scoped symbol table mapping names to types.
type Env struct {
	parent  *Env
	symbols map[string]types.Type
}

func NewEnv(parent *Env) *Env {
	return &Env{parent: parent, symbols: make(map[string]types.Type)}
}

func (e *Env) Set(name string, t types.Type) {
	e.symbols[name] = t
}

func (e *Env) Get(name string) (types.Type, bool) {
	if t, ok := e.symbols[name]; ok {
		return t, true
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return nil, false
}
