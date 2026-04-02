# Phase 3: Go Codegen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit valid Go source from a Splash AST, and wire it into `splash check` / `splash build` CLI commands.

**Architecture:** An `Emitter` struct walks the AST and builds a Go source string. Two passes in `EmitFile`: (1) scan for `@approve` functions, (2) emit declarations. Runtime helpers (`splashCoalesce`, `splashAudit`) are emitted in a preamble only when needed. `cmd/splash/main.go` drives parse → check → codegen → `go build`.

**Tech Stack:** Go 1.22, `go/parser` (for syntax validation in tests), `os/exec` (for end-to-end build test), existing `internal/{lexer,parser,typechecker,callgraph,safety}`.

---

## File Map

| File | Responsibility |
|------|---------------|
| `internal/codegen/codegen.go` | `Emitter` struct, `New`, `EmitFile`, type name emission, write helpers, runtime helper strings |
| `internal/codegen/decl.go` | `emitDecl`, `emitTypeDecl`, `emitEnumDecl`, `emitFunctionDecl` |
| `internal/codegen/stmt.go` | `emitStmt`, `emitBlock` — all statement types |
| `internal/codegen/expr.go` | `emitExprStr` — all expression types |
| `internal/codegen/codegen_test.go` | All tests |
| `cmd/splash/main.go` | CLI: `splash check <file>`, `splash build <file> [-o out]` |

---

### Task 1: Scaffolding — Emitter, type names, write helpers

**Files:**
- Create: `internal/codegen/codegen.go`
- Create: `internal/codegen/codegen_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/codegen/codegen_test.go
package codegen_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/codegen"
)

func TestEmitTypeName(t *testing.T) {
	e := codegen.New()
	for _, c := range []struct {
		in   ast.TypeExpr
		want string
	}{
		{&ast.NamedTypeExpr{Name: "String"}, "string"},
		{&ast.NamedTypeExpr{Name: "Int"}, "int"},
		{&ast.NamedTypeExpr{Name: "Float"}, "float64"},
		{&ast.NamedTypeExpr{Name: "Bool"}, "bool"},
		{
			&ast.OptionalTypeExpr{Inner: &ast.NamedTypeExpr{Name: "String"}},
			"*string",
		},
		{
			&ast.NamedTypeExpr{
				Name:     "List",
				TypeArgs: []ast.TypeExpr{&ast.NamedTypeExpr{Name: "Int"}},
			},
			"[]int",
		},
		{
			&ast.FnTypeExpr{
				Params:     []ast.TypeExpr{&ast.NamedTypeExpr{Name: "String"}},
				ReturnType: &ast.NamedTypeExpr{Name: "Bool"},
			},
			"func(string) bool",
		},
	} {
		got := e.EmitTypeName(c.in)
		if got != c.want {
			t.Errorf("EmitTypeName(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/codegen/... -run TestEmitTypeName -v
```
Expected: `FAIL — package codegen_test: cannot find package`

- [ ] **Step 3: Implement `internal/codegen/codegen.go`**

```go
// Package codegen emits Go source from a Splash AST.
package codegen

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
)

// Emitter accumulates Go source for a single Splash file.
type Emitter struct {
	body          strings.Builder
	imports       map[string]bool
	indent        int
	needsCoalesce bool
	needsAudit    bool
	approvedFns   map[string]bool
}

// New creates a ready-to-use Emitter.
func New() *Emitter {
	return &Emitter{
		imports:     make(map[string]bool),
		approvedFns: make(map[string]bool),
	}
}

// EmitFile generates a complete Go source file from a Splash AST.
func (e *Emitter) EmitFile(f *ast.File) string {
	// Pass 1: collect @approve function names
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				e.approvedFns[fn.Name] = true
			}
		}
	}

	// Pass 2: emit declarations into body buffer
	for _, decl := range f.Declarations {
		e.emitDecl(decl)
	}

	// Helpers need imports — add them after body emission
	if e.needsAudit {
		e.imports["encoding/json"] = true
		e.imports["os"] = true
		e.imports["time"] = true
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
		for imp := range e.imports {
			fmt.Fprintf(&out, "\t%q\n", imp)
		}
		out.WriteString(")\n\n")
	}

	if e.needsCoalesce {
		out.WriteString(splashCoalesceHelper)
		out.WriteString("\n")
	}
	if e.needsAudit {
		out.WriteString(splashAuditHelper)
		out.WriteString("\n")
	}

	out.WriteString(e.body.String())
	return out.String()
}

// EmitTypeName returns the Go type string for a Splash TypeExpr.
// It is exported for testing.
func (e *Emitter) EmitTypeName(t ast.TypeExpr) string {
	return e.emitTypeName(t)
}

func (e *Emitter) emitTypeName(t ast.TypeExpr) string {
	if t == nil {
		return ""
	}
	switch typ := t.(type) {
	case *ast.NamedTypeExpr:
		switch typ.Name {
		case "String":
			return "string"
		case "Int":
			return "int"
		case "Float":
			return "float64"
		case "Bool":
			return "bool"
		case "List":
			if len(typ.TypeArgs) == 1 {
				return "[]" + e.emitTypeName(typ.TypeArgs[0])
			}
			return "[]any"
		default:
			return typ.Name
		}
	case *ast.OptionalTypeExpr:
		return "*" + e.emitTypeName(typ.Inner)
	case *ast.FnTypeExpr:
		var params []string
		for _, p := range typ.Params {
			params = append(params, e.emitTypeName(p))
		}
		ret := e.emitTypeName(typ.ReturnType)
		return fmt.Sprintf("func(%s) %s", strings.Join(params, ", "), ret)
	}
	return "any"
}

// --- write helpers ---

func (e *Emitter) writef(format string, args ...any) {
	fmt.Fprintf(&e.body, format, args...)
}

func (e *Emitter) writeLine(format string, args ...any) {
	fmt.Fprintf(&e.body, strings.Repeat("\t", e.indent)+format+"\n", args...)
}

// --- runtime helper sources ---

const splashCoalesceHelper = `func splashCoalesce[T any](val *T, fallback T) T {
	if val == nil {
		return fallback
	}
	return *val
}
`

const splashAuditHelper = `func splashAudit(fn string, ts time.Time) {
	enc := json.NewEncoder(os.Stdout)
	enc.Encode(map[string]any{"fn": fn, "ts": ts.Format(time.RFC3339)})
}
`

// emitDecl dispatches to the appropriate declaration emitter.
// Defined here as a placeholder; filled out in Task 2.
func (e *Emitter) emitDecl(d ast.Decl) {}
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./internal/codegen/... -run TestEmitTypeName -v
```
Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/codegen.go internal/codegen/codegen_test.go
git commit -m "feat: codegen scaffolding — Emitter, EmitFile, type names"
```

---

### Task 2: Declaration emission — struct, enum, function signature

**Files:**
- Create: `internal/codegen/decl.go`
- Modify: `internal/codegen/codegen_test.go` (add test)

- [ ] **Step 1: Write the failing test**

Add to `codegen_test.go`:

```go
// helper used in Tasks 2-6
func parse(t *testing.T, src string) *ast.File {
	t.Helper()
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, diags := p.ParseFile()
	for _, d := range diags {
		t.Logf("parse diagnostic: %s", d)
	}
	return file
}

func emitSrc(t *testing.T, src string) string {
	t.Helper()
	f := parse(t, src)
	e := codegen.New()
	return e.EmitFile(f)
}

func mustGoSyntax(t *testing.T, src string) {
	t.Helper()
	fset := gotoken.NewFileSet()
	_, err := goparser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		t.Errorf("generated Go has syntax errors: %v\nsource:\n%s", err, src)
	}
}

func TestEmitTypeDecl(t *testing.T) {
	src := `
module user
type User { name: String, age: Int }
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "type User struct") {
		t.Errorf("expected 'type User struct', got:\n%s", out)
	}
	if !strings.Contains(out, "name string") {
		t.Errorf("expected field 'name string', got:\n%s", out)
	}
}

func TestEmitEnumDecl(t *testing.T) {
	src := `
module result
enum Status { Pending, Done, Failed }
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "type Status interface") {
		t.Errorf("expected 'type Status interface', got:\n%s", out)
	}
	if !strings.Contains(out, "StatusPending") {
		t.Errorf("expected variant type StatusPending, got:\n%s", out)
	}
}

func TestEmitFunctionDecl(t *testing.T) {
	src := `
module greet
fn greet(name: String) -> String {
    return name
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "func greet(name string) string") {
		t.Errorf("expected function signature, got:\n%s", out)
	}
}
```

Add imports to `codegen_test.go`:
```go
import (
	"strings"
	"testing"

	goparser "go/parser"
	gotoken "go/token"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
)
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/codegen/... -run "TestEmitTypeDecl|TestEmitEnumDecl|TestEmitFunctionDecl" -v
```
Expected: `FAIL — emitDecl is a no-op`

- [ ] **Step 3: Create `internal/codegen/decl.go`**

```go
package codegen

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
)

func (e *Emitter) emitDecl(d ast.Decl) {
	switch decl := d.(type) {
	case *ast.TypeDecl:
		e.emitTypeDecl(decl)
	case *ast.EnumDecl:
		e.emitEnumDecl(decl)
	case *ast.FunctionDecl:
		e.emitFunctionDecl(decl)
	// ModuleDecl, UseDecl, ConstraintDecl: skip in v0.1
	}
}

func (e *Emitter) emitTypeDecl(decl *ast.TypeDecl) {
	e.writef("type %s struct {\n", decl.Name)
	e.indent++
	for _, field := range decl.Fields {
		e.writeLine("%s %s", field.Name, e.emitTypeName(field.Type))
	}
	e.indent--
	e.writeLine("}\n")
}

func (e *Emitter) emitEnumDecl(decl *ast.EnumDecl) {
	// Marker interface: type Color interface{ colorVariant() }
	marker := strings.ToLower(decl.Name[:1]) + decl.Name[1:] + "Variant"
	e.writef("type %s interface{ %s() }\n\n", decl.Name, marker)
	// Variant structs: type ColorRed struct{} / func (ColorRed) colorVariant() {}
	for _, v := range decl.Variants {
		typeName := decl.Name + v.Name
		if v.Payload != nil {
			e.writef("type %s struct{ Value %s }\n", typeName, e.emitTypeName(v.Payload))
		} else {
			e.writef("type %s struct{}\n", typeName)
		}
		e.writef("func (%s) %s() {}\n\n", typeName, marker)
	}
}

func (e *Emitter) emitFunctionDecl(decl *ast.FunctionDecl) {
	sig := e.funcSignature(decl)
	e.writef("%s {\n", sig)
	e.indent++
	e.emitBlock(decl.Body)
	e.indent--
	e.writeLine("}\n")
}

func (e *Emitter) funcSignature(decl *ast.FunctionDecl) string {
	var params []string
	for _, p := range decl.Params {
		params = append(params, fmt.Sprintf("%s %s", p.Name, e.emitTypeName(p.Type)))
	}
	ret := e.emitTypeName(decl.ReturnType)
	sig := fmt.Sprintf("func %s(%s)", decl.Name, strings.Join(params, ", "))
	if ret != "" {
		sig += " " + ret
	}
	return sig
}

// emitBlock is defined in stmt.go; declared here as a forward reference.
// (Go doesn't need forward declarations — this comment is for the reader.)
```

Also update `codegen.go`: remove the placeholder `emitDecl` stub (it was defined as a no-op).

- [ ] **Step 4: Run test to verify it passes**

```
go test ./internal/codegen/... -run "TestEmitTypeDecl|TestEmitEnumDecl|TestEmitFunctionDecl" -v
```
Expected: `FAIL` — `emitBlock` not defined yet. That's expected; the function body is empty so only `TestEmitFunctionDecl` will fail on syntax if `{` has no matching close... actually `emitFunctionDecl` calls `emitBlock` which doesn't exist yet.

Add a stub `emitBlock` in `decl.go` temporarily:
```go
// temporary stub — replaced in Task 3
func (e *Emitter) emitBlock(b *ast.BlockStmt) {}
```

Re-run:
```
go test ./internal/codegen/... -run "TestEmitTypeDecl|TestEmitEnumDecl|TestEmitFunctionDecl" -v
```
Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/decl.go internal/codegen/codegen_test.go internal/codegen/codegen.go
git commit -m "feat: codegen type/enum/function declaration emission"
```

---

### Task 3: Statement emission

**Files:**
- Create: `internal/codegen/stmt.go`
- Modify: `internal/codegen/codegen_test.go` (add test)
- Modify: `internal/codegen/decl.go` (remove stub emitBlock)

- [ ] **Step 1: Write the failing test**

Add to `codegen_test.go`:

```go
func TestEmitStatements(t *testing.T) {
	src := `
module calc
fn add(x: Int, y: Int) -> Int {
    let result = x
    return result
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "result := x") {
		t.Errorf("expected let-binding 'result := x', got:\n%s", out)
	}
	if !strings.Contains(out, "return result") {
		t.Errorf("expected 'return result', got:\n%s", out)
	}
}

func TestEmitIfStmt(t *testing.T) {
	src := `
module check
fn isPositive(n: Int) -> Bool {
    if n > 0 {
        return true
    }
    return false
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "if (n > 0)") {
		t.Errorf("expected 'if (n > 0)', got:\n%s", out)
	}
}

func TestEmitForStmt(t *testing.T) {
	src := `
module loop
fn printAll(items: List<String>) {
    for item in items {
        let x = item
    }
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "for _, item := range items") {
		t.Errorf("expected 'for _, item := range items', got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/codegen/... -run "TestEmitStatements|TestEmitIfStmt|TestEmitForStmt" -v
```
Expected: `FAIL` — stub `emitBlock` emits nothing

- [ ] **Step 3: Create `internal/codegen/stmt.go`**

```go
package codegen

import "gosplash.dev/splash/internal/ast"

func (e *Emitter) emitBlock(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		e.emitStmt(s)
	}
}

func (e *Emitter) emitStmt(s ast.Stmt) {
	switch stmt := s.(type) {
	case *ast.LetStmt:
		e.emitLetStmt(stmt)
	case *ast.ReturnStmt:
		e.emitReturnStmt(stmt)
	case *ast.ExprStmt:
		e.emitExprStmt(stmt)
	case *ast.IfStmt:
		e.emitIfStmt(stmt)
	case *ast.GuardStmt:
		e.emitGuardStmt(stmt)
	case *ast.ForStmt:
		e.emitForStmt(stmt)
	case *ast.AssignStmt:
		e.writeLine("%s = %s", e.emitExprStr(stmt.Target), e.emitExprStr(stmt.Value))
	case *ast.BlockStmt:
		e.writeLine("{")
		e.indent++
		e.emitBlock(stmt)
		e.indent--
		e.writeLine("}")
	}
}

func (e *Emitter) emitLetStmt(s *ast.LetStmt) {
	if s.Type != nil {
		e.writeLine("var %s %s = %s", s.Name, e.emitTypeName(s.Type), e.emitExprStr(s.Value))
	} else {
		e.writeLine("%s := %s", s.Name, e.emitExprStr(s.Value))
	}
}

func (e *Emitter) emitReturnStmt(s *ast.ReturnStmt) {
	if s.Value == nil {
		e.writeLine("return")
		return
	}
	// @approve call inside return: emit audit first
	if call, ok := s.Value.(*ast.CallExpr); ok {
		if ident, ok2 := call.Callee.(*ast.Ident); ok2 && e.approvedFns[ident.Name] {
			e.needsAudit = true
			e.writeLine("splashAudit(%q, time.Now())", ident.Name)
		}
	}
	e.writeLine("return %s", e.emitExprStr(s.Value))
}

func (e *Emitter) emitExprStmt(s *ast.ExprStmt) {
	// @approve call as a statement: emit audit first
	if call, ok := s.Expr.(*ast.CallExpr); ok {
		if ident, ok2 := call.Callee.(*ast.Ident); ok2 && e.approvedFns[ident.Name] {
			e.needsAudit = true
			e.writeLine("splashAudit(%q, time.Now())", ident.Name)
		}
	}
	e.writeLine("%s", e.emitExprStr(s.Expr))
}

func (e *Emitter) emitIfStmt(s *ast.IfStmt) {
	e.writeLine("if %s {", e.emitExprStr(s.Cond))
	e.indent++
	e.emitBlock(s.Then)
	e.indent--
	if s.Else != nil {
		e.writeLine("} else {")
		e.indent++
		e.emitStmt(s.Else)
		e.indent--
	}
	e.writeLine("}")
}

func (e *Emitter) emitGuardStmt(s *ast.GuardStmt) {
	e.writeLine("if !(%s) {", e.emitExprStr(s.Cond))
	e.indent++
	e.emitBlock(s.Else)
	e.indent--
	e.writeLine("}")
}

func (e *Emitter) emitForStmt(s *ast.ForStmt) {
	e.writeLine("for _, %s := range %s {", s.Binding, e.emitExprStr(s.Iter))
	e.indent++
	e.emitBlock(s.Body)
	e.indent--
	e.writeLine("}")
}
```

Also remove the stub `emitBlock` from `decl.go`. It's now in `stmt.go`.

`emitExprStr` is needed by `stmt.go` and will be defined in `expr.go` (Task 4). Add a stub in `stmt.go` or `codegen.go` for now:

In `codegen.go`, add a temporary stub after the `emitDecl` (which no longer needs a stub now):
```go
// temporary stub — replaced in Task 4
func (e *Emitter) emitExprStr(expr ast.Expr) string { return "nil" }
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./internal/codegen/... -run "TestEmitStatements|TestEmitIfStmt|TestEmitForStmt" -v
```
Expected: `PASS` (let-binding and return emit correctly; if condition is `(nil > nil)` but syntax is valid; for uses the iter expr as `nil` but structure is correct)

Note: `TestEmitIfStmt` expects `if (n > 0)` which requires `emitExprStr` to work. It will fail until Task 4. That's fine — the test is written for the full implementation.

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/stmt.go internal/codegen/decl.go internal/codegen/codegen.go
git commit -m "feat: codegen statement emission"
```

---

### Task 4: Expression emission

**Files:**
- Create: `internal/codegen/expr.go`
- Modify: `internal/codegen/codegen.go` (remove `emitExprStr` stub)
- Modify: `internal/codegen/codegen_test.go` (add tests)

- [ ] **Step 1: Write the failing test**

Add to `codegen_test.go`:

```go
func TestEmitBinaryExpr(t *testing.T) {
	src := `
module math
fn sum(a: Int, b: Int) -> Int {
    return a + b
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "(a + b)") {
		t.Errorf("expected '(a + b)', got:\n%s", out)
	}
}

func TestEmitCallExpr(t *testing.T) {
	src := `
module greet
fn hello(name: String) -> String {
    return name
}
fn main() {
    hello("world")
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, `hello("world")`) {
		t.Errorf("expected 'hello(\"world\")', got:\n%s", out)
	}
}

func TestEmitStructLiteral(t *testing.T) {
	src := `
module user
type User { name: String }
fn makeUser() -> User {
    return User { name: "alice" }
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, `User{name: "alice"}`) {
		t.Errorf("expected struct literal, got:\n%s", out)
	}
}

func TestEmitListLiteral(t *testing.T) {
	src := `
module lists
fn nums() -> List<Int> {
    return [1, 2, 3]
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "[]any{1, 2, 3}") {
		t.Errorf("expected list literal, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/codegen/... -run "TestEmitBinaryExpr|TestEmitCallExpr|TestEmitStructLiteral|TestEmitListLiteral" -v
```
Expected: `FAIL` — stub returns `"nil"` for all exprs

- [ ] **Step 3: Create `internal/codegen/expr.go`**

```go
package codegen

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/token"
)

// emitExprStr returns the Go string for a Splash expression.
func (e *Emitter) emitExprStr(expr ast.Expr) string {
	if expr == nil {
		return "nil"
	}
	switch ex := expr.(type) {
	case *ast.IntLiteral:
		return fmt.Sprintf("%d", ex.Value)
	case *ast.FloatLiteral:
		return fmt.Sprintf("%g", ex.Value)
	case *ast.StringLiteral:
		return fmt.Sprintf("%q", ex.Value)
	case *ast.BoolLiteral:
		if ex.Value {
			return "true"
		}
		return "false"
	case *ast.NoneLiteral:
		return "nil"
	case *ast.Ident:
		return ex.Name
	case *ast.BinaryExpr:
		return e.emitBinaryExpr(ex)
	case *ast.UnaryExpr:
		return e.emitUnaryExpr(ex)
	case *ast.CallExpr:
		return e.emitCallExpr(ex)
	case *ast.MemberExpr:
		return fmt.Sprintf("%s.%s", e.emitExprStr(ex.Object), ex.Member)
	case *ast.IndexExpr:
		return fmt.Sprintf("%s[%s]", e.emitExprStr(ex.Object), e.emitExprStr(ex.Index))
	case *ast.NullCoalesceExpr:
		e.needsCoalesce = true
		return fmt.Sprintf("splashCoalesce(%s, %s)", e.emitExprStr(ex.Left), e.emitExprStr(ex.Right))
	case *ast.StructLiteral:
		return e.emitStructLiteral(ex)
	case *ast.ListLiteral:
		return e.emitListLiteral(ex)
	case *ast.ClosureExpr:
		return e.emitClosureExpr(ex)
	case *ast.MatchExpr:
		// v0.1: match not supported in codegen — emit panic
		return `(func() any { panic("match: not supported in v0.1 codegen") })()`
	}
	return "nil"
}

var binaryOpMap = map[token.Kind]string{
	token.PLUS:    "+",
	token.MINUS:   "-",
	token.STAR:    "*",
	token.SLASH:   "/",
	token.PERCENT: "%",
	token.EQ:      "==",
	token.NEQ:     "!=",
	token.LT:      "<",
	token.LTE:     "<=",
	token.GT:      ">",
	token.GTE:     ">=",
	token.AND_AND: "&&",
	token.OR_OR:   "||",
}

func (e *Emitter) emitBinaryExpr(ex *ast.BinaryExpr) string {
	op, ok := binaryOpMap[ex.Op]
	if !ok {
		op = "/* unknown op */"
	}
	return fmt.Sprintf("(%s %s %s)", e.emitExprStr(ex.Left), op, e.emitExprStr(ex.Right))
}

func (e *Emitter) emitUnaryExpr(ex *ast.UnaryExpr) string {
	switch ex.Op {
	case token.MINUS:
		return fmt.Sprintf("(-%s)", e.emitExprStr(ex.Operand))
	case token.BANG:
		return fmt.Sprintf("(!%s)", e.emitExprStr(ex.Operand))
	}
	return e.emitExprStr(ex.Operand)
}

func (e *Emitter) emitCallExpr(ex *ast.CallExpr) string {
	var args []string
	for _, arg := range ex.Args {
		args = append(args, e.emitExprStr(arg))
	}
	return fmt.Sprintf("%s(%s)", e.emitExprStr(ex.Callee), strings.Join(args, ", "))
}

func (e *Emitter) emitStructLiteral(ex *ast.StructLiteral) string {
	var fields []string
	for _, f := range ex.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", f.Name, e.emitExprStr(f.Value)))
	}
	return fmt.Sprintf("%s{%s}", ex.TypeName, strings.Join(fields, ", "))
}

func (e *Emitter) emitListLiteral(ex *ast.ListLiteral) string {
	var elems []string
	for _, el := range ex.Elements {
		elems = append(elems, e.emitExprStr(el))
	}
	return fmt.Sprintf("[]any{%s}", strings.Join(elems, ", "))
}

func (e *Emitter) emitClosureExpr(ex *ast.ClosureExpr) string {
	var params []string
	for _, p := range ex.Params {
		params = append(params, fmt.Sprintf("%s %s", p.Name, e.emitTypeName(p.Type)))
	}
	body := e.emitExprStr(ex.Body)
	return fmt.Sprintf("func(%s) any { return %s }", strings.Join(params, ", "), body)
}
```

Remove the stub `emitExprStr` from `codegen.go`.

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/codegen/... -v
```
Expected: All tests pass, including the statement tests from Task 3 that needed real expressions (`TestEmitIfStmt` should now pass with `(n > 0)`).

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/expr.go internal/codegen/codegen.go
git commit -m "feat: codegen expression emission"
```

---

### Task 5: Optionals + null coalesce + @approve audit log

**Files:**
- Modify: `internal/codegen/codegen_test.go` (add tests)
- No new files — null coalesce is in `expr.go`, @approve is in `stmt.go` (already wired)

The `NullCoalesceExpr` path in `emitExprStr` already sets `e.needsCoalesce = true` and emits `splashCoalesce(...)`. The @approve audit in `emitExprStmt` and `emitReturnStmt` already sets `e.needsAudit = true`. This task adds tests that verify both work end-to-end and that `splashCoalesce` / `splashAudit` appear in the preamble.

- [ ] **Step 1: Write the failing tests**

Add to `codegen_test.go`:

```go
func TestNullCoalesceEmitsHelper(t *testing.T) {
	src := `
module optional
fn greeting(name: String?) -> String {
    return name ?? "stranger"
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "splashCoalesce(name, \"stranger\")") {
		t.Errorf("expected splashCoalesce call, got:\n%s", out)
	}
	if !strings.Contains(out, "func splashCoalesce") {
		t.Errorf("expected splashCoalesce helper in preamble, got:\n%s", out)
	}
}

func TestOptionalParamType(t *testing.T) {
	src := `
module optional
fn greet(name: String?) -> String {
    return name ?? "world"
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "name *string") {
		t.Errorf("expected 'name *string' for optional param, got:\n%s", out)
	}
}

func TestApproveAuditLog(t *testing.T) {
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
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, `splashAudit("processPayment", time.Now())`) {
		t.Errorf("expected splashAudit call before processPayment, got:\n%s", out)
	}
	if !strings.Contains(out, "func splashAudit") {
		t.Errorf("expected splashAudit helper in preamble, got:\n%s", out)
	}
	if !strings.Contains(out, `"encoding/json"`) {
		t.Errorf("expected encoding/json import, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/codegen/... -run "TestNullCoalesceEmitsHelper|TestOptionalParamType|TestApproveAuditLog" -v
```
Expected: `FAIL` — `splashCoalesce` and `splashAudit` checks will fail until plumbing is verified

- [ ] **Step 3: Verify wiring is complete**

Run the tests. If they fail, trace:
1. `TestNullCoalesceEmitsHelper`: `NullCoalesceExpr` in `emitExprStr` sets `e.needsCoalesce = true` → `EmitFile` emits `splashCoalesceHelper`. If it fails, check `NullCoalesceExpr` is being parsed and that `emitExprStr` is being called via `emitReturnStmt`.
2. `TestOptionalParamType`: `OptionalTypeExpr` → `*T` in `emitTypeName`. If it fails, check that `funcSignature` calls `emitTypeName(p.Type)` for each param.
3. `TestApproveAuditLog`: `@approve` on `processPayment` → `e.approvedFns["processPayment"] = true` in `EmitFile` pass 1. `emitExprStmt` for `processPayment(100)` checks `e.approvedFns[ident.Name]` → emits `splashAudit(...)`. If it fails, verify `emitExprStmt` in `stmt.go` has the `@approve` check.

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/codegen/... -v
```
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/codegen/codegen_test.go
git commit -m "feat: codegen optionals, null coalesce, @approve audit log"
```

---

### Task 6: `cmd/splash` CLI + end-to-end test

**Files:**
- Create: `cmd/splash/main.go`
- Modify: `internal/codegen/codegen_test.go` (add end-to-end test)

- [ ] **Step 1: Write the failing end-to-end test**

Add to `codegen_test.go`:

```go
func TestEmitFileValidGo_EndToEnd(t *testing.T) {
	// A complete Splash program that exercises types, functions, and expressions.
	src := `
module main
type Point { x: Int, y: Int }
fn add(a: Int, b: Int) -> Int {
    return a + b
}
fn makePoint(x: Int, y: Int) -> Point {
    return Point { x: x, y: y }
}
fn main() {
    let result = add(1, 2)
    let pt = makePoint(result, 0)
    let _ = pt
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)

	// Verify key structure
	if !strings.Contains(out, "package main") {
		t.Errorf("expected 'package main', got:\n%s", out)
	}
	if !strings.Contains(out, "type Point struct") {
		t.Errorf("expected Point struct, got:\n%s", out)
	}
	if !strings.Contains(out, "func add(a int, b int) int") {
		t.Errorf("expected add signature, got:\n%s", out)
	}
	if !strings.Contains(out, "Point{x: x, y: y}") {
		t.Errorf("expected Point struct literal, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or note status)**

```
go test ./internal/codegen/... -run TestEmitFileValidGo_EndToEnd -v
```
Expected: Should pass given prior tasks. If it fails, fix before proceeding.

- [ ] **Step 3: Create `cmd/splash/main.go`**

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/typechecker"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: splash <check|build> <file.splash> [-o output]")
		os.Exit(1)
	}
	cmd, file := os.Args[1], os.Args[2]

	switch cmd {
	case "check":
		if err := runCheck(file); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "build":
		out := strings.TrimSuffix(filepath.Base(file), ".splash")
		for i := 3; i < len(os.Args)-1; i++ {
			if os.Args[i] == "-o" {
				out = os.Args[i+1]
			}
		}
		if err := runBuild(file, out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func parseFile(path string) (*ast.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	toks := lexer.New(path, string(src)).Tokenize()
	p := parser.New(path, toks)
	f, diags := p.ParseFile()
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(diags) > 0 {
		return nil, fmt.Errorf("parse errors in %s", path)
	}
	return f, nil
}

func runCheck(path string) error {
	f, err := parseFile(path)
	if err != nil {
		return err
	}

	tc := typechecker.New()
	_, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}

	g := callgraph.Build(f)
	sc := safety.New()
	safetyErrs := sc.Check(f, g)
	for _, d := range safetyErrs {
		fmt.Fprintln(os.Stderr, d)
	}

	if len(typeErrs)+len(safetyErrs) > 0 {
		return fmt.Errorf("check failed")
	}
	fmt.Printf("%s: ok\n", path)
	return nil
}

func runBuild(path, out string) error {
	f, err := parseFile(path)
	if err != nil {
		return err
	}

	tc := typechecker.New()
	_, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(typeErrs) > 0 {
		return fmt.Errorf("type errors")
	}

	g := callgraph.Build(f)
	sc := safety.New()
	safetyErrs := sc.Check(f, g)
	for _, d := range safetyErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(safetyErrs) > 0 {
		return fmt.Errorf("safety errors")
	}

	e := codegen.New()
	goSrc := e.EmitFile(f)

	// For splash build, generated file must be package main
	if f.Module != nil && f.Module.Name != "main" {
		goSrc = strings.Replace(goSrc, "package "+f.Module.Name+"\n", "package main\n", 1)
	}

	tmpDir, err := os.MkdirTemp("", "splash-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goSrc), 0644); err != nil {
		return err
	}
	goMod := "module splashbuild\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return err
	}

	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = tmpDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

Note: `parseFile` uses `*ast.File` but `ast` needs to be imported. Add `"gosplash.dev/splash/internal/ast"` to the import list.

- [ ] **Step 4: Build the CLI and verify it compiles**

```
go build ./cmd/splash/...
```
Expected: binary produced, no errors.

- [ ] **Step 5: Manual smoke test**

Create a test file `/tmp/hello.splash`:
```
module hello
fn greet(name: String) -> String {
    return name
}
fn main() {
    let msg = greet("world")
    let _ = msg
}
```

Run:
```
./splash check /tmp/hello.splash
```
Expected: `hello.splash: ok`

Run:
```
./splash build /tmp/hello.splash -o /tmp/hello
```
Expected: `/tmp/hello` binary produced. `go build` succeeds.

- [ ] **Step 6: Run full test suite**

```
go test ./...
```
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/splash/main.go internal/codegen/codegen_test.go
git commit -m "feat: cmd/splash check and build commands, end-to-end codegen"
```

---

## Known Limitations (v0.1)

- **`match` expressions:** emit `panic("match: not supported in v0.1 codegen")`. Track as follow-up.
- **Optional chaining (`?.`):** emitted as regular member access (no nil guard). Safe for non-nil values only.
- **`List<T>` literals:** typed as `[]any` — full type inference required for proper `[]T`. Track as follow-up.
- **`Result<T,E>`:** mapped to `any`. Track as follow-up.
- **Context threading (`needs` effects):** deferred entirely. `splash build` produces standalone executables; effect declarations are compile-time-checked but not runtime-enforced in v0.1.
- **`@approve` suspension:** emits audit log only. No runtime suspension mechanism. See Phase 4.
- **`package main` override in `runBuild`:** string-replaces the package declaration. Correct for executables; modules targeting library use need `splash lib` command (future).

## Self-Review Against Spec

**Type mapping:** `String→string`, `Int→int`, `Float→float64`, `Bool→bool`, `T?→*T`, `List<T>→[]T`, `fn(A)→B→func(A) B` — all covered in Task 1.

**Optionals→pointers:** `OptionalTypeExpr` in `emitTypeName` returns `*T` ✓. `NoneLiteral` returns `nil` ✓. `NullCoalesceExpr` emits `splashCoalesce` ✓.

**`@approve` audit log:** scanned in `EmitFile` pass 1, emitted as `splashAudit(fn, time.Now())` in `emitExprStmt` and `emitReturnStmt` ✓. Helper emitted in preamble when needed ✓. Imports (`encoding/json`, `os`, `time`) added ✓.

**`splash check`:** parse + type check + safety check → exit 1 on errors ✓.

**`splash build`:** full pipeline → codegen → `go build` ✓.

**Context threading:** explicitly deferred ✓ (spec says v0.1 only).
