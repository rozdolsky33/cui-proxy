"""Whole-branch review regressions; prepared statically before production fixes."""
import copy
from contextlib import nullcontext
from email.message import Message
import gzip
import hashlib
import inspect
import io
import json
from pathlib import Path
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import harness
import release_archive as archive
import release_closeout as closeout


def digest(label):
    return hashlib.sha256(label.encode()).hexdigest()


def valid_evidence():
    valid = {"candidate": {"binary_sha256": "a" * 64, "source_commit": "b" * 40,
                                   "manifest_sha256": "c" * 64, "version": "0.7.4", "embedded_clean_revision": True},
                     "baseline": {"binary_sha256": "d" * 64, "provenance_sha256": "e" * 64,
                                  "asset_verified": True, "version": "0.7.3"},
                     "producer": {"commit": "f" * 40, "clean": True,
                                  "module_sha256": {name: "a" * 64 for name in
                                                    ("release_closeout.py", "release_archive.py", "harness.py", "ocr_recovery_probe.py")}},
                     "archive": {"archive_sha256": "b" * 64, "binary_sha256": "a" * 64,
                                 "stock_member_layout_verified": True, "stock_builder_executed_by_probe": False},
                     "stages": {name: {key: True for key in ("check_config", "preflight", "identity", "graceful_exit")}
                                for name in ("baseline", "candidate", "rollback")},
                     "canonical_requests": 4, "alias_response_proofs": 2,
                     "mapping_counts": [0, 1, 1, 1, 2, 2, 2],
                     "originals_absent_from_alias_wire": True, "latest_vault_rollback_verified": True,
                     "backup_preserved": True, "archive_stable": True, "candidate_binary_stable": True,
                     "baseline_binary_stable": True, "rollback_healthy": True,
                     "audit": {"records": 4, "sanitized": 2, "forwarded": 2, "metadata_only": True,
                               "privacy_verified": True, "sha256": "c" * 64},
                     "privacy": {"scan_completed": True, "shutdown_complete": True,
                                 "handles_closed": True, "value_free": True, "leaking_files": 0},
                     "cleanup": {"private_environment_removed": True, "children_stopped": True,
                                 "extracted_directory_removed": True}}
    valid["mapping_counts_kind"] = "process_local_vault_size_gauge"
    valid["effective_config"] = {"sha256": "f" * 64, "profile": 'sanitize', "on_error": "block",
                                         "attachments": "block", "vault_mode": "file",
                                         "loopback_only": True, "private_paths": True}
    valid["effective_config"] = {"sha256": "d" * 64, "profile": 'sanitize', "on_error": "block",
                                         "attachments": "block", "vault_mode": "file", "loopback_only": True,
                                         "private_paths": True}
    valid["mapping_counts_kind"] = "process_local_vault_size_gauge"
    valid["baseline"].update(version="0.7.3", release_tag="v0.7.3",
        source_commit="b" * 40, embedded_source_commit="b" * 40,
        asset_sha256=digest("baseline-asset"), checksums_sha256=digest("checksums"),
        attribution="operator_supplied_release_asset")
    valid["archive"].update(packaging_method="exact_byte_verification_archive",
        binary_sha256=valid["candidate"]["binary_sha256"], archive_bytes=4096,
        member_sha256={name: digest(name) for name in archive.STOCK_FILES})
    valid["archive"]["member_sha256"]["cui-proxy"] = valid["candidate"]["binary_sha256"]
    valid["backup_sha256"] = {"vault": digest("preupgrade-vault"),
        "key": digest("unchanged-key"), "config": valid["effective_config"]["sha256"]}
    valid["postcandidate_vault_sha256"] = digest("postcandidate-vault")
    # New required handoff proof: sampled immediately after check_latest_vault
    # and before rollback startup; not a final post-flush ciphertext invariant.
    valid["rollback_input_vault_sha256"] = valid["postcandidate_vault_sha256"]
    # New required explicit proof: native code already checks this relationship,
    # but the old producer does not retain its final observation for validation.
    valid["final_key_sha256"] = valid["backup_sha256"]["key"]
    valid["audit"]["chain_verified"] = True
    return valid


class ReceiverReviewTests(unittest.TestCase):
    def receive(self, raw, extra_headers=(), *, proof=True):
        with tempfile.TemporaryDirectory() as temporary:
            folder = Path(temporary)
            captured = {}
            def factory(address, handler):
                captured["handler"] = handler
                return SimpleNamespace(server_port=1234, serve_forever=lambda: None)
            with patch.object(harness.http.server, "ThreadingHTTPServer", side_effect=factory):
                harness.upstream_main(folder, response_proof=proof)
            receiver = object.__new__(captured["handler"])
            receiver.rfile = io.BytesIO(raw)
            receiver.wfile = io.BytesIO()
            receiver.headers = Message()
            receiver.headers["Content-Length"] = str(len(raw))
            for name, value in extra_headers:
                receiver.headers[name] = value
            statuses = []
            receiver.send_error = lambda status, *args: statuses.append(status)
            receiver.send_response = lambda status, *args: statuses.append(status)
            receiver.send_header = lambda *args: None
            receiver.end_headers = lambda: None
            error = None
            try:
                with patch.object(harness.time, "sleep", return_value=None):
                    receiver.do_POST()
            except (ValueError, harness.CheckFailure) as caught:
                error = caught
            records = [json.loads(p.read_bytes()) for p in folder.glob("*.json")
                       if p.name != "ready.json"]
            return statuses, error, records

    def body(self):
        return {"metadata": {"harness_id": "review-case"},
                "input": [{"content": [{"type": "input_text", "text": "echo"}]}]}

    def assert_rejected_without_completed_receipt(self, raw, headers=()):
        statuses, error, records = self.receive(raw, headers)
        self.assertTrue(error is not None or any(status >= 400 for status in statuses))
        self.assertFalse(any(row.get("response_proof", {}).get("completed") is True
                             for row in records))

    def test_opt_in_receiver_rejects_duplicate_json_keys_at_every_depth(self):
        wire = json.dumps(self.body(), separators=(",", ":")).encode()
        for raw in (wire.replace(b'{"metadata":', b'{"metadata":{},"metadata":', 1),
                    wire.replace(b'"harness_id":', b'"harness_id":"ignored","harness_id":', 1),
                    wire.replace(b'"text":"echo"', b'"text":"ignored","text":"echo"', 1)):
            with self.subTest(level=len(raw)):
                self.assert_rejected_without_completed_receipt(raw)

    def test_opt_in_receiver_rejects_case_insensitive_repeated_headers(self):
        wire = json.dumps(self.body()).encode()
        self.assert_rejected_without_completed_receipt(wire,
            (("X-Review", "first"), ("x-review", "second")))

    def test_default_receiver_retains_legacy_duplicate_capture_compatibility(self):
        wire = json.dumps(self.body()).encode().replace(b'"text": "echo"',
            b'"text": "ignored", "text": "echo"')
        statuses, error, records = self.receive(wire,
            (("X-Review", "first"), ("x-review", "second")), proof=False)
        self.assertIsNone(error)
        self.assertIn(200, statuses)
        self.assertEqual(len(records), 1)
        self.assertNotIn("response_proof", records[0])


class FrameReviewTests(unittest.TestCase):
    def wire(self):
        payload = json.dumps([{"role": "user", "content": []}])
        event = {"type": "response.output_text.delta", "item_id": "item-output",
                 "output_index": 0, "delta": payload}
        return ("data: " + json.dumps(event) + "\n\n" +
                'data: {"type":"response.completed"}\n\n').encode()

    def test_proof_requires_terminal_sse_delimiter_for_lf_and_crlf(self):
        for raw in (self.wire()[:-2], self.wire().replace(b"\n", b"\r\n")[:-4]):
            proof = harness.UpstreamResponseProof()
            proof.record(raw, len(raw))
            with self.subTest(newline=b"\r" in raw), self.assertRaises(harness.CheckFailure):
                proof.finish()
            self.assertEqual(proof._buffer, bytearray())
            # The strict proof rule must not change the legacy decoder default.
            self.assertIsInstance(harness.decode_stream(raw), list)

    def test_proof_accepts_fully_delimited_lf_and_crlf_and_discards_ram(self):
        for raw in (self.wire(), self.wire().replace(b"\n", b"\r\n")):
            proof = harness.UpstreamResponseProof()
            proof.record(raw, len(raw))
            self.assertTrue(proof.finish()["completed"])
            self.assertEqual(proof._buffer, bytearray())


class EvidenceReviewTests(unittest.TestCase):
    def reject(self, report):
        before = copy.deepcopy(report)
        with self.assertRaises(ValueError):
            closeout.validate_evidence(report)
        self.assertEqual(report, before)

    def test_positive_fixture_contains_complete_internally_consistent_emitted_proofs(self):
        report = valid_evidence()
        before = copy.deepcopy(report)
        closeout.validate_evidence(report)
        self.assertEqual(report, before)

    def test_every_archive_digest_and_packaging_field_is_required(self):
        for field in ("packaging_method", "archive_sha256", "binary_sha256", "archive_bytes",
                      "member_sha256", "stock_member_layout_verified", "stock_builder_executed_by_probe"):
            report = valid_evidence()
            del report["archive"][field]
            with self.subTest(field=field):
                self.reject(report)
        for member in archive.STOCK_FILES:
            report = valid_evidence()
            del report["archive"]["member_sha256"][member]
            with self.subTest(member=member):
                self.reject(report)

    def test_backup_latest_vault_final_key_and_baseline_proofs_are_required(self):
        for field in ("backup_sha256", "postcandidate_vault_sha256", "rollback_input_vault_sha256", "final_key_sha256"):
            report = valid_evidence()
            del report[field]
            with self.subTest(field=field):
                self.reject(report)
        for field in ("vault", "key", "config"):
            report = valid_evidence()
            del report["backup_sha256"][field]
            with self.subTest(backup=field):
                self.reject(report)
        for field in ("asset_sha256", "checksums_sha256", "release_tag", "source_commit",
                      "embedded_source_commit", "attribution"):
            report = valid_evidence()
            del report["baseline"][field]
            with self.subTest(baseline=field):
                self.reject(report)

    def test_contradictory_digest_relationships_cannot_be_acknowledged_by_booleans(self):
        changes = (
            lambda r: r["archive"].update(binary_sha256=digest("different-binary")),
            lambda r: r["archive"]["member_sha256"].update({"cui-proxy": digest("different-member")}),
            lambda r: r["archive"]["member_sha256"].update({"extra": digest("extra")}),
            lambda r: r["archive"].update(packaging_method="unrecognized"),
            lambda r: r["archive"].update(archive_bytes=True),
            lambda r: r["archive"]["member_sha256"].update({"LICENSE": "malformed"}),
            lambda r: r.update(postcandidate_vault_sha256=r["backup_sha256"]["vault"]),
            lambda r: r.update(rollback_input_vault_sha256=digest("wrong-rollback-input")),
            lambda r: r.update(rollback_input_vault_sha256=r["backup_sha256"]["vault"]),
            lambda r: r.update(final_key_sha256=digest("changed-key")),
            lambda r: r["backup_sha256"].update(config=digest("changed-config")),
            lambda r: r["backup_sha256"].update(key="malformed"),
            lambda r: r["baseline"].update(embedded_source_commit="c" * 40),
            lambda r: r["baseline"].update(release_tag="v9.9.9"),
        )
        for index, change in enumerate(changes):
            report = valid_evidence()
            change(report)
            with self.subTest(index=index):
                self.reject(report)


    def test_each_candidate_baseline_and_member_identity_field_is_required_and_typed(self):
        groups = {"candidate": ("binary_sha256", "source_commit", "manifest_sha256", "version", "embedded_clean_revision"),
                  "baseline": ("binary_sha256", "provenance_sha256", "version", "release_tag",
                    "source_commit", "embedded_source_commit", "asset_sha256", "checksums_sha256",
                    "asset_verified", "attribution")}
        for group, fields in groups.items():
            for field in fields:
                report = valid_evidence()
                del report[group][field]
                with self.subTest(group=group, missing=field):
                    self.reject(report)
        for field in ("archive_sha256", "binary_sha256"):
            report = valid_evidence()
            report["archive"][field] = "malformed"
            with self.subTest(malformed=field):
                self.reject(report)
        for member in archive.STOCK_FILES:
            report = valid_evidence()
            report["archive"]["member_sha256"][member] = "malformed"
            with self.subTest(member_digest=member):
                self.reject(report)
        for field in ("postcandidate_vault_sha256", "rollback_input_vault_sha256", "final_key_sha256"):
            report = valid_evidence()
            report[field] = "malformed"
            with self.subTest(malformed=field):
                self.reject(report)


    def test_rollback_handoff_digest_is_required_and_matches_latest_candidate_not_backup(self):
        valid = valid_evidence()
        self.assertEqual(valid["rollback_input_vault_sha256"], valid["postcandidate_vault_sha256"])
        self.assertNotEqual(valid["rollback_input_vault_sha256"], valid["backup_sha256"]["vault"])
        for replacement in (None, "malformed", digest("unrelated-vault"), valid["backup_sha256"]["vault"]):
            report = copy.deepcopy(valid)
            if replacement is None:
                del report["rollback_input_vault_sha256"]
            else:
                report["rollback_input_vault_sha256"] = replacement
            with self.subTest(replacement_kind=type(replacement).__name__):
                self.reject(report)


def audit_rows():
    rows = []
    previous = "0" * 64
    for index, decision in enumerate(("sanitized", "forwarded", "sanitized", "forwarded")):
        counts = {"secret": 1} if decision == "sanitized" else {}
        row = {"ts": "2026-11-23T00:00:00Z", "request_id": format(index + 1, "016x"),
            "adapter": "codex", "profile": "sanitize", "decision": decision,
            "reason": "", "rule_ids": sorted(counts), "counts": counts,
            "approval_id": "", "prev_hash": previous}
        row["hash"] = hashlib.sha256(json.dumps(row, separators=(",", ":")).encode()).hexdigest()
        previous = row["hash"]
        rows.append(row)
    return rows


class AuditReviewTests(unittest.TestCase):
    def check(self, rows, *, cli_returncode=0):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary).resolve() / "audit.jsonl"
            path.write_text("".join(json.dumps(row) + "\n" for row in rows))
            with patch.object(closeout.subprocess, "run", return_value=SimpleNamespace(
                    returncode=cli_returncode, stdout=b"", stderr=b"")) as verify:
                kwargs = {"binary": "/private/candidate"} if "binary" in inspect.signature(closeout._audit).parameters else {}
                result = closeout._audit(path, {"private-original"}, **kwargs)
                return result, verify.call_args_list, path

    def test_audit_requires_exact_candidate_native_chain_verification_with_captured_output(self):
        result, calls, path = self.check(audit_rows())
        self.assertTrue(result["metadata_only"])
        matching = [call for call in calls if call.args and call.args[0] ==
                    ["/private/candidate", "audit", "verify", "-file", str(path)]]
        self.assertEqual(len(matching), 1, "missing exact-candidate native audit verification")
        self.assertIs(matching[0].kwargs.get("capture_output"), True)
        self.assertTrue(result.get("chain_verified") is True)

    def test_native_audit_verification_failure_never_receives_metadata_only_ack(self):
        with self.assertRaises((ValueError, harness.CheckFailure)):
            self.check(audit_rows(), cli_returncode=1)

    def test_closed_audit_schema_requires_every_field_and_valid_unique_ids(self):
        for field in audit_rows()[0]:
            rows = audit_rows()
            del rows[0][field]
            with self.subTest(missing=field), self.assertRaises((ValueError, harness.CheckFailure)):
                self.check(rows)
        for invalid in (None, "", "not-an-id", "A" * 16, 5):
            rows = audit_rows()
            rows[0]["request_id"] = invalid
            with self.subTest(id_type=type(invalid).__name__), self.assertRaises((ValueError, harness.CheckFailure)):
                self.check(rows)
        rows = audit_rows()
        rows[1]["request_id"] = rows[0]["request_id"]
        with self.assertRaises((ValueError, harness.CheckFailure)):
            self.check(rows)

    def test_closed_audit_nested_counts_and_chain_schema_reject_malformed_metadata(self):
        changes = (
            lambda r: r.update(extra="unexpected"),
            lambda r: r.update(counts={"secret": True}),
            lambda r: r.update(counts={"secret": 0}),
            lambda r: r.update(counts={"secret": -1}),
            lambda r: r.update(counts={"secret": "1"}),
            lambda r: r.update(counts={"unsafe field": 1}),
            lambda r: r.update(rule_ids=["different"]),
            lambda r: r.update(ts="not-a-timestamp"),
            lambda r: r.update(profile="unexpected"),
            lambda r: r.update(approval_id="unexpected"),
            lambda r: r.update(prev_hash="bad"),
            lambda r: r.update(detail="unexpected"),
        )
        for index, change in enumerate(changes):
            rows = audit_rows()
            change(rows[0])
            with self.subTest(index=index), self.assertRaises((ValueError, harness.CheckFailure)):
                self.check(rows)


class OutputBoundaryReviewTests(unittest.TestCase):
    def arguments(self, root, report, *, version="0.7.4"):
        values = {"candidate-binary": root / "candidate", "candidate-sha256": "a" * 64,
            "candidate-source": root / "candidate-source", "candidate-commit": "a" * 40,
            "candidate-version": version, "candidate-manifest": root / "candidate.json",
            "baseline-binary": root / "baseline", "baseline-provenance": root / "baseline.json",
            "producer-source": root / "producer-source", "producer-commit": "b" * 40,
            "reference-lock": root / "lock", "report": report}
        return [part for key, value in values.items() for part in ("--" + key, str(value))]

    def test_main_refuses_source_tree_output_before_creating_any_report_including_aliases(self):
        for tree in ("candidate-source", "producer-source"):
            for alias in (False, True):
                with self.subTest(tree=tree, alias=alias), tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary).resolve()
                    source = root / tree
                    source.mkdir()
                    output_parent = source
                    if alias:
                        output_parent = root / "source-alias"
                        output_parent.symlink_to(source, target_is_directory=True)
                    report = output_parent / "must-not-exist.json"
                    with patch.object(closeout.recovery, "reference_lock", return_value=nullcontext()),                          patch.object(closeout, "run_probe", side_effect=ValueError("release_test_stop")):
                        try:
                            closeout.main(self.arguments(root, report))
                        except (ValueError, OSError, SystemExit):
                            pass
                    self.assertFalse(report.exists())
                    self.assertEqual(list(source.iterdir()), [])

    def test_malformed_candidate_version_never_enters_partial_failure_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            marker = "INVALID_PRIVATE_VERSION_MARKER"
            args = SimpleNamespace(candidate_binary=root / "candidate", candidate_sha256="a" * 64,
                candidate_source=root / "candidate-source", candidate_commit="a" * 40,
                candidate_version=marker, candidate_manifest=root / "manifest.json",
                baseline_binary=root / "baseline", baseline_provenance=root / "baseline.json",
                producer_source=root / "producer-source", producer_commit="b" * 40,
                report=root / "report.json", verification_archive=None)
            manifest = json.dumps({"binary_sha256": args.candidate_sha256,
                "source_commit": args.candidate_commit, "source_clean": True, "dirty": False}).encode()
            report = {}
            with patch.object(closeout.recovery, "bind_identity", return_value={}),                  patch.object(closeout.archive, "capture_file", return_value=manifest),                  patch.object(closeout.archive, "verify_baseline_provenance", return_value={"version": "0.7.3", "source_commit": None}),                  patch.object(closeout, "read_build_info", return_value={}):
                with self.assertRaises(ValueError):
                    closeout.run_probe(args, report)
            self.assertNotIn(marker, json.dumps(report))


class TarTailReviewTests(unittest.TestCase):
    def tar_bytes(self, name, data):
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w", format=tarfile.USTAR_FORMAT) as output:
            member = tarfile.TarInfo(name)
            member.size = len(data)
            member.mode = 0o755 if name == "cui-proxy" else 0o644
            output.addfile(member, io.BytesIO(data))
            if name == "cui-proxy":
                examples = tarfile.TarInfo("examples")
                examples.type = tarfile.DIRTYPE
                examples.mode = 0o755
                output.addfile(examples)
                for extra in sorted(archive.STOCK_FILES - {"cui-proxy"}):
                    contents = b"packaged-document"
                    entry = tarfile.TarInfo(extra)
                    entry.mode = 0o644
                    entry.size = len(contents)
                    output.addfile(entry, io.BytesIO(contents))
        return stream.getvalue()

    def test_archive_rejects_nonzero_bytes_and_second_tar_members_after_end_marker(self):
        binary = b"retained-binary"
        first = self.tar_bytes("cui-proxy", binary)
        for tail in (b"nonzero-trailing-data", self.tar_bytes("second-member", b"extra")):
            with self.subTest(tail_size=len(tail)), tempfile.TemporaryDirectory() as temporary:
                path = Path(temporary).resolve() / "archive.tar.gz"
                path.write_bytes(gzip.compress(first + tail, mtime=0))
                with self.assertRaises(ValueError):
                    archive.verify_archive(path, archive.file_sha256(path),
                        hashlib.sha256(binary).hexdigest())

    def test_archive_allows_only_zero_padding_after_valid_end_marker(self):
        binary = b"retained-binary"
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary).resolve() / "archive.tar.gz"
            path.write_bytes(gzip.compress(self.tar_bytes("cui-proxy", binary) + b"\0" * 1024, mtime=0))
            metadata = archive.verify_archive(path, archive.file_sha256(path),
                hashlib.sha256(binary).hexdigest())
            self.assertEqual(metadata["binary_sha256"], hashlib.sha256(binary).hexdigest())
