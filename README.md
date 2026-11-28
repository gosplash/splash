# 🏄‍♂️ Splash

**Designed for machines to write and humans to trust.**

Splash is an early statically typed language for backend and control-plane software. It compiles to Go, and an experimental VM executes a subset directly. Splash is designed around explicit contracts, fast deterministic feedback and verifiable authority boundaries. Coding agents can write conventional Splash software. Software that runs agents can also declare tool, reachability and approval boundaries.

## Implementation status

| Behavior | Evidence |
| --- | --- |
| Native Go backend for the documented pilot subset | Implemented and integration-tested on the reference examples. [Supported subset](docs/design/supported-subset.md) |
| Experimental VM execution | `splash run <file.splash> --backend vm` runs typed business logic from a standalone Splash binary. [VM subset and limits](docs/design/splash-ir-and-vm.md#public-vm-execution) |
| Named-call redline checking and CLI artifact rejection | Uses resolved direct and qualified named calls. Unresolved indirect calls are rejected in agent- and sandbox-reachable code and in programs with approval gates. |
| Declared caller–callee effect compatibility | Enforced for resolved direct and qualified calls visited by the type checker. Unknown labels in `needs` and `@sandbox` effect lists are rejected. |
| Inferred effects and coverage | `splash effects` reports declared grants, modeled effects and coverage separately. The compiler propagates modeled effects through resolved calls, including recursion, and rejects known effects outside declared authority. The first external contract covers `std/ai.prompt`. |
| Required policy coverage | `check`, `emit`, `build`, `tools`, and VM `run` report indeterminate verification when required source coverage is missing. The shared gate preserves known violations and leaves effect inspection available. |
| Approval and classification | Implemented for selected checks and lowering forms. Request-bound approval and general classified-data flow enforcement are proposed. |
| Verified source artifact boundary | The compiler requires an encapsulated `VerifiedProgram` for Go emission, VM lowering, and tool export. It authorizes the captured source under the shared policy checks. |
| Structured check diagnostics | `splash check <file.splash> --json` emits a versioned source verdict with diagnostic codes, source locations, and resolved effect witnesses. [JSON report v1](docs/design/semantic-compiler-interface.md#check-json-v1) |
| Function contract inspection | `splash inspect <file.splash> <module.function> --json` reports a signature, declared authority, inferred effects, and effect coverage. [Inspect JSON v1](docs/design/semantic-compiler-interface.md#inspect-json-v1) |
| Dependency queries and authority comparisons | Proposed. Graph, effect, and approval inspection use detached projections of the analyzed program. |
| TTCS and cost evaluation | Experimental methodology. Comparative authoring results are pending. |

See the [repository assessment](docs/design/repository-assessment.md) for scoped evidence and the [status vocabulary](docs/design/product-and-roadmap.md#public-implementation-status) for how to read these labels.

Core authority concepts now have first-class syntax:

- `tool fn` for AI-callable tools
- `approve fn` for human-gated operations
- `agent fn` for agent entrypoints

`splash tools` emits argument JSON Schema for tool declarations. `splash check` evaluates the current static checks. Use `splash build` and run the program to verify native behavior.

---

## Quick Start

```bash
go build ./cmd/splash/...          # build the compiler

./splash check examples/effects/effects.splash
./splash check examples/sandbox/sandbox.splash
./splash check examples/approval/approval.splash
./splash inspect examples/hello/hello.splash hello.main --json

./splash build examples/hello/hello.splash -o hello && ./hello
./splash emit  examples/approval/approval.splash   # print generated Go
./splash tools examples/agent_tools/agent_tools.splash  # print JSON Schema
./splash graph examples/finance/finance.splash     # print agent roots + call edges
./splash effects examples/finance/finance.splash   # print function effect surfaces
./splash approvals examples/approval/approval.splash  # print approval-gated paths
```

For the experimental VM, save this program as `main.splash`.

```splash
module invoice

fn total(quantity: Int, unit_cents: Int) -> Int {
    return quantity * unit_cents
}

fn main() {
    println(total(4, 2500))
}
```

Run `./splash run main.splash --backend vm` to print `10000`. Running with an existing Splash binary requires no Go installation. The default limits are 1,000,000 dispatched instructions and 256 active frames. Run `splash run --help` for the limit flags.

Use OS-level process isolation for untrusted source. The VM limits bound instruction dispatch and active frames within the Splash process. [Public VM execution](docs/design/splash-ir-and-vm.md#public-vm-execution)

All tests:

```bash
go test ./...
```

---

## Repository Layout

```
cmd/splash/                 CLI: check / run / build / emit / tools / graph / effects / approvals
internal/
  lexer/                    tokenizer
  parser/                   Pratt parser → AST
  ast/                      AST node types, annotation kinds
  effects/                  EffectSet bitmask, Parse(), String()
  typechecker/              type inference, constraint checking, use-imports
  callgraph/                call graph, Reachable(), Callers(), Parents(), PathTo()
  safety/                   redline fn, approve fn, @containment, @sandbox, @budget, @sensitive
  compiler/                 captured analysis, verification, and artifact APIs
    internal/codegen/       private Go emitter
    internal/lowering/      private Splash source to VM IR lowering
    internal/toolschema/    private tool schema generation
  ir/                       typed VM intermediate representation
  bytecode/                 lowering and validation of VM instructions
  vm/                       execution and runtime limits
  diagnostic/               Diagnostic type (Error/Warning + position)
  types/                    data classification lattice
examples/                   native and illustrative Splash examples
docs/superpowers/plans/     implementation plans (historical)
WHITEPAPER.md               design rationale and formal model
```

---

## Compilation Pipeline

```
source (.splash)
  → Lexer        tokenize
  → Parser       produce *ast.File
  → TypeChecker  type inference and selected constraint checks
  → CallGraph    build directed call graph, compute agent roots + reachability
  → SafetyChecker  redline fn, approve fn, @containment, @sandbox, @budget, @sensitive/@restricted
  → VerifiedProgram
      → Go source → go build
      → VM admission → IR → validated bytecode → execution
      → tool manifest
```

The CLI shares analysis and verification across `check`, `emit`, `build`, `tools`, and VM `run`. A verified handle stays bound to the captured source. Native compilation and VM admission can still fail after source verification succeeds.

---

## Effect System

Effects are declared in function signatures with `needs`. A resolved caller must cover the callee’s declared capabilities, including unused grants. `splash effects` separately shows effects inferred from modeled operations and resolved calls. Incomplete coverage preserves known effects and withholds unused-grant findings. The existing graph-based policy checks continue to use declared labels. [Effect checking](SPEC.md#53-effect-checking)

```splash
fn fetch(id: Int) needs DB.read -> Report { ... }
fn summarize(id: Int) needs DB.read, AI -> String { return fetch(id) }
// Under the specified rule a caller of summarize must declare DB.read and AI.
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
| `tool fn` | function | marks a tool candidate for argument schema export |
| `redline fn` | function | safety check rejects a modeled agent path to this function |
| `approve fn` | function | injects human approval gate; emits `(T, error)` Go signature |
| `@reason "..."` | declaration | attaches human-readable justification metadata to the next declaration |
| `@agent_allowed` | function | exempts from `@containment(agent: "approved_only")` check |
| `@containment(agent: "none"\|"read_only"\|"approved_only")` | module | evaluates entry-module policy over the modeled graph |
| `@sandbox(allow: [...], deny: [...])` | function | checks declared effects on modeled reachable functions |
| `@budget(max_cost: Float, max_calls: Int)` | function | declares resource limits; types validated at compile time |
| `@sensitive` | field | field is PII; containing type cannot satisfy `Loggable`; `tool fn` cannot return it |
| `@restricted` | field | highest classification level for supported checks |
| `@internal` | field | field is internal-only; affects classification lattice |

---

## Data Classification

Classification is a property of types, not values. Four levels form a lattice:

```
public < @internal < @sensitive < @restricted
```

The current checks cover selected type and sink shapes:

1. Tool results with supported named and optional-named record types are checked for directly annotated fields above `@internal`.
2. `println` and supported `Loggable` calls check argument type classification. Projected fields currently use the underlying field type.

---

## `splash tools` Output

Example argument schema from `splash tools <file.splash> --format anthropic`:

```json
[
  {
    "name": "search_catalog",
    "description": "Search the catalog for items matching a query.",
    "input_schema": {
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

`use billing` resolves to `billing.splash` in the same directory. Cycles are detected and rejected. The `expose` list controls what is injected into the importing namespace. If two imported modules export the same function name, use qualified calls such as `left.price()` and `right.price()`. A local binding shadows an imported or global function in an unqualified call. The compiler diagnoses ambiguous flat function references. Types still need distinct names in the flattened Go package. An approval-bearing call must be a standalone statement or the whole value of a `let` or `return`. Programs with approval gates reject unresolved indirect calls conservatively, since the compiler cannot establish which ones need the approval error channel. `splash emit` and `splash build` merge declarations into a single Go package.

---

## `approve fn` Runtime

`approve fn` on a function does three things at compile time:

1. The function's Go return type is widened to `(T, error)`.
2. `splashApprove("fn_name")` is injected as the first statement of the body.
3. Modeled transitive callers gain error propagation for supported direct call forms.

```go
// Generated Go for approve fn charge_card(...) needs Net -> Charge
func chargeCard(customerID int, amountCents int) (Charge, error) {
    if err := splashApprove("charge_card"); err != nil {
        return Charge{}, err
    }
    // ... body
}
```

The default `ApprovalAdapter` prompts on stdin. Generated Go accepts a custom adapter through `SetApprovalAdapter`. A production webhook adapter and request-bound approval remain proposed.

---

## Package Contracts

**`internal/callgraph`**
- `Build(f *ast.File) *Graph` — constructs the call graph
- `g.AgentRoots() []string` — functions with `needs Agent` or `tool fn`
- `g.Reachable(roots []string) map[string]bool` — forward BFS
- `g.Callers(targets map[string]bool) map[string]bool` — reverse BFS (used for `approve fn` cascade)
- `g.Parents(roots []string) map[string]string` — BFS parent map for path reconstruction
- `callgraph.PathTo(parents, target)` — reconstruct call path for error messages

**`internal/safety`**
- `New().Check(modules, graph, effects)` returns known violations and missing required source evidence.
- Add a new safety pass by adding a `checkFoo` method and calling it from `Check`

**`internal/compiler`**
- `Analyze(path)` captures source and resolved semantic facts.
- `Verify(analyzed)` issues a `VerifiedProgram` after the required source checks pass.
- Artifact entry points accept only that handle. The Go emitter, VM lowerer, and tool schema generator are private dependencies.

**`internal/typechecker`**
- `tc.SetFileLoader(baseDir, readFileFn)` — enables `use` import resolution
- `tc.LoadedTypedFiles() []*TypedFile` — typed imported modules in dependency order
- `tc.Check(f) (*TypedFile, []diagnostic.Diagnostic)`

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
| `effects/` | `needs` declarations, effect propagation, `redline fn`, `approve fn` |
| `data_safety/` | `@sensitive`, `@restricted`, data classification |
| `containment/` | `@containment` module policy |
| `agent_tools/` | `tool fn`, `redline fn` on dangerous operations, agent entry point |
| `ai_prompt/` | Frontend `std/ai` and `ai.prompt<T>` examples |
| `approval/` | `approve fn` runtime, `ApprovalAdapter` |
| `multi_file/` | `use module`, cross-file types, `splash emit` merging |
| `sandbox/` | `@sandbox` effect constraints, `@budget` resource limits |

---

## Performance

`splash check` runs parsing, type checking, call graph construction and the implemented safety passes. The April whitepaper reported these Apple M2 timings for an in-process synthetic chain fixture:

| Chain size | Reported in-process check |
|---|---|
| 100 functions | 218 µs |
| 500 functions | 1.06 ms |
| 2,000 functions | 6.24 ms |

The fixture includes file reads and excludes CLI startup, native compilation and functional tests. The [assessment](docs/design/repository-assessment.md#validation-and-performance) records later Apple M5 measurements and the limits of both methods.

Run benchmarks: `go test -bench=. ./internal/callgraph/... ./internal/typechecker/... ./cmd/splash/...`

---

## What's Not Done

- `std/db`, `std/cache`, `std/http` standard library adapters
- Runtime budget enforcement (compile-time type validation is done; counter injection in `ai.prompt` calls is not)
- Non-blocking approval adapters (`SlackApproval`, `WebhookApproval`)
- `@bypass(reason, approved_by)` auditable escape hatch
- Splash VM support beyond the experimental literal-output subset, along with native and portable deployment targets. [Execution design](docs/design/splash-ir-and-vm.md)
- `splash deploy` with capability manifest diffing

## License

Splash is licensed under the [MIT License](LICENSE).

The Go toolchain and standard library are licensed under the [BSD 3-Clause License](https://github.com/golang/go/blob/master/LICENSE).
