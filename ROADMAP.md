# Splash Roadmap

What's shipped, what's next, and why things are ordered the way they are.

---

## v0.1 — Implementation baseline

The [implementation status](README.md#implementation-status) describes current coverage. The compiler, examples and April whitepaper show the original design and the subset available today. The [current engineering plan](docs/design/engineering-plan.md) runs compiler soundness and conventional authoring research in parallel.

**Language core**
- Lexer, parser, type checker and declared effect metadata
- Call graph construction and agent-reachability analysis for modeled calls
- Go codegen, `Backend` interface abstraction
- `splash check`, `splash build`, `splash emit`, `splash tools`

**Supported safety checks and lowering**
- `redline fn` — compile-time rejection of modeled agent paths, with a witness
- `approve fn` — generated approval adapter dispatch and error propagation for supported calls
- `@containment` — entry-module policy evaluated over the modeled graph
- `@sandbox` — configurable allow/deny checks over declared effects on modeled reachable functions
- `@budget` — compile-time argument type validation
- `@sensitive` / `@restricted` — checks for selected tool result and logging type shapes
- `Loggable` built-in; unknown constraint names are compile errors

**Tooling**
- `splash tools` — argument schemas from `tool fn` declarations after current safety checks, with `--format anthropic|openai`
- Multi-file modules (`use path` loads sibling `.splash` files; cycle detection; `expose` list)
- `std/ai` frontend bindings and prompt type handling
- Native and illustrative examples with different levels of support

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

**Splash IR and VM** — an experimental stack VM will run a typed subset from validated IR and bytecode. Go transpilation remains the transitional default. Native code generation and cross-platform integration are later work. LLVM and other lowering tools remain possible future comparisons. [Execution design](docs/design/splash-ir-and-vm.md)

**Effect polymorphism** — functions parameterized over their effects. The current system is monomorphic by design. If production use shows it's needed, this is the v0.3 candidate.

---

## Not on the Roadmap

**Replacing compile-time effect checks with runtime checks.** The later capability-aware runtime can restrict concrete access for an invocation while the compiler establishes the source-level effect contract.

**Gradual typing or `any` escape.** The type system is intentionally strict. Escape hatches for type safety undermine the classification and constraint enforcement the safety model depends on.
