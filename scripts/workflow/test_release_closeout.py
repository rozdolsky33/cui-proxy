import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import harness
import ocr_recovery_probe as recovery
import release_closeout as closeout
from test_release_review_regressions import valid_evidence as complete_evidence


class ReleaseCloseoutTests(unittest.TestCase):
    def test_owned_private_environment_paths_are_canonical_before_child_and_config_creation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            owned = root / "owned"
            owned.mkdir()
            alias = root / "alias"
            alias.symlink_to(owned, target_is_directory=True)
            env = object.__new__(closeout.ReleaseEnvironment)
            env.directory = alias
            with patch.object(harness.Environment, "spawn", return_value=object()):
                env.spawn(["python", "--upstream"], "upstream")
            self.assertEqual(env.directory, owned)

    def test_original_absence_applies_to_entire_body_headers_and_fixture_metadata(self):
        body = {"input": [harness.message("alias-A")], "metadata": {"harness_id": "owned-test"}}
        closeout.assert_alias_wire(body, {"Content-Type": "application/json"}, ["original-A", "original-B"], ["alias-A"])
        for changed, headers in ((dict(body, metadata={"hidden": "original-A"}), {}),
                                 (body, {"X-Hidden": "original-B"}),
                                 (dict(body, extra="original-A"), {})):
            with self.assertRaises(ValueError):
                closeout.assert_alias_wire(changed, headers, ["original-A", "original-B"], ["alias-A"])

    def test_upstream_semantic_digest_proves_aliases_without_backend_original_expectations(self):
        inputs = [harness.message("alias-A"), harness.message("alias-B")]
        proof = {"completed": True, "bytes": 100, "semantic_sha256": harness.semantic_sha256(inputs)}
        closeout.check_alias_response(proof, inputs, ["alias-A", "alias-B"], ["original-A", "original-B"])
        for change in ({"completed": False}, {"bytes": 0}, {"semantic_sha256": "0" * 64}):
            with self.assertRaises(ValueError):
                closeout.check_alias_response(dict(proof, **change), inputs, ["alias-A", "alias-B"], ["original-A", "original-B"])
        with self.assertRaises(ValueError):
            closeout.check_alias_response(proof, [harness.message("original-A")], ["alias-A"], ["original-A"])

    def test_rollback_requires_latest_postcandidate_vault_not_preupgrade_backup(self):
        with tempfile.TemporaryDirectory() as temporary:
            vault = Path(temporary).resolve() / "vault"
            old = hashlib.sha256(b"baseline-state").hexdigest()
            latest = hashlib.sha256(b"candidate-state-with-B").hexdigest()
            vault.write_bytes(b"candidate-state-with-B")
            closeout.check_latest_vault(vault, latest, old)
            linked = vault.with_name("linked-vault")
            linked.symlink_to(vault)
            with self.assertRaises(ValueError):
                closeout.check_latest_vault(linked, latest, old)
            vault.write_bytes(b"baseline-state")
            with self.assertRaises(ValueError):
                closeout.check_latest_vault(vault, latest, old)

    def test_running_preflight_pid_version_policy_and_revision_are_required(self):
        report = {"ready": True, "pid": 1234, "version": "0.7.4", "revision": "a" * 40,
                  "policy": {"profile": 'sanitize', "attachments": "block", "on_error": "block"}}
        closeout.check_preflight(report, 1234, "0.7.4", "a" * 40)
        for change in ({"ready": False}, {"pid": 1235}, {"version": "0.7.3"}, {"revision": "b" * 40},
                       {"policy": {"profile": "controlled", "attachments": "passthrough", "on_error": "block"}}):
            with self.assertRaises(ValueError):
                closeout.check_preflight(dict(report, **change), 1234, "0.7.4", "a" * 40)

    def test_native_evidence_missing_any_health_persistence_archive_or_cleanup_proof_fails_closed(self):
        valid = complete_evidence()
        closeout.validate_evidence(valid)
        for field in valid:
            changed = copy.deepcopy(valid)
            del changed[field]
            with self.subTest(missing=field), self.assertRaises(ValueError):
                closeout.validate_evidence(changed)
        unsupported = dict(valid, unsupported_format_expected=True, rollback_healthy=False)
        with self.assertRaises(ValueError):
            closeout.validate_evidence(unsupported)

    def test_generic_ocr_binding_keeps_legacy_default_but_accepts_explicit_release_source(self):
        source = Path(recovery.__file__).resolve().parents[2]
        for expected in (recovery.CANDIDATE_COMMIT, "d" * 40):
            with patch.object(recovery, "digest", return_value="a" * 64), \
                 patch.object(recovery, "source_binding", side_effect=lambda _root, commit: {"commit": commit, "clean": True}), \
                 patch.object(recovery, "read_build_info", return_value={"vcs.revision": expected, "vcs.modified": "false"}), \
                 patch.object(recovery, "captured", side_effect=lambda args, _cwd=None: args[-1]):
                kwargs = {} if expected == recovery.CANDIDATE_COMMIT else {"expected_binary_commit": expected}
                identity = recovery.bind_identity("unused-binary", "a" * 64, source, source, "e" * 40, **kwargs)
            self.assertEqual(identity["binary_source"]["commit"], expected)


    def test_release_uses_supported_sanitize_policy_for_both_binary_versions(self):
        with patch.object(harness.Environment, "__init__", return_value=None) as initialize:
            closeout.ReleaseEnvironment("baseline", {"private-value"}, {}, "failure")
        self.assertEqual(initialize.call_args.kwargs["profile"], "sanitize")
        ready = {"ready": True, "pid": 1234, "version": "0.7.4",
                 "revision": "a" * 40, "policy": {"profile": "sanitize",
                 "attachments": "block", "on_error": "block"}}
        closeout.check_preflight(ready, 1234, "0.7.4", "a" * 40)
        ready["policy"]["profile"] = "controlled"
        with self.assertRaises(ValueError):
            closeout.check_preflight(ready, 1234, "0.7.4", "a" * 40)

    def test_release_early_failure_scans_one_closed_capture_without_relaxing_default(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            captured = directory / "check-config.stderr"
            handle = captured.open("wb")
            handle.write(b"configuration rejected")
            handle.close()
            with self.assertRaises(recovery.ProbeFailure):
                recovery.captured_log_summary(directory, [handle], [], {"private-value"},
                    directory / 'retained-default-failure', {})
            evidence = {}
            recovery.captured_log_summary(directory, [handle], [], {"private-value"},
                directory / 'retained-success-unused', evidence, minimum_captures=1)
            self.assertTrue(evidence["scan_completed"])
            self.assertTrue(evidence["shutdown_complete"])
            self.assertTrue(evidence["handles_closed"])
            self.assertTrue(evidence["value_free"])
            captured.write_bytes(b"private-value")
            with self.assertRaises(recovery.ProbeFailure):
                recovery.captured_log_summary(directory, [handle], [], {"private-value"},
                    directory / 'retained-leak-failure', {}, minimum_captures=1)


    def test_restart_gauge_zero_requires_exact_postrequest_counts_and_persistence_proofs(self):
        valid = complete_evidence()
        valid["mapping_counts"] = [0, 1, 0, 1, 2, 0, 2]
        closeout.validate_evidence(valid)
        for counts in ([0, 0, 0, 1, 2, 0, 2], [0, 1, 0, 0, 2, 0, 2],
                       [0, 1, 0, 1, 1, 0, 2], [0, 1, 0, 1, 2, 0, 1],
                       [0, 1, 3, 1, 2, 0, 2], [0, 1, 0, 1, 2, 3, 2],
                       [False, 1, 0, 1, 2, 0, 2], [0, True, 0, 1, 2, 0, 2]):
            changed = copy.deepcopy(valid)
            changed["mapping_counts"] = counts
            with self.subTest(counts=counts), self.assertRaises(ValueError):
                closeout.validate_evidence(changed)
        for field in ("originals_absent_from_alias_wire", "latest_vault_rollback_verified"):
            changed = copy.deepcopy(valid)
            changed[field] = False
            with self.subTest(field=field), self.assertRaises(ValueError):
                closeout.validate_evidence(changed)
        changed = copy.deepcopy(valid)
        changed["alias_response_proofs"] = 0
        with self.assertRaises(ValueError):
            closeout.validate_evidence(changed)


if __name__ == "__main__":
    unittest.main()
