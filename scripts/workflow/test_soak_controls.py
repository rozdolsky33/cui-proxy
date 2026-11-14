import threading
import unittest
from unittest.mock import Mock, patch

from soak_schedule import plan_request
import soak_controls as controls


def plan(serial):
    row = plan_request(0, serial, "fixed"); row["churn_indices"] = []
    return row


def success():
    return {"outcome": "supported_success", "completed": True, "semantic_verified": True, "boundary_verified": True, "old_substitutions_only": True}


class ControlTests(unittest.TestCase):
    def test_declared_schedule_has_ten_changes_inside_each_run(self):
        times = controls.control_times(14400)
        self.assertEqual(len(times), 10)
        self.assertGreater(times[0], 0)
        self.assertLess(times[-1], 14400)
        self.assertEqual(times, sorted(set(times)))

    def test_reload_requires_observed_load_and_does_not_add_request(self):
        now = [0.0]; env = Mock(); env.metrics.return_value = {"active_requests": 0}
        execute = Mock(return_value=success()); reloader = Mock(); rows = []
        runner = controls.ControlledExecutor(execute, env, reloader, rows.append, restart_times=[], reload_times=[1], clock=lambda: now[0])
        now[0] = 2; runner(plan(0))
        reloader.reload.assert_not_called()
        env.metrics.return_value = {"active_requests": 2}; reloader.reload.return_value = {"generation": 1, "reload_returned_success": True}
        runner(plan(1))
        self.assertEqual(execute.call_count, 2)
        reloader.reload.assert_called_once_with(1)
        self.assertEqual(runner.summary()["reloads_completed"], 1)
        self.assertTrue(any(row.get("active_requests") == 2 for row in rows))

    def test_restart_consumes_existing_continuation_and_waits_new_admissions(self):
        now = [0.0]; env = Mock(); env.metrics.return_value = {"active_requests": 1}
        active = threading.Event(); release = threading.Event(); restarting = threading.Event(); continue_restart = threading.Event(); late = threading.Event()
        calls = []
        def execute(item):
            calls.append(item["identity"])
            if item["identity"][2] == 0:
                active.set(); self.assertTrue(release.wait(3))
            if item["identity"][2] == 1: late.set()
            return success()
        runner = controls.ControlledExecutor(execute, env, Mock(), lambda row: None, restart_times=[1], reload_times=[], clock=lambda: now[0])
        errors = []
        def call(item):
            try: runner(item)
            except BaseException as error: errors.append(error)
        def restart(env, index, *, continuation):
            restarting.set(); self.assertTrue(continue_restart.wait(3)); release.set()
            continuation()
            return {"continuation_verified": True, "graceful_exit": True, "ready": True}
        first = threading.Thread(target=call, args=(plan(0),)); first.start(); self.assertTrue(active.wait(3)); now[0] = 2
        with patch("soak_controls.restart_proxy", side_effect=restart) as restart_mock:
            recovery = threading.Thread(target=call, args=(plan(79),)); recovery.start(); self.assertTrue(restarting.wait(3))
            following = threading.Thread(target=call, args=(plan(1),)); following.start()
            self.assertFalse(late.wait(0.02)); continue_restart.set()
            for thread in [first, recovery, following]: thread.join(3); self.assertFalse(thread.is_alive())
        self.assertEqual(errors, [])
        self.assertCountEqual(calls, [plan(i)["identity"] for i in [0, 79, 1]])
        self.assertEqual(restart_mock.call_count, 1)
        self.assertEqual(runner.summary()["restarts_completed"], 1)

    def test_evidence_failure_prevents_reload_mutation(self):
        now = [0.0]; env = Mock(); env.metrics.return_value = {"active_requests": 1}; reloader = Mock()
        runner = controls.ControlledExecutor(Mock(), env, reloader, Mock(side_effect=OSError("injected")), restart_times=[], reload_times=[1], clock=lambda: now[0])
        now[0] = 2
        with self.assertRaises(OSError): runner(plan(0))
        reloader.reload.assert_not_called()
        self.assertEqual(runner.summary()["reloads_completed"], 0)

    def test_restart_defers_if_an_admitted_request_has_not_reached_proxy(self):
        now = [0.0]; env = Mock(); env.metrics.return_value = {"active_requests": 1}
        execute = Mock(return_value=success())
        runner = controls.ControlledExecutor(execute, env, Mock(), lambda row: None, restart_times=[1], reload_times=[], clock=lambda: now[0])
        runner.active = 2
        now[0] = 2
        with patch("soak_controls.restart_proxy") as restart:
            runner(plan(79))
        restart.assert_not_called()
        execute.assert_called_once()
        self.assertEqual(runner.summary()["restarts_completed"], 0)

    def test_failed_restart_cannot_be_retried_by_waiting_clients(self):
        now = [0.0]; env = Mock(); env.metrics.return_value = {"active_requests": 1}; execute = Mock(return_value=success())
        runner = controls.ControlledExecutor(execute, env, Mock(), lambda row: None, restart_times=[1], reload_times=[], clock=lambda: now[0])
        runner.active = 1; now[0] = 2
        with patch("soak_controls.restart_proxy", return_value={"graceful_exit": True, "ready": True, "continuation_verified": True}) as restart:
            with self.assertRaises(ValueError): runner(plan(79))
            with self.assertRaises(ValueError): runner(plan(0))
        self.assertEqual(restart.call_count, 1)
        execute.assert_not_called()
        self.assertTrue(runner.summary()["failed"])

    def test_invalid_control_schedules_are_rejected(self):
        for values in [[True], [float("nan")], [-1], [1, 1], [2, 1]]:
            with self.assertRaises(ValueError):
                controls.ControlledExecutor(Mock(), Mock(), Mock(), Mock(), restart_times=values, reload_times=[])
