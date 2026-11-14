"""Explicit negative-policy cases and value-free rejection oracles."""

import copy
import http.client
import time
from pathlib import Path

from performance import request_headers
from soak_cancel import _address, backend_evidence
from soak_receipts import verify_receipt
import json
import re

from performance_workload import encode


BLOCK_CANARY = "soak-refusal-canary-7d8e6f240b1a"


def negative_policy(baseline):
    """Explicit negative-policy variant; canonical fairness policy is unchanged."""
    policy = copy.deepcopy(baseline)
    policy["rules"].append({"name": "soak-refusal", "type": "regex", "pattern": re.escape(BLOCK_CANARY), "action": "block", "synth": next(rule["synth"] for rule in policy["rules"] if rule.get("synth"))})
    return policy


def build_rejection_case(workload, serial, size, variant):
    if variant not in ("blocked", "unsupported", "malformed_json"):
        raise ValueError("soak_rejection_variant")
    case = workload.case(serial, size)
    body = json.loads(case["body"])
    forbidden = list(case["occurrences"])
    if variant == "blocked":
        value = BLOCK_CANARY
        body["input"].append({"role": "user", "content": [{"type": "input_text", "text": value}]})
        forbidden.append(value)
    elif variant == "unsupported":
        body["input"].append({"role": "user", "content": [{"type": "input_audio", "input_audio": {"data": "AA==", "format": "wav"}}]})
    encoded = encode(body)
    if variant == "malformed_json":
        encoded = encoded[:-1]
    return dict(case, body=encoded, operation_id=f"reject-{variant}-{serial}", forbidden_values=forbidden)


def verify_diagnostic(status, body, variant, forbidden):
    expected = {"blocked": ("policy", "rule_match", "policy"),
                "unsupported": ("sanitize", "bad_body", "attachment_audio_unsupported"),
                "malformed_json": ("sanitize", "bad_body", "sanitize_bad_body")}
    if variant not in expected or status != 422 or len(body) > 65536:
        raise ValueError("soak_rejection_diagnostic")
    if any(value.encode() in body for value in forbidden):
        raise ValueError("soak_rejection_payload_leak")
    try:
        error = json.loads(body)["error"]
        reason, kind, recovery = expected[variant]
        message = error["message"]
        match = re.search(r"(?:^|[\s(])kind=([a-z_]+)", message)
        if (error["type"] != "cui_proxy_blocked" or error["code"] != reason
                or error["recovery"]["code"] != recovery or match is None or match.group(1) != kind
                or re.fullmatch("[0-9a-f]{16}", error["request_id"]) is None
                or any(value in message for value in forbidden)):
            raise ValueError("soak_rejection_diagnostic")
    except (KeyError, TypeError, json.JSONDecodeError):
        raise ValueError("soak_rejection_diagnostic") from None
    return {"reason": reason, "kind": kind, "recovery_code": recovery,
            "request_id": error["request_id"]}


def execute_rejection(url, case, variant, upstream_url, *, timeout=45):
    """Check one controlled rejection and its independently observed boundary."""
    if timeout <= 0 or timeout > 45:
        raise ValueError("invalid rejection timeout")
    address = _address(url)
    started = time.monotonic()
    connection = http.client.HTTPConnection(address.hostname, address.port, timeout=timeout)
    try:
        connection.request("POST", address.path or "/v1/responses", body=case["body"], headers=request_headers(case))
        response = connection.getresponse()
        body = response.read(65537)
        diagnostic = verify_diagnostic(response.status, body, variant, case["forbidden_values"])
        remaining = timeout - (time.monotonic() - started)
        if remaining <= 0:
            raise ValueError("rejection deadline exceeded")
        receipt = backend_evidence(upstream_url, "receipt", case["operation_id"], remaining)
        verify_receipt(receipt, absent=True)
        return {"operation_id": case["operation_id"], "outcome": "expected_rejection", "completed": False,
                "expected_failure_verified": True, "boundary_verified": True,
                "upstream_arrivals": 0, "diagnostic": diagnostic,
                "duration_ms": (time.monotonic() - started) * 1000}
    finally:
        connection.close()


def verify_log_privacy(paths, forbidden_values):
    """Scan owned files locally, including chunk boundaries; retain only counts."""
    needles = {value.encode() for value in forbidden_values if value}
    overlap = max((len(value) for value in needles), default=1) - 1
    checked = 0
    for path in paths:
        with Path(path).open("rb") as stream:
            tail = b""
            while True:
                chunk = stream.read(65536)
                if not chunk:
                    break
                window = tail + chunk
                if any(value in window for value in needles):
                    raise ValueError("protected value in owned log")
                tail = window[-overlap:] if overlap else b""
        checked += 1
    if not checked or not needles:
        raise ValueError("missing log privacy evidence")
    return {"files_checked": checked, "protected_values_checked": len(needles), "privacy_verified": True}
