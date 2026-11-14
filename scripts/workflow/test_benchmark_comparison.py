import unittest
import json
from pathlib import Path
import tempfile
from unittest import mock

import benchmark_comparison as comparison


class BenchmarkComparisonTests(unittest.TestCase):
    def output(self, ns=100, size=20, allocations=2, iterations=10):
        return f"goos: linux\ngoarch: arm64\nBenchmarkRequestWalk-8 {iterations} {ns} ns/op {size} B/op {allocations} allocs/op\nPASS\n"

    def sample(self, ns=100, size=20, allocations=2):
        return {"iterations": 10, "ns_per_op": ns,
                "bytes_per_op": size, "allocations_per_op": allocations}

    def compared(self, base, candidate):
        result = comparison.compare_samples(base, candidate)
        self.assertEqual(set(result), {"report_only", "sample_count", "base", "candidate",
                                      "relative_change_percent", "absolute_change"})
        return result

    def test_parse_reports_only_the_fixed_benchmark_and_numeric_metrics(self):
        self.assertEqual(comparison.parse_output(self.output()), self.sample())

    def test_accepts_go_throughput_column_without_exporting_raw_output(self):
        text = self.output().replace("ns/op", "ns/op 7852.30 MB/s")
        self.assertEqual(comparison.parse_output(text), self.sample())

    def test_rejects_invalid_optional_throughput(self):
        for value in ("NaN", "Inf", "-1"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                comparison.parse_output(self.output().replace("ns/op", f"ns/op {value} MB/s"))

    def test_rejects_missing_duplicate_or_unknown_benchmark(self):
        for text in ("PASS\n", self.output() + self.output(),
                     self.output().replace("BenchmarkRequestWalk", "BenchmarkOther")):
            with self.subTest(kind=len(text)), self.assertRaises(ValueError):
                comparison.parse_output(text)

    def test_rejects_malformed_units_counts_and_nonfinite_values(self):
        for text in (self.output(iterations=9), self.output(ns="NaN"),
                     self.output(ns="Inf"), self.output(ns=0),
                     self.output(size=-1), self.output(allocations="1.5"),
                     self.output().replace("ns/op", "ms/op")):
            with self.subTest(kind=len(text)), self.assertRaises(ValueError):
                comparison.parse_output(text)

    def test_keeps_improvements_and_allocation_regressions_report_only(self):
        result = self.compared([self.sample()] * 5,
                                            [self.sample(90, 25, 3)] * 5)
        self.assertTrue(result["report_only"])
        self.assertEqual(result["sample_count"], 5)
        self.assertEqual(result["relative_change_percent"], {
            "ns_per_op": -10.0, "bytes_per_op": 25.0,
            "allocations_per_op": 50.0})

    def test_reports_latency_regression_without_reclassifying_as_failure(self):
        result = self.compared([self.sample()] * 5,
                                            [self.sample(200)] * 5)
        self.assertTrue(result["report_only"])
        self.assertEqual(result["relative_change_percent"]["ns_per_op"], 100.0)

    def test_requires_five_complete_finite_equal_work_samples_per_arm(self):
        invalid = [[], [self.sample()] * 4,
                   [self.sample()] * 4 + [self.sample(float("nan"))],
                   [self.sample()] * 4 + [{**self.sample(), "iterations": 11}],
                   [self.sample()] * 4 + [{**self.sample(), "extra": 1}]]
        for samples in invalid:
            with self.subTest(count=len(samples)), self.assertRaises(ValueError):
                comparison.compare_samples(samples, [self.sample()] * 5)

    def test_zero_allocation_baselines_have_explicit_absolute_changes(self):
        result = self.compared([self.sample(100, 0, 0)] * 5,
                                            [self.sample(100, 10, 0)] * 5)
        self.assertIsNone(result["relative_change_percent"]["bytes_per_op"])
        self.assertEqual(result["relative_change_percent"]["allocations_per_op"], 0)
        self.assertEqual(result["absolute_change"]["bytes_per_op"], 10)

    def test_uses_median_of_individual_samples(self):
        base = [self.sample(n) for n in [1, 2, 100, 200, 300]]
        candidate = [self.sample(n) for n in [100, 2, 50, 1, 60]]
        result = self.compared(base, candidate)
        self.assertEqual(result["base"]["ns_per_op"], 100)
        self.assertEqual(result["candidate"]["ns_per_op"], 50)




class BenchmarkRunnerTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.base = self.root / "base"
        self.candidate = self.root / "candidate"
        self.base.write_bytes(b"base executable")
        self.candidate.write_bytes(b"candidate executable")
        self.report = self.root / "summary.json"
        self.captures = self.root / "private"
        self.calls = []

    def execute(self, command, log):
        arm = Path(command[0]).name
        self.calls.append(arm)
        ns = 100 if arm == "base" else 90
        Path(log).write_text(f"BenchmarkRequestWalk-2 10 {ns} ns/op 20 B/op 2 allocs/op\nPASS\n")
        return 0

    def run_comparison(self):
        return comparison.run_comparison(self.base, self.candidate, "a" * 40,
                                         "b" * 40, self.report, self.captures)

    def test_alternates_five_equal_work_pairs_and_reports_identities(self):
        with mock.patch.object(comparison, "execute_command", self.execute, create=True):
            result = self.run_comparison()
        self.assertEqual(self.calls, ["base", "candidate", "candidate", "base",
                                     "base", "candidate", "candidate", "base",
                                     "base", "candidate"])
        self.assertEqual(result["status"], "complete")
        self.assertEqual(result.get("benchmark"), "BenchmarkRequestWalk")
        self.assertEqual(result.get("iterations"), 10)
        self.assertEqual(result.get("repetitions"), 5)
        self.assertEqual(result.get("gomaxprocs"), 2)
        self.assertTrue(result["report_only"])
        self.assertFalse(result["performance_qualified"])
        self.assertEqual(result["comparison"]["relative_change_percent"]["ns_per_op"], -10)
        self.assertEqual(result["base_commit"], "a" * 40)
        self.assertEqual(len(result["base_binary_sha256"]), 64)
        self.assertEqual(json.loads(self.report.read_text()), result)

    def test_command_failure_stops_later_pairs_and_retains_safe_failure(self):
        def failed(command, log):
            if len(self.calls) == 2:
                self.calls.append("failed")
                Path(log).write_text("private diagnostic marker")
                return 7
            return self.execute(command, log)
        with mock.patch.object(comparison, "execute_command", failed, create=True):
            with self.assertRaises(RuntimeError):
                self.run_comparison()
        self.assertEqual(len(self.calls), 3)
        report = json.loads(self.report.read_text())
        self.assertEqual(report["status"], "failed")
        self.assertNotIn("private diagnostic marker", self.report.read_text())

    def test_interrupt_propagates_after_incomplete_evidence_is_recorded(self):
        with mock.patch.object(comparison, "execute_command", side_effect=KeyboardInterrupt,
                               create=True):
            with self.assertRaises(KeyboardInterrupt):
                self.run_comparison()
        self.assertEqual(json.loads(self.report.read_text())["status"], "interrupted")

    def test_binary_changes_during_measurement_are_rejected(self):
        def changed(command, log):
            code = self.execute(command, log)
            self.candidate.write_bytes(b"changed executable")
            return code
        with mock.patch.object(comparison, "execute_command", changed, create=True):
            with self.assertRaises(ValueError):
                self.run_comparison()
        self.assertEqual(json.loads(self.report.read_text())["status"], "failed")

    def test_existing_report_is_preserved_and_no_commands_run(self):
        self.report.write_text("existing evidence")
        with mock.patch.object(comparison, "execute_command", self.execute, create=True):
            with self.assertRaises(FileExistsError):
                self.run_comparison()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.report.read_text(), "existing evidence")


if __name__ == "__main__":
    unittest.main()
