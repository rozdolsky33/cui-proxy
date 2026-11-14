import threading
import unittest
from unittest.mock import patch

from soak_phase import run_phase
from soak_resources import ResourceMonitor


class TimelineTests(unittest.TestCase):
    def test_phase_report_retains_actual_monotonic_origin(self):
        lock = threading.Lock()
        now = [500.0]
        def clock():
            with lock:
                value = now[0]
                now[0] += 0.01
                return value
        def execute(plan):
            return {"completed": True, "outcome": "supported_success", "supported": True,
                    "semantic_verified": True, "boundary_verified": True,
                    "upstream_receipt_verified": True}
        report = run_phase(execute, "fixed", duration=0.2, clock=clock)
        self.assertEqual(report["started_s"], 500.0)
        self.assertTrue(report["completed"])

    def test_resource_sample_retains_the_same_clock_origin(self):
        rows = []
        monitor = ResourceMonitor(lambda: {}, rows.append, clock=lambda: 101.0)
        monitor.started = 100.0
        with patch("soak_resources.resource_metadata", return_value={"runtime_available": True}):
            monitor._take(1)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["observed_s"], 101.0)
        self.assertEqual(rows[0]["elapsed_s"], 1.0)
