import copy
import hashlib
import http.client
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import cancellation_campaign as campaign
from test_cancellation_qualification import report as qualification_report


class CancellationCampaignTests(unittest.TestCase):
    def setUp(self):
        corpus_patch = mock.patch.object(campaign, "expected_corpus_identity", side_effect=lambda base, fixtures: base)
        corpus_patch.start()
        self.addCleanup(corpus_patch.stop)
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "run"
        self.value, self.expected = qualification_report()
        self.options = SimpleNamespace(output=self.root, binary=Path(sys.executable), seed_binary=Path(sys.executable), fixtures=Path(self.temporary.name), build_manifest=Path(self.temporary.name)/"build.json", binary_source=Path(self.temporary.name), binary_commit="a"*40)
        self.finished = False
        self.started_trials = 0
        self.fail_trial = None
        self.fail_setup = False
        self.events = []
        self.original_path = os.environ.get("PATH")
        self.original_phase = campaign.soak_campaign.run_recorded_phase
        self.original_run_campaign = campaign.soak.run_campaign
        self.original_factory = http.client.HTTPConnection

    def processes(self):
        if self.finished: return {}
        return {90: {"pid": 90, "parent_pid": 1, "state": "S", "start": self.value["finalization"]["cleanup"]["owned_children"][0]["start"]}, 91: {"pid": 91, "parent_pid": 1, "state": "S", "start": "backend-start"}}

    def fake_soak(self, arguments):
        self.assertNotIn("--warmup-seconds", arguments)
        self.assertNotIn("--warmup-count", arguments)
        self.root.mkdir()
        directory = self.root / "proxy-environment"
        directory.mkdir()
        logs = [(directory / name).open("wb") for name in ("proxy.log", "backend.log")]
        (directory / "audit.jsonl").write_text("")
        children = [SimpleNamespace(pid=pid, poll=lambda: 0 if self.finished else None) for pid in (90, 91)]
        env = SimpleNamespace(directory=directory, logs=logs, children=children, proxy=children[0])
        forbidden = {f"private-marker-{index}" for index in range(40)}
        metadata = SimpleNamespace(mappings={}, forbidden_values=forbidden)
        executors = {"fixed": metadata}
        execute = lambda plan: executors["fixed"]
        controlled = SimpleNamespace(env=env, execute=execute)
        warmup_bytes = json.dumps(self.value["warmup"], sort_keys=True).encode()
        (self.root / "proxy-warmup.json").write_bytes(warmup_bytes)
        expected_path = Path(arguments[arguments.index("--expected-identity-output") + 1])
        expected_path.write_text(json.dumps(self.expected))
        self.events = [dict(type="campaign_start", identity=self.value["identity"], settings=self.value["settings"]), dict(type="campaign_artifact", name="proxy-warmup.json", bytes=len(warmup_bytes), sha256=hashlib.sha256(warmup_bytes).hexdigest())]
        try:
            if self.fail_setup: raise RuntimeError("injected setup failure")
            campaign.soak_campaign.run_recorded_phase(controlled, "fixed")
        finally:
            for log in logs: log.close()
            self.finished = True

    def fake_trial(self, driver, retained):
        index = self.started_trials
        self.started_trials += 1
        if index == self.fail_trial:
            retained["completed"] = False
            raise RuntimeError("injected trial failure")
        retained.update(copy.deepcopy(self.value["trials"][index]))
        return retained

    def execute(self, privacy=True, cli_catches=False):
        clocks = iter((100.0, 319.0, 320.0, 321.0))
        def cli(arguments):
            try:
                return campaign.soak.run_campaign(arguments)
            except Exception:
                return 1
        with mock.patch.object(campaign.soak, "run_campaign", side_effect=self.fake_soak), mock.patch.object(campaign.shutil, "which", return_value=sys.executable), mock.patch.object(campaign.soak, "main", side_effect=cli if cli_catches else self.fake_soak), mock.patch.object(campaign, "read_processes", side_effect=self.processes), mock.patch.object(campaign, "NativeTrialDriver", side_effect=lambda *a, **k: SimpleNamespace(factory_restored=True, workers_joined=5)), mock.patch.object(campaign, "run_trial", side_effect=self.fake_trial), mock.patch.object(campaign, "read_sealed_evidence", side_effect=lambda *a: {"events": self.events, "framing_complete": True, "trailing_bytes": 0, "run_completed": False}), mock.patch.object(campaign, "verify_log_privacy", return_value={"privacy_verified": privacy, "files_checked": 5, "protected_values_checked": 40}):
            return campaign.run_probe(self.options, clock=lambda: next(clocks))

    def assert_restored(self):
        self.assertEqual(os.environ.get("PATH"), self.original_path)
        self.assertIs(campaign.soak_campaign.run_recorded_phase, self.original_phase)
        self.assertIs(campaign.soak.run_campaign, self.original_run_campaign)
        self.assertIs(http.client.HTTPConnection, self.original_factory)

    def test_runs_three_trials_and_publishes_only_cancellation_qualification(self):
        result = self.execute()
        self.assertTrue(result["cancellation_qualified"])
        self.assertFalse(result["performance_qualified"])
        self.assertEqual(self.started_trials, 3)
        artifact = json.loads(Path(result["report_path"]).read_text())
        self.assertTrue(artifact["completed"])
        self.assertFalse(artifact["collector_completed"])
        self.assert_restored()

    def test_cli_exception_conversion_preserves_intentional_successful_stop(self):
        result = self.execute(cli_catches=True)
        self.assertTrue(result["cancellation_qualified"])
        self.assertEqual(self.started_trials, 3)
        self.assert_restored()

    def test_cli_exception_conversion_preserves_original_failure(self):
        self.fail_setup = True
        with self.assertRaises(RuntimeError): self.execute(cli_catches=True)
        self.assert_restored()

    def test_setup_and_trial_failures_restore_scope_and_retain_failure(self):
        self.fail_trial = 1
        with self.assertRaises(RuntimeError): self.execute()
        self.assert_restored()
        failure = self.root.parent / (self.root.name + ".cancellation-failure.json")
        artifact = json.loads(failure.read_text())
        self.assertFalse(artifact["completed"])
        self.assertEqual(len(artifact["trials"]), 2)
        self.assertFalse(artifact["trials"][-1]["completed"])

    def test_setup_failure_restores_scope_before_any_trial(self):
        self.fail_setup = True
        with self.assertRaises(RuntimeError): self.execute()
        self.assertEqual(self.started_trials, 0)
        self.assert_restored()

    def test_privacy_failure_cannot_publish_qualified_artifact(self):
        with self.assertRaises(ValueError): self.execute(privacy=False)
        self.assertFalse((self.root.parent / (self.root.name + ".cancellation-evaluation.json")).exists())
        self.assert_restored()

    def test_refuses_existing_output_without_changing_it(self):
        self.root.mkdir()
        marker = self.root / "retained"
        marker.write_text("keep")
        with self.assertRaises(ValueError): self.execute()
        self.assertEqual(marker.read_text(), "keep")
        self.assert_restored()

    def test_failed_trial_records_privacy_verification(self):
        self.fail_trial = 1
        with self.assertRaises(RuntimeError):
            self.execute()
        paths = {name: Path(str(self.root) + suffix) for name, suffix in {"failure_privacy": ".cancellation-failure-privacy.json", "failure": ".cancellation-failure.json"}.items()}
        privacy = json.loads(paths["failure_privacy"].read_text())
        self.assertTrue(privacy["privacy_verified"])
        self.assertFalse(json.loads(paths["failure"].read_text())["completed"])

    def test_failure_privacy_scan_failure_preserves_original_error(self):
        paths = campaign._paths(self.root)
        paths["failure"].write_text("{}")
        paths["failure_log"].write_text("retained failure")
        with mock.patch.object(campaign, "verify_log_privacy", side_effect=ValueError("privacy_failure")) as scan:
            campaign.retain_failure_privacy(paths, {"forbidden": set()})
        metadata = json.loads(paths["failure_privacy"].read_text())
        self.assertFalse(metadata["privacy_verified"])
        self.assertEqual(metadata["error_kind"], "ValueError")
        self.assertIn(paths["failure"], scan.call_args.args[0])
        self.assertIn(paths["failure_log"], scan.call_args.args[0])


    def test_failure_scan_error_keeps_original_trial_exception(self):
        self.fail_trial = 1
        original = campaign.retain_failure_privacy
        def fail_scan(paths, retained):
            with mock.patch.object(campaign, "verify_log_privacy", side_effect=ValueError("privacy_failure")):
                return original(paths, retained)
        with mock.patch.object(campaign, "retain_failure_privacy", side_effect=fail_scan):
            with self.assertRaises(RuntimeError):
                self.execute()
        metadata = json.loads(Path(str(self.root) + ".cancellation-failure-privacy.json").read_text())
        self.assertFalse(metadata["privacy_verified"])
        self.assertFalse(Path(str(self.root) + ".cancellation-evaluation.json").exists())




if __name__ == "__main__": unittest.main()

