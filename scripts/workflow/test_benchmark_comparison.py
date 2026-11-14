import unittest

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


if __name__ == "__main__":
    unittest.main()
