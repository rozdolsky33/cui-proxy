import contextlib
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import soak_bypass
from soak_phase import PhaseFailure
from soak_warmup import WarmupFailure


class BypassCampaignTests(unittest.TestCase):
    def setup_mocks(self, stack):
        names = ("ScenarioExecutor", "run_warmup", "bind_fixture_replay", "run_recorded_phase")
        mocks = {name: stack.enter_context(patch.object(soak_bypass, name, create=True)) for name in names}
        mocks["ScenarioExecutor"].side_effect = [MagicMock(), MagicMock(), MagicMock()]
        mocks["run_warmup"].return_value = {"completed": True}
        mocks["bind_fixture_replay"].side_effect = [MagicMock(), MagicMock()]
        mocks["run_recorded_phase"].side_effect = [
            {"phase": phase, "completed": True} for phase in ("fixed", "churn")]
        env = SimpleNamespace(url="http://localhost:8000", upstream_url="http://localhost:8001", mappings={})
        campaign = MagicMock()
        campaign.root = Path("unused")
        reports = {phase: {"phase": phase, "completed": True, "duration_s": 7200,
                            "schedule": [{"identity": [phase, 0, 0]}]} for phase in ("fixed", "churn")}
        return mocks, env, campaign, reports

    def test_replays_both_original_schedules_after_distinct_warmup(self):
        with contextlib.ExitStack() as stack:
            mocks, env, campaign, reports = self.setup_mocks(stack)
            result = soak_bypass.run_bypass_phases(env, object(), None, Path("unused"), campaign, reports)
            constructors = mocks["ScenarioExecutor"].call_args_list
            self.assertTrue(constructors[0].kwargs["warmup"])
            self.assertTrue(all(call.kwargs["bypass"] for call in constructors))
            self.assertTrue(all(call.args[0] == env.upstream_url for call in constructors))
            calls = mocks["run_recorded_phase"].call_args_list
            self.assertEqual([call.args[1] for call in calls], ["fixed", "churn"])
            for call, phase in zip(calls, ("fixed", "churn")):
                self.assertEqual(call.kwargs["replay"], reports[phase]["schedule"])
                self.assertEqual(call.kwargs["duration"], 7200)
            self.assertEqual([call.args[0] for call in campaign.link_phase.call_args_list],
                             ["bypass-fixed", "bypass-churn"])
            self.assertFalse(result["summary"]["performance_qualified"])

    def test_failed_warmup_never_runs_replay(self):
        with contextlib.ExitStack() as stack:
            mocks, env, campaign, reports = self.setup_mocks(stack)
            failed = {"completed": False}
            mocks["run_warmup"].side_effect = WarmupFailure(failed)
            with self.assertRaises(WarmupFailure):
                soak_bypass.run_bypass_phases(env, object(), None, Path("unused"), campaign, reports)
            mocks["run_recorded_phase"].assert_not_called()
            self.assertTrue(any(call.args[1] is failed for call in campaign.save_report.call_args_list))

    def test_failed_fixed_replay_prevents_churn_and_retains_report(self):
        with contextlib.ExitStack() as stack:
            mocks, env, campaign, reports = self.setup_mocks(stack)
            failed = {"completed": False}
            mocks["run_recorded_phase"].side_effect = PhaseFailure(failed)
            with self.assertRaises(PhaseFailure):
                soak_bypass.run_bypass_phases(env, object(), None, Path("unused"), campaign, reports)
            mocks["run_recorded_phase"].assert_called_once()
            campaign.link_phase.assert_not_called()
            self.assertTrue(any(call.args[1] is failed for call in campaign.save_report.call_args_list))

    def test_incomplete_proxy_report_is_rejected_before_any_request(self):
        with contextlib.ExitStack() as stack:
            mocks, env, campaign, reports = self.setup_mocks(stack)
            reports["churn"]["completed"] = False
            with self.assertRaises(ValueError):
                soak_bypass.run_bypass_phases(env, object(), None, Path("unused"), campaign, reports)
            mocks["run_warmup"].assert_not_called()
            mocks["run_recorded_phase"].assert_not_called()
