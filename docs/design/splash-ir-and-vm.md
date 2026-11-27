# Splash IR and VM

Approved execution architecture with a deliberately small first implementation. Splash owns its executable semantics through a typed IR and VM, with native machine code as a later target. The compiler and initial VM are implemented in Go. A distributed compiler can run supported VM programs without invoking an external Go toolchain. Self-hosting is a separate project.

The earlier roadmap scoped out a custom runtime in favor of Go. The [execution architecture decision](../decisions/2026-11-26-own-execution-semantics.md) supersedes that constraint. Go transpilation remains the transitional execution target for existing commands and differential tests. Changing the default requires its own release decision. The [semantic compiler interface](semantic-compiler-interface.md) governs shared analysis and verification.

## Backend choices

| Approach | Relevance to Splash |
| --- | --- |
| Go transpilation | Existing bootstrap and comparison target. It supplies a mature toolchain and runtime. Splash lowering must translate any language semantics that differ from Go. |
| LLVM | Mature optimization and native code generation. Integrating its IR and runtime conventions would be substantial work before Splash has defined its execution model. [LLVM language reference](https://llvm.org/docs/LangRef.html) |
| Cranelift | Useful reference for an embeddable backend designed around fast code generation. Consider it when measuring native backend tradeoffs. [Cranelift](https://cranelift.dev/) |
| WebAssembly | Useful reference for typed validation, explicit imports and portable modules. Targeting it would require mapping Splash's runtime contracts onto a second execution model. [Validation](https://webassembly.github.io/spec/core/valid/index.html) · [Modules and imports](https://webassembly.github.io/spec/core/syntax/modules.html) |
| Lua | Useful reference for shipping a VM as an embeddable library and standalone program. Lua's register VM and dynamic value model serve different language semantics. [Lua reference manual](https://www.lua.org/manual/5.4/manual.html#1) |
| Splash VM, then native code | Choose this direction to own errors, resource accounting and host calls. Start with a stack machine and pay the costs of garbage collection, concurrency, native ABIs and deployment only as the language needs them. |

## Compiler migration

```mermaid
flowchart LR
    S[Captured Splash source] --> A[Shared semantic analysis]
    A --> G[Existing Go lowering]
    A --> L[VM subset checks and IR lowering]
    L --> I[Typed control-flow IR]
    I --> IV[IR validation]
    IV --> B[Stack bytecode]
    B --> V[Bytecode validation]
    V --> M[Splash VM]
    G --> O[Independent behavior oracle]
    M --> O
    I -. future .-> N[Native code generation]
```

At the reviewed #9 revision, the analyzed program already retains captured bytes, module-owned type results, named declaration references, and the call graph. Go emission consumes a flattened AST plus target-specific options. The existing `Backend.Emit` interface returns Go source. The CLI adapts the package name, writes a synthetic module and invokes `go build` itself. That source-emission interface is too narrow for a VM or an artifact that carries verification evidence. [Analyzed program](https://github.com/gosplash/splash/blob/f09ebd9b82961a8323afd2616831e2f2d23e3f07/cmd/splash/main.go#L98) · [Go backend interface](https://github.com/gosplash/splash/blob/f09ebd9b82961a8323afd2616831e2f2d23e3f07/internal/codegen/codegen.go#L18) · [Go build path](https://github.com/gosplash/splash/blob/f09ebd9b82961a8323afd2616831e2f2d23e3f07/cmd/splash/main.go#L242)

Add a parallel lowering path from the same captured analyzed program. Read module-owned typed declarations and call references directly, including alias-qualified calls. Keep Go-specific flattening and approval return-tuple rewriting on the Go path. A backend consumes the compiler's binding decisions and does not re-resolve calls from source names. Use deterministic program-local ordinals for function, record, field and slot operands. These can change between compilations. Cross-revision declaration identity belongs to the compiler and must remain distinct from ordinals and display names.

Compiler completeness is still a dependency for a policy-bearing target. Closures and match have incomplete traversal. The special `ai.prompt` path needs explicit external or unresolved evidence. The initial VM can reject these source forms while that work continues. The frontend also needs more type conformance before its current success alone can admit a program to a typed IR. The new lowerer must require established types and structural support for every accepted form, including the entry and captured dependencies. [Semantic compiler interface](semantic-compiler-interface.md#verified-artifact-production) · [Named-call implementation](https://github.com/gosplash/splash/pull/9)

## Typed control-flow IR

Keep semantic IR distinct from execution bytecode. IR functions have typed inputs and results, values and basic blocks with explicit terminators. The first slice needs only one block and void termination. Typed temporaries and local slots can grow with control flow and calls. Defer SSA, optimization passes and a general memory model. A separate IR validator checks the admitted structural and type invariants before backend lowering. As the IR grows, extend validation to control-flow targets, value definitions and uses, call contracts and terminators. The VM target lowers validated IR operations into stack instructions. Native lowering can consume the same semantic operations later.

The IR should be able to attach declaration identity, source range, and relevant verified policy evidence to operations. Keep calls, record construction and projection, branches, returns, and failure explicit. When the supported subset grows, add typed allocation, error edges, host operations and approval requests. Keep the security-relevant operation and its trust requirements explicit through lowering. Generated code and bytecode must preserve the evaluation and error behavior the compiler verified.

This sketch illustrates the future shape. The opcode names and integer rules are provisional.

```text
func billing.calculate_total(quantity: i64, price: i64) -> i64
entry:
  total = mul.checked.i64 quantity, price
  return total

func billing.issue_refund(request: RefundRequest) -> Refund
entry:
  approval = approval.request ledger.refund, request
  branch approval, approved, denied
approved:
  refund = host.call ledger.refund, request, approval
  return refund
denied:
  raise ApprovalDenied
```

Start with `String` and `Void` for the hello milestone. Add signed integer and boolean operations, typed calls and branches for recursion. Add immutable record construction and field projection for invoice. Evaluate arguments and field initializers once in source order. Require condition and operand types to be established. Keep mutation, general memory and optionals outside the first fixture set.

Propose a signed 64-bit `Int` with explicit checked-overflow and division-by-zero errors, division toward zero, no implicit numeric conversions, and left-to-right evaluation. Include the minimum-integer divided by minus one overflow case when integers are introduced. Use fixed-width operations so behavior can agree across supported architectures. This is a proposed source-language contract. VM-001 has no arithmetic instructions. Initial parity is restricted to values for which every intermediate fits the Go target's current integer type. All invoice inputs fit signed 32-bit intermediates, including the rounding offset. Review the language contract and implement consistent behavior before broadening parity claims.

## Stack VM and artifacts

Represent executable code as private in-memory stack instructions initially. VM admission validates bytecode independently of the IR, checking opcodes and operands, constant references, stack shape and termination for the initial subset. Add branch-target validity, stack types and heights at joins, initialized local reads, and call and return signatures as those instructions land. Resolve recursive signatures from the function table before checking function bodies. VM frames will carry arguments, local slots, an operand stack base and a return position. Instruction and frame limits apply from VM-001, where execution needs one entry frame. Recursion and loop cases will exercise exhaustion once those control transfers are available. Add fuzz tests for both validators as their input shapes grow, using malformed IR and bytecode fixtures as seeds. Report admission failure, invalid IR, invalid bytecode, execution errors and budget exhaustion distinctly. Preserve source locations where available and grow runtime diagnostics to include function identity, instruction and frame information through the compiler's provenance interface.

A later versioned `.spb` container can persist the validated program. Stage this after in-memory conformance. Keep canonical runtime types, constants, function signatures and code, explicit typed host imports, declared and derived authority where verified, debug locations, and provenance in bounded sections. Specify version/feature compatibility, deterministic encoding and separate debug data from runtime decisions. Validate the actual types, instructions and imports on load. An embedded authority manifest can describe requirements. It cannot establish that the loader should grant them. Policy-bearing distribution needs authenticated compiler evidence or trusted reanalysis bound to the exact artifact. Add malformed format, incompatible version and import mismatch cases before shipping a loader.

At first run VM conformance through an internal entry point. After it works, add an opt-in form such as `splash run main.splash --backend vm`. Preserve `splash check` as a source-analysis command. `splash test` and `splash trace` can follow deterministic adapters. `splash build main.splash --target vm` can follow a supported bytecode format. Native targets such as `arm64-macos` and `x86_64-linux` are later product decisions. They require an ABI, runtime and artifact-format strategy and independent target conformance.

## Admission, authority and runtime evidence

VM-001 admits a single unannotated module without imports, containing one zero-argument `main` with an implicit void result and a sequence of resolved builtin `println` calls on string literals. Termination is implicit. Execution uses one entry frame, an injected output hook and deterministic count and depth limits. The internal entry point runs the existing source checks and validates admitted source forms explicitly before lowering. Reject unknown semantic facts, unresolved calls, unsupported syntax, and policy-bearing annotations or operations. A missing graph edge cannot establish that an operation is absent. A rejected VM input yields a diagnostic without silently changing targets.

For a later capability-aware runtime, an artifact-specific `VerifiedProgram` establishes what a program may request under the compiler's rules. It does not grant concrete host access. Instantiate each run with a host table containing only explicitly granted adapters, matched to verified imports and operation contracts. Resource identifiers, destination rules, credentials and request-specific approval belong to trusted adapter configuration. Avoid ambient filesystem, network, process or provider access.

| Runtime concern | Design requirement for the later policy-bearing runtime |
| --- | --- |
| Resource accounting | Meter instructions and frames deterministically. Account for language allocations separately from the host process's actual memory. Enforce deadlines at dispatch and propagate cancellation into host operations. Use OS isolation for untrusted candidates. |
| Host authority | Check operation grants and resource restrictions at invocation. Require the adapter to enforce destination and resource scope in its own external calls. |
| Approval | Bind the approval to the exact operation, normalized request, principal and invocation context. Define expiry, replay and denial behavior. The host operation validates the approval before performing the mutation. |
| External costs | Propagate explicit limits to the provider where supported. Charge from trusted usage evidence and specify how estimated, unknown and exceeded costs affect the call. |
| Observability | Capture deterministic sequence numbers and source-correlated call, branch, outcome and authority events. Keep classified values and credentials out of traces by default. Treat richer value tracing as separately authorized debugging. |

The runtime and bound adapters remain trusted. Source verification establishes modeled language and policy properties. Independent IR and bytecode validation establishes structural properties at each representation. Runtime grants and controls establish what this invocation can access and spend. TTCS candidates still require the protected grader and process isolation. [Verification completeness](semantic-compiler-interface.md#commands-and-status) · [TTCS isolation design](ttcs-harness.md#isolation-and-run-lifecycle)

Make deterministic execution under deterministic host inputs a design objective. Use a virtual clock, seeded randomness, in-memory state, scripted HTTP and AI responses, and request-bound test approval. A controlled scheduler can follow defined concurrency semantics. Capture reproducible execution events and consider replay when the host contracts define what must be recorded. Runtime evidence should link back to static contracts for repair without exposing classified payloads or secrets.

## Cross-platform execution and contracts

Treat portable business logic and data contracts as IR requirements. Preserve fixed-width values, explicit typed record layouts and structured errors. Keep backend storage layouts separate from any published ABI or wire layout. Export identity, value ownership and lifetimes, cancellation and async behavior, and compatibility checks need explicit contracts before a target exposes them.

| Capability | Purpose and boundary |
| --- | --- |
| Local embedding and bindings | Execute the same Splash implementation through host-language interfaces. An embedded VM can come first for Swift and Kotlin. Ahead-of-time native libraries and a WebAssembly web target can follow shared conformance. |
| Remote SDK generation | Derive typed requests, responses, serialization, validation and error contracts for a remote Splash service. Clients have no local implementation or server authority by virtue of the SDK. |
| Source transpilation | Consider readable Swift, Kotlin or TypeScript for a supported portable subset. Preserve evaluation and error semantics through independent conformance before expanding the subset. |

Target deployment policies describe which effects a deployment admits. Export admission must check transitive authority against that policy once compiler evidence supports the check. Runtime host grants still define the concrete operations for a particular execution. Client-side validation can give fast feedback. The server independently validates inputs and authorizes operations. VM-001 introduces no export syntax, binding generator, SDK, source target or target policy enforcement.

## Acceptance and rollout

| Milestone | Implementation | Independent evidence |
| --- | --- | --- |
| 1. Hello world | Lower a tiny source program through one typed IR block and a handful of stack instructions. | The same source prints `Hello, world` in Go and the VM. The validator rejects a wrong stack shape and the backend rejects an unsupported source form before dispatch. |
| 2. Recursion | Add integers, booleans, control flow and typed call frames. | Factorial or summation matches independent expected values, including the base case. Test arity, frame isolation and deterministic frame-limit exhaustion. |
| 3. Invoice | Add qualified imports, records, field projection and the small arithmetic set needed by the existing probe. | The exact same Splash driver and source passes all nine cases through Go and VM. Both execute the truncating starter and fail on the half-cent case. The Go-language comparator stays independently graded. [Invoice fixture](https://github.com/gosplash/splash/pull/10) |

Keep the hello fixture small enough to isolate entry, constant, output and return. For invoice, add a VM execution option to the Splash grader adapter and preserve the existing inputs and oracle. Differential results help find backend drift. Independent expected behavior and the specified semantics decide correctness when backends disagree.

The Go reference path at `f09ebd9` passed native smoke checks for literal hello and factorial. It printed `Hello, world`, then `1` and `120` for factorial inputs 0 and 5. The run used Go 1.26.3 on darwin/arm64. These are Go-side baselines for the first two milestones.

Record native Go from Go source, Splash through Go transpilation, and the same Splash source through Splash VM as separate conditions. Compare the two Splash paths on identical inputs to examine backend effects without changing the authoring language. Measure frontend analysis, lowering, validation, external build where applicable, startup, execution, memory and total edit-to-test latency separately. Keep these measurements distinct from TTCS, which measures end-to-end production of independently accepted solutions.

After the fixture milestones, expand the executable subset with optionals, typed errors and an explicit allocation model. Then introduce host capabilities, deterministic adapters and execution tracing. Run the conventional inventory demonstration before the policy-bearing support demonstration. The latter requires effect and call coverage, native approval and denial behavior, trustworthy artifact admission, and audited host integrations. Measure analysis, IR lowering, validation, cold startup, execution, and repair loops separately before claiming a performance or AX improvement.

Treat changing the default away from Go as a release decision. Require supported-application parity, documented behavior on boundary inputs, debugger and error-reporting readiness, packaging and migration instructions. Once execution semantics and the runtime ABI stabilize, evaluate the first native target. A Splash compiler written in Splash comes later, after the language can express and maintain the toolchain and an independent bootstrap can verify it.
