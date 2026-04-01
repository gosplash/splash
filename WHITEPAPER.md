# Splash

**Compiler-Enforced Safety for AI Agent Systems**

*White Paper — v0.1*

---

## Reader's Guide

This document has four audiences. Each reads a different path through it.

**CISOs, VPs of Engineering, compliance teams:** Read Section 1 (The Problem) and Section 5 (Supply Chain and Organizational Safety). Skim Section 4 for the stability table. Skip the code.

**Developers:** Read Sections 1, 2, and 3. Look at the code samples first. The comparison table in Section 3 is the fastest orientation.

**Language designers and potential contributors:** Sections 1, 2, 4, and 6. Section 4 has the formal treatment of the effect system. Section 6 covers the compiler architecture.

**AI lab researchers and partners:** Read Section 1, then Section 4 (AI Safety Architecture) and Section 7 (Ecosystem and Integration). The provenance chain and output contract specifications are in Section 4.

---

## Section 1: The Problem

Deployment is where AI safety breaks down.

Labs spend billions on RLHF, constitutional AI, and red-teaming to make models behave well in controlled settings. Then a model gets deployed as an agent — connected to a database, authorized to make API calls, capable of writing files — and the safety guarantees evaporate. The model didn't forget its training. The system has no guarantees at all.

Consider what a production AI agent deployment looks like today. A function gets registered as a tool. The LLM calls it with arguments it generated. The function runs. If something goes wrong — wrong data accessed, PII logged, a transaction sent twice, a record deleted — the system has no structural way to prevent it. Developers write careful prompts. They add checks in function bodies. They hope.

Three failure modes drive most production incidents:

**PII in logs.** A developer logs a user object for debugging. The object has an email field. Somewhere in the call chain, a language model received that log as context. The email is now in a training corpus or an API provider's logs. No one made a bad decision. The type system had no way to prevent it.

**Unconstrained agent capabilities.** An agent gets a set of tools and a goal. The goal requires reading from the database. Nothing prevents the agent from calling a write function if it decides that's useful. Nothing prevents it from calling a function that deletes records. The sandbox is the developer's vigilance, not the runtime.

**Silent privilege escalation in dependencies.** A package that previously made only network calls adds database access in a patch release. The lockfile updates. CI passes. The new behavior ships to production. No one approved it. No one saw it.

Splash addresses all three at the compiler level. These are type-system properties enforced before the binary is produced — not runtime policies that can be misconfigured, not linter rules that can be suppressed.

---

## Section 2: The Approach

Splash is a compiled, statically-typed programming language for backend systems and AI agent infrastructure. Its syntax is legible to both human reviewers and LLMs: explicit contracts, no magic, no implicit coercion.

The core premise is that most of what makes AI agent systems dangerous is structurally expressible. The wrong function is callable from the wrong context. Sensitive data can flow into a log. A dependency can acquire capabilities it wasn't granted. Structurally expressible problems have structural solutions. A compiler can enforce the constraint. Safety moves from "the developer remembered" to "the build failed."

Four design decisions carry most of the weight:

**Effects as function signatures.** Every function declares the capabilities it requires: `needs DB, Net, Clock`. The compiler verifies that every call site provides the declared effects. A function declared with `needs DB.read` cannot call a function that `needs DB.write`. An agent cannot invoke a function that `needs FS` unless the agent was explicitly granted filesystem access. Violations fail to compile.

**Data classification in the type system.** Fields annotated `@sensitive` or `@restricted` affect what constraints their containing types can satisfy. A `@sensitive Email` field prevents the type from satisfying the `Loggable` constraint. The logging call fails at build time, not in a runtime scan.

**Agent context as a typed boundary.** Agent context is not inferred — it's explicit. Functions marked `needs Agent`, and call paths reachable from `agent.execute()`, form a distinct execution context the compiler tracks. Safety annotations check by tracing call graphs from these entry points. The compiler knows which functions an agent can reach, and rejects the ones it shouldn't.

**Adapter-first stdlib.** Every service in the standard library — databases, caches, queues, storage, AI providers — is defined as a constraint interface, not a concrete implementation. Application code depends on the interface. Backends wire in at startup. This is what makes `splash dev --adapters memory` possible: zero infrastructure to start development, a one-line change in `main()` to swap in real backends, no application code changes.

---

## Section 3: The Language

*This section is for developers. It's also the fastest path for any audience to see what Splash looks like.*

### Starting a Project

```splash
$ splash new thatsermon-api
$ splash dev --adapters memory
  listening on :8080
  hot reload enabled
```

No Postgres. No Redis. No Docker. The `--adapters memory` flag wires every stdlib service to an in-memory implementation. Write handlers, test behavior, iterate. When ready for real infrastructure, change one line in `main()`. The application code doesn't change.

### What Code Looks Like

```splash
// A function that reads from the database and calls an AI model.
// Its capabilities are declared in the signature, not hidden in the body.

async fn analyze(sermon_id: SermonId) -> Result<SermonInsight, AppError>
  needs DB, AI
{
  let sermon = try db.find(Sermon, sermon_id)
  return ai.prompt<SermonInsight>({
    model:   "claude-opus-4-6",
    system:  "You are a biblical scholar.",
    input:   sermon.transcript,
    budget:  Cost.usd(0.05),
    timeout: 30.seconds,
  })
}
```

`ai.prompt<SermonInsight>` uses the `SermonInsight` type as the structured output schema and as the validation contract. The type is the JSON Schema. When the model returns, Splash validates the response against the type before returning to the caller. No hand-written schema. No separate validation step. One source of truth for humans, agents, and the compiler.

### Data That Can't Leak

```splash
type User {
  id:      UserId
  display: String
  @sensitive
  email:   Email      // PII
  @restricted
  ssn:     SSN        // never leaves the process
}

// Fails to compile:
fn bad(u: User) { log.info("User: {u.email}") }
// Error: cannot interpolate @sensitive value into log statement

// Compiles:
fn good(u: User) { log.info("User: {u.email.masked}") }   // z***@g***.com
```

The `@sensitive` annotation isn't a warning. The `Email` type no longer satisfies `Loggable`. The interpolation is a compile error. The only paths to the raw value require explicit declassification with an audit annotation.

### Making a Function AI-Callable

```splash
/// Search sermons by theme, speaker, or scripture reference.
@tool
fn search_sermons(
  /// The search query — theme, topic, or verse reference
  query:   String,
  limit:   Int     = 10,
  speaker: String?,
  book:    String?,
) needs DB -> List<SermonResult> { ... }
```

`@tool` turns any function into an AI-callable tool. The doc comment becomes the tool description. The type signature becomes the JSON Schema. The same source is the implementation, the schema, and the documentation.

### Sandboxed Agents

```splash
@sandbox(allow: [DB.read, AI], deny: [Net, FS, DB.write])
@budget(max_cost: Cost.usd(0.50), max_calls: 20)
async fn answer_question(q: String) needs Agent -> Result<Answer, AgentError> {
  return agent.execute(q, tools: [search_sermons, lookup_verse])
}
```

The agent gets exactly the capabilities declared, nothing more. `DB.write` is denied. `Net` is denied. The budget limits cost and call count. These aren't configuration values — they're compile-time constraints on what the agent runtime will permit.

### Migrations as Compiled Code

Most projects treat database migrations as a separate concern: numbered SQL files, a separate tool, and a handshake between the schema and the application code that nobody enforces. Splash makes migrations first-class:

```splash
migration "003_add_user_location" {
  depends_on: "002_add_user_email_index"

  up {
    alter_table(User) {
      add_column(location: GeoPoint?, default: none)
    }
  }

  down {
    alter_table(User) { drop_column(.location) }
  }
}
```

If `User.location` exists in the type definition but no migration creates the column, the build fails. If the migration adds a `String` column but the type declares `@sensitive Email`, the build fails — the classification mismatch is caught before deployment. Every migration requires a `down` block; marking one `irreversible` is allowed, but silence is not.

`splash migrate gen` scaffolds a new migration from the diff between current types and the applied schema. `splash migrate check` validates the full migration graph against the current type definitions without running anything. For enterprise teams with strict change-control processes, that's a pre-deployment gate that actually catches problems.

### The Comparison

| Concern | Common Approach | Splash |
|---|---|---|
| PII in logs | Runtime scanning | Compile-time data classification |
| AI tool calling | Manual OpenAPI specs | `@tool` on any function |
| Agent sandboxing | Docker + configuration | Effect-system capability bounds |
| Structured outputs | Hand-written JSON Schema | Type signature is the schema |
| Backend portability | Rewrite application code | Adapter swap in `main()` |
| Context / deadlines | `ctx` as first parameter | Implicit availability, explicit access |
| Database migrations | Separate tool, no type checking | Compiler-verified, up/down required |
| Dep privilege escalation | Invisible in lockfile | Lockfile diff + human approval |
| Agent danger zones | Prompt engineering | `@redline` — call graph enforcement |
| Human-in-the-loop | Manual integration per vendor | `@approve` keyword |

---

## Section 4: AI Safety Architecture

*This section has two parts. The compiler-enforced guarantees are for everyone. The formal properties are for language designers and AI lab researchers assessing technical soundness.*

### Compiler-Enforced Guarantees

Three annotations form the compiler-enforced AI safety layer. They are not library calls you can forget to make. They are part of the language.

**`@redline` — absolute prohibitions.**

```splash
@redline(reason: "Schema mutations require human DBA review")
fn drop_table(table: String) needs DB.admin { ... }

@redline(reason: "User deletion has legal and compliance implications")
fn hard_delete_user(id: UserId) needs DB.write { ... }
```

`@redline` marks a function as unreachable from any Agent context. The compiler traces every call path from `agent.execute()` and `needs Agent` functions. If any path reaches a `@redline` function, the build fails. No policy file can relax this. No flag can suppress it.

```splash
// Fails to compile:
@sandbox(allow: [DB.write])
async fn agent_cleanup(goal: String) needs Agent {
  return agent.execute(goal, tools: [hard_delete_user])
  // Error: @redline fn "hard_delete_user" is not callable from Agent context.
  //        This restriction cannot be overridden.
}
```

**`@containment` — module-level agent lockout.**

```splash
@containment(agent: .none)
module billing

fn update_subscription(user: UserId, plan: Plan) needs DB, Net { ... }
fn process_refund(charge: ChargeId, amount: Money) needs DB, Net { ... }
```

`@containment` on a module removes every function in that module from the agent's reachable call graph. The compiler enforces this at the module level. Nothing in the `billing` module is visible to agents, regardless of what tools they're given. Three levels are available:

```splash
@containment(agent: .none)          // agents can't touch anything
@containment(agent: .read_only)     // agents can call read functions, not write
@containment(agent: .approved_only) // every call from agent context requires @approve
```

**`@approve` — human-in-the-loop as a language keyword.**

```splash
@approve(
  prompt:     "Charge {amount} to {method.last4}?",
  timeout:    5.minutes,
  on_timeout: .reject,
)
async fn charge_card(amount: Money, method: PaymentMethod) -> Result<Charge, PaymentError>
  needs Net
{ ... }
```

When an agent reaches a function marked `@approve`, the runtime suspends execution and presents the approval request to a human operator. The agent's reasoning from the active trace context is included automatically. The function doesn't run until a human responds, or the timeout elapses and the `on_timeout` policy fires.

The compiler enforces `@approve` requirements when policy demands it:

```splash
policy "owyhee-holdings" {
  agent_policy {
    require_approve: [DB.write + Net]   // any fn with both effects needs @approve
    require_approve_on_sensitive: true   // any fn touching @sensitive data needs @approve
  }
}
```

Any function satisfying those conditions but lacking `@approve` fails to compile when reachable from Agent context. The developer must add `@approve` or add `@agent_allowed(reason: "...")` with a stated justification. The compiler requires one or the other. Silence is a build failure.

### Stability Boundary

| Mechanism | Status | Enforced By |
|---|---|---|
| `@redline` | Stable v0.1 | Compiler — call graph analysis |
| `@containment` | Stable v0.1 | Compiler — module boundary |
| `@approve` | Stable v0.1 | Compiler + runtime |
| `@agent_allowed` | Stable v0.1 | Compiler — requires stated reason |
| Capability decay | **Unstable** | Runtime (`std/safety`) |
| Provenance chains | **Unstable** | Runtime (`std/safety`) |
| Drift detection | **Unstable** | Runtime (`std/safety`) |
| Output contracts | **Unstable** | Runtime (`std/safety`) |

The compiler-enforced primitives will not change without a major version. The `std/safety` runtime APIs ship for production use and feedback. The mechanisms that prove out become stable in v0.2.

### Formal Properties

*This subsection is for language designers and researchers evaluating the effect system's soundness.*

#### Effect System

Every Splash function has an associated effect set drawn from a fixed vocabulary:

```
E ::= DB | DB.read | DB.write | DB.admin
    | Net | Cache | AI | FS | Exec | Clock
    | Secrets | Secrets.read | Secrets.write
    | Agent | Store | Queue | Metric
```

Effect declarations are monotone: declared, not inferred, and a call site must provide a superset of the callee's required effects. For a function `f` with declared effects `E_f` called from a context with available effects `E_ctx`:

```
E_f ⊆ E_ctx
```

The compiler checks this statically at every call site. There is no dynamic effect dispatch.

Effects form a partial order via subset inclusion. Refinements (`DB.read`, `DB.write`, `DB.admin`) are subtypes of their parent (`DB`): granting `DB` grants all sub-effects. Granting `DB.read` grants only read operations.

**Effect polymorphism.** v0.1 does not support effect polymorphism. Effects are monomorphic and declared. A generic function cannot be parameterized over its effects — it must declare a fixed effect set, and callers must satisfy it. This is a deliberate scoping decision: effect polymorphism (as in Koka or Frank) adds significant complexity to the type system and inference engine. The monomorphic system covers the practical cases — a function that sometimes needs `DB` and sometimes doesn't is two functions. If production experience shows that effect polymorphism is needed, it's on the v0.2 roadmap. Stating "we don't support X" clearly is more useful than leaving researchers to discover it.

#### Agent Context and Call Graph Analysis

Agent context is a distinguished capability in the effect system. Functions with `needs Agent`, and call paths reachable from `agent.execute()`, are agent-context entry points. The compiler builds the full call graph and propagates agent-reachability transitively.

For `@redline` enforcement: a function `f` marked `@redline` must not appear in any call path from any agent-context entry point. If such a path exists, the build fails.

This analysis runs over the same call graph the compiler builds for `needs` propagation — every call site must satisfy the callee's effect requirements, so the full graph is already available. `@redline` and `@approve` checks are additional predicates evaluated over that graph in the same pass.

The analysis is whole-program. Compilation units cannot be checked in isolation for `@redline` and `@approve` correctness. This constraint is intentional: it prevents effect laundering through separately-compiled modules.

#### Data Classification

Classification is a property of types, not values. The four levels form a lattice:

```
public < @internal < @sensitive < @restricted
```

Field annotations declare the minimum classification of that field's type in context. The classification of a composite type is the maximum classification of its fields.

Constraint satisfaction is classification-aware. The `Loggable` constraint has a precondition: the implementing type's classification must be `public` or `@internal`. `@sensitive` and `@restricted` types cannot implement `Loggable`. This fails at the constraint satisfaction site, before any log call is written. Every subsequent log interpolation of that type fails to compile as a consequence.

Storage operations enforce classification at the boundary. `@restricted` values are process-local by definition: no storage adapter accepts them. `@sensitive` values require the storage adapter to declare encryption support; adapters without it are rejected at the call site.

#### Compilation Model

Splash compiles to Go. The frontend handles parsing, type inference, effect checking, classification checking, call graph analysis, and code generation. The output is idiomatic Go source. `go build` produces the final binary.

Splash inherits Go's runtime: goroutines, garbage collection, fast compilation, a mature toolchain. The safety properties live in the Splash frontend. Go sees generated code that has already been verified; it does not need to understand Splash's effect system.

Structured concurrency maps to Go's goroutine model. `group { async f(); async g() }` compiles to a goroutine group with cancellation semantics using Go's `context` package and `errgroup`. The structured guarantee — all children cancelled and awaited before the parent continues — is upheld by the generated code.

Context propagation is implemented via goroutine-local storage. Context is implicitly available in every Splash function; explicit reads (`ctx.remaining`, `ctx.check()`, `ctx.get(AuthUser)`) compile to reads from a goroutine-local value. Developers don't thread context through signatures; the compiler inserts the plumbing.

---

## Section 5: Supply Chain and Organizational Safety

*This section is for security teams, compliance officers, and anyone responsible for what runs in production.*

### Effect-Permissioned Dependencies

Every Splash package declares the maximum effects it requires. Your project grants those effects explicitly. The lockfile records the grants. A version upgrade that acquires new effects fails the build until a human approves the change.

```toml
# splash.lock

[[package]]
name    = "pkg/stripe"
version = "2.1.0"
hash    = "sha256:a1b2c3d4..."
effects = ["Net"]      # locked — v2.2.0 adding DB is a build failure
```

When `pkg/stripe` 2.2.0 adds `DB` access, the update surfaces as a prompt, not a diff:

```shell
$ splash update pkg/stripe
  ⚠ pkg/stripe@2.2.0 requests NEW effects: [Net, DB]
  ⚠ Previously granted: [Net]
  ? Grant DB to pkg/stripe? [y/n]
```

The lockfile diff is the audit trail. The grant is the record.

### Four Attack Vectors, Addressed at the Language Level

**Silent privilege escalation.** Any change to a package's effect set appears as a build failure on update. The lockfile captures what was granted, and the compiler enforces it.

**Transitive dependency compromise.** If `pkg/retry` (a transitive dependency of `pkg/stripe`) starts making network calls, `pkg/stripe`'s declared max effects must change. That change propagates up to your lockfile and requires re-approval. Transitive deps cannot exceed the effects granted to their direct parent.

**Typosquatting.** Unverified packages with no download history require `--trust-unverified` to install. AI agents cannot pass that flag — it's not in the agent CLI interface by design.

**Build-time code execution.** Splash has no build hooks. No `postinstall`. No `build.rs`. Compilation is pure. A dependency cannot execute arbitrary code during `splash build`.

### Organizational Policy

```splash
// splash.policy — enforced at build time

policy "owyhee-holdings" {
  deny_effects:           [FS]
  net_proxy:              "https://egress.internal.owyhee.dev"
  require_publisher:      verified
  max_dep_effects:        2
  block_vulnerabilities:  critical, high
  ci_lockfile_review:     true

  agent_policy {
    can_add_deps:         true
    can_grant_effects:    false    // human must approve
    can_trust_unverified: false
    max_budget_per_call:  Cost.usd(1.00)
  }
}
```

Policy files enforce guardrails across every project in the organization. A project can inherit and restrict further, never relax. AI agents can add dependencies but cannot grant effects. Effect grants require a human. This is a build constraint, not documentation.

### SOC 2 Alignment

Four SOC 2 control families map directly to Splash language properties:

**CC6 (Logical and Physical Access Controls).** Effect declarations and `@sandbox` constraints implement least-privilege access at the language level. Every agent's capabilities are declared, locked, and auditable from source.

**CC7 (System Operations).** `@approve` gates create mandatory approval workflows for high-risk operations. The approval prompt, timeout, and timeout policy are declared in source and enforced by the runtime.

**CC9 (Risk Mitigation).** `std/resilience` (CircuitBreaker, RetryPolicy, Bulkhead) provides documented, testable failure handling. `std/safety` provenance chains create the audit trail CC9 requires for automated decisions.

**A1 (Availability).** `@deadline` propagation and `ctx.remaining` checks prevent unbounded execution. Structured concurrency guarantees resource cleanup. `@deploy` canary configuration with automatic rollback on error rate thresholds is declared in source.

---

## Section 6: Implementation Roadmap

Splash compiles to Go. `splash build` is a frontend that parses, type-checks, and verifies Splash source, emits Go, and calls `go build`. The output is a single statically-linked binary. Splash inherits Go's runtime — goroutines, GC, fast compilation — while the frontend enforces Splash's safety properties before Go sees the code.

The Go target is a deliberate choice. Building a production runtime from scratch is a multi-year project before a developer can ship their first API. Go's runtime is proven and well-understood. The Splash compiler team focuses on the frontend — type system, effect system, classification analysis, safety enforcement — and delegates runtime concerns to Go.

### Phase 1: Parser and Type Checker

- Lexer and parser for `.splash` syntax
- Type inference: generics with flat constraint bounds, optionals, Result types
- Module system with `expose` declarations
- Constraint satisfaction checking, including classification-aware constraint preconditions
- Error reporting with source locations

Deliverable: a type-checker that accepts or rejects Splash programs and produces typed ASTs.

### Phase 2: Effect System and Go Codegen

- Effect declaration and propagation (`needs` checking at every call site)
- Call graph construction and agent-context reachability analysis
- `@redline` enforcement via call graph tracing
- `@containment` module boundary enforcement
- `@approve` gate insertion: the compiler rewrites agent-reachable call sites to inject runtime suspension points
- Data classification checks (`@sensitive`, `@restricted` constraint satisfaction)
- Go code generation from typed, verified ASTs
- `splash build` and `splash dev` CLI

Deliverable: `splash build` compiles a Splash program to a Go binary. Effect system and safety properties are enforced before Go processes the output.

The call graph analysis powering `@redline` and `@approve` piggybacks on the graph the compiler builds for `needs` propagation. Effect checking already requires knowing, for each call site, what effects the callee needs and whether the caller provides them. `@redline` and `@approve` are additional predicates evaluated over the same graph in the same pass.

### Phase 3: Standard Library

- `std/db`, `std/cache`, `std/http`, `std/queue`, `std/storage` with default adapters
- `std/jwt`, `std/crypto`, `std/secrets`
- `std/resilience` (CircuitBreaker, RetryPolicy, Bulkhead)
- `std/ai` with `@tool`, `ai.prompt<T>`, `@sandbox`, `@budget`
- `std/metric`, `std/trace`, `std/health` (OTLP export)
- `std/safety` (unstable): provenance chains, drift detection, output contracts, capability decay
- Migration tooling (`splash migrate`)

Deliverable: a developer can build a production-grade API with `splash new`, `splash dev --adapters memory`, and `splash deploy`.

### Contributing

Splash is developed in the open at gosplash.dev. The compiler is written in Go. The most useful entry points for contributors are the parser and type checker (Phase 1), the effect propagation pass (Phase 2), and stdlib adapters (Phase 3). Language designers interested in the effect system formalism or the call graph analysis are encouraged to engage early — these are the areas where design input has the highest leverage before implementation hardens. See CONTRIBUTING.md for specifics on the development workflow, test suite, and RFC process for language changes.

---

## Section 7: Ecosystem and Integration

*This section is for AI labs and potential partners.*

### The Missing Layer

AI labs invest heavily in model-level safety: RLHF, constitutional AI, red-teaming, output filters. These matter and operate at the wrong level to prevent the class of failures in Section 1.

A model trained to avoid harmful outputs can still be called with harmful arguments. A model with strong refusal behavior can still be granted excessive database permissions. A model that declines to leak PII can still produce PII that a badly-written tool logs to stdout.

Splash operates at the systems layer, below the model. `@redline`, `@containment`, `@approve`, and the effect system don't interact with the model's behavior — they constrain the environment the model operates in. The model cannot call a `@redline` function because the function is not in its callable set. The model cannot access `DB.write` if the sandbox denies it. These guarantees hold regardless of what the model decides.

Think of model-level alignment as a seatbelt: it reduces harm when things go wrong. Splash is the guardrail: it structurally prevents certain wrong turns. Both are necessary. The gap between "we made the model safer" and "we made the system safe" is where most production AI incidents happen.

### Model-Agnostic by Design

The `AIAdapter` constraint means Splash applications are not bound to any specific model provider:

```splash
constraint AIAdapter {
  async fn prompt<T: Serializable>(self, req: PromptRequest) -> Result<T, AIError>
  async fn embed(self, text: String) -> Result<Embedding, AIError>
  async fn stream(self, req: PromptRequest) -> Result<Stream<String>, AIError>
}
```

Anthropic, OpenAI, xAI, and local models are all adapters. An application built on Splash can switch model providers with a one-line change in `main()`. The safety properties — `@redline`, `@approve`, data classification — are enforced by the Splash runtime regardless of which model is behind the `AIAdapter`.

This is not incidental. It means Splash's safety infrastructure is available to every model, not tied to any provider's SDK.

### `@tool` as Distribution

Every function annotated `@tool` becomes callable by any AI agent running in a Splash runtime:

```splash
/// Search sermons by theme, speaker, or scripture reference.
@tool
fn search_sermons(query: String, limit: Int = 10) -> List<SermonResult>
  needs DB { ... }
```

The doc comment is the tool description. The type signature is the schema. The function is the implementation. One source of truth for all three.

OpenAI's function calling and Anthropic's tool use share a structural problem: developers hand-write JSON schemas that describe their functions, then maintain those schemas separately from the implementations. The schemas drift. Arguments get renamed. Required fields go optional. The model calls a function with arguments that no longer match, and the error surfaces at runtime, in production, after the agent has already started a task.

Splash eliminates that class of bugs. The `@tool` decorator generates the schema from the type signature at compile time. If the function signature changes, the schema changes with it. If the schema change breaks the model's calling pattern, that's a design decision the developer makes explicitly — not a drift that accumulates silently.

An ecosystem of Splash applications is an ecosystem of `@tool`-decorated functions with accurate, compiler-generated schemas, typed return values, declared effect bounds, and budget tracking. For a model provider, that's a qualitatively different integration surface than today's JSON schema landscape.

### Liability and Provenance

As AI agents make financial transactions, access medical records, and modify infrastructure, liability becomes an urgent practical question. When an agent takes a harmful action, who is responsible and what's the evidence?

Splash's provenance chains (in `std/safety`) record every agent action with full context: the function called, the arguments, the model's stated reasoning, the parent action, the effects used, the cost, and the duration. The chain traces from goal to leaf action. Every mutation is attributable to either a human (authenticated, with role) or an agent (session ID, model, reasoning).

An organization running a Splash runtime can show, for any production incident, exactly what the agent did, in what order, for what stated reason, and under what approved capabilities. That record doesn't eliminate liability. It transforms "the AI did something and we don't know why" into a structured audit trail — a distinction that matters in regulatory inquiries, customer contracts, and legal proceedings.

---

## Appendix: The Splash Language Specification

The complete Splash language specification — syntax, type system, stdlib reference, and code samples — is maintained as a companion document. Every claim in this paper about what the compiler enforces corresponds to a specific section of the specification.

*Splash Language Specification v0.1*

---

*Splash v0.1 — gosplash.dev*
