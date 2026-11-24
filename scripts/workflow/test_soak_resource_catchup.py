import unittest
from unittest.mock import patch

import soak_resources as resources
from test_soak_resources import metrics


class Clock:
    def __init__(self):
        self.now = 1000.0
    def __call__(self):
        return self.now


class Stop:
    def __init__(self, clock):
        self.clock = clock
        self.stopped = False
    def set(self):
        self.stopped = True
    def wait(self, seconds):
        if self.stopped:
            return True
        self.clock.now += seconds
        return False


class Thread:
    def __init__(self, *, target, **_kwargs):
        self.target = target
    def start(self):
        pass
    def join(self, *_args, **_kwargs):
        pass
    def is_alive(self):
        return False


class ResourceCatchupTests(unittest.TestCase):
    def check_overrun(self, slow_call):
        clock = Clock()
        rows = []
        calls = []
        monitor = None
        def measure():
            calls.append(clock.now)
            clock.now += 1.17 if len(calls) == slow_call else 0.01
            return {"available": True, "proxy_rss_bytes": 100,
                    "child_rss_bytes": 0, "metrics": metrics()}
        def record(row):
            rows.append(row)
            if len(rows) == 4:
                monitor.stop.set()
        monitor = resources.ResourceMonitor(measure, record, clock=clock)
        monitor.stop = Stop(clock)
        with patch.object(resources.threading, "Thread", Thread):
            with monitor:
                monitor.thread.target()
        self.assertIsNone(monitor.failure)
        self.assertEqual(len(calls), 4)
        self.assertEqual([row["slot"] for row in rows], [0, 1, 2, 3])
        self.assertEqual([row["observation_started_s"] for row in rows], calls)
        self.assertAlmostEqual(rows[slow_call - 1]["observed_s"] - calls[slow_call - 1], 1.17)
        self.assertAlmostEqual(calls[slow_call], rows[slow_call - 1]["observed_s"])

    def test_poll_overrun_collects_next_real_nominal_slot_instead_of_skipping_it(self):
        self.check_overrun(2)

    def test_slow_initial_poll_does_not_skip_first_background_slot(self):
        self.check_overrun(1)
