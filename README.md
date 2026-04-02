# Splash

Splash is a compiled, statically-typed backend language with a built-in effect system, call graph analysis, and AI agent safety enforcement. It transpiles to Go. Safety properties — effect constraints, data classification, agent reachability — are enforced before codegen. The Go backend sees a verified, effect-annotated AST with no security-relevant decisions remaining.

```splash
@sandbox(allow: [DB.read, AI])
@tool
fn search_catalog(query: String) needs DB.read, AI -> List<SearchResult> {
    let rows = db.query(query)
    return ai.rank<SearchResult>(rows)
}
```

`splash tools search_catalog.splash` emits JSON Schema. `splash check` enforces the sandbox at compile time. `splash build` produces a native binary.

---

## Quick Start

```bash
go build ./cmd/splash/...          # build the compiler

./splash check examples/effects/effects.splash
./splash check examples/sandbox/sandbox.splash
./splash check examples/approval/approval.splash

./splash build examples/hello/hello.splash -o hello && ./hello
./splash emit  examples/approval/approval.splash   # print generated Go
./splash tools examples/agent_tools/agent_tools.splash  # print JSON Schema
```

All tests:

```bash
go test ./...
```

---

## Repository Layout

```
cmd/splash/main.go          CLI: check / build / emit / tools
internal/
  lexer/                    tokenizer
  parser/                   Pratt parser → AST
  ast/                      AST node types, annotation kinds
  effects/                  EffectSet bitmask, Parse(), String()
  typechecker/              type inference, constraint checking, use-imports
  callgraph/                call graph, Reachable(), Callers(), Parents(), PathTo()
  safety/                   @redline, @approve, @containment, @sandbox, @budget, @sensitive
  codegen/                  Backend interface, GoBackend, Emitter
  toolschema/               @tool → JSON Schema extraction
  diagnostic/               Diagnostic type (Error/Warning + position)
  types/                    data classification lattice
examples/                   nine checked, buildable Splash programs
docs/superpowers/plans/     implementation plans (historical)
WHITEPAPER.md               design rationale and formal model
```

---

## Compilation Pipeline

```
source (.splash)
  → Lexer        tokenize
  → Parser       produce *ast.File
  → TypeChecker  type inference, effect propagation, constraint satisfaction
  → CallGraph    build directed call graph, compute agent roots + reachability
  → SafetyChecker  @redline, @approve, @containment, @sandbox, @budget, @sensitive/@restricted
  → Emitter      verified AST → Go source
  → go build     Go source → native binary
```

Each stage is independent. The CLI in `cmd/splash/main.go` wires them. `splash check` runs through SafetyChecker and stops. `splash build` runs the full pipeline including `go build`.

---

## Effect System

Effects are declared in function signatures with `needs`. The compiler verifies every call site provides a superset of the callee's required effects.

```splash
fn fetch(id: Int) needs DB.read -> Report { ... }
fn summarize(id: Int) needs DB.read, AI -> String { return fetch(id) }
// fn bad() -> String { return summarize(1) }  // error: missing DB.read, AI
```

**Effect vocabulary:**

| Effect | Meaning |
|---|---|
| `DB.read` | read from database |
| `DB.write` | write to database |
| `DB.admin` | schema-level database operations |
| `Net` | outbound network calls |
| `AI` | calls to AI model providers |
| `Agent` | agent entry point (structural, not a capability) |
| `FS` | filesystem access |
| `Exec` | subprocess execution |
| `Cache` | cache read/write |
| `Secrets` | access to secrets store |
| `Queue` | message queue operations |
| `Metric` | metrics emission |

`Agent` is structural — it marks a function as an agent entry point for call graph analysis. It does not represent a runtime capability.

---

## Annotation Reference

| Annotation | Applies To | Effect |
|---|---|---|
| `@tool` | function | marks as AI-callable; `splash tools` emits JSON Schema |
| `@redline` | function | build fails if any agent-reachable path reaches this function |
| `@approve` | function | injects human approval gate; emits `(T, error)` Go signature |
| `@agent_allowed` | function | exempts from `@containment(agent: "approved_only")` check |
| `@containment(agent: "none"\|"read_only"\|"approved_only")` | module | module-level agent access policy |
| `@sandbox(allow: [...], deny: [...])` | function | constrains effects of the entire reachable call graph |
| `@budget(max_cost: Float, max_calls: Int)` | function | declares resource limits; types validated at compile time |
| `@sensitive` | field | field is PII; containing type cannot satisfy `Loggable`; `@tool` cannot return it |
| `@restricted` | field | field is process-local; no storage adapter accepts it |
| `@internal` | field | field is internal-only; affects classification lattice |

---

## Data Classification

Classification is a property of types, not values. Four levels form a lattice:

```
public < @internal < @sensitive < @restricted
```

The classification of a composite type is the max classification of its fields. The compiler enforces two rules today:

1. `@tool` functions cannot return a type whose classification exceeds `@internal` — PII would flow into the agent's context.
2. `println` (and any function requiring `Loggable`) cannot accept a `@sensitive`-classified argument.

---

## `splash tools` Output

JSON Schema for every `@tool` function in a file:

```json
[
  {
    "name": "search_catalog",
    "description": "Search the catalog for items matching a query.",
    "parameters": {
      "type": "object",
      "properties": {
        "query": { "type": "string" },
        "limit": { "type": "integer" }
      },
      "required": ["query", "limit"]
    },
    "effects": ["DB.read"]
  }
]
```

The schema is derived entirely from the type signature and `///` doc comments. No hand-written schema.

---

## Multi-File Modules

```splash
// billing.splash
module billing
expose charge_customer, Charge

type Charge { id: Int; amount_cents: Int }
fn charge_customer(id: Int, amount: Int) needs Net -> Charge { ... }

// agent.splash
module agent
use billing

fn run_billing_agent() needs Agent, Net -> Charge {
    return charge_customer(1, 100)   // billing symbols injected flat
}
```

`use billing` resolves to `billing.splash` in the same directory. Cycles are detected and rejected. The `expose` list controls what is injected into the importing namespace. `splash emit` and `splash build` merge declarations into a single Go package.

---

## `@approve` Runtime

`@approve` on a function does three things at compile time:

1. The function's Go return type is widened to `(T, error)`.
2. `splashApprove("fn_name")` is injected as the first statement of the body.
3. Every transitive caller also gets `(T, error)` signatures — denial propagates as an error, not a panic.

```go
// Generated Go for @approve fn charge_card(...) needs Net -> Charge
func chargeCard(customerID int, amountCents int) (Charge, error) {
    if err := splashApprove("charge_card"); err != nil {
        return Charge{}, err
    }
    // ... body
}
```

The default `ApprovalAdapter` prompts stdin. Swap it before agent startup:

```go
SetApprovalAdapter(&WebhookApproval{URL: secrets.ApprovalWebhook})
```

---

## Package Contracts

**`internal/callgraph`**
- `Build(f *ast.File) *Graph` — constructs the call graph
- `g.AgentRoots() []string` — functions with `needs Agent` or `@tool`
- `g.Reachable(roots []string) map[string]bool` — forward BFS
- `g.Callers(targets map[string]bool) map[string]bool` — reverse BFS (used for `@approve` cascade)
- `g.Parents(roots []string) map[string]string` — BFS parent map for path reconstruction
- `callgraph.PathTo(parents, target)` — reconstruct call path for error messages

**`internal/safety`**
- `New().Check(file, graph)` — runs all passes, returns `[]diagnostic.Diagnostic`
- Add a new safety pass by adding a `checkFoo` method and calling it from `Check`

**`internal/codegen`**
- `Backend` interface: `Emit(f *ast.File, opts Options) string`
- `NewGoBackend() Backend` — returns the Go emitter
- `Options.ApprovalCallers map[string]bool` — transitive callers of `@approve` functions (set by CLI after callgraph analysis)
- To add a new backend: implement `Backend`, add a factory function, wire in CLI

**`internal/typechecker`**
- `tc.SetFileLoader(baseDir, readFileFn)` — enables `use` import resolution
- `tc.LoadedFiles() []*ast.File` — imported files for `mergeFiles` in the CLI
- `tc.Check(f) (TypeMap, []diagnostic.Diagnostic)`

---

## Adding a Safety Check

1. Add a `checkFoo(file *ast.File, g *callgraph.Graph) []diagnostic.Diagnostic` method to `internal/safety/checker.go`
2. Call it from `Check()`
3. Add tests to `internal/safety/checker_test.go` using the `check(src string)` helper

The `check` helper parses, builds the call graph, and runs all safety passes. Write the failing test first.

---

## Examples

| Example | Demonstrates |
|---|---|
| `hello/` | types, optionals, null coalescing, struct literals |
| `effects/` | `needs` declarations, effect propagation, `@redline`, `@approve` |
| `data_safety/` | `@sensitive`, `@restricted`, data classification |
| `containment/` | `@containment` module policy |
| `agent_tools/` | `@tool`, `@redline` on dangerous operations, agent entry point |
| `ai_prompt/` | `use std/ai`, `ai.prompt<T>`, `Result<T, E>` |
| `approval/` | `@approve` runtime, `ApprovalAdapter` |
| `multi_file/` | `use module`, cross-file types, `splash emit` merging |
| `sandbox/` | `@sandbox` effect constraints, `@budget` resource limits |

---

## What's Not Done

- `std/db`, `std/cache`, `std/http` standard library adapters
- Runtime budget enforcement (compile-time type validation is done; counter injection in `ai.prompt` calls is not)
- Non-blocking approval adapters (`SlackApproval`, `WebhookApproval`)
- `@bypass(reason, approved_by)` auditable escape hatch
- LLVM backend (the `Backend` interface is the extension point; Go is the only current implementation)
- `splash deploy` with capability manifest diffing
