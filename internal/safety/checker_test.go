// internal/safety/checker_test.go
package safety_test

import (
	"testing"

	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/safety"
)

func check(src string) []diagnostic.Diagnostic {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	g := callgraph.Build(file)
	c := safety.New()
	return c.Check(file, g)
}


func hasError(diags []diagnostic.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == diagnostic.Error {
			return true
		}
	}
	return false
}

func TestRedline_AgentCannotReach(t *testing.T) {
	src := `
module foo
fn run_agent() -> String needs Agent { return dangerous() }
@redline
fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent-reachable function is @redline")
	}
}

func TestRedline_NonAgentCanCall(t *testing.T) {
	src := `
module foo
fn safe_caller() -> String { return dangerous() }
@redline
fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: non-agent can call @redline: %v", diags)
	}
}

func TestRedline_TransitiveViolation(t *testing.T) {
	// agent -> helper -> dangerous: should still error
	src := `
module foo
fn run_agent() -> String needs Agent { return helper() }
fn helper() -> String { return dangerous() }
@redline
fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent transitively reaches @redline function")
	}
}

func TestRedline_NoAgent_NoError(t *testing.T) {
	src := `
module foo
@redline
fn dangerous() -> String { return "ok" }
fn other() -> String { return "hi" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: no agent, no violation: %v", diags)
	}
}
