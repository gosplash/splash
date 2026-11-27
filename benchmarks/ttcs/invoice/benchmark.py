"""Measure cold-process invoice execution on the Go and Splash public CLIs."""

import argparse
import json
import platform
import statistics
import subprocess
import sys
import tempfile
from pathlib import Path
from time import perf_counter_ns

from grade import output_mismatch, read_cases, run, stage_candidate

ORACLE = Path(__file__).resolve().parent / "oracle"
CONDITIONS = (
    ("go", "go", "go/invoice.go"),
    ("splash-go", "splash", "splash/invoice.splash"),
    ("splash-vm", "splash-vm", "splash/invoice.splash"),
)


def measure(condition, lane, source, cases, splash):
    with tempfile.TemporaryDirectory(prefix="ttcs-invoice-benchmark-") as temp:
        directory = Path(temp)
        build, execute = stage_candidate(lane, source, cases, splash, directory)
        sample = {}
        start = perf_counter_ns()
        if build is not None:
            run(build, directory, 90)
            built = perf_counter_ns()
        actual = run(execute, directory, 30 if build is None else 5)
        finished = perf_counter_ns()

    mismatch = output_mismatch(actual.splitlines(), cases)
    if mismatch is not None:
        raise ValueError(f"{condition}: {mismatch}")
    if build is not None:
        sample["build"] = (built - start) / 1_000_000
        sample["execute"] = (finished - built) / 1_000_000
    sample["total"] = (finished - start) / 1_000_000
    return sample


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--splash", type=Path, required=True,
                        help="absolute path to the prebuilt Splash compiler")
    parser.add_argument("--samples", type=int, default=12, help="samples per condition (default: 12)")
    args = parser.parse_args(argv)
    if not args.splash.is_absolute():
        parser.error("--splash must be an absolute path")
    if args.samples < 1:
        parser.error("--samples must be at least 1")

    try:
        cases = read_cases(ORACLE / "cases.json")
        with tempfile.TemporaryDirectory(prefix="ttcs-invoice-benchmark-") as temp:
            go_version = run(["go", "version"], Path(temp), 30).strip()
        samples = {condition: [] for condition, _, _ in CONDITIONS}
        # Warm up the inherited caches and verify that all conditions pass before sampling.
        for condition, lane, source in CONDITIONS:
            measure(condition, lane, ORACLE / "testdata" / source, cases, args.splash)
        for index in range(args.samples):
            offset = index % len(CONDITIONS)
            for condition, lane, source in CONDITIONS[offset:] + CONDITIONS[:offset]:
                sample = measure(condition, lane, ORACLE / "testdata" / source, cases, args.splash)
                samples[condition].append(sample)
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(f"benchmark failed: {error}", file=sys.stderr)
        return 2

    report = {
        "host": platform.platform(),
        "go_version": go_version,
        "cases": len(cases),
        "conditions": {
            condition: {
                "samples_ms": results,
                "median_ms": {
                    phase: statistics.median(result[phase] for result in results)
                    for phase in results[0]
                },
            }
            for condition, results in samples.items()
        },
    }
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
