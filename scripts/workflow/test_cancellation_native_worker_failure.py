"""Worker exceptions must remain visible while waiting for native evidence."""
import concurrent.futures
import unittest
from cancellation_native import NativeTrialDriver


class WorkerFailureTests(unittest.TestCase):
    def test_observation_propagates_finished_worker_failure(self):
        driver = NativeTrialDriver.__new__(NativeTrialDriver)
        failure = RuntimeError("worker_failed_before_transport")
        future = concurrent.futures.Future()
        future.set_exception(failure)
        driver.futures = {"active": future}
        with self.assertRaises(RuntimeError) as raised:
            driver.observe()
        self.assertIs(raised.exception, failure)

    def test_fixed_trial_plans_have_explicit_empty_churn_assignments(self):
        from cancellation_campaign import trial_plans
        for trial in range(3):
            for role, plan in trial_plans(trial).items():
                with self.subTest(trial=trial, role=role):
                    self.assertEqual(plan.get("churn_indices"), [])

    def test_receipts_use_result_operation_ids(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        driver = NativeTrialDriver.__new__(NativeTrialDriver)
        driver.env = SimpleNamespace(upstream_url="http://127.0.0.1:1")
        driver.clock = lambda: 1.0
        operations = {role: {"request": {"identity": ["fixed", 1, index]},
                             "result": {"operation_id": "probe-" + role}}
                      for index, role in enumerate(("queued", "active"))}
        with patch("cancellation_native.backend_evidence", return_value={}) as lookup:
            driver.receipts(operations)
        self.assertEqual([call.args[2] for call in lookup.call_args_list],
                         ["probe-queued", "probe-active"])
