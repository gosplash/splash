# TTCS invoice fixture

The same [task](task/TASK.md) and public inputs apply to the Go and Splash starters. The independent grader stages a single candidate source file and compiles a native probe for that language. One table of expected results grades both lanes. Cases exercise exact cents, half-cent ties, aggregation before rounding, zero, and maximum inputs.

Run the reference and negative controls from the repository root with Python 3.9+ and Go on PATH:

```sh
python3 -B -m unittest discover -s benchmarks/ttcs/invoice -p test_grade.py
```

The test builds the current Splash compiler. Both starters pass the published exact-cent cases and fail the full table at the half-cent case. Both reference implementations pass the full table.

Grade a known, trusted Go snapshot directly:

```sh
python3 benchmarks/ttcs/invoice/grade.py --lane go \
  --candidate benchmarks/ttcs/invoice/oracle/testdata/go/invoice.go \
  --cases benchmarks/ttcs/invoice/oracle/cases.json
```

For Splash, use `--lane splash`, supply `--splash /path/to/splash`, and select the corresponding `.splash` source. Each case contains a name, input integers in quantity/unit-cents/discount-percent order, and expected integers in subtotal/discount/due order. Exit 0 means all cases passed. Exit 1 means observable output differed. Exit 2 reports input, compilation, execution, or grader failure.

## Grader boundary

Only `task/` is intended for the candidate workspace. The runner, oracle data, and reference implementations belong in the protected grading environment. Checked-in cases and solutions are public infrastructure controls. Use fresh sealed grading cases for measured trials.

The local runner builds and executes trusted fixtures with subprocess timeouts. Temporary staging alone does not isolate arbitrary generated code from the host. Before running an authoring trial, provide the protected toolchain, hidden cases and sandbox described in the [pilot harness design](../../../docs/design/ttcs-harness.md). This fixture supplies no authoring measurements.
