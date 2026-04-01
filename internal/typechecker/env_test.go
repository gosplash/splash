package typechecker_test

import (
	"testing"

	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

func TestEnvGetSet(t *testing.T) {
	env := typechecker.NewEnv(nil)
	env.Set("x", types.String)
	got, ok := env.Get("x")
	if !ok {
		t.Fatal("expected to find x")
	}
	if got != types.String {
		t.Errorf("got %v, want String", got)
	}
}

func TestEnvLexicalScoping(t *testing.T) {
	parent := typechecker.NewEnv(nil)
	parent.Set("x", types.String)
	child := typechecker.NewEnv(parent)
	child.Set("y", types.Int)

	_, ok := child.Get("x") // finds in parent
	if !ok {
		t.Error("child should see parent's x")
	}
	_, ok = parent.Get("y") // parent cannot see child
	if ok {
		t.Error("parent should not see child's y")
	}
}

func TestEnvShadowing(t *testing.T) {
	parent := typechecker.NewEnv(nil)
	parent.Set("x", types.String)
	child := typechecker.NewEnv(parent)
	child.Set("x", types.Int) // shadow

	got, _ := child.Get("x")
	if got != types.Int {
		t.Error("child's x should shadow parent's x")
	}
}
