# Splash — Working Context for AI Assistants

This file is the working context for AI coding assistants. Read it before editing any file in this repo. The README covers what things are; this file covers how to work with them.

---

## What This Repo Is

A compiler for the Splash language, written in Go. Splash transpiles to Go. The compiler's job is to enforce safety properties — effects, data classification, agent reachability — before the Go backend sees the AST. Every safety guarantee is a compile-time property. There is no runtime enforcement layer (except `@approve` adapter dispatch, which is opt-in and explicit).

---

## Before You Edit Anything

1. **Run `go test ./...`** — all tests must pass before and after your change.
2. **Write the failing test first.** Every new enforcement check, codegen behavior, or language feature has a test. See test patterns below.
3. **Don't add packages.** The package structure is settled. Add to existing files.
4. **Don't create helpers for one-off uses.** No premature abstraction.
5. **Don't add error handling for things that can't fail.** Trust internal invariants.

---

## Package Responsibilities (and What Not to Do in Each)

### `internal/lexer`
Tokenizes `.splash` source. Do not add language semantics here. If a new token kind is needed, add it to `internal/token`.

### `internal/parser`
Pratt parser. Produces `*ast.File`. Do not do type checking or effect resolution here. The parser's job is AST shape, not meaning. New syntax goes here; new constraints go in the type checker or safety checker.

### `internal/ast`
AST node types and annotation kinds. `annotation.go` has `AnnotationKind` constants and the string→kind lookup map. When adding a new annotation, add the constant and the string mapping — that's all. No logic here.

### `internal/effects`
`EffectSet` bitmask and `Parse()`. The full effect vocabulary is in `effects.go`. Adding a new effect means adding a constant and a case in `Parse()` and `String()`. Keep effects as a closed set — new effects require language spec changes.

### `internal/typechecker`
Type inference, constraint satisfaction, `use` import resolution. The type checker runs before safety checks. It should not enforce `@redline` or `@approve` — those belong in the safety checker. It does enforce `@sensitive` on `println` (logging constraint) because that's a type-system property, not a call graph property.

`SetFileLoader(baseDir, readFileFn)` must be called before `Check()` if the file uses `use` declarations. `LoadedFiles()` returns imported files for `mergeFiles` in the CLI.

### `internal/callgraph`
Call graph construction and reachability. `Build()` walks the AST. Do not add enforcement logic here — the graph is a data structure. Safety checks belong in `internal/safety`.

Key methods:
- `Reachable(roots)` — forward BFS from roots
- `Callers(targets)` — reverse BFS (used for `@approve` cascade)
- `Parents(roots)` — BFS parent map for error path reconstruction
- `PathTo(parents, target)` — reconstruct call path string slice

### `internal/safety`
All compile-time enforcement passes. `Check()` calls each pass and returns all diagnostics. **This is where new enforcement goes.** Each pass is a method on `*Checker`.

To add a new check:
1. Write failing tests in `checker_test.go` using the `check(src)` helper
2. Add `checkFoo(file, g)` method to `checker.go`
3. Call it from `Check()`

Do not import `codegen` from safety. Do not import `typechecker` from safety. The safety checker works directly on AST + call graph.

### `internal/codegen`
`Backend` interface + `GoBackend` implementation. The `Emitter` is the internal implementation — don't expose more of it. `Options` carries per-compilation settings (currently `ApprovalCallers`). Adding a new codegen option means adding a field to `Options` — nothing else in the CLI needs to change.

The `Emitter` has per-function state (`inApprovalFn`, `currentFnReturnType`, `currentFnIsMain`) cleared after each function. If adding new per-function codegen state, follow this pattern.

`@approve` cascade: the CLI computes `approveFns` + `g.Callers(approveFns)` and passes the result as `Options.ApprovalCallers`. The emitter uses this to widen return types and inject error propagation. The emitter does NOT import callgraph — the CLI bridges them.

### `internal/toolschema`
Extracts `ToolSchema` (JSON Schema) from `@tool` functions. `Extract(file)` returns all tools. `ExtractReachable(file, reachable)` filters to agent-reachable tools. The schema maps Splash types to JSON Schema types — add new type mappings in `typeToSchema()`.

### `cmd/splash/main.go`
CLI wiring only. `parseFile`, `runCheck`, `runEmit`, `runBuild`, `runTools`. Each command follows the same pattern: parse → type check → call graph → safety check → codegen. Do not add business logic here. `mergeFiles` handles multi-file module merging for codegen.

---

## Test Patterns

### Safety checker tests (`internal/safety/checker_test.go`)
```go
func TestFoo_ViolationCase(t *testing.T) {
    src := `
module foo
// ... minimal Splash source that triggers the violation
`
    diags := check(src)
    if !hasError(diags) {
        t.Error("expected error: ...")
    }
}

func TestFoo_CleanCase(t *testing.T) {
    src := `...`
    diags := check(src)
    if hasError(diags) {
        t.Errorf("unexpected errors: %v", diags)
    }
}
```

Always test both the violation and the clean path.

### Codegen tests (`internal/codegen/codegen_test.go`)
Tests check that generated Go source contains expected substrings. Use `containsStr(src, substr)`. For `@approve` cascade tests, use `emitSrcWithApproval(src)` which builds the call graph and sets `ApprovalCallers` before emitting.

### Type checker tests (`internal/typechecker/typechecker_test.go`)
`typecheck(src)` returns `(TypeMap, []diagnostic.Diagnostic)`. Check for presence/absence of errors and for specific type assignments.

---

## Settled Design Decisions

**Effect checking is static.** No dynamic effect dispatch. Effects are declared, not inferred.

**Safety is pre-codegen.** The Go backend sees a verified AST. No enforcement happens in generated Go code (except `@approve` gate dispatch, which is explicit runtime behavior, not enforcement).

**No escape hatches yet.** There is no `@unsafe` or `@suppress`. `@bypass` is on the v0.2 roadmap with required justification and audit logging.

**`@approve` widens return types.** An `@approve` function gets `(T, error)` in Go. This cascades through all transitive callers. `main()` is the exception — it calls `os.Exit(1)` instead of returning error.

**`Agent` is structural, not a capability.** It marks agent entry points for call graph analysis. `@sandbox` allow/deny checks exclude the `Agent` effect.

**Multi-file modules use flat symbol injection.** `use billing` makes `billing`'s exported symbols available without a namespace prefix. Cycles are rejected. The `expose` list controls what's injected.

**The CLI bridges callgraph and codegen.** `internal/codegen` does not import `internal/callgraph`. The CLI computes what codegen needs and passes it via `Options`.

**`Backend` interface is the LLVM extension point.** Code generation targets `Backend`. The current implementation is `GoBackend`. A new backend implements `Backend` and adds a factory function — the CLI and safety model are unchanged.

---

## Effect Vocabulary (complete list)

`DB.read`, `DB.write`, `DB.admin`, `DB` (expands to read+write), `Net`, `AI`, `Agent`, `FS`, `Exec`, `Cache`, `Secrets`, `Secrets.read`, `Secrets.write`, `Queue`, `Metric`, `Store`, `Clock`

Unknown effect names in `needs` clauses are caught by the type checker. Unknown names in `@sandbox` allow/deny lists are silently ignored by `effects.Parse()` (the type checker catches them in `needs` context; sandbox lists share the same parser).

---

## What's In Progress / Not Done

- `std/db`, `std/cache`, `std/http` stdlib adapters — not started
- Runtime `@budget` enforcement — compile-time type validation is done; counter injection in `ai.prompt` calls requires `std/ai` runtime
- `SlackApproval`, `WebhookApproval` non-blocking adapters — not started
- `@bypass` auditable escape hatch — not started
- LLVM backend — `Backend` interface is the extension point; Go is the only current implementation
- `splash deploy` with capability manifest diffing — not started

Do not implement any of the above without a plan in `docs/superpowers/plans/`.

---

## Commit Style

- `feat:` new language features or enforcement
- `fix:` bug fixes
- `test:` test-only changes
- `docs:` whitepaper, README, CLAUDE.md, examples
- `refactor:` internal restructuring with no behavior change
