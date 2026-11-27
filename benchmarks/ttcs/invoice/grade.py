"""Run the same invoice oracle against a trusted Rust, Go, or Splash source file."""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

SPLASH_DRIVER = "probe.splash"
RUST_MANIFEST = Path(__file__).resolve().parent / "rust"


def read_cases(path):
    cases = json.loads(path.read_text())
    if not isinstance(cases, list) or not cases:
        raise ValueError("cases must be a nonempty JSON array")
    names = set()
    for case in cases:
        if not isinstance(case, dict) or set(case) != {"name", "inputs", "expected"}:
            raise ValueError("each case requires name, inputs, and expected")
        name = case["name"]
        if not isinstance(name, str) or not name or name in names:
            raise ValueError("case names must be nonempty and unique")
        names.add(name)
        inputs = case["inputs"]
        if not isinstance(inputs, list) or len(inputs) != 3 or any(
            type(value) is not int or not 0 <= value <= limit
            for value, limit in zip(inputs, (10_000, 2_000, 100))
        ):
            raise ValueError(f"{name}: expected three integer inputs within task bounds")
        expected = case["expected"]
        if not isinstance(expected, list) or len(expected) != 3 or any(
            type(value) is not int or value < 0 for value in expected
        ):
            raise ValueError(f"{name}: expected three nonnegative integer results")
    return cases


def go_driver(cases):
    lines = ['package main', 'import "fmt"', 'func main() {']
    for index, case in enumerate(cases):
        values = ", ".join(map(str, case["inputs"]))
        result = f"result{index}"
        lines.append(f"    {result} := InvoiceTotals({values})")
        for field in ("Subtotal", "Discount", "Due"):
            lines.append(f"    fmt.Println({result}.{field})")
    return "\n".join(lines + ["}", ""])


def splash_driver(cases):
    lines = [
        "module invoice_probe", "use invoice as candidate", "fn main() {",
    ]
    for index, case in enumerate(cases):
        values = ", ".join(map(str, case["inputs"]))
        result = f"result_{index}"
        lines.append(f"    let {result} = candidate.invoice_totals({values})")
        for field in ("subtotal", "discount", "due"):
            lines.append(f"    println({result}.{field})")
    return "\n".join(lines + ["}", ""])


def rust_driver(cases):
    lines = ["mod invoice;", "use invoice::invoice_totals;", "fn main() {"]
    for index, case in enumerate(cases):
        values = ", ".join(map(str, case["inputs"]))
        result = f"result_{index}"
        lines.append(f"    let {result} = invoice_totals({values});")
        for field in ("subtotal", "discount", "due"):
            lines.append(f'    println!("{{}}", {result}.{field});')
    return "\n".join(lines + ["}", ""])


def run(command, directory, timeout, *, env=None):
    result = subprocess.run(
        command, cwd=directory, capture_output=True, text=True, timeout=timeout, env=env,
    )
    if result.returncode != 0:
        diagnostic = result.stderr.strip() or result.stdout.strip()
        label = " ".join(command[:2])
        raise ValueError(f"{label} failed: {diagnostic}")
    return result.stdout


def stage_candidate(lane, candidate_source, cases, splash, directory, *, release=False):
    artifact = directory / ("probe.exe" if os.name == "nt" else "probe")
    if lane == "rust":
        for name in ("Cargo.toml", "Cargo.lock"):
            shutil.copyfile(RUST_MANIFEST / name, directory / name)
        src = directory / "src"
        src.mkdir()
        (src / "main.rs").write_text(rust_driver(cases))
        shutil.copyfile(candidate_source, src / "invoice.rs")
        profile = "release" if release else "debug"
        build = ["cargo", "build", "--offline", "--locked", "--quiet"]
        if release:
            build.append("--release")
        return build, [str(directory / "target" / profile / artifact.name)]
    if lane == "go":
        driver = directory / "probe.go"
        driver.write_text(go_driver(cases))
        shutil.copyfile(candidate_source, directory / "invoice.go")
        return ["go", "build", "-o", str(artifact), "invoice.go", driver.name], [str(artifact)]

    (directory / SPLASH_DRIVER).write_text(splash_driver(cases))
    shutil.copyfile(candidate_source, directory / "invoice.splash")
    if lane == "splash-vm":
        return None, [str(splash), "run", SPLASH_DRIVER, "--backend", "vm"]
    return [str(splash), "build", SPLASH_DRIVER, "-o", str(artifact)], [str(artifact)]


def run_candidate(lane, candidate_source, cases, splash):
    with tempfile.TemporaryDirectory(prefix="ttcs-invoice-") as temp:
        directory = Path(temp)
        build, execute = stage_candidate(lane, candidate_source, cases, splash, directory)
        if lane == "splash":
            run([str(splash), "check", SPLASH_DRIVER], directory, 30)
        if build is not None:
            env = {**os.environ, "CARGO_TARGET_DIR": str(directory / "target")} if lane == "rust" else None
            run(build, directory, 90, env=env)
        return run(execute, directory, 30 if build is None else 5).splitlines()


def output_mismatch(actual, cases):
    expected_count = 3 * len(cases)
    if len(actual) != expected_count:
        return f"expected {expected_count} result fields, got {len(actual)}"
    for index, case in enumerate(cases):
        got = actual[3 * index:3 * index + 3]
        want = list(map(str, case["expected"]))
        if got != want:
            return f"{case['name']}: expected {want}, got {got!r}"
    return None


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lane", choices=("rust", "go", "splash", "splash-vm"), required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--cases", type=Path, required=True)
    parser.add_argument("--splash", type=Path, help="path to the trusted Splash compiler")
    args = parser.parse_args(argv)
    if args.lane.startswith("splash") and args.splash is None:
        parser.error("--splash is required for the Splash lanes")
    try:
        cases = read_cases(args.cases)
        splash = args.splash.resolve() if args.splash is not None else None
        actual = run_candidate(args.lane, args.candidate, cases, splash)
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(f"grading failed: {error}", file=sys.stderr)
        return 2
    mismatch = output_mismatch(actual, cases)
    if mismatch is not None:
        print(mismatch, file=sys.stderr)
        return 1
    print(f"accepted {len(cases)} cases")
    return 0


if __name__ == "__main__":
    sys.exit(main())
