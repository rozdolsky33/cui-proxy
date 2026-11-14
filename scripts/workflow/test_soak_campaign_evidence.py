import tempfile
import unittest
from pathlib import Path
from soak_evidence import EvidenceWriter,read_sealed_evidence
from soak_campaign_evidence import CampaignEvidence


def child(root,name,completed=True):
    directory=root/name;directory.mkdir(mode=0o700)
    with EvidenceWriter(directory/"events.jsonl") as writer:
        writer({"type":"phase_marker","phase":name})
        writer.seal(directory/"seal.json",completed=completed)


class CampaignEvidenceTests(unittest.TestCase):
    def test_incomplete_context_retains_sealed_root(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}):pass
            report=read_sealed_evidence(root/"events.jsonl",root/"seal.json")
            self.assertFalse(report["run_completed"])
            self.assertEqual(root.stat().st_mode & 0o777,0o700)

    def test_all_phase_links_are_needed_and_still_do_not_qualify(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                for name in ("proxy-fixed","proxy-churn","bypass-fixed","bypass-churn"):
                    child(root,name);campaign.link_phase(name)
                campaign.finish()
            report=read_sealed_evidence(root/"events.jsonl",root/"seal.json")
            self.assertTrue(report["run_completed"])
            self.assertFalse(campaign.performance_qualified)

    def test_missing_duplicate_or_changed_child_prevents_completion(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                with self.assertRaises(ValueError):campaign.finish()
                child(root,"proxy-fixed");campaign.link_phase("proxy-fixed")
                with self.assertRaises(ValueError):campaign.link_phase("proxy-fixed")
                for name in ("proxy-churn","bypass-fixed","bypass-churn"):
                    child(root,name);campaign.link_phase(name)
                with (root/"proxy-fixed"/"events.jsonl").open("ab") as stream:stream.write(b"changed")
                with self.assertRaises(ValueError):campaign.finish()
            self.assertFalse(read_sealed_evidence(root/"events.jsonl",root/"seal.json")["run_completed"])

    def test_failed_child_and_path_escape_are_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                child(root,"proxy-fixed",False)
                with self.assertRaises(ValueError):campaign.link_phase("proxy-fixed")
                with self.assertRaises(ValueError):campaign.link_phase("../escape")

    def test_exception_after_finish_request_keeps_campaign_incomplete(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with self.assertRaises(RuntimeError):
                with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                    for name in ("proxy-fixed","proxy-churn","bypass-fixed","bypass-churn"):
                        child(root,name);campaign.link_phase(name)
                    campaign.finish()
                    raise RuntimeError("cleanup failed")
            self.assertFalse(read_sealed_evidence(root/"events.jsonl",root/"seal.json")["run_completed"])

    def test_parent_sync_failure_retains_incomplete_root(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with patch("soak_campaign_evidence._sync_parent",side_effect=OSError("sync failed")):
                with self.assertRaises(OSError):
                    with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}):pass
            self.assertFalse(read_sealed_evidence(root/"events.jsonl",root/"seal.json")["run_completed"])

    def test_changed_child_after_finish_request_prevents_completed_seal(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with self.assertRaises(ValueError):
                with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                    for name in ("proxy-fixed","proxy-churn","bypass-fixed","bypass-churn"):
                        child(root,name);campaign.link_phase(name)
                    campaign.finish()
                    with (root/"proxy-fixed"/"events.jsonl").open("ab") as stream:stream.write(b"changed")
            self.assertFalse(read_sealed_evidence(root/"events.jsonl",root/"seal.json")["run_completed"])

    def test_summary_artifacts_are_private_immutable_and_bound_to_root(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                campaign.save_report("proxy-summary.json",{"completed":False,"request_count":4})
                self.assertTrue((root/"proxy-summary.json").exists())
                self.assertEqual((root/"proxy-summary.json").stat().st_mode & 0o777,0o600)
                with self.assertRaises(ValueError):campaign.save_report("proxy-summary.json",{})
                with self.assertRaises(ValueError):campaign.save_report("../escape.json",{})
                with self.assertRaises(ValueError):campaign.save_report("raw.json",{"body":"private"})

    def test_changed_summary_prevents_completed_root(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as campaign:
                for name in ("proxy-fixed","proxy-churn","bypass-fixed","bypass-churn"):
                    child(root,name);campaign.link_phase(name)
                campaign.save_report("summary.json",{"completed":True})
                (root/"summary.json").write_text("changed")
                with self.assertRaises(ValueError):campaign.finish()
