"""Explicit failure modes for the isolated fake upstream only."""


def send_fault_response(handler, mode, body_sha256):
    if mode is None:
        return False
    if mode not in ("upstream_error", "malformed_success"):
        raise ValueError("soak_fault_selector")
    if mode == "upstream_error":
        status, content_type = 503, "application/json"
        body = b'{"error":{"type":"benchmark_upstream_failure"}}'
    else:
        status, content_type = 200, "text/event-stream"
        body = b"data: {\n\n"
    handler.send_response(status)
    handler.send_header("Content-Type", content_type)
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("X-Benchmark-Body-SHA256", body_sha256)
    handler.end_headers()
    handler.wfile.write(body)
    handler.wfile.flush()
    return True
