import inspect
import threading
import time
import unittest
from unittest.mock import Mock

import soak_controls as controls
import soak_recovery as recovery
from soak_stages import SoakStageCollector as StageCollector
from soak_schedule import plan_request
from soak_lifecycle import lifecycle_step

def plan(serial):
    for sequence in range(serial, serial + 1000):
        candidate = plan_request(0, sequence, "churn")
        if candidate["scenario"] == "W10" and lifecycle_step(candidate)["stage"] == "continue":
            return candidate
    raise AssertionError("scheduled continuation unavailable")

import test_soak_recovery as recovery_cases


class RestartStageGuardTests(unittest.TestCase):
    def runner(self):
        now = [0.0]
        runner = controls.ControlledExecutor(Mock(), Mock(), Mock(), Mock(),
            restart_times=[1], reload_times=[], clock=lambda: now[0])
        runner.image_checkpoint = Mock()
        runner.active_categories = {"text": 1, "image": 0, "lifecycle": 0}
        runner.completed_images = 0
        runner.active = 1
        now[0] = 2.0
        return runner

    def test_due_restart_keeps_original_dispatch_running_while_any_image_is_active(self):
        runner = self.runner()
        runner.active_categories["image"] = 1
        runner.active += 1
        self.assertIsNone(runner._select(plan(0)))
        self.assertFalse(runner.restarting)

    def test_due_restart_does_not_mislabel_lifecycle_work_as_text_load(self):
        runner = self.runner()
        runner.active_categories["lifecycle"] = 1
        runner.active += 1
        self.assertIsNone(runner._select(plan(0)))
        self.assertFalse(runner.restarting)

    def test_natural_image_quiet_with_text_load_atomically_closes_existing_restart_guard(self):
        runner = self.runner()
        self.assertEqual(runner._select(plan(0)), "restart")
        self.assertTrue(runner.restarting)

    def test_image_ineligibility_keeps_original_plans_running_without_new_due_deadline(self):
        runner = self.runner()
        runner.active_categories["image"] = 1
        runner.active += 1
        self.assertIsNone(runner._select(plan(0)))
        runner.clock = lambda: 600.0
        self.assertIsNone(runner._select(plan(0)))
        self.assertFalse(runner.restarting)

    def collector(self, *, appears):
        row = {"identity": {"session": "measured"}, "time": "2026-11-24T00:00:00Z",
               "stages_ms": {"extraction_run": 1.0}, "extraction_cache_hits": 0}
        env = Mock()
        env.metrics.side_effect = [{"recent_requests": []}, {"recent_requests": []},
                                  {"recent_requests": [row]}] if appears else None
        env.metrics.return_value = {"recent_requests": []}
        now = [0.0]
        def clock():
            now[0] += 0.01
            return now[0]
        stopped = threading.Event()
        collector = StageCollector(env, set(), record=lambda row: None,
                                   phase_stop=stopped, clock=clock)
        collector.sample()
        return collector, stopped

    def checkpoint(self, collector, *, timeout=0.2):
        if "expected_image_completions" in inspect.signature(collector.before_restart).parameters:
            return collector.before_restart(expected_image_completions=1, timeout=timeout)
        return collector.before_restart()

    def test_checkpoint_waits_for_real_stage_defer_after_image_client_completion(self):
        collector, stopped = self.collector(appears=True)
        self.checkpoint(collector)
        self.assertEqual(len(collector.records), 1)
        self.assertFalse(stopped.is_set())

    def test_missing_completed_image_stage_is_fatal_not_an_acknowledged_shutdown_gap(self):
        collector, stopped = self.collector(appears=False)
        with self.assertRaises(RuntimeError):
            self.checkpoint(collector, timeout=0.05)
        self.assertTrue(stopped.is_set())

    def test_restart_records_fresh_signal_checkpoint_immediately_before_terminate(self):
        fixture = recovery_cases.RestartTests()
        env, old = fixture.env()
        events = []
        old.terminate.side_effect = lambda: events.append("terminate")
        def checkpoint():
            events.append("checkpoint")
            return {"active_text": 1, "active_images": 0, "active_lifecycle": 0,
                    "active_requests": 1, "observed_s": time.monotonic()}
        kwargs = {"before_signal": checkpoint} if "before_signal" in inspect.signature(recovery.restart_proxy).parameters else {}
        evidence = recovery.restart_proxy(env, 1, continuation=fixture.continuation, **kwargs)
        self.assertEqual(events[:2], ["checkpoint", "terminate"])
        self.assertEqual(evidence.get("signal_checkpoint", {}).get("active_text"), 1)
        self.assertLessEqual(evidence["signal_checkpoint"]["observed_s"], evidence["signal_started_s"])

    def test_lost_text_load_before_signal_prevents_any_process_termination(self):
        fixture = recovery_cases.RestartTests()
        env, old = fixture.env()
        def checkpoint():
            raise RuntimeError("restart_not_eligible")
        kwargs = {"before_signal": checkpoint} if "before_signal" in inspect.signature(recovery.restart_proxy).parameters else {}
        with self.assertRaises(RuntimeError):
            recovery.restart_proxy(env, 1, continuation=fixture.continuation, **kwargs)
        old.terminate.assert_not_called()


    def test_ordinary_due_to_continuation_cadence_does_not_consume_eligibility_deadline(self):
        runner = self.runner()
        runner.clock = lambda: 600.0
        self.assertEqual(runner._select(plan(0)), "restart")
