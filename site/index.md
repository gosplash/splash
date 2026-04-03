---
layout: default
title: Splash
---

# Splash

**Compiler-Enforced Safety for AI Agent Systems**

*White Paper — v0.1*

*Zach Graves <zach@zachgraves.com>, Claude Opus 4.6, Claude Sonnet 4.6*

---

Splash is a capability-secure backend language with first-class agent semantics — the first integrated, production-oriented compiler-enforced safety layer for AI agent systems. It prevents PII leaks, constrains agent capabilities, and audits every automated decision — before the binary is produced. These are type-system properties, not runtime policies. It doesn't matter whether a human or an AI wrote the code. The compiler enforces the same constraints on both.

---

<div class="reader-guide" markdown="1">

## Reader's Guide

This document has four audiences. Each reads a different path through it.

**CISOs, VPs of Engineering, compliance teams:** Read Section 1 (The Problem) and Section 5 (Supply Chain and Organizational Safety). Skim Section 4 for the stability table. Skip the code.

**Developers:** Read Sections 1, 2, and 3. Look at the code samples first. The comparison table in Section 3 is the fastest orientation.

**Language designers and potential contributors:** Sections 1, 2, 4, and 6. Section 4 has the formal treatment of the effect system. Section 6 covers the compiler architecture.

**AI lab researchers and partners:** Read Section 1, then Section 4 (AI Safety Architecture) and Section 7 (Ecosystem and Integration). The provenance chain and output contract specifications are in Section 4.

</div>

---

## Section 1: The Problem

Deployment is where AI safety breaks down.

Labs spend billions on RLHF, constitutional AI, and red-teaming to make models behave well in controlled settings. Then a model gets deployed as an agent — connected to a database, authorized to make API calls, capable of writing files — and the safety guarantees evaporate. The model didn't forget its training. The system has no guarantees at all.

Consider what a production AI agent deployment looks like today. A function gets registered as a tool. The LLM calls it with arguments it generated. The function runs. If something goes wrong — wrong data accessed, PII logged, a transaction sent twice, a record deleted — the system has no structural way to prevent it. Developers write careful prompts. They add checks in function bodies. They hope.

Three failure modes drive most production incidents:

**PII in logs.** A developer logs a user object for debugging. The object has an email field. Somewhere in the call chain, a language model received that log as context. The email is now in a training corpus or an API provider's logs. No one made a bad decision. The type system had no way to prevent it.

**Unconstrained agent capabilities.** An agent gets a set of tools and a goal. The goal requires reading from the database. Nothing prevents the agent from calling a write function if it decides that's useful. Nothing prevents it from calling a function that deletes records. The sandbox is the developer's vigilance, not the runtime.

**Silent privilege escalation in dependencies.** A package that previously made only network calls adds database access in a patch release. The lockfile updates. CI passes. The new behavior ships to production. No one approved it. No one saw it.

These aren't developer failures or AI failures. They're system failures. The developer who logged the user object wasn't careless — the type system has no way to distinguish a loggable string from a PII email. The agent that called a write function didn't misbehave — nothing in its callable set indicated which functions were dangerous. The dependency update that added new capabilities wasn't negligent — the lockfile has no concept of capabilities to track.

Hiding side effects doesn't make them safe — it makes them invisible.

> **It doesn't matter whether a human or an AI wrote the code. Neither one can be expected to catch what the language gives the compiler no way to enforce.**

Splash addresses two classes of failure. Structural mistakes — the wrong function callable from the wrong context, sensitive data flowing into a log, a dependency acquiring capabilities it wasn't granted — are caught at compile time. The binary that fails to build cannot cause the incident. Behavioral anomalies — an agent acting within its granted capabilities but making poor decisions — are addressed at runtime through `std/safety`: provenance chains, drift detection, and output contracts. Section 4 covers both layers.

The compile-time guarantees are the foundation. They are type-system properties enforced before the binary is produced — not runtime policies that can be misconfigured, not linter rules that can be suppressed.

---

## Section 2: The Approach

Splash is a compiled, statically-typed programming language for backend systems and AI agent infrastructure. Its syntax is legible to both human reviewers and LLMs: explicit contracts, no magic, no implicit coercion.

The core premise is that most of what makes AI agent systems dangerous is structurally expressible. The wrong function is callable from the wrong context. Sensitive data can flow into a log. A dependency can acquire capabilities it wasn't granted. Structurally expressible problems have structural solutions. A compiler can enforce the constraint. Safety moves from "the developer remembered" to "the build failed."

Four design decisions carry most of the weight:

**Effects as function signatures.** Every function declares the capabilities it requires: `needs DB, Net, Clock`. The compiler verifies that every call site provides the declared effects. A function declared with `needs DB.read` cannot call a function that `needs DB.write`. An agent cannot invoke a function that `needs FS` unless the agent was explicitly granted filesystem access. Violations fail to compile.

Effects are also design pressure. A function's capability surface is visible in its signature — five effects on one function is the same signal as a constructor with ten parameters. The compiler doesn't just enforce safety; it rewards decomposition. The natural Splash architecture becomes small, focused functions with tight effect declarations, composed by a thin orchestration layer that declares the union. The agent entry point's `needs` clause is the capability manifest: readable by humans, auditable by tools, enforced by the compiler.

*A note on the current vocabulary:* `DB.read`, `DB.write`, and `Net` describe the services they use rather than the capability boundaries they cross. This is a deliberate v0.1 choice — the names are immediately intuitive to developers. A v0.2 design pass will evaluate a boundary-oriented vocabulary (`Persist.read`, `Network`, `Infer`) that expresses *what external boundary a function crosses* rather than *which backend it uses*. The compiler mechanism and safety semantics are identical either way. The prerequisite for any rename is solving the granularity question: real sandboxing policies distinguish cache reads from database reads, and row mutations from schema changes — the new taxonomy must support those distinctions before it ships.

**Data classification in the type system.** Fields annotated `@sensitive` or `@restricted` affect what constraints their containing types can satisfy. A `@sensitive Email` field prevents the type from satisfying the `Loggable` constraint. The logging call fails at build time, not in a runtime scan.

**Agent context as a typed boundary.** Agent context is not inferred — it's explicit. Functions marked `needs Agent`, and call paths reachable from `agent.execute()`, form a distinct execution context the compiler tracks. Safety annotations check by tracing call graphs from these entry points. The compiler knows which functions an agent can reach, and rejects the ones it shouldn't.

**Adapter-first stdlib.** Every service in the standard library — databases, caches, queues, storage, AI providers — is defined as a constraint interface, not a concrete implementation. Application code depends on the interface. Backends wire in at startup. This is what makes `splash dev --adapters memory` possible: zero infrastructure to start development, a one-line change in `main()` to swap in real backends, no application code changes.

---

## Section 3: The Language

*This section is for developers. It's also the fastest path for any audience to see what Splash looks like.*

### Starting a Project

```splash
$ splash new acme-api
$ splash dev --adapters memory
  listening on :8080
  hot reload enabled
```

No Postgres. No Redis. No Docker. The `--adapters memory` flag wires every stdlib service to an in-memory implementation. Write handlers, test behavior, iterate. When ready for real infrastructure, change one line in `main()`. The application code doesn't change.

### What Code Looks Like

```splash
// A function that reads from the database and calls an AI model.
// Its capabilities are declared in the signature, not hidden in the body.

fn analyze(doc_id: DocumentId) needs DB, AI -> Result<DocumentInsight, AppError>
{
  let doc = try db.find(Document, doc_id)
  return ai.prompt<DocumentInsight>({
    model:   "claude-opus-4-6",
    system:  "You are a document analyst.",
    input:   doc.content,
    budget:  Cost.usd(0.05),
    timeout: 30.seconds,
  })
}
```

`ai.prompt<DocumentInsight>` uses the `DocumentInsight` type as the structured output schema and as the validation contract. The type is the JSON Schema. When the model returns, Splash validates the response against the type before returning to the caller. No hand-written schema. No separate validation step. One source of truth for humans, agents, and the compiler.

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
/// Search documents by topic, author, or keyword.
@tool
fn search_documents(
  /// The search query — topic, keyword, or author name
  query:   String,
  limit:   Int     = 10,
  author:  String?,
  tag:     String?,
) needs DB -> List<DocumentResult> { ... }
```

`@tool` turns any function into an AI-callable tool. The doc comment becomes the tool description. The type signature becomes the JSON Schema. The same source is the implementation, the schema, and the documentation.

### Sandboxed Agents

```splash
@sandbox(allow: [DB.read, AI], deny: [Net, FS, DB.write])
@budget(max_cost: Cost.usd(0.50), max_calls: 20)
fn answer_question(q: String) needs Agent -> Result<Answer, AgentError> {
  return agent.execute(q, tools: [search_documents, lookup_reference])
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
fn agent_cleanup(goal: String) needs Agent {
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

`@approve` means "this action requires authorization before execution." The annotation lives on the function. The compiler enforces that it is present where policy demands. The runtime routes the approval request through an `ApprovalAdapter` — the organization decides how authorization works.

```splash
@approve
fn charge_card(amount: Money, method: PaymentMethod) needs Net -> Result<Charge, PaymentError>
{ ... }
```

`@approve` is a precondition, not a return type modifier. The function's declared type is unchanged — `charge_card` returns a `Charge`. The annotation means "this function does not execute until the adapter approves." If the adapter denies, the function body never runs. The Splash programmer writes normal code; the compiler and runtime handle the gate.

The error model is uniform across all agent failures. `@approve` denials, AI call failures, and budget exceeded errors all propagate as Go errors up the same call path to the `needs Agent` boundary — the agent entry point is the single declared error surface. The Splash programmer writes `-> Charge` and `-> HealthInsight`; the compiler injects the error propagation in generated Go. The agent entry point returns `(T, error)` and its callers — HTTP handlers, queue workers — handle it as a normal Go error. The compiler never injects `os.Exit` inside generated function bodies; that decision belongs to the application boundary, not the compiler.

This symmetry is intentional: **error propagation follows the same path as capability propagation.** Effects flow up the call graph to the Agent boundary. Errors flow up the same path to the same boundary. Both terminate where agent execution begins.

The future target exposes typed denial at the Splash level — callers handle `Denied` and `Timeout` as structured cases:

```splash
// Future target
enum ApprovalError { Denied | Timeout | AdapterUnavailable }

constraint ApprovalAdapter {
  fn request(self, req: ApprovalRequest) -> Result<ApprovalResponse, ApprovalError>
}
```

The `ApprovalRequest` carries the function name, serialized arguments, trace context, and a deadline. Arguments are serialized with data classification awareness: `@restricted` fields are redacted automatically before the request leaves the process. A human reviewing an approval request for `charge_card` sees the amount and the last four digits of the card — not the full card number, not the CVV. The data classification system earns its keep in a place you would not think to look.

Adapters swap in at initialization, not at call sites:

```splash
fn main() {
  let approval = match env {
    .dev     => StdinApproval {}
    .staging => SlackApproval { channel: "#approvals" }
    .prod    => WebhookApproval { url: secrets.approval_webhook }
  }
  run(approval: approval)
}
```

`StdinApproval` blocks on a terminal prompt — suitable for development. `WebhookApproval` fires a webhook and awaits a callback with a deadline. `PolicyApproval` applies organizational rules automatically (charges under $10 from verified users are auto-approved with an audit log entry). Same application code, different resolution strategy per environment.

`StdinApproval` blocks the calling goroutine on a terminal prompt — honest behavior for a development tool where a human is present. Production adapters (`WebhookApproval`, `SlackApproval`) are designed to be non-blocking: the approval request is enqueued, a response channel is awaited with a deadline, and the outcome flows back as a typed result. A pending approval in production does not block the agent's event loop. Phase 4a shipped the adapter pattern with `StdinApproval`. Phase 4b shipped denial propagation: the error cascades from adapter through every transitive caller, so production adapters that return denial errors work correctly without process-killing behavior. Non-blocking production adapters (`SlackApproval`, `WebhookApproval`) are the next milestone.

The compiler enforces `@approve` requirements when `@containment` policy demands it:

```splash
@containment(agent: .approved_only)
module billing
```

Any function in the `billing` module that is reachable from an agent context but lacks `@approve` fails to compile. The developer must add `@approve` or `@agent_allowed(reason: "...")`. Silence is a build failure.

### Stability Boundary

| Mechanism | Status | Enforced By |
|---|---|---|
| `@redline` | Stable v0.1 | Compiler — call graph analysis |
| `@containment` | Stable v0.1 | Compiler — module boundary |
| `@approve` annotation | Stable v0.1 | Compiler — presence enforcement |
| `@approve` runtime dispatch | Stable v0.1 | Runtime — `ApprovalAdapter` + denial cascade |
| `@agent_allowed` | Stable v0.1 | Compiler — requires stated reason |
| Capability decay | **Unstable** | Runtime (`std/safety`) |
| Provenance chains | **Unstable** | Runtime (`std/safety`) |
| Drift detection | **Unstable** | Runtime (`std/safety`) |
| Output contracts | **Unstable** | Runtime (`std/safety`) |

The compiler-enforced primitives will not change without a major version. The `std/safety` runtime APIs ship for production use and feedback. The mechanisms that prove out become stable in v0.2.

### Formal Properties

*This subsection is for language designers and researchers evaluating the effect system's soundness.*

**Prior art.** Splash's effect system draws from Koka and Frank; its capability model from Pony and Capsicum; its data classification from information flow type systems in the tradition of Jif and FlowCaml. The contribution is not any individual mechanism — it's their unification into a single language designed for AI agent deployment, with agent reachability as a first-class static property checked at compile time. What is new is not the mechanisms, but their integration into a single, compile-time enforced system where agent reachability is a first-class property — the call graph is not an implementation detail but the primary artifact the safety model reasons over.

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

**Soundness.** The effect system has no escape hatch by design. There is no `unsafe` block, no `@suppress` annotation, no compiler flag that relaxes checking. `@redline` cannot be overridden by policy or configuration. The analysis is whole-program: compilation units cannot circumvent `@redline` or `@approve` enforcement through separate compilation, because both checks require the complete call graph. A Splash program that builds has been verified — every call site satisfies the callee's effect requirements, every agent-reachable path has been checked against `@redline` and `@approve` constraints, and every `@sensitive` and `@restricted` classification has been propagated through the type system. Within its declared scope, the system is sound. This is a deliberate design choice: an escape hatch is a vulnerability.

**Escape valves.** The no-escape-hatch stance will be revisited deliberately in v0.2, which will introduce a `@bypass(reason: "...", approved_by: "...")` annotation. It requires a stated justification, is logged to the provenance chain, and is surfaced by `splash audit`. The key constraint: `@bypass` is never available for `@redline`. Redlines are absolute. Everything else — effect mismatches, classification violations — admits an auditable, greppable override rather than forcing developers off the language. Bypassed constraints do not propagate implicitly; any call site relying on a bypass must itself declare it, ensuring the escape is visible in the call graph and cannot silently undermine downstream safety assumptions.

#### Agent Context and Call Graph Analysis

Agent context is a distinguished capability in the effect system. Functions with `needs Agent`, and call paths reachable from `agent.execute()`, are agent-context entry points. The compiler builds the full call graph and propagates agent-reachability transitively.

For `@redline` enforcement: a function `f` marked `@redline` must not appear in any call path from any agent-context entry point. If such a path exists, the build fails.

This analysis runs over the same call graph the compiler builds for `needs` propagation — every call site must satisfy the callee's effect requirements, so the full graph is already available. `@redline` and `@approve` checks are additional predicates evaluated over that graph in the same pass.

The analysis is whole-program. Compilation units cannot be checked in isolation for `@redline` and `@approve` correctness. This constraint is intentional: it prevents effect laundering through separately-compiled modules.

**Scalability.** Whole-program call graph analysis is O(V+E) in the number of functions and call edges — linear in program size. For the server-side programs Splash targets (thousands of functions, not millions), this is fast in practice, and it runs as a single pass over the same graph the effect checker already builds. The cost is real but bounded and predictable. Incremental builds can cache the call graph and invalidate only the subgraph reachable from changed functions; this is on the Phase 3 roadmap. Dynamic dispatch and runtime-loaded code are conservatively approximated in the call graph; where full resolution is not possible, the compiler defaults to denying agent reachability, preserving the soundness of `@redline` and `@approve` enforcement.

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

**Runtime overhead.** Splash's safety properties are enforced entirely at compile time. The emitted Go binary carries no effect-checking overhead — effects are a frontend constraint, not a runtime mechanism. The only runtime cost is explicit and opt-in: `@approve` gates invoke an `ApprovalAdapter` before the function body executes, and `std/safety` provenance chains record agent decision paths. For everything else, Splash programs have Go-equivalent performance.

Structured concurrency maps to Go's goroutine model. `group { async f(); async g() }` compiles to a goroutine group with cancellation semantics using Go's `context` package and `errgroup`. The structured guarantee — all children cancelled and awaited before the parent continues — is upheld by the generated code.

Context propagation is implemented via goroutine-local storage. Context is implicitly available in every Splash function; explicit reads (`ctx.remaining`, `ctx.check()`, `ctx.get(AuthUser)`) compile to reads from a goroutine-local value. Developers don't thread context through signatures; the compiler inserts the plumbing.

**Backend abstraction.** Code generation targets a `Backend` interface. The current implementation emits Go. An LLVM backend is straightforward to add: the safety model is fully enforced before codegen, so the backend receives a verified, effect-annotated AST with no security-relevant decisions remaining. This separation is intentional. All guarantees about effects, data flow, and agent reachability are established in the compiler front-end. Backends are responsible only for translating verified programs into executable form. The same source file, the same call graph analysis, the same `@redline` and `@approve` enforcement — regardless of whether the output is a Go binary, a native object via LLVM, or a WASM module.

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

### Case Study: axios Supply Chain Attack (March 31, 2026)

An attacker compromised a maintainer account for axios — a JavaScript HTTP client with 100 million weekly downloads — and published malicious versions containing a hidden dependency (`plain-crypto-js@4.2.1`) with a `postinstall` hook that downloaded and executed a cross-platform remote access trojan. The malware self-deleted after execution. The packages were live for approximately three hours.

Five independent Splash defenses would have stopped it. Any one is sufficient.

**No build hooks — the execution vector is inert.** The RAT was delivered through npm's `postinstall` mechanism. Splash has no build-time hooks. The malicious code would have been inert bytes in the dependency cache. The execution vector doesn't exist in the language.

**Effect-permissioned lockfile — new effects are visible and require approval.** `plain-crypto-js` requires `Net`, `FS`, and `Exec` to phone home and deploy payloads. Adding it to axios's dependency tree surfaces in the lockfile diff as a new package requesting those effects — a visible, reviewable change that cannot ship without explicit human approval.

**Transitive effect ceiling — child cannot exceed parent's grants.** axios is legitimately granted `[Net]` — it's an HTTP client. `plain-crypto-js` claims `[Net, FS, Exec]`. A transitive dependency cannot exceed the effects granted to its direct parent. The build fails.

**Unverified publisher gate — staged packages are blocked.** The attacker published a clean `4.2.0` eighteen hours before the attack to establish brief history. Splash's publisher verification gate blocks packages with no verified download history without `--trust-unverified`. AI agents cannot pass that flag by design.

**CI lockfile review — automated builds block on unapproved changes.** With `ci_lockfile_review: true`, any lockfile change introducing new effects or unverified dependencies blocks the automated build until a human reviews the diff. The three-hour publication window becomes irrelevant.

The axios attack required a `postinstall` hook, an unverified dependency, a transitive privilege escalation, and an unmonitored CI window. Splash eliminates all four preconditions at the language level. The attack has no surface to land on.

### The Operational Controls Comparison

Today's best-practice response to supply chain attacks is a stack of independent operational controls: locked lockfiles, `--ignore-scripts` in CI pipelines, registry proxies with quarantine, secret isolation during installation, automated vulnerability alerts, and dedicated incident response procedures. Each control requires correct configuration across developer machines, CI systems, and organizational process. Each can be misconfigured. Each can be forgotten. The incident response checklist at the end of every security advisory exists because *when these controls fail*, there is significant manual forensic work ahead.

Splash collapses most of this to two structural properties: no build hooks (eliminates the entire `postinstall` attack class) and effect-permissioned lockfile (makes privilege escalation visible and reviewable before it ships). The remaining controls — secret isolation, unverified package quarantine — map to `@restricted` types and the publisher verification gate.

These aren't controls you configure. They're properties of the language. There is no `--ignore-scripts` flag because there are no scripts to ignore. The argument isn't that operational security practices are wrong. It's that they're necessary *because the language doesn't help you*. Splash makes the language help you.

### Organizational Policy

```splash
// splash.policy — enforced at build time

policy "acme" {
  deny_effects:           [FS]
  net_proxy:              "https://egress.corp.internal"
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

The compilation model and backend abstraction are described in Section 4 (Compilation Model). The phases below track the buildout of the language and standard library against that architecture.

The Go target is a deliberate first choice. Building a production runtime from scratch is a multi-year project before a developer can ship their first API. Go's runtime is proven and well-understood. The Splash compiler team focuses on the frontend — type system, effect system, classification analysis, safety enforcement — and delegates runtime concerns to Go. The `Backend` interface means additional targets (LLVM, WASM) can be added without touching the safety-relevant frontend.

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
- `@approve` annotation enforcement: compiler verifies presence at agent-reachable call sites
- Data classification checks (`@sensitive`, `@restricted` constraint satisfaction)
- Go code generation from typed, verified ASTs
- `splash build` and `splash dev` CLI

Deliverable: `splash build` compiles a Splash program to a Go binary. Effect system and safety properties are enforced before Go processes the output.

The call graph analysis powering `@redline` and `@approve` piggybacks on the graph the compiler builds for `needs` propagation. Effect checking already requires knowing, for each call site, what effects the callee needs and whether the caller provides them. `@redline` and `@approve` are additional predicates evaluated over the same graph in the same pass.

### Phase 3: Standard Library

- `std/ai` with `@tool`, `ai.prompt<T>`, `@sandbox`, `@budget` ✅
- `std/db`, `std/cache`, `std/http`, `std/queue`, `std/storage` with default adapters
- `std/jwt`, `std/crypto`, `std/secrets`
- `std/resilience` (CircuitBreaker, RetryPolicy, Bulkhead)
- `std/metric`, `std/trace`, `std/health` (OTLP export)
- `std/safety` (unstable): provenance chains, drift detection, output contracts, capability decay
- Migration tooling (`splash migrate`)

Deliverable: a developer can build a production-grade API with `splash new`, `splash dev --adapters memory`, and `splash deploy`.

### Phase 4: Runtime Safety

- ✅ `@approve` adapter pattern, body injection, `StdinApproval` (Phase 4a)
- ✅ Denial propagation — `@approve` functions get `(T, error)` Go signatures; error cascades through every transitive caller; `main()` exits gracefully (Phase 4b)
- Non-blocking production adapters: `SlackApproval`, `WebhookApproval`, `PolicyApproval` (Phase 4c)
- ✅ `@sandbox` compile-time effect allow/deny enforcement against agent-reachable call graph
- ✅ `@budget` compile-time argument type validation
- `@budget` runtime enforcement — instrumented counter in `ai.prompt` calls, `BudgetExceeded` propagation (requires `std/ai` runtime)
- ✅ `Loggable` built-in constraint; `@sensitive` blocks `Loggable` satisfaction; unknown constraint names are compile errors
- ✅ Multi-file module loading (`use path` resolves sibling `.splash` files; cycle detection; `expose` list)
- `splash deploy` with capability manifest diffing (lockfile-level capability tracking)

Deliverable: `@approve` works in production without killing the process. One denied approval propagates as an error to one request; other requests in flight are unaffected. An organization can swap authorization strategies per environment without changing application code.

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
  fn prompt<T: Serializable>(self, req: PromptRequest) -> Result<T, AIError>
  fn embed(self, text: String) -> Result<Embedding, AIError>
  fn stream(self, req: PromptRequest) -> Result<Stream<String>, AIError>
}
```

Anthropic, OpenAI, xAI, and local models are all adapters. An application built on Splash can switch model providers with a one-line change in `main()`. The safety properties — `@redline`, `@approve`, data classification — are enforced by the Splash runtime regardless of which model is behind the `AIAdapter`.

This is not incidental. It means Splash's safety infrastructure is available to every model, not tied to any provider's SDK.

### `@tool` as Distribution

Every function annotated `@tool` becomes callable by any AI agent running in a Splash runtime:

```splash
/// Search documents by topic, author, or keyword.
@tool
fn search_documents(query: String, limit: Int = 10) needs DB -> List<DocumentResult> { ... }
```

The doc comment is the tool description. The type signature is the schema. The function is the implementation. One source of truth for all three.

OpenAI's function calling and Anthropic's tool use share a structural problem: developers hand-write JSON schemas that describe their functions, then maintain those schemas separately from the implementations. The schemas drift. Arguments get renamed. Required fields go optional. The model calls a function with arguments that no longer match, and the error surfaces at runtime, in production, after the agent has already started a task.

Splash eliminates that class of bugs. The `@tool` decorator generates the schema from the type signature at compile time. If the function signature changes, the schema changes with it. If the schema change breaks the model's calling pattern, that's a design decision the developer makes explicitly — not a drift that accumulates silently.

An ecosystem of Splash applications is an ecosystem of `@tool`-decorated functions with accurate, compiler-generated schemas, typed return values, declared effect bounds, and budget tracking. That's not a convenience — it's infrastructure that doesn't exist anywhere else.

Today's tool ecosystem is a collection of hand-maintained JSON schemas that drift from implementations, with no shared model for what effects a tool can perform, what data classifications it touches, or what it will cost to call. There is no structural way for a model provider to know, before invocation, whether a tool will make network calls, write to a database, or consume $0.05 of AI compute. A model can be given a tool and have no way to know it's dangerous.

Splash makes all of that statically knowable at the call site. For model providers and AI labs building agent infrastructure, the difference between "tools you can invoke hopefully" and "tools you can reason about structurally" is the difference between an integration surface and a foundation. A model provider integrating with Splash applications can verify, at compile time, that a tool will only read from the database, will cost at most $0.05 per invocation, and will never touch PII — without reading the implementation.

### Compiler-Derived Tool Surfaces

Tool schemas in Splash are not developer-authored. They are compiler projections of the agent-reachable call graph.

```bash
$ splash tools agent_tools.splash
```

This command does not enumerate all `@tool` functions. It includes only those that are agent-reachable and not excluded by `@redline` or `@containment`. The output is the set of actions the agent is structurally permitted to take — filtered by the same call graph analysis that enforces the rest of the safety model. A function absent from the output is not merely undocumented; it is structurally unreachable from agent context.

**The absence is the guarantee.**

This is a qualitatively different guarantee than OpenAPI, MCP-style tool registries, or hand-written function-calling schemas. Those formats describe what the system does. A Splash tool surface describes what the agent is allowed to do, enforced before the binary is produced.

The schema is not documentation of the system. It is the projection of what the agent can reach — and its absences are as meaningful as its presences. A model provider consuming a Splash tool surface can reason about it structurally: any function not in the schema cannot be called, cannot be reached through a chain of calls, and cannot be smuggled in through a dependency that acquired excess effects. The boundary is the compiler's output, not a developer's discipline.

Where dynamic dispatch or runtime loading prevents full call graph resolution, the compiler conservatively excludes such paths from the agent surface — the guarantee defaults to denial, not admission.

For AI labs and model providers building agent infrastructure, this is the distinction that matters: the difference between a tool list you trust because a developer wrote it carefully, and a tool list you trust because a compiler produced it from a verified call graph.

### Liability and Provenance

As AI agents make financial transactions, access medical records, and modify infrastructure, liability becomes an urgent practical question. When an agent takes a harmful action, who is responsible and what's the evidence?

Splash's provenance chains (in `std/safety`) record every agent action with full context: the function called, the arguments, the model's stated reasoning, the parent action, the effects used, the cost, and the duration. The chain traces from goal to leaf action. Every mutation is attributable to either a human (authenticated, with role) or an agent (session ID, model, reasoning).

An organization running a Splash runtime can show, for any production incident, exactly what the agent did, in what order, for what stated reason, and under what approved capabilities. That record doesn't eliminate liability. It transforms "the AI did something and we don't know why" into a structured audit trail — a distinction that matters in regulatory inquiries, customer contracts, and legal proceedings.

### Beyond AI Agents

The safety primitives in Splash were designed for LLM agents calling tool functions. None of them are specific to LLMs.

`@redline` doesn't know what an agent is. It knows what a call graph is. `needs Vehicle.braking` and `needs DB.write` are the same type system mechanism — a declared capability that the compiler verifies at every call site. `@containment(agent: .none)` works identically whether the agent is a language model or a PID controller. The underlying primitive is not "safety for AI." It is **compiler-verified autonomy boundaries**: the proof that an autonomous component cannot reach capabilities it was not granted, regardless of what that component is.

The pattern applies wherever an autonomous component makes decisions that trigger real-world consequences.

**Vehicle autonomy.** A neural network planner that needs `Vehicle.steering` and `Vehicle.throttle` cannot call a function that needs `Vehicle.safety_override` — the compiler proves it. Emergency brake functions in a `@containment(agent: .none)` module are structurally invisible to the planner. A software update to the navigation subsystem that suddenly requests `Vehicle.braking` fails the build. These are compile-time proofs, not runtime checks that might fail under the conditions that matter most.

**Medical devices.** A dosing algorithm's effect declarations enumerate exactly which hardware registers it can write. `@redline` on the manual override function means no code path from the autonomous dosing agent can reach it — provable from the binary, not inferred from test coverage. FDA increasingly asks for evidence that autonomous components cannot reach safety-critical functions through any code path; a compiler proof is stronger evidence than a test suite.

**Industrial control.** The autonomous optimizer for a chemical plant declares `needs Process.read, Process.throttle`. The emergency shutdown controls live in a `@containment(agent: .none)` module. The compiler guarantees the optimizer's call graph does not connect to them — not a process boundary that could be bridged, not a permission check that could be bypassed at 3am during an incident.

**Drone operations.** A navigation agent that needs `Drone.motors, Drone.navigation` cannot call `Drone.payload_release`. `@approve` on high-consequence maneuvers routes through a deterministic safety validator with a hard timeout — the same language primitive that routes human approval for financial transactions, applied to a 200ms pre-release check.

The v0.1 compiler produces Go binaries for backend services. The domains above are the roadmap, not the current deliverable. But the primitives are already general — the same type system, the same call graph analysis, the same compiler. The agent doesn't have to be an LLM. It just has to be something that acts autonomously in a system where acting wrong is expensive.

---

## Section 8: Compiler Performance

Splash's safety model requires whole-program analysis: the call graph must be complete before `@redline`, `@approve`, `@sandbox`, and `@containment` can be verified. This raises a practical question: does the analysis cost show up in developer feedback loops?

The answer, measured on real hardware, is no.

### Benchmark Methodology

Benchmarks use synthetic Splash programs at three sizes — 100, 500, and 2000 functions — across three workloads:

- **Flat:** independent functions with no effects or call relationships
- **Effects chain:** linear call chain with `DB.read` declared at every site, exercising effect propagation checking at every call site
- **Mixed types:** named record types with struct literal construction, exercising type resolution

The end-to-end `splash check` benchmark runs the full pipeline — parse, type check, call graph construction, and all safety passes — on programs that include `@approve`, `@redline`, and `@sensitive` types, as a realistic representation of production code.

All measurements on Apple M2.

### Results

**Call graph construction** (BFS + adjacency map, O(V+E)):

| Functions | Build | Reachable BFS | Callers (reverse BFS) |
|---|---|---|---|
| 100 | 14 µs | — | — |
| 1,000 | 180 µs | — | — |
| 5,000 | 919 µs | — | — |

**Type checker** (`Check` pass only, excluding parse):

| Functions | Flat | Effect chain | Mixed types |
|---|---|---|---|
| 100 | 17 µs | 25 µs | 79 µs |
| 500 | 108 µs | 159 µs | 485 µs |
| 2,000 | 535 µs | 819 µs | — |

**`splash check` full pipeline** (parse + type check + call graph + all safety passes, including file I/O):

| Functions | Wall time |
|---|---|
| 100 | 218 µs |
| 500 | 1.06 ms |
| 2,000 | 6.24 ms |

### Interpretation

A 500-function Splash program clears `splash check` in 1 millisecond. A 2,000-function program — larger than most single-module backend services — in 6 milliseconds. These are wall-clock times including file I/O, not in-memory-only measurements.

Scaling is linear in program size, as expected for O(V+E) analysis. The whole-program call graph requirement that gives Splash its soundness guarantees does not create a quadratic blowup — the graph is built in a single pass over the same data structure the type checker already constructs.

The safety checks themselves (all five passes: `@redline`, `@approve`, `@containment`, `@sandbox`, data classification) add negligible overhead on top of type checking. They are additional predicates evaluated over the same graph, not separate traversals.

Incremental caching — invalidating only the subgraph reachable from changed functions — is on the roadmap and would reduce hot-reload times further. The current single-shot analysis is already fast enough that caching is an optimization, not a requirement.

### Runtime Overhead

The safety model has no runtime cost for the common case. Effect checking, call graph analysis, data classification, and agent reachability are enforced entirely at compile time. The emitted Go binary carries no effect-checking overhead — effects are a frontend constraint, not a runtime mechanism.

The only explicit runtime costs are:

- **`@approve` gate:** one `ApprovalAdapter.Request(name)` call before the function body executes. Cost is determined by the adapter implementation (stdin prompt, Slack message, webhook). The compiler overhead is zero.
- **`std/safety` provenance chains:** structured logging of agent decisions. Opt-in, disabled by default, cost proportional to chain depth.

For everything else, Splash programs have Go-equivalent performance at runtime.

---

## Appendix: The Splash Language Specification

The complete Splash language specification — syntax, type system, stdlib reference, and code samples — is maintained as a companion document. Every claim in this paper about what the compiler enforces corresponds to a specific section of the specification.

---

*Splash v0.1 — gosplash.dev*
