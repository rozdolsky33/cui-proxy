import json
import os
import tempfile
import threading
import unittest
from pathlib import Path
from unittest.mock import patch

import soak_evidence as evidence


class EvidenceTests(unittest.TestCase):
    def test_records_survive_close_with_sequence_and_private_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer:
                writer({"event": "assigned", "identity": ["fixed", 0, 0]})
                writer({"event": "completed", "identity": ["fixed", 0, 0], "request": {"outcome": "supported_success"}})
            report = evidence.read_evidence(path)
            self.assertTrue(report["framing_complete"])
            self.assertEqual(len(report["events"]), 2)
            self.assertEqual(os.stat(path).st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError): evidence.EvidenceWriter(path)

    def test_parallel_writers_have_one_order(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer:
                threads = [threading.Thread(target=lambda client=c: [writer({"event": "assigned", "identity": ["fixed", client, i]}) for i in range(20)]) for c in range(4)]
                for thread in threads: thread.start()
                for thread in threads: thread.join()
            self.assertEqual(len(evidence.read_evidence(path)["events"]), 80)

    def test_partial_tail_is_retained_but_never_complete(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer: writer({"event": "assigned", "identity": ["fixed", 0, 0]})
            with path.open("ab") as stream: stream.write(b'{"interrupted":')
            report = evidence.read_evidence(path)
            self.assertFalse(report["framing_complete"])
            self.assertEqual(len(report["events"]), 1)
            self.assertGreater(report["trailing_bytes"], 0)

    def test_complete_record_tampering_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer: writer({"event": "assigned", "identity": ["fixed", 0, 0]})
            row = json.loads(path.read_text()); row["event"]["identity"][1] = 3
            path.write_text(json.dumps(row) + "\n")
            with self.assertRaises(ValueError): evidence.read_evidence(path)

    def test_private_payload_nonfinite_and_oversize_are_rejected_before_write(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer:
                for event in [{"body": "private"}, {"duration_ms": float("nan")}, {"event": "assigned", "padding": "x" * 200000}]:
                    with self.assertRaises(ValueError): writer(event)
            self.assertEqual(path.stat().st_size, 0)

    def test_fsync_failure_poison_writer_and_prevents_following_records(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer:
                with patch("soak_evidence.os.fsync", side_effect=OSError("injected")):
                    with self.assertRaises(OSError): writer({"event": "assigned", "identity": ["fixed", 0, 0]})
                with self.assertRaises(ValueError): writer({"event": "assigned", "identity": ["fixed", 0, 1]})

    def test_seal_detects_deleted_final_record_and_exposes_incomplete_run(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            seal = Path(directory) / "seal.json"
            with evidence.EvidenceWriter(path) as writer:
                writer({"event": "assigned", "identity": ["fixed", 0, 0]})
                writer({"event": "completed", "identity": ["fixed", 0, 0]})
                writer.seal(seal, completed=False)
                with self.assertRaises(ValueError): writer({"event": "late"})
            self.assertFalse(evidence.read_sealed_evidence(path, seal)["run_completed"])
            path.write_bytes(path.read_bytes().splitlines(keepends=True)[0])
            with self.assertRaises(ValueError): evidence.read_sealed_evidence(path, seal)

    def test_missing_seal_never_proves_complete_run(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            with evidence.EvidenceWriter(path) as writer: writer({"event": "assigned"})
            with self.assertRaises(FileNotFoundError): evidence.read_sealed_evidence(path, Path(directory) / "missing")

    def test_phase_writes_every_assignment_and_completion_before_sealing(self):
        import itertools
        from soak_recorded import run_recorded_phase
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "phase"
            clock = itertools.count(step=0.01).__next__
            report = run_recorded_phase(lambda plan: {"outcome": "supported_success", "completed": True}, "fixed", path, duration=1, clock=clock)
            saved = evidence.read_sealed_evidence(path / "events.jsonl", path / "seal.json")
            self.assertTrue(saved["run_completed"])
            self.assertGreater(len(saved["events"]), 0)
            assigned = [row["identity"] for row in saved["events"] if row["event"] == "assigned"]
            completed = [row["identity"] for row in saved["events"] if row["event"] == "completed"]
            self.assertCountEqual(assigned, completed)
            self.assertEqual(len(completed), len(report["results"]))

    def test_failed_phase_retains_evidence_and_incomplete_seal(self):
        import itertools
        from soak_phase import PhaseFailure
        from soak_recorded import run_recorded_phase
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "phase"
            def fail(plan): raise ValueError("injected")
            with self.assertRaises(PhaseFailure):
                run_recorded_phase(fail, "fixed", path, duration=1, clock=itertools.count(step=0.01).__next__)
            saved = evidence.read_sealed_evidence(path / "events.jsonl", path / "seal.json")
            self.assertFalse(saved["run_completed"])
            self.assertGreater(len(saved["events"]), 0)
