# Splash Language Positioning Memo

**Date:** 2026-04-07
**Status:** Draft
**Author:** Codex + Zach Graves
**Scope:** Product and language direction for Splash as a specialized language for agent-built software

---

## Thesis

Splash should not try to compete as a broad general-purpose language.

Splash should become the best language for writing software whose authority must be statically understood before it runs, especially software built by and with AI agents.

That means the project should optimize for:

1. Whole-program analyzability
2. Explicit capability boundaries
3. Auditable behavior
4. Safe agent operation
5. A narrow but powerful control-plane domain

This is a smaller ambition than "replace Go" and a much more defensible one.

---

## Product Position

Splash is a control-plane language.

It is for:

- agent entrypoints
- tool surfaces
- approvals and review gates
- orchestrating model calls
- policy-aware data access
- capability-constrained service logic
- workflows where call paths and authority matter more than raw expressiveness

It is not primarily for:

- frontend/UI work
- systems programming
- high-performance numerics
- general scripting
- large framework ecosystems
- applications whose main value comes from runtime dynamism

The goal is not to be "good enough for everything."
The goal is to be obviously better for one high-value class of software.

The target domain starts with AI agent control planes, because that is where the pressure is highest today.

But the primitives are more general than LLMs. Any system with autonomous components that need statically-verified authority boundaries fits the same model:

- model-driven agents
- financial automation
- medical and clinical workflows
- industrial control software
- robotic and vehicle autonomy

The narrowness is about the kind of software, not about whether the autonomous component happens to be a language model.

---

## Why Now

The timing matters.

AI systems are now capable enough to write code, review code, find security weaknesses, and exploit mistakes in software that would previously have survived for years. That changes the economics of software safety.

Post-hoc scanning, red-teaming, and runtime monitoring are becoming mandatory. But they are not enough for the highest-stakes control software. If the language gives the compiler no way to understand authority, reachability, approval boundaries, and data classification, then the most important guarantees still depend on review process and operational vigilance.

Splash is a response to that shift.

It is an attempt to move a critical subset of software into a world where authority and safety constraints are enforced before the program runs, not merely checked after the fact.

---

## Design Consequences

If the product position above is real, it should constrain the language.

### 1. Whole-program analysis is the product

The center of Splash is not syntax and not codegen. It is the compiler's ability to answer:

- What can this program do?
- What can this agent reach?
- Which functions require approval?
- Which tools are exposed?
- Which data classifications can flow to which sinks?

This implies:

- `splash check` is the primary command
- multi-file semantics must be first-class, not incidental
- every later phase should consume one canonical analyzed program
- new language features should be rejected if they break whole-program reasoning

### 2. Explicitness beats convenience

Splash should choose visible structure over terse magic.

This implies:

- explicit imports
- explicit exports
- explicit effect declarations
- explicit approval boundaries
- explicit data classification
- predictable name resolution

This argues against:

- hidden re-exports
- macro systems
- dynamic code loading
- import-time magic
- clever control-flow sugar that obscures reachability

### 3. Authority must be structural

Agent capability and human approval should not feel like optional decorative annotations bolted on after the fact.

Over time, the language should move toward first-class forms for concepts such as:

- agent entrypoints
- tools
- approval-gated functions
- module containment policy
- model/tool interaction

Even if annotations remain in the short term, the semantics should be treated as foundational.

### 4. Auditability is a first-order feature

Splash programs should be easy for a human reviewer, compliance team, or security engineer to inspect.

This implies:

- generated artifacts should be readable
- compiler diagnostics should explain why a path is reachable or disallowed
- the CLI should expose call graphs, effect summaries, approval surfaces, and tool surfaces directly
- the language should bias toward boring, grep-friendly source structure

### 5. The language should remain intentionally small

Small is not a temporary state. Small is part of the value proposition.

The standard for adding a feature should be:

"Does this materially improve Splash as a language for statically-governed agent software?"

Not:

"Would this make Splash more like a general-purpose language?"

---

## Concrete Direction Changes

These are not abstract preferences. They imply specific design choices.

### Keep

- Effect declarations in function signatures
- Compile-time safety enforcement
- Tool schemas derived from signatures
- Data classification as part of the type story
- Readable generated Go
- The call graph as a core compiler concept
- `@approve`, `@containment`, `@sandbox`, and related authority controls

### Change

- Treat multi-file and whole-program analysis as canonical infrastructure, not CLI glue
- Strengthen the module system toward explicit imports/exports and less namespace injection
- Improve CLI visibility into compiler knowledge
- Tighten the line between "language semantics" and "organization-specific policy"
- Consider upgrading the most fundamental annotations into dedicated syntax over time

### Reject

- Macro systems
- Implicit capability acquisition
- Runtime reflection-heavy core semantics
- Dynamic module loading
- Feature growth aimed mainly at general-purpose completeness
- Syntax churn that does not improve analyzability or auditability

---

## Language Questions To Resolve

The following questions are strategic, not cosmetic.

### Q1. Are foundational concepts staying as annotations?

Current examples:

- `@tool`
- `@approve`
- `@containment`
- `@sandbox`

Long term, Splash should decide whether these stay as annotations or become declaration forms or keywords.

A plausible direction:

```splash
tool fn search(query: String) needs DB.read -> SearchResult { ... }
approve fn charge_card(id: Int, amount: Int) needs Net -> Charge { ... }
agent fn run_support() needs Agent, DB.read, AI -> Reply { ... }
```

Why this matters:

- easier to read
- easier to teach
- clearer parser and AST semantics
- less risk of foundational ideas feeling optional

### Q2. Should imports remain flat?

Current behavior injects imported symbols into the local namespace.

That is convenient, but it weakens readability at scale.

A stricter direction would be:

- explicit exports only
- explicit imports only
- namespaced references by default
- optional explicit local aliasing

Example:

```splash
use billing

fn run() needs Net -> billing.Charge {
    return billing.charge_customer(1, 100)
}
```

Why this matters:

- clearer ownership
- clearer grepability
- fewer hidden edges in whole-program reasoning

### Q3. What belongs in the language vs policy layer?

Some guarantees belong in core language semantics:

- effect checking
- approval propagation
- containment
- tool extraction
- classification-aware type checks

Some rules are likely policy:

- whether a given team may expose a tool returning internal data
- whether a given module may call a specific external service
- environment-specific deployment rules

Splash should eventually separate:

- language-enforced invariants
- repo or org policy enforced by config

### Q4. How far should data classification go?

Today classification mostly gates certain type uses.

A stronger version would add:

- classification-aware sink checks
- explicit declassification/redaction operations
- clearer integration with tools, model prompts, storage, and logs

This is likely one of Splash's strongest long-term differentiators if it remains statically understandable.

---

## Near-Term Implementation Priorities

These are the next concrete bets that align with the thesis.

### 1. Build a canonical analyzed program representation

One driver should produce the full program once:

- parsed
- typechecked
- imports resolved
- declarations merged or linked
- call graph built
- safety facts attached

That representation should feed:

- `check`
- `build`
- `emit`
- `tools`
- future graph/debug commands

Reason:

This removes an entire class of correctness bugs and makes whole-program analysis architectural rather than conventional.

### 2. Add CLI-level golden tests

Focus on end-to-end behavior, not just package-local tests.

Must-cover cases:

- multi-file safety
- imported approval propagation
- imported tool extraction
- containment across modules
- classification across tool boundaries
- generated Go shape for approval and AI runtime paths

Reason:

Splash's promises are cross-phase promises. Those need cross-phase tests.

### 3. Add compiler introspection commands

Candidates:

- `splash graph`
- `splash effects`
- `splash approvals`
- `splash explain <symbol>`

Reason:

If whole-program understanding is the value, users should be able to inspect it directly rather than infer it from failures.

### 4. Tighten the module system

Move toward:

- explicit exports
- explicit imports
- namespaced references or explicit aliasing
- clearer duplicate/conflict rules

Reason:

Whole-program reasoning gets weaker as namespace behavior gets looser.

### 5. Improve diagnostics substantially

Best-in-class diagnostics should include:

- exact path from root to violation
- exact missing effect or forbidden sink
- exact imported declaration involved
- suggested fix when obvious

Reason:

For Splash, diagnostics are product surface, not implementation detail.

---

## Medium-Term Roadmap Filters

When evaluating proposals, use these filters in order:

### Filter 1: Does this strengthen static authority reasoning?

If no, reject or defer.

### Filter 2: Does this improve the target domain?

The target domain is agent-built control-plane software, not software in general.

If the main benefit is broad language completeness, reject or defer.

### Filter 3: Can the compiler explain it?

If a feature cannot produce clear diagnostics and inspectable consequences, it is a bad fit.

### Filter 4: Will the generated/runtime story stay readable?

If a feature forces a large amount of invisible runtime machinery, be skeptical.

### Filter 5: Does this keep Splash small?

The language should accumulate power faster than it accumulates surface area.

---

## Example Of The Desired User Experience

A strong Splash workflow should feel like this:

1. A developer writes an agent entrypoint, a few tools, and a data type.
2. The compiler can immediately answer what that agent can reach, what effects it requires, and whether any approval or containment rule is violated.
3. Tool schemas are emitted automatically from the same source.
4. If the program is rejected, the compiler shows the exact path and reason.
5. The generated runtime remains understandable enough that a human can audit it.

This is a fundamentally different experience from "write code and hope the runtime/policy stack does the right thing."

---

## Anti-Goals

Splash should not try to be:

- a JavaScript replacement
- a Go replacement
- a systems language
- a dynamic scripting language
- a framework platform for arbitrary app categories
- a research language optimized for novel syntax

It should be a sharp language for statically-governed agent software.

---

## Working North Star

Use this sentence to evaluate future design work:

> Splash is the best language for writing software whose authority must be statically understood before it runs.

And this sentence to evaluate feature proposals:

> If a feature makes Splash more general but less analyzable, it is probably the wrong feature.

---

## Open Strategic Tension

There is one tension worth naming explicitly.

Splash still needs to be pleasant enough to use that it does not feel like a policy DSL disguised as a language.

That means the project should still care about:

- readable syntax
- good data modeling
- reasonable ergonomics
- concise but explicit source

But ergonomics should be in service of analyzable power, not in tension with it.

That is the line to hold.
