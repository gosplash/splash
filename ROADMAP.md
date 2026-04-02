# Splash Roadmap

What's shipped, what's next, and why things are ordered the way they are.

---

## Shipped

**Language core**
- Lexer, parser, type checker, effect system
- Call graph construction and agent-reachability analysis
- Go codegen, `Backend` interface abstraction
- `splash check`, `splash build`, `splash emit`, `splash tools`

**Safety enforcement (all compile-time)**
- `@redline` — absolute agent lockout with call path in error
- `@approve` — human gate; `(T, error)` cascade through all transitive callers (Phase 4a + 4b)
- `@containment` — module-level agent access policy
- `@sandbox` — configurable effect allow/deny against agent-reachable call graph
- `@budget` — compile-time argument type validation
- `@sensitive` / `@restricted` — data classification; `@tool` return type enforcement; `println` enforcement
- `Loggable` built-in constraint; unknown constraint names are compile errors

**Tooling**
- `splash tools` — JSON Schema from `@tool` function signatures, filtered to agent-reachable set
- Multi-file modules (`use path` loads sibling `.splash` files; cycle detection; `expose` list)
- `std/ai` — `@tool`, `ai.prompt<T>`, `Result<T, AIError>`
- Nine documented examples covering every shipped feature

---

## Next

These are ordered by dependency and leverage, not by difficulty.

### Phase 4c — Non-blocking approval adapters

`SlackApproval` and `WebhookApproval` that return an error on denial without blocking a goroutine. The `ApprovalAdapter` interface is already defined — these are implementations, not language changes. One denied approval propagates as an error to one request; everything else in flight is unaffected.

This is the piece that makes `@approve` production-usable. `StdinApproval` is for local development.

### `@bypass` — auditable escape hatch

`@bypass(reason: "...", approved_by: "...")` for effect mismatches and classification violations that cannot be avoided. Logged to the provenance chain, surfaced by `splash audit`. Not available for `@redline` — redlines are absolute. Requires a stated justification visible in the call graph.

Without this, developers hit a hard wall and leave the language. With it, the escape is visible, greppable, and reviewable.

### `std/db` — database adapter

The first stdlib adapter. `db.find`, `db.query`, `db.save`, `db.delete` against the `DB` effect vocabulary. Default adapter: in-memory (for `splash dev`). Production adapters: Postgres, SQLite. Unlocks the adapter-first development model described in the whitepaper.

`std/http`, `std/cache`, `std/queue`, `std/storage` follow the same pattern once the adapter interface is established.

### `@budget` runtime enforcement

Inject a budget counter into `ai.prompt` calls. Return `BudgetExceeded` when `max_cost` or `max_calls` is hit. Compile-time argument validation is done; this is the runtime piece. Requires `std/ai` to have a real execution path, which requires the stdlib to exist.

### `std/safety` — provenance chains

Record every agent action: function called, arguments, model reasoning, parent action, effects used, cost, duration. The chain traces from goal to leaf. Structured audit trail for production incidents and compliance requirements. Unstable API — ships for production use and feedback, stabilizes in v0.2.

---

## Later

**`splash migrate`** — compiler-verified database migrations. Up/down required. Type-checked against the schema the application declares.

**`splash deploy`** — capability manifest diffing. A version upgrade that acquires new effects fails the build until a human approves the change. Lockfile-level capability tracking.

**`splash new` / `splash dev --adapters memory`** — project scaffolding and zero-infrastructure development. `splash dev` wires every stdlib service to an in-memory implementation. Change one line in `main()` to swap in real backends.

**LLVM backend** — `Backend` interface is the extension point. The safety model is fully enforced before codegen; the backend receives a verified, effect-annotated AST. Unlocks native targets, WASM, and embedded contexts.

**Effect polymorphism** — functions parameterized over their effects. The current system is monomorphic by design (v0.1 scope). If production use shows it's needed, this is the v0.2 candidate.

---

## Not on the Roadmap

**Runtime effect enforcement.** Effects are a compile-time property. There is no plan to add runtime effect checking — it would add overhead and provide weaker guarantees than the static analysis. The soundness argument in the whitepaper (Section 4) depends on effects being enforced before the binary is produced.

**Gradual typing or `any` escape.** The type system is intentionally strict. Escape hatches for type safety undermine the classification and constraint enforcement that the safety model depends on.

**A VM or custom runtime.** Splash targets Go's runtime. The compiler team works on the frontend; Go handles goroutines, GC, and the toolchain. This is not a limitation — it's a scoping decision that lets the language ship.
