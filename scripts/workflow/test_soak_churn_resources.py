import unittest

from soak_churn_resources import evaluate_churn_resources


class ChurnResourceTests(unittest.TestCase):
    def rows(self):
        return [dict(observation_started_s=9.4, observed_s=9.5,
                     runtime_available=True, rss_available=True,
                     live_heap_bytes=1000000, proxy_rss_bytes=2000000, gc_cycles=10),
                dict(observation_started_s=7210.5, observed_s=7210.6,
                     runtime_available=True, rss_available=True,
                     live_heap_bytes=2200000, proxy_rss_bytes=4400000, gc_cycles=20)]

    def evaluate(self, rows, mappings=None):
        return evaluate_churn_resources(rows, started_s=10, duration_s=7200, elapsed_s=7200,
            mapping_evidence=mappings or dict(initial_pairs=10000, final_pairs=22000,
                                              persisted_churn_pairs=12000))

    def test_reports_observed_growth_per_persisted_new_mapping(self):
        result = self.evaluate(self.rows())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(result["new_mappings"], 12000)
        self.assertEqual(result["live_heap_bytes_per_new_mapping"], 100)
        self.assertEqual(result["proxy_rss_bytes_per_new_mapping"], 200)
        self.assertFalse(result["performance_qualified"])

    def test_missing_or_distant_boundary_samples_are_rejected(self):
        for rows in (self.rows()[:1], self.rows()[1:]):
            with self.assertRaises(ValueError): self.evaluate(rows)
        rows = self.rows(); rows[0]["observed_s"] = 7; rows[0]["observation_started_s"] = 6.9
        with self.assertRaises(ValueError): self.evaluate(rows)

    def test_unavailable_and_nonfinite_measurements_cannot_supply_growth(self):
        for key, value in (("runtime_available", False), ("rss_available", False),
                           ("live_heap_bytes", float("nan")), ("proxy_rss_bytes", True)):
            rows = self.rows(); rows[0][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): self.evaluate(rows)

    def test_mapping_denominator_must_match_original_persisted_churn(self):
        with self.assertRaises(ValueError):
            self.evaluate(self.rows(), dict(initial_pairs=10000, final_pairs=21999,
                                             persisted_churn_pairs=11999))

    def test_negative_net_growth_is_reported_without_clamping_or_leak_claim(self):
        rows = self.rows(); rows[1]["live_heap_bytes"] = 880000
        result = self.evaluate(rows)
        self.assertEqual(result["live_heap_bytes_per_new_mapping"], -10)
        self.assertEqual(result["state"], "pass")
