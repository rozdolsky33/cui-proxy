import contextlib
import io
import unittest
from unittest.mock import patch

import soak


class SoakCommandTests(unittest.TestCase):
    def arguments(self):
        return ["--binary", "binary", "--seed-binary", "seed", "--fixtures", "fixtures",
                "--build-manifest", "build.json", "--binary-commit", "a" * 40,
                "--output", "campaign"]

    def setup_mocks(self, stack):
        names = ("run_campaign", "make_identity_probe", "finalize_evidence")
        mocks = {name: stack.enter_context(patch.object(soak, name, create=True)) for name in names}
        mocks["run_campaign"].return_value = {"completed": True, "performance_qualified": False}
        stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
        stack.enter_context(contextlib.redirect_stderr(io.StringIO()))
        return mocks

    def test_default_command_keeps_original_phase_and_control_settings(self):
        with contextlib.ExitStack() as stack:
            mocks = self.setup_mocks(stack)
            self.assertEqual(soak.main(self.arguments()), 0)
            call = mocks["run_campaign"].call_args
            self.assertEqual(call.kwargs["fixed_seconds"], 7200)
            self.assertEqual(call.kwargs["churn_seconds"], 7200)
            self.assertEqual(call.kwargs["warmup_seconds"], 60)
            self.assertEqual(call.kwargs["warmup_count"], 100)
            self.assertEqual(call.kwargs["control_count"], 10)
            self.assertEqual(call.kwargs["drain_seconds"], 45)
            workload = call.args[3]
            metadata = workload.metadata()
            self.assertEqual(metadata["tier"], "normal")
            self.assertEqual(metadata["mapping_pairs"], 10000)
            self.assertEqual(metadata["dictionary_entries"], 1000)
            self.assertEqual(metadata["regex_declarations"], 50)
            self.assertEqual(len(workload.rules), len(soak.Workload("normal").rules) + 1)
            self.assertTrue(all(rule["name"].startswith("soak-g0-") for rule in workload.rules))

    def test_finalizer_keeps_new_mapping_values_for_later_bypass_privacy_check(self):
        with contextlib.ExitStack() as stack:
            mocks = self.setup_mocks(stack)
            self.assertEqual(soak.main(self.arguments()), 0)
            finalize = mocks["run_campaign"].call_args.kwargs["finalize_mode"]
            finalize(object(), "proxy", {"final_mappings": {"new-original": "new-token"}})
            finalize(object(), "bypass", {})
            values = mocks["finalize_evidence"].call_args.kwargs["forbidden_values"]
            self.assertIn("new-original", values)
            self.assertIn("new-token", values)
            self.assertNotIn("secret", values)
            workload = mocks["run_campaign"].call_args.args[3]
            self.assertTrue(all(pair[1] in values for pair in workload.pairs))

    def test_failure_returns_nonzero_without_exposing_exception_content(self):
        with contextlib.ExitStack() as stack:
            mocks = self.setup_mocks(stack)
            mocks["run_campaign"].side_effect = ValueError("private exception detail")
            output = io.StringIO()
            with contextlib.redirect_stderr(output):
                self.assertEqual(soak.main(self.arguments()), 1)
            self.assertNotIn("private exception detail", output.getvalue())

    def test_invalid_drain_does_not_start_campaign(self):
        with contextlib.ExitStack() as stack:
            mocks = self.setup_mocks(stack)
            with self.assertRaises(SystemExit):
                soak.main(self.arguments() + ["--drain-seconds", "46"])
            mocks["run_campaign"].assert_not_called()

    def test_requested_identity_is_captured_by_the_first_campaign_probe(self):
        import json
        from pathlib import Path
        import tempfile
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            mocks = self.setup_mocks(stack)
            identity = dict(revision="pre_run")
            mocks["make_identity_probe"].return_value = lambda: identity
            output = Path(directory) / "campaign"
            expected = Path(directory) / "expected.json"
            def run(*args, **kwargs):
                self.assertFalse(expected.exists())
                self.assertEqual(kwargs["identity_probe"](), identity)
                self.assertEqual(json.loads(expected.read_text()), identity)
                return dict(completed=True, performance_qualified=False)
            mocks["run_campaign"].side_effect = run
            with contextlib.redirect_stdout(io.StringIO()):
                result = soak.main(self.arguments() + ["--output", str(output),
                    "--expected-identity-output", str(expected)])
            self.assertEqual(result, 0)
