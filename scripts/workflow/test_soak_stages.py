import threading
import unittest
from types import SimpleNamespace
from unittest.mock import MagicMock

from soak_stages import SoakStageCollector


class SoakStagesTests(unittest.TestCase):
    def setup_case(self, record):
        row = {"time": "2026-11-14T00:00:00Z", "identity": {"session": "a" * 32},
               "stages_ms": {"extraction_run": 1.0}, "extraction_cache_hits": 0}
        env = SimpleNamespace(metrics=MagicMock(return_value={"recent_requests": [row]}))
        stop = threading.Event()
        clock = MagicMock(side_effect=[100.0, 101.0, 102.0])
        collector = SoakStageCollector(env, set(), record=record, phase_stop=stop, clock=clock)
        return collector, env, stop

    def test_first_observation_is_persisted_once_on_monotonic_clock(self):
        events = []
        collector, env, stop = self.setup_case(events.append)
        collector.sample()
        collector.sample()
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event"], "request_stages")
        self.assertEqual(events[0]["observed_s"], 100.0)
        self.assertEqual(collector.records[0]["observed_s"], 100.0)
        self.assertFalse(stop.is_set())

    def test_evidence_failure_stops_further_admissions(self):
        collector, env, stop = self.setup_case(MagicMock(side_effect=OSError("storage failure")))
        collector.sample()
        self.assertTrue(stop.is_set())
        self.assertIsNotNone(collector.failure)

    def test_poll_error_is_retained_without_raw_exception_text(self):
        events = []
        collector, env, stop = self.setup_case(events.append)
        env.metrics.side_effect = OSError("private diagnostic")
        collector.sample()
        self.assertEqual(collector.errors, 1)
        self.assertEqual(events[0]["event"], "stage_poll_error")
        self.assertNotIn("private diagnostic", str(events))
