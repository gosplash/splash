import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


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

    def grade(self, lane, candidate, cases="oracle/cases.json"):
        return subprocess.run(
            [
                sys.executable, str(HERE / "grade.py"), "--lane", lane,
                "--splash", str(self.splash), "--candidate", str(HERE / candidate),
                "--cases", str(HERE / cases),
            ],
            capture_output=True, text=True, timeout=120,
        )

    def test_references_pass(self):
        for lane, extension in (("go", "go"), ("splash", "splash")):
            with self.subTest(lane=lane):
                result = self.grade(lane, f"oracle/testdata/{lane}/invoice.{extension}")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("accepted", result.stdout)

    def test_truncating_controls_fail_on_half_cent(self):
        for lane, extension in (("go", "go"), ("splash", "splash")):
            with self.subTest(lane=lane):
                result = self.grade(lane, f"task/testdata/{lane}/invoice.{extension}")
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("half-cent", result.stderr)

    def test_starters_pass_unchanged_exact_cent_behavior(self):
        for lane, extension in (("go", "go"), ("splash", "splash")):
            with self.subTest(lane=lane):
                result = self.grade(
                    lane, f"task/testdata/{lane}/invoice.{extension}", "task/public-cases.json"
                )
                self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
