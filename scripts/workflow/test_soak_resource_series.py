import copy
import unittest
from soak_resource_series import evaluate_resource_series


class ResourceSeriesTests(unittest.TestCase):
    def rows(self):
        return [dict(event="resource", slot=i, elapsed_s=i+.1, observation_started_s=100+i,
                     observed_s=100+i+.1, rss_available=True, runtime_available=True,
                     activity_available=True, proxy_rss_bytes=1000000, child_rss_bytes=2000000,
                     heap_objects_bytes=500000, live_heap_bytes=500000, goroutines=10,
                     gc_cycles=2, active_requests=4) for i in range(5)]

    def check(self, rows, faults=None):
        return evaluate_resource_series(rows, started_s=100, ended_s=105,
                                        restart_intervals=[] if faults is None else faults)

    def test_complete_series_reports_availability_and_peaks(self):
        result = self.check(self.rows())
        self.assertEqual(result["state"], "pass")
        self.assertEqual(result["expected_slots"], 5)
        self.assertEqual(result["unavailable_fault_samples"], 0)
        self.assertFalse(result["performance_qualified"])

    def test_missing_slot_needs_an_explicit_restart_interval(self):
        rows = self.rows()
        rows.pop(2)
        with self.assertRaises(ValueError):
            self.check(rows)
        result = self.check(rows, [dict(started_s=101.5, ended_s=102.5)])
        self.assertEqual(result["missing_fault_slots"], 1)
        self.assertEqual(result["state"], "pass")

    def test_unavailable_sample_uses_real_observation_interval(self):
        rows = self.rows()
        rows[2].update(runtime_available=False, activity_available=False, active_requests=None,
                       heap_objects_bytes=None, live_heap_bytes=None, goroutines=None, gc_cycles=None)
        with self.assertRaises(ValueError):
            self.check(rows)
        result = self.check(rows, [dict(started_s=101.9, ended_s=102.05)])
        self.assertEqual(result["unavailable_fault_samples"], 1)
        with self.assertRaises(ValueError):
            self.check(rows, [dict(started_s=100, ended_s=101)])

    def test_known_rss_over_budget_still_fails_inside_restart(self):
        rows = self.rows()
        rows[2].update(rss_available=False, proxy_rss_bytes=512*1024**2+1, child_rss_bytes=None)
        self.assertEqual(self.check(rows, [dict(started_s=101.9, ended_s=102.2)])["state"], "fail")

    def test_corrupt_sampling_and_invalid_values_are_rejected(self):
        mutations = [lambda rows: rows.append(rows[0]),
                     lambda rows: rows[2].update(observed_s=float("nan")),
                     lambda rows: rows[2].update(observation_started_s=101),
                     lambda rows: rows[2].update(elapsed_s=99),
                     lambda rows: rows[2].update(slot=True),
                     lambda rows: rows[2].update(live_heap_bytes=True),
                     lambda rows: rows[2].update(observation_started_s=104)]
        for mutate in mutations:
            rows = copy.deepcopy(self.rows())
            mutate(rows)
            with self.assertRaises(ValueError):
                self.check(rows)

    def test_large_unexplained_horizon_fails_without_expanding_slot_range(self):
        with self.assertRaises(ValueError):
            evaluate_resource_series(self.rows(), started_s=100, ended_s=10**12,
                                     restart_intervals=[])
