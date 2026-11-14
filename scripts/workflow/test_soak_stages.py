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
        clock = MagicMock(side_effect=[100.0, 101.0, 102.0, 103.0, 104.0, 105.0])
        collector = SoakStageCollector(env, set(), record=record, phase_stop=stop, clock=clock)
        return collector, env, stop

    def test_first_observation_is_persisted_once_on_monotonic_clock(self):
        events = []
        collector, env, stop = self.setup_case(events.append)
        collector.sample()
        collector.sample()
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event"], "request_stages")
        self.assertEqual(events[0]["observed_s"], 101.0)
        self.assertEqual(collector.records[0]["observed_s"], 101.0)
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

    def test_new_stage_retains_conservative_monotonic_completion_interval(self):
        events = []
        collector, env, stop = self.setup_case(events.append)
        rows = env.metrics.return_value
        env.metrics.side_effect = [{"recent_requests": []}, rows]
        collector.clock.side_effect = [100.0, 101.0, 102.0, 103.0]
        collector.sample()
        collector.sample()
        self.assertEqual(events[0]["observation_lower_s"], 100.0)
        self.assertEqual(events[0]["observed_s"], 103.0)
        self.assertEqual(collector.records[0]["observation_lower_s"], 100.0)

    def test_failed_poll_preserves_the_last_successful_lower_bound(self):
        events = []
        collector, env, stop = self.setup_case(events.append)
        rows = env.metrics.return_value
        env.metrics.side_effect = [{"recent_requests": []}, OSError("poll unavailable"), rows]
        collector.clock.side_effect = [100.0, 101.0, 102.0, 103.0, 104.0, 105.0]
        collector.sample()
        collector.sample()
        collector.sample()
        observed = next(event for event in events if event["event"] == "request_stages")
        self.assertEqual(observed["observation_lower_s"], 100.0)
        self.assertEqual(observed["observed_s"], 105.0)

    def test_initial_snapshot_does_not_invent_a_completion_lower_bound(self):
        events = []
        collector, env, stop = self.setup_case(events.append)
        collector.sample()
        self.assertIsNone(events[0]["observation_lower_s"])
        self.assertIsNone(collector.records[0]["observation_lower_s"])

    def test_backwards_poll_clock_stops_admission(self):
        collector, env, stop = self.setup_case(lambda event: None)
        collector.clock.side_effect = [100.0, 99.0]
        collector.sample()
        self.assertTrue(stop.is_set())
        self.assertIsNotNone(collector.failure)
