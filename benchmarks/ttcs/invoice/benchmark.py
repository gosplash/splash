"""Measure verified invoice workflows across Rust, Go, and both Splash backends."""

import argparse
import hashlib
import json
import math
import os
import platform
import shutil
import statistics
import subprocess
import sys
import tempfile
from pathlib import Path
from time import perf_counter_ns

from grade import SPLASH_DRIVER, output_mismatch, read_cases, run, stage_candidate

ORACLE = Path(__file__).resolve().parent / "oracle"
REPO = ORACLE.parents[3]
CONDITIONS = (
    ("rust", "rust", "rust/invoice.rs", False),
    ("go", "go", "go/invoice.go", False),
    ("splash-go", "splash", "splash/invoice.splash", False),
    ("splash-vm", "splash-vm", "splash/invoice.splash", False),
    ("rust-release", "rust", "rust/invoice.rs", True),
)
WORKFLOWS = ("static", "cold", "warm", "edit", "unchanged", "prebuilt")


def static_command(lane, splash, release):
    if lane == "rust":
        return ["cargo", "check", "--offline", "--locked", "--quiet", *(["--release"] if release else [])]
    if lane == "go":
        return ["go", "vet", "invoice.go", "probe.go"]
    return [str(splash), "check", SPLASH_DRIVER]


def verify_output(actual, cases, condition, workflow):
    mismatch = output_mismatch(actual.splitlines(), cases)
    if mismatch is not None:
        raise ValueError(f"{condition}/{workflow}: {mismatch}")


def benchmark_environment(root):
    # The fixture is offline and dependency-free. Isolate tool configuration
    # that would otherwise change compiler invocations or cache placement.
    prefixes = ("CARGO_", "RUSTC", "RUSTFLAGS", "__CARGO_", "GOCACHE", "GOFLAGS", "GOEXPERIMENT")
    env = {key: value for key, value in os.environ.items() if not key.startswith(prefixes)}
    env.update(CARGO_HOME=str(root / "cargo-home"), GOENV="off", GOTOOLCHAIN="local", GOWORK="off")
    return env


def measure(condition, workflow, cases, splash, directory, warm_cache, base_env):
    name, lane, source, release = condition
    build, execute = stage_candidate(lane, ORACLE / "testdata" / source, cases, splash, directory, release=release)
    env = base_env.copy()
    if lane == "rust":
        target = str(directory / "target")
        env.update(CARGO_TARGET_DIR=target, CARGO_BUILD_BUILD_DIR=target)
        env["CARGO_INCREMENTAL"] = "0" if release else "1"
    elif lane in ("go", "splash"):
        env["GOCACHE"] = str(directory / "go-cache" if workflow == "cold" else warm_cache / lane)

    if workflow == "unchanged":
        if lane == "rust":
            execute = ["cargo", "run", "--offline", "--locked", "--quiet", *(["--release"] if release else [])]
            build = None
        elif lane == "go":
            execute = ["go", "run", "invoice.go", "probe.go"]
            build = None

    if workflow in ("warm", "edit", "unchanged", "prebuilt"):
        if build is not None:
            run(build, directory, 90, env=env)
        verify_output(run(execute, directory, 30, env=env), cases, name, "preparation")

    if workflow == "static":
        start = perf_counter_ns()
        run(static_command(lane, splash, release), directory, 90, env=env)
        return {"total": (perf_counter_ns() - start) / 1_000_000}

    if workflow == "edit":
        candidate = directory / ("src/invoice.rs" if lane == "rust" else Path(source).name)
        shutil.copyfile(ORACLE / "testdata" / "edited" / source, candidate)
    if workflow == "warm" and build is not None:
        Path(execute[0]).unlink()
    if workflow == "prebuilt":
        build = None

    start = perf_counter_ns()
    if build is not None:
        run(build, directory, 90, env=env)
        built = perf_counter_ns()
    actual = run(execute, directory, 30, env=env)
    finished = perf_counter_ns()
    verify_output(actual, cases, name, workflow)

    sample = {"total": (finished - start) / 1_000_000}
    if build is not None:
        sample["build"] = (built - start) / 1_000_000
        sample["execute"] = (finished - built) / 1_000_000
    return sample


def summarize(samples):
    summary = {}
    for phase in samples[0]:
        values = sorted(sample[phase] for sample in samples)
        summary[phase] = {
            "samples": len(values),
            "median": statistics.median(values),
            "p95": values[math.ceil(.95 * len(values)) - 1],
            "min": values[0],
            "max": values[-1],
        }
    return {"samples_ms": samples, "statistics_ms": summary}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--splash", type=Path, required=True,
                        help="absolute path to the prebuilt Splash compiler")
    parser.add_argument("--splash-revision", required=True, help="source revision of the Splash compiler")
    parser.add_argument("--hardware", default=platform.processor(), help="CPU or host model label")
    parser.add_argument("--samples", type=int, default=12, help="samples per workflow and condition (default: 12)")
    args = parser.parse_args(argv)
    if not args.splash.is_absolute():
        parser.error("--splash must be an absolute path")
    if args.samples < 1:
        parser.error("--samples must be at least 1")

    try:
        cases_path = ORACLE / "cases.json"
        cases = read_cases(cases_path)
        with tempfile.TemporaryDirectory(prefix="ttcs-invoice-benchmark-") as temp:
            root = Path(temp)
            base_env = benchmark_environment(root)
            versions = {tool: run(command, root, 30, env=base_env).strip() for tool, command in {
                "rustc": ["rustc", "--version", "--verbose"],
                "cargo": ["cargo", "--version", "--verbose"],
                "go": ["go", "version"],
                "splash_build_info": ["go", "version", "-m", str(args.splash)],
            }.items()}
            recorded_revision = next((field.split("=", 1)[1] for field in versions["splash_build_info"].split()
                                      if field.startswith("vcs.revision=")), None)
            if recorded_revision and not recorded_revision.startswith(args.splash_revision):
                raise ValueError("--splash-revision disagrees with the binary's recorded revision")
            versions["splash_revision"] = recorded_revision or args.splash_revision
            versions["splash_revision_source"] = "Go build metadata" if recorded_revision else "caller-supplied"
            versions["splash_sha256"] = hashlib.sha256(args.splash.read_bytes()).hexdigest()
            if (REPO / ".git").exists():
                versions["benchmark_revision"] = run(["git", "rev-parse", "HEAD"], REPO, 30).strip()
                versions["benchmark_dirty"] = bool(run(["git", "status", "--porcelain"], REPO, 30).strip())
            measurements = {name: {} for name, *_ in CONDITIONS}
            warm_cache = root / "go-cache-warm"

            def run_sample(condition, workflow):
                with tempfile.TemporaryDirectory(dir=root) as temp_workspace:
                    return measure(condition, workflow, cases, args.splash, Path(temp_workspace), warm_cache, base_env)

            # Warm the toolchain and host once. Cold samples still get new compiler caches.
            for condition in CONDITIONS:
                run_sample(condition, "warm")
                run_sample(condition, "static")
            for workflow in WORKFLOWS:
                applicable = tuple(condition for condition in CONDITIONS
                                   if workflow != "prebuilt" or condition[0] != "splash-vm")
                samples = {condition[0]: [] for condition in applicable}
                for index in range(args.samples):
                    offset = index % len(applicable)
                    for condition in applicable[offset:] + applicable[:offset]:
                        samples[condition[0]].append(run_sample(condition, workflow))
                for name, workflows in measurements.items():
                    workflows[workflow] = summarize(samples[name]) if name in samples else None
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(f"benchmark failed: {error}", file=sys.stderr)
        return 2

    report = {
        "host": platform.platform(),
        "hardware": args.hardware,
        "samples_per_workflow": args.samples,
        "cache_conditions": {
            "cold": "fresh GOCACHE or Cargo target directory; inherited OS caches and preinstalled Rust standard library",
            "warm": "same source and workspace after preparation; final native output removed, intermediates retained",
            "edit": "same prepared workspace and caches; source replaced with quotient-and-remainder rounding",
            "unchanged": "same workspace, source and caches; prime and time the same language-appropriate rerun command",
            "prebuilt": "previously built and launched artifact; fresh process",
            "static": "fresh workspace; warm shared per-lane Go cache or fresh Cargo target directory",
        },
        "tool_configuration": "isolated Cargo home, target and build directories; compiler/profile overrides cleared; Go persisted config and external cache disabled; tool path and OS settings inherited",
        "python": platform.python_version(),
        "versions": versions,
        "cases": len(cases),
        "oracle_sha256": hashlib.sha256(cases_path.read_bytes()).hexdigest(),
        "ordering": "rotate starting condition each sample within a workflow",
        "conditions": {name: measurements[name] for name, _, _, release in CONDITIONS if not release},
        "optimized_release": {name: measurements[name] for name, _, _, release in CONDITIONS if release},
    }
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
