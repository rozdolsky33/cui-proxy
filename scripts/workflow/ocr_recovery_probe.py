#!/usr/bin/env python3
"""Live, metadata-only OCR dependency recovery evidence; run under the reference lock."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import subprocess
import tempfile
import time
from urllib.parse import urlsplit, urlunsplit
import uuid

import harness
import performance as perf
from extraction_fixtures import validate_coverage
from extraction_performance import attachment_case
from performance_workload import Workload
from soak_boundary import verify_deferred_boundary
from soak_identity import read_build_info
from soak_receipts import receipt_id, verify_receipt
from harness import OPENER
from urllib.error import HTTPError
from urllib.request import Request


CANDIDATE_COMMIT = "c8aaef5799c75bccb19a7121304198507d36649b"
TOOL_NAMES = ('tesseract', 'pdftotext', 'pdftoppm', 'pdfinfo')
DETAIL = "attachment_image_missing_tool"
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
REQUEST_ID = re.compile(r"[0-9a-f]{16}\Z")


class ProbeFailure(RuntimeError):
    """Only fixed codes from this module may escape into retained evidence."""


def require(condition, code):
    if not condition:
        raise ProbeFailure(code)


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


@contextmanager
def reference_lock(path):
    """Use the existing global lock inode; never create, truncate, or unlink it."""
    descriptor = None
    try:
        descriptor = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
        require(stat.S_ISREG(os.fstat(descriptor).st_mode), "reference_lock_invalid")
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ProbeFailure("reference_machine_busy") from None
        yield
    except OSError:
        raise ProbeFailure("reference_lock_unavailable") from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


class PrivateExtractors:
    """Own three regular-file launchers; removing OCR never touches a real tool."""
    def __init__(self, directory, tools):
        self.directory = Path(directory)
        self.tools = {name: Path(tools[name]).resolve(strict=True) for name in TOOL_NAMES}
        self.originals = {}
        self.wrappers = {}
        self.created = False
        self.removed = False
        self.cleanup = {"launcher_restored": False, "real_tools_unchanged": False,
                        "private_directory_removed": False}

    def __enter__(self):
        try:
            self.directory.mkdir(mode=0o700)
            self.created = True
            for name, tool in self.tools.items():
                require(tool.is_file() and os.access(tool, os.X_OK), "real_extractor_unavailable")
                self.originals[name] = digest(tool)
                content = ("#!/bin/sh\nexec " + shlex.quote(str(tool)) + ' "$@"\n').encode()
                self.wrappers[name] = content
                path = self.directory / name
                with path.open("xb") as output:
                    output.write(content)
                path.chmod(0o700)
        except BaseException as error:
            self.__exit__(None, None, None)
            if isinstance(error, OSError):
                raise ProbeFailure("private_launcher_setup_failed") from None
            raise
        return self

    def _owned(self, path, name):
        require(not path.is_symlink() and path.is_file()
                and path.read_bytes() == self.wrappers[name], "private_launcher_changed")

    def environment(self):
        environment = dict(os.environ)
        environment["PATH"] = str(self.directory)
        for name in ("TESSDATA_PREFIX", "TESSDATA_DIR"):
            environment.pop(name, None)
        return environment

    @contextmanager
    def without_ocr(self):
        launcher, backup = self.directory / "tesseract", self.directory / "tesseract.removed"
        self._owned(launcher, "tesseract")
        require(not backup.exists() and not self.removed, "private_launcher_state")
        launcher.rename(backup)
        self.removed = True
        try:
            yield
        finally:
            require(not launcher.exists(), "private_launcher_restore_failed")
            self._owned(backup, "tesseract")
            backup.rename(launcher)
            self.removed = False
            self.cleanup["launcher_restored"] = True

    def __exit__(self, *_error):
        if not self.created:
            return
        if self.removed:
            backup = self.directory / "tesseract.removed"
            self._owned(backup, "tesseract")
            require(not (self.directory / "tesseract").exists(), "private_launcher_restore_failed")
            backup.rename(self.directory / "tesseract")
            self.removed = False
        self.cleanup["launcher_restored"] = True
        unchanged = all(digest(self.tools[name]) == expected for name, expected in self.originals.items())
        self.cleanup["real_tools_unchanged"] = unchanged
        for name in self.wrappers:
            path = self.directory / name
            if path.exists() or path.is_symlink():
                self._owned(path, name)
                path.unlink()
        self.directory.rmdir()
        self.cleanup["private_directory_removed"] = not self.directory.exists()
        require(unchanged and all(self.cleanup.values()), "extractor_cleanup_failed")


def captured(arguments, cwd=None):
    result = subprocess.run(arguments, cwd=cwd, capture_output=True, check=False, text=True)
    require(result.returncode == 0, "identity_command_failed")
    return result.stdout.strip()


def source_binding(source, expected_commit):
    root = Path(source).resolve(strict=True)
    require(re.fullmatch(r"[0-9a-f]{40}", expected_commit) is not None, "source_commit_invalid")
    actual = captured(["git", "rev-parse", "HEAD"], root)
    require(actual == expected_commit, "source_commit_mismatch")
    require(not captured(["git", "status", "--porcelain", "--untracked-files=normal"], root), "source_dirty")
    return {"commit": actual, "clean": True}


def bind_identity(binary, expected_sha256, binary_source, producer_source, producer_commit):
    require(HEX64.fullmatch(expected_sha256) is not None, "binary_sha256_invalid")
    require(digest(binary) == expected_sha256, "binary_sha256_mismatch")
    binary_binding = source_binding(binary_source, CANDIDATE_COMMIT)
    build = read_build_info(binary)
    require(build.get("vcs.revision") == CANDIDATE_COMMIT and build.get("vcs.modified") == "false",
            "embedded_binary_identity_mismatch")
    producer = source_binding(producer_source, producer_commit)
    root = Path(producer_source).resolve(strict=True)
    modules = (Path(__file__), Path(perf.__file__), Path(harness.__file__),
               Path(attachment_case.__code__.co_filename), Path(verify_deferred_boundary.__code__.co_filename))
    files = {}
    for module in modules:
        resolved = module.resolve(strict=True)
        require(resolved.is_relative_to(root), "producer_import_outside_source")
        relative = str(resolved.relative_to(root))
        require(captured(["git", "ls-files", "--error-unmatch", relative], root) == relative,
                "producer_import_untracked")
        files[relative] = digest(resolved)
    return {"binary_sha256": expected_sha256, "binary_source": binary_binding,
            "embedded_clean_revision": True, "producer_source": producer, "producer_files": files}


def api_url(base, path):
    parts = urlsplit(base)
    require(parts.scheme == "http" and parts.hostname in ("127.0.0.1", "localhost"), "nonlocal_probe_url")
    return urlunsplit((parts.scheme, parts.netloc, path, "", ""))


def read_api(url):
    with harness.OPENER.open(url, timeout=5) as response:
        return json.load(response)


def read_receipt(upstream_url, operation):
    return read_api(api_url(upstream_url, "/benchmark/receipt/" + receipt_id(operation)))


def require_receipt(upstream_url, operation, expected_hash=None, *, absent=False):
    require(absent or (isinstance(expected_hash, str) and HEX64.fullmatch(expected_hash)),
            "canonical_receipt_hash_missing")
    receipt = read_receipt(upstream_url, operation)
    verify_receipt(receipt, absent=absent, expected_hash=expected_hash)
    return True


def check_missing_failure(status, raw):
    require(status == 422, "missing_tool_status")
    try:
        error = json.loads(raw)["error"]
        guidance = error["recovery"]
        ident = error["request_id"]
        require(error.get("code") == 'sanitize' and DETAIL in error.get("message", ""),
                "missing_tool_error")
        require(error.get("type") == "cui_proxy_blocked", "missing_tool_error_family")
        require(guidance.get("code") == DETAIL and guidance.get("category") == "dependency"
                and guidance.get("retry") == "after_repair", "missing_tool_recovery_code")
        remedy = guidance.get("remedy")
        require(isinstance(remedy, str) and "Tesseract" in remedy and "restart" in remedy.lower(),
                "missing_tool_recovery_guidance")
        require(isinstance(ident, str) and REQUEST_ID.fullmatch(ident), "failure_audit_identity_missing")
    except (ValueError, KeyError, TypeError, AttributeError):
        raise ProbeFailure("missing_tool_response_shape") from None
    return {"request_id": ident, "code": DETAIL}


def successful(row):
    return (row.get("status") == 200 and row.get("completed") is True
            and row.get("outcome") == "supported_success" and row.get("semantic_verified") is True
            and row.get("boundary_verified") is True)


def check_overlap(rows, failure_started, failure_finished):
    require(len(rows) == 3 and failure_started <= failure_finished, "text_overlap_shape")
    for row in rows:
        require(successful(row), "text_semantics_failed")
        require(row.get("started_s", float("inf")) <= failure_started
                and row.get("finished_s", float("-inf")) >= failure_finished, "three_text_overlap_not_observed")
    return 3


def audit_summary(path, successful_ids, failure_id, forbidden_values):
    allowed = {"ts", "request_id", "adapter", "profile", "decision", "reason", "rule_ids", "counts",
               "detail", "approval_id", "prev_hash", "hash"}
    raw = Path(path).read_bytes()
    require(forbidden_values and all(isinstance(value, str) and value for value in forbidden_values),
            "audit_privacy_oracle_missing")
    require(not any(value.encode() in raw for value in forbidden_values), "audit_value_leak")
    try:
        rows = [json.loads(line) for line in raw.splitlines()]
        require(all(type(row) is dict and set(row) <= allowed for row in rows), "audit_schema_not_metadata_only")
        require(all(
            row.get("reason") == "sanitize" if row.get("decision") == "blocked"
            else row.get("reason", "") == "" for row in rows
        ), "audit_reason_mismatch")
        expected = set(successful_ids) | {failure_id}
        require(len(expected) == len(successful_ids) + 1 and len(rows) == len(expected)
                and {row.get("request_id") for row in rows} == expected, "audit_request_binding_missing")
        require(all(isinstance(row.get("hash"), str) and HEX64.fullmatch(row["hash"]) for row in rows),
                "audit_hash_missing")
        failures = [row for row in rows if row["request_id"] == failure_id]
        require(len(failures) == 1 and failures[0].get("decision") == "blocked"
                and failures[0].get("detail") == DETAIL, "audit_missing_tool_block_missing")
        require(all(row.get("decision") == 'sanitized' for row in rows if row["request_id"] != failure_id),
                "audit_success_binding_failed")
        require(all(row.get("detail", "") == "" for row in rows
                    if row.get("decision") == "sanitized"), "audit_success_detail")
    except (ValueError, TypeError, KeyError):
        raise ProbeFailure("audit_invalid") from None
    return {"sha256": hashlib.sha256(raw).hexdigest(), "records": len(rows),
            "missing_tool_blocks": 1, "metadata_only": True, "privacy_verified": True}


class CleanupGate:
    """Run the closed-log check immediately before Harness deletes its directory."""
    def __init__(self, temporary, callback):
        self.temporary = temporary
        self.callback = callback
        self.called = False

    def cleanup(self):
        if self.called:
            return
        self.called = True
        try:
            self.callback()
        finally:
            self.temporary.cleanup()


def captured_log_summary(directory, handles, children, forbidden_values, failure_directory, evidence,
                         *, preserve=False):
    """Inspect final captures locally; keep only safe names/counts/hashes in evidence."""
    validated_capture_sources = set()
    root = Path(directory).resolve(strict=True)
    values = set(forbidden_values)
    paths = {Path(handle.name) for handle in handles}
    paths.update(path for path in root.rglob("*") if path.suffix in (".log", ".stdout", ".stderr"))
    files = []
    evidence.update(shutdown_complete=all(child.poll() is not None for child in children),
                    handles_closed=all(handle.closed for handle in handles),
                    scan_completed=False, value_free=False, leaking_files=0,
                    checked_known_values=len(values), failure_logs_retained=False, files=[])

    def retain():
        if not files:
            return
        target = Path(failure_directory)
        target.mkdir(mode=0o700)
        for label, source, _data in files:
            descriptor = os.open(target / label, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(descriptor, "wb") as destination, source.open("rb") as original:
                shutil.copyfileobj(original, destination)
        evidence["failure_logs_retained"] = True

    try:
        for index, source in enumerate(sorted(paths), 1):
            require(not source.is_symlink() and source.is_file() and source.resolve().is_relative_to(root),
                    "captured_log_path_invalid")
            resolved_capture_source = source.resolve()
            if resolved_capture_source in validated_capture_sources:
                continue
            validated_capture_sources.add(resolved_capture_source)
            label = source.name if source.name in ("backend.log", "proxy.log", "upstream.log") else f"capture_{index:03}.log"
            require(label not in {item[0] for item in files}, "captured_log_name_collision")
            raw = source.read_bytes()
            files.append((label, source, raw))
            evidence["files"].append({"name": label, "bytes": len(raw),
                                      "sha256": hashlib.sha256(raw).hexdigest()})
        require(values and all(isinstance(value, str) and value for value in values),
                "captured_log_privacy_oracle_missing")
        needles = {encoded for value in values for encoded in (
            value.encode(), json.dumps(value, ensure_ascii=True)[1:-1].encode(),
            json.dumps(value, ensure_ascii=False)[1:-1].encode())}
        for _label, _source, raw in files:
            if any(needle in raw for needle in needles):
                evidence["leaking_files"] += 1
        require(len(handles) >= 2 and len(files) >= 2, "captured_logs_missing")
        require(evidence["shutdown_complete"] and evidence["handles_closed"], "captured_logs_not_final")
        evidence["scan_completed"] = True
        evidence["value_free"] = evidence["leaking_files"] == 0
        require(evidence["value_free"], "captured_log_value_leak")
    except BaseException:
        retain()
        raise
    if preserve:
        retain()


class LiveEnvironment(perf.Environment):
    def __init__(self, binary, seed_binary, workload, size, isolated, privacy_values, failure_directory):
        super().__init__(binary, seed_binary, workload, size, correlate=True)
        self.isolated = isolated
        self.privacy_values = set(privacy_values)
        self.failure_directory = failure_directory
        self.log_evidence = {}

    def spawn(self, arguments, name, env=None):
        if Path(arguments[0]).resolve() == Path(self.binary).resolve():
            self.privacy_values.update(self.mappings)
            env = self.isolated.environment()
        return super().spawn(arguments, name, env)

    def __exit__(self, exception_type, exception, traceback):
        if not isinstance(self.temp, CleanupGate):
            self.temp = CleanupGate(self.temp, lambda: captured_log_summary(
                self.directory, self.logs, self.children, self.privacy_values,
                self.failure_directory, self.log_evidence, preserve=exception_type is not None))
        try:
            return super().__exit__(exception_type, exception, traceback)
        finally:
            # Even an earlier Harness shutdown failure must reach its original
            # directory cleanup; a partial shutdown can never yield clean proof.
            if not self.temp.called:
                self.temp.cleanup()


def wait_preflight(env, *, missing=False, pid=None):
    observed = {}

    def ready():
        nonlocal observed
        observed = read_api(api_url(env.ui, "/api/preflight"))
        if pid is not None:
            require(observed.get("pid") == pid, "proxy_restarted_during_recovery")
        checks = {row.get("name"): row for row in observed.get("tools", [])}
        require(observed.get("policy", {}).get("attachments") == "extract_text", "extraction_policy_missing")
        if missing:
            tool = checks.get("tesseract", {})
            return (observed.get("ready") is False and tool.get("code") == "missing_tool"
                    and tool.get("ready") is False and bool(tool.get("remedy"))
                    and all(checks.get(name, {}).get("ready") is True for name in TOOL_NAMES[1:]))
        return observed.get("ready") is True and all(checks.get(name, {}).get("ready") is True for name in TOOL_NAMES)

    harness.wait_until(ready, 22, "ocr_recovery_preflight_timeout")
    require(type(observed.get("pid")) is int and observed["pid"] > 0, "preflight_process_identity_missing")
    return observed["pid"]


def cache_hits(env):
    hits = env.metrics().get("extraction_cache_hits")
    require(type(hits) is int and hits >= 0, "extraction_cache_evidence_missing")
    return hits


def validate_evidence(report):
    """Absence of any required proof keeps this functional probe unqualified."""
    identity = report.get("identity", {})
    require(identity.get("binary_source", {}).get("commit") == CANDIDATE_COMMIT
            and identity.get("binary_source", {}).get("clean") is True
            and identity.get("embedded_clean_revision") is True
            and HEX64.fullmatch(identity.get("binary_sha256", ""))
            and report.get("identity_stable") is True, "candidate_binding_evidence_missing")
    producer = identity.get("producer_source", {})
    require(re.fullmatch(r"[0-9a-f]{40}", producer.get("commit", "")) and producer.get("clean") is True
            and len(identity.get("producer_files", {})) >= 4
            and all(HEX64.fullmatch(value) for value in identity["producer_files"].values()),
            "producer_binding_evidence_missing")
    require(report.get("image_successes") == 2 and report.get("text_semantic_successes") == 3
            and report.get("canonical_receipts_verified") == 5 and report.get("text_overlap_count") == 3
            and report.get("same_proxy_process") is True
            and report.get("baseline_uncached") is True and report.get("restored_uncached") is True,
            "canonical_operation_evidence_missing")
    missing = report.get("missing_image", {})
    require(missing.get("status") == 422 and missing.get("recovery_code") == DETAIL
            and missing.get("upstream_arrivals") == 0 and missing.get("uncached") is True,
            "missing_tool_evidence_missing")
    audit = report.get("audit", {})
    require(audit.get("records") == 6 and audit.get("missing_tool_blocks") == 1
            and audit.get("metadata_only") is True and audit.get("privacy_verified") is True
            and HEX64.fullmatch(audit.get("sha256", "")), "audit_privacy_evidence_missing")
    logs = report.get("log_privacy", {})
    require(all(logs.get(field) is True for field in
                ("shutdown_complete", "handles_closed", "scan_completed", "value_free"))
            and logs.get("leaking_files") == 0 and logs.get("checked_known_values", 0) > 0
            and logs.get("failure_logs_retained") is False
            and len(logs.get("files", [])) >= 2, "captured_log_evidence_missing")
    require(all(isinstance(row, dict) and re.fullmatch(r"[A-Za-z0-9_.-]+", row.get("name", ""))
                and type(row.get("bytes")) is int and row["bytes"] >= 0
                and HEX64.fullmatch(row.get("sha256", "")) for row in logs["files"]),
            "captured_log_metadata_invalid")
    cleanup = report.get("cleanup", {})
    required_cleanup = {"launcher_restored", "real_tools_unchanged", "private_directory_removed",
                        "private_environment_removed", "child_processes_stopped"}
    require(set(cleanup) == required_cleanup and all(value is True for value in cleanup.values()),
            "cleanup_evidence_missing")
    fixtures = report.get("fixture_sha256", [])
    require(len(fixtures) == 2 and len(set(fixtures)) == 2
            and all(HEX64.fullmatch(value) for value in fixtures)
            and HEX64.fullmatch(report.get("fixture_manifest_sha256", "")), "fixture_evidence_missing")
    require(set(report.get("real_tool_sha256", {})) == set(TOOL_NAMES)
            and all(HEX64.fullmatch(value) for value in report["real_tool_sha256"].values())
            and HEX64.fullmatch(report.get("seed_binary_sha256", "")), "extractor_binding_evidence_missing")


def send_raw_case(url, case, timeout=5, max_body_bytes=65536):
    request = Request(api_url(url, "/v1/responses"), data=case["body"],
                      headers=perf.request_headers(case, None), method="POST")
    try:
        response = OPENER.open(request, timeout=timeout)
    except HTTPError as error:
        response = error
    try:
        raw = response.read(max_body_bytes + 1)
        require(len(raw) <= max_body_bytes, "recovery_response_too_large")
        return response.status, raw
    finally:
        response.close()


def image_request(env, case, expected):
    row = perf.request(env.url, case, expected_hash=hashlib.sha256(expected).hexdigest(),
                       expected_layout_hash=perf.text_layout_hash(expected))
    row["semantic_verified"] = row.get("supported") is True and row.get("outcome") == "supported_success"
    row["boundary_verified"] = row["semantic_verified"] and row.get("completed") is True
    require(successful(row), "image_canonical_semantics_failed")
    require_receipt(env.upstream_url, case["operation_id"], hashlib.sha256(expected).hexdigest())
    return row


def operation_id(case):
    return dict(case, operation_id="ocr-recovery-" + uuid.uuid4().hex)


def run_probe(arguments, report):
    identity = bind_identity(arguments.binary, arguments.binary_sha256, arguments.binary_source,
                             arguments.producer_source, arguments.producer_commit)
    report["identity"] = identity
    report["seed_binary_sha256"] = digest(arguments.seed_binary)
    manifest = json.loads((Path(arguments.fixtures) / "manifest.json").read_text())
    validate_coverage(manifest)
    images = []
    seen = set()
    for fixture in manifest["images"]:
        if fixture.get("format", "png") == "png" and fixture.get("sha256") not in seen:
            images.append(fixture)
            seen.add(fixture.get("sha256"))
        if len(images) == 2:
            break
    require(len(images) == 2 and all(HEX64.fullmatch(row.get("sha256", "")) for row in images),
            "distinct_uncached_images_missing")
    report["fixture_sha256"] = [row["sha256"] for row in images]
    report["fixture_manifest_sha256"] = digest(Path(arguments.fixtures) / "manifest.json")
    tools = {}
    for name in TOOL_NAMES:
        selected = getattr(arguments, name) or shutil.which(name)
        require(selected is not None, "real_extractor_unavailable")
        tools[name] = Path(selected).resolve(strict=True)
    env = None
    with tempfile.TemporaryDirectory(prefix="ocr-recovery-private-") as temporary:
        isolated = PrivateExtractors(Path(temporary) / "tools", tools)
        try:
            with isolated:
                report["real_tool_sha256"] = dict(isolated.originals)
                workload = Workload()
                text_cases = [operation_id(workload.case(index + 10, 262144)) for index in range(3)]
                privacy_values = {value for case in text_cases for value in case["occurrences"]}
                for fixture in images:
                    expected = fixture.get("expected")
                    require(isinstance(expected, str) and expected, "fixture_privacy_oracle_missing")
                    privacy_values.add(expected)
                    privacy_values.update(line for line in expected.splitlines() if line)
                env = LiveEnvironment(arguments.binary, arguments.seed_binary, workload, 262144, isolated,
                                      privacy_values, str(arguments.report) + ".private-failure-logs")
                with env:
                    pid = wait_preflight(env)
                    first_hits = cache_hits(env)
                    baseline, baseline_expected = attachment_case(
                        workload, 0, 262144, images[0], arguments.fixtures, env.mappings,
                        "ocr-recovery-baseline", include_expected_body=True)
                    baseline = operation_id(baseline)
                    baseline_row = image_request(env, baseline, baseline_expected)
                    require(cache_hits(env) == first_hits, "baseline_image_not_uncached")
                    failed, failed_expected = attachment_case(
                        workload, 1, 262144, images[1], arguments.fixtures, env.mappings,
                        "ocr-recovery-missing", include_expected_body=True)
                    failed = operation_id(failed)
                    forbidden = list(set(value for case in [baseline, failed] + text_cases for value in case["occurrences"]))
                    env.privacy_values.update(forbidden)

                    def text_request(case):
                        row = perf.request(env.url, case, defer_boundary=True)
                        row = verify_deferred_boundary(case, row, env.mappings)
                        row["semantic_verified"] = (row.get("supported") is True
                                                    and row.get("outcome") == "supported_success")
                        require(successful(row), "text_semantics_failed")
                        require_receipt(env.upstream_url, case["operation_id"], row.get("boundary_sha256"))
                        return row

                    with isolated.without_ocr():
                        wait_preflight(env, missing=True, pid=pid)
                        before_missing = cache_hits(env)
                        with ThreadPoolExecutor(max_workers=3) as pool:
                            futures = [pool.submit(text_request, case) for case in text_cases]
                            harness.wait_until(lambda: env.metrics().get("active_requests") == 3,
                                               5, "three_text_requests_not_active")
                            failure_started = time.monotonic()
                            status, raw = send_raw_case(env.url, failed)
                            failure_finished = time.monotonic()
                            missing = check_missing_failure(status, raw)
                            rows = [future.result() for future in futures]
                        report["text_overlap_count"] = check_overlap(rows, failure_started, failure_finished)
                        require_receipt(env.upstream_url, failed["operation_id"], absent=True)
                        require(cache_hits(env) == before_missing, "missing_image_cache_hit")
                        report["missing_image"] = {"status": 422, "recovery_code": DETAIL,
                                                   "upstream_arrivals": 0, "uncached": True}
                    wait_preflight(env, pid=pid)
                    before_recovery = cache_hits(env)
                    restored = operation_id(failed)
                    restored_row = image_request(env, restored, failed_expected)
                    require(cache_hits(env) == before_recovery, "restored_image_cache_hit")
                    require_receipt(env.upstream_url, failed["operation_id"], absent=True)
                    successful_ids = [row.get("server_request_id") for row in [baseline_row] + rows + [restored_row]]
                    require(all(isinstance(ident, str) and REQUEST_ID.fullmatch(ident) for ident in successful_ids),
                            "success_audit_identity_missing")
                    report["audit"] = audit_summary(env.directory / "audit.jsonl", successful_ids,
                                                     missing["request_id"], forbidden)
                    report.update(image_successes=2, text_semantic_successes=3,
                                  canonical_receipts_verified=5, same_proxy_process=True,
                                  baseline_uncached=True, restored_uncached=True)
        finally:
            report["cleanup"] = dict(isolated.cleanup)
            if env is not None:
                report["log_privacy"] = dict(env.log_evidence)
                report["cleanup"].update(
                    private_environment_removed=not env.directory.exists(),
                    child_processes_stopped=all(child.poll() is not None for child in env.children))
    require(report.get("cleanup") and all(report["cleanup"].values()), "cleanup_evidence_missing")
    require(bind_identity(arguments.binary, arguments.binary_sha256, arguments.binary_source,
                          arguments.producer_source, arguments.producer_commit) == identity, "identity_changed")
    report["identity_stable"] = True


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "binary-sha256", "binary-source", "seed-binary", "producer-source",
                 "producer-commit", "fixtures", "reference-lock", "report"):
        parser.add_argument("--" + name, required=True)
    for name in TOOL_NAMES:
        parser.add_argument("--" + name)
    arguments = parser.parse_args(argv)
    report = {"schema_version": 1, "probe": "live_ocr_recovery", "passed": False}
    descriptor = None
    try:
        with reference_lock(arguments.reference_lock):
            descriptor = os.open(arguments.report, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            try:
                run_probe(arguments, report)
                validate_evidence(report)
                report["passed"] = True
            except BaseException as error:
                report["failure_code"] = str(error) if isinstance(error, ProbeFailure) else "ocr_recovery_probe_failed"
            finally:
                with os.fdopen(descriptor, "w") as output:
                    descriptor = None
                    json.dump(report, output, indent=2, sort_keys=True)
                    output.write("\n")
                    output.flush()
                    os.fsync(output.fileno())
    except (OSError, ProbeFailure):
        return 1
    finally:
        if descriptor is not None:
            os.close(descriptor)
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
