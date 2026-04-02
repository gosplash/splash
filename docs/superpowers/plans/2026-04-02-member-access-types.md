# Phase 3c-A: Member Access Type Resolution

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `MemberExpr` in the type checker so that field access (`p.name`, `p?.nickname`) resolves to the declared field type instead of `Unknown`.

**Architecture:** The type checker's `checkExpr` has a `MemberExpr` case that currently calls `checkExpr` on the object but always returns `types.Unknown`. The fix adds a `resolveMemberType` helper that: (1) unwraps any optional wrapping on the object type, (2) looks up the `ast.TypeDecl` in `tc.typeDecls`, (3) finds the field by name, (4) resolves and returns its type. Optional chaining (`?.`) wraps the result in `OptionalType`. Unknown fields produce a type error. The codegen already emits `obj.Member` correctly — this is a type checker-only change.

**Tech Stack:** Go stdlib only. No new dependencies.

---

## Existing code to understand before starting

**`internal/typechecker/typechecker.go`** — the file you'll change. Key facts:
- `TypeChecker` struct has `typeDecls map[string]*ast.TypeDecl` populated in `pass1`
- `checkExpr` is the expression type resolver — the `MemberExpr` case is at around line 210
- `resolveTypeExpr` converts an `ast.TypeExpr` to a `types.Type` — use this to resolve field types
- `tc.errorf(pos, format, args...)` records a diagnostic error

**`internal/ast/expr.go`** — `MemberExpr` has fields `Object Expr`, `Member string`, `Optional bool`

**`internal/ast/decl.go`** — `TypeDecl` has `Fields []FieldDecl`; `FieldDecl` has `Name string` and `Type TypeExpr`

**`internal/types/types.go`** — `NamedType` has `Name string`; `OptionalType` has `Inner Type`

**`internal/typechecker/typechecker_test.go`** — uses a `check(src string) []diagnostic.Diagnostic` helper and `hasError` — use these in new tests.

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/typechecker/typechecker.go` | Modify | Fix `MemberExpr` case; add `resolveMemberType` helper |
| `internal/typechecker/typechecker_test.go` | Modify | Tests for field access, optional fields, optional chaining, unknown field error, end-to-end |

---

## Task 1: Basic member access resolution

Resolve `p.name` on a named type to the field's declared type. Handle optional fields (`nickname: String?`). Produce a type error for unknown fields.

**Files:**
- Modify: `internal/typechecker/typechecker.go`
- Modify: `internal/typechecker/typechecker_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/typechecker/typechecker_test.go`:

```go
func TestMemberAccess_BasicField(t *testing.T) {
	// p.name on Person should resolve — no type errors
	src := `
module demo
type Person { name: String; age: Int }
fn get_name(p: Person) -> String { return p.name }
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for basic field access")
	}
}

func TestMemberAccess_OptionalField(t *testing.T) {
	// p.nickname is declared String? — returning it as String? should be fine
	src := `
module demo
type Person { name: String; nickname: String? }
fn get_nick(p: Person) -> String? { return p.nickname }
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for optional field access")
	}
}

func TestMemberAccess_UnknownField(t *testing.T) {
	src := `
module demo
type Person { name: String }
fn bad(p: Person) -> String { return p.nonexistent }
`
	if !hasError(check(src)) {
		t.Error("expected type error for unknown field access")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/zagraves/Code/gosplash
go test ./internal/typechecker/... -run "TestMemberAccess" -v
```

Expected: `TestMemberAccess_BasicField` and `TestMemberAccess_OptionalField` FAIL (because `p.name` still returns `Unknown`, so the return type mismatch may or may not trigger). `TestMemberAccess_UnknownField` FAIL (no error produced yet).

Note: if `TestMemberAccess_BasicField` accidentally passes (because `Unknown` is assignable to everything), that's OK — `TestMemberAccess_UnknownField` not producing an error is the definitive failure.

- [ ] **Step 3: Add `resolveMemberType` helper to `typechecker.go`**

Add this method to `TypeChecker`, after `buildFunctionType` (around line 73):

```go
// resolveMemberType resolves the type of a field access on a named type.
// objType is the type of the object; member is the field name; optional indicates
// the ?. operator was used (result is wrapped in OptionalType).
func (tc *TypeChecker) resolveMemberType(objType types.Type, member string, optional bool, pos token.Position) types.Type {
	// Unwrap optional: Person? → Person for field lookup
	inner := objType
	if opt, ok := objType.(*types.OptionalType); ok {
		inner = opt.Inner
	}

	nt, ok := inner.(*types.NamedType)
	if !ok {
		// Primitive or unknown — cannot resolve fields
		return types.Unknown
	}

	decl, ok := tc.typeDecls[nt.Name]
	if !ok {
		// Named type not in scope (forward ref not registered — shouldn't happen after pass1)
		return types.Unknown
	}

	for _, field := range decl.Fields {
		if field.Name == member {
			fieldType := tc.resolveTypeExpr(field.Type)
			if optional {
				// ?. propagates nil: if field is already T?, keep it; otherwise wrap
				if _, isOpt := fieldType.(*types.OptionalType); !isOpt {
					return &types.OptionalType{Inner: fieldType}
				}
			}
			return fieldType
		}
	}

	tc.errorf(pos, "type %s has no field %q", nt.Name, member)
	return types.Unknown
}
```

- [ ] **Step 4: Update the `MemberExpr` case in `checkExpr`**

Find this block in `checkExpr` (around line 210):

```go
case *ast.MemberExpr:
    tc.checkExpr(e.Object, env, typed, typeParamEnv)
    result = types.Unknown // field type resolution is future work
```

Replace it with:

```go
case *ast.MemberExpr:
    objType := tc.checkExpr(e.Object, env, typed, typeParamEnv)
    result = tc.resolveMemberType(objType, e.Member, e.Optional, e.Pos())
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/typechecker/... -run "TestMemberAccess" -v
```

Expected: all three PASS.

- [ ] **Step 6: Run the full test suite**

```bash
go test ./...
```

Expected: all packages PASS. No regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/typechecker/typechecker.go internal/typechecker/typechecker_test.go
git commit --no-gpg-sign -m "feat: resolve member access types from TypeDecl field declarations"
```

---

## Task 2: Optional chaining, type propagation, and end-to-end

Verify `?.` wraps the result in `OptionalType`, and that the full hello.splash pattern — struct literal → binding → member access → null coalesce — type-checks cleanly end-to-end.

**Files:**
- Modify: `internal/typechecker/typechecker_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/typechecker/typechecker_test.go`:

```go
func TestMemberAccess_OptionalChaining(t *testing.T) {
	// p?.name where p: Person? should resolve without error
	// The ?. operator means: if p is nil, return nil; otherwise return p.name
	src := `
module demo
type Person { name: String }
fn get_name(p: Person?) -> String? { return p?.name }
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for optional chaining")
	}
}

func TestMemberAccess_ChainedAccess(t *testing.T) {
	// Multi-field type: access two different fields in the same function
	src := `
module demo
type Point { x: Int; y: Int }
fn sum(p: Point) -> Int { return p.x + p.y }
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for chained field access")
	}
}

func TestMemberAccess_HelloSplashPattern(t *testing.T) {
	// The hello.splash example: Person with optional nickname, null coalesce
	src := `
module hello
type Person { name: String; nickname: String? }
fn greet(p: Person) -> String {
    let display_name = p.nickname ?? p.name
    return "Hello, " + display_name
}
fn main() {
    let p = Person { name: "world", nickname: none }
    println(greet(p))
}
`
	if hasError(check(src)) {
		t.Error("unexpected type errors in hello.splash pattern")
	}
}

func TestMemberAccess_WrongReturnType(t *testing.T) {
	// p.age is Int — returning it as String should be a type error
	src := `
module demo
type Person { name: String; age: Int }
fn bad(p: Person) -> String { return p.age }
`
	if !hasError(check(src)) {
		t.Error("expected type error: returning Int field as String")
	}
}
```

- [ ] **Step 2: Run tests to verify current state**

```bash
go test ./internal/typechecker/... -run "TestMemberAccess" -v
```

Expected: `TestMemberAccess_OptionalChaining`, `TestMemberAccess_ChainedAccess`, and `TestMemberAccess_HelloSplashPattern` PASS (Task 1's fix handles these). `TestMemberAccess_WrongReturnType` may or may not pass depending on whether `Unknown` is assignable to `String` — if it accidentally passes, that's OK since `TestMemberAccess_UnknownField` already validates error reporting.

If any tests unexpectedly fail, investigate before proceeding.

- [ ] **Step 3: Run the full test suite**

```bash
go test ./...
```

Expected: all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/typechecker/typechecker_test.go
git commit --no-gpg-sign -m "test: member access type resolution — optional chaining and end-to-end coverage"
```
