import contextlib
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import soak_finalization


class FinalizationTests(unittest.TestCase):
    def setup_case(self, directory, stack):
        root = Path(directory)
        for name in ("proxy.log", "backend.log"):
            (root / name).write_text("")
        (root / "audit.jsonl").write_text('{"request_id":"one"}\n')
        env = SimpleNamespace(directory=root)
        rows = [{"server_request_id": str(i), "completed": True} for i in range(3)]
        result = {"warmup": {"completed": True, "results": [{"result": rows[0]}]},
                  "phases": {phase: {"completed": True, "results": [{"request": rows[i]}]}
                             for i, phase in enumerate(("fixed", "churn"), 1)},
                  "controls": {"failed": False, "reloads_completed": 10}}
        coverage = stack.enter_context(patch.object(soak_finalization, "verify_policy_coverage", create=True))
        coverage.return_value = {"audit_records": 3}
        privacy = stack.enter_context(patch.object(soak_finalization, "verify_log_privacy", create=True))
        privacy.return_value = {"privacy_verified": True}
        return env, result, rows, coverage, privacy

    def test_reconciles_warmup_and_both_measured_phases(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            env, result, rows, coverage, privacy = self.setup_case(directory, stack)
            report = soak_finalization.finalize_evidence(env, "proxy", result, forbidden_values=["canary"])
            self.assertTrue(report["verified"])
            self.assertEqual(coverage.call_args.args[1], rows)
            self.assertEqual(list(coverage.call_args.args[2]), list(range(11)))
            self.assertEqual(set(privacy.call_args.args[0]),
                             {env.directory / name for name in ("proxy.log", "backend.log", "audit.jsonl")})

    def test_missing_audit_prevents_proxy_finalization(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            env, result, rows, coverage, privacy = self.setup_case(directory, stack)
            (env.directory / "audit.jsonl").unlink()
            with self.assertRaises((ValueError, OSError)):
                soak_finalization.finalize_evidence(env, "proxy", result, forbidden_values=["canary"])
            coverage.assert_not_called()

    def test_privacy_failure_prevents_verification(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            env, result, rows, coverage, privacy = self.setup_case(directory, stack)
            privacy.side_effect = ValueError("privacy failure")
            with self.assertRaises(ValueError):
                soak_finalization.finalize_evidence(env, "proxy", result, forbidden_values=["canary"])

    def test_bypass_rejects_unexpected_proxy_audit_records(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            env, result, rows, coverage, privacy = self.setup_case(directory, stack)
            with self.assertRaises(ValueError):
                soak_finalization.finalize_evidence(env, "bypass", result, forbidden_values=["canary"])
            coverage.assert_not_called()

    def test_missing_phase_rows_cannot_be_finalized(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            env, result, rows, coverage, privacy = self.setup_case(directory, stack)
            result["phases"]["churn"]["results"] = []
            with self.assertRaises(ValueError):
                soak_finalization.finalize_evidence(env, "proxy", result, forbidden_values=["canary"])

    def test_audit_reader_rejects_partial_duplicate_and_nonfinite_records(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "audit.jsonl"
            for content in (b'{"id":1}', b'{"id":1,"id":2}\n', b'{"value":NaN}\n',
                            b'{"value":1e999}\n', b'[]\n'):
                with self.subTest(case_length=len(content)):
                    path.write_bytes(content)
                    with self.assertRaises(ValueError):
                        soak_finalization.read_audit(path)

    def test_audit_reader_rejects_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "audit.jsonl"
            source = Path(directory) / "source.jsonl"
            source.write_text('{}\n')
            path.symlink_to(source)
            with self.assertRaises(OSError):
                soak_finalization.read_audit(path)

    def test_empty_bypass_audit_is_verified_without_proxy_coverage(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            env, result, rows, coverage, privacy = self.setup_case(directory, stack)
            (env.directory / "audit.jsonl").write_text("")
            report = soak_finalization.finalize_evidence(env, "bypass", result, forbidden_values=["canary"])
            self.assertTrue(report["verified"])
            self.assertTrue(report["audit"]["bypass_proxy_absence_verified"])
            coverage.assert_not_called()
