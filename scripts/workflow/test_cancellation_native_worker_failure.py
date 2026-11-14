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
