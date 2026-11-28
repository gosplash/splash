# Splash — Working Context for AI Assistants

This file is the working context for AI coding assistants. Read it before editing any file in this repo. The README covers what things are; this file covers how to work with them.

---

## What This Repo Is

A compiler for Splash, written in Go, with a Go backend and an experimental VM. Source policy checks run before either backend sees the analyzed program. VM admission, validation, and runtime limits have separate checks. Go approval delegates to the configured host adapter.

---

## Before You Edit Anything

1. **Run `go test ./...`** — all tests must pass before and after your change.
2. **Write the failing test first.** Every new enforcement check, codegen behavior, or language feature has a test. See test patterns below.
3. **Use existing packages for changes.** The execution architecture defines separate `internal/compiler/internal/lowering`, `internal/ir`, `internal/bytecode`, and `internal/vm` packages. See [the IR and VM design](docs/design/splash-ir-and-vm.md) when changing their interfaces.
4. **Don't create helpers for one-off uses.** No premature abstraction.
5. **Don't add error handling for things that can't fail.** Trust internal invariants.

---

## Package Responsibilities (and What Not to Do in Each)

### `internal/lexer`
Tokenizes `.splash` source. Do not add language semantics here. If a new token kind is needed, add it to `internal/token`.

### `internal/parser`
Pratt parser. Produces `*ast.File` and validates that integer literals fit the AST's `int64` representation. Put operand typing and binding in `internal/typechecker`. Resolved call contracts use its evidence in `internal/effects`. Put graph-based policy checks in `internal/safety`.

### `internal/ast`
AST node types and annotation kinds. `annotation.go` has `AnnotationKind` constants and the string→kind lookup map. When adding a new annotation, add the constant and the string mapping — that's all. No logic here.

### `internal/effects`
`EffectSet` bitmask, `Lookup()`, and `Parse()`. The vocabulary table in `effects.go` drives parsing and rendering. When the specification adds an effect, add its constant and vocabulary entry together.

### `internal/typechecker`
Type inference, constraint satisfaction, `use` import resolution. The type checker runs before safety checks. It should not enforce `@redline` or `@approve` — those belong in the safety checker. It does enforce `@sensitive` on `println` (logging constraint) because that's a type-system property, not a call graph property.

`SetFileLoader(baseDir, readFileFn)` must be called before `Check()` if the file uses `use` declarations. `LoadedTypedFiles()` returns imported modules to the compiler in dependency order.

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

Keep safety independent of code generation and type checking. `Check` consumes module-owned ASTs, resolved types, the call graph, and inferred effect summaries supplied by shared analysis. Parser-only fixtures pass `nil` for inference evidence.

### `internal/compiler`
Owns captured source and resolved evidence. `Analyze` returns an inspectable `AnalyzedProgram`. `Verify` mints `VerifiedProgram` using the shared source policy checks. All source artifact entry points require that handle. Keep inspection projections detached from compiler state, and keep raw source artifact producers within `internal/compiler/internal`.

### `internal/compiler/internal/codegen`
`Backend` interface + `GoBackend` implementation. The `Emitter` is the internal implementation — don't expose more of it. `Options` carries per-compilation settings (currently `ApprovalCallers`). Adding a new codegen option means adding a field to `Options` — nothing else in the CLI needs to change.

The `Emitter` has per-function state (`inApprovalFn`, `currentFnReturnType`, `currentFnIsMain`) cleared after each function. If adding new per-function codegen state, follow this pattern.

`@approve` cascade: the compiler computes `approveFns` + `g.Callers(approveFns)` and passes the result as `Options.ApprovalCallers`. The emitter uses this to widen return types and inject error propagation. The emitter does NOT import callgraph — the compiler bridges them.

### `internal/compiler/internal/toolschema`
`VerifiedProgram.ExportTools` passes the owning checked modules to `toolschema.Extract`. Project checked parameter types in `TypeToSchema`. Unsupported parameters fail export before serialization.

### `cmd/splash/main.go`
CLI wiring only. Route `check`, `emit`, `build`, `tools`, and VM `run` through `compiler.Verify`. Graph, effects, and approvals use detached projections of the analysis. Keep compiler policy in `internal/compiler` and `internal/safety`.

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

### Codegen tests (`internal/compiler/internal/codegen/codegen_test.go`)
Tests check that generated Go source contains expected substrings. Use `containsStr(src, substr)`. For `@approve` cascade tests, use `emitSrcWithApproval(src)` which builds the call graph and sets `ApprovalCallers` before emitting.

### Type checker tests (`internal/typechecker/typechecker_test.go`)
`typecheck(src)` returns `(TypeMap, []diagnostic.Diagnostic)`. Check for presence/absence of errors and for specific type assignments.

---

## Settled Design Decisions

**Effect checking is static.** Source contracts declare authority. Infer effects only from resolved calls and modeled operations. Carry incomplete coverage separately from known effects.

**Source policy precedes lowering.** Go emission, VM lowering, and tool export take a `VerifiedProgram`. Native builds, VM admission, and runtime checks keep their own failure modes.

**No escape hatches yet.** There is no `@unsafe` or `@suppress`. `@bypass` is on the v0.2 roadmap with required justification and audit logging.

**`@approve` widens return types.** An `@approve` function gets `(T, error)` in Go. This cascades through all transitive callers. `main()` is the exception — it calls `os.Exit(1)` instead of returning error.

**`Agent` is structural, not a capability.** It marks agent entry points for call graph analysis. `@sandbox` allow/deny checks exclude the `Agent` effect.

**Multi-file modules use flat symbol injection.** `use billing` makes `billing`'s exported symbols available without a namespace prefix. Cycles are rejected. The `expose` list controls what's injected.

**The compiler bridges callgraph and codegen.** It passes resolved facts to private Go lowering via `Options`.

**`Backend` interface is the LLVM extension point.** Code generation targets `Backend`. The current implementation is `GoBackend`. A new backend implements `Backend` and adds a factory function — the CLI and safety model are unchanged.

---

## Effect Vocabulary (complete list)

Use the [specified vocabulary](SPEC.md#52-effect-vocabulary). The type checker validates `needs` and the safety checker validates `@sandbox` lists using `effects.Lookup`. Keep graph summaries and diagnostics on the same vocabulary.

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

Before opening a pull request that substantially changes the compiler or VM, pin the diff and run three independent, read-only reviews in parallel. Assign one reviewer to language semantics and compatibility, one to compiler and runtime boundaries, and one to conformance tests and repository standards. Address blocking findings, rerun the relevant tests, and send corrective changes back to the reviewer who found the issue. Summarize the review and validation in the pull request.

When creating any pull request, use the template in `.github/PULL_REQUEST_TEMPLATE.md`.
