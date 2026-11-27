# TTCS pilot harness

Implementation design for the first conventional Splash-versus-Go pilot. The [TTCS protocol](time-to-correct-software.md) defines the metrics and comparison rules. This document defines the harness inputs, isolation, task adapters and acceptance before launching trials. The harness can accept another language by supplying a starter, toolchain adapter, native driver and equivalence evidence. The [invoice fixture](../../benchmarks/ttcs/invoice/README.md) is the first native smoke path for both language adapters. Its public oracle controls exercise the design. Protected trial orchestration, additional tasks and agent runs remain future work.

## Trial inputs and adapter

| Input | What it pins |
| --- | --- |
| Task manifest | Task and variant IDs, immutable starter, common ticket, per-lane editable paths, expected observable behaviors, required gates, and a separate protected oracle ID. |
| Trial manifest | Pairing block and assigned arm, model and harness versions, tool permissions, reference digest, time/token limits, checkpoint cadence, toolchain and resource/cache policy. |
| Language adapter | Visible check/build/test commands, protected baseline and build inputs, native probe driver, diagnostic tags and fixture serialization. |
| Grader configuration | Hidden fixtures, equivalence and regression assertions, per-probe limits, expected output codec and rubric version. The task manifest can expose the oracle ID without its contents. |

Use an adapter contract conceptually shaped as `prepare(starter)`, `commands()`, `build(snapshot, probe)`, and `run(artifact)`. Keep scheduling, agent invocation, event collection, snapshotting, grading decisions and metric computation in the common harness. Both baseline arms can read source, invoke local commands and use equivalent version-matched language references. The baseline Splash arm uses its text CLI and the Go arm uses idiomatic Go commands. Pin each lane's native build and verification commands. Keep adapter differences in the run manifest.

The initial common fixture interface can be a typed business operation paired with a trusted native probe that prints a short, unambiguous record of primitive fields to stdout. Generate the per-case driver from typed fixture values using language-aware escaping and a reserved entry point. Stage only allowlisted source paths and validated declarations of allowed inputs. For Splash, call the candidate's public function from a trusted entry in a staged closure; for Go, build the equivalent call in a trusted package/runner. Use the same observable output fields and requirements. The trusted grader compares output and, for stateful tasks, the observed sequence of states. Provide public probes of representative cases. Hidden values may be presented as inputs to the candidate during grading; expected outputs and grading logic stay outside the candidate process. If using compiled-in per-case input for Splash, record the extra grading builds separately from authoring time.

## Five matched task pairs

Keep the same requirements, initial behavior and controlled inputs in both variants. Where a sequence matters, pass state explicitly through successive calls in the trusted driver. The initial benchmark targets business logic and module maintenance within the executable subset.

| Pair | Public capability | Hidden checks and an incorrect control |
| --- | --- | --- |
| Invoice calculation | Return subtotal, discount and amount due in integer cents. Specify the rounding mode, tie behavior, input bounds and discount policy. | Check exact cent boundaries, tie cases and permitted extreme quantities. A truncating solution fails the half-cent boundary. |
| Shipment validation | Evaluate one shipment record against a declared quantity limit and known identifier. The driver can apply cases to a batch. Return a typed decision and reason. | Check limit equality, negative/zero quantities, empty identifier and rule precedence for conflicting invalid fields. An off-by-one implementation fails the limit case. |
| Inventory feature addition | Add backorder behavior to a starter reconciliation step. Specify valid transitions, shortage behavior, and idempotence for a retry of the same identifier against unchanged delivery history. | Check existing success paths, shortage, replay and a fresh ID. A double-application control passes the first event and fails replay. |
| Unfamiliar repository bug repair | Repair a boundary-condition defect in a starter with several relevant functions and distractors. Preserve unrelated behavior. | Exercise the reported symptom, neighboring values and pass-to-pass checks. A fix for only the visible example fails a related input. |
| Multi-module refactoring | Extract a calculation into a distinct module while preserving its entry point, types and behavior. Use supported Splash imports and idiomatic Go package or file organization as the task specifies. | Build a trusted caller using the original interface and run shared expected behavior. Require the stated ownership change with a small language adapter assertion. A behavior-preserving inline implementation fails the module requirement. |

Author each pair with a public starter and ticket, a visible probe, a known-correct reference and plausible incorrect variants. Keep the reference and hidden cases outside task and source context. Review whether a reasonable alternative can satisfy each task. Validate both languages on the same correct and incorrect behavior classes before enrolling a pair. Checks for architecture or interface shape must be specified in the ticket.

## Isolation and run lifecycle

1. Start each trial with a fresh agent session and a clean workspace made from the pinned starter. Mount toolchain, harness configuration and public docs read-only. Allow candidate writes only to the workspace.
2. Use an elapsed monotonic clock and capture immutable source snapshots outside the workspace on scheduled checkpoints and final submission. Record optional edit/verification snapshots under a predeclared rule for sensitivity analysis. Snapshot normalized relative paths and file bytes, reject escaping paths and links, and deduplicate by content digest while retaining every checkpoint timestamp.
3. Grade after the session without sending hidden results back. The protected runner stages each snapshot in a fresh build environment with a pinned compiler and driver. Prevent candidate build hooks and dependencies from altering toolchain, driver, grader or other cases. Run native artifacts in a separate sandbox without network access, credentials or write access to the oracle, baseline, toolchain or previously captured snapshots. Enforce CPU, memory, file, stdout and wall-time limits. Use a bounded result decoder outside the candidate.
4. Record static acceptance as its own milestone. A candidate qualifies for TTCS only when one snapshot meets every required gate, including native build and hidden functional regressions. A timed-out, crashed or otherwise unsuccessful trial remains in the dataset. Separate infrastructure errors using a preregistered rerun rule.

## Events, reports and readiness

Record schema version, trial/task/arm IDs, monotonic timestamps, event kind and duration, optional snapshot digest, command category, exit status and resource/token usage with its source. Distinguish source reads, semantic queries, edits, check/build/test commands, model calls and checkpoint/final submission. Label repair windows from observed failures and subsequent edits/checks and disclose overlap with nested operations. Track delivered source and semantic tokens separately from local bytes read. Preserve explicit unknowns in token accounting. Report analysis and grading time separately.

The primary TTCS curve uses common elapsed-time checkpoints and final submission. Report sampled resolution, earliest observed acceptance, final acceptance and capped time with deadline charged to unsolved trials. Report CTCS as summed inference and metered execution costs across all attempted trials divided by accepted final solutions. Disclose price and allocation assumptions, accounting for cached input, output and separately metered reasoning where available. Publish numerator and denominator; report no finite CTCS when no final solution is accepted.

Before a pilot run, dry-run the manifest and harness on correct references, starters and incorrect controls in both lanes. Check replay against immutable snapshots, timeout and crash recording, no hidden feedback, containment of candidate writes, and trace aggregation on synthetic events. Then use five matched pairs with at least three fresh trials per arm if the execution environment supports it. This is a feasibility and variance study. Use its observed discovery and repair patterns to choose later diagnostic and semantic-query experiments on unseen tasks.
