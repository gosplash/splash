# Agent Experience in Splash

Proposed design. Splash should help coding agents produce correct, maintainable programs and give humans explicit evidence about what those programs can do. The authoring axis includes conventional software. The execution axis covers authority for programs that host agents. A typed control-plane and backend language is a focused starting point for both. [Repository assessment](repository-assessment.md)

The kickoff uses the [supported-subset reference](supported-subset.md) to start conventional authoring research alongside compiler work. The [architecture decision](../decisions/2026-11-26-analyzed-program-and-verification.md) establishes the common semantic foundation. Repair and preservation workflows remain goals for the first native vertical slice.

## Design principles

- **Measure the whole authoring loop.** Use wall time to accepted software, hidden correctness and regression tests, repair iterations, context and tool cost. Compiler latency explains part of that outcome.
- **Make semantic locality a design principle.** A local modification should require the smallest semantic context sufficient to make and verify it correctly. Given a function, an agent should be able to retrieve its signature, resolved dependencies, authority contract, applied constraints, relevant tests and source locations. Hidden conversions, implicit state, ambiguous dispatch, global mutation and context-dependent behavior warrant scrutiny because they can increase the context a correct change requires.
- **Give repair-sized feedback.** Point to the smallest responsible source location and explain the invariant with expected and actual values or a call path. Keep human and structured formats consistent. Let agents attempt ordinary repairs, rerun deterministic verification, then run tests. Changes to approval, classification, reachability, effect grants or trusted bindings need explicit review when they broaden authority.
- **Use a predictable executable subset.** Small, conventional constructs and a deterministic formatter can reduce search and patch churn. Explicit top-level contracts with local inference are a plausible balance. Prefer evidence from generation and maintenance tasks over guesses about a model’s preferences.
- **Make the review boundary legible.** Compare public API and authority facts from two reviewed scopes. Keep human reasons attached to sensitive declarations. Runtime choice of adapter and deployment policy belongs in the trust model. [Semantic interface](semantic-compiler-interface.md)

AX measures how efficiently agents produce correct changes. Developer experience (DX) includes how easily people read, maintain and review those changes. Trust depends on what the compiler and runtime can establish under their stated boundaries. Evaluate proposed features against all three. An explicit authority declaration gives agents an inspectable contract, gives people a reviewable grant, and gives the compiler an invariant to enforce. These benefits depend on implementing and validating the analysis. Token savings alone provide weak grounds for a language change.

## Semantic locality and context cost

The canonical analyzed program supplies the facts for both compiler decisions and agent queries. Expose information progressively. For a refund change, start with the signature and source reference, request the money type and approval contract, then fetch callers, effect witnesses or implementation details if the task needs them. Scope and omitted facts should be visible at each step. Existing languages expose semantic information too. Splash's hypothesis is that adding authority and effect facts to the same interface reduces the work needed for constrained changes.

| Metric | Measurement |
| --- | --- |
| Source tokens | Raw source delivered to the model, including snippets obtained through tools. |
| Semantic tokens | Compiler-derived payload delivered to the model, with comments and author notes attributable separately where possible. |
| Discovery calls | Queries, file reads, searches and similar actions used to establish task context. |
| Context expansion | Additional source or semantic tokens and calls after the first prescribed inspection. |
| Irrelevant context | Share of sampled context items outside a task-specific relevance rubric, with reviewer agreement recorded. |
| Context sufficiency | Accepted outcomes and follow-up contract questions under a fixed context budget, measured across paired trials. |

Report delivered tokens separately from bytes tools read locally. Compare progressive retrieval with a fixed summary and ordinary discovery under matched model, time and token limits. Test maintenance and refactoring tasks with different dependency depths. A smaller context is valuable when acceptance and regression outcomes hold. [TTCS protocol](time-to-correct-software.md)

## Feedback and workflow

A coding agent should be able to read a bounded module summary, query a contract and its callers, make a narrow change, check it, repair the primary diagnostic and exercise the actual behavior. A developer should see the source diff, focused test evidence and policy-relevant changes from the same snapshot. CI owns the release decision. An external agent may author a proposed authority grant, while the reviewer decides whether it belongs in the program.

For example, an agent adding validation before a customer refund would inspect the refund input type, callers and approval marker, keep the existing gate in place, correct a type mismatch at the relevant field, then run allowed, denied, invalid and regression cases. If the change expands the set of routes that can reach the refund, the review should show the old and new root relationships. The test runner checks behavior while the compiler checks defined source invariants.

### Repair protocol

1. The verifier reports a primary mismatch at the responsible expression, with expected `Money`, actual `Int`, and the related refund signature. It links to a compatible constructor where available and records evidenced follow-on errors as consequences. The agent confirms whether the input is measured in cents before editing.
2. The agent applies a change against the diagnostic's snapshot and reruns verification. For incomplete programs, the compiler returns an indeterminate verdict where evidence is missing and provides scoped facts for repair.
3. On a verified result, the agent compares requested invariants with the base snapshot and runs functional tests. The review can show that the requested authority facts and approval boundaries are unchanged within the compared scope. It reports functional test status independently.

Use causal IDs only when the compiler can identify the dependency. Preserve independent failures. Measure time, repeated diagnostics and inference cycles per accepted repair. [Diagnostic and comparison contracts](semantic-compiler-interface.md)

### Semantic regression checking

Let a task specify what to preserve, such as public function contracts, declared grants, inferred effects, agent reachability, approval boundaries, tool schemas or classified-data constraints at modeled sinks. Compare compiler facts from two verified, comparable snapshots. Report each requested property as unchanged, changed with a witness, or indeterminate when coverage is incomplete. A conventional service can use public contracts and effect summaries even without an agent entry. Functional tests continue to check behavior.

Bootstrap with a short, versioned reference derived from the implemented subset, canonical ordinary and policy-bearing examples, a diagnostic catalog, and an executable conformance set. Make syntax apparently accepted by parsing but unsupported later clear in the reference. A grammar or API export can be generated from authoritative data where the source permits it. A compact file in the repository can serve agents immediately. A docs command, site discovery index or structured standard library catalog becomes worthwhile after there are consumers and a source of facts to maintain.

## Language choices as experiments

| Choice | Hypothesis and measurement |
| --- | --- |
| Explicit function signatures and effects, local inference | Agents can predict inputs and impact from smaller views, with a higher edit count for changed authority. Remove full bodies from randomized summary conditions and measure contract questions, repair cycles and incorrect edits. |
| One canonical import idiom | Agents navigate better with module-qualified identity and recognizable import patterns. Compare equivalent task fixtures and inspect cross-module failure classes before proposing a migration. |
| Exhaustive matches, explicit optional and error handling | Sealed alternatives and local errors can catch missed cases earlier. Compare equivalent guard and match tasks after both lower correctly. Include missing variants, default branches and approval error paths in hidden tests. |
| Conventional concise syntax | Existing models may write conventional forms more reliably. Compare equivalent examples with equal documentation, models and tasks. Measure actionability, token cost and code-review errors alongside first-pass validity. |
| Predictable evaluation and resource ownership | Explicit evaluation/short-circuit rules, lifetimes and cancellation contracts may prevent failures that final types cannot show. Use effectful fallback and lifecycle tests before adding syntax. Structured concurrency needs task/adapter semantics and native conformance first. |
| Deterministic formatting and small standard interfaces | Stable formatting plus one documented pattern for each supported operation may reduce edits and ambiguity. Measure semantic versus formatting churn, source read volume and repair stability. |

Keep the initial syntax relatively stable while developing semantic identity, explicit contracts, effect analysis, repair feedback and change detection. Specify evaluation order, default arguments, optional lowering and error boundaries with concrete conformance cases. Separate parser acceptance, checked semantics and backend support. Existing source spells agent, tool and approval boundaries visibly. Changes to syntax need evidence of an authoring or review benefit, a rationale, examples and a compatibility or migration path.

## Deterministic infrastructure

The PRD’s adapter-first model can shorten the test loop if applications can exercise real contracts without credentials and provisioned services. Start with typed fixture input/output, an in-memory repository interface, deterministic IDs and a fake approval adapter for the policy demonstration. Add in-process HTTP/JSON only when a testable boundary contract exists. Use isolated state per test, a virtual clock for expiration, seeded pseudo-randomness for data generation, and fixed failure sequences for retries and deadlines. Use a fake queue and controlled scheduler for selected delivery/concurrency scenarios, with additional native tests for real concurrency behavior.

Favor contract tests shared by fake and real adapters, including cancellation, consistency, idempotency, errors and returned data scope. Gradually extend to database and HTTP contracts, file or object storage, queues, caching, config/auth, structured logging, metrics/tracing and lifecycle. Each brings independent capability declarations and trust decisions. Property tests help check balances, idempotence and valid state transitions. Test permissions should explicitly restrict side effects in the harness, especially when foreign Go code is present.

## Success criteria and boundaries

- The v1 interface reports a source range and stable code for every enrolled diagnostic and produces byte-identical canonical results for identical semantic inputs. A repeatable test covers compile failure, successful repair, and unsupported or partial analysis.
- In a randomized same-Splash comparison, structured feedback and summaries reduce capped time or input tokens on held-out maintenance tasks, with no meaningful loss of completion or increase in undetected policy violations. Pre-register effect and non-inferiority margins after the pilot. Record unfavorable results.
- Compare conventional and semantically equipped Go with the Splash conditions. If Go tooling achieves similar gains on conventional work, test whether integrated authority and semantic regression checks give Splash an additional advantage on matched constrained tasks. [Experimental conditions](time-to-correct-software.md#questions-and-comparison-conditions)
- On a conventional backend task, the agent implements and repairs a change, passes equivalent hidden tests and verifies a native executable without an embedded AI system. Report the cost against idiomatic Go.
- On a supported policy task, the tool surface, classified projection and approval behavior have native integration evidence. An unauthorized expansion is visible with a witness and the protected review path prevents an unapproved release.
- Measure check latency by real task size and phase before setting a p95 target. Record context delivered to the model separately from bytes read by local tools. Test comprehension of the contract and the stability of downstream edits. [Benchmark design](time-to-correct-software.md)

Initial non-goals include a general-purpose ecosystem, model inference inside the compiler, automatic authority-granting fixes, a proof of arbitrary behavior, immediate syntax redesign, and simultaneous CLI, daemon, LSP and MCP launches. Prioritize a narrow verified surface and a measurable authoring advantage.
