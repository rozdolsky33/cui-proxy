"""Campaign sequencing without processes, clocks, or network activity."""
import contextlib
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import soak_campaign as campaign_module
from soak_phase import PhaseFailure


class CampaignSequenceTests(unittest.TestCase):
    def setup_mocks(self, stack):
        names = ("ScenarioExecutor", "run_warmup", "wait_for_idle", "observe_idle_gc",
                 "inspect_vault", "PolicyReloader", "ControlledExecutor", "ResourceMonitor",
                 "run_recorded_phase", "finalize_mapping_evidence")
        mocks = {name: stack.enter_context(patch.object(campaign_module, name)) for name in names}
        mocks["StageCollector"] = stack.enter_context(patch.object(campaign_module, "StageCollector", create=True))
        stages = mocks["StageCollector"].return_value
        stages.__enter__.return_value = stages
        stages.records = []
        stages.errors = 0
        executors = [MagicMock(), MagicMock(), MagicMock()]
        mocks["ScenarioExecutor"].side_effect = executors
        mocks["run_warmup"].return_value = {"completed": True}
        mocks["wait_for_idle"].return_value = {"goroutines": 5}
        mocks["observe_idle_gc"].return_value = {"completed": True}
        mocks["inspect_vault"].side_effect = [
            {"mappings": {"initial": "a"}},
            {"mappings": {"initial": "a", "fixed": "b"}},
            {"mappings": {"initial": "a", "fixed": "b", "churn": "c"}},
        ]
        controls = mocks["ControlledExecutor"].return_value
        controls.summary.return_value = {"failed": False, "restarts_completed": 10,
                                         "reloads_completed": 10}
        mocks["ResourceMonitor"].return_value.__enter__.return_value.samples = 1
        mocks["run_recorded_phase"].side_effect = [
            {"phase": phase, "completed": True} for phase in ("fixed", "churn")]
        mocks["finalize_mapping_evidence"].side_effect = lambda report, *args: {
            **report, "mapping_evidence": {"unmet_requirements": []}}
        env = SimpleNamespace(url="http://localhost", upstream_url="http://localhost", mappings={},
                              directory=Path("unused"), seed_binary=Path("unused"), metrics=MagicMock())
        campaign = MagicMock()
        campaign.root = Path("unused")
        return mocks, executors, env, campaign

    def test_fixed_then_churn_share_controls_and_persisted_mapping_baseline(self):
        with contextlib.ExitStack() as stack:
            mocks, executors, env, campaign = self.setup_mocks(stack)
            result = campaign_module.run_proxy_phases(env, object(), None, Path("unused"), campaign)
            calls = mocks["run_recorded_phase"].call_args_list
            self.assertEqual([call.args[1] for call in calls], ["fixed", "churn"])
            self.assertIs(calls[0].args[0], calls[1].args[0])
            self.assertEqual([call.kwargs["duration"] for call in calls], [7200, 7200])
            self.assertEqual([call.kwargs["drain_seconds"] for call in calls], [0, 45])
            self.assertEqual(executors[1].mappings, {"initial": "a"})
            self.assertEqual(executors[2].mappings, {"initial": "a", "fixed": "b"})
            self.assertEqual([call.args[0] for call in campaign.link_phase.call_args_list],
                             ["proxy-fixed", "proxy-churn"])
            self.assertTrue(result["summary"]["finalization_pending"])
            self.assertFalse(result["summary"]["performance_qualified"])
            self.assertEqual(result["summary"]["resource_samples"], 1)
            self.assertEqual(result["final_mappings"], {"initial": "a", "fixed": "b", "churn": "c"})
            for executor in executors[1:]:
                executor.verify_final_absence.assert_called_once()

    def test_failed_fixed_phase_is_retained_and_churn_never_starts(self):
        with contextlib.ExitStack() as stack:
            mocks, executors, env, campaign = self.setup_mocks(stack)
            report = {"phase": "fixed", "completed": False}
            mocks["run_recorded_phase"].side_effect = PhaseFailure(report)
            with self.assertRaises(PhaseFailure):
                campaign_module.run_proxy_phases(env, object(), None, Path("unused"), campaign)
            mocks["run_recorded_phase"].assert_called_once()
            self.assertTrue(any(call.args[1] is report for call in campaign.save_report.call_args_list))
            campaign.link_phase.assert_not_called()
            self.assertTrue(any(call.args[0] == "proxy-stage-records.json"
                                for call in campaign.save_report.call_args_list))

    def test_unmet_mapping_evidence_prevents_churn(self):
        with contextlib.ExitStack() as stack:
            mocks, executors, env, campaign = self.setup_mocks(stack)
            mocks["finalize_mapping_evidence"].side_effect = lambda report, *args: {
                **report, "mapping_evidence": {"unmet_requirements": ["population"]}}
            with self.assertRaises(campaign_module.CampaignModeFailure):
                campaign_module.run_proxy_phases(env, object(), None, Path("unused"), campaign)
            mocks["run_recorded_phase"].assert_called_once()
            campaign.link_phase.assert_not_called()

    def test_retains_request_stage_evidence_across_both_phases(self):
        with contextlib.ExitStack() as stack:
            mocks, executors, env, campaign = self.setup_mocks(stack)
            campaign_module.run_proxy_phases(env, object(), None, Path("unused"), campaign)
            mocks["StageCollector"].assert_called_once()
            calls = [call for call in campaign.save_report.call_args_list
                     if call.args[0] == "proxy-stage-records.json"]
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0].args[1], {"records": [], "poll_errors": 0})

    def test_resource_monitor_receives_real_sampler_observations(self):
        from contextlib import ExitStack
        from unittest.mock import patch
        import soak_campaign
        with ExitStack() as stack:
            mocks, executors, env, campaign = self.setup_mocks(stack)
            sampler_type = stack.enter_context(patch('performance.ResourceSampler'))
            sampler = sampler_type.return_value
            sampler.samples = []
            expected = {'rss_available': True, 'proxy_rss_bytes': 12345}
            sampler.sample.side_effect = lambda: sampler.samples.append(dict(expected))
            soak_campaign.run_proxy_phases(env, object(), None, Path('unused'), campaign)
            measure = mocks['ResourceMonitor'].call_args.args[0]
            observed = measure()
            self.assertIsInstance(observed, dict)
            self.assertEqual(observed, expected)
            sampler_type.assert_called_once_with(env)
            self.assertEqual(sampler.samples, [])
