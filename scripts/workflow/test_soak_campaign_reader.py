import json
import tempfile
import unittest
from pathlib import Path

from soak_campaign_evidence import CampaignEvidence
from soak_evidence import EvidenceWriter
from soak_campaign_reader import read_completed_campaign, REQUIRED_REPORTS


class CampaignReaderTests(unittest.TestCase):
    def campaign(self, directory, *, finish=True, omit=None):
        root = Path(directory) / "campaign"
        with CampaignEvidence(root, {"source": "test"}, {"fixed_seconds": 150}) as campaign:
            for name in ("proxy-fixed", "proxy-churn", "bypass-fixed", "bypass-churn"):
                child = root / name
                child.mkdir()
                with EvidenceWriter(child / "events.jsonl") as writer:
                    writer({"event": "phase_complete"})
                    writer.seal(child / "seal.json", completed=True)
                campaign.link_phase(name)
            for name in REQUIRED_REPORTS:
                if name != omit:
                    campaign.save_report(name, {"completed": True})
            if finish:
                campaign.finish()
        return root

    def test_reads_every_sealed_phase_and_registered_report(self):
        with tempfile.TemporaryDirectory() as directory:
            root = self.campaign(directory)
            result = read_completed_campaign(root)
            self.assertEqual(set(result["reports"]), set(REQUIRED_REPORTS))
            self.assertEqual(len(result["phases"]), 4)
            self.assertFalse(result["performance_qualified"])
            self.assertEqual(result["settings"]["fixed_seconds"], 150)

    def test_incomplete_root_does_not_become_a_complete_campaign(self):
        with tempfile.TemporaryDirectory() as directory:
            root = self.campaign(directory, finish=False)
            with self.assertRaises(ValueError):
                read_completed_campaign(root)

    def test_missing_registered_report_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = self.campaign(directory, omit="proxy-finalization.json")
            with self.assertRaises(ValueError):
                read_completed_campaign(root)

    def test_changed_report_and_phase_are_rejected(self):
        for name in ("proxy-finalization.json", "proxy-fixed/events.jsonl", "proxy-fixed/seal.json", "events.jsonl", "seal.json"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = self.campaign(directory)
                path = root / name
                path.write_bytes(path.read_bytes() + b" ")
                with self.assertRaises(ValueError):
                    read_completed_campaign(root)

    def test_symlinked_report_or_phase_directory_is_rejected(self):
        for kind in ("report", "phase", "root"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                root = self.campaign(directory)
                target = root / "proxy-finalization.json" if kind == "report" else root / "proxy-fixed"
                if kind == "root":
                    target = root
                moved = Path(directory) / "moved"
                target.rename(moved)
                target.symlink_to(moved, target_is_directory=kind != "report")
                with self.assertRaises(ValueError):
                    read_completed_campaign(root)

    def test_authenticated_duplicate_and_nonfinite_json_are_rejected(self):
        from soak_campaign_reader import read_report_bytes
        for value in (b'{"count":1,"count":2}', b'{"count":NaN}', b'{"count":1e999}'):
            with self.assertRaises(ValueError):
                read_report_bytes(value)

    def rewrite_events(self, root, mutate):
        from soak_evidence import read_sealed_evidence
        events = read_sealed_evidence(root / "events.jsonl", root / "seal.json")["events"]
        mutate(events)
        (root / "events.jsonl").unlink()
        (root / "seal.json").unlink()
        with EvidenceWriter(root / "events.jsonl") as writer:
            for event in events:
                writer(event)
            writer.seal(root / "seal.json", completed=True)

    def test_hash_valid_registration_contradictions_are_rejected(self):
        def duplicate_artifact(events):
            event = next(e for e in events if e.get("type") == "campaign_artifact")
            events.insert(-1, dict(event))
        def escape_artifact(events):
            next(e for e in events if e.get("type") == "campaign_artifact")["name"] = "../outside.json"
        def wrong_phase_count(events):
            next(e for e in events if e.get("type") == "campaign_phase")["event_count"] += 1
        def wrong_order(events):
            next(e for e in events if e.get("type") == "campaign_phase")["phase"] = "proxy-churn"
        def duplicate_start(events):
            events.insert(1, dict(events[0]))
        for mutate in (duplicate_artifact, escape_artifact, wrong_phase_count, wrong_order, duplicate_start):
            with self.subTest(mutation=mutate), tempfile.TemporaryDirectory() as directory:
                root = self.campaign(directory)
                self.rewrite_events(root, mutate)
                with self.assertRaises(ValueError):
                    read_completed_campaign(root)
