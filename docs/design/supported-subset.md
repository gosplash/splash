# Splash reference for the conventional pilot

This reference targets the native compiler at baseline `9bf9e36` with Go 1.26.3 on darwin/arm64. The kickoff retains the ordinary language forms shown here. This is a deliberately small executable subset for authoring experiments. See the [language specification](../../SPEC.md) for normative syntax and the [repository assessment](repository-assessment.md) for analysis and lowering gaps.

## Check and run

From the Splash repository:

```sh
go build -o /tmp/splash-pilot ./cmd/splash
/tmp/splash-pilot check examples/hello/hello.splash
/tmp/splash-pilot build examples/hello/hello.splash -o /tmp/splash-pilot-hello
/tmp/splash-pilot-hello
```

The program prints `Hello, world`. For changes, run both `check` and `build`, then run functional tests. `check` performs static analysis for the selected entry and its imports. Native compilation catches additional errors, including cases where Go lowering does not support the source form. `emit` can help inspect how a construct lowers.

## Core forms

| Write | Guidance |
| --- | --- |
| `module inventory` | Place the module declaration first. For an executable, define an entry with `fn main() { ... }`. |
| `fn total(qty: Int, cents: Int) -> Int` | Parameter and return types are explicit. Omit the arrow for functions with no result. End every reachable path in a result-returning function with a value. |
| `type Item { ... }` | Use one typed field per line. Construct with `Item { qty: 2, cents: 125 }` and read a field with `item.qty`. Supply each task-required field with its declared type. |
| `let result = compute(input)` | Local binding can infer its type. Use `let result: Int = ...` for an explicit type. Read the resulting name to satisfy Go's local-variable use rules. |
| `if qty < 1 { ... }` | Use a boolean condition and braces. An `else` block is supported. |
| `Int`, `Bool`, `String` | Use same-type operands. `Int` lowers to Go `int`, so use bounded values for portable integer arithmetic. For this pilot use integer cents, make the rounding rule explicit and use `println` only in fixture entry points. |
| `use pricing as rules` | Loads `pricing.splash` relative to the entry directory and binds `rules` as an alias. Use qualified calls where a task permits and distinct top-level names throughout the closure because Go lowering merges the declarations. |

A pure function needs no `needs` clause for these tasks. Use simple named calls, records and conditionals. Keep imports acyclic and avoid name collisions. Treat qualified calls as an executable subset observation, with no implication about authority graph coverage.

## Multi-file example

Save these two files in the same directory. The task-facing entry selects and prints a result field. The shared function can be called from native fixture probes with other data.

`pricing.splash`

```splash
module pricing
expose Line, total_line

type Line {
    units: Int
    unit_cents: Int
}

fn total_line(line: Line) -> Int {
    if line.units < 1 {
        return 0
    }
    return line.units * line.unit_cents
}
```

`invoice.splash`

```splash
module invoice
use pricing as rules

fn main() {
    let line = rules.Line { units: 2, unit_cents: 125 }
    let total: Int = rules.total_line(line)
    println(total)
}
```

From that directory, `/tmp/splash-pilot check invoice.splash` should succeed. Build with `/tmp/splash-pilot build invoice.splash -o ./invoice` and run `./invoice`. The expected output is `250`. A public checker or runner can substitute an input case without putting expected outputs in the candidate executable.

## Choose simple alternatives for this pilot

Use a record with an explicit boolean and typed values for accepted/rejected results. Put variable-length batches in the trusted fixture driver and probe one case or state transition at a time. The Go emitter generates list literals as `[]any`, so a parsed list literal is poor evidence for a usable typed list workflow. The checked-in hello example demonstrates a narrow optional-record-field case with `none` and `??`; use a pure fallback because the current emitter evaluates the fallback eagerly as a Go argument. [List and coalescing lowering](../../internal/compiler/internal/codegen/expr.go#L44)

Keep user-defined generics, exhaustive matching, arbitrary callback or dynamic dispatch, async behavior, AI adapters, and HTTP/DB adapters outside these starter tasks. Avoid relying on full argument, assignment, and struct-initializer validation in `check`. Invoke functions with their declared argument types and arity, and build and execute every graded candidate. [Checker limits](../../internal/typechecker/typechecker.go#L377) · [Native examples and limits](repository-assessment.md#validation-and-performance)
