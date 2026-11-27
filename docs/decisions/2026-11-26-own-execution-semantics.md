# Own Splash's executable semantics through typed IR and a stack VM

Status: accepted direction, experimental implementation. Date: 2026-11-26.

## Context and Problem Statement

Go transpilation bootstrapped a running compiler and provides a mature comparison target. Splash needs to define execution semantics independently so that source contracts, errors and authority remain consistent across its execution targets. The canonical analyzed program supplies shared semantic facts to both the Go backend and the proposed VM.

## Considered Options

- Continue defining execution through Go transpilation.
- Adopt an external execution or native lowering model as the primary target.
- Define Splash's executable semantics in typed IR and implement a validated stack VM.

## Decision Outcome

Use backend-independent typed control-flow IR and distinct executable stack bytecode. Validate each representation independently. Lower both from captured, module-owned analysis and resolved references through the applicable backend path. Compiler verification, structural validation and runtime grants have separate responsibilities.

Own the executable semantics and work toward a Splash execution target becoming the default. Keep the compiler and first VM in Go. Go transpilation remains the current default and a differential comparison path through migration. Retiring that default requires a separate release decision with independent conformance, platform and tooling readiness, and a migration path.

Design for portable shared business logic and contracts. Keep local execution bindings, remote service SDKs and source transpilation separate. Plan explicit target policies and host interfaces so portability can preserve modeled authority constraints. These interfaces and code generators are deferred.

### Consequences

- VM-001 covers only the admitted literal-output entry program. It adds in-memory execution and validators while leaving the current CLI unchanged.
- Recursion and invoice conformance are later milestones. The signed 64-bit checked integer contract is proposed pending language review and consistent backend implementation.
- A serialized bytecode format, host grants, deterministic adapters, target bindings, native code generation and self-hosting remain future work. Isolated execution is still required for untrusted authoring trials.

## More Information

[Execution design, admission and milestones](../design/splash-ir-and-vm.md) · [Canonical analyzed program decision](2026-11-26-analyzed-program-and-verification.md)
