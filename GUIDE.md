# Splash — Writing Guide

This is the guide for writing Splash fluently. The [SPEC](SPEC.md) is the grammar reference. This document is how to think.

---

## The Mental Model

Splash programs have two concerns: **what they do** and **what they're allowed to do**. Most languages only let you express the first. Splash lets you express both, and the compiler enforces the contract.

Effects are declarations, not implementations. When you write `needs DB.read`, you are not causing a database read — you are declaring that this function requires the database-read capability to be in scope. The compiler enforces this requirement across the entire call graph. The effect tells readers exactly what this code touches.

Safety annotations (`@redline`, `@approve`, `@sandbox`, `@containment`) are also declarations. They describe invariants the compiler enforces over the call graph. You write them once; the compiler checks them forever, on every build.

The call graph is the central data structure. The compiler builds it, identifies every agent entry point, and traces every reachable function. All safety checks operate over this reachable set. If a function is not reachable from an agent root, it is outside the agent's authority. If a dangerous function is reachable, the build fails.

Agent entry points are functions that declare `needs Agent` (runtime entry) or are exposed as `@tool` (external agent interface). These are the trust boundaries where the agent's authority begins.

Splash programs are capability graphs: functions are nodes, effects are capabilities, and the compiler enforces which nodes an agent can reach.

---

## What Splash Does Not Do

- It does not enforce effects at runtime
- It does not verify external Go libraries called via FFI — those form a trust boundary
- It does not prevent you from writing code with consequences — it makes reachability of that code explicit

Calls into external Go code are trusted. Splash guarantees apply to Splash code; the Go compiler handles the rest. The model is: guarantee what can be proved statically, make trust boundaries explicit where it cannot.

---

## Designing a Splash Program

Start with three questions before writing a line of code:

1. **What data does this system handle?** Identify PII and process-local secrets early. Annotate them at the type level with `@sensitive` and `@restricted`. Classification is a property of types — decide it at the type definition, not at each call site.

2. **What effects does each operation require?** Be specific. A function that reads a report needs `DB.read`. A function that reads a report and summarizes it needs `DB.read, AI`. Write the smallest `needs` clause that is correct.

3. **What should an agent never be able to do?** Put `@redline` on those functions immediately. A `@redline` is an absolute guarantee, not a runtime check. If it ends up reachable, the build fails with a full call path.

---

## Functions and Effects

Every function signature is a contract.

```splash
fn fetch_order(order_id: Int) needs DB.read -> Order {
    return db.query(order_id)
}
```

The `needs DB.read` is not documentation — it is a constraint the compiler enforces. Any function that calls `fetch_order` must also declare `needs DB.read`. This propagates up the call tree all the way to the agent entry point.

**Declare the minimum required effects.** A read helper should not declare `DB.write`. If it does, every caller inherits that requirement. Tight `needs` clauses are self-documenting and prevent accidental capability creep.

**Effects propagate up the call graph — they do not get hidden or collapsed.** If `f` calls `g` which calls `h`, and `h` needs `Net`, then `g` needs `Net`, and `f` needs `Net`. The compiler enforces this at every call site. You cannot hide effects by burying them in a helper.

---

## Writing Agent Tools

A `@tool` function is the interface between an AI agent and your system. It has three parts: the implementation, the type signature (which becomes the JSON Schema), and the doc comment (which becomes the description).

```splash
@tool
/// Search orders by customer email. Returns up to limit results.
fn search_orders(email: String, limit: Int) needs DB.read -> List<OrderSummary> {
    return db.query(email, limit)
}
```

`splash tools` derives the full JSON Schema from this. No hand-written schema. No drift between docs and implementation. By default the output uses the OpenAI wire format (`type/function` wrapper, `parameters` key). Pass `--format anthropic` for the Anthropic format (`input_schema` key, no wrapper).

**Rules for `@tool` functions:**

- Return types must not contain `@sensitive` or `@restricted` fields. The build fails if classified data can reach an agent. An agent's context window is not a secure store — if you need to return data that touches PII, return a projection type that contains only public fields.
- Write the doc comment as a description for the agent — be precise about what the function does, what the parameters mean, and what it returns.
- Default parameter values appear in the schema as non-required properties. Use them for optional filters and pagination.
- Optional parameters (`String?`) become optional schema properties.

**Returning projections instead of full types:**

```splash
type User {
    id:    Int
    name:  String
    @sensitive
    email: String
}

// UserSummary contains only public fields — safe to return from @tool
type UserSummary {
    id:   Int
    name: String
}

@tool
/// Look up a user by ID.
fn get_user(id: Int) needs DB.read -> UserSummary {
    let user = db.find(id)
    return UserSummary { id: user.id, name: user.name }
}
```

---

## Protecting Dangerous Operations

Two annotations protect dangerous functions. They are not interchangeable.

**`@redline` — compile-time prohibition.** The build fails if any agent-reachable path reaches this function. No policy, no runtime check, no adapter. The function simply cannot execute in an agent context.

Use `@redline` for operations that should never be automated under any circumstances: dropping tables, rebuilding indices, deleting production data, revoking credentials.

```splash
@redline(reason: "Schema changes require DBA sign-off")
fn drop_table(table_name: String) needs DB.admin { ... }
```

**`@approve` — human gate.** The function can run, but only after the `ApprovalAdapter` approves it. In development, this prompts stdin. In production, swap in a webhook or Slack adapter.

Use `@approve` for high-consequence operations that should be possible to automate with oversight: charging a card, sending a bulk email, publishing a deployment.

```splash
@approve
/// Charge a customer. Requires human approval before executing.
fn charge_card(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }
```

The `@approve` gate is injected into the function body — the caller writes normal code. The approval fires inside `charge_card`, invisible to the call site. This is deliberate: the gate is the function's responsibility, not the caller's.

**`@approve` widens the return type.** The generated Go signature becomes `(Charge, error)`. This error propagates through all transitive callers up to the `needs Agent` boundary — the agent entry point is where errors surface. `fn main()` is emitted as `run() error` with a thin wrapper that handles process exit; the compiler never injects `os.Exit` inside generated function bodies. In production, agent entry points are called from HTTP handlers or queue workers, where the `(T, error)` return integrates naturally.

**Choosing between them:**

| Question | Answer |
|----------|--------|
| Could this operation ever be safe to automate? | `@approve` |
| Should this operation never be automated, period? | `@redline` |
| Could the agent cause irreversible damage by reaching this? | `@redline` |

---

## Sandboxing Agents

`@sandbox` defines the maximum authority of an agent. It pins the effect surface at compile time — every function reachable from the agent must satisfy the allow/deny constraints.

```splash
@sandbox(allow: [DB.read, AI])
@budget(max_cost: 0.05, max_calls: 3)
fn summarize_agent(topic: String) needs Agent, DB.read, AI -> String {
    let data = fetch_data(topic)
    return summarize(data)
}
```

The compiler walks the entire reachable call graph from `summarize_agent`. If any reachable function uses an effect not in `allow`, the build fails — with the name of the offending function and which effect violated the constraint.

**`@sandbox` does not replace `needs`.** The agent still declares `needs DB.read, AI` in its signature. `@sandbox` constrains what the agent and its callees are allowed to use; `needs` declares what the agent actually uses. They must be consistent.

**`@budget` validates argument types at compile time.** `max_cost` must be a numeric literal; `max_calls` must be an integer. This catches `max_calls: 1.5` at build time. Runtime enforcement — injecting a counter into `ai.prompt` calls — is not yet implemented; `@budget` today is a compile-time contract that the runtime will enforce when `std/ai` is complete.

**When to use `@sandbox`:** On any agent function where you want an explicit, auditable statement of its effect surface. It is documentation that the compiler enforces. Even if the current implementation only uses `DB.read`, `@sandbox(allow: [DB.read])` ensures a future change that accidentally adds `Net` will fail the build immediately.

---

## Data Classification

Classification lives on types, not values. Decide at the field level.

```splash
type Customer {
    id:           Int
    display_name: String

    @sensitive
    email:        String    // PII — can't be logged, can't flow to @tool

    @restricted
    payment_token: String?  // process-local — no storage adapter accepts it
}
```

The classification of `Customer` is `@restricted` because the highest-classified field is `@restricted`. The compiler enforces:

- `@tool` functions cannot return `Customer` or any type containing `@sensitive`/`@restricted` fields.
- `println` (and anything requiring `Loggable`) cannot accept `Customer`.

**Design principle:** expose public projections at the boundary, keep classified data internal.

```splash
// The internal type carries full data
type Customer { ... }

// The boundary type is safe to return from @tool
type CustomerProfile {
    id:           Int
    display_name: String
}

@tool
/// Look up a customer profile. Does not include contact info.
fn get_profile(id: Int) needs DB.read -> CustomerProfile { ... }
```

---

## Module Containment

`@containment` is a module-level policy that constrains how agents interact with an entire module. Apply it when a module's functions are inherently consequential — billing, auth, admin operations.

```splash
@containment(agent: "approved_only")
module billing
expose charge_customer, get_balance
```

Three policies:

| Policy | What agents can do |
|--------|--------------------|
| `"none"` | Nothing — agents cannot reach any function in this module |
| `"read_only"` | Only functions with `DB.read` effects |
| `"approved_only"` | Only functions annotated with `@approve` or `@agent_allowed` |

`"approved_only"` is the most useful: it lets agents call billing functions, but every call goes through the approval gate. The module is open to automation but never uncontrolled.

`@agent_allowed` is the escape hatch inside `"approved_only"` containment — for read functions that don't need a gate:

```splash
@containment(agent: "approved_only")
module billing

@agent_allowed  // agents can read balance without approval
fn get_balance(id: Int) needs DB.read -> Int { ... }

@approve        // but charging still requires a gate
fn charge(id: Int, amount: Int) needs Net -> Charge { ... }
```

---

## Multi-File Modules

Use modules when a single file is getting large or when types need to be shared across multiple agents.

```splash
// types.splash
module types
expose Order, OrderSummary, Customer

type Order { ... }
type OrderSummary { id: Int; total_cents: Int }
type Customer { ... }
```

```splash
// agent.splash
module agent
use types

@tool
fn get_order(id: Int) needs DB.read -> OrderSummary { ... }
```

`use types` injects `Order`, `OrderSummary`, and `Customer` directly into `agent`'s namespace — no prefix. The compiler resolves `types.splash` in the same directory, type-checks both files together, and emits them as a single Go package.

**`expose` is the public API.** Only listed names are injected. Internal helpers and intermediate types stay in the declaring module's namespace.

**Cycles are rejected.** If `agent.splash` uses `types.splash` and `types.splash` uses `agent.splash`, the compiler errors with a cycle report. Design modules as a DAG.

---

## Common Mistakes

**Declaring too many effects.** If a function only reads, it only needs `DB.read`. Declaring `DB.read, DB.write` because "it might write someday" forces every caller to acquire write capability now. Declare what you use.

**Missing `needs Agent` on the entry point.** An agent entry function that doesn't declare `needs Agent` won't be treated as an agent root by the call graph. Safety checks won't trace paths from it.

**Returning classified types from `@tool`.** If your `@tool` function returns a type with `@sensitive` fields, the build fails. Create a projection type.

**Treating `@sandbox` as a substitute for `@redline`.** `@sandbox` constrains effects; it does not prevent a function from being reachable. A `@redline` function inside a `@sandbox` agent will still fail the build — `@sandbox` does not exempt it.

**Forgetting `///` doc comments on `@tool` parameters.** The JSON Schema description for each parameter comes from the `///` comment immediately above it in the parameter list:

```splash
@tool
/// Find all orders matching the given filters.
fn find_orders(
    /// The customer ID to filter by.
    customer_id: Int,
    /// Maximum number of results to return.
    limit: Int
) needs DB.read -> List<OrderSummary> { ... }
```

**Using struct literals with lowercase names.** Struct literals are only parsed when the type name starts with an uppercase letter. `result { ... }` will not parse as a struct literal — it will parse as a block.

---

## A Complete Example

A realistic agent module: customer support tools with data safety, approval gating, and effect constraints.

```splash
// support.splash
module support
expose search_orders, refund_order, Customer, OrderSummary

type Customer {
    id:           Int
    display_name: String
    @sensitive
    email:        String
}

// Safe to return from @tool — no classified fields
type OrderSummary {
    id:          Int
    status:      String
    total_cents: Int
}

// Internal — full order with customer data
type Order {
    id:       Int
    customer: Customer
    total:    Int
}

@tool
/// Search orders for a customer. Returns public summaries only.
fn search_orders(
    /// The customer ID to search.
    customer_id: Int,
    /// Maximum results to return.
    limit: Int
) needs DB.read -> List<OrderSummary> {
    return db.query(customer_id, limit)
}

// @approve: refunds are consequential — require human sign-off
// @redline would block this entirely; @approve allows it with oversight
@approve
fn refund_order(order_id: Int, reason: String) needs DB.write, Net -> OrderSummary {
    return OrderSummary { id: order_id, status: "refunded", total_cents: 0 }
}

// Blocks any agent from calling this — too destructive to automate
@redline(reason: "Account deletion requires CX team sign-off")
fn delete_account(customer_id: Int) needs DB.admin { ... }

// Agent entry point. @sandbox pins the effect surface to DB.read only for
// the search path; the refund path adds Net and DB.write (via @approve).
@sandbox(allow: [DB.read, DB.write, Net])
@budget(max_cost: 0.01, max_calls: 10)
fn run_support_agent(customer_id: Int) needs Agent, DB.read -> String {
    let orders = search_orders(customer_id, 5)
    return "found orders"
}
```

What the compiler enforces on this file:

- `search_orders` is a valid `@tool`: `OrderSummary` has no classified fields.
- `delete_account` with `@redline` — if `run_support_agent` ever gains a call path to it, the build fails.
- `@sandbox(allow: [DB.read, DB.write, Net])` — the compiler checks every function reachable from `run_support_agent` uses only those effects.
- `@budget` — `max_cost: 0.01` is numeric and `max_calls: 10` is an integer. Both pass type validation.
