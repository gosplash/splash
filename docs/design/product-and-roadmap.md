# Proposed product and roadmap evolution

Review draft for PRD, roadmap, README, specification, website and whitepaper changes. Public messaging establishes why Splash exists. Adoption depends on what a developer can reproduce. Engineering and research milestones establish the evidence for each public claim. The [repository assessment](repository-assessment.md) records current evidence and the [engineering plan](engineering-plan.md) sequences the work.

The kickoff adopts the headline and corrects the README and website introduction. The broader adoption rollout and successor whitepaper remain proposals. The original April whitepaper text is preserved. [Architecture decision](../decisions/2026-11-26-analyzed-program-and-verification.md)

## Product direction

Suggested website introduction:

> An agent-native programming language
>
> **Designed for machines to write and humans to trust.**
>
> Splash is a statically typed programming language designed around explicit contracts, fast compiler feedback, and verifiable authority boundaries.
>
> One language for software written by agents and software that runs them.
>
> **Built for authorship.** Help coding agents understand, implement, repair and verify software.
>
> **Built for trust.** Make software authority explicit, inspectable and enforceable.
>
> Splash is an early-stage open-source language. See implementation status for supported guarantees and limitations.

Keep the vision in the headline, the two use cases beside it and a visible implementation-status link alongside the quick start. Link the Docs, Specification and GitHub from the introduction. Lead the README with the same distinction.

| | AI as software author | AI as software participant |
| --- | --- | --- |
| Example | Codex writes and modifies an inventory service. | A support agent recommends a refund or requests an approval-gated refund operation. |
| Embedded model | None required. | Typically present. |
| Splash's contribution | Comprehension, correctness and verification of a change. | Explicit authority, tool exposure, reachability and approval. |
| Evidence of success | TTCS on matched tasks with independently accepted behavior. | Independent policy conformance and native behavior within the defined trust model. |

Splash initially targets typed backend and control-plane software. Go is the initial comparator for that workload. Rust and the discussion it prompted provide historical context. The long-term reason for Splash is the growing role of machine authorship and execution under human responsibility. AX expresses the authorship goal, and authority by construction expresses the execution goal within modeled boundaries. The semantic compiler supports both, with TTCS and conformance supplying distinct evidence. [April memo](../superpowers/specs/2026-04-07-splash-language-positioning.md#L82)

| Audience | Message and evidence to show |
| --- | --- |
| Coding agents and developers | Quick edit/check/repair loop, precise contracts and canonical examples. Show a real diagnostic and a conventional change with hidden tests once available. |
| Engineering organizations | Review compiler-derived changes to public contracts and authority. Show a checked revision, native test results and a protected review decision tied to that revision. |
| Security and compliance teams | Enumerate the checks the compiler actually ran, which approval and runtime controls exist, and which adapters or deployment components are trusted. Link to supported scope and native evidence. |
| Language designers and researchers | State the hypotheses behind AX. Publish TTCS protocol, match conditions, task eligibility and uncertainty. Treat compiler-to-agent feedback as an experimental interface. |

## Public implementation status

Use one vocabulary in the README, current website, roadmap, specification notes and examples. Apply labels to a precise behavior and scope, with the revision or release and links to evidence. Multiple labels can apply.

| Label | Meaning |
| --- | --- |
| Specified | Normative language behavior has been defined. |
| Implemented | Compiler or runtime implementation exists for the stated scope. |
| Conformance-tested | Relevant positive and negative cases exercise the behavior through its supported interface. |
| Integration-tested | Relevant behavior has been demonstrated in a native executable with its stated runtime conditions. |
| Proposed | Design under consideration. |
| Experimental | Implementation or hypothesis undergoing evaluation. |

For example, `approve fn` has specified behavior, compiler implementation and tests for selected static checks and generated forms. Request-bound production approval is proposed. A code-generation test alone does not qualify for the integration label. For every security-related row, name the inputs, supported forms, trust assumptions and any required runtime evidence. Keep the published status tied to the source-linked assessment and refresh it as conformance lands. [Repository assessment](repository-assessment.md#security-evidence-and-limitations)

## Proposed edits

| Destination | Concrete change |
| --- | --- |
| [PRD.md](../../PRD.md#L11) | Append an AX paragraph under Philosophy. Add a versioned success section defining authoring and execution as independent axes, TTCS as the success measure, and ordinary backend work as a target. Keep Go/Swift/Rust influences, explicitness, observability, adapters and the original authority rationale. Label adapter catalogs and dependency security as intended designs. |
| [ROADMAP.md](../../ROADMAP.md#L7) | Replace broad shipped guarantees with an implemented-evidence table. Put checked binding, effect coverage, verified output paths and diagnostics first. Start the benchmark harness in parallel and keep graph queries before verified diffs. Keep previously planned integrations visible with dependencies and evidence gates. |
| [README.md](../../README.md#L1) | Adopt the current description and use two tested quick starts. The conventional example can build/run hello today. The safety example can show graph inspection or schema extraction and documented bounded checks. Give exact flags, verification scope and limits, link to the benchmark method and use the audit table as evidence. |
| [SPEC.md](../../SPEC.md#L383) | Track implementation deviations separately from normative intent. Resolve real semantic decisions through reviewed proposals, including `DB` grants, supported call resolution and effects, cross-module policy and optionals/evaluation, approval error behavior, and classified data sinks. Keep JSON schemas and product language in compiler documentation. |
| [Website](https://gosplash.dev/) and [source](../../site/index.md#L5) | Lead with the three levels above. Link to implementation status, both introductory use cases, and the historical and revised whitepapers. Present structured feedback and authority diffs as proposed until delivered. [Published GitHub description](https://github.com/gosplash/splash) |
| [WHITEPAPER.md](../../WHITEPAPER.md) | Preserve the April version as a dated, accessible historical document. Publish a separately dated revision covering the November initiative, its evidence and open proposals. |

Suggested PRD text:

> **Agent Experience.** Splash should make it straightforward for an autonomous coding agent to discover a local contract, produce a small change, interpret deterministic compiler feedback, and verify the result with reproducible tests. We will evaluate this on conventional and agent-enabled software by measuring time to accepted behavior, repair iterations, context cost and unintended authority changes. The same source and deployment trust rules apply to every author.

Proposed README quick start grounding:

```sh
go build -o ./splash ./cmd/splash
./splash check examples/hello/hello.splash
./splash build examples/hello/hello.splash -o hello
./hello
./splash graph examples/agent_tools/agent_tools.splash
./splash tools examples/agent_tools/agent_tools.splash --format openai
```

## Conflicts to resolve explicitly

- **Effect guarantees versus implemented checks.** The intended static subset rule is central to the design. Treat missing enforcement as a compiler gap, and resolve effect vocabulary discrepancies explicitly before public claims or compatibility commitments. [Spec](../../SPEC.md#L383)
- **Closed-world claims versus existing import and output paths.** Retain whole-closure reasoning and make binding, verified output and completeness prerequisites. Revisit absolute binary and foreign-code claims in the shared whitepaper and website source together. [Whitepaper compilation and trust model](../../WHITEPAPER.md#L418)
- **Recovery and error semantics.** Older documents use different `ai.prompt<T>` result models, approval and async spellings. Consolidate implemented syntax now and reserve the runtime error-boundary choice for design and native evidence. [Spec standard library](../../SPEC.md#L596) · [roadmap](../../ROADMAP.md#L33)
- **Bounded classification versus general data-flow claims.** Document the current sinks, projection and type shapes. Advance storage, serialization and foreign-boundary checking only with explicit semantics and validation. [Spec classification](../../SPEC.md#L529)
- **Escape hatches in autonomous repair.** A redline remains absolute within its defined semantics. Unsupported analysis yields incomplete evidence and cannot authorize verified output. Trusted boundaries are explicit and reviewable. Any future exception must be narrow, attributable and visible in authority comparisons. Automated repair suggestions require explicit review for exceptions. Reassess the planned `@bypass` against these criteria before it becomes a commitment. [Whitepaper exceptions](../../WHITEPAPER.md#L388)
- **Source latency versus authoring advantage.** Credit the historical synthetic measurements with their method and limits. Remove universal timing, growth and runtime-speed implications. Publish comparative TTCS findings only for the tasks and conditions actually measured. [Whitepaper benchmarking](../../WHITEPAPER.md#L712)

## Whitepaper revision

Publish the April and November work with dates, version history and links between them. Preserve the April text and provide a historical-status notice pointing readers to the current implementation record. The revised whitepaper can explain what changed without revising the historical rationale.

1. Autonomous software authorship as a language design problem.
2. Agent Experience and semantic locality.
3. Compiler-enforced authority and formal trust boundaries.
4. The semantic compiler as an interface for deterministic verification and agent queries.
5. Time to Correct Software as an empirical method, with measured results only when available.

## Adoption and readiness

Keep the tested hello-world flow and bounded inspection example while the two reference applications mature. Make the inventory demonstration the first evidence of an agent authoring conventional software. It should show an implementation and a follow-up change using structured feedback, independent tests and a matched Go comparison. Make the support demonstration the evidence of bounded agent execution. It should show exact tool membership, approval and denial behavior, and detection of an unintended authority expansion in a later change. Publish reproducible instructions and tie each guarantee to static or native evidence. [Reference demonstrations](engineering-plan.md#reference-demonstration-1-customer-support)

| Milestone | Evidence required |
| --- | --- |
| Compiler foundation | Canonical analyzed program and a verified path for artifact production. |
| Usable language subset | Positive and negative conformance for documented forms, including native build and behavior where applicable. |
| Agent-native feedback | Stable structured diagnostics and bounded semantic queries grounded in the same snapshot. |
| Authoring advantage | Credible TTCS and cost outcomes on matched conventional tasks, with failures and uncertainty reported. |
| Trust advantage | Independent policy tests, native runtime evidence where claimed, and sound comparisons of trusted authority snapshots. |
| Adoption readiness | Reproducible reference applications and dependable infrastructure for the initial workflows. |
| Ecosystem expansion | Demonstrated demand or measured adoption blockers. |

Treat engineering readiness, research outcomes and public adoption as separate decisions. The conventional pilot can run during compiler work. Publish the authoring claim only if comparative results support it, and the trust claim only within demonstrated coverage. DB/HTTP adapters, production approval, provider integration, dependency grants and richer analysis each need their own evidence-backed case. Defer a broad backend stack, alternate code generation and additional compiler infrastructure until measurements or adoption blockers justify them. [Engineering stage gates](engineering-plan.md#stages-dependencies-and-acceptance) · [TTCS protocol](time-to-correct-software.md)
