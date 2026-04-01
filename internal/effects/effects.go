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
	None    EffectSet = 0
	DBRead  EffectSet = 1 << iota
	DBWrite
	Net
	AI
	Agent
	// DB is the parent of DBRead and DBWrite — used only in Parse expansion.
	DB EffectSet = DBRead | DBWrite
)

// Has reports whether e contains the flag f.
func (e EffectSet) Has(f EffectSet) bool {
	return e&f == f
}

// String returns a human-readable representation of the effect set.
func (e EffectSet) String() string {
	if e == None {
		return "none"
	}
	var parts []string
	if e.Has(DBRead) {
		parts = append(parts, "DB.read")
	}
	if e.Has(DBWrite) {
		parts = append(parts, "DB.write")
	}
	if e.Has(Net) {
		parts = append(parts, "Net")
	}
	if e.Has(AI) {
		parts = append(parts, "AI")
	}
	if e.Has(Agent) {
		parts = append(parts, "Agent")
	}
	return strings.Join(parts, "|")
}

// Parse converts a slice of AST effect expressions into an EffectSet bitmask.
// Unknown effect names are silently ignored (the type checker catches them).
func Parse(exprs []ast.EffectExpr) EffectSet {
	var result EffectSet
	for _, e := range exprs {
		switch e.Name {
		case "DB":
			result |= DB
		case "DB.read":
			result |= DBRead
		case "DB.write":
			result |= DBWrite
		case "Net":
			result |= Net
		case "AI":
			result |= AI
		case "Agent":
			result |= Agent
		}
	}
	return result
}
