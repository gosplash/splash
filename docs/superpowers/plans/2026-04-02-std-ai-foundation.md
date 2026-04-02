# Phase 3c-B: `std/ai` Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support the whitepaper's `ai.prompt<T>(...)` pattern end-to-end in the compiler frontend: parse generic call syntax, inject `ai`/`AIError` from `use std/ai`, and type-check `ai.prompt<T>` → `Result<T, AIError>`.

**Architecture:** Three compiler-only changes, no runtime or AI provider calls. Task 1 adds `TypeArgs` to `CallExpr` (pure AST). Task 2 adds generic call syntax to the parser using a non-mutating forward scan for disambiguation. Task 3 wires `use std/ai` into the typechecker — injecting `ai: AIAdapter` and `AIError` into scope, then type-checking `ai.prompt<T>` to `Result<T, AIError>`. Each task builds on the previous.

**Tech Stack:** Go stdlib only. No new dependencies.

---

## Existing code to understand before starting

**`internal/ast/expr.go`** — `CallExpr` has `Callee Expr`, `Args []Expr`, `Position`. No `TypeArgs` field yet. `MemberExpr` has `Object Expr`, `Member string`, `Optional bool`.

**`internal/parser/parser.go`** — Pratt parser. `tokenPrecedence` maps `token.LT` → `precCompare`. The infix loop switch handles `DOT`, `OPT_CHAIN`, `LPAREN`, `LBRACKET`, `NULL_COAL`, `ASSIGN`, and a `default` binary case. `LT` falls into `default` (comparison). Parser struct has `tokens []token.Token` and `pos int`, enabling random-access lookahead without mutation. `isGenericCallAhead()` can scan `p.tokens[p.pos+k]` without changing `p.pos`.

**`internal/typechecker/typechecker.go`** — `pass1` iterates `file.Declarations` only. `file.Uses` (type `[]*ast.UseDecl`) is available but currently ignored. `checkCallExpr` handles generic type param inference via `fnDecls` but does not consult `CallExpr.TypeArgs` (field doesn't exist yet). `resolveTypeExprWithParams` already handles `Result<T, E>` at line 404.

**`internal/ast/node.go`** — `File.Uses []*UseDecl` — populated by the parser.

**`internal/typechecker/typechecker_test.go`** — uses `check(src string) []diagnostic.Diagnostic` and `hasError`. Tests use 2-space indentation inside Splash source strings (match existing style).

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/ast/expr.go` | Modify | Add `TypeArgs []TypeExpr` to `CallExpr` |
| `internal/parser/parser.go` | Modify | `isGenericCallAhead()` helper; `case token.LT` in infix loop for generic calls |
| `internal/parser/parser_test.go` | Modify | Tests for generic call parsing and LT-as-comparison regression |
| `internal/typechecker/typechecker.go` | Modify | `processUseDecl` injects `ai`/`AIError`; `checkCallExpr` types `ai.prompt<T>` |
| `internal/typechecker/typechecker_test.go` | Modify | Tests for `ai.prompt<T>` type resolution |

---

## Task 1: Add `TypeArgs` to `CallExpr`

This is a pure AST change. All existing code continues to work because `TypeArgs` is nil by default.

**Files:**
- Modify: `internal/ast/expr.go`

- [ ] **Step 1: Add `TypeArgs []TypeExpr` to `CallExpr`**

In `internal/ast/expr.go`, change:

```go
// CallExpr is a function call expression.
type CallExpr struct {
	Callee   Expr
	Args     []Expr
	Position token.Position
}
```

To:

```go
// CallExpr is a function call expression.
type CallExpr struct {
	Callee   Expr
	Args     []Expr
	TypeArgs []TypeExpr
	Position token.Position
}
```

- [ ] **Step 2: Run full test suite to verify no regressions**

```bash
cd /Users/zagraves/Code/gosplash
go test ./...
```

Expected: all pass. `TypeArgs` is nil by default so all existing call sites still work.

- [ ] **Step 3: Commit**

```bash
git add internal/ast/expr.go
git commit --no-gpg-sign -m "feat: add TypeArgs to CallExpr for generic call syntax"
```

---

## Task 2: Parse generic call syntax `f<T>(...)`

Adds parser support for `ai.prompt<SermonInsight>(text)`. The key challenge: `<` is also the less-than comparison operator. Disambiguation uses a non-mutating forward scan (`isGenericCallAhead`) that checks whether the token sequence from the current position matches `< IDENT[?] [, IDENT[?]]* > (`.

**Files:**
- Modify: `internal/parser/parser.go`
- Modify: `internal/parser/parser_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/parser/parser_test.go` (inside `package parser_test`):

```go
func TestParseGenericCall_SingleTypeArg(t *testing.T) {
	src := `
module demo
fn test(ai: String) -> String {
  return ai.prompt<SermonInsight>(ai)
}
`
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("unexpected parse errors: %v", diags)
	}
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatal("expected FunctionDecl")
	}
	ret, ok := fn.Body.Stmts[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatal("expected ReturnStmt")
	}
	call, ok := ret.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", ret.Value)
	}
	if len(call.TypeArgs) != 1 {
		t.Fatalf("expected 1 type arg, got %d", len(call.TypeArgs))
	}
	typeArg, ok := call.TypeArgs[0].(*ast.NamedTypeExpr)
	if !ok {
		t.Fatalf("expected NamedTypeExpr, got %T", call.TypeArgs[0])
	}
	if typeArg.Name != "SermonInsight" {
		t.Errorf("expected type arg SermonInsight, got %q", typeArg.Name)
	}
	if len(call.Args) != 1 {
		t.Errorf("expected 1 call arg, got %d", len(call.Args))
	}
}

func TestParseGenericCall_StillParseComparisonLT(t *testing.T) {
	// a < b must still parse as a comparison, not as a generic call
	src := `
module demo
fn test(a: Int, b: Int) -> Bool {
  return a < b
}
`
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("unexpected parse errors: %v", diags)
	}
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatal("expected FunctionDecl")
	}
	ret, ok := fn.Body.Stmts[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatal("expected ReturnStmt")
	}
	bin, ok := ret.Value.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr for a < b, got %T", ret.Value)
	}
	if bin.Op != token.LT {
		t.Errorf("expected LT op, got %v", bin.Op)
	}
}
```

Note: the `parse` helper and `token` package are already imported in the test file. `token.LT` is available via `gosplash.dev/splash/internal/token`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/parser/... -run "TestParseGenericCall" -v
```

Expected: `TestParseGenericCall_SingleTypeArg` FAIL (generic call not yet supported). `TestParseGenericCall_StillParseComparisonLT` may PASS (comparison already works).

- [ ] **Step 3: Add `isGenericCallAhead()` to `parser.go`**

Add this method to `Parser` after `parseCallExpr` (around line 925):

```go
// isGenericCallAhead returns true if the token sequence from the current position
// matches a generic call's type-argument list: < TypeIdent[?] [, TypeIdent[?]]* > (
// This scan is non-mutating — it reads p.tokens by index without changing p.pos.
func (p *Parser) isGenericCallAhead() bool {
	i := p.pos
	if i >= len(p.tokens) || p.tokens[i].Kind != token.LT {
		return false
	}
	i++ // past <
	if i >= len(p.tokens) || p.tokens[i].Kind != token.IDENT {
		return false
	}
	i++ // past first type arg ident
	// optional ? for optional type (e.g. <Foo?>)
	if i < len(p.tokens) && p.tokens[i].Kind == token.QUESTION {
		i++
	}
	// optional comma-separated additional type args
	for i < len(p.tokens) && p.tokens[i].Kind == token.COMMA {
		i++ // past comma
		if i >= len(p.tokens) || p.tokens[i].Kind != token.IDENT {
			return false
		}
		i++ // past type arg ident
		if i < len(p.tokens) && p.tokens[i].Kind == token.QUESTION {
			i++
		}
	}
	// must be > then (
	if i >= len(p.tokens) || p.tokens[i].Kind != token.GT {
		return false
	}
	i++
	return i < len(p.tokens) && p.tokens[i].Kind == token.LPAREN
}
```

- [ ] **Step 4: Add `case token.LT:` to the infix loop in `parseExpr`**

In `parseExpr` (around line 808), the `switch cur.Kind` currently has `DOT`, `OPT_CHAIN`, `LPAREN`, `LBRACKET`, `NULL_COAL`, `ASSIGN`, and `default`. Add a `case token.LT:` before `default`:

```go
		case token.LT:
			if p.isGenericCallAhead() {
				// Generic call: callee<TypeArgs>(args)
				p.advance() // past <
				var typeArgs []ast.TypeExpr
				for !p.check(token.GT) && !p.check(token.EOF) {
					typeArgs = append(typeArgs, p.parseTypeExpr())
					if !p.check(token.COMMA) {
						break
					}
					p.advance()
				}
				p.eat(token.GT)
				callPos := p.current().Pos
				p.eat(token.LPAREN)
				var args []ast.Expr
				for !p.check(token.RPAREN) && !p.check(token.EOF) {
					args = append(args, p.parseExpr(precLowest))
					if !p.check(token.COMMA) {
						break
					}
					p.advance()
				}
				p.eat(token.RPAREN)
				left = &ast.CallExpr{Callee: left, Args: args, TypeArgs: typeArgs, Position: callPos}
			} else {
				// Comparison: left < right
				p.advance()
				right := p.parseExpr(prec + 1)
				left = &ast.BinaryExpr{Left: left, Op: cur.Kind, Right: right, Position: cur.Pos}
			}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/parser/... -run "TestParseGenericCall" -v
```

Expected: both PASS.

- [ ] **Step 6: Run full parser suite to confirm no regressions**

```bash
go test ./internal/parser/... -v
```

Expected: all PASS.

- [ ] **Step 7: Run the full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/parser/parser.go internal/parser/parser_test.go
git commit --no-gpg-sign -m "feat: parse generic call syntax f<T>(...) with LT disambiguation"
```

---

## Task 3: Type-check `ai.prompt<T>` → `Result<T, AIError>`

Two sub-tasks: (a) process `use std/ai` in the typechecker to inject `ai: AIAdapter` and `AIError` into scope; (b) in `checkCallExpr`, detect `ai.prompt<T>` calls and return `Result<T, AIError>`.

**Files:**
- Modify: `internal/typechecker/typechecker.go`
- Modify: `internal/typechecker/typechecker_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/typechecker/typechecker_test.go`:

```go
func TestAIPrompt_BasicReturn(t *testing.T) {
	// ai.prompt<SermonInsight>(text) should return Result<SermonInsight, AIError>
	// and be valid as the return value of a function returning that type.
	src := `
module demo
use std/ai

type SermonInsight {
  title: String
}

async fn analyze(text: String) needs AI -> Result<SermonInsight, AIError> {
  return ai.prompt<SermonInsight>(text)
}
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for ai.prompt<T> call")
	}
}

func TestAIPrompt_WrongReturnType(t *testing.T) {
	// ai.prompt<Foo> returns Result<Foo, AIError>, not Result<Bar, AIError>
	src := `
module demo
use std/ai

type Foo { x: Int }
type Bar { y: String }

async fn bad(text: String) needs AI -> Result<Bar, AIError> {
  return ai.prompt<Foo>(text)
}
`
	if !hasError(check(src)) {
		t.Error("expected type error: ai.prompt<Foo> returned as Result<Bar, AIError>")
	}
}

func TestAIPrompt_NoStdAi_AiIsUndefined(t *testing.T) {
	// Without use std/ai, 'ai' is not in scope
	src := `
module demo

type Foo { x: Int }

fn bad(text: String) -> Foo {
  return ai.prompt<Foo>(text)
}
`
	if !hasError(check(src)) {
		t.Error("expected error: ai is undefined without use std/ai")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/typechecker/... -run "TestAIPrompt" -v
```

Expected: all three FAIL (`ai` is undefined, `AIError` is unknown, prompt<T> not handled).

- [ ] **Step 3: Add `processUseDecl` and call it from `pass1`**

In `typechecker.go`, update `pass1` to process `file.Uses` first:

```go
// pass1: register all top-level declarations (handles forward references).
func (tc *TypeChecker) pass1(file *ast.File) {
	for _, u := range file.Uses {
		tc.processUseDecl(u)
	}
	for _, decl := range file.Declarations {
		switch d := decl.(type) {
		case *ast.TypeDecl:
			tc.typeDecls[d.Name] = d
			tc.globals.Set(d.Name, tc.buildNamedType(d))
		case *ast.ConstraintDecl:
			tc.constraintDecls[d.Name] = d
		case *ast.FunctionDecl:
			tc.fnDecls[d.Name] = d
			tc.globals.Set(d.Name, tc.buildFunctionType(d))
		case *ast.EnumDecl:
			tc.globals.Set(d.Name, types.Named(d.Name))
		}
	}
}

// processUseDecl injects stdlib module bindings into globals.
func (tc *TypeChecker) processUseDecl(u *ast.UseDecl) {
	switch u.Path {
	case "std/ai":
		// Inject 'ai' as an AIAdapter instance and 'AIError' as a named type.
		tc.globals.Set("ai", types.Named("AIAdapter"))
		tc.globals.Set("AIError", types.Named("AIError"))
	}
}
```

- [ ] **Step 4: Add AIAdapter method dispatch in `checkCallExpr`**

In `checkCallExpr`, add detection for `ai.prompt<T>` before the existing logic. The existing method starts at around line 308. Add at the top of the method body (before `calleeType := tc.checkExpr(...)`):

```go
func (tc *TypeChecker) checkCallExpr(e *ast.CallExpr, env *Env, typed *TypedFile, typeParamEnv map[string]*types.TypeParamType) types.Type {
	// Special case: constraint method call on AIAdapter — ai.prompt<T>(args) → Result<T, AIError>
	if mem, ok := e.Callee.(*ast.MemberExpr); ok && mem.Member == "prompt" {
		objType := tc.checkExpr(mem.Object, env, typed, typeParamEnv)
		if nt, ok2 := objType.(*types.NamedType); ok2 && nt.Name == "AIAdapter" {
			for _, arg := range e.Args {
				tc.checkExpr(arg, env, typed, typeParamEnv)
			}
			if len(e.TypeArgs) == 1 {
				t := tc.resolveTypeExprWithParams(e.TypeArgs[0], typeParamEnv)
				return &types.ResultType{Ok: t, Err: types.Named("AIError")}
			}
			return types.Unknown
		}
	}

	calleeType := tc.checkExpr(e.Callee, env, typed, typeParamEnv)
	// ... rest of existing checkCallExpr unchanged
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/typechecker/... -run "TestAIPrompt" -v
```

Expected: all three PASS.

- [ ] **Step 6: Run the full typechecker suite**

```bash
go test ./internal/typechecker/... -v
```

Expected: all PASS. No regressions.

- [ ] **Step 7: Run the full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/typechecker/typechecker.go internal/typechecker/typechecker_test.go
git commit --no-gpg-sign -m "feat: type-check ai.prompt<T> via std/ai injection → Result<T, AIError>"
```
