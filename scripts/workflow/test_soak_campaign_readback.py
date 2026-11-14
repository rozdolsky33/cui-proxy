import tempfile
import unittest
from pathlib import Path

from soak_campaign_evidence import CampaignEvidence


class CampaignReadbackTests(unittest.TestCase):
    def test_roundtrip_reads_only_registered_hash_matched_report(self):
        with tempfile.TemporaryDirectory() as directory:
            with CampaignEvidence(Path(directory) / "campaign", {}, {}) as campaign:
                report = {"phase": "fixed", "completed": True, "schedule": []}
                campaign.save_report("proxy-fixed-summary.json", report)
                self.assertEqual(campaign.load_report("proxy-fixed-summary.json"), report)
                with self.assertRaises(ValueError):
                    campaign.load_report("unregistered.json")

    def test_changed_report_is_rejected_before_returning_data(self):
        with tempfile.TemporaryDirectory() as directory:
            with CampaignEvidence(Path(directory) / "campaign", {}, {}) as campaign:
                campaign.save_report("proxy-fixed-summary.json", {"completed": True})
                (campaign.root / "proxy-fixed-summary.json").write_text('{"completed":false}')
                with self.assertRaises(ValueError):
                    campaign.load_report("proxy-fixed-summary.json")

    def test_symlink_cannot_replace_registered_report(self):
        with tempfile.TemporaryDirectory() as directory:
            with CampaignEvidence(Path(directory) / "campaign", {}, {}) as campaign:
                campaign.save_report("proxy-fixed-summary.json", {"completed": True})
                path = campaign.root / "proxy-fixed-summary.json"
                target = Path(directory) / "copy.json"
                target.write_bytes(path.read_bytes())
                path.unlink()
                path.symlink_to(target)
                with self.assertRaises((ValueError, OSError)):
                    campaign.load_report("proxy-fixed-summary.json")
