import copy
import unittest

from soak_stage_acceptance import evaluate_extraction_windows, worst_nearest_rank_p95


class StageAcceptanceTests(unittest.TestCase):
    def records(self, count=20):
        return [dict(time=f"stage-{window}-{i}", identity=dict(session=f"client-{i%4}"),
                     observation_lower_s=100+window*900+10+i*2,
                     observed_s=101+window*900+10+i*2,
                     extraction_cache_hits=0,
                     stages_ms=dict(extraction_run=100, extraction_wait=20))
                for window in range(8) for i in range(count)]

    def evaluate(self, rows, faults=None):
        return evaluate_extraction_windows(rows, started_s=100, duration_s=7200,
                                           fault_intervals=[] if faults is None else faults)

    def test_all_original_windows_and_no_campaign_qualification(self):
        result = self.evaluate(self.records())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(len(result["windows"]), 8)
        self.assertFalse(result["performance_qualified"])

    def test_original_p95_is_not_replaced_by_maximum(self):
        rows = self.records()
        rows[0]["stages_ms"]["extraction_run"] = 10000
        self.assertEqual(self.evaluate(rows)["state"], "pass")
        rows[1]["stages_ms"]["extraction_run"] = 10000
        self.assertEqual(self.evaluate(rows)["state"], "fail")

    def test_optional_fast_records_cannot_dilute_a_bad_window(self):
        self.assertEqual(worst_nearest_rank_p95([10000], [1]*100), 10000)
        self.assertEqual(worst_nearest_rank_p95([1]*20, [10000]), 1)
        self.assertEqual(worst_nearest_rank_p95([1]*10, [10000]), 10000)

    def test_boundary_uncertainty_is_retained_without_false_failure_claim(self):
        rows = self.records(count=10)
        rows.append(dict(time="boundary", identity=dict(session="client-0"),
                         observation_lower_s=999, observed_s=1001, extraction_cache_hits=0,
                         stages_ms=dict(extraction_run=10000, extraction_wait=0)))
        result = self.evaluate(rows)
        self.assertEqual(result["state"], "incomplete")
        self.assertEqual([r["optional_samples"] for r in result["windows"][:2]], [1, 1])

    def test_fault_samples_are_reported_separately_and_budget_still_applies(self):
        rows = self.records()
        rows[0]["stages_ms"]["extraction_run"] = 10000
        fault = dict(started_s=110, ended_s=112)
        result = self.evaluate(rows, [fault])
        self.assertEqual(result["fault_samples"], 1)
        self.assertEqual(result["state"], "pass")
        rows[0]["stages_ms"]["extraction_run"] = 30001
        self.assertEqual(self.evaluate(rows, [fault])["state"], "fail")

    def test_missing_duplicate_unbounded_and_nonfinite_records_rejected(self):
        original = self.records()
        cases = [original[20:], original+[original[0]]]
        for field, value in [("observation_lower_s", None), ("observed_s", float("nan")),
                             ("observation_lower_s", 10000), ("extraction_cache_hits", True)]:
            rows = copy.deepcopy(original)
            rows[0][field] = value
            cases.append(rows)
        rows = copy.deepcopy(original)
        rows[0]["stages_ms"]["extraction_run"] = float("inf")
        cases.append(rows)
        for rows in cases:
            with self.assertRaises(ValueError):
                self.evaluate(rows)
        with self.assertRaises(ValueError):
            evaluate_extraction_windows(original, started_s=100, duration_s=150, fault_intervals=[])

    def test_quantile_bound_matches_exhaustive_subset_oracle(self):
        import itertools
        import random
        randomizer = random.Random(741170)
        for _ in range(100):
            certain = [randomizer.randrange(10) for _ in range(randomizer.randrange(1, 25))]
            optional = [randomizer.randrange(10) for _ in range(randomizer.randrange(8))]
            expected = []
            for mask in itertools.product((False, True), repeat=len(optional)):
                samples = sorted(certain + [value for value, keep in zip(optional, mask) if keep])
                expected.append(samples[(95*len(samples)+99)//100-1])
            self.assertEqual(worst_nearest_rank_p95(certain, optional), max(expected))

    def test_cached_image_cannot_hide_unavailable_stage_timings(self):
        rows = self.records()
        rows[0]["extraction_cache_hits"] = 2
        rows[0]["stages_ms"] = {"body_inspect": 5}
        with self.assertRaises(ValueError):
            self.evaluate(rows)
