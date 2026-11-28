// internal/effects/effects.go
// Package effects defines the EffectSet bitmask type for Splash's effect system.
package effects

import (
	"strings"

	"gosplash.dev/splash/internal/ast"
)

// EffectSet is a bitmask of effects declared on a function.
type EffectSet uint32

const (
	None   EffectSet = 0
	DBRead EffectSet = 1 << iota
	DBWrite
	Net
	AI
	Agent
	DBAdmin
	FS
	Exec
	Cache
	Secrets
	Queue
	Metric
	// DB is the parent of DBRead and DBWrite — used only in Parse expansion.
	DB EffectSet = DBRead | DBWrite
)

var atomicEffects = [...]struct {
	name string
	flag EffectSet
}{
	{"DB.read", DBRead}, {"DB.write", DBWrite}, {"DB.admin", DBAdmin},
	{"Net", Net}, {"AI", AI}, {"Agent", Agent}, {"FS", FS}, {"Exec", Exec},
	{"Cache", Cache}, {"Secrets", Secrets}, {"Queue", Queue}, {"Metric", Metric},
}

// Lookup resolves a label in the declared effect vocabulary.
func Lookup(name string) (EffectSet, bool) {
	if name == "DB" {
		return DB, true
	}
	for _, effect := range atomicEffects {
		if name == effect.name {
			return effect.flag, true
		}
	}
	return None, false
}

// Has reports whether e contains the flag f.
func (e EffectSet) Has(f EffectSet) bool {
	return e&f == f
}

// String returns a human-readable representation of the effect set.
func (e EffectSet) String() string {
	if e == None {
		return "none"
	}
	return strings.Join(e.labels(), "|")
}

func (e EffectSet) labels() []string {
	var parts []string
	for _, effect := range atomicEffects {
		if e.Has(effect.flag) {
			parts = append(parts, effect.name)
		}
	}
	return parts
}

// Parse converts a slice of AST effect expressions into an EffectSet bitmask.
// Unknown effect names are silently ignored (the type checker catches them).
func Parse(exprs []ast.EffectExpr) EffectSet {
	var result EffectSet
	for _, e := range exprs {
		flag, _ := Lookup(e.Name)
		result |= flag
	}
	return result
}
