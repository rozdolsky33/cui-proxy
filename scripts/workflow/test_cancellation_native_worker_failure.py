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
