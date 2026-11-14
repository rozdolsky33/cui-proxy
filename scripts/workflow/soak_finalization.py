"""Audit and log reconciliation after owned environment cleanup."""
import json
import math
import os
from pathlib import Path
import stat

from soak_policy_coverage import verify_policy_coverage
from soak_rejections import verify_log_privacy


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate audit field")
        result[key] = value
    return result


def _float(value):
    number = float(value)
    if not math.isfinite(number):
        raise ValueError("non-finite audit number")
    return number


def _constant(value):
    raise ValueError("non-finite audit constant")


def read_audit(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode):
            raise ValueError("audit artifact must be a regular file")
        records = []
        consumed = 0
        while consumed < before.st_size:
            line = stream.readline(min(1024 * 1024 + 1, before.st_size - consumed))
            if not line or len(line) > 1024 * 1024 or not line.endswith(b"\n"):
                raise ValueError("audit record is truncated or exceeds the bound")
            consumed += len(line)
            record = json.loads(line, object_pairs_hook=_object, parse_float=_float,
                                parse_constant=_constant)
            if not isinstance(record, dict):
                raise ValueError("audit record must be an object")
            records.append(record)
        if stream.read(1):
            raise ValueError("audit artifact grew during read")
        after = os.fstat(stream.fileno())
        fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
        if any(getattr(before, field) != getattr(after, field) for field in fields):
            raise ValueError("audit artifact changed during read")
        return records


def _rows(result):
    if not isinstance(result, dict) or set(result.get("phases", {})) != {"fixed", "churn"}:
        raise ValueError("both completed phases are required")
    sources = [(result.get("warmup"), "result")]
    sources.extend((result["phases"][phase], "request") for phase in ("fixed", "churn"))
    rows = []
    for report, key in sources:
        if not isinstance(report, dict) or report.get("completed") is not True:
            raise ValueError("incomplete report cannot be finalized")
        entries = report.get("results")
        if not isinstance(entries, list) or not entries:
            raise ValueError("missing report outcomes")
        for entry in entries:
            if not isinstance(entry, dict) or not isinstance(entry.get(key), dict):
                raise ValueError("malformed report outcome")
            rows.append(entry[key])
    return rows


def finalize_evidence(env, mode, result, *, forbidden_values):
    """Reconcile all proxy audit rows and prove retained logs omit protected values."""
    if mode not in ("proxy", "bypass"):
        raise ValueError("unknown campaign mode")
    forbidden_values = tuple(forbidden_values)
    if not forbidden_values or any(not isinstance(value, str) or not value for value in forbidden_values):
        raise ValueError("privacy checks require the protected value set")
    rows = _rows(result)
    directory = Path(env.directory)
    logs = sorted(directory.glob("*.log"))
    for name in ("proxy.log", "backend.log"):
        if directory / name not in logs:
            raise ValueError("required retained log is missing")
    audit_path = directory / "audit.jsonl"
    try:
        records = read_audit(audit_path)
    except FileNotFoundError:
        if mode == "proxy":
            raise
        records = []
    else:
        logs.append(audit_path)
    if mode == "proxy":
        controls = result.get("controls", {})
        count = controls.get("reloads_completed")
        if controls.get("failed") is not False or type(count) is not int or count < 0:
            raise ValueError("invalid policy control evidence")
        coverage = verify_policy_coverage(records, rows, list(range(count + 1)))
    else:
        if records:
            raise ValueError("bypass unexpectedly generated proxy audit records")
        coverage = {"audit_records": 0, "bypass_proxy_absence_verified": True}
    privacy = verify_log_privacy(logs, forbidden_values)
    if not isinstance(privacy, dict) or privacy.get("privacy_verified") is not True:
        raise ValueError("privacy evidence did not verify")
    return {"verified": True, "outcomes_reconciled": len(rows),
            "audit": coverage, "privacy": privacy, "performance_qualified": False}
