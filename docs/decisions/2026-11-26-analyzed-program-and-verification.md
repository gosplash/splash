# Use one analyzed program and a typed verification boundary

Date 2026-11-26. Architecture decision for the initial implementation. Language compatibility choices listed below require separate review.

## Context and Problem Statement

Splash targets typed backend and control-plane software. Agent Experience covers software written by agents, including conventional services. Authority by construction covers defined execution constraints. Both need the compiler to report the same facts it uses to check and lower source. The April proposal identified the shared representation, and the November audit found separate CLI pipelines and loss of typed import information. [April positioning memo](../superpowers/specs/2026-04-07-splash-language-positioning.md#L294) · [Repository assessment](../design/repository-assessment.md)

## Considered Options

- Add inspection commands over the flattened AST and per-command pipelines.
- Share an in-process analyzed program with a typed verification transition.
- Make a persistent semantic service and serialized graph the primary compiler interface.

## Decision Outcome

Use an immutable, in-process analyzed program with canonical module and declaration identity. Retain captured sources, typed modules, resolved calls, derived effects, policy evidence and coverage as they become available. All compiler consumers use those facts. An encapsulated `VerifiedProgram` will be required for artifact production under an explicit scope, ruleset and artifact purpose. `verified`, `rejected` and `indeterminate` preserve phase and property evidence. Source, analysis and artifact identities bind provenance. Protected reanalysis or authenticated verifier evidence establishes authenticity for enforcement.

Retain explicit declared authority and conservative inferred effects. The initial target checks both implementation conformance and caller coverage of every possible callee’s declared authority. This preserves the documented compositional contract and supersedes the earlier design assumption of declared-only effect information. Inferred-only caller compatibility remains research. A redline remains absolute within its defined semantics. Unsupported evidence cannot satisfy an artifact requirement.

Versioned JSON over the single-shot CLI is the first public semantic interface. Use progressive context and structured diagnostics for repair. Compare compatible verified evidence against explicit preservation contracts, with functional behavior graded independently. Defer persistence, incremental analysis, SDKs and additional transports until measured needs justify them.

Run soundness work alongside language-independent TTCS and CTCS preparation. Go is the first comparator, the five-task pilot is conventional, and research includes unsuccessful trials. Keep syntax stable unless evidence supports a change. The public ambition is “Designed for machines to write and humans to trust.” [Semantic interface](../design/semantic-compiler-interface.md) · [Implementation dependencies](../design/engineering-plan.md) · [TTCS](../design/time-to-correct-software.md)

### Consequences

- A caller may need to cover an unused grant on a callee. Inferred effects expose actual modeled use for review.
- The `emit` path now runs the current safety passes. The first analysis slice retains source and typed module ownership while graph construction and lowering consume the legacy projection.
- Before enforcing new language behavior, settle supported direct and qualified call forms, flat-import ambiguity and migration rules, effect label validation, `DB` refinement semantics, and how structural `Agent` participates. Specify trusted operation summaries and treatment of unresolved calls. Existing grammar and normative effect rules are unchanged by this iteration.
- Complete typed verification, stable symbol and provenance identities, and semantic comparison remain implementation milestones.
