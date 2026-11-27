# Checked integer semantics across execution targets

Status: accepted. Date: 2026-11-27.

## Context and Problem Statement

Splash needs predictable integer behavior across deployment targets. The existing Go backend lowers `Int` to Go `int`. VM-002 introduces arithmetic and establishes the intended Splash contract before backend-specific behavior becomes entrenched.

## Considered Options

- Retain target-dependent width and behavior.
- Define fixed-width wrapping arithmetic.
- Define fixed-width checked arithmetic with explicit failures.

## Decision Outcome

Splash `Int` is signed 64-bit. Addition, subtraction, multiplication, division, and unary negation report overflow when the mathematical result cannot be represented. Division truncates toward zero. Remainder follows the dividend's sign or is zero. Division and remainder by zero report a division-by-zero error. The minimum integer divided by minus one overflows. Its remainder by minus one is zero. Negating the minimum integer overflows.

Evaluate operands and call arguments from left to right. Numeric operations require operands of the appropriate type, with no implicit numeric conversion. Use independently expected VM results to check arithmetic boundary behavior.

### Consequences

- VM-002 targets this contract. Existing Go lowering continues to use Go `int`.
- Cross-backend parity is limited to operations whose values and intermediates fit both implementations and succeed under the Splash contract. Expand the shared domain after Go lowering implements equivalent checked behavior.
- Integer width and error conventions for future embedding interfaces derive from this contract.

## More Information

[Splash IR and VM design](../design/splash-ir-and-vm.md#typed-control-flow-ir) · [Execution architecture decision](2026-11-26-own-execution-semantics.md)
