import fcntl
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import ocr_recovery_probe as probe


class PrivateExtractorTests(unittest.TestCase):
    def tools(self, root):
        result = {}
        for name in probe.TOOL_NAMES:
            path = root / ("real-" + name)
            path.write_text("#!/bin/sh\nexit 0\n")
            path.chmod(0o700)
            result[name] = path
        return result

    def test_private_launcher_restores_on_exception_without_touching_real_tools(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tools = self.tools(root)
            before = {name: path.read_bytes() for name, path in tools.items()}
            isolated = probe.PrivateExtractors(root / "private", tools)
            with isolated:
                launcher = isolated.directory / "tesseract"
                self.assertTrue(launcher.is_file())
                with self.assertRaisesRegex(RuntimeError, "deliberate"):
                    with isolated.without_ocr():
                        self.assertFalse(launcher.exists())
                        self.assertEqual(isolated.environment()["PATH"], str(isolated.directory))
                        raise RuntimeError("deliberate")
                self.assertTrue(launcher.is_file())
                self.assertEqual({name: path.read_bytes() for name, path in tools.items()}, before)
            self.assertFalse(isolated.directory.exists())
            self.assertTrue(all(isolated.cleanup.values()))

    def test_existing_directory_and_symlink_launchers_are_not_removed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tools = self.tools(root)
            directory = root / "existing"
            directory.mkdir()
            sentinel = directory / "keep"
            sentinel.write_text("keep")
            with self.assertRaises(probe.ProbeFailure):
                with probe.PrivateExtractors(directory, tools):
                    pass
            self.assertEqual(sentinel.read_text(), "keep")

    def test_other_extractors_remain_private_and_no_host_path_fallback(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with probe.PrivateExtractors(root / "private", self.tools(root)) as isolated:
                with patch.dict(os.environ, {"PATH": "/host/tools", "TESSDATA_PREFIX": "/host/data"}):
                    child = isolated.environment()
                self.assertEqual(child["PATH"], str(isolated.directory))
                self.assertNotIn("TESSDATA_PREFIX", child)
                with isolated.without_ocr():
                    for name in ("pdftotext", "pdftoppm"):
                        self.assertTrue((isolated.directory / name).is_file())


class RecoveryAssertionTests(unittest.TestCase):
    def failure(self):
        return {"error": {"code": "transformation_error", "request_id": "0123456789abcdef",
                          "message": "attachment_image_missing_tool",
                          "recovery": {"code": "attachment_image_missing_tool", "category": "dependency",
                                       "retry": "after_repair", "remedy": "Install Tesseract and restart the proxy."}}}

    def test_missing_tool_requires_exact_failure_and_recovery_guidance(self):
        raw = json.dumps(self.failure()).encode()
        self.assertEqual(probe.check_missing_failure(422, raw)["code"], "attachment_image_missing_tool")
        for field, value in (("code", "other"), ("category", "other"), ("retry", "other"), ("remedy", "")):
            invalid = self.failure()
            invalid["error"]["recovery"][field] = value
            with self.assertRaises(probe.ProbeFailure):
                probe.check_missing_failure(422, json.dumps(invalid).encode())
        for status, body in ((200, raw), (503, raw), (422, b"not json"), (422, b"{}")):
            with self.assertRaises(probe.ProbeFailure):
                probe.check_missing_failure(status, body)

    def test_all_three_text_requests_must_be_canonical_and_enclose_failure(self):
        row = {"started_s": 1., "finished_s": 4., "status": 200, "completed": True,
               "outcome": "supported_success", "semantic_verified": True, "boundary_verified": True}
        self.assertEqual(probe.check_overlap([dict(row) for _ in range(3)], 2., 3.), 3)
        for change in ({"finished_s": 2.5}, {"semantic_verified": False},
                       {"boundary_verified": False}, {"completed": False}):
            rows = [dict(row), dict(row), dict(row, **change)]
            with self.assertRaises(probe.ProbeFailure):
                probe.check_overlap(rows, 2., 3.)
        with self.assertRaises(probe.ProbeFailure):
            probe.check_overlap([row, row], 2., 3.)

    def test_audit_missing_binding_extra_fields_and_values_fail_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "audit.jsonl"
            record = {"request_id": "0123456789abcdef", "decision": "blocked",
                      "detail": "attachment_image_missing_tool", "hash": "a" * 64}
            path.write_text(json.dumps(record) + "\n")
            result = probe.audit_summary(path, [], record["request_id"], ["privatefixturevalue"])
            self.assertEqual(result["missing_tool_blocks"], 1)
            for mutated in (dict(record, detail="other"), dict(record, payload="privatefixturevalue"),
                            dict(record, reason="privatefixturevalue")):
                path.write_text(json.dumps(mutated) + "\n")
                with self.assertRaises(probe.ProbeFailure):
                    probe.audit_summary(path, [], record["request_id"], ["privatefixturevalue"])

    def test_missing_canonical_receipt_is_not_success(self):
        with patch.object(probe, "read_receipt", return_value={"count": 0}):
            with self.assertRaises((probe.ProbeFailure, ValueError)):
                probe.require_receipt("http://127.0.0.1:1", "operation", "a" * 64)


class LockAndBindingTests(unittest.TestCase):
    def test_busy_reference_lock_fails_before_any_probe_subprocess(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "reference-machine.lock"
            path.touch()
            with path.open("rb") as held:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaisesRegex(probe.ProbeFailure, "reference_machine_busy"):
                    with probe.reference_lock(path):
                        self.fail("busy lock admitted probe")
            self.assertTrue(path.exists())

    def test_lock_requires_existing_regular_non_symlink_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with self.assertRaises(probe.ProbeFailure):
                with probe.reference_lock(root / "missing"):
                    pass
            actual = root / "actual"
            actual.touch()
            link = root / "link"
            link.symlink_to(actual)
            with self.assertRaises(probe.ProbeFailure):
                with probe.reference_lock(link):
                    pass

    def test_candidate_and_producer_binding_are_both_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / "candidate"
            binary.write_bytes(b"candidate")
            with patch.object(probe, "source_binding", return_value={"commit": "a" * 40}), \
                 patch.object(probe, "read_build_info", return_value={"vcs.revision": probe.CANDIDATE_COMMIT,
                                                                     "vcs.modified": "false"}):
                with self.assertRaises(probe.ProbeFailure):
                    probe.bind_identity(binary, "0" * 64, tmp, tmp, "a" * 40)
            with patch.object(probe, "source_binding", side_effect=probe.ProbeFailure("producer_binding")):
                with self.assertRaises(probe.ProbeFailure):
                    probe.bind_identity(binary, hashlib.sha256(b"candidate").hexdigest(), tmp, tmp, "a" * 40)


class EvidenceCompletenessTests(unittest.TestCase):
    def evidence(self):
        return {"identity": {"binary_sha256": "a" * 64, "embedded_clean_revision": True,
                             "binary_source": {"commit": probe.CANDIDATE_COMMIT, "clean": True},
                             "producer_source": {"commit": "b" * 40, "clean": True},
                             "producer_files": {str(index): "c" * 64 for index in range(4)}},
                "identity_stable": True, "image_successes": 2, "text_semantic_successes": 3,
                "canonical_receipts_verified": 5, "text_overlap_count": 3, "same_proxy_process": True,
                "baseline_uncached": True, "restored_uncached": True,
                "missing_image": {"status": 422, "recovery_code": probe.DETAIL,
                                  "upstream_arrivals": 0, "uncached": True},
                "audit": {"records": 6, "missing_tool_blocks": 1, "metadata_only": True,
                          "privacy_verified": True, "sha256": "d" * 64},
                "cleanup": {name: True for name in ("launcher_restored", "real_tools_unchanged",
                            "private_directory_removed", "private_environment_removed", "child_processes_stopped")},
                "fixture_sha256": ["e" * 64, "f" * 64], "fixture_manifest_sha256": "a" * 64,
                "real_tool_sha256": {name: "b" * 64 for name in probe.TOOL_NAMES}, "seed_binary_sha256": "c" * 64,
                "log_privacy": {"shutdown_complete": True, "handles_closed": True, "scan_completed": True,
                                "value_free": True, "checked_known_values": 1, "leaking_files": 0,
                                "failure_logs_retained": False,
                                "files": [{"name": name, "bytes": 0, "sha256": "d" * 64}
                                          for name in ("backend.log", "proxy.log")]}}

    def test_every_required_proof_is_mandatory(self):
        valid = self.evidence()
        probe.validate_evidence(valid)
        for field in valid:
            changed = copy.deepcopy(valid)
            del changed[field]
            with self.subTest(field=field), self.assertRaises(probe.ProbeFailure):
                probe.validate_evidence(changed)
        for field in valid["cleanup"]:
            changed = copy.deepcopy(valid)
            changed["cleanup"][field] = False
            with self.subTest(cleanup=field), self.assertRaises(probe.ProbeFailure):
                probe.validate_evidence(changed)

    def test_uncached_and_same_process_proofs_cannot_be_omitted(self):
        for field in ("baseline_uncached", "restored_uncached", "same_proxy_process"):
            changed = self.evidence()
            changed[field] = False
            with self.subTest(field=field), self.assertRaises(probe.ProbeFailure):
                probe.validate_evidence(changed)

    def test_clean_shutdown_log_proof_is_mandatory(self):
        for field in ("shutdown_complete", "handles_closed", "scan_completed", "value_free"):
            changed = self.evidence()
            changed["log_privacy"][field] = False
            with self.subTest(field=field), self.assertRaises(probe.ProbeFailure):
                probe.validate_evidence(changed)
        changed = self.evidence()
        changed["log_privacy"]["files"] = []
        with self.assertRaises(probe.ProbeFailure):
            probe.validate_evidence(changed)


class CapturedLogPrivacyTests(unittest.TestCase):
    def files(self, root, shutdown):
        handles = []
        for name in ("backend.log", "proxy.log"):
            handle = (root / name).open("w")
            handle.write("startup\n")
            handle.write(shutdown)
            handle.close()
            handles.append(handle)
        return handles

    def test_shutdown_value_leak_rejects_and_retains_only_private_failure_logs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            logs = root / "environment"
            logs.mkdir()
            handles = self.files(logs, "shutdown privatefixturevalue\n")
            evidence = {}
            retained = root / "private-failure-logs"
            with self.assertRaisesRegex(probe.ProbeFailure, "captured_log_value_leak"):
                probe.captured_log_summary(logs, handles, [SimpleNamespace(poll=lambda: 0)],
                                           {"privatefixturevalue"}, retained, evidence)
            self.assertFalse(evidence["value_free"])
            self.assertEqual(evidence["leaking_files"], 2)
            self.assertTrue(evidence["failure_logs_retained"])
            self.assertEqual(retained.stat().st_mode & 0o777, 0o700)
            for path in retained.iterdir():
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertNotIn("privatefixturevalue", json.dumps(evidence))

    def test_cleanup_gate_scans_final_closed_logs_and_always_deletes_environment(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            logs = root / "environment"
            logs.mkdir()
            handles = self.files(logs, "clean shutdown\n")
            evidence = {}
            deleted = []
            temporary = SimpleNamespace(cleanup=lambda: deleted.append(True))
            callback = lambda: probe.captured_log_summary(
                logs, handles, [SimpleNamespace(poll=lambda: 0)], {"privatefixturevalue"},
                root / "private-failure-logs", evidence)
            gate = probe.CleanupGate(temporary, callback)
            gate.cleanup()
            self.assertEqual(deleted, [True])
            self.assertTrue(evidence["value_free"])
            self.assertTrue(evidence["shutdown_complete"])
            self.assertTrue(evidence["handles_closed"])
            self.assertEqual(len(evidence["files"]), 2)
            failed_gate = probe.CleanupGate(temporary, lambda: (_ for _ in ()).throw(probe.ProbeFailure("failed")))
            with self.assertRaises(probe.ProbeFailure):
                failed_gate.cleanup()
            self.assertEqual(deleted, [True, True])

    def test_live_environment_scan_observes_bytes_written_only_during_shutdown(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            logs = root / "environment"
            logs.mkdir()
            handles = [(logs / name).open("w") for name in ("backend.log", "proxy.log")]
            stopped = [None]
            cleaned = []
            env = object.__new__(probe.LiveEnvironment)
            env.directory = logs
            env.temp = SimpleNamespace(cleanup=lambda: cleaned.append(True))
            env.logs = handles
            env.children = [SimpleNamespace(poll=lambda: stopped[0])]
            env.privacy_values = {"privatefixturevalue"}
            env.failure_directory = root / "private-failure-logs"
            env.log_evidence = {}

            def shutdown(instance, *_error):
                for handle in instance.logs:
                    handle.write("late shutdown privatefixturevalue\n")
                    handle.close()
                stopped[0] = 0
                instance.temp.cleanup()

            with patch.object(probe.perf.Environment, "__exit__", shutdown):
                with self.assertRaisesRegex(probe.ProbeFailure, "captured_log_value_leak"):
                    env.__exit__(None, None, None)
            self.assertEqual(cleaned, [True])
            self.assertTrue(env.log_evidence["shutdown_complete"])
            self.assertTrue(env.log_evidence["handles_closed"])
            self.assertEqual(env.log_evidence["leaking_files"], 2)
            self.assertTrue(env.log_evidence["failure_logs_retained"])


if __name__ == "__main__":
    unittest.main()
