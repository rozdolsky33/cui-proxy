import copy
import unittest

from soak_resource_acceptance import evaluate_fixed_resources, evaluate_idle_recovery


class ResourceAcceptanceTests(unittest.TestCase):
    def rows(self):
        return [dict(event="resource", slot=i, observed_s=100+i,
                     rss_available=True, runtime_available=True,
                     proxy_rss_bytes=1000000, child_rss_bytes=2000000,
                     live_heap_bytes=500000, goroutines=10, gc_cycles=i//100)
                for i in range(7200)]

    def test_complete_stable_fixed_phase(self):
        result = evaluate_fixed_resources(self.rows(), started_s=100, duration_s=7200)
        self.assertEqual(result["state"], "pass")
        self.assertFalse(result["performance_qualified"])
        self.assertEqual(result["observed_slots"], 7200)

    def test_missing_duplicate_unavailable_nonfinite_and_short_rejected(self):
        original = self.rows()
        cases = [original[:-1], original + [original[0]]]
        for field, value in [("rss_available", False), ("runtime_available", False),
                             ("proxy_rss_bytes", float("nan")), ("live_heap_bytes", True),
                             ("observed_s", float("inf"))]:
            rows = copy.deepcopy(original)
            rows[5][field] = value
            cases.append(rows)
        for rows in cases:
            with self.subTest(case=len(rows)):
                with self.assertRaises(ValueError):
                    evaluate_fixed_resources(rows, started_s=100, duration_s=7200)
        with self.assertRaises(ValueError):
            evaluate_fixed_resources(original, started_s=100, duration_s=150)

    def test_memory_caps_and_fixed_growth(self):
        rows = self.rows()
        rows[40]["child_rss_bytes"] = 1024**3 + 1
        self.assertEqual(evaluate_fixed_resources(rows, started_s=100, duration_s=7200)["state"], "fail")
        rows = self.rows()
        for row in rows[-900:]:
            row["live_heap_bytes"] = 550001
        result = evaluate_fixed_resources(rows, started_s=100, duration_s=7200)
        self.assertEqual(result["state"], "fail")
        self.assertTrue(result["positive_growth_requires_review"])

    def test_small_positive_growth_retains_investigation(self):
        rows = self.rows()
        for row in rows[-900:]:
            row["proxy_rss_bytes"] += 1
        result = evaluate_fixed_resources(rows, started_s=100, duration_s=7200)
        self.assertEqual(result["state"], "pass")
        self.assertTrue(result["positive_growth_requires_review"])

    def test_internal_slot_gaps_and_delayed_samples_rejected(self):
        for mutate in (lambda rows: rows.pop(3000),
                       lambda rows: rows[3000].update(observed_s=rows[2999]["observed_s"]),
                       lambda rows: rows[3000].update(slot=9000)):
            rows = self.rows()
            mutate(rows)
            with self.assertRaises(ValueError):
                evaluate_fixed_resources(rows, started_s=100, duration_s=7200)

    def test_exact_caps_and_growth_boundary(self):
        rows = self.rows()
        rows[10]["proxy_rss_bytes"] = 512 * 1024**2
        rows[10]["child_rss_bytes"] = 1024**3
        for row in rows[-900:]:
            row["live_heap_bytes"] = 550000
        self.assertEqual(evaluate_fixed_resources(rows, started_s=100, duration_s=7200)["state"], "pass")
        rows[10]["proxy_rss_bytes"] += 1
        self.assertEqual(evaluate_fixed_resources(rows, started_s=100, duration_s=7200)["state"], "fail")

    def test_idle_recovery_requires_equivalent_gc_and_deadline(self):
        before = dict(goroutines=10, active_requests=0, runtime_available=True,
                      activity_available=True, elapsed_s=1)
        after = dict(before, goroutines=15, elapsed_s=60)
        gc = dict(before, procedure="two_observed_idle_gc_cycles", initial_gc_cycles=4,
                  gc_cycles=6, cycles_observed=2)
        self.assertEqual(evaluate_idle_recovery(before, after, gc, gc)["state"], "pass")
        for bad in [dict(after, goroutines=16), dict(after, elapsed_s=60.01)]:
            self.assertEqual(evaluate_idle_recovery(before, bad, gc, gc)["state"], "fail")
        with self.assertRaises(ValueError):
            evaluate_idle_recovery(before, after, gc, dict(gc, cycles_observed=1))
