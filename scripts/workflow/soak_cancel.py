"""Cancel a live stream and independently observe upstream handler release."""

import http.client
import json
import socket
import time
import urllib.parse

import harness
import performance
from soak_receipts import receipt_id, verify_receipt


def _address(url):
    address = urllib.parse.urlsplit(url)
    if address.scheme != "http" or address.hostname not in ("127.0.0.1", "localhost") or not address.port or address.username or address.password:
        raise ValueError("soak_cancel_url")
    return address


def backend_evidence(url, kind, operation, timeout):
    address = _address(url)
    if kind not in ("receipt", "activity"):
        raise ValueError("soak_cancel_evidence")
    endpoint = f"http://{address.netloc}/benchmark/{kind}/{receipt_id(operation)}"
    with harness.OPENER.open(endpoint, timeout=timeout) as response:
        data = response.read(4097)
    if len(data) > 4096:
        raise ValueError("soak_cancel_evidence_size")
    row = json.loads(data)
    if not isinstance(row, dict):
        raise ValueError("soak_cancel_evidence")
    return row


def _activity(row):
    fields = ("arrived", "active", "finished", "disconnected")
    if row.get("complete") is not True or any(type(row.get(field)) is not int for field in fields):
        raise ValueError("soak_cancel_activity")
    if row["arrived"] != 1 or row["active"] + row["finished"] != 1 or any(row[field] < 0 for field in fields):
        raise ValueError("soak_cancel_activity")
    if row["disconnected"] > row["finished"]:
        raise ValueError("soak_cancel_activity")
    return row


def cancel_request(url, case, expected_hash, upstream_url, *, timeout=45):
    address = _address(url)
    started = time.monotonic()
    deadline = started + timeout
    connection = http.client.HTTPConnection(address.hostname, address.port, timeout=timeout)
    response = None
    def remaining():
        value = deadline - time.monotonic()
        if value <= 0:
            raise TimeoutError("soak_cancel_deadline")
        return value
    try:
        selected = dict(case, benchmark_fault="cancel")
        connection.request("POST", address.path or "/v1/responses", body=case["body"],
                           headers=performance.request_headers(selected))
        transport = connection.sock
        response = connection.getresponse()
        if response.status != 200 or not response.getheader("Content-Type", "").startswith("text/event-stream"):
            raise ValueError("soak_cancel_response")
        frame = bytearray()
        first_byte = None
        while not bytes(frame).replace(b"\r\n", b"\n").endswith(b"\n\n"):
            transport.settimeout(remaining())
            part = response.read(1)
            if not part or len(frame) >= 4096:
                raise ValueError("soak_cancel_stream")
            if first_byte is None:
                first_byte = time.monotonic()
            frame.extend(part)
        data = b"\n".join(line[5:].strip() for line in bytes(frame).splitlines() if line.startswith(b"data:"))
        if json.loads(data) != {"type": "response.output_text.delta", "delta": "cancel-probe"}:
            raise ValueError("soak_cancel_stream")
        receipt = backend_evidence(upstream_url, "receipt", case["operation_id"], min(remaining(), 1))
        verify_receipt(receipt, expected_hash=expected_hash)
        active = _activity(backend_evidence(upstream_url, "activity", case["operation_id"], min(remaining(), 1)))
        if active["active"] != 1:
            raise ValueError("soak_cancel_not_active")
        cancelled = time.monotonic()
        try:
            transport.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        response.close()
        connection.close()
        while True:
            state = _activity(backend_evidence(upstream_url, "activity", case["operation_id"], min(remaining(), 1)))
            if state["active"] == 0:
                if state["finished"] != 1 or state["disconnected"] != 1:
                    raise ValueError("soak_cancel_disconnect_unproven")
                break
            time.sleep(min(.02, remaining()))
        finished = time.monotonic()
        row = {"outcome": "cancelled", "completed": False, "status": 200,
                "expected_failure_verified": True, "boundary_verified": True,
                "upstream_released": True,
                "upstream_release_observed_ms": (finished - cancelled) * 1000,
                "started_s": started, "finished_s": finished,
                "duration_ms": (finished - started) * 1000,
                "first_byte_ms": (first_byte - started) * 1000}
        row["operation_id"] = case["operation_id"]
        row["upstream_receipt_verified"] = True
        server_id = response.getheader("X-Cui-Proxy-Request-Id")
        if isinstance(server_id, str) and len(server_id) == 16 and all(c in "0123456789abcdef" for c in server_id):
            row["server_request_id"] = server_id
        return row
    finally:
        if response is not None:
            response.close()
        connection.close()
