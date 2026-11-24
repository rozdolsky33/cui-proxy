import threading
import unittest
from unittest.mock import Mock, patch

import soak_controls as controls
from soak_lifecycle import lifecycle_step
from soak_schedule import plan_request
from test_soak_controls import success


def scheduled(scenario, stage=None):
    for serial in range(1000):
        planned = plan_request(0, serial, "fixed")
        if planned["scenario"] == scenario and (stage is None or lifecycle_step(planned)["stage"] == stage):
            return planned
    raise AssertionError("scheduled case unavailable")


class ControlDispatchTests(unittest.TestCase):
    def test_outer_dispatch_treats_supported_lifecycle_text_as_live_text_at_restart(self):
        ready = {"W07": threading.Event(), "W09": threading.Event()}
        release = threading.Event()
        failures = []
        now = [0.0]
        def execute(item):
            if item["scenario"] in ready:
                ready[item["scenario"]].set()
                if not release.wait(5):
                    raise RuntimeError("test dispatch release timeout")
            return success()
        env = Mock()
        env.metrics.return_value = {"active_requests": 2}
        runner = controls.ControlledExecutor(execute, env, Mock(), lambda row: None,
            restart_times=[1], reload_times=[], clock=lambda: now[0])
        runner.image_checkpoint = lambda completed: {"image_records": completed,
                                                    "expected_image_records": completed}
        def dispatch(item):
            try:
                runner(item)
            except BaseException as error:
                failures.append(type(error).__name__)
        threads = [threading.Thread(target=dispatch, args=(scheduled(case),)) for case in ready]
        checkpoints = []
        def restart(_env, _index, *, continuation, before_signal=None):
            self.assertIsNotNone(before_signal)
            checkpoints.append(before_signal())
            continuation()
            return {"graceful_exit": True, "ready": True, "continuation_verified": True}
        try:
            for thread in threads:
                thread.start()
            for event in ready.values():
                self.assertTrue(event.wait(2))
            now[0] = 2.0
            with patch.object(controls, "restart_proxy", side_effect=restart) as restarted:
                runner(scheduled("W10", "continue"))
            self.assertEqual(restarted.call_count, 1)
            self.assertEqual(len(checkpoints), 1)
            self.assertEqual(checkpoints[0]["admitted_text_operations"], 2)
            self.assertEqual(checkpoints[0]["admitted_image_operations"], 0)
            self.assertEqual(checkpoints[0]["admitted_other_operations"], 0)
            self.assertEqual(checkpoints[0]["active_requests"], 2)
        finally:
            release.set()
            for thread in threads:
                thread.join(2)
        self.assertFalse(failures)
        self.assertTrue(all(not thread.is_alive() for thread in threads))
        self.assertEqual(runner.active, 0)
        self.assertEqual(sum(runner.active_categories.values()), 0)
