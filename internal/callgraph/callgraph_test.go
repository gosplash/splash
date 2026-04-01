// internal/callgraph/callgraph_test.go
package callgraph_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/token"
)

func parse(src string) *ast.File {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	return file
}

func TestBuildDirectCall(t *testing.T) {
	file := parse(`
module foo
fn a() -> String { return b() }
fn b() -> String { return "hi" }
`)
	g := callgraph.Build(file)
	nodeA := g.Node("a")
	if nodeA == nil {
		t.Fatal("expected node for 'a'")
	}
	if !nodeA.Calls("b") {
		t.Error("expected 'a' to call 'b'")
	}
}

func TestBuildEffects(t *testing.T) {
	file := parse(`
module foo
fn fetch() needs Net -> String { return "ok" }
`)
	g := callgraph.Build(file)
	node := g.Node("fetch")
	if node == nil {
		t.Fatal("expected node for 'fetch'")
	}
	if !node.Effects.Has(effects.Net) {
		t.Error("expected Net effect on 'fetch'")
	}
}

func TestAgentRoots(t *testing.T) {
	file := parse(`
module foo
fn run_agent() needs Agent -> String { return "ok" }
fn helper() -> String { return "hi" }
`)
	g := callgraph.Build(file)
	roots := g.AgentRoots()
	if len(roots) != 1 || roots[0] != "run_agent" {
		t.Errorf("expected [run_agent], got %v", roots)
	}
}

func TestReachableTransitive(t *testing.T) {
	file := parse(`
module foo
fn a() -> String { return b() }
fn b() -> String { return c() }
fn c() -> String { return "leaf" }
fn unrelated() -> String { return "x" }
`)
	g := callgraph.Build(file)
	reached := g.Reachable([]string{"a"})
	for _, name := range []string{"a", "b", "c"} {
		if !reached[name] {
			t.Errorf("expected %q in reachable set", name)
		}
	}
	if reached["unrelated"] {
		t.Error("'unrelated' should not be in reachable set")
	}
}

func TestAgentRoots_ToolAnnotation(t *testing.T) {
	src := `module foo
@tool
fn search() -> String { return "results" }
fn helper() -> String { return "hi" }
`
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	g := callgraph.Build(file)
	roots := g.AgentRoots()
	if len(roots) != 1 || roots[0] != "search" {
		t.Errorf("expected [search] as agent root via @tool, got %v", roots)
	}
}

func TestNode_HasAnnotation(t *testing.T) {
	_ = token.Position{} // ensure import used
	file := parse(`
module foo
@redline
fn danger() -> String { return "boom" }
`)
	g := callgraph.Build(file)
	node := g.Node("danger")
	if node == nil {
		t.Fatal("expected node for 'danger'")
	}
	if !node.HasAnnotation(ast.AnnotRedline) {
		t.Error("expected @redline annotation on 'danger'")
	}
}
