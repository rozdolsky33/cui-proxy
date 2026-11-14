"""Read a completed, sealed campaign without promoting it to qualification."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tempfile

from soak_evidence import _encode, _metadata, _pairs, read_sealed_evidence

PHASES = ("proxy-fixed", "proxy-churn", "bypass-fixed", "bypass-churn")
REQUIRED_REPORTS = (
    "proxy-warmup.json", "proxy-fixed-summary.json", "proxy-churn-summary.json",
    "proxy-controls.json", "proxy-stage-records.json", "proxy-mode-summary.json",
    "proxy-finalization.json", "bypass-warmup.json", "bypass-fixed-summary.json",
    "bypass-churn-summary.json", "bypass-mode-summary.json", "bypass-finalization.json",
    "campaign-summary.json",
)


def _reject_constant(value):
    raise ValueError("campaign_nonfinite_json")


def read_report_bytes(data):
    report = json.loads(data, object_pairs_hook=_pairs, parse_constant=_reject_constant)
    if not isinstance(report, dict):
        raise ValueError("campaign_report_shape")
    _metadata(report)
    return report


def _capture(path, *, expected_hash=None, expected_size=None):
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except OSError as error:
        raise ValueError("campaign_file_unavailable") from error
    try:
        before = os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode) or (expected_size is not None and before.st_size != expected_size):
            raise ValueError("campaign_file_type_or_size")
        remaining, chunks = before.st_size, []
        while remaining:
            block = os.read(descriptor, min(remaining, 1024 * 1024))
            if not block:
                raise ValueError("campaign_file_truncated")
            chunks.append(block)
            remaining -= len(block)
        if os.read(descriptor, 1):
            raise ValueError("campaign_file_grew")
        after = os.fstat(descriptor)
        if any(getattr(before, field) != getattr(after, field)
               for field in ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")):
            raise ValueError("campaign_file_changed")
        data = b"".join(chunks)
        if expected_hash is not None and hashlib.sha256(data).hexdigest() != expected_hash:
            raise ValueError("campaign_file_digest")
        return data
    finally:
        os.close(descriptor)


def _sealed(events, seal):
    if seal != _encode(read_report_bytes(seal)) + b"\n":
        raise ValueError("campaign_seal_encoding")
    # Run the existing framing/hash-chain verifier against immutable captured bytes.
    # The private temporary directory prevents another reader from replacing them.
    with tempfile.TemporaryDirectory(prefix="cui-campaign-read-") as directory:
        directory = Path(directory)
        (directory / "events.jsonl").write_bytes(events)
        (directory / "seal.json").write_bytes(seal)
        result = read_sealed_evidence(directory / "events.jsonl", directory / "seal.json")
    if result.get("run_completed") is not True:
        raise ValueError("campaign_incomplete_seal")
    return result


def _hash(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def read_completed_campaign(root):
    root = Path(root)
    if root.is_symlink() or not root.is_dir():
        raise ValueError("campaign_root_directory")
    events_bytes, seal_bytes = _capture(root / "events.jsonl"), _capture(root / "seal.json")
    sealed = _sealed(events_bytes, seal_bytes)
    events = sealed["events"]
    if (not events or events[0].get("type") != "campaign_start"
            or events[-1].get("type") != "campaign_finished"
            or sum(event.get("type") == "campaign_start" for event in events) != 1
            or sum(event.get("type") == "campaign_finished" for event in events) != 1
            or type(events[-1].get("phase_count")) is not int or events[-1]["phase_count"] != 4
            or events[-1].get("performance_qualified") is not False):
        raise ValueError("campaign_lifecycle")
    start = events[0]
    if (not isinstance(start.get("identity"), dict) or not isinstance(start.get("settings"), dict)
            or start.get("performance_qualified") is not False):
        raise ValueError("campaign_start_identity")
    reports, phases = {}, {}
    for event in events:
        kind = event.get("type")
        if kind == "campaign_artifact":
            name, digest, size = event.get("name"), event.get("sha256"), event.get("bytes")
            if (not isinstance(name, str) or not re.fullmatch(r"[a-z][a-z0-9-]*[.]json", name)
                    or name in reports or not _hash(digest) or type(size) is not int or size < 0):
                raise ValueError("campaign_artifact_registration")
            reports[name] = read_report_bytes(_capture(root / name, expected_hash=digest, expected_size=size))
        elif kind == "campaign_phase":
            name = event.get("phase")
            if (len(phases) >= len(PHASES) or name != PHASES[len(phases)]
                    or event.get("run_completed") is not True
                    or type(event.get("event_count")) is not int or event["event_count"] < 0
                    or any(not _hash(event.get(key)) for key in ("events_sha256", "seal_sha256"))):
                raise ValueError("campaign_phase_registration")
            directory = root / name
            if directory.is_symlink() or not directory.is_dir():
                raise ValueError("campaign_phase_directory")
            child = _sealed(_capture(directory / "events.jsonl", expected_hash=event["events_sha256"]),
                            _capture(directory / "seal.json", expected_hash=event["seal_sha256"]))
            if len(child["events"]) != event["event_count"]:
                raise ValueError("campaign_phase_event_count")
            phases[name] = child
    if tuple(phases) != PHASES or not set(REQUIRED_REPORTS) <= reports.keys():
        raise ValueError("campaign_required_evidence_missing")
    return dict(identity=start["identity"], settings=start["settings"], events=events,
                reports=reports, phases=phases, root_events_sha256=hashlib.sha256(events_bytes).hexdigest(),
                root_seal_sha256=hashlib.sha256(seal_bytes).hexdigest(), performance_qualified=False)
