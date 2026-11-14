import contextlib
import copy
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import soak_run


class CampaignRunTests(unittest.TestCase):
    def setup_mocks(self, stack):
        names = ("CampaignEvidence", "Environment", "load_manifest", "attest_corpus",
                 "run_proxy_phases", "run_bypass_phases")
        mocks = {name: stack.enter_context(patch.object(soak_run, name, create=True)) for name in names}
        campaign = mocks["CampaignEvidence"].return_value.__enter__.return_value
        campaign.root = Path("unused")
        reports = {phase: {"phase": phase, "completed": True, "duration_s": 7200,
                            "schedule": []} for phase in ("fixed", "churn")}
        campaign.load_report.side_effect = lambda name: copy.deepcopy(reports[
            "fixed" if "fixed" in name else "churn"])
        events = []
        environments = []
        def environment(*args, **kwargs):
            env = SimpleNamespace(children=[], logs=[], directory=kwargs["retained_directory"])
            environments.append(env)
            @contextlib.contextmanager
            def entered():
                events.append("enter")
                try:
                    yield env
                finally:
                    events.append("cleanup")
            return entered()
        mocks["Environment"].side_effect = environment
        mocks["attest_corpus"].return_value = (object(), {"verified_files": 802, "manifest_sha256": "identity"})
        mocks["run_proxy_phases"].return_value = {"phases": reports, "summary": {"performance_qualified": False}}
        mocks["run_bypass_phases"].return_value = {"phases": reports, "summary": {"performance_qualified": False}}
        def finalize(env, mode, result):
            self.assertEqual(events[-1], "cleanup")
            events.append("finalize-" + mode)
            return {"verified": True}
        identity = MagicMock(return_value={"source": "identity"})
        return mocks, campaign, events, environments, identity, finalize

    def run_subject(self, identity, finalize):
        return soak_run.run_campaign(Path("unused"), Path("binary"), Path("seed"), object(),
                                      Path("fixtures"), identity_probe=identity,
                                      finalize_mode=finalize)

    def test_cleanup_finalization_and_identity_precede_next_mode_and_completion(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            result = self.run_subject(identity, finalize)
            self.assertEqual(events, ["enter", "cleanup", "finalize-proxy", "enter", "cleanup", "finalize-bypass"])
            self.assertEqual(identity.call_count, 3)
            self.assertEqual(mocks["attest_corpus"].call_count, 3)
            self.assertEqual(len(environments), 2)
            self.assertNotEqual(environments[0].directory, environments[1].directory)
            campaign.finish.assert_called_once()
            self.assertFalse(result["performance_qualified"])
            bypass = mocks["run_bypass_phases"].call_args
            self.assertEqual(bypass.args[-1], mocks["run_proxy_phases"].return_value["phases"])
            self.assertEqual(campaign.load_report.call_count, 2)

    def test_failed_proxy_finalization_prevents_bypass_and_completion(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            def failure(*args):
                self.assertEqual(events[-1], "cleanup")
                raise ValueError("finalization failed")
            with self.assertRaises(ValueError):
                self.run_subject(identity, failure)
            mocks["run_bypass_phases"].assert_not_called()
            campaign.finish.assert_not_called()

    def test_identity_change_prevents_bypass(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            identity.side_effect = [{"source": "before"}, {"source": "after"}]
            with self.assertRaises(ValueError):
                self.run_subject(identity, finalize)
            mocks["run_bypass_phases"].assert_not_called()
            campaign.finish.assert_not_called()

    def test_leftover_child_prevents_finalization_and_bypass(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            def proxy(env, *args, **kwargs):
                child = MagicMock()
                child.poll.return_value = None
                env.children.append(child)
                return {"phases": {}, "summary": {}}
            mocks["run_proxy_phases"].side_effect = proxy
            finalizer = MagicMock()
            with self.assertRaises(RuntimeError):
                self.run_subject(identity, finalizer)
            finalizer.assert_not_called()
            mocks["run_bypass_phases"].assert_not_called()
            campaign.finish.assert_not_called()

    def test_invalid_proxy_artifact_prevents_bypass_environment_setup(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            campaign.load_report.side_effect = ValueError("artifact changed")
            with self.assertRaises(ValueError):
                self.run_subject(identity, finalize)
            self.assertEqual(len(environments), 1)
            mocks["run_bypass_phases"].assert_not_called()
            campaign.finish.assert_not_called()

    def test_corpus_change_prevents_bypass(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            mocks["attest_corpus"].side_effect = [(object(), {"identity": 1}), (object(), {"identity": 2})]
            with self.assertRaises(ValueError):
                self.run_subject(identity, finalize)
            self.assertEqual(len(environments), 1)
            campaign.finish.assert_not_called()

    def test_unverified_finalization_prevents_bypass(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            with self.assertRaises(ValueError):
                self.run_subject(identity, lambda *args: {"verified": False})
            self.assertEqual(len(environments), 1)
            campaign.finish.assert_not_called()

    def test_changed_identity_after_bypass_prevents_completed_root(self):
        with contextlib.ExitStack() as stack:
            mocks, campaign, events, environments, identity, finalize = self.setup_mocks(stack)
            identity.side_effect = [{"source": 1}, {"source": 1}, {"source": 2}]
            with self.assertRaises(ValueError):
                self.run_subject(identity, finalize)
            self.assertEqual(len(environments), 2)
            campaign.finish.assert_not_called()
