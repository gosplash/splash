# Phase 4a: @approve Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace call-site `splashAudit` with function-body `splashApprove`, backed by a swappable `ApprovalAdapter` interface and a `StdinApproval` default that blocks until the operator says yes.

**Architecture:** `@approve` is a precondition — the function body doesn't run until the adapter approves. In generated Go, `splashApprove("name")` is injected as the first statement of the function body. The function signature does not change. `StdinApproval` loops on stdin until the operator types `y` or kills the process. The `ApprovalAdapter` interface defines `Request(name string) error` (nil = approved) so Phase 4b production adapters (`SlackApproval`, `WebhookApproval`) can return real denial errors without an interface change. In Phase 4a, `StdinApproval.Request` always returns nil; `splashApprove` panics if it ever receives a non-nil error (dead code in Phase 4a, placeholder for Phase 4b).

**Tech Stack:** Go, standard library only (`bufio`, `fmt`, `os`, `strings`)

**Phase 4b scope (explicitly out):** `Result<T, ApprovalError>` return type codegen, call-site error propagation, denial without process kill.

---

## File Map

| File | Change |
|---|---|
| `internal/codegen/codegen_test.go` | Rename `TestApproveAuditLog` → `TestApproveGate`, update assertions; add `TestApprovalAdapterSwap` |
| `internal/codegen/codegen.go` | Remove `needsAudit`/`approvedFns`, add `needsApproval`; replace `splashAuditHelper` const with `splashApprovalHelper`; update import set |
| `internal/codegen/decl.go` | `emitFunctionDecl` checks `@approve` annotation, emits `splashApprove(name)` as first statement of body |
| `internal/codegen/stmt.go` | Remove `@approve` call-site injection from `emitReturnStmt` and `emitExprStmt` |

---

## Task 1: Write failing tests

**Files:**
- Modify: `internal/codegen/codegen_test.go`

- [ ] **Step 1: Rename `TestApproveAuditLog` to `TestApproveGate` and update assertions**

Replace the existing `TestApproveAuditLog` function (lines 294–316) with:

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
	out := emitSrc(t, src)
	mustGoSyntax(t, out)

	// Gate injected at top of @approve function body
	if !strings.Contains(out, `splashApprove("processPayment")`) {
		t.Errorf("expected splashApprove at function body top, got:\n%s", out)
	}
	// Old call-site audit must be gone
	if strings.Contains(out, "splashAudit(") {
		t.Errorf("splashAudit should be removed, got:\n%s", out)
	}
	// Call site in run() must NOT have injection
	runIdx := strings.Index(out, "func run()")
	if runIdx >= 0 && strings.Contains(out[runIdx:], "splashApprove(") {
		t.Errorf("splashApprove must not appear at call site in run(), got:\n%s", out)
	}
	// Helper in preamble
	if !strings.Contains(out, "func splashApprove") {
		t.Errorf("expected splashApprove helper in preamble, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Add `TestApprovalAdapterSwap` after `TestApproveGate`**

```go
func TestApprovalAdapterSwap(t *testing.T) {
	src := `
module payments
@approve
fn processPayment(amount: Int) {
    let x = amount
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)

	if !strings.Contains(out, "type ApprovalAdapter interface") {
		t.Errorf("expected ApprovalAdapter interface, got:\n%s", out)
	}
	if !strings.Contains(out, "func SetApprovalAdapter") {
		t.Errorf("expected SetApprovalAdapter swap function, got:\n%s", out)
	}
	if !strings.Contains(out, "splashStdinApproval") {
		t.Errorf("expected splashStdinApproval default impl, got:\n%s", out)
	}
	if !strings.Contains(out, `"bufio"`) {
		t.Errorf("expected bufio import, got:\n%s", out)
	}
}
```

- [ ] **Step 3: Run the tests — both must fail**

```
go test ./internal/codegen/... -run "TestApproveGate|TestApprovalAdapterSwap" -v
```

Expected output:
```
--- FAIL: TestApproveGate
    expected splashApprove at function body top
--- FAIL: TestApprovalAdapterSwap
    expected ApprovalAdapter interface
FAIL
```

- [ ] **Step 4: Commit the failing tests**

```bash
git add internal/codegen/codegen_test.go
git commit --no-gpg-sign -m "test: expect @approve body gate and swappable adapter"
```

---

## Task 2: Implement body injection and adapter runtime

**Files:**
- Modify: `internal/codegen/codegen.go`
- Modify: `internal/codegen/decl.go`
- Modify: `internal/codegen/stmt.go`

- [ ] **Step 1: Update `Emitter` struct and `New()` in `codegen.go`**

Remove `needsAudit bool` and `approvedFns map[string]bool`. Add `needsApproval bool`.

```go
type Emitter struct {
	body          strings.Builder
	imports       map[string]bool
	indent        int
	needsCoalesce bool
	needsApproval bool
}

func New() *Emitter {
	return &Emitter{
		imports: make(map[string]bool),
	}
}
```

- [ ] **Step 2: Replace `EmitFile` body in `codegen.go`**

Remove pass-1 (the `approvedFns` collection loop). Replace the `needsAudit` import/helper block with `needsApproval`. Full replacement:

```go
func (e *Emitter) EmitFile(f *ast.File) string {
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

- [ ] **Step 3: Replace `splashAuditHelper` with `splashApprovalHelper` in `codegen.go`**

Delete the `splashAuditHelper` const and add:

```go
const splashApprovalHelper = `// ApprovalAdapter is the interface for @approve gate implementations.
// Request blocks until the named function is approved.
// Return nil to approve; return an error to deny.
// StdinApproval (the default) loops until the operator types y — it never returns an error.
// Production adapters (SlackApproval, WebhookApproval) can return ApprovalError on denial;
// full denial handling via Result<T, ApprovalError> is Phase 4b.
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

func splashApprove(name string) {
	if err := _splashApproval.Request(name); err != nil {
		// StdinApproval never reaches here.
		// Phase 4b production adapters return denial errors — handled via Result<T, ApprovalError>.
		panic(fmt.Sprintf("approval denied for %s: %v", name, err))
	}
}
`
```

- [ ] **Step 4: Inject `splashApprove` at function body top in `decl.go`**

Replace `emitFunctionDecl`:

```go
func (e *Emitter) emitFunctionDecl(decl *ast.FunctionDecl) {
	sig := e.funcSignature(decl)
	e.writef("%s {\n", sig)
	e.indent++
	for _, ann := range decl.Annotations {
		if ann.Kind == ast.AnnotApprove {
			e.needsApproval = true
			e.writeLine("splashApprove(%q)", decl.Name)
			break
		}
	}
	e.emitBlock(decl.Body)
	e.indent--
	e.writeLine("}\n")
}
```

- [ ] **Step 5: Remove call-site injection from `stmt.go`**

Replace `emitReturnStmt` — remove the `@approve` call-site block (lines 53–58):

```go
func (e *Emitter) emitReturnStmt(s *ast.ReturnStmt) {
	if s.Value == nil {
		e.writeLine("return")
		return
	}
	e.writeLine("return %s", e.emitExprStr(s.Value))
}
```

Replace `emitExprStmt` — remove the `@approve` call-site block (lines 63–69):

```go
func (e *Emitter) emitExprStmt(s *ast.ExprStmt) {
	e.writeLine("%s", e.emitExprStr(s.Expr))
}
```

- [ ] **Step 6: Run the codegen tests — all must pass**

```
go test ./internal/codegen/... -v
```

Expected: all tests pass including `TestApproveGate` and `TestApprovalAdapterSwap`. If `TestApproveGate` fails on the `runIdx` assertion, verify the generated output puts `splashApprove` inside `func processPayment` and not inside `func run`.

- [ ] **Step 7: Run the full test suite**

```
go test ./...
```

Expected: PASS. No regressions in parser, typechecker, safety, callgraph, or toolschema.

- [ ] **Step 8: Commit**

```bash
git add internal/codegen/codegen.go internal/codegen/decl.go internal/codegen/stmt.go
git commit --no-gpg-sign -m "feat: @approve body gate with swappable ApprovalAdapter, StdinApproval default"
```

---

## Self-Review

**Spec coverage:**
- `@approve` injection moves from call site to function body ✅ Task 2 Step 4
- `ApprovalAdapter` interface with `Request(name string) error` ✅ Task 2 Step 3
- `_splashApproval` package-level var defaulting to `&splashStdinApproval{}` ✅ Task 2 Step 3
- `SetApprovalAdapter` swap function ✅ Task 2 Step 3
- `StdinApproval.Request` loops until `y`, never returns error ✅ Task 2 Step 3
- `splashApprove` panics on non-nil (dead code in 4a, documented) ✅ Task 2 Step 3
- Old `splashAudit` / `approvedFns` / call-site injection removed ✅ Task 2 Steps 1, 2, 5
- Tests written before implementation ✅ Task 1
- Function signature does NOT change — `@approve` is a pure precondition ✅ Task 2 Step 4

**Placeholder scan:** None — all code blocks are complete and runnable.

**Type consistency:** `ann.Kind == ast.AnnotApprove` matches existing usage. `needsApproval` used consistently in `codegen.go` (struct field, EmitFile assembly) and `decl.go` (setter). `splashApprovalHelper` const name matches reference in `EmitFile`.

**Phase boundary:** Call-site error propagation (`c, err := charge_card(amount)`) and `Result<T, ApprovalError>` return type codegen are explicitly Phase 4b. The `ApprovalAdapter.Request` interface signature is defined now with `error` return so Phase 4b production adapters don't require a breaking interface change.
