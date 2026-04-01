# Phase 2: Effect System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Splash effect system — effect set types, call graph construction, and `@redline`/`@approve`/`@containment` enforcement passes.

**Architecture:** Three new packages layered on top of the existing AST and type checker. `internal/effects` defines the EffectSet bitmask type and parses `needs` clauses from AST nodes. `internal/callgraph` walks function bodies to build a directed call graph and compute transitive reachability. `internal/safety` runs the three enforcement passes (redline, approve, containment) against the call graph.

**Tech Stack:** Go 1.21+, existing `gosplash.dev/splash/internal/{ast,diagnostic,token}` packages. No new dependencies.

---

## File Map

| File | Responsibility |
|------|---------------|
| `internal/effects/effects.go` | `EffectSet` bitmask type, effect name constants, `Parse()` from AST |
| `internal/effects/effects_test.go` | EffectSet parsing and bitmask tests |
| `internal/callgraph/callgraph.go` | `Node`, `Graph`, `Build()`, `AgentRoots()`, `Reachable()` |
| `internal/callgraph/callgraph_test.go` | Graph construction and reachability tests |
| `internal/safety/checker.go` | `Checker`, `Check()`, three enforcement sub-passes |
| `internal/safety/checker_test.go` | End-to-end enforcement tests |

---

## Background: Agent Entry Points

An "agent entry point" is any function that:
- Has `Agent` in its `needs` clause, OR
- Is annotated with `@tool` (callable by an external agent)

Every function transitively reachable from an agent entry point is "agent-reachable." The safety passes operate on this reachable set.

---

## Task 1: `internal/effects` — EffectSet

**Files:**
- Create: `internal/effects/effects.go`
- Create: `internal/effects/effects_test.go`

The `EffectSet` is a uint32 bitmask. Effect names are hierarchical — `DB` is a parent that expands to `DBRead | DBWrite`. `Parse()` converts `[]ast.EffectExpr` (which are just name strings) into a bitmask.

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/effects/ 2>&1
```
Expected: compile error — package does not exist yet.

- [ ] **Step 3: Implement `internal/effects/effects.go`**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/effects/ -v 2>&1
```
Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/effects/effects.go internal/effects/effects_test.go
git commit --no-gpg-sign -m "feat: effects package — EffectSet bitmask and Parse()"
```

---

## Task 2: `internal/callgraph` — Graph and Reachability

**Files:**
- Create: `internal/callgraph/callgraph.go`
- Create: `internal/callgraph/callgraph_test.go`

`Build()` walks every `FunctionDecl` in an `ast.File`, records its declared effects and annotations, and collects the names of every `CallExpr` target in the body. `Reachable()` performs a BFS from a set of root function names, returning the set of all transitively reachable function names. `AgentRoots()` finds all functions that are agent entry points (have `Agent` effect or `@tool` annotation).

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/callgraph/ 2>&1
```
Expected: compile error — package does not exist.

- [ ] **Step 3: Implement `internal/callgraph/callgraph.go`**

```go
// Package callgraph builds a directed call graph from a Splash AST and computes reachability.
package callgraph

import (
	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
)

// Node represents a single function in the call graph.
type Node struct {
	Name        string
	Effects     effects.EffectSet
	Annotations []ast.Annotation
	callees     map[string]bool // direct callees by name
}

// Calls reports whether this node directly calls the named function.
func (n *Node) Calls(name string) bool { return n.callees[name] }

// HasAnnotation reports whether this node has the given annotation kind.
func (n *Node) HasAnnotation(kind ast.AnnotationKind) bool {
	for _, a := range n.Annotations {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

// Graph is the complete call graph for a file.
type Graph struct {
	nodes map[string]*Node
}

// Node returns the node for the named function, or nil if not found.
func (g *Graph) Node(name string) *Node { return g.nodes[name] }

// AgentRoots returns the names of all agent entry-point functions:
// those with the Agent effect or annotated with @tool.
func (g *Graph) AgentRoots() []string {
	var roots []string
	for name, node := range g.nodes {
		if node.Effects.Has(effects.Agent) || node.HasAnnotation(ast.AnnotTool) {
			roots = append(roots, name)
		}
	}
	return roots
}

// Reachable performs a BFS from the given root function names and returns
// the set of all transitively reachable function names (roots included).
func (g *Graph) Reachable(roots []string) map[string]bool {
	visited := make(map[string]bool)
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		node := g.nodes[cur]
		if node == nil {
			continue
		}
		for callee := range node.callees {
			if !visited[callee] {
				queue = append(queue, callee)
			}
		}
	}
	return visited
}

// Build constructs a call graph by walking all function declarations in file.
func Build(file *ast.File) *Graph {
	g := &Graph{nodes: make(map[string]*Node)}

	// First pass: register all function nodes.
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		g.nodes[fn.Name] = &Node{
			Name:        fn.Name,
			Effects:     effects.Parse(fn.Effects),
			Annotations: fn.Annotations,
			callees:     make(map[string]bool),
		}
	}

	// Second pass: walk bodies to collect call edges.
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || fn.Body == nil {
			continue
		}
		node := g.nodes[fn.Name]
		collectCalls(fn.Body, node)
	}

	return g
}

// collectCalls walks a block statement and records all direct CallExpr targets.
func collectCalls(block *ast.BlockStmt, node *Node) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		walkStmt(stmt, node)
	}
}

func walkStmt(stmt ast.Stmt, node *Node) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		if s.Value != nil {
			walkExpr(s.Value, node)
		}
	case *ast.LetStmt:
		walkExpr(s.Value, node)
	case *ast.ExprStmt:
		walkExpr(s.Expr, node)
	case *ast.AssignStmt:
		walkExpr(s.Target, node)
		walkExpr(s.Value, node)
	case *ast.IfStmt:
		walkExpr(s.Cond, node)
		collectCalls(s.Then, node)
		if s.Else != nil {
			walkStmt(s.Else, node)
		}
	case *ast.GuardStmt:
		walkExpr(s.Cond, node)
		collectCalls(s.Else, node)
	case *ast.ForStmt:
		walkExpr(s.Iter, node)
		collectCalls(s.Body, node)
	case *ast.BlockStmt:
		collectCalls(s, node)
	}
}

func walkExpr(expr ast.Expr, node *Node) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Record the direct callee name if it's a simple identifier.
		if ident, ok := e.Callee.(*ast.Ident); ok {
			node.callees[ident.Name] = true
		}
		walkExpr(e.Callee, node)
		for _, arg := range e.Args {
			walkExpr(arg, node)
		}
	case *ast.BinaryExpr:
		walkExpr(e.Left, node)
		walkExpr(e.Right, node)
	case *ast.UnaryExpr:
		walkExpr(e.Operand, node)
	case *ast.MemberExpr:
		walkExpr(e.Object, node)
	case *ast.IndexExpr:
		walkExpr(e.Object, node)
		walkExpr(e.Index, node)
	case *ast.NullCoalesceExpr:
		walkExpr(e.Left, node)
		walkExpr(e.Right, node)
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			walkExpr(el, node)
		}
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			walkExpr(f.Value, node)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/callgraph/ -v 2>&1
```
Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/callgraph/callgraph.go internal/callgraph/callgraph_test.go
git commit --no-gpg-sign -m "feat: callgraph — Build, AgentRoots, Reachable"
```

---

## Task 3: `internal/safety` — `@redline` Enforcement

**Files:**
- Create: `internal/safety/checker.go`
- Create: `internal/safety/checker_test.go`

`Checker.Check()` takes an `*ast.File` and a `*callgraph.Graph`, builds the agent-reachable set, and runs enforcement passes. Task 3 implements the `@redline` pass only.

`@redline` on a function means it must NEVER be reachable from an agent context. If it is, emit an error with the call path so the developer can see how the agent reaches it.

- [ ] **Step 1: Write the failing tests**

```go
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
fn run_agent() needs Agent -> String { return dangerous() }
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
fn run_agent() needs Agent -> String { return helper() }
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
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/safety/ 2>&1
```
Expected: compile error — package does not exist.

- [ ] **Step 3: Implement `internal/safety/checker.go`** (redline pass only)

```go
// Package safety runs enforcement passes over the call graph: @redline, @approve, @containment.
package safety

import (
	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/token"
)

// Checker runs safety enforcement passes over a parsed file and its call graph.
type Checker struct{}

// New creates a new Checker.
func New() *Checker { return &Checker{} }

// Check runs all enforcement passes and returns any violations as diagnostics.
func (c *Checker) Check(file *ast.File, g *callgraph.Graph) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	roots := g.AgentRoots()
	agentReachable := g.Reachable(roots)

	diags = append(diags, c.checkRedline(file, g, agentReachable)...)

	return diags
}

// checkRedline emits an error for every @redline function that is agent-reachable.
func (c *Checker) checkRedline(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		node := g.Node(fn.Name)
		if node == nil {
			continue
		}
		if !node.HasAnnotation(ast.AnnotRedline) {
			continue
		}
		if agentReachable[fn.Name] {
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"@redline function %q is reachable from an agent context",
				fn.Name,
			))
		}
	}

	return diags
}

func posOf(file *ast.File) token.Position {
	if file.Module != nil {
		return file.Module.Pos()
	}
	return token.Position{}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/safety/ -v 2>&1
```
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/safety/checker.go internal/safety/checker_test.go
git commit --no-gpg-sign -m "feat: safety checker — @redline enforcement"
```

---

## Task 4: `internal/safety` — `@approve` Enforcement

**Files:**
- Modify: `internal/safety/checker.go`
- Modify: `internal/safety/checker_test.go`

`@approve` means: this specific call site has been explicitly reviewed and approved. A function in the agent-reachable set that has both `DB.write` and `Net` effects (i.e., could exfiltrate data) must either be annotated `@approve` or `@agent_allowed`. Missing both is a compile error.

The rule checked here: **if a function is agent-reachable and has `DBWrite` AND `Net` effects, it requires `@approve` or `@agent_allowed`.**

This is the minimal version of the policy. Richer rule configuration (from a policy file) is deferred.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/safety/checker_test.go`:

```go
func TestApprove_DBWriteAndNetRequiresApproval(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
fn exfil() needs DB.write, Net -> String { return "leaked" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent-reachable function with DB.write+Net requires @approve")
	}
}

func TestApprove_WithApproveAnnotation_NoError(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
@approve
fn exfil() needs DB.write, Net -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: @approve present: %v", diags)
	}
}

func TestApprove_WithAgentAllowed_NoError(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
@agent_allowed
fn exfil() needs DB.write, Net -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: @agent_allowed present: %v", diags)
	}
}

func TestApprove_DBWriteOnlyNoRequirement(t *testing.T) {
	// DB.write alone (no Net) does not trigger the rule
	src := `
module foo
fn run_agent() needs Agent -> String { return writer() }
fn writer() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: DB.write alone should not require @approve: %v", diags)
	}
}
```

- [ ] **Step 2: Run to verify new tests fail**

```bash
go test ./internal/safety/ -run TestApprove -v 2>&1
```
Expected: all 4 FAIL (approve check not yet implemented).

- [ ] **Step 3: Add `checkApprove` to `internal/safety/checker.go`**

Add the method and wire it into `Check()`:

```go
// In Check(), after checkRedline:
diags = append(diags, c.checkApprove(file, g, agentReachable)...)
```

```go
// checkApprove emits an error for every agent-reachable function that combines
// DBWrite and Net effects without @approve or @agent_allowed.
func (c *Checker) checkApprove(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !agentReachable[fn.Name] {
			continue
		}
		node := g.Node(fn.Name)
		if node == nil {
			continue
		}
		// Policy: DB.write + Net in agent context requires explicit approval.
		if node.Effects.Has(effects.DBWrite) && node.Effects.Has(effects.Net) {
			if !node.HasAnnotation(ast.AnnotApprove) && !node.HasAnnotation(ast.AnnotAgentAllowed) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q combines DB.write and Net effects in an agent context — add @approve or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}
```

Also add `"gosplash.dev/splash/internal/effects"` to the import block in `checker.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/safety/ -v 2>&1
```
Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/safety/checker.go internal/safety/checker_test.go
git commit --no-gpg-sign -m "feat: safety checker — @approve enforcement (DB.write+Net policy)"
```

---

## Task 5: `internal/safety` — `@containment` Enforcement

**Files:**
- Modify: `internal/safety/checker.go`
- Modify: `internal/safety/checker_test.go`

`@containment` is a module-level annotation with a policy argument. It restricts which functions an agent can reach within that module:
- `@containment(agent: "none")` — no agent-reachable function may live in this module
- `@containment(agent: "read_only")` — only `DB.read` (not `DB.write`) is permitted for agent-reachable functions
- `@containment(agent: "approved_only")` — every agent-reachable function must have `@approve` or `@agent_allowed`

The containment policy is read from the `module` declaration's annotations. The checker walks all functions in the file, and for those in the agent-reachable set, enforces the policy.

Note: Since a single `.splash` file maps to one module, "module" and "file" are synonymous here.

- [ ] **Step 1: Write the failing tests**

Add to `internal/safety/checker_test.go`:

```go
func TestContainment_None_BlocksAllAgentAccess(t *testing.T) {
	src := `
@containment(agent: "none")
module payments
fn run_agent() needs Agent -> String { return process() }
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: @containment(agent: none) blocks all agent-reachable functions")
	}
}

func TestContainment_None_NoAgent_NoError(t *testing.T) {
	src := `
@containment(agent: "none")
module payments
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: no agent, containment none is satisfied: %v", diags)
	}
}

func TestContainment_ReadOnly_BlocksWrite(t *testing.T) {
	src := `
@containment(agent: "read_only")
module payments
fn run_agent() needs Agent -> String { return process() }
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: read_only containment blocks DB.write in agent context")
	}
}

func TestContainment_ReadOnly_AllowsRead(t *testing.T) {
	src := `
@containment(agent: "read_only")
module payments
fn run_agent() needs Agent -> String { return query() }
fn query() needs DB.read -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: read_only allows DB.read: %v", diags)
	}
}

func TestContainment_ApprovedOnly_RequiresAnnotation(t *testing.T) {
	src := `
@containment(agent: "approved_only")
module payments
fn run_agent() needs Agent -> String { return process() }
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: approved_only requires @approve or @agent_allowed")
	}
}

func TestContainment_ApprovedOnly_WithApprove_NoError(t *testing.T) {
	src := `
@containment(agent: "approved_only")
module payments
fn run_agent() needs Agent -> String { return process() }
@approve
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: @approve satisfies approved_only: %v", diags)
	}
}
```

- [ ] **Step 2: Run to verify new tests fail**

```bash
go test ./internal/safety/ -run TestContainment -v 2>&1
```
Expected: all 6 FAIL.

- [ ] **Step 3: Add `checkContainment` to `internal/safety/checker.go`**

Add a helper to read the containment policy from the module annotation, wire it into `Check()`, and implement the three policy modes:

```go
// In Check(), after checkApprove:
diags = append(diags, c.checkContainment(file, g, agentReachable)...)
```

```go
type containmentPolicy int

const (
	containmentNone         containmentPolicy = iota // no agent access
	containmentReadOnly                              // DB.read only
	containmentApprovedOnly                          // @approve or @agent_allowed required
	containmentUnrestricted                          // no containment annotation
)

// moduleContainment reads the @containment annotation from the module declaration.
func moduleContainment(file *ast.File) containmentPolicy {
	if file.Module == nil {
		return containmentUnrestricted
	}
	for _, ann := range file.Module.Annotations {
		if ann.Kind != ast.AnnotContainment {
			continue
		}
		agentExpr, ok := ann.Args["agent"]
		if !ok {
			continue
		}
		lit, ok := agentExpr.(*ast.StringLiteral)
		if !ok {
			continue
		}
		switch lit.Value {
		case "none":
			return containmentNone
		case "read_only":
			return containmentReadOnly
		case "approved_only":
			return containmentApprovedOnly
		}
	}
	return containmentUnrestricted
}

// checkContainment enforces the module-level @containment policy against the agent-reachable set.
func (c *Checker) checkContainment(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
	policy := moduleContainment(file)
	if policy == containmentUnrestricted {
		return nil
	}

	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !agentReachable[fn.Name] {
			continue
		}
		node := g.Node(fn.Name)
		if node == nil {
			continue
		}

		switch policy {
		case containmentNone:
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"function %q is agent-reachable but module has @containment(agent: \"none\")",
				fn.Name,
			))
		case containmentReadOnly:
			if node.Effects.Has(effects.DBWrite) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q has DB.write but module has @containment(agent: \"read_only\")",
					fn.Name,
				))
			}
		case containmentApprovedOnly:
			if !node.HasAnnotation(ast.AnnotApprove) && !node.HasAnnotation(ast.AnnotAgentAllowed) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q is agent-reachable but module has @containment(agent: \"approved_only\") — add @approve or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}
```

- [ ] **Step 4: Run all safety tests**

```bash
go test ./internal/safety/ -v 2>&1
```
Expected: all 14 tests PASS.

- [ ] **Step 5: Run the full test suite**

```bash
go test ./... 2>&1
```
Expected: all packages PASS, no failures.

- [ ] **Step 6: Commit**

```bash
git add internal/safety/checker.go internal/safety/checker_test.go
git commit --no-gpg-sign -m "feat: safety checker — @containment enforcement (none/read_only/approved_only)"
```

---

## Self-Review

### Spec Coverage

| Requirement | Task |
|-------------|------|
| EffectSet bitmask with parent expansion (DB → DBRead\|DBWrite) | Task 1 |
| Parse `needs` clauses from AST | Task 1 |
| Build directed call graph from function bodies | Task 2 |
| Agent entry points: `needs Agent` and `@tool` | Task 2 |
| Transitive reachability (BFS) | Task 2 |
| `@redline` enforcement with transitive violations | Task 3 |
| `@approve` / `@agent_allowed` enforcement (DB.write+Net policy) | Task 4 |
| `@containment(agent: "none")` | Task 5 |
| `@containment(agent: "read_only")` | Task 5 |
| `@containment(agent: "approved_only")` | Task 5 |

### Placeholder Scan

No TBDs or placeholder steps. All code is written out.

### Type Consistency

- `effects.EffectSet` used consistently across `callgraph.Node.Effects` and `safety.checkApprove` / `checkContainment`
- `callgraph.Graph.Node()` returns `*callgraph.Node` — used in Task 3/4/5 safety passes
- `callgraph.Graph.AgentRoots()` returns `[]string` — used in `Checker.Check()`
- `callgraph.Graph.Reachable([]string)` returns `map[string]bool` — used in all three passes
- `ast.AnnotRedline`, `ast.AnnotApprove`, `ast.AnnotAgentAllowed`, `ast.AnnotContainment`, `ast.AnnotTool` — all defined in `ast/annotation.go`
- `diagnostic.Errorf(token.Position, string, ...any)` — matches existing diagnostic package API

### Known Deferred Items

- **Call path in redline errors** — the error says "reachable from agent context" but doesn't show the path (e.g. `run_agent → helper → dangerous`). Path reconstruction requires back-tracking through the BFS, which is non-trivial. Left for a follow-up.
- **Richer `@approve` policy rules** (from a policy file, not hardcoded DB.write+Net) — the policy file mechanism is a Phase 3 concern.
- **Cross-module reachability** — the call graph currently covers a single file. Multi-module analysis requires the module resolver, which is not yet built.
