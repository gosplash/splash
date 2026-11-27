import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
LANES = (("go", "go"), ("splash", "splash"), ("splash-vm", "splash"))
HAS_RUST = shutil.which("cargo") is not None and shutil.which("rustc") is not None


class GradeInvoiceTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        build_dir = tempfile.TemporaryDirectory()
        cls.addClassCleanup(build_dir.cleanup)
        cls.splash = Path(build_dir.name) / "splash"
        subprocess.run(
            ["go", "build", "-o", str(cls.splash), "./cmd/splash"],
            cwd=HERE.parents[2],
            check=True,
            timeout=120,
        )

    def grade(self, lane, candidate, cases="oracle/cases.json", *, env=None):
        return subprocess.run(
            [
                sys.executable, str(HERE / "grade.py"), "--lane", lane,
                "--splash", str(self.splash), "--candidate", str(HERE / candidate),
                "--cases", str(HERE / cases),
            ],
            capture_output=True, text=True, timeout=120, env=env,
        )

    def test_references_pass(self):
        for lane, language in LANES:
            with self.subTest(lane=lane):
                result = self.grade(lane, f"oracle/testdata/{language}/invoice.{language}")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("accepted", result.stdout)

    def test_truncating_controls_fail_on_half_cent(self):
        for lane, language in LANES:
            with self.subTest(lane=lane):
                result = self.grade(lane, f"task/testdata/{language}/invoice.{language}")
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("half-cent", result.stderr)

    def test_starters_pass_unchanged_exact_cent_behavior(self):
        for lane, language in LANES:
            with self.subTest(lane=lane):
                result = self.grade(
                    lane, f"task/testdata/{language}/invoice.{language}", "task/public-cases.json"
                )
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_vm_reference_passes_without_go_on_path(self):
        with tempfile.TemporaryDirectory() as empty_path:
            result = self.grade(
                "splash-vm", "oracle/testdata/splash/invoice.splash",
                env={**os.environ, "PATH": empty_path},
            )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("accepted 9 cases", result.stdout)

    @unittest.skipUnless(HAS_RUST, "Rust and Cargo required")
    def test_rust_reference_and_rounding_control(self):
        reference = self.grade("rust", "oracle/testdata/rust/invoice.rs")
        self.assertEqual(reference.returncode, 0, reference.stderr)
        self.assertIn("accepted 9 cases", reference.stdout)
        variant = self.grade("rust", "task/testdata/rust/invoice.rs", "task/public-cases.json")
        self.assertEqual(variant.returncode, 0, variant.stderr)
        starter = self.grade("rust", "task/testdata/rust/invoice.rs")
        self.assertEqual(starter.returncode, 1, starter.stderr)
        self.assertIn("half-cent rounds up: expected ['1', '1', '0'], got ['1', '0', '1']", starter.stderr)

    @unittest.skipUnless(HAS_RUST, "Rust and Cargo required")
    def test_public_cli_benchmark_reports_all_workflows(self):
        result = subprocess.run(
            [sys.executable, "-B", str(HERE / "benchmark.py"), "--splash", str(self.splash),
             "--samples", "1", "--splash-revision",
             subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=HERE.parents[2], text=True).strip()
             if (HERE.parents[2] / ".git").exists() else "archive-test-build"],
            cwd=HERE.parents[2], capture_output=True, text=True, timeout=120,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["cases"], 9)
        self.assertEqual(set(report["conditions"]), {"rust", "go", "splash-go", "splash-vm"})
        self.assertEqual(set(report["optimized_release"]), {"rust-release"})
        conditions = {**report["conditions"], **report["optimized_release"]}
        for condition, workflows in conditions.items():
            expected = {"static", "cold", "warm", "edit", "unchanged", "prebuilt"}
            self.assertEqual(set(workflows), expected)
            for workflow, measurements in workflows.items():
                with self.subTest(condition=condition, workflow=workflow):
                    if condition == "splash-vm" and workflow == "prebuilt":
                        self.assertIsNone(measurements)
                        continue
                    sample, = measurements["samples_ms"]
                    self.assertGreater(sample["total"], 0)
                    summary = measurements["statistics_ms"]["total"]
                    self.assertEqual(summary["samples"], 1)
                    self.assertEqual(summary["median"], sample["total"])
                    self.assertEqual(summary["p95"], sample["total"])


if __name__ == "__main__":
    unittest.main()
