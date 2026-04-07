# Splash Examples

These examples focus on Splash's actual design center: control-plane software whose authority must be statically understood before it runs. They are intentionally centered on agent entrypoints, tool surfaces, approval gates, containment, and data-flow boundaries rather than generic application code.

All can be checked with `splash check` and built with `splash build`. `tool fn` declarations emit JSON Schema via `splash tools`. `splash emit` prints the generated Go source.

```
splash check <file.splash>         # parse + type check + safety enforcement
splash build <file.splash>         # codegen → go build → binary
splash emit  <file.splash>         # print generated Go source
splash tools <file.splash>         # emit JSON Schema for tool declarations
splash graph <file.splash>         # print agent roots + direct call graph
splash effects <file.splash>       # print each function's effect surface
splash approvals <file.splash>     # print approval-gated functions + callers
```

---

## 01 · hello

**`hello/hello.splash`** — the simplest Splash program.

- Module declarations and named record types
- Functions with explicit return types
- Optional types (`String?`) and null coalescing (`??`)
- Struct literal syntax

```splash
type Person {
    name:     String
    nickname: String?
}

fn greet(p: Person) -> String {
    let display_name = p.nickname ?? p.name
    return "Hello, " + display_name
}
```

---

## 02 · effects

**`effects/effects.splash`** — the effect system.

Every function declares the capabilities it requires in its signature. The compiler verifies this at every call site. A function with `needs DB.read` cannot call a function that `needs DB.write`. Violations fail the build.

- `needs` clause on function declarations
- Effect propagation through the call graph
- `redline fn` — permanently blocks a function from agent contexts
- `approve fn` — requires human sign-off (emits structured audit log in v0.1)

```splash
@reason "Schema mutations require human DBA review"
redline fn drop_report_table() needs DB.admin { ... }

approve fn archive_reports(before_date: String) needs DB.write { ... }

// generate_summary inherits both effects it transitively needs
fn generate_summary(id: Int) needs DB.read, AI -> String { ... }
```

**Try breaking it.** Add a function with `needs Agent` that calls `drop_report_table()`. `splash check` will fail with a `redline fn` violation.

---

## 03 · data_safety

**`data_safety/data_safety.splash`** — data classification.

Classification is a property of types, not values. The four levels: `public < @internal < @sensitive < @restricted`. The compiler propagates classification through the type system and blocks operations that violate it.

- `@sensitive` fields: the containing type cannot satisfy `Loggable` — log calls fail at compile time
- `@restricted` fields: process-local, no storage adapter accepts them
- Optional sensitive fields (`String?`)

```splash
type User {
    id:           Int
    display_name: String
    @sensitive
    email:        String    // PII — cannot be logged
    @restricted
    ssn:          String?   // never leaves the process
}
```

Two enforcement checks are active: `tool fn` functions cannot return types with `@sensitive` or `@restricted` fields (safety checker), and `println` rejects arguments of `@sensitive`-classified types (type checker). Run `splash check examples/data_safety/data_safety.splash` — the file checks clean because it only uses public fields in its functions. Add `tool fn expose_user(id: Int) -> User { ... }` to see the first enforcement in action.

---

## 04 · containment

**`containment/containment.splash`** — module-level agent lockout.

`@containment` on a module removes its functions from an agent's reachable call graph. The compiler enforces this at the module boundary.

Three policies:
- `"none"` — agents can't touch anything in this module
- `"read_only"` — agents can call read functions, not write
- `"approved_only"` — every agent call requires `approve fn`

```splash
@containment(agent: "approved_only")
module billing

approve fn charge_customer(customer_id: Int, amount_cents: Int) needs DB.write, Net { ... }
```

**Try breaking it.** Remove `approve` from `charge_customer`. `splash check` will flag the containment violation — `approved_only` requires every function callable from agents to be approval-gated.

---

## 05 · agent_tools

**`agent_tools/agent_tools.splash`** — `tool fn` and `agent fn`.

`tool fn` turns any function into an AI-callable tool. The doc comment becomes the tool description. The type signature becomes the JSON schema. The function is the implementation — one source of truth for all three.

- `tool fn` on read-safe functions
- Optional parameters with default values
- `redline fn` on dangerous operations (index rebuilds require a human)
- `agent fn` entry point with scoped effect declarations

```splash
tool fn search_catalog(query: String, limit: Int) needs DB.read -> List<SearchResult> { ... }

@reason "Index rebuilds require human operator sign-off"
redline fn rebuild_search_index() needs DB.write { ... }

agent fn run_search_agent(goal: String) needs DB.read -> String { ... }
```

The agent entry point carries the structural `Agent` effect through `agent fn`. The compiler verifies that no reachable path from this function requires more than the declared effects, and that no `redline fn` function is reachable.

---

## 06 · ai_prompt

**`ai_prompt/ai_prompt.splash`** — `ai.prompt<T>` and structured AI output.

`use std/ai` brings `ai` into scope. `ai.prompt<T>` uses a Splash type as the structured output contract — the type is the JSON Schema, the compiler validates the return type, no hand-written schema or separate validation step.

- `use std/ai` module import
- `ai.prompt<T>` generic call syntax
- `Result<T, AIError>` return type
- `tool fn` with `effects: ["AI"]` in schema output

```splash
use std/ai

type SermonInsight { title: String; summary: String }

/// Analyze a sermon transcript and extract structured insights.
tool fn
async fn analyze_sermon(transcript: String) needs AI -> Result<SermonInsight, AIError> {
  return ai.prompt<SermonInsight>(transcript)
}
```

Run `splash tools ai_prompt/ai_prompt.splash` to see the generated JSON Schema with the `effects` field.

---

## 07 · approval

**`approval/approval.splash`** — `approve fn` as a function precondition.

`approve fn` means the function does not execute until the `ApprovalAdapter` approves the call. The function's return type is unchanged at the Splash level — `charge_card` returns a `Charge`. The compiler injects `splashApprove("charge_card")` as the first statement of the function body. Call sites are untouched.

```splash
approve fn charge_card(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }

approve fn issue_refund(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }
```

Run `splash emit approval/approval.splash` to see the generated Go. `splashApprove` appears at the top of each `approve fn` body. `validate_amount` and `run_billing_agent` are untouched — no injection at call sites. The `ApprovalAdapter` interface and `SetApprovalAdapter` swap function are in the preamble.

The default adapter (`StdinApproval`) loops on a terminal prompt until the operator types `y`. Swap it at initialization for production:

```go
// In production main() — Splash side uses SetApprovalAdapter via generated Go
SetApprovalAdapter(&WebhookApproval{URL: secrets.ApprovalWebhook})
```

---

## 08 · multi_file

**`multi_file/types.splash`** and **`multi_file/agent.splash`** — multi-file modules with namespaced access.

`use types` in `agent.splash` loads `types.splash` from the same directory. `SearchResult` and `SearchQuery` are defined in `types.splash`; `agent.splash` can reference them through the `types.` namespace. The compiler resolves the import, type-checks both files, and emits a single Go package.

```splash
// types.splash
module types

type SearchResult { title: String; url: String; score: Int }
type SearchQuery  { text: String; limit: Int }

// agent.splash
module agent
use types

tool fn search(query: types.SearchQuery) needs DB.read -> types.SearchResult { ... }
```

Run `splash emit examples/multi_file/agent.splash` to see both modules merged into one Go package.

---

## 09 · sandbox

**`sandbox/sandbox.splash`** — `@sandbox` effect constraints and `@budget` resource limits.

`@sandbox` pins the effect surface of an agent entry point at compile time. The compiler walks every transitively reachable function and verifies that each one's declared effects satisfy the allow/deny lists. Violations fail `splash check` — not a runtime audit.

`@budget` declares advisory resource limits. Argument types are validated at compile time (`max_cost` must be numeric, `max_calls` must be an integer).

```splash
@sandbox(allow: [DB.read, AI])
@budget(max_cost: 0.10, max_calls: 5)
fn safe_search_agent(term: String) needs Agent, DB.read, AI -> String {
    let result = query_catalog(term)   // needs DB.read — allowed
    return summarize(result)           // needs AI — allowed
}
```

**Try breaking it.** Add a function with `needs Net` to the reachable call graph of `safe_search_agent`. `splash check` will fail: `reachable function "..." uses effect Net not in allow list`.

---

## Running the Examples

With the `splash` binary built from the repo root:

```bash
# Build the compiler
go build ./cmd/splash/...

# Check any example
./splash check examples/hello/hello.splash
./splash check examples/effects/effects.splash
./splash check examples/containment/containment.splash
./splash check examples/approval/approval.splash
./splash check examples/ai_prompt/ai_prompt.splash
./splash check examples/sandbox/sandbox.splash

# Build to a binary (main function required)
./splash build examples/hello/hello.splash -o hello
./hello

# See the generated Go source
./splash emit examples/approval/approval.splash
./splash emit examples/containment/containment.splash

# Emit JSON Schema for tool declarations
./splash tools examples/agent_tools/agent_tools.splash
./splash tools examples/ai_prompt/ai_prompt.splash
```

## Compiler Status

| Feature | Status |
|---|---|
| Lexer + parser | ✅ Complete |
| Type checker | ✅ Complete |
| Effect system (`needs`) | ✅ Complete |
| Call graph analysis | ✅ Complete |
| `redline fn` enforcement | ✅ Complete |
| `@containment` enforcement | ✅ Complete |
| `approve fn` runtime (`ApprovalAdapter`, `StdinApproval`) | ✅ Complete (Phase 4a) |
| Go codegen | ✅ Complete |
| `splash check` / `splash build` / `splash emit` | ✅ Complete |
| `tool fn` JSON Schema (`splash tools`) | ✅ Complete |
| `tool fn` safety filtering (agent-reachable only) | ✅ Complete |
| `use std/ai` + `ai.prompt<T>` type checking | ✅ Complete |
| Effects field in tool schema output | ✅ Complete |
| Member access type resolution | ✅ Complete |
| `approve fn` denial / error cascade (`(T, error)` Go signatures) | ✅ Complete (Phase 4b) |
| `@sandbox` effect allow/deny enforcement (compile-time) | ✅ Complete |
| `@budget` argument type validation (compile-time) | ✅ Complete |
| `std/db` stdlib | Planned — Phase 4 |
| `@sensitive` / `Loggable` enforcement (`tool fn` return type + `println`) | ✅ Complete |
| Multi-file modules (`use path` loads sibling `.splash` files) | ✅ Complete |
