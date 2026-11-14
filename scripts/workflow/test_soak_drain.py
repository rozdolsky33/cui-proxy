import unittest
from soak_journal import SoakJournal, replay_requests
from soak_schedule import churn_due_indices


class DrainTests(unittest.TestCase):
    def test_horizon_caps_arrivals_without_lowering_the_target(self):
        self.assertEqual(churn_due_indices(65, 98, 40, horizon=60), (98, 99))
        self.assertEqual(churn_due_indices(7201, 11998, 40, horizon=7200), (11998,11999))

    def test_default_cutoff_remains_unchanged(self):
        journal = SoakJournal("churn", duration=1)
        self.assertIsNone(journal.claim(0, 1))

    def test_drain_assigns_only_offered_pairs_and_stops_when_assigned(self):
        journal = SoakJournal("churn", duration=1, drain_seconds=2)
        first = journal.claim(0, 0); journal.complete(first["identity"])
        last = journal.claim(0, 1.8); journal.complete(last["identity"])
        self.assertEqual(first["churn_indices"], [0])
        self.assertFalse(first["drain"])
        self.assertEqual(last["churn_indices"], [1])
        self.assertTrue(last["drain"])
        self.assertIsNone(journal.claim(1, 1.9))
        replay = replay_requests(journal.snapshot(), "churn", 1, drain_seconds=2)
        self.assertEqual(len(replay[0]), 2)
        with self.assertRaises(ValueError): replay_requests(journal.snapshot(), "churn", 1)

    def test_drain_keeps_image_and_lifecycle_slots_in_the_schedule(self):
        journal = SoakJournal("churn", duration=1, drain_seconds=2)
        for _ in range(7):
            row = journal.claim(0, 0); journal.complete(row["identity"])
        rows = []
        for elapsed in [1.1,1.2,1.3,1.4]:
            row = journal.claim(0, elapsed); journal.complete(row["identity"]); rows.append(row)
        self.assertEqual([row["category"] for row in rows], ["image","image","lifecycle","text"])
        self.assertEqual([row["churn_indices"] for row in rows], [[],[],[],[1]])
        self.assertIsNone(journal.claim(0, 1.5))
        replay_requests(journal.snapshot(), "churn", 1, drain_seconds=2)

    def test_deadline_is_bounded_and_invalid_settings_fail(self):
        journal = SoakJournal("churn", duration=1, drain_seconds=2)
        self.assertIsNone(journal.claim(0, 3))
        for phase, duration in [("fixed",1),("churn",46),("churn",True),("churn",float("nan")),("churn",-1)]:
            with self.assertRaises(ValueError): SoakJournal(phase, duration=1, drain_seconds=duration)

    def test_replay_rejects_falsified_drain_marker(self):
        journal = SoakJournal("churn", duration=1, drain_seconds=2)
        row = journal.claim(0, 1.2); journal.complete(row["identity"])
        records = journal.snapshot(); records[0]["drain"] = False
        with self.assertRaises(ValueError): replay_requests(records,"churn",1,drain_seconds=2)

    def test_phase_records_original_horizon_and_completed_drain(self):
        from soak_phase import run_phase
        now = [0.0]
        def execute(plan):
            now[0] = 1.1
            return {"outcome": "supported_success", "completed": True}
        report = run_phase(execute, "churn", duration=1, drain_seconds=2, clock=lambda: now[0])
        self.assertTrue(report["completed"])
        self.assertTrue(report["churn_drain_completed"])
        self.assertEqual(report["duration_s"], 1)
        self.assertEqual(report["churn_due_pairs"], 2)
        self.assertEqual(report["churn_assigned_pairs"], 2)
        self.assertEqual(report["drain_requests"], 1)

    def test_exhausted_drain_deadline_is_incomplete(self):
        from soak_phase import run_phase, PhaseFailure
        now = [0.0]
        def execute(plan):
            now[0] = 4.0
            return {"outcome": "supported_success", "completed": True}
        with self.assertRaises(PhaseFailure) as caught:
            run_phase(execute, "churn", duration=1, drain_seconds=2, clock=lambda: now[0])
        self.assertFalse(caught.exception.report["completed"])
        self.assertFalse(caught.exception.report["churn_drain_completed"])
        self.assertEqual(caught.exception.report["drain_failure"], "churn_backlog")

    def test_recorded_replay_consumes_exact_post_horizon_requests(self):
        import tempfile
        from pathlib import Path
        from soak_recorded import run_recorded_phase
        from soak_evidence import read_sealed_evidence
        journal = SoakJournal("churn", duration=1, drain_seconds=2)
        for client in range(4):
            row = journal.claim(client, 0); journal.complete(row["identity"])
        row = journal.claim(0, 1.1); journal.complete(row["identity"])
        seen = []
        def execute(plan):
            seen.append(plan["identity"])
            return {"outcome": "supported_success", "completed": True}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "phase"
            report = run_recorded_phase(execute, "churn", root, duration=1, drain_seconds=2, replay=journal.snapshot(), clock=lambda: 0)
            saved = read_sealed_evidence(root / "events.jsonl", root / "seal.json")
        self.assertTrue(saved["run_completed"])
        self.assertEqual(len(seen), 5)
        self.assertEqual(report["drain_requests"], 1)
        self.assertTrue(report["churn_drain_completed"])

    def test_replay_requires_boolean_drain_marker(self):
        journal = SoakJournal("churn", duration=1, drain_seconds=2)
        row = journal.claim(0, 1.2); journal.complete(row["identity"])
        records = journal.snapshot(); records[0]["drain"] = 1
        with self.assertRaises(ValueError): replay_requests(records,"churn",1,drain_seconds=2)
