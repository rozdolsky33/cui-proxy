import copy
import unittest

from performance_stats import summarize_pairs


class PairedStatisticsTests(unittest.TestCase):
    def samples(self):
        proxy = [{"operation_id": "a", "first_byte_ms": 20},
                 {"operation_id": "b", "first_byte_ms": 40},
                 {"operation_id": "c", "first_byte_ms": 30}]
        bypass = [{"operation_id": "c", "first_byte_ms": 35},
                  {"operation_id": "a", "first_byte_ms": 10},
                  {"operation_id": "b", "first_byte_ms": 10}]
        return proxy, bypass

    def test_pairs_by_identity_before_percentiles(self):
        self.assertEqual(summarize_pairs(*self.samples()),
                         {"count": 3, "p50": 10, "p95": 30, "p99": 30})

    def test_retains_negative_differences(self):
        self.assertEqual(summarize_pairs(
            [{"operation_id": "a", "first_byte_ms": 5}],
            [{"operation_id": "a", "first_byte_ms": 9}])["p99"], -4)

    def test_missing_pairs_are_rejected(self):
        proxy, bypass = self.samples()
        with self.assertRaises(ValueError):
            summarize_pairs(proxy, bypass[:-1])

    def test_duplicate_identities_are_rejected(self):
        proxy, bypass = self.samples()
        for left, right in ((proxy + proxy[:1], bypass), (proxy, bypass + bypass[:1])):
            with self.assertRaises(ValueError):
                summarize_pairs(left, right)

    def test_invalid_samples_are_rejected(self):
        for value in (True, float("nan"), float("inf"), -1, "20", None, 10**400):
            with self.subTest(value_type=type(value).__name__):
                with self.assertRaises(ValueError):
                    summarize_pairs([{"operation_id": "a", "first_byte_ms": value}],
                                    [{"operation_id": "a", "first_byte_ms": 1}])
        for record in ({}, {"operation_id": "", "first_byte_ms": 1},
                       {"operation_id": 1, "first_byte_ms": 1}):
            with self.assertRaises(ValueError):
                summarize_pairs([record], [record])

    def test_empty_measurements_are_rejected(self):
        with self.assertRaises(ValueError):
            summarize_pairs([], [])

    def test_input_is_not_changed(self):
        proxy, bypass = self.samples()
        before = copy.deepcopy((proxy, bypass))
        summarize_pairs(proxy, bypass)
        self.assertEqual((proxy, bypass), before)

    def test_nearest_rank_tail(self):
        proxy = [{"operation_id": str(i), "first_byte_ms": i} for i in range(1, 101)]
        bypass = [{"operation_id": str(i), "first_byte_ms": 0} for i in range(1, 101)]
        self.assertEqual(summarize_pairs(proxy, bypass),
                         {"count": 100, "p50": 50, "p95": 95, "p99": 99})


class OutcomeStatisticsTests(unittest.TestCase):
    def summarize(self, rows):
        import performance_stats
        self.assertTrue(hasattr(performance_stats, "summarize_outcomes"))
        return performance_stats.summarize_outcomes(rows)

    def row(self, ident, outcome, supported=True, completed=True):
        return dict(operation_id=ident, outcome=outcome, supported=supported,
                    completed=completed, duration_ms=45.0)

    def test_timeouts_remain_in_supported_denominator(self):
        rows = [self.row("one", "supported_success"),
                self.row("two", "unresolved", completed=False),
                self.row("three", "expected_policy_block", supported=False)]
        summary = self.summarize(rows)
        self.assertEqual(summary["attempted"], 3)
        self.assertEqual(summary["completed"], 2)
        self.assertEqual(summary["supported_attempted"], 2)
        self.assertEqual(summary["supported_success"], 1)
        self.assertEqual(summary["failures"], 1)
        self.assertEqual(summary["supported_success_rate"], 0.5)
        self.assertEqual(summary["outcomes"]["unresolved"], 1)

    def test_unsupported_failure_is_not_success(self):
        summary = self.summarize([self.row("one", "transformation_error", supported=False)])
        self.assertEqual(summary["failures"], 1)
        self.assertIsNone(summary["supported_success_rate"])

    def test_rejects_duplicate_outcomes_and_invalid_records(self):
        good = self.row("one", "supported_success")
        invalid = [dict(good, outcome="unknown"), dict(good, completed=False),
                   dict(good, supported=False), dict(good, supported=1),
                   dict(good, duration_ms=float("inf")), dict(good, duration_ms=-1),
                   dict(good, outcome="expected_policy_block"),
                   dict(good, operation_id="")]
        for row in invalid:
            with self.subTest():
                with self.assertRaises(ValueError): self.summarize([row])
        with self.assertRaises(ValueError): self.summarize([good, good])

    def test_empty_outcomes_do_not_claim_success(self):
        summary = self.summarize([])
        self.assertEqual(summary["attempted"], 0)
        self.assertEqual(summary["supported_success"], 0)
        self.assertIsNone(summary["supported_success_rate"])
