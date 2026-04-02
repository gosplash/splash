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

func TestParents_DirectCall(t *testing.T) {
	file := parse(`
module foo
fn agent() needs Agent -> String { return helper() }
fn helper() -> String { return "hi" }
`)
	g := callgraph.Build(file)
	parents := g.Parents([]string{"agent"})

	if parents["agent"] != "" {
		t.Errorf("root should have empty parent, got %q", parents["agent"])
	}
	if parents["helper"] != "agent" {
		t.Errorf("expected helper's parent to be 'agent', got %q", parents["helper"])
	}
}

func TestParents_Transitive(t *testing.T) {
	file := parse(`
module foo
fn agent() needs Agent -> String { return mid() }
fn mid() -> String { return leaf() }
fn leaf() -> String { return "x" }
`)
	g := callgraph.Build(file)
	parents := g.Parents([]string{"agent"})

	if parents["mid"] != "agent" {
		t.Errorf("expected mid's parent to be 'agent', got %q", parents["mid"])
	}
	if parents["leaf"] != "mid" {
		t.Errorf("expected leaf's parent to be 'mid', got %q", parents["leaf"])
	}
}

func TestPathTo_Simple(t *testing.T) {
	parents := map[string]string{
		"agent": "",
		"mid":   "agent",
		"leaf":  "mid",
	}
	got := callgraph.PathTo(parents, "leaf")
	want := []string{"agent", "mid", "leaf"}
	if len(got) != len(want) {
		t.Fatalf("expected path %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d]: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestPathTo_NotReachable(t *testing.T) {
	parents := map[string]string{"root": ""}
	got := callgraph.PathTo(parents, "unreachable")
	if got != nil {
		t.Errorf("expected nil for unreachable target, got %v", got)
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

func TestCallers(t *testing.T) {
	file := parse(`
module foo
@approve
fn charge() -> Int { return 0 }
fn run() -> Int { return charge() }
fn unrelated() -> Int { return 0 }
`)
	g := callgraph.Build(file)

	// Seed the target set — charge is @approve
	targets := map[string]bool{"charge": true}
	callers := g.Callers(targets)

	// charge itself is included
	if !callers["charge"] {
		t.Error("expected Callers to include the target itself")
	}
	// run calls charge directly
	if !callers["run"] {
		t.Error("expected Callers to include 'run' (direct caller)")
	}
	// unrelated is not reachable
	if callers["unrelated"] {
		t.Error("expected Callers to exclude 'unrelated'")
	}
}

func TestCallersTransitive(t *testing.T) {
	file := parse(`
module foo
@approve
fn leaf() { }
fn middle() { leaf() }
fn top() { middle() }
fn outside() { }
`)
	g := callgraph.Build(file)
	callers := g.Callers(map[string]bool{"leaf": true})

	if !callers["leaf"] {
		t.Error("expected leaf in callers")
	}
	if !callers["middle"] {
		t.Error("expected middle in callers (direct caller of leaf)")
	}
	if !callers["top"] {
		t.Error("expected top in callers (transitive caller)")
	}
	if callers["outside"] {
		t.Error("expected outside excluded from callers")
	}
}
