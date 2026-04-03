# Splash Roadmap

What's shipped, what's next, and why things are ordered the way they are.

---

## v0.1 — Launch Ready

The launch surface is the compiler, the examples, and the whitepaper. The goal is not "build your production app on Splash today." It is: here's what Splash is, here's why it matters, here's a working compiler that proves the ideas.

**Language core**
- Lexer, parser, type checker, effect system
- Call graph construction and agent-reachability analysis
- Go codegen, `Backend` interface abstraction
- `splash check`, `splash build`, `splash emit`, `splash tools`

**Safety enforcement (all compile-time)**
- `@redline` — absolute agent lockout with full call path in error
- `@approve` — human gate; `(T, error)` cascade through all transitive callers
- `@containment` — module-level agent access policy (`none`, `read_only`, `approved_only`)
- `@sandbox` — configurable effect allow/deny against the agent-reachable call graph
- `@budget` — compile-time argument type validation
- `@sensitive` / `@restricted` — data classification; `@tool` return type enforcement; `Loggable` constraint
- `Loggable` built-in; unknown constraint names are compile errors

**Tooling**
- `splash tools` — JSON Schema from `@tool` signatures, filtered to agent-reachable set; `--format anthropic|openai` for API-compatible output
- Multi-file modules (`use path` loads sibling `.splash` files; cycle detection; `expose` list)
- `std/ai` — `@tool`, `ai.prompt<T>` (returns `T`; errors propagate to `needs Agent` boundary)
- Nine documented examples covering every safety primitive

---

## v0.2 — Making It Usable

Everything here makes Splash usable for building real applications. None of it is required to understand what Splash is or why it matters — but all of it is required before someone can ship production code with it.

### `catch AIError` — local error handling escape hatch

By default, `ai.prompt<T>` returns `T` and errors propagate invisibly to the `needs Agent` boundary. This is the right default: most callers have no meaningful recovery strategy and the boundary is the correct place to handle AI failures.

`catch AIError` is the opt-in escape for callers that do have a recovery strategy:

```splash
fn ask_health_coach(user_id: Int, question: String) needs Agent, DB.read, AI -> HealthInsight {
  catch AIError {
    return HealthInsight { summary: "Service unavailable", status: "error", recommendations: [], train_today: false }
  }
  return ai.prompt<HealthInsight>(PromptOptions { ... })
}
```

The `catch` block intercepts the error before it propagates. The function's return type stays `HealthInsight` — no `Result<T, E>` leak into the type system. The block must return a value of the declared return type.

This is an opt-in escape hatch, not a replacement for the default. It's useful for retry logic, fallback responses, and graceful degradation below the Agent boundary. Without it, the only recovery option is at the agent entry point itself.

Design constraint: `catch AIError` applies only to the immediately enclosing function body. It cannot catch errors from called functions unless those functions also use `catch`. This prevents silent error swallowing across module boundaries.

### `@bypass` — auditable escape hatch

`@bypass(reason: "...", approved_by: "...")` for effect mismatches and classification violations that can't be avoided. Logged to the provenance chain, surfaced by `splash audit`. Not available for `@redline` — redlines are absolute. The escape is visible in the call graph and cannot silently propagate.

Without this, developers hit a hard wall on legitimate edge cases and leave. With it, the escape is greppable, reviewable, and attributable.

### Phase 4c — Non-blocking approval adapters

`SlackApproval` and `WebhookApproval` that return an error on denial without blocking a goroutine. The `ApprovalAdapter` interface is already defined — these are implementations, not language changes. `StdinApproval` exists for local development; this is the piece that makes `@approve` production-usable.

### `std/db` — database adapter

`db.find`, `db.query`, `db.save`, `db.delete` against the `DB` effect vocabulary. Default adapter: in-memory (for `splash dev`). Production adapters: Postgres, SQLite. Unlocks the adapter-first development model the whitepaper describes.

`std/http`, `std/cache`, `std/queue`, `std/storage` follow the same pattern once the adapter interface is established.

### `splash new` / `splash dev --adapters memory`

Project scaffolding and zero-infrastructure development. `splash dev` wires every stdlib service to an in-memory implementation. Change one line in `main()` to swap in real backends. No Postgres, no Docker, no setup.

### `@budget` runtime enforcement

Inject a budget counter into `ai.prompt` calls. Return `BudgetExceeded` when `max_cost` or `max_calls` is hit. Compile-time argument validation is done; this is the runtime piece. Requires `std/ai` to have a real execution path, which requires the stdlib to exist.

### `std/safety` — provenance chains

Record every agent action: function called, arguments, model reasoning, parent action, effects used, cost, duration. Structured audit trail for production incidents and compliance requirements. Unstable API — ships for production use and feedback, stabilizes in v0.3.

---

## v0.3 and Beyond

**`splash migrate`** — compiler-verified database migrations. Up/down required. Type-checked against the schema the application declares.

**`splash deploy`** — capability manifest diffing. A version upgrade that acquires new effects fails the build until a human approves the change. Lockfile-level capability tracking.

**LLVM backend** — the `Backend` interface is the extension point. The safety model is fully enforced before codegen; the backend receives a verified, effect-annotated AST with no security-relevant decisions remaining. Unlocks native targets, WASM, and embedded contexts.

**Effect polymorphism** — functions parameterized over their effects. The current system is monomorphic by design. If production use shows it's needed, this is the v0.3 candidate.

---

## Not on the Roadmap

**Runtime effect enforcement.** Effects are a compile-time property. There is no plan to add runtime effect checking — it would add overhead and provide weaker guarantees than the static analysis.

**Gradual typing or `any` escape.** The type system is intentionally strict. Escape hatches for type safety undermine the classification and constraint enforcement the safety model depends on.

**A VM or custom runtime.** Splash targets Go's runtime. The compiler team works on the frontend; Go handles goroutines, GC, and the toolchain. This is a scoping decision, not a limitation.
