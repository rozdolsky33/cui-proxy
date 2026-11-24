#!/usr/bin/env python3
"""Native private same-vault upgrade/rollback evidence on retained release bytes."""
import argparse
import copy
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import uuid
from urllib.parse import urlsplit

import harness
import ocr_recovery_probe as recovery
import release_archive as archive
from performance_churn import flush_proxy
from soak_identity import read_build_info


HEX = re.compile(r"[0-9a-f]{64}\Z")
COMMIT = re.compile(r"[0-9a-f]{40}\Z")


def require(condition, code):
    if not condition:
        raise ValueError("release_closeout_" + code)


def _serialized(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def _absent(value, originals):
    data = _serialized(value)
    for original in originals:
        require(type(original) is str and original, "privacy_oracle")
        for needle in (original.encode(), json.dumps(original)[1:-1].encode()):
            require(needle not in data, "original_on_alias_wire")


def assert_alias_wire(body, headers, originals, aliases):
    require(originals and aliases and type(body) is dict and type(headers) is dict, "alias_wire_shape")
    _absent(body, originals)
    _absent(headers, originals)
    encoded = _serialized(body)
    require(all(type(alias) is str and alias and alias.encode() in encoded for alias in aliases), "alias_missing")


def check_alias_response(proof, inputs, aliases, originals):
    _absent(inputs, originals)
    encoded = _serialized(inputs)
    require(aliases and all(type(alias) is str and alias and alias.encode() in encoded for alias in aliases),
            "upstream_alias_missing")
    require(type(proof) is dict and set(proof) == {"completed", "bytes", "semantic_sha256"}
            and proof["completed"] is True and type(proof["bytes"]) is int
            and 0 < proof["bytes"] <= harness.RESPONSE_PROOF_LIMIT
            and proof["semantic_sha256"] == harness.semantic_sha256(inputs), "upstream_response_proof")


def check_latest_vault(path, candidate_sha256, backup_sha256):
    require(type(candidate_sha256) is str and HEX.fullmatch(candidate_sha256)
            and candidate_sha256 != backup_sha256 and archive.file_sha256(path) == candidate_sha256,
            "latest_vault_required")


def _version(value):
    require(type(value) is str and re.fullmatch(r"v?\d+\.\d+\.\d+", value), "release_version")
    return tuple(int(part) for part in value.removeprefix("v").split("."))


def check_preflight(report, pid, version, revision):
    require(type(report) is dict and report.get("ready") is True and type(report.get("pid")) is int
            and report["pid"] == pid and _version(report.get("version")) == _version(version), "preflight_identity")
    if revision is not None:
        require(report.get("revision") == revision, "preflight_revision")
    policy = report.get("policy", {})
    require(policy.get("profile") == 'sanitize' and policy.get("on_error") == "block"
            and policy.get("attachments") == "block", "preflight_policy")


def _config(path, directory, originals):
    raw = archive.capture_file(path, 1024 * 1024)
    config = json.loads(raw)
    root = Path(directory).resolve()
    require(Path(path).resolve().is_relative_to(root), "configuration_not_private")
    require(all(original.encode() not in raw for original in originals), "original_in_configuration")
    require(config["policy"]["profile"] == 'sanitize' and config["policy"]["on_error"] == "block"
            and config["policy"]["attachments"] == "block" and config["vault"]["mode"] == "file"
            and config["audit"]["enabled"] is True and config["audit"]["on_error"] == "block", "configuration_policy")
    for listen in (config["listen"], config["ui"]["listen"]):
        require(type(listen) is str and re.fullmatch(r"127\.0\.0\.1:\d{1,5}", listen), "configuration_listener")
    for url in config["upstreams"].values():
        parsed = urlsplit(url)
        require(parsed.scheme == "http" and parsed.hostname == "127.0.0.1", "configuration_upstream")
    for path_value in (config["vault"]["file"], config["vault"]["key_file"], config["audit"]["file"]):
        require(Path(path_value).resolve().is_relative_to(root), "configuration_private_path")
    metadata = {"sha256": archive.sha256(raw), "profile": 'sanitize', "on_error": "block",
                "attachments": "block", "vault_mode": "file", "loopback_only": True, "private_paths": True}
    return config, metadata


class ReleaseEnvironment(harness.Environment):
    def __init__(self, baseline, originals, report, failure_directory):
        super().__init__(baseline, profile='sanitize', attachments="block", response_proof=True)
        self.originals = originals
        self.report = report
        self.failure_directory = failure_directory
        self.stage = "baseline"
        self.proxy = None
        self.config_path = None
        self.log_evidence = {}
        self.privacy_values = set(originals)

    def spawn(self, arguments, name, env=None):
        self.directory = self.directory.resolve()
        if len(arguments) > 1 and arguments[1] == "serve":
            self.config_path = Path(arguments[arguments.index("-config") + 1])
            _value, metadata = _config(self.config_path, self.directory, self.originals)
            previous = self.report.setdefault("effective_config", metadata)
            require(previous == metadata, "effective_configuration_changed")
            previous = self.report.setdefault("effective_config", metadata)
            require(previous == metadata, "configuration_changed_between_versions")
            result = subprocess.run([str(arguments[0]), "check-config", "-config", str(self.config_path)],
                                    capture_output=True, timeout=15)
            for suffix, data in (("stdout", result.stdout), ("stderr", result.stderr)):
                path = self.directory / (self.stage + "-check-config." + suffix)
                with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as output:
                    output.write(data)
            require(result.returncode == 0, "check_config_failed")
            self.report["stages"][self.stage]["check_config"] = True
        child = super().spawn(arguments, name, env)
        if len(arguments) > 1 and arguments[1] == "serve":
            self.proxy = child
            self.current_binary = Path(arguments[0])
        return child

    def start(self, binary, stage):
        require(self.proxy is not None and self.proxy.poll() is not None, "previous_proxy_still_running")
        self.stage = stage
        self.spawn([str(binary), "serve", "-config", str(self.config_path)], "proxy-" + stage)

    def running(self, binary_sha256, version, revision):
        observed = {}

        def ready():
            nonlocal observed
            require(self.proxy.poll() is None, "service_start_failed")
            try:
                observed = recovery.read_api(recovery.api_url(self.ui, "/api/preflight"))
            except (OSError, ValueError):
                return False
            return observed.get("ready") is True

        harness.wait_until(ready, 10, "release_preflight_timeout")
        check_preflight(observed, self.proxy.pid, version, revision)
        require(archive.file_sha256(self.current_binary) == binary_sha256, "running_binary_changed")
        self.report["stages"][self.stage].update(preflight=True, identity=True)

    def stop(self):
        require(self.proxy is not None and self.proxy.poll() is None, "service_exited_before_graceful_stop")
        flush_proxy(self.proxy)
        self.report["stages"][self.stage]["graceful_exit"] = True

    def __exit__(self, *error):
        self.temp = recovery.CleanupGate(self.temp, lambda: recovery.captured_log_summary(
            self.directory, self.logs, self.children, self.privacy_values, self.failure_directory,
            self.log_evidence, preserve=error[0] is not None, minimum_captures=1 if error[0] is not None else 2))
        try:
            return super().__exit__(*error)
        finally:
            if not self.temp.called:
                self.temp.cleanup()


def _aliases(expected, observed, original):
    found = []

    def visit(left, right):
        if type(left) is str and left == original:
            require(type(right) is str and right and right != original, "mapping_not_created")
            found.append(right)
        elif type(left) is list:
            require(type(right) is list and len(left) == len(right), "mapping_shape")
            for a, b in zip(left, right):
                visit(a, b)
        elif type(left) is dict:
            require(type(right) is dict and set(left) == set(right), "mapping_shape")
            for key in left:
                visit(left[key], right[key])
        else:
            require(left == right, "mapping_shape")

    visit(expected, observed)
    require(len(found) == 1, "mapping_identity")
    return found


def _request(env, inputs, expected, originals, *, aliases=None):
    ident = "release-" + uuid.uuid4().hex
    body = {"model": "workflow-test", "stream": True, "input": copy.deepcopy(inputs),
            "metadata": {"harness_id": ident, "shape": "echo", "fault": ""}}
    headers = {"Content-Type": "application/json"}
    if aliases is not None:
        assert_alias_wire(body, headers, originals, aliases)
    status, raw, _duration = env.send(body, False, headers)
    proof = {}

    def witnessed():
        nonlocal proof
        proof = env.upstream_response_proof(ident)
        return type(proof) is dict and proof.get("completed") is True

    harness.wait_until(witnessed, 3, "release_upstream_response_proof_missing")
    captures = env.capture(ident)
    harness.check_operation(status, harness.decode_stream(raw), expected, captures, originals, 200, False)
    require(len(captures) == 1, "upstream_capture_count")
    capture = json.loads(captures[0])
    wire = capture["body"]
    actual_headers = capture["headers"]
    if aliases is not None:
        assert_alias_wire(wire, actual_headers, originals, aliases)
        actual_aliases = aliases
    else:
        actual_aliases = _aliases(expected, wire["input"], expected[0]["content"][0]["text"])
    env.privacy_values.update(actual_aliases)
    check_alias_response(proof, wire["input"], actual_aliases, originals)
    env.report["canonical_requests"] += 1
    if aliases is not None:
        env.report["alias_response_proofs"] += 1
    return copy.deepcopy(wire["input"]), actual_aliases


def _audit(path, originals, *, binary):
    raw = archive.capture_file(path, 1024 * 1024)
    require(not any(original.encode() in raw or json.dumps(original)[1:-1].encode() in raw
                    for original in originals), "audit_privacy")
    rows = [harness._proof_loads(line) for line in raw.splitlines()]
    required = {"ts", "request_id", "adapter", "profile", "decision", "reason", "rule_ids",
                "counts", "approval_id", "prev_hash", "hash"}
    require(len(rows) == 4, "audit_records")
    for row in rows:
        require(type(row) is dict and required <= set(row) <= required | {"detail"}, "audit_schema")
        require(type(row["ts"]) is str and re.fullmatch(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z", row["ts"]), "audit_timestamp")
        require(type(row["request_id"]) is str and re.fullmatch(r"[0-9a-f]{16}", row["request_id"]), "audit_request_id")
        require(row["adapter"] == "codex" and row["profile"] == "sanitize" and
                row["reason"] == "" and row.get("detail", "") == "" and row["approval_id"] == "", "audit_fields")
        counts = row["counts"]
        require(type(counts) is dict and all(type(name) is str and re.fullmatch(r"[A-Za-z0-9._-]{1,64}", name)
                and type(count) is int and count > 0 for name, count in counts.items()), "audit_counts")
        require(type(row["rule_ids"]) is list and row["rule_ids"] == sorted(counts), "audit_rule_ids")
        require(all(type(row[key]) is str and HEX.fullmatch(row[key]) for key in ("prev_hash", "hash")), "audit_hash")
    require([row["decision"] for row in rows] == ["sanitized", "forwarded", "sanitized", "forwarded"], "audit_decisions")
    require(len({row["request_id"] for row in rows}) == 4, "audit_request_ids")
    require(rows[0]["prev_hash"] == "0" * 64 and
            all(rows[index]["prev_hash"] == rows[index - 1]["hash"] for index in range(1, 4)), "audit_chain_links")
    verified = subprocess.run([str(binary), "audit", "verify", "-file", str(path)],
                              capture_output=True, timeout=30, check=False)
    require(verified.returncode == 0, "audit_chain_verification")
    return {"records": 4, "sanitized": 2, "forwarded": 2, "metadata_only": True,
            "privacy_verified": True, "chain_verified": True, "sha256": archive.sha256(raw)}


def validate_evidence(report):
    def hashed(value):
        return type(value) is str and HEX.fullmatch(value) is not None
    def committed(value):
        return type(value) is str and COMMIT.fullmatch(value) is not None
    def shape(value, keys):
        return type(value) is dict and set(value) == set(keys)
    for role in ("candidate", "baseline", "producer", "archive", "stages", "audit", "privacy", "cleanup", "effective_config"):
        require(type(report.get(role)) is dict, "missing_" + role)
    candidate = report["candidate"]
    require(shape(candidate, ("binary_sha256", "source_commit", "manifest_sha256", "version", "embedded_clean_revision")) and
            hashed(candidate["binary_sha256"]) and committed(candidate["source_commit"]) and
            hashed(candidate["manifest_sha256"]) and candidate["embedded_clean_revision"] is True, "candidate_identity")
    candidate_version = _version(candidate["version"])
    baseline = report["baseline"]
    require(shape(baseline, ("version", "release_tag", "source_commit", "binary_sha256", "provenance_sha256",
            "asset_sha256", "checksums_sha256", "asset_verified", "attribution", "embedded_source_commit")) and
            all(hashed(baseline[key]) for key in ("binary_sha256", "provenance_sha256", "asset_sha256", "checksums_sha256")) and
            baseline["asset_verified"] is True and baseline["attribution"] == "operator_supplied_release_asset" and
            baseline["binary_sha256"] != candidate["binary_sha256"], "baseline_identity")
    require(_version(baseline["version"]) < candidate_version and
            baseline["release_tag"] == "v" + baseline["version"].removeprefix("v") and
            (baseline["source_commit"] is None or committed(baseline["source_commit"])) and
            (baseline["embedded_source_commit"] is None or committed(baseline["embedded_source_commit"])) and
            (baseline["source_commit"] is None or baseline["source_commit"] == baseline["embedded_source_commit"]), "baseline_identity")
    producer = report["producer"]
    modules = producer.get("module_sha256")
    require(shape(producer, ("commit", "clean", "module_sha256")) and committed(producer["commit"]) and
            producer["clean"] is True and type(modules) is dict and
            {"release_closeout.py", "release_archive.py", "harness.py", "ocr_recovery_probe.py"} <= set(modules) and
            all(hashed(value) for value in modules.values()), "producer_identity")
    packaged = report["archive"]
    require(shape(packaged, ("archive_sha256", "binary_sha256", "archive_bytes", "stock_member_layout_verified",
                            "stock_builder_executed_by_probe", "member_sha256", "packaging_method")) and
            hashed(packaged["archive_sha256"]) and packaged["binary_sha256"] == candidate["binary_sha256"] and
            type(packaged["archive_bytes"]) is int and 0 < packaged["archive_bytes"] <= archive.MAX_ARCHIVE_BYTES and
            packaged["stock_member_layout_verified"] is True and packaged["stock_builder_executed_by_probe"] is False and
            packaged["packaging_method"] in ("exact_byte_verification_archive", "provided_release_asset_bytes"), "archive_identity")
    members = packaged["member_sha256"]
    require(shape(members, archive.STOCK_FILES) and all(hashed(value) for value in members.values()) and
            members["cui-proxy"] == candidate["binary_sha256"], "archive_members")
    require(set(report["stages"]) == {"baseline", "candidate", "rollback"} and
            all(shape(stage, ("check_config", "preflight", "identity", "graceful_exit")) and
                all(stage[key] is True for key in stage) for stage in report["stages"].values()), "stage_health")
    counts = report.get("mapping_counts")
    # Restart gauges have no sample until their first completed request.
    # Alias-only restoration and vault/key evidence establish persistence separately.
    require(report.get("mapping_counts_kind") == "process_local_vault_size_gauge" and
            type(counts) is list and len(counts) == 7 and all(type(count) is int for count in counts) and
            counts[0] == 0 and [counts[index] for index in (1, 3, 4, 6)] == [1, 1, 2, 2] and
            counts[2] in (0, 1) and counts[5] in (0, 2) and report.get("canonical_requests") == 4 and
            report.get("alias_response_proofs") == 2, "canonical_persistence")
    require(all(report.get(key) is True for key in ("originals_absent_from_alias_wire", "latest_vault_rollback_verified",
        "backup_preserved", "archive_stable", "candidate_binary_stable", "baseline_binary_stable", "rollback_healthy")), "persistence_or_stability")
    backup = report.get("backup_sha256")
    require(shape(backup, ("vault", "key", "config")) and all(hashed(value) for value in backup.values()) and
            hashed(report.get("postcandidate_vault_sha256")) and hashed(report.get("rollback_input_vault_sha256")) and
            hashed(report.get("final_key_sha256")) and
            report["postcandidate_vault_sha256"] != backup["vault"] and
            report["rollback_input_vault_sha256"] == report["postcandidate_vault_sha256"] and
            report["final_key_sha256"] == backup["key"], "persistence_digests")
    audit = report["audit"]
    require(shape(audit, ("records", "sanitized", "forwarded", "metadata_only", "privacy_verified", "chain_verified", "sha256")) and
            audit["records"] == 4 and audit["sanitized"] == 2 and audit["forwarded"] == 2 and
            audit["metadata_only"] is True and audit["privacy_verified"] is True and audit["chain_verified"] is True and
            hashed(audit["sha256"]), "audit_evidence")
    privacy = report["privacy"]
    require(all(privacy.get(key) is True for key in ("scan_completed", "shutdown_complete", "handles_closed", "value_free")) and
            type(privacy.get("leaking_files")) is int and privacy["leaking_files"] == 0, "log_privacy")
    require(all(report["cleanup"].get(key) is True for key in
            ("private_environment_removed", "children_stopped", "extracted_directory_removed")), "cleanup_evidence")
    effective = report["effective_config"]
    require(shape(effective, ("sha256", "profile", "on_error", "attachments", "vault_mode", "loopback_only", "private_paths")) and
            hashed(effective["sha256"]) and effective["sha256"] == backup["config"] and effective["profile"] == "sanitize" and
            effective["on_error"] == "block" and effective["attachments"] == "block" and effective["vault_mode"] == "file" and
            effective["loopback_only"] is True and effective["private_paths"] is True, "effective_configuration_evidence")


def _validate_arguments(args):
    _version(args.candidate_version)
    for output in (args.report, args.verification_archive):
            if output is not None:
                target = Path(output).resolve()
                require(not target.is_relative_to(Path(args.candidate_source).resolve())
                        and not target.is_relative_to(Path(args.producer_source).resolve()), "output_inside_source")

def run_probe(args, report):
    _validate_arguments(args)
    identity = recovery.bind_identity(args.candidate_binary, args.candidate_sha256, args.candidate_source,
                                     args.producer_source, args.producer_commit, expected_binary_commit=args.candidate_commit)
    manifest_bytes = archive.capture_file(args.candidate_manifest, 1024 * 1024)
    manifest = json.loads(manifest_bytes)
    require(manifest.get("binary_sha256") == args.candidate_sha256 and manifest.get("source_commit") == args.candidate_commit
            and manifest.get("source_clean") is True and manifest.get("dirty") is False, "candidate_manifest")
    report["candidate"] = {"binary_sha256": args.candidate_sha256, "source_commit": args.candidate_commit,
                           "manifest_sha256": archive.sha256(manifest_bytes), "version": args.candidate_version,
                           "embedded_clean_revision": True}
    report["baseline"] = archive.verify_baseline_provenance(args.baseline_binary, args.baseline_provenance)
    baseline_build = read_build_info(args.baseline_binary)
    baseline_revision = baseline_build.get("vcs.revision")
    if baseline_revision and report["baseline"]["source_commit"]:
        require(baseline_revision == report["baseline"]["source_commit"], "baseline_embedded_source")
    report["baseline"]["embedded_source_commit"] = baseline_revision
    require(_version(report["baseline"]["version"]) < _version(args.candidate_version), "baseline_not_prior_release")
    module_paths = {"release_closeout.py": Path(__file__), "release_archive.py": Path(archive.__file__),
                    "harness.py": Path(harness.__file__), "ocr_recovery_probe.py": Path(recovery.__file__)}
    root = Path(args.producer_source).resolve()
    require(all(path.resolve().is_relative_to(root) for path in module_paths.values()), "producer_import_outside_source")
    report["producer"] = {"commit": args.producer_commit, "clean": True,
                          "module_sha256": {name: archive.file_sha256(path) for name, path in module_paths.items()}}
    if args.candidate_archive is not None:
        require(args.candidate_archive_sha256 is not None and args.verification_archive is None, "archive_selection")
        asset = Path(args.candidate_archive)
        report["archive"] = archive.verify_archive(asset, args.candidate_archive_sha256, args.candidate_sha256,
                                                    source_root=args.candidate_source)
        report["archive"]["packaging_method"] = "provided_release_asset_bytes"
    else:
        require(args.verification_archive is not None and args.candidate_archive_sha256 is None, "archive_selection")
        asset = Path(args.verification_archive)
        report["archive"] = archive.create_verification_archive(args.candidate_binary, args.candidate_source, asset,
                                                                 args.candidate_sha256)
    originals = list(harness.scenario("W01", 0)[2])
    require(len(originals) >= 2 and originals[0] != originals[1], "controlled_originals")
    originals = originals[:2]
    report.update(stages={name: {key: False for key in ("check_config", "preflight", "identity", "graceful_exit")}
                         for name in ("baseline", "candidate", "rollback")}, canonical_requests=0, alias_response_proofs=0,
                  mapping_counts=[], rollback_healthy=False)
    env = None
    extracted = None
    try:
        with tempfile.TemporaryDirectory(prefix="release-extracted-") as extracted_directory:
            extracted = Path(extracted_directory).resolve() / "package"
            candidate = archive.extract_archive(asset, extracted, report["archive"]["archive_sha256"], args.candidate_sha256,
                                                 source_root=args.candidate_source)
            try:
                env = ReleaseEnvironment(args.baseline_binary, originals, report, str(args.report) + ".private-failure-logs")
                with env:
                    env.running(report["baseline"]["binary_sha256"], report["baseline"]["version"], baseline_revision)
                    config, _metadata = _config(env.config_path, env.directory, originals)
                    vault, key, audit = (Path(config["vault"]["file"]), Path(config["vault"]["key_file"]),
                                         Path(config["audit"]["file"]))
                    report["mapping_counts_kind"] = "process_local_vault_size_gauge"
                    counts = report["mapping_counts"]
                    counts.append(env.vault_count())
                    expected_a, expected_b = ([harness.message(value)] for value in originals)
                    aliases_a, alias_values_a = _request(env, expected_a, expected_a, originals)
                    counts.append(env.vault_count())
                    env.stop()
                    backup = env.directory / "consistent-preupgrade-backup"
                    backup.mkdir(mode=0o700)
                    backup_hashes = {}
                    for label, source in (("vault", vault), ("key", key), ("config", env.config_path)):
                        data = archive.capture_file(source)
                        (backup / label).write_bytes(data)
                        (backup / label).chmod(0o600)
                        backup_hashes[label] = archive.sha256(data)
                    report["backup_sha256"] = dict(backup_hashes)
                    env.start(candidate, "candidate")
                    env.running(args.candidate_sha256, args.candidate_version, args.candidate_commit)
                    counts.append(env.vault_count())
                    _request(env, aliases_a, expected_a, originals, aliases=alias_values_a)
                    counts.append(env.vault_count())
                    aliases_b, alias_values_b = _request(env, expected_b, expected_b, originals)
                    counts.append(env.vault_count())
                    env.stop()
                    latest = archive.file_sha256(vault)
                    require(archive.file_sha256(key) == backup_hashes["key"], "key_changed")
                    check_latest_vault(vault, latest, backup_hashes["vault"])
                    rollback_input = archive.file_sha256(vault)
                    require(rollback_input == latest and rollback_input != backup_hashes["vault"], "rollback_input_vault")
                    report["rollback_input_vault_sha256"] = rollback_input
                    report["postcandidate_vault_sha256"] = latest
                    env.start(args.baseline_binary, "rollback")
                    env.running(report["baseline"]["binary_sha256"], report["baseline"]["version"], baseline_revision)
                    counts.append(env.vault_count())
                    _request(env, aliases_a + aliases_b, expected_a + expected_b, originals, aliases=alias_values_a + alias_values_b)
                    counts.append(env.vault_count())
                    env.stop()
                    report["rollback_healthy"] = True
                    report["audit"] = _audit(audit, list(env.privacy_values), binary=candidate)
                    _final_config, final_metadata = _config(env.config_path, env.directory, originals)
                    require(final_metadata == report["effective_config"], "effective_configuration_changed")
                    final_key = archive.file_sha256(key)
                    require(final_key == backup_hashes["key"], "key_changed_after_rollback")
                    report["final_key_sha256"] = final_key
                    report.update(originals_absent_from_alias_wire=True, latest_vault_rollback_verified=True,
                                  backup_preserved=all(archive.file_sha256(backup / name) == value for name, value in backup_hashes.items()),
                                  archive_stable=archive.file_sha256(asset) == report["archive"]["archive_sha256"],
                                  candidate_binary_stable=(archive.file_sha256(candidate) == args.candidate_sha256
                                                           and archive.file_sha256(args.candidate_binary) == args.candidate_sha256),
                                  baseline_binary_stable=archive.file_sha256(args.baseline_binary) == report["baseline"]["binary_sha256"])
            finally:
                if env is not None:
                    report["privacy"] = dict(env.log_evidence)
                    report["cleanup"] = {"private_environment_removed": not env.directory.exists(),
                                         "children_stopped": all(child.poll() is not None for child in env.children)}
    finally:
        if extracted is not None:
            report.setdefault("cleanup", {})["extracted_directory_removed"] = not extracted.exists()
    require(recovery.bind_identity(args.candidate_binary, args.candidate_sha256, args.candidate_source,
                                  args.producer_source, args.producer_commit, expected_binary_commit=args.candidate_commit) == identity,
            "identity_changed")
    require(archive.file_sha256(args.candidate_manifest) == report["candidate"]["manifest_sha256"], "manifest_changed")
    current_baseline = archive.verify_baseline_provenance(args.baseline_binary, args.baseline_provenance)
    require(all(report["baseline"].get(key) == value for key, value in current_baseline.items()),
            "baseline_provenance_changed")
    require(all(archive.file_sha256(path) == report["producer"]["module_sha256"][name]
                for name, path in module_paths.items()), "producer_module_changed")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("candidate-binary", "candidate-sha256", "candidate-source", "candidate-commit", "candidate-version",
                 "candidate-manifest", "baseline-binary", "baseline-provenance", "producer-source", "producer-commit",
                 "reference-lock", "report"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--candidate-archive")
    parser.add_argument("--candidate-archive-sha256")
    parser.add_argument("--verification-archive")
    args = parser.parse_args(argv)
    _validate_arguments(args)
    report = {"schema_version": 1, "scope": "native_exact_byte_upgrade_rollback", "passed": False}
    try:
        with recovery.reference_lock(args.reference_lock):
            descriptor = os.open(args.report, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            try:
                run_probe(args, report)
                validate_evidence(report)
                report["passed"] = True
            except BaseException as error:
                message = str(error)
                report["failure_code"] = message if isinstance(error, ValueError) and re.fullmatch(r"release_[a-z_]+", message) else "release_closeout_failed"
                report["error_class"] = type(error).__name__
            finally:
                with os.fdopen(descriptor, "w") as output:
                    json.dump(report, output, sort_keys=True, indent=2, allow_nan=False)
                    output.write("\n")
                    output.flush()
                    os.fsync(output.fileno())
    except (OSError, recovery.ProbeFailure):
        return 1
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
