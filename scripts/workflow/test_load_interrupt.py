import concurrent.futures
import threading
import time
import types
import unittest
from unittest import mock

import performance_load as load


class MixedInterruptTests(unittest.TestCase):
    def test_interrupt_stops_dispatch_before_joining_workers(self):
        for interruption in (KeyboardInterrupt, SystemExit):
            with self.subTest(interruption=interruption.__name__):
                stop = threading.Event()
                release = threading.Event()
                ready = threading.Event()
                lock = threading.Lock()
                calls = []
                stopped_before_join = []
                waiting_thread = threading.current_thread()
                original_result = concurrent.futures.Future.result
                interrupted = False

                def operation(*args):
                    with lock:
                        calls.append(1)
                        if len(calls) == 4:
                            ready.set()
                    if not release.wait(5):
                        raise RuntimeError("worker_release_timeout")
                    time.sleep(0.001)
                    return {"outcome": "success"}

                class TrackingExecutor(concurrent.futures.ThreadPoolExecutor):
                    def __exit__(self, *args):
                        stopped_before_join.append(stop.is_set())
                        release.set()
                        return super().__exit__(*args)

                def interrupt_result(future, *args, **kwargs):
                    nonlocal interrupted
                    if threading.current_thread() is waiting_thread and not interrupted:
                        self.assertTrue(ready.wait(5), "workers did not start")
                        interrupted = True
                        raise interruption()
                    return original_result(future, *args, **kwargs)

                with mock.patch.object(load, "threading", types.SimpleNamespace(
                        Event=lambda: stop, Barrier=threading.Barrier, Lock=threading.Lock)), \
                        mock.patch.object(load.concurrent.futures, "ThreadPoolExecutor", TrackingExecutor), \
                        mock.patch.object(concurrent.futures.Future, "result", interrupt_result):
                    with self.assertRaises(interruption):
                        load.mixed_closed_loop(operation, text_count=16)
                self.assertEqual(stopped_before_join, [True])
                self.assertEqual(len(calls), 4)


    def test_interrupt_during_submission_releases_start_barrier(self):
        stop = threading.Event()
        barrier = threading.Barrier(5)
        before_join = []
        submitted = 0

        class InterruptExecutor(concurrent.futures.ThreadPoolExecutor):
            def submit(self, *args, **kwargs):
                nonlocal submitted
                submitted += 1
                if submitted == 2:
                    raise KeyboardInterrupt()
                return super().submit(*args, **kwargs)

            def __exit__(self, *args):
                before_join.append((stop.is_set(), barrier.broken))
                # Test-only fallback prevents a broken implementation hanging CI.
                barrier.abort()
                return super().__exit__(*args)

        thread_module = types.SimpleNamespace(
            Event=lambda: stop, Barrier=lambda *args, **kwargs: barrier, Lock=threading.Lock)
        with mock.patch.object(load, "threading", thread_module), \
                mock.patch.object(load.concurrent.futures, "ThreadPoolExecutor", InterruptExecutor):
            with self.assertRaises(KeyboardInterrupt):
                load.mixed_closed_loop(lambda *args: {"outcome": "success"}, text_count=16)
        self.assertEqual(before_join, [(True, True)])


if __name__ == "__main__":
    unittest.main()
