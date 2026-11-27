# TTCS invoice fixture

The same [task](task/TASK.md) and public inputs apply to the Go and Splash starters. The independent grader stages a single candidate source file and a driver for its language. One table of expected results grades native Go and both Splash backends. Cases exercise exact cents, half-cent ties, aggregation before rounding, zero, and maximum inputs.

Run the reference and negative controls from the repository root with Python 3.9+ and Go on PATH:

```sh
python3 -B -m unittest discover -s benchmarks/ttcs/invoice -p test_grade.py
```

The tests build the current Splash compiler. Both starters pass the published exact-cent cases and fail the full table at the half-cent case. Both reference implementations pass the full table on their respective backends.

Grade a known, trusted Go snapshot directly:

```sh
python3 benchmarks/ttcs/invoice/grade.py --lane go \
  --candidate benchmarks/ttcs/invoice/oracle/testdata/go/invoice.go \
  --cases benchmarks/ttcs/invoice/oracle/cases.json
```

For Splash, supply `--splash /path/to/splash` and select the corresponding `.splash` source. Use `--lane splash` for Go transpilation or `--lane splash-vm` for `splash run probe.splash --backend vm`. The VM lane can use a prebuilt Splash binary on a host without Go.

Each case contains a name, input integers in quantity/unit-cents/discount-percent order, and expected integers in subtotal/discount/due order. Exit 0 means all cases passed. Exit 1 means observable output differed. Exit 2 reports input, compilation, execution, or grader failure.

## CLI execution benchmark

Run the benchmark from the repository root with Python 3.9+ and Go on PATH.

```sh
go build -o /tmp/splash-invoice ./cmd/splash
python3 -B benchmarks/ttcs/invoice/benchmark.py --splash /tmp/splash-invoice --samples 12
```

| Condition | Timed commands |
| --- | --- |
| `go` | `go build` from the reference Go source and Go driver, then launch the new binary |
| `splash-go` | `splash build` to analyze the reference Splash source and driver, emit Go and build, then launch the new binary |
| `splash-vm` | `splash run probe.splash --backend vm` to analyze, lower, validate, and execute the same Splash source and driver |

Every sample uses a fresh directory, fresh processes, and a new output path for native binaries. The runner records wall-clock time from launch through captured output and process exit. Staging, oracle validation, and the Splash compiler build occur outside the timed window. The runner checks every invocation against the nine independent expected results before reporting timings. It reports individual samples and medians in milliseconds, with separate build and executable launch-through-exit timings for the native conditions. The VM command reports a single elapsed time across its internal phases.

The runner makes one unreported warm-up pass and rotates the starting condition on each measured pass. The two native builds use the Go caches and toolchain settings inherited from the environment. File system caches also persist across processes. This measures the invoice fixture's startup-inclusive source-to-result latency with a prebuilt compiler and warmed caches. TTCS measures how long an authoring system takes to produce an independently accepted change.

The [VM-004 Apple M5 run](results/vm-004-apple-m5.json) records the exact tested revision, reproduction commands, oracle digest, and all 12 samples per condition. Its median source-to-result latencies were 248.7 ms for native Go, 254.1 ms for Splash through Go, and 6.35 ms for Splash VM. The native measurements include building and launching a fresh executable. The VM measurement includes loading and running the source through the prebuilt Splash CLI. These timings describe the end-to-end workflow for one fixture and host.

## Grader boundary

Only `task/` is intended for the candidate workspace. The runner, oracle data, and reference implementations belong in the protected grading environment. Checked-in cases and solutions are public infrastructure controls. Use fresh sealed grading cases for measured trials.

The local runner builds and executes trusted fixtures with subprocess timeouts. Temporary staging alone does not isolate arbitrary generated code from the host. Before running an authoring trial, provide the protected toolchain, hidden cases and sandbox described in the [pilot harness design](../../../docs/design/ttcs-harness.md). This fixture supplies no authoring measurements.
