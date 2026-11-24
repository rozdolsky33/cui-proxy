import unittest
from unittest.mock import Mock

import soak_resources as resources


def metrics(cycles=4, goroutines=10, active=0):
    return {"active_requests": active, "runtime": {"available": True, "heap_objects_bytes": 120, "live_heap_bytes": 100, "goroutines": goroutines, "gc_cycles": cycles}}


class ResourceTests(unittest.TestCase):
    def test_samples_keep_availability_and_only_declared_metadata(self):
        row = resources.resource_metadata({"proxy_rss_bytes": 1000, "child_rss_bytes": 200, "metrics": dict(metrics(), unrelated="discard")})
        self.assertTrue(row["rss_available"])
        self.assertTrue(row["runtime_available"])
        self.assertEqual(row["live_heap_bytes"], 100)
        self.assertNotIn("unrelated", row)
        missing = resources.resource_metadata({"proxy_rss_bytes": None, "child_rss_bytes": 0, "metrics": None})
        self.assertFalse(missing["rss_available"])
        self.assertFalse(missing["runtime_available"])
        self.assertIsNone(missing["live_heap_bytes"])

    def test_invalid_numeric_samples_are_not_reported_available(self):
        for value in [True, -1, float("nan"), "100"]:
            row = resources.resource_metadata({"proxy_rss_bytes": value, "child_rss_bytes": 0, "metrics": metrics()})
            self.assertFalse(row["rss_available"])
        sample = metrics(); sample["runtime"]["gc_cycles"] = True
        row = resources.resource_metadata({"metrics": sample})
        self.assertFalse(row["runtime_available"])

    def test_observed_gc_requires_cycle_advance_during_idle(self):
        observations = iter([metrics(4), metrics(5), metrics(6)])
        elapsed = [0.0]
        rows = []
        result = resources.observe_idle_gc(lambda: next(observations), timeout=10, clock=lambda: elapsed[0], sleep=lambda seconds: elapsed.__setitem__(0, elapsed[0]+seconds), record=rows.append)
        self.assertEqual(result["cycles_observed"], 2)
        self.assertEqual(result["live_heap_bytes"], 100)
        self.assertEqual(len(rows), 3)
        self.assertEqual(result["procedure"], "two_observed_idle_gc_cycles")

    def test_gc_cannot_pass_with_active_requests_or_cycle_reset(self):
        for sequence in [[metrics(4), metrics(6, active=1)], [metrics(4), metrics(1)]]:
            observations = iter(sequence); elapsed = [0.0]
            with self.assertRaises(ValueError):
                resources.observe_idle_gc(lambda: next(observations), timeout=10, clock=lambda: elapsed[0], sleep=lambda seconds: elapsed.__setitem__(0, elapsed[0]+seconds))

    def test_gc_timeout_retains_observations_and_never_relabels_old_heap(self):
        elapsed = [0.0]; rows = []
        with self.assertRaises(TimeoutError):
            resources.observe_idle_gc(lambda: metrics(), timeout=2, clock=lambda: elapsed[0], sleep=lambda seconds: elapsed.__setitem__(0, elapsed[0]+seconds), record=rows.append)
        self.assertGreaterEqual(len(rows), 2)

    def test_post_drain_requires_baseline_plus_five_within_deadline(self):
        observations = iter([metrics(goroutines=16), metrics(goroutines=15)])
        elapsed = [0.0]
        result = resources.wait_for_idle(lambda: next(observations), baseline=10, clock=lambda: elapsed[0], sleep=lambda seconds: elapsed.__setitem__(0, elapsed[0]+seconds))
        self.assertEqual(result["goroutines"], 15)
        self.assertEqual(result["elapsed_s"], 1)

    def test_observer_rejects_missing_runtime_and_invalid_deadline(self):
        with self.assertRaises(ValueError): resources.observe_idle_gc(lambda: {"active_requests": 0}, timeout=1)
        with self.assertRaises(ValueError): resources.wait_for_idle(lambda: metrics(), timeout=float("nan"))

    def test_monitor_records_measurement_failure_as_unavailable(self):
        rows = []
        measure = Mock(side_effect=OSError("private detail"))
        monitor = resources.ResourceMonitor(measure, rows.append, clock=lambda: 5.2)
        monitor.started = 0
        row = monitor._take(0)
        self.assertFalse(row["rss_available"])
        self.assertFalse(row["runtime_available"])
        self.assertEqual(row["sample_error"], "OSError")
        self.assertNotIn("private detail", str(rows))
        self.assertEqual(row["elapsed_s"], 5.2)
        self.assertEqual(resources.next_slot(0, 5.2, 1), 1)

    def test_record_failure_stops_the_phase_and_remains_visible(self):
        import threading
        stop = threading.Event()
        monitor = resources.ResourceMonitor(lambda: {}, Mock(side_effect=OSError("injected")), phase_stop=stop)
        monitor.started = monitor.clock()
        with self.assertRaises(OSError): monitor._take(0)
        self.assertTrue(stop.is_set())
        self.assertIsNotNone(monitor.failure)

    def test_background_monitor_stops_and_retains_real_sample_times(self):
        import threading
        rows = []; enough = threading.Event()
        def record(row):
            rows.append(row)
            if len(rows) >= 2: enough.set()
        with resources.ResourceMonitor(lambda: {"proxy_rss_bytes": 100, "child_rss_bytes": 0, "metrics": metrics()}, record, interval=0.01) as monitor:
            self.assertTrue(enough.wait(2))
        self.assertFalse(monitor.thread.is_alive())
        self.assertGreaterEqual(len(rows), 2)
        self.assertLess(rows[0]["elapsed_s"], rows[1]["elapsed_s"])
        self.assertTrue(all(row["runtime_available"] for row in rows))

    def test_resource_poll_retains_start_and_end_bounds(self):
        from soak_resources import ResourceMonitor
        events = []
        clock = iter([11.0, 11.25])
        monitor = ResourceMonitor(lambda: {}, events.append, clock=lambda: next(clock))
        monitor.started = 10.0
        self.assertTrue(monitor._take(1))
        self.assertEqual(events[0]["observation_started_s"], 11.0)
        self.assertEqual(events[0]["observed_s"], 11.25)
        self.assertEqual(events[0]["elapsed_s"], 1.25)

    def test_backwards_resource_poll_clock_stops_admission(self):
        import threading
        from soak_resources import ResourceMonitor
        events, stop = [], threading.Event()
        clock = iter([11.0, 10.5])
        monitor = ResourceMonitor(lambda: {}, events.append, phase_stop=stop, clock=lambda: next(clock))
        monitor.started = 10.0
        self.assertFalse(monitor._take(1))
        self.assertTrue(stop.is_set())
        self.assertIsInstance(monitor.failure, ValueError)
        self.assertEqual(events, [])
