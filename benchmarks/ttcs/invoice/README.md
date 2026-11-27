# TTCS invoice fixture

The same [task](task/TASK.md) and public inputs apply to the Rust, Go, and Splash starters. The independent grader stages a single candidate source file and a driver for its language. One table of expected results grades native Rust, native Go, and both Splash backends. Cases exercise exact cents, half-cent ties, aggregation before rounding, zero, and maximum inputs.

Run the reference and negative controls from the repository root with Python 3.9+ and Go on PATH:

```sh
python3 -B -m unittest discover -s benchmarks/ttcs/invoice -p test_grade.py
```

The tests build the current Splash compiler. With Rust and Cargo on PATH, they also exercise the Rust lane and the full comparison runner. Each truncating starter passes the published exact-cent cases and fails the full table at the half-cent case. Each reference implementation passes all nine cases.

Grade a known, trusted Go snapshot directly:

```sh
python3 benchmarks/ttcs/invoice/grade.py --lane go \
  --candidate benchmarks/ttcs/invoice/oracle/testdata/go/invoice.go \
  --cases benchmarks/ttcs/invoice/oracle/cases.json
```

For Rust, use `--lane rust --candidate benchmarks/ttcs/invoice/oracle/testdata/rust/invoice.rs` with the same cases. The runner stages a dependency-free Cargo package.

For Splash, supply `--splash /path/to/splash` and select the corresponding `.splash` source. Use `--lane splash` for Go transpilation or `--lane splash-vm` for `splash run probe.splash --backend vm`. The VM lane can use a prebuilt Splash binary on a host without Go.

Each case contains a name, input integers in quantity/unit-cents/discount-percent order, and expected integers in subtotal/discount/due order. Exit 0 means all cases passed. Exit 1 means observable output differed. Exit 2 reports input, compilation, execution, or grader failure.

## CLI execution benchmark

Run on one host with Python 3.9+, Go, Rust, and Cargo on PATH. Build the Splash frontend at the pinned baseline in a separate checkout. Run the comparator from this checkout.

```sh
scratch=$(mktemp -d)
baseline="$scratch/source"
git clone --quiet --no-checkout . "$baseline"
git -C "$baseline" checkout --quiet --detach e15ec6d
(cd "$baseline" && go build -buildvcs=true -o "$scratch/splash" ./cmd/splash)
python3 -B benchmarks/ttcs/invoice/benchmark.py --splash "$scratch/splash" \
  --splash-revision e15ec6de9706c5c084d577c43bace12361566800 --samples 12 --hardware 'Apple M5'
```

The [VM-004 baseline](results/vm-004-apple-m5.json) measured one startup-inclusive source-to-result path per toolchain at `5a5f075`. The comparison reuses its nine-case oracle, reference Go and Splash sources, and fresh-process timing convention. It runs the unchanged Splash driver against both Splash backends.

| Workflow | Setup and timed commands |
| --- | --- |
| Static verification | Fresh workspace. Rust runs `cargo check --offline --locked --quiet`, Go runs `go vet invoice.go probe.go` with a warm per-lane Go cache, and both Splash paths run `splash check probe.splash`. These commands provide different checks. Go vet loads and type-checks a fresh invoice package, runs analyzers, and can reuse warmed dependency results. Rust has a fresh Cargo target directory. Splash check performs frontend and safety analysis without running VM admission or native lowering. |
| Cold build-to-result | Fresh workspace and empty `GOCACHE` or Cargo target directory. Rust and Go build then launch a fresh artifact. Splash through Go runs `splash build` then launches its artifact. Splash VM runs `splash run probe.splash --backend vm` in one process. Rust uses the standard library shipped with its toolchain. The Go cold path may compile its standard library. OS caches remain inherited. |
| Warm build-to-result | Prepare and verify the reference in the same workspace. Remove the native output file before timing while retaining build intermediates and caches. Run the same build and launch commands as in the cold path. Cargo may restore an artifact from its existing target state. |
| Incremental edit-to-result | Prepare and verify the reference, then replace the candidate in the same workspace. The edit uses quotient-and-remainder rounding instead of an offset and division while preserving the nine expected results. Time build and launch immediately after the edit. The source file write is outside timing. The Rust dev profile reuses incremental compiler state. Go reuses its build cache, and the Splash frontend reanalyzes the program. |
| Unchanged-source execution | Prime each workspace with the same command sequence used for timing and verify the output. Preserve its source, compiler caches, and any output artifact. Use `cargo run` for Rust, `go run` for Go, `splash build` and launch for the Go-backed Splash path, and `splash run` for the VM. These are developer rerun commands, each with its own validation or build behavior. |
| Previously compiled native binary | Prepare, build, and launch once outside timing. Time another direct launch of the already-built executable. The VM has no prebuilt artifact in this comparison. |

The primary Rust condition uses Cargo's normal dev profile with incremental compilation enabled. A separate Rust release condition uses Cargo's optimized release profile with incremental compilation disabled. See the [Cargo profiles reference](https://doc.rust-lang.org/cargo/reference/profiles.html) and [`cargo check` behavior](https://doc.rust-lang.org/cargo/commands/cargo-check.html).

Timing includes process launch, captured output, and exit. Staging, preparation, oracle comparison, and the Splash compiler build are outside timing. The runner rotates condition order within each workflow and primes warm build and static checks outside timing. It uses an isolated Cargo home and isolated compiler caches. It clears Cargo compiler and profile overrides and disables persisted Go settings and external Go cache delegation. Tool discovery via PATH, OS and SDK settings, workspace-ancestor Cargo configuration, and Go debugging settings are inherited. Use a clean toolchain configuration for comparisons and verify that the output reports the expected Splash VCS revision with `vcs.modified=false`. Each executable sample and preparation run must pass the unchanged oracle. A failure aborts the report. The runner preserves each timing, records versions and the oracle digest, and reports median, nearest-rank p95, minimum, maximum, and sample count. With 12 samples the p95 is the maximum.

The VM repeats analysis, lowering, validation, and execution for each timed run. Separate build and launch phase timings are available for workflows that explicitly split those commands. In-process native throughput is outside this comparison. It uses one small workload, inherited OS caches, and a prebuilt Splash compiler, with tool-specific cold and warm cache behavior. [The authoring protocol](../../../docs/design/time-to-correct-software.md) defines a future matched Rust agent arm with its own outcome measures.

## Apple M5 results

The [raw comparison report](results/rust-comparison-apple-m5.json) contains all 348 timing records, method settings, compiler versions, source revisions, and the oracle SHA-256 digest. Every executable timing passed the nine-case oracle. The run used a 10-core Apple M5 with 32 GiB of memory under ordinary host load, without CPU isolation or OS cache flushing.

Median latency in milliseconds, 12 samples per applicable cell. Rust release is shown separately from the development comparisons. Per-phase samples, p95, and observed ranges are in the report.

| Workflow | Rust dev | Native Go | Splash through Go | Splash VM | Rust release |
| --- | ---: | ---: | ---: | ---: | ---: |
| Static verification | 100.40 | 62.53 | 3.31 | 3.01 | 99.12 |
| Cold build-to-result | 347.29 | 1,689.94 | 1,698.62 | 3.94 | 290.73 |
| Warm build-to-result | 145.03 | 231.13 | 312.37 | 4.29 | 128.87 |
| Incremental edit-to-result | 277.69 | 307.33 | 315.40 | 4.39 | 215.85 |
| Unchanged-source execution | 105.39 | 139.45 | 296.82 | 3.33 | 98.85 |
| Prebuilt native binary | 4.16 | 5.51 | 5.60 | — | 2.72 |

## Grader boundary

Only `task/` is intended for the candidate workspace. The runner, oracle data, and reference implementations belong in the protected grading environment. Checked-in cases and solutions are public infrastructure controls. Use fresh sealed grading cases for measured trials.

The local runner builds and executes trusted fixtures with subprocess timeouts. Temporary staging alone does not isolate arbitrary generated code from the host. Before running an authoring trial, provide the protected toolchain, hidden cases and sandbox described in the [pilot harness design](../../../docs/design/ttcs-harness.md). This fixture supplies no authoring measurements.
