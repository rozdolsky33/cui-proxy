import threading
import time
import unittest

from soak_journal import SoakJournal
from soak_phase import PhaseFailure, run_replay


class ReplayClock:
    def __init__(self):
        self.value = 0.0
        self.waits = []
        self.lock = threading.Lock()

    def now(self):
        with self.lock:
            return self.value

    def advance(self, seconds):
        with self.lock:
            self.value += seconds

    def wait(self, seconds):
        with self.lock:
            self.waits.append(seconds)
            self.value += seconds


class ReplayPacingTests(unittest.TestCase):
    def schedule(self, duration, offsets, *, phase="fixed", drain_seconds=0):
        journal = SoakJournal(phase, duration=duration, drain_seconds=drain_seconds)
        for offset in offsets:
            for client in range(4):
                plan = journal.claim(client, offset)
                if plan is not None:
                    journal.complete(plan["identity"])
        return journal.snapshot()

    def test_first_request_and_events_wait_for_absolute_deadline(self):
        clock = ReplayClock()
        observed = []
        events = []

        def execute(plan):
            observed.append((plan["elapsed_s"], clock.now()))
            return {"outcome": "supported_success"}

        run_replay(execute, self.schedule(1.0, [0.2, 0.4]), "fixed",
                   duration=1.0, clock=clock.now, wait=clock.wait,
                   record=lambda event: events.append((event, clock.now())))
        self.assertEqual(len(observed), 8)
        self.assertTrue(all(actual >= planned for planned, actual in observed))
        self.assertTrue(all(actual >= event.get("elapsed_s", 0)
                            for event, actual in events))

    def test_overdue_work_adds_no_relative_delay(self):
        clock = ReplayClock()

        def execute(_plan):
            clock.advance(1.0)
            return {"outcome": "supported_success"}

        run_replay(execute, self.schedule(0.5, [0.0, 0.1]), "fixed",
                   duration=0.5, clock=clock.now, wait=clock.wait)
        self.assertEqual(clock.waits, [])

    def test_four_client_streams_overlap_and_keep_their_order(self):
        barrier = threading.Barrier(4)
        observed = {client: [] for client in range(4)}

        def execute(plan):
            observed[plan["identity"][1]].append(plan["elapsed_s"])
            barrier.wait(timeout=2)
            return {"outcome": "supported_success"}

        report = run_replay(execute, self.schedule(0.15, [0.0, 0.1]),
                            "fixed", duration=0.15)
        self.assertTrue(report["completed"])
        self.assertEqual(observed, {client: [0.0, 0.1] for client in range(4)})

    def test_cancel_during_wait_prevents_emission_and_execution(self):
        clock = ReplayClock()
        stop = threading.Event()
        events = []
        observed = []

        def wait(_seconds):
            stop.set()

        report = run_replay(lambda plan: observed.append(plan),
                            self.schedule(1.0, [0.2]), "fixed", duration=1.0,
                            clock=clock.now, wait=wait, stop=stop,
                            record=events.append)
        self.assertFalse(report["completed"])
        self.assertEqual(events, [])
        self.assertEqual(observed, [])

    def test_recorded_churn_drain_is_paced_after_nominal_horizon(self):
        clock = ReplayClock()
        observed = []

        def execute(plan):
            observed.append((plan["elapsed_s"], clock.now()))
            return {"outcome": "supported_success"}

        records = self.schedule(1.0, [0.0, 1.1], phase="churn", drain_seconds=1)
        report = run_replay(execute, records, "churn", duration=1.0,
                            drain_seconds=1, clock=clock.now, wait=clock.wait)
        self.assertTrue(report["completed"])
        self.assertTrue(any(planned > 1.0 for planned, _actual in observed))
        self.assertTrue(all(actual >= planned for planned, actual in observed))

    def test_execution_failure_stops_replay_without_waiting_for_horizon(self):
        clock = ReplayClock()
        events = []

        def execute(_plan):
            raise ValueError("execution failed")

        with self.assertRaises(PhaseFailure) as raised:
            run_replay(execute, self.schedule(10.0, [0.0, 5.0]), "fixed",
                       duration=10.0, clock=clock.now, wait=clock.wait,
                       record=events.append)
        self.assertTrue(raised.exception.report["failures"])
        self.assertFalse(raised.exception.report["completed"])
        self.assertLess(clock.now(), 5.0)
        self.assertTrue(events)

    def test_record_failure_does_not_execute_unrecorded_request(self):
        clock = ReplayClock()
        observed = []

        def record(_event):
            raise OSError("recording failed")

        with self.assertRaises(PhaseFailure) as raised:
            run_replay(lambda plan: observed.append(plan),
                       self.schedule(10.0, [0.0, 5.0]), "fixed", duration=10.0,
                       clock=clock.now, wait=clock.wait, record=record)
        self.assertFalse(raised.exception.report["completed"])
        self.assertEqual(observed, [])

    def test_fast_bypass_preserves_recorded_offsets_and_full_phase(self):
        duration = 0.24
        records = self.schedule(duration, [0.0, 0.12])
        started = time.monotonic()
        observed = []

        def execute(plan):
            observed.append((plan["elapsed_s"], time.monotonic() - started))
            return {"outcome": "supported_success", "completed": True}

        report = run_replay(execute, records, "fixed", duration=duration)
        self.assertTrue(report["completed"])
        self.assertEqual(len(observed), 8)
        self.assertGreaterEqual(report["elapsed_s"], duration)
        for planned, actual in observed:
            self.assertGreaterEqual(actual, planned)

    def test_external_stop_interrupts_wait_without_sending_future_plan(self):
        records = self.schedule(2.0, [0.0, 1.5])
        stop = threading.Event()
        observed = []

        def execute(plan):
            observed.append(plan["elapsed_s"])
            stop.set()
            return {"outcome": "supported_success", "completed": True}

        started = time.monotonic()
        report = run_replay(execute, records, "fixed", duration=2.0, stop=stop)
        self.assertFalse(report["completed"])
        self.assertLess(time.monotonic() - started, 1.0)
        self.assertTrue(observed)
        self.assertTrue(all(offset == 0.0 for offset in observed))


if __name__ == "__main__":
    unittest.main()
