# Splash Examples

Seven examples demonstrating the core language features. All can be checked with `splash check` and built with `splash build`. `@tool` functions emit JSON Schema via `splash tools`. `splash emit` prints the generated Go source.

```
splash check <file.splash>         # parse + type check + safety enforcement
splash build <file.splash>         # codegen → go build → binary
splash emit  <file.splash>         # print generated Go source
splash tools <file.splash>         # emit JSON Schema for @tool functions
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
- `@redline` — permanently blocks a function from agent contexts
- `@approve` — requires human sign-off (emits structured audit log in v0.1)

```splash
@redline(reason: "Schema mutations require human DBA review")
fn drop_report_table() needs DB.admin { ... }

@approve
fn archive_reports(before_date: String) needs DB.write { ... }

// generate_summary inherits both effects it transitively needs
fn generate_summary(id: Int) needs DB.read, AI -> String { ... }
```

**Try breaking it.** Add a function with `needs Agent` that calls `drop_report_table()`. `splash check` will fail with a `@redline` violation.

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

The `Loggable` constraint enforcement is planned for the constraint system (Phase 3). The field annotations and classification propagation are tracked by the type checker today.

---

## 04 · containment

**`containment/containment.splash`** — module-level agent lockout.

`@containment` on a module removes its functions from an agent's reachable call graph. The compiler enforces this at the module boundary.

Three policies:
- `"none"` — agents can't touch anything in this module
- `"read_only"` — agents can call read functions, not write
- `"approved_only"` — every agent call requires `@approve`

```splash
@containment(agent: "approved_only")
module billing

@approve
fn charge_customer(customer_id: Int, amount_cents: Int) needs DB.write, Net { ... }
```

**Try breaking it.** Remove the `@approve` annotation from `charge_customer`. `splash check` will flag the containment violation — `approved_only` requires every function callable from agents to be annotated.

---

## 05 · agent_tools

**`agent_tools/agent_tools.splash`** — `@tool` and agent entry points.

`@tool` turns any function into an AI-callable tool. The doc comment becomes the tool description. The type signature becomes the JSON schema. The function is the implementation — one source of truth for all three.

- `@tool` on read-safe functions
- Optional parameters with default values
- `@redline` on dangerous operations (index rebuilds require a human)
- Agent entry point with scoped effect declarations

```splash
@tool
fn search_catalog(query: String, limit: Int) needs DB.read -> List<SearchResult> { ... }

@redline(reason: "Index rebuilds require human operator sign-off")
fn rebuild_search_index() needs DB.write { ... }

fn run_search_agent(goal: String) needs Agent, DB.read -> String { ... }
```

The agent entry point declares `needs Agent, DB.read`. The compiler verifies that no reachable path from this function requires more than those effects, and that no `@redline` function is reachable.

---

## 06 · ai_prompt

**`ai_prompt/ai_prompt.splash`** — `ai.prompt<T>` and structured AI output.

`use std/ai` brings `ai` into scope. `ai.prompt<T>` uses a Splash type as the structured output contract — the type is the JSON Schema, the compiler validates the return type, no hand-written schema or separate validation step.

- `use std/ai` module import
- `ai.prompt<T>` generic call syntax
- `Result<T, AIError>` return type
- `@tool` with `effects: ["AI"]` in schema output

```splash
use std/ai

type SermonInsight { title: String; summary: String }

/// Analyze a sermon transcript and extract structured insights.
@tool
async fn analyze_sermon(transcript: String) needs AI -> Result<SermonInsight, AIError> {
  return ai.prompt<SermonInsight>(transcript)
}
```

Run `splash tools ai_prompt/ai_prompt.splash` to see the generated JSON Schema with the `effects` field.

---

## 07 · approval

**`approval/approval.splash`** — `@approve` as a function precondition.

`@approve` means the function does not execute until the `ApprovalAdapter` approves the call. The function's return type is unchanged — `charge_card` returns a `Charge`. The compiler injects `splashApprove("charge_card")` as the first statement of the function body. Call sites are untouched.

```splash
@approve
fn charge_card(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }

@approve
fn issue_refund(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }
```

Run `splash emit approval/approval.splash` to see the generated Go. `splashApprove` appears at the top of each `@approve` function body. `validate_amount` and `run_billing_agent` are untouched — no injection at call sites. The `ApprovalAdapter` interface and `SetApprovalAdapter` swap function are in the preamble.

The default adapter (`StdinApproval`) loops on a terminal prompt until the operator types `y`. Swap it at initialization for production:

```go
// In production main() — Splash side uses SetApprovalAdapter via generated Go
SetApprovalAdapter(&WebhookApproval{URL: secrets.ApprovalWebhook})
```

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

# Build to a binary (main function required)
./splash build examples/hello/hello.splash -o hello
./hello

# See the generated Go source
./splash emit examples/approval/approval.splash
./splash emit examples/containment/containment.splash

# Emit JSON Schema for @tool functions
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
| `@redline` enforcement | ✅ Complete |
| `@containment` enforcement | ✅ Complete |
| `@approve` runtime (`ApprovalAdapter`, `StdinApproval`) | ✅ Complete (Phase 4a) |
| Go codegen | ✅ Complete |
| `splash check` / `splash build` / `splash emit` | ✅ Complete |
| `@tool` JSON Schema (`splash tools`) | ✅ Complete |
| `@tool` safety filtering (agent-reachable only) | ✅ Complete |
| `use std/ai` + `ai.prompt<T>` type checking | ✅ Complete |
| Effects field in tool schema output | ✅ Complete |
| Member access type resolution | ✅ Complete |
| `@approve` denial / `Result<T, ApprovalError>` | Planned — Phase 4b |
| `@sandbox` / `@budget` enforcement | Planned — Phase 4 |
| `std/db` stdlib | Planned — Phase 4 |
| `@sensitive` / `Loggable` constraint enforcement | Planned — Phase 4 |
| Multi-file modules | Planned — Phase 4 |
