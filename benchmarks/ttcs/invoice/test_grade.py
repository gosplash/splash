import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
LANES = (("go", "go"), ("splash", "splash"), ("splash-vm", "splash"))


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

    def test_public_cli_benchmark_reports_all_three_conditions(self):
        result = subprocess.run(
            [sys.executable, "-B", str(HERE / "benchmark.py"), "--splash", str(self.splash),
             "--samples", "1"],
            cwd=HERE.parents[2], capture_output=True, text=True, timeout=120,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["cases"], 9)
        self.assertEqual(set(report["conditions"]), {"go", "splash-go", "splash-vm"})
        for condition, measurements in report["conditions"].items():
            with self.subTest(condition=condition):
                sample, = measurements["samples_ms"]
                self.assertGreater(sample["total"], 0)
                if condition == "splash-vm":
                    self.assertEqual(set(sample), {"total"})
                else:
                    self.assertEqual(set(sample), {"build", "execute", "total"})
                    self.assertGreater(sample["build"], 0)
                    self.assertGreater(sample["execute"], 0)


if __name__ == "__main__":
    unittest.main()
