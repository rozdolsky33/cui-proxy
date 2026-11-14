import threading
import unittest

from soak_phase import PhaseFailure, run_phase


class PhaseTests(unittest.TestCase):
    def clock(self):
        lock = threading.Lock()
        tick = [-1]
        def now():
            with lock:
                tick[0] += 1
                return tick[0] / 10
        return now

    def test_four_clients_record_every_assignment_and_result(self):
        barrier = threading.Barrier(4, timeout=2)
        events = []
        active = set()
        lock = threading.Lock()
        def execute(plan):
            client = plan["identity"][1]
            with lock:
                self.assertNotIn(client, active)
                active.add(client)
            if plan["identity"][2] == 0:
                barrier.wait()
            with lock:
                active.remove(client)
            return {"outcome": "supported_success", "completed": True}
        report = run_phase(execute, "fixed", duration=1, clock=self.clock(), record=events.append)
        self.assertTrue(report["completed"])
        self.assertEqual({row["identity"][1] for row in report["schedule"]}, {0, 1, 2, 3})
        identities = {tuple(row["identity"]) for row in report["schedule"]}
        self.assertEqual(identities, {tuple(row["identity"]) for row in report["results"]})
        self.assertEqual(len(events), 2 * len(identities))
        for identity in identities:
            self.assertEqual([event["event"] for event in events if tuple(event["identity"]) == identity], ["assigned", "completed"])

    def test_execution_failure_retains_evidence_and_stops(self):
        events = []
        def execute(plan):
            raise ValueError("private payload must stay out of report")
        with self.assertRaises(PhaseFailure) as caught:
            run_phase(execute, "churn", duration=7200, clock=self.clock(), record=events.append)
        report = caught.exception.report
        self.assertFalse(report["completed"])
        self.assertGreater(len(report["schedule"]), 0)
        self.assertEqual(len(report["schedule"]), len(report["results"]))
        self.assertNotIn("private payload", str(report))
        self.assertTrue(all(row["request"]["outcome"] == "harness_failure" for row in report["results"]))

    def test_external_stop_is_incomplete_without_starting_requests(self):
        stopped = threading.Event()
        stopped.set()
        report = run_phase(lambda _: self.fail("unexpected request"), "fixed", stop=stopped)
        self.assertFalse(report["completed"])
        self.assertEqual(report["schedule"], [])

    def test_record_failure_prevents_unrecorded_execution(self):
        calls = []
        def record(event):
            raise OSError("private location")
        with self.assertRaises(PhaseFailure) as caught:
            run_phase(lambda row: calls.append(row), "fixed", record=record)
        self.assertEqual(calls, [])
        self.assertFalse(caught.exception.report["completed"])
        self.assertNotIn("private location", str(caught.exception.report))

    def test_normal_completion_does_not_set_shared_external_stop(self):
        stop = threading.Event()
        report = run_phase(lambda _: {"outcome": "supported_success"}, "fixed", duration=1, clock=self.clock(), stop=stop)
        self.assertTrue(report["completed"])
        self.assertFalse(stop.is_set())


class ReplayPhaseTests(unittest.TestCase):
    def test_replay_runs_exact_saved_requests_in_each_client_order(self):
        from soak_journal import SoakJournal
        from soak_phase import run_replay
        journal = SoakJournal("churn")
        for serial in range(3):
            for client in range(4):
                row = journal.claim(client, 60 + serial)
                journal.complete(row["identity"])
        saved = journal.snapshot()
        seen = [[] for _ in range(4)]
        def execute(plan):
            seen[plan["identity"][1]].append(plan)
            return {"outcome": "supported_success"}
        report = run_replay(execute, saved, "churn")
        self.assertTrue(report["completed"])
        self.assertEqual(len(report["results"]), len(saved))
        for client in range(4):
            self.assertEqual(seen[client], [row for row in saved if row["identity"][1] == client])

    def test_invalid_replay_is_rejected_before_execution(self):
        from soak_phase import run_replay
        calls = []
        with self.assertRaises(ValueError):
            run_replay(lambda plan: calls.append(plan), [{"scenario": "invalid"}], "fixed")
        self.assertEqual(calls, [])
