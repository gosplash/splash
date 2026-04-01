// internal/effects/effects_test.go
package effects_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/token"
)

func pos() token.Position { return token.Position{} }

func TestParseEmpty(t *testing.T) {
	got := effects.Parse(nil)
	if got != effects.None {
		t.Errorf("expected None, got %v", got)
	}
}

func TestParseDBExpands(t *testing.T) {
	exprs := []ast.EffectExpr{{Name: "DB", Pos: pos()}}
	got := effects.Parse(exprs)
	if !got.Has(effects.DBRead) || !got.Has(effects.DBWrite) {
		t.Errorf("DB should expand to DBRead|DBWrite, got %v", got)
	}
}

func TestParseDBReadOnly(t *testing.T) {
	exprs := []ast.EffectExpr{{Name: "DB.read", Pos: pos()}}
	got := effects.Parse(exprs)
	if !got.Has(effects.DBRead) {
		t.Errorf("expected DBRead set, got %v", got)
	}
	if got.Has(effects.DBWrite) {
		t.Errorf("expected DBWrite NOT set, got %v", got)
	}
}

func TestParseAgent(t *testing.T) {
	exprs := []ast.EffectExpr{{Name: "Agent", Pos: pos()}}
	got := effects.Parse(exprs)
	if !got.Has(effects.Agent) {
		t.Errorf("expected Agent set, got %v", got)
	}
}

func TestParseMultiple(t *testing.T) {
	exprs := []ast.EffectExpr{
		{Name: "Net", Pos: pos()},
		{Name: "DB.read", Pos: pos()},
	}
	got := effects.Parse(exprs)
	if !got.Has(effects.Net) || !got.Has(effects.DBRead) {
		t.Errorf("expected Net|DBRead, got %v", got)
	}
}

func TestString(t *testing.T) {
	s := effects.Parse([]ast.EffectExpr{{Name: "DB.read", Pos: pos()}}).String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}
