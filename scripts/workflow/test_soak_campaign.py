import copy
from contextlib import ExitStack
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import soak_campaign as campaign_module
from soak_campaign_evidence import CampaignEvidence
from soak_evidence import EvidenceWriter
from soak_warmup import WarmupFailure


class CampaignTests(unittest.TestCase):
    def test_failed_warmup_prevents_all_measured_phases(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)/"campaign"
            env=SimpleNamespace(url="http://127.0.0.1:1",upstream_url="http://127.0.0.1:2",mappings={},directory=Path(folder),seed_binary="unused")
            failure=WarmupFailure({"completed":False,"results":[],"failures":[{"stage":"operation"}]})
            with CampaignEvidence(root,{"source_sha256":"a"*64},{"clients":4}) as evidence:
                with patch.object(campaign_module,"run_warmup",side_effect=failure,create=True),patch.object(campaign_module,"run_recorded_phase",side_effect=AssertionError("phase started"),create=True):
                    with self.assertRaises(WarmupFailure):
                        campaign_module.run_proxy_phases(env,object(),None,Path(folder),evidence)
                self.assertTrue((root/"proxy-warmup.json").exists())
