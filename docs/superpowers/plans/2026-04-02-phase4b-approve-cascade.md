# Phase 4b: @approve Error Cascade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the dead-code `panic` in `splashApprove` with real error propagation — `@approve` functions get `(T, error)` Go signatures, and the error cascades transitively through every caller so one denied approval returns an error to one request without killing the process.

**Architecture:** The callgraph package gains `Callers(targets map[string]bool) map[string]bool` — reverse BFS that finds every function that transitively calls any target. The CLI driver computes this "approve-callers" set and gives it to the emitter via `SetApprovalCallers`. The emitter's `EmitFile` pre-pass builds `approveFns` from AST annotations and `fnDecls` for return-type lookups. During emission, `@approve` functions get `(T, error)` signatures and an error-returning body gate; approval-propagating callers get the same signature change plus error-handling at every call site; `main()` is the sole exception — it gets `fmt.Fprintf + os.Exit(1)` instead of `return err`. `splashApprove` changes from void (panicking) to returning `error` directly from the adapter.

**Tech Stack:** Go, standard library only. `codegen` does NOT import `callgraph` — the CLI driver bridges them.

**Phase 4a preserved:** `StdinApproval` still loops until `y` and returns nil. The cascade machinery exists but is inert in dev. Production adapters that return real denial errors (Phase 4c) will exercise it automatically.

---

## File Map

| File | Change |
|---|---|
| `internal/callgraph/callgraph_test.go` | Add `TestCallers` |
| `internal/callgraph/callgraph.go` | Add `Callers(targets map[string]bool) map[string]bool` method |
| `internal/codegen/codegen_test.go` | Update `TestApproveGate`; add `TestApproveCascade`, `TestApproveMainExit`, `emitSrcWithApproval` helper |
| `internal/codegen/codegen.go` | New fields on `Emitter`; update `New()`; add `SetApprovalCallers`; update `EmitFile` pre-pass; update `splashApprovalHelper` const; update import set for main-exit case |
| `internal/codegen/decl.go` | `emitFunctionDecl` — signature change, body injection, implicit `return nil`, per-fn state tracking |
| `internal/codegen/expr.go` | Add `zeroValueFor(ast.TypeExpr) string` helper |
| `internal/codegen/stmt.go` | `emitReturnStmt` — rewrite inside approval fns; `emitLetStmt` — multi-return + error check; `emitExprStmt` — error check |
| `cmd/splash/main.go` | `runBuild` and `runEmit` compute approval sets and call `SetApprovalCallers` |

---

## Task 1: Write failing tests

**Files:**
- Modify: `internal/callgraph/callgraph_test.go`
- Modify: `internal/codegen/codegen_test.go`

- [ ] **Step 1: Add `TestCallers` to the callgraph test file**

This goes after the existing tests in `internal/callgraph/callgraph_test.go`:

```go
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
	// run calls charge transitively
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
```

- [ ] **Step 2: Run callgraph tests — both must fail**

```
go test ./internal/callgraph/... -run "TestCallers" -v
```

Expected: FAIL — `callgraph.Graph` has no `Callers` method.

- [ ] **Step 3: Add `emitSrcWithApproval` helper to `codegen_test.go`**

Add this after the existing `emitSrc` helper. It builds the callgraph, computes approval sets, and passes them to the emitter — exercising the cascade path:

```go
func emitSrcWithApproval(t *testing.T, src string) string {
	t.Helper()
	f := parse(t, src)

	// Collect @approve function names from AST
	approveFns := make(map[string]bool)
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				approveFns[fn.Name] = true
			}
		}
	}

	g := callgraph.Build(f)
	approveCallers := g.Callers(approveFns)

	e := codegen.New()
	e.SetApprovalCallers(approveCallers)
	return e.EmitFile(f)
}
```

This requires two new imports in `codegen_test.go`:

```go
import (
	"strings"
	"testing"

	goparser "go/parser"
	gotoken "go/token"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
)
```

- [ ] **Step 4: Update `TestApproveGate` — expect `(T, error)` signature and error-returning gate**

Replace the existing `TestApproveGate` function with:

```go
func TestApproveGate(t *testing.T) {
	src := `
module payments
@approve
fn processPayment(amount: Int) {
    let x = amount
}
fn run() {
    processPayment(100)
}
`
	// Use plain emitSrc (no graph) — cascade does not fire, but @approve function
	// itself still gets the approval gate and error return.
	out := emitSrc(t, src)
	mustGoSyntax(t, out)

	// Void @approve function gets error return
	if !strings.Contains(out, "func processPayment(amount int) error") {
		t.Errorf("expected error return on @approve function, got:\n%s", out)
	}
	// Body injection is now an error-returning guard
	if !strings.Contains(out, `if err := splashApprove("processPayment"); err != nil`) {
		t.Errorf("expected error-returning gate, got:\n%s", out)
	}
	// Implicit return nil after void @approve function body
	if !strings.Contains(out, "return nil") {
		t.Errorf("expected 'return nil' at end of void @approve body, got:\n%s", out)
	}
	// Helper in preamble
	if !strings.Contains(out, "func splashApprove") {
		t.Errorf("expected splashApprove helper in preamble, got:\n%s", out)
	}
	// splashApprove must now return error (not be void)
	if !strings.Contains(out, "func splashApprove(name string) error") {
		t.Errorf("expected splashApprove to return error, got:\n%s", out)
	}
	// No cascade without graph: run() call site must not have error injection
	runIdx := strings.Index(out, "func run()")
	if runIdx < 0 {
		t.Fatal("func run() not found in output — cannot verify call site is clean")
	}
	if strings.Contains(out[runIdx:], "if err :=") {
		t.Errorf("error injection must not appear at call site without graph, got:\n%s", out)
	}
}
```

- [ ] **Step 5: Add `TestApproveCascade`**

Add after `TestApproveGate`:

```go
func TestApproveCascade(t *testing.T) {
	src := `
module payments
@approve
fn charge(amount: Int) -> Int {
    return amount
}
fn run() -> Int {
    let result = charge(100)
    return result
}
`
	out := emitSrcWithApproval(t, src)
	mustGoSyntax(t, out)

	// @approve function: (Int, error) return
	if !strings.Contains(out, "func charge(amount int) (int, error)") {
		t.Errorf("expected (int, error) return on @approve function, got:\n%s", out)
	}
	// @approve function body: error-returning gate
	if !strings.Contains(out, `if err := splashApprove("charge"); err != nil`) {
		t.Errorf("expected error-returning gate in charge body, got:\n%s", out)
	}
	// @approve function: explicit return becomes return x, nil
	if !strings.Contains(out, "return amount, nil") {
		t.Errorf("expected 'return amount, nil' inside charge, got:\n%s", out)
	}
	// Cascade: run() also gets (int, error) return
	if !strings.Contains(out, "func run() (int, error)") {
		t.Errorf("expected run() to gain (int, error) return via cascade, got:\n%s", out)
	}
	// Cascade: call site in run() handles error
	if !strings.Contains(out, "result, err := charge(100)") {
		t.Errorf("expected multi-return call site for charge(), got:\n%s", out)
	}
	if !strings.Contains(out, "if err != nil") {
		t.Errorf("expected error check after charge() call, got:\n%s", out)
	}
	// Cascade: return in run() becomes return result, nil
	if !strings.Contains(out, "return result, nil") {
		t.Errorf("expected 'return result, nil' in run(), got:\n%s", out)
	}
}
```

- [ ] **Step 6: Add `TestApproveMainExit`**

```go
func TestApproveMainExit(t *testing.T) {
	src := `
module main
@approve
fn doWork() {
    println("done")
}
fn main() {
    doWork()
}
`
	out := emitSrcWithApproval(t, src)
	mustGoSyntax(t, out)

	// main() must NOT get an error return (Go forbids it)
	if strings.Contains(out, "func main() error") {
		t.Errorf("main() must not have error return, got:\n%s", out)
	}
	// main() call site: graceful exit, not return err
	if !strings.Contains(out, "os.Exit(1)") {
		t.Errorf("expected os.Exit(1) in main() denial path, got:\n%s", out)
	}
	if !strings.Contains(out, `fmt.Fprintf(os.Stderr`) {
		t.Errorf("expected fmt.Fprintf in main() denial path, got:\n%s", out)
	}
}
```

- [ ] **Step 7: Run all new tests — all must fail**

```
go test ./internal/callgraph/... -run "TestCallers" -v
go test ./internal/codegen/... -run "TestApproveGate|TestApproveCascade|TestApproveMainExit" -v
```

Expected: all FAIL (implementations don't exist yet).

- [ ] **Step 8: Commit failing tests**

```bash
git add internal/callgraph/callgraph_test.go internal/codegen/codegen_test.go
git commit --no-gpg-sign -m "test: phase 4b failing tests — approval cascade, main exit, Callers method"
```

---

## Task 2: Add `Callers` to callgraph

**Files:**
- Modify: `internal/callgraph/callgraph.go`

- [ ] **Step 1: Add the `Callers` method**

Add after `Reachable` in `internal/callgraph/callgraph.go`:

```go
// Callers returns the set of all functions that transitively call any function
// in targets. The targets themselves are included in the result.
// This is reverse reachability: BFS backwards through the call graph.
func (g *Graph) Callers(targets map[string]bool) map[string]bool {
	// Build reverse adjacency: callee → []callers
	reverse := make(map[string][]string)
	for name, node := range g.nodes {
		for callee := range node.callees {
			reverse[callee] = append(reverse[callee], name)
		}
	}
	// BFS backwards from every target
	visited := make(map[string]bool)
	queue := make([]string, 0, len(targets))
	for name := range targets {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		for _, caller := range reverse[cur] {
			if !visited[caller] {
				queue = append(queue, caller)
			}
		}
	}
	return visited
}
```

- [ ] **Step 2: Run callgraph tests — must pass**

```
go test ./internal/callgraph/... -v
```

Expected: all callgraph tests PASS including `TestCallers` and `TestCallersTransitive`.

- [ ] **Step 3: Commit**

```bash
git add internal/callgraph/callgraph.go
git commit --no-gpg-sign -m "feat: callgraph.Callers — reverse BFS to find transitive callers of a target set"
```

---

## Task 3: Emitter fields, pre-pass, signature rewriting, body injection

**Files:**
- Modify: `internal/codegen/codegen.go`
- Modify: `internal/codegen/decl.go`
- Modify: `internal/codegen/expr.go`
- Modify: `internal/codegen/stmt.go` (return rewriting only — call-site cascade is Task 4)

- [ ] **Step 1: Add fields to `Emitter` and update `New()` in `codegen.go`**

Replace the `Emitter` struct and `New()`:

```go
type Emitter struct {
	body          strings.Builder
	imports       map[string]bool
	indent        int
	needsCoalesce bool
	needsApproval bool

	// Phase 4b: approval cascade
	approveFns     map[string]bool              // @approve-annotated function names (built in EmitFile pre-pass)
	approveCallers map[string]bool              // transitive callers of approveFns (set externally via SetApprovalCallers)
	fnDecls        map[string]*ast.FunctionDecl // all function declarations (for return-type lookups at call sites)

	// Per-function emission state (set in emitFunctionDecl, cleared after)
	inApprovalFn        bool
	currentFnReturnType ast.TypeExpr
	currentFnIsMain     bool
}

func New() *Emitter {
	return &Emitter{
		imports:        make(map[string]bool),
		approveFns:     make(map[string]bool),
		approveCallers: make(map[string]bool),
		fnDecls:        make(map[string]*ast.FunctionDecl),
	}
}
```

- [ ] **Step 2: Add `SetApprovalCallers` to `codegen.go`**

Add after `New()`:

```go
// SetApprovalCallers provides the set of functions that transitively call any
// @approve function. Used by the CLI driver after callgraph analysis.
// The emitter always builds approveFns itself from AST annotations in EmitFile.
func (e *Emitter) SetApprovalCallers(approveCallers map[string]bool) {
	e.approveCallers = approveCallers
}
```

- [ ] **Step 3: Update `EmitFile` in `codegen.go` — add pre-pass**

Replace `EmitFile` with:

```go
func (e *Emitter) EmitFile(f *ast.File) string {
	// Pre-pass: index all function declarations and collect @approve names.
	// approveFns is always built from AST annotations regardless of external input.
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		e.fnDecls[fn.Name] = fn
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				e.approveFns[fn.Name] = true
			}
		}
	}

	// Emit declarations into body buffer
	for _, decl := range f.Declarations {
		e.emitDecl(decl)
	}

	// Helpers need imports — add after body emission
	if e.needsApproval {
		e.imports["bufio"] = true
		e.imports["fmt"] = true
		e.imports["os"] = true
		e.imports["strings"] = true
	}

	// Assemble: package + imports + helpers + body
	var out strings.Builder
	pkgName := "main"
	if f.Module != nil {
		pkgName = f.Module.Name
	}
	fmt.Fprintf(&out, "package %s\n\n", pkgName)

	if len(e.imports) > 0 {
		out.WriteString("import (\n")
		importList := make([]string, 0, len(e.imports))
		for imp := range e.imports {
			importList = append(importList, imp)
		}
		sort.Strings(importList)
		for _, imp := range importList {
			fmt.Fprintf(&out, "\t%q\n", imp)
		}
		out.WriteString(")\n\n")
	}

	if e.needsCoalesce {
		out.WriteString(splashCoalesceHelper)
		out.WriteString("\n")
	}
	if e.needsApproval {
		out.WriteString(splashApprovalHelper)
		out.WriteString("\n")
	}

	out.WriteString(e.body.String())
	return out.String()
}
```

- [ ] **Step 4: Update `splashApprovalHelper` const in `codegen.go` — `splashApprove` returns `error`**

Replace the `splashApprovalHelper` const. The only change is `splashApprove`: it now returns `error` directly from the adapter instead of panicking. `StdinApproval` never returns an error, so Phase 4a behavior is preserved in dev:

```go
const splashApprovalHelper = `// ApprovalAdapter is the interface for @approve gate implementations.
// Request blocks until the named function is approved.
// Return nil to approve; return an error to deny.
// StdinApproval (the default) loops until the operator types y — it never returns an error.
// Production adapters (SlackApproval, WebhookApproval) return a non-nil error on denial,
// which propagates up the call stack without killing the process.
type ApprovalAdapter interface {
	Request(name string) error
}

var _splashApproval ApprovalAdapter = &splashStdinApproval{}

// SetApprovalAdapter replaces the package-level approval adapter.
// Call this in tests or in production main() before any @approve function runs.
func SetApprovalAdapter(a ApprovalAdapter) { _splashApproval = a }

type splashStdinApproval struct{}

func (*splashStdinApproval) Request(name string) error {
	for {
		fmt.Fprintf(os.Stderr, "[approve] %s — approve? (y/N): ", name)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) == "y" {
			return nil
		}
		fmt.Fprintf(os.Stderr, "Not approved. Try again or Ctrl+C to abort.\n")
	}
}

func splashApprove(name string) error {
	return _splashApproval.Request(name)
}
`
```

- [ ] **Step 5: Add `zeroValueFor` to `expr.go`**

Add this method to `internal/codegen/expr.go`:

```go
// zeroValueFor returns the Go zero-value literal for a Splash type expression.
// Used to generate early-exit return values when approval is denied.
func (e *Emitter) zeroValueFor(t ast.TypeExpr) string {
	if t == nil {
		return ""
	}
	switch typ := t.(type) {
	case *ast.NamedTypeExpr:
		switch typ.Name {
		case "String":
			return `""`
		case "Int":
			return "0"
		case "Float":
			return "0.0"
		case "Bool":
			return "false"
		default:
			return typ.Name + "{}"
		}
	case *ast.OptionalTypeExpr:
		return "nil"
	default:
		return "nil"
	}
}
```

- [ ] **Step 6: Update `funcSignature` in `decl.go` — wrap return type for approval functions**

Replace `funcSignature` in `internal/codegen/decl.go`:

```go
func (e *Emitter) funcSignature(decl *ast.FunctionDecl) string {
	var params []string
	for _, p := range decl.Params {
		params = append(params, fmt.Sprintf("%s %s", p.Name, e.emitTypeName(p.Type)))
	}

	isApprovalFn := e.approveFns[decl.Name] || e.approveCallers[decl.Name]
	isMain := decl.Name == "main"

	ret := e.emitTypeName(decl.ReturnType)
	sig := fmt.Sprintf("func %s(%s)", decl.Name, strings.Join(params, ", "))

	switch {
	case isApprovalFn && !isMain && ret != "":
		sig += fmt.Sprintf(" (%s, error)", ret)
	case isApprovalFn && !isMain && ret == "":
		sig += " error"
	case ret != "":
		sig += " " + ret
	}
	return sig
}
```

- [ ] **Step 7: Update `emitFunctionDecl` in `decl.go` — per-fn state, gate injection, implicit `return nil`**

Replace `emitFunctionDecl` in `internal/codegen/decl.go`:

```go
func (e *Emitter) emitFunctionDecl(decl *ast.FunctionDecl) {
	isApprove := e.approveFns[decl.Name]
	isCaller := e.approveCallers[decl.Name]
	isMain := decl.Name == "main"

	// Set per-function emission state for stmt emitters
	e.inApprovalFn = (isApprove || isCaller) && !isMain
	e.currentFnReturnType = decl.ReturnType
	e.currentFnIsMain = isMain

	sig := e.funcSignature(decl)
	e.writef("%s {\n", sig)
	e.indent++

	if isApprove {
		// Inject approval gate: if denied, return before body runs.
		e.needsApproval = true
		if decl.ReturnType != nil {
			e.writeLine("if err := splashApprove(%q); err != nil {", decl.Name)
			e.indent++
			e.writeLine("return %s, err", e.zeroValueFor(decl.ReturnType))
			e.indent--
			e.writeLine("}")
		} else {
			e.writeLine("if err := splashApprove(%q); err != nil {", decl.Name)
			e.indent++
			e.writeLine("return err")
			e.indent--
			e.writeLine("}")
		}
	}

	e.emitBlock(decl.Body)

	// Void approval functions need an explicit return nil — Go requires it.
	// Non-void functions have explicit Splash returns rewritten to "return x, nil" by emitReturnStmt.
	if e.inApprovalFn && decl.ReturnType == nil {
		e.writeLine("return nil")
	}

	e.indent--
	e.writeLine("}\n")

	// Clear per-function state
	e.inApprovalFn = false
	e.currentFnReturnType = nil
	e.currentFnIsMain = false
}
```

- [ ] **Step 8: Update `emitReturnStmt` in `stmt.go` — rewrite returns inside approval functions**

Replace `emitReturnStmt` in `internal/codegen/stmt.go`:

```go
func (e *Emitter) emitReturnStmt(s *ast.ReturnStmt) {
	if !e.inApprovalFn {
		if s.Value == nil {
			e.writeLine("return")
			return
		}
		e.writeLine("return %s", e.emitExprStr(s.Value))
		return
	}
	// Inside an @approve or approval-propagating function (not main):
	// append ", nil" to carry the error channel alongside the normal return value.
	if s.Value == nil {
		e.writeLine("return nil")
		return
	}
	e.writeLine("return %s, nil", e.emitExprStr(s.Value))
}
```

- [ ] **Step 9: Run tests to see progress — TestApproveGate should pass now**

```
go test ./internal/codegen/... -run "TestApproveGate" -v
```

Expected: PASS. `TestApproveCascade` and `TestApproveMainExit` still fail (call-site cascade not implemented).

- [ ] **Step 10: Run full suite — no regressions**

```
go test ./...
```

Expected: callgraph and codegen tests pass. `TestApproveCascade` and `TestApproveMainExit` fail.

- [ ] **Step 11: Commit**

```bash
git add internal/codegen/codegen.go internal/codegen/decl.go internal/codegen/expr.go internal/codegen/stmt.go
git commit --no-gpg-sign -m "feat: @approve functions get (T, error) signatures, error-returning body gate, return rewriting"
```

---

## Task 4: Call-site cascade, `main` exit, CLI wiring

**Files:**
- Modify: `internal/codegen/stmt.go` (call-site handling in `emitLetStmt`, `emitExprStmt`)
- Modify: `cmd/splash/main.go`

- [ ] **Step 1: Update `emitLetStmt` in `stmt.go` — multi-return + error check for `@approve` call sites**

Replace `emitLetStmt` in `internal/codegen/stmt.go`:

```go
func (e *Emitter) emitLetStmt(s *ast.LetStmt) {
	// Check if the RHS is a direct call to an @approve function.
	// If so, unwrap the (T, error) return and handle denial.
	if call, ok := s.Value.(*ast.CallExpr); ok {
		if ident, ok2 := call.Callee.(*ast.Ident); ok2 && e.approveFns[ident.Name] {
			if e.currentFnIsMain {
				// main() cannot return error — use graceful exit.
				e.imports["fmt"] = true
				e.imports["os"] = true
				e.writeLine("%s, err := %s", s.Name, e.emitExprStr(s.Value))
				e.writeLine("if err != nil {")
				e.indent++
				e.writeLine(`fmt.Fprintf(os.Stderr, "approval denied: %%v\n", err)`)
				e.writeLine("os.Exit(1)")
				e.indent--
				e.writeLine("}")
			} else {
				// Normal cascade: propagate the error up.
				callee := e.fnDecls[ident.Name]
				e.writeLine("%s, err := %s", s.Name, e.emitExprStr(s.Value))
				e.writeLine("if err != nil {")
				e.indent++
				if e.currentFnReturnType != nil {
					e.writeLine("return %s, err", e.zeroValueFor(e.currentFnReturnType))
				} else {
					e.writeLine("return err")
				}
				e.indent--
				e.writeLine("}")
				_ = callee // used for return type lookup above
			}
			return
		}
	}
	// Default: no @approve call, original behavior.
	if s.Type != nil {
		e.writeLine("var %s %s = %s", s.Name, e.emitTypeName(s.Type), e.emitExprStr(s.Value))
	} else {
		e.writeLine("%s := %s", s.Name, e.emitExprStr(s.Value))
	}
}
```

- [ ] **Step 2: Update `emitExprStmt` in `stmt.go` — error check for `@approve` call sites (void use)**

Replace `emitExprStmt` in `internal/codegen/stmt.go`:

```go
func (e *Emitter) emitExprStmt(s *ast.ExprStmt) {
	// Check if this is a direct call to an @approve function used as a statement
	// (return value discarded). Handle the (T, error) or (error) return.
	if call, ok := s.Expr.(*ast.CallExpr); ok {
		if ident, ok2 := call.Callee.(*ast.Ident); ok2 && e.approveFns[ident.Name] {
			calleeDecl := e.fnDecls[ident.Name]
			hasReturnVal := calleeDecl != nil && calleeDecl.ReturnType != nil

			if e.currentFnIsMain {
				e.imports["fmt"] = true
				e.imports["os"] = true
				if hasReturnVal {
					e.writeLine("if _, err := %s; err != nil {", e.emitExprStr(s.Expr))
				} else {
					e.writeLine("if err := %s; err != nil {", e.emitExprStr(s.Expr))
				}
				e.indent++
				e.writeLine(`fmt.Fprintf(os.Stderr, "approval denied: %%v\n", err)`)
				e.writeLine("os.Exit(1)")
				e.indent--
				e.writeLine("}")
			} else {
				if hasReturnVal {
					e.writeLine("if _, err := %s; err != nil {", e.emitExprStr(s.Expr))
				} else {
					e.writeLine("if err := %s; err != nil {", e.emitExprStr(s.Expr))
				}
				e.indent++
				if e.currentFnReturnType != nil {
					e.writeLine("return %s, err", e.zeroValueFor(e.currentFnReturnType))
				} else {
					e.writeLine("return err")
				}
				e.indent--
				e.writeLine("}")
			}
			return
		}
	}
	e.writeLine("%s", e.emitExprStr(s.Expr))
}
```

- [ ] **Step 3: Run `TestApproveCascade` and `TestApproveMainExit` — must pass**

```
go test ./internal/codegen/... -run "TestApproveCascade|TestApproveMainExit" -v
```

Expected: PASS.

- [ ] **Step 4: Run full codegen suite**

```
go test ./internal/codegen/... -v
```

Expected: all 19+ tests pass.

- [ ] **Step 5: Commit emitter changes**

```bash
git add internal/codegen/stmt.go
git commit --no-gpg-sign -m "feat: call-site cascade — @approve calls propagate error through let and expr stmts, main uses os.Exit"
```

- [ ] **Step 6: Wire CLI driver — compute approval sets in `runBuild` and `runEmit`**

Add this helper to `cmd/splash/main.go` (place it before `runBuild`):

```go
// collectApproveFns returns the set of @approve-annotated function names in f.
func collectApproveFns(f *ast.File) map[string]bool {
	fns := make(map[string]bool)
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				fns[fn.Name] = true
			}
		}
	}
	return fns
}
```

In `runBuild`, locate the lines:

```go
e := codegen.New()
goSrc := e.EmitFile(f)
```

Replace with:

```go
e := codegen.New()
e.SetApprovalCallers(g.Callers(collectApproveFns(f)))
goSrc := e.EmitFile(f)
```

In `runEmit`, locate:

```go
e := codegen.New()
fmt.Print(e.EmitFile(f))
```

Replace with:

```go
g := callgraph.Build(f)
e := codegen.New()
e.SetApprovalCallers(g.Callers(collectApproveFns(f)))
fmt.Print(e.EmitFile(f))
```

Note: `runEmit` doesn't currently build the callgraph; add the `g := callgraph.Build(f)` line before the emitter. The `callgraph` import is already present in `main.go` (`runCheck` and `runBuild` both use it).

- [ ] **Step 7: Run full test suite**

```
go test ./...
```

Expected: all packages pass.

- [ ] **Step 8: Verify with the approval example**

```bash
go build -o splash ./cmd/splash/... && ./splash emit examples/approval/approval.splash
```

Verify in the output:
- `func charge_card(customer_id int, amount_cents int) (Charge, error)` — `@approve` function with `(T, error)` return
- `if err := splashApprove("charge_card"); err != nil { return Charge{}, err }` — error-returning gate
- `func run_billing_agent(customer_id int, amount_cents int) (Charge, error)` — cascaded to caller
- `charge, err := charge_card(customer_id, amount_cents)` — multi-return call site
- `if err != nil { return Charge{}, err }` — error propagation
- `return charge, nil` — normal return rewritten
- `func splashApprove(name string) error` — no longer void

- [ ] **Step 9: Commit CLI wiring**

```bash
git add cmd/splash/main.go
git commit --no-gpg-sign -m "feat: wire @approve cascade into splash build and splash emit via callgraph.Callers"
```

---

## Self-Review

**Spec coverage:**

- `splashApprove` returns `error` instead of panicking ✅ Task 3 Step 4
- `@approve` functions get `(T, error)` or `error` Go signatures ✅ Task 3 Step 6
- Body injection returns `zero, err` on denial ✅ Task 3 Step 7
- Void `@approve` functions get `return nil` at end ✅ Task 3 Step 7
- Return statements inside approval fns rewritten: `return x` → `return x, nil` ✅ Task 3 Step 8
- Transitive callers computed via `callgraph.Callers` ✅ Task 2
- Caller function signatures also get `(T, error)` ✅ Task 3 Step 6
- Call sites in callers: multi-return + `if err != nil { return zero, err }` ✅ Task 4 Steps 1-2
- `main()` exception: `fmt.Fprintf + os.Exit(1)`, no error return ✅ Task 4 Steps 1-2
- `codegen` does NOT import `callgraph` — bridged by CLI driver ✅ Task 4 Step 6
- `StdinApproval` still loops until `y`, still never returns error — dev behavior unchanged ✅ (no change to adapter logic)
- Tests written before implementation ✅ Task 1

**Placeholder scan:** None.

**Type consistency:**
- `SetApprovalCallers` defined in Task 3 Step 2, used in Task 4 Step 6 ✅
- `zeroValueFor` defined in Task 3 Step 5, used in Task 3 Steps 7, 4 Steps 1-2 ✅
- `inApprovalFn`, `currentFnReturnType`, `currentFnIsMain` set in Task 3 Step 7, read in Task 3 Step 8 and Task 4 Steps 1-2 ✅
- `approveFns` built in Task 3 Step 3 (EmitFile pre-pass), read in Task 3 Steps 6-7 and Task 4 Steps 1-2 ✅
- `fnDecls` built in Task 3 Step 3, read in Task 4 Steps 1-2 ✅
- `callgraph.Callers` defined in Task 2, used in `emitSrcWithApproval` (Task 1) and CLI (Task 4) ✅
