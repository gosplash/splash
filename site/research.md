---
layout: default
title: Research
description: Splash research on compiler feedback, semantic context, and autonomous software engineering. Includes benchmark data, methods, and reproduction instructions.
permalink: /research/
---

# Splash research

Splash studies how programming languages affect the cost of producing correct software with coding agents. **Time to Correct Software (TTCS)** measures elapsed authoring time to the first observed snapshot accepted by an independent grader. We track completion, context use, and regressions alongside elapsed time. [TTCS research protocol][protocol]

[Hypotheses](#hypotheses) · [Execution benchmarks](#rust-and-go-execution-benchmarks) · [Compiler scaling](#compiler-scaling) · [Authoring pilot](#ttcs-pilot-001) · [Cross-module study](#proposed-cross-module-experiment)

[SHA-256 checksums for site downloads]({{ '/assets/research/SHA256SUMS.txt' | relative_url }})

## Hypotheses

1. **Faster feedback can shorten repair loops.** Compare backend workflows using the same Splash source, model, and grading criteria. To isolate latency itself, hold visible checks constant and vary feedback delay.
2. **Compiler-derived context can reduce discovery and rework.** Test whether signatures, dependencies, effects, and diagnostic witnesses help agents make correct changes with less source context.
3. **Explicit authority contracts can improve review.** Test whether compiler evidence helps catch unintended authority changes that pass functional tests. Evaluate this separately from conventional authoring tasks.

## Rust and Go execution benchmarks

The four paths run the same bounded invoice calculation. Every timed executable run passed the same independent nine-case oracle. The table reports median latency in milliseconds from 12 samples per applicable cell on an Apple M5 with macOS 27.0.1, Go 1.26.3, and Rust and Cargo 1.100.1. The report includes all 348 records, toolchain versions, per-phase timings where available, p95, and ranges. [Method and controls][rust-method] · [Raw JSON][rust-data]

<div class="research-table" role="region" aria-label="Execution benchmark results" tabindex="0" markdown="1">

| Workflow | Native Rust dev | Native Go | Splash through Go | Splash VM |
| --- | ---: | ---: | ---: | ---: |
| Static verification | 100.40 | 62.53 | 3.31 | 3.01 |
| Cold build to result | 347.29 | 1,689.94 | 1,698.62 | 3.94 |
| Warm build to result | 145.03 | 231.13 | 312.37 | 4.29 |
| Incremental edit to result | 277.69 | 307.33 | 315.40 | 4.39 |
| Unchanged source rerun | 105.39 | 139.45 | 296.82 | 3.33 |
| Prebuilt native binary launch | 4.16 | 5.51 | 5.60 | — |

</div>

Rust dev uses Cargo's incremental development profile. The separate optimized release arm recorded a 215.85 ms median for edit to result and 2.72 ms for prebuilt binary launch. Cold runs started with empty build caches or target directories and inherited OS caches. Rust used its preinstalled standard library. Static checks differ across the toolchains. Each end-to-end result includes the work of its developer command, including compilation where applicable. Prebuilt launch is a separate workflow, and these tests do not establish native runtime throughput or agent productivity. The [earlier Go and Splash run][vm-baseline] records its own warm-cache samples.

### Reproduce the execution comparison

Use Python 3.9 or later with the recorded Go, Rust, and Cargo versions on `PATH`. The two checkouts pin the benchmark harness and measured Splash compiler independently.

```bash
scratch=$(mktemp -d)
git clone https://github.com/gosplash/splash.git "$scratch/bench"
git -C "$scratch/bench" checkout --detach 61c6f087a97c4d1f21e2fe6d1922b07af95f4ffa
git -C "$scratch/bench" worktree add --detach "$scratch/compiler" \
  e15ec6de9706c5c084d577c43bace12361566800
(cd "$scratch/compiler" && go build -buildvcs=true -o "$scratch/splash" ./cmd/splash)
cd "$scratch/bench"
python3 -B benchmarks/ttcs/invoice/benchmark.py --splash "$scratch/splash" \
  --splash-revision e15ec6de9706c5c084d577c43bace12361566800 \
  --samples 12 --hardware "Apple M5" > "$scratch/execution-results.json"
```

## Compiler scaling

These in-process benchmarks measure synthetic programs on an Apple M5 running Darwin ARM64. Analysis includes file reads, parsing, typing, and call graph assembly. Each value below is a median of five samples, in milliseconds per operation. The [raw log]({{ '/assets/research/compiler-scaling-apple-m5-2026-11-27.txt' | relative_url }}) includes all 91 benchmark cells and allocation data.

<div class="research-table" role="region" aria-label="Compiler scaling results" tabindex="0" markdown="1">

| Workload | Analysis | Typed modules to VM IR | VM load with validation | VM execution |
| --- | ---: | ---: | ---: | ---: |
| 100 functions | 0.156 | 0.035 | 0.041 | 0.014 |
| 500 functions | 0.897 | 0.186 | 0.219 | 0.078 |
| 2,000 functions | 3.237 | 0.730 | 0.778 | 0.278 |
| 10,000 functions | 17.140 | 4.250 | 4.238 | 1.508 |
| 100 modules | 1.416 | 0.038 | 0.045 | 0.016 |
| 64 records × 32 fields | 2.134 | 0.838 | 0.646 | 0.196 |

</div>

A separate synthetic workload with 1,000 recursive policy groups measured 22.753 ms for analysis and 4.935 ms for safety checking. It exercised declared contracts at the EFF-001 candidate revision. The timing excludes inferred-effect analysis, CLI startup, native compilation, and external operations. VM execution reuses a validated VM. The 10,000-function chain uses an elevated frame budget. Treat these results as a baseline for synthetic scaling.

### Reproduce compiler scaling

Use the recorded Go version, 1.26.3. Measured source revision `f3c9890bac510e607b8e0469342a20901f5cc1e1` is the pinned EFF-001 checkout plus the [benchmark patch]({{ '/assets/research/compiler-scaling-f3c9890.patch' | relative_url }}).

```bash
scratch=$(mktemp -d)
git clone https://github.com/gosplash/splash.git "$scratch/scaling"
git -C "$scratch/scaling" checkout --detach 06e504f0adf545de56cdc282d119b78e75ddbf05
curl -fsSL https://gosplash.dev/assets/research/compiler-scaling-f3c9890.patch \
  -o "$scratch/scaling.patch"
git -C "$scratch/scaling" apply "$scratch/scaling.patch"
cd "$scratch/scaling"
GOMAXPROCS=10 go test ./cmd/splash -run '^$' -bench '^BenchmarkScaling' \
  -benchmem -benchtime=100ms -count=5 > "$scratch/scaling-results.txt"
```

## TTCS Pilot 001

Six fresh Codex sessions repaired the same invoice-rounding bug in parallel. Each arm had three sessions using `gpt-6.2-sol` at medium reasoning effort, a short language reference, and the same restricted workspace tools. Splash used its Go-backed native build.

<div class="research-table" role="region" aria-label="Pilot 001 authoring results" tabindex="0" markdown="1">

| Language | Final solutions accepted | First accepted snapshots | Median |
| --- | ---: | --- | ---: |
| Go | 3/3 | 13.334 s, 15.007 s, 15.011 s | 15.007 s |
| Splash | 3/3 | 13.840 s, 13.952 s, 14.905 s | 13.952 s |

</div>

All six solutions passed 40 sealed cases and the nine checked-in cases. All used the same arithmetic repair with one write and one visible verification. The clock started before Codex launch, with snapshots scheduled at 15, 30, 60, 120, and 180 seconds where reached, plus submission. The observed ranges overlap. Each first accepted snapshot is an upper bound on when that patch became correct. A single public task with three samples per arm leaves comparative authoring efficiency unestablished.

Go used 241,367 input tokens, including 216,192 cached. Splash used 225,881, including 196,608 cached. If cached input is a subset of total input, that leaves 25,175 uncached for Go and 29,273 for Splash. The counters include repeated harness context. Actual monetary costs were unavailable. The initial tooling failure and its usage are retained separately.

[Download the Pilot 001 report, raw results, and reproduction kit]({{ '/assets/research/ttcs-pilot-001.tar.gz' | relative_url }}). The kit pins the source revision, session settings, toolchain image, and original case digest. Its public runner generates fresh grading inputs. Original sealed cases remain private.

### Reproduce Pilot 001 with fresh cases

Use Docker, Python 3.9 or later, and authenticated Codex CLI 0.170.0 access to the recorded model. This runs six fresh sessions in a separate directory.

```bash
scratch=$(mktemp -d)
mkdir "$scratch/kit" "$scratch/pilot"
curl -fsSL https://gosplash.dev/assets/research/ttcs-pilot-001.tar.gz \
  | tar -xz -C "$scratch/kit"
cp "$scratch/kit"/{pilot.py,Dockerfile,.dockerignore,prompt.txt,*-reference.md} "$scratch/pilot/"
git clone https://github.com/gosplash/splash.git "$scratch/repo"
revision=e15ec6de9706c5c084d577c43bace12361566800
git -C "$scratch/repo" archive "$revision" cmd internal go.mod benchmarks/ttcs/invoice \
  | tar -x -C "$scratch/pilot"
cd "$scratch/pilot"
mv benchmarks/ttcs/invoice fixture
docker build --platform linux/arm64 -t splash-ttcs-invoice:e15ec6d .
python3 -B pilot.py
```

## Proposed cross-module experiment

Study unfamiliar order-management software with roughly 20–30 modules and several hundred functions. Use matched Go and Splash starters, a common bug report, and public regression checks. Limit the first fixture to supported imports and in-process business logic. Agents must trace dependencies, change behavior across module boundaries, and preserve existing contracts. Compare native Go, Splash through Go, and Splash through its VM. The two Splash arms share a starter, model, tools, and acceptance rules. The Go comparison covers the combined language and toolchain workflow. A later condition can add structured diagnostics and semantic inspection.

Dry-run all three arms against shared behavior and regression gates. Use common checkpoints and submission for the primary completion curve. Capture additional snapshots at successful writes and link each visible verification to the checked revision by digest. After the session, grade the snapshots independently and report first correct write, first visibly verified correct revision, and submission separately. Track completion, regressions, source context delivered, files inspected, verification attempts, and cached and uncached usage.

Define infrastructure failures and reruns before dispatch. Treat an initial small run as a feasibility study. Comparative claims require multiple matched tasks, randomized arm order within task blocks, repeated trials, and a common deadline. Holdout tasks, starters, and sealed cases stay private until the experiments conclude. [Harness design][harness]

### Open methodological questions

- How much latency is spent on discovery and reasoning, tool and process startup, compilation, and candidate execution?
- How should a study measure source context delivered to the model separately from repeated harness context and cached tokens?
- Do structured compiler interfaces improve correctness against a Go arm with comparable semantic tooling?
- How many distinct tasks and repeated trials are needed to compare completion curves, regressions, and total cost per accepted solution?
- How should policy conformance be graded independently when the compared toolchains verify different properties?

[protocol]: https://github.com/gosplash/splash/blob/e15ec6de9706c5c084d577c43bace12361566800/docs/design/time-to-correct-software.md
[harness]: https://github.com/gosplash/splash/blob/e15ec6de9706c5c084d577c43bace12361566800/docs/design/ttcs-harness.md
[rust-method]: https://github.com/gosplash/splash/blob/0694a9b4ea833d4b6c96ee9a7063ecc2de1da826/benchmarks/ttcs/invoice/README.md
[rust-data]: https://raw.githubusercontent.com/gosplash/splash/0694a9b4ea833d4b6c96ee9a7063ecc2de1da826/benchmarks/ttcs/invoice/results/rust-comparison-apple-m5.json
[vm-baseline]: https://raw.githubusercontent.com/gosplash/splash/e15ec6de9706c5c084d577c43bace12361566800/benchmarks/ttcs/invoice/results/vm-004-apple-m5.json
