#!/usr/bin/env python3
"""Local deterministic workflow checks. Never print request or fixture contents."""

import argparse
import base64
import concurrent.futures
import hashlib
import http.server
import json
import math
import os
import platform
import re
from pathlib import Path
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import zlib


VERSION = 1
SEED = 1102026
ROOT = Path(__file__).resolve().parents[2]
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
MANIFEST = json.loads((Path(__file__).with_name("manifest.json")).read_text())


class CheckFailure(Exception):
    """Only fixed assertion identifiers, never values, belong in this error."""


def require(condition, code):
    if not condition:
        raise CheckFailure(code)


def semantics(value):
    if isinstance(value, str) and value.lstrip().startswith(("{", "[")):
        try:
            return semantics(json.loads(value))
        except json.JSONDecodeError:
            return value
    if isinstance(value, list):
        return [semantics(item) for item in value]
    if isinstance(value, dict):
        return {key: semantics(item) for key, item in value.items()}
    return value


def check_operation(status, response, expected, captures, originals,
                    expected_status=200, bypass=False):
    require(status == expected_status, "unexpected_status")
    if expected_status == 422:
        require(not captures, "forbidden_forward")
        return
    check_boundary(captures, originals, bypass)
    require(json.dumps(semantics(response), sort_keys=True) == json.dumps(semantics(expected), sort_keys=True), "semantic_mismatch")


def check_boundary(captures, originals, bypass=False):
    require(len(captures) == 1, "upstream_evidence_count")
    if not bypass:
        decoded = []
        for raw in captures:
            try:
                decoded.append(json.dumps(semantics(json.loads(raw)), ensure_ascii=False))
            except (ValueError, UnicodeError):
                decoded.append(raw.decode("utf-8", errors="replace"))
        require(not any(value in text for text in decoded for value in originals),
                "original_leaked")
        require(not any("data:image/" in text or '"image_url"' in text for text in decoded),
                "image_forwarded")


def mutation_checks():
    base = dict(status=200, response={"a": "original-a", "b": "original-b"},
                expected={"a": "original-a", "b": "original-b"},
                captures=[b'{"a":"masked-a","b":"masked-b"}'],
                originals=["original-a", "original-b"])
    mutations = [dict(base, captures=[b"original-a"]),
                 dict(base, response={"a": "original-b", "b": "original-a"}),
                 dict(base, status=422, expected_status=422)]
    detected = 0
    for case in mutations:
        try:
            check_operation(**case)
        except CheckFailure:
            detected += 1
    require(detected == 3, "mutation_survived")
    return detected


def image_part():
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 2, 8, 2, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress((b"\x00" + b"\xff" * 6) * 2)) + chunk(b"IEND", b"")
    return {"type": "input_image", "image_url": "data:image/png;base64," + base64.b64encode(png).decode(), "detail": "high"}


def upstream_main(directory):
    """Separate process: receives only requests, never client oracle state."""
    folder = Path(directory)

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            raw = self.rfile.read(int(self.headers["Content-Length"]))
            body = json.loads(raw)
            meta = body["metadata"]
            ident = meta["harness_id"]
            # Capture names are bounded ASCII tokens, never client-chosen paths.
            if not isinstance(ident, str) or re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]{0,127}", ident) is None:
                self.send_error(400)
                return
            root = os.path.join(os.path.realpath(folder), "")
            capture_path = os.path.realpath(os.path.join(root, ident + ".json"))
            cancel_path = os.path.realpath(os.path.join(root, ident + ".cancelled"))
            if not capture_path.startswith(root):
                self.send_error(400)
                return
            if not cancel_path.startswith(root):
                self.send_error(400)
                return
            capture = {"body": body, "headers": dict(self.headers)}
            (Path(capture_path)).write_text(json.dumps(capture))
            fault = meta.get("fault", "")
            if fault in ("upstream", "non_sse"):
                self.send_response(503 if fault == "upstream" else 200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"result":"controlled"}')
                return
            if self.headers.get("X-Workflow-Delay-MS") == "80":
                time.sleep(0.08)
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            if meta.get("shape") == "arguments":
                payload = body["input"][0]["content"][0]["text"]
                event = "response.function_call_arguments.delta"
            else:
                payload = json.dumps(body["input"], ensure_ascii=False)
                event = "response.output_text.delta"
            # Small deterministic chunks exercise split substitutions and JSON escapes.
            try:
                if fault == "cancel":
                    for _ in range(300):
                        data = json.dumps({"type": event, "item_id": "item-output", "output_index": 0, "delta": "x" * 65536})
                        self.wfile.write(("event: " + event + "\ndata: " + data + "\n\n").encode())
                        self.wfile.flush()
                        time.sleep(.02)
                else:
                    for i in range(0, len(payload), 7):
                        data = json.dumps({"type": event, "item_id": "item-output", "output_index": 0, "delta": payload[i:i + 7]})
                        self.wfile.write(("event: " + event + "\ndata: " + data + "\n\n").encode())
                        self.wfile.flush()
                    self.wfile.write(b'event: response.completed\ndata: {"type":"response.completed"}\n\n')
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                (Path(cancel_path)).touch()

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    (folder / "ready.json").write_text(json.dumps({"port": server.server_port}))
    server.serve_forever()


def wait_until(predicate, timeout, code):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if predicate():
            return
        time.sleep(.05)
    raise CheckFailure(code)


def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class Environment:
    def __init__(self, binary, profile="sanitize", attachments="extract_text"):
        self.binary, self.profile, self.attachments = binary, profile, attachments
        self.children, self.logs = [], []

    def spawn(self, args, name, env=None):
        log = open(self.directory / name, "wb")
        self.logs.append(log)
        child = subprocess.Popen(args, stdout=log, stderr=log, env=env, start_new_session=True)
        self.children.append(child)
        return child

    def __enter__(self):
        self.temp = tempfile.TemporaryDirectory(prefix="cui-workflow-")
        self.directory = Path(self.temp.name)
        self.captures = self.directory / "captures"
        self.captures.mkdir()
        try:
            self.spawn([sys.executable, str(Path(__file__).resolve()), "--upstream", str(self.captures)], "upstream.log")
            ready = self.captures / "ready.json"
            wait_until(ready.exists, 10, "upstream_startup")
            upstream = json.loads(ready.read_text())["port"]
            self.upstream_url = f"http://127.0.0.1:{upstream}"
            proxy, ui = port(), port()
            self.url, self.ui = f"http://127.0.0.1:{proxy}", f"http://127.0.0.1:{ui}"
            tools = self.directory / "tools"
            tools.mkdir()
            ocr = tools / "tesseract"
            ocr.write_text(f"#!{sys.executable}\nimport sys\nsys.stdin.buffer.read()\nsys.stdout.write('privatevalue999999')\n")
            ocr.chmod(0o700)
            rules = [{"name": "workflow-value", "type": "regex", "pattern": "privatevalue[0-9]{6}", "synth": "secret"},
                     {"name": "workflow-block", "type": "regex", "pattern": "forbiddenvalue[0-9]{6}", "synth": "secret", "action": "block"}]
            cfg = {"listen": f"127.0.0.1:{proxy}", "ui": {"listen": f"127.0.0.1:{ui}"},
                   "upstreams": {"codex": self.upstream_url},
                   "policy": {"profile": self.profile, "attachments": self.attachments, "on_error": "block"},
                   "vault": {"mode": "file", "file": str(self.directory / "vault.enc"), "key_file": str(self.directory / "vault.key")},
                   "audit": {"enabled": True, "file": str(self.directory / "decisions.jsonl"), "on_error": "block"}, "rules": rules}
            path = self.directory / "config.json"
            path.write_text(json.dumps(cfg))
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ.get("PATH", ""))
            child = self.spawn([str(self.binary), "serve", "-config", str(path)], "proxy.log", env)

            def healthy():
                require(child.poll() is None, "proxy_exited_during_startup")
                try:
                    with OPENER.open(self.ui + "/healthz", timeout=.2) as res:
                        return res.status == 200
                except (OSError, urllib.error.URLError):
                    return False
            wait_until(healthy, 10, "proxy_startup")
            return self
        except BaseException:
            self.__exit__(*sys.exc_info())
            raise

    def __exit__(self, *_):
        for child in reversed(self.children):
            # Descendants may outlive an exited session leader. The group is
            # ours even when Popen.poll() already reports the parent's exit.
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            end = time.monotonic() + 12
            while time.monotonic() < end:
                child.poll()
                try:
                    os.killpg(child.pid, 0)
                except ProcessLookupError:
                    break
                except PermissionError:
                    # A denied zero-signal probe is not proof the group is gone.
                    # Keep polling until the grace deadline, then force cleanup.
                    pass
                time.sleep(.05)
            else:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            child.wait(timeout=5)
        for log in self.logs:
            log.close()
        self.temp.cleanup()

    def send(self, body, bypass=False, headers=None):
        payload = b"{" if body["metadata"].get("fault") == "bad_json" else json.dumps(body).encode()
        req = urllib.request.Request((self.upstream_url if bypass else self.url) + "/v1/responses",
                                     payload, headers={"Content-Type": "application/json", **(headers or {})})
        start = time.monotonic()
        try:
            response = OPENER.open(req, timeout=15)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            if body["metadata"].get("fault") == "cancel":
                response.readline()
                data = b""
            else:
                chunks, size = [], 0
                while True:
                    require(time.monotonic() - start < 45, "request_deadline")
                    chunk = response.read1(65536)
                    if not chunk:
                        break
                    size += len(chunk)
                    require(size <= 1 << 20, "correctness_response_limit")
                    chunks.append(chunk)
                data = b"".join(chunks)
            return response.status, data, (time.monotonic() - start) * 1000

    def capture(self, ident):
        path = self.captures / (ident + ".json")
        return [path.read_bytes()] if path.exists() else []

    def vault_count(self):
        with OPENER.open(self.ui + "/api/metrics", timeout=5) as response:
            return json.load(response)["vault_size"]


def decode_stream(data):
    deltas, terminal = [], False
    for line in data.decode().splitlines():
        if not line.startswith("data: "):
            continue
        item = json.loads(line[6:])
        if item.get("type") == "response.completed":
            terminal = True
        elif item.get("type", "").endswith(".delta"):
            deltas.append(item["delta"])
    require(terminal, "missing_terminal_event")
    return json.loads("".join(deltas))


def message(value):
    return {"role": "user", "content": [{"type": "input_text", "text": value}]}


def scenario(case, client):
    a, b = f"privatevalue{client * 10 + 1:06d}", f"privatevalue{client * 10 + 2:06d}"
    inputs = [message(json.dumps({"a": a, "b": b, "literal": 'quote " and backslash \\ and unicode λ', "number": 42}))]
    headers, shape, fault, status = {}, "echo", "", 200
    originals = [a, b]
    expected = inputs
    if case == "W02":
        shape = "arguments"
        expected = {"path": "result.txt", "value": a + "\n" + b}
        inputs = [message(json.dumps(expected))]
    elif case == "W03":
        inputs = [{"type": "function_call_output", "call_id": "call-test", "output": json.dumps({"a": a, "b": b, "status": "passed"})}, message(b)]
        expected = inputs
    elif case == "W04":
        shape = "arguments"
        expected = {"argv": ["connect", "--host", a, "--resource", b]}
        inputs = [message(json.dumps(expected))]
    elif case in ("W05", "W06", "W06-limit"):
        originals = ["privatevalue999999"]
        replacement = {"type": "input_text", "text": originals[0]}
        tool_type = "custom_tool_call_output" if case == "W05" else "function_call_output"
        inputs = [{"type": tool_type, "call_id": "call-images", "output": [{"type": "input_text", "text": "before"}, image_part(), image_part(), {"type": "input_text", "text": "after"}]}]
        expected = [{"type": tool_type, "call_id": "call-images", "output": [{"type": "input_text", "text": "before"}, replacement, replacement, {"type": "input_text", "text": "after"}]}]
        if case in ("W06", "W06-limit"):
            inputs.insert(0, {"role": "user", "content": [image_part()]})
            expected.insert(0, {"role": "user", "content": [replacement]})
        if case == "W06-limit":
            inputs[1]["output"] = [image_part() for _ in range(8)]
            expected, status = None, 422
    elif case == "W08":
        headers["X-Codex-Turn-Metadata"] = json.dumps({"cwd": "/tmp/" + a, "workspaces": {"/tmp/" + b: {"git_remote": a}}})
    elif case == "W11-policy":
        inputs, originals, expected, status = [message("forbiddenvalue000001")], ["forbiddenvalue000001"], None, 422
    elif case == "W11-shape":
        inputs, originals, expected, status = [{"role": "assistant", "content": [image_part()]}], [], None, 422
    elif case == "W11-json":
        fault, expected, status = "bad_json", None, 422
    elif case == "W11-strict":
        expected, status = None, 422
    elif case == "W11-disabled":
        inputs, originals, expected, status = [{"role": "user", "content": [image_part()]}], [], None, 422
    elif case == "W12-upstream":
        fault, status, expected = "upstream", 503, {"result": "controlled"}
    elif case == "W12-non-sse":
        fault, status, expected = "non_sse", 502, None
    elif case == "W12-cancel":
        fault, expected = "cancel", None
    return inputs, expected, originals, headers, shape, fault, status


CASES = [case for case, definition in MANIFEST["cases"].items() if definition.get("environment", "normal") == "normal"]


def operation(env, case, client, serial, bypass=False, context_input=None, outputs=None, mutation=None):
    inputs, expected, originals, headers, shape, fault, status = scenario(case, client)
    require(status == MANIFEST["cases"][case]["status"], "scenario_manifest_disagreement")
    if context_input is not None:
        inputs = context_input + inputs
        expected = context_input + expected
        originals = list(set(originals + re.findall(r"privatevalue[0-9]{6}", json.dumps(context_input))))
    ident = f"c{client}-{case}-{serial}"
    body = {"model": "workflow-test", "stream": True, "input": inputs,
            "metadata": {"harness_id": ident, "shape": shape, "fault": fault}}
    result = {"scenario": case, "client": client, "bypass": bypass, "outcome": "unresolved"}
    try:
        actual, raw, elapsed = env.send(body, bypass or mutation in ("no_sanitization", "forward_negative"), headers)
        captures = env.capture(ident)
        result.update(status=actual, duration_ms=round(elapsed, 3), request_bytes=len(json.dumps(body).encode()))
        if fault == "cancel":
            check_boundary(captures, originals, bypass)
            require(actual == 200 and len(captures) == 1, "cancel_setup")
            wait_until(lambda: (env.captures / (ident + ".cancelled")).exists(), 5, "cancel_not_propagated")
            result["outcome"] = "client_cancelled"
        elif fault == "non_sse":
            check_boundary(captures, originals, bypass)
            require(actual == 502 and len(captures) == 1, "successful_non_sse_not_refused")
            result["outcome"] = "expected_representation_block"
        else:
            parsed = decode_stream(raw) if status == 200 else (json.loads(raw) if status == 503 else None)
            if mutation == "swap_restoration":
                text = json.dumps(parsed)
                text = text.replace(originals[0], "swap-placeholder").replace(originals[1], originals[0]).replace("swap-placeholder", originals[1])
                parsed = json.loads(text)
            check_operation(actual, parsed, expected, captures, originals, status, bypass)
            if case == "W08":
                captured = json.loads(captures[0])
                metadata = next((v for k, v in captured["headers"].items() if k.lower() == "x-codex-turn-metadata"), None)
                require(metadata is not None, "metadata_dropped")
                metadata = json.loads(metadata)
                require(isinstance(metadata.get("cwd"), str) and len(metadata.get("workspaces", {})) == 1, "metadata_shape_changed")
            if status == 422:
                error = json.loads(raw)
                text = json.dumps(error)
                require(not any(value in text for value in originals), "error_value_leaked")
                require(MANIFEST["cases"][case]["diagnostic"] in text, "wrong_refusal_diagnostic")
                result["outcome"] = "expected_policy_block" if case in ("W11-policy", "W11-strict") else "expected_representation_block"
            elif status == 503:
                result["outcome"] = "upstream_failure"
            else:
                if case == "W02":
                    with tempfile.TemporaryDirectory(prefix="cui-effect-") as td:
                        target = Path(td) / "result.txt"
                        require(parsed["path"] == target.name, "wrong_tool_path")
                        target.write_text(parsed["value"])
                        require(target.read_text() == expected["value"], "wrong_file_effect")
                result["outcome"] = "supported_success"
                if outputs is not None:
                    outputs.append(parsed)
        result["passed"] = True
    except CheckFailure as error:
        result.update(passed=False, assertion=str(error))
        if str(error) in ("original_leaked", "image_forwarded", "semantic_mismatch", "error_value_leaked"):
            result["outcome"] = "transformation_error"
        elif str(error) == "unexpected_status" and result.get("status") == 422:
            result["outcome"] = "unexpected_proxy_refusal"
    except Exception as error:
        result.update(passed=False, outcome="harness_invalid", error_type=type(error).__name__)
    return result


def run(binary, clients, bypass=False):
    results = []
    with Environment(binary) as env:
        vault_before = env.vault_count()
        # Each client owns a sequential workflow stream. Workers synchronize at start.
        barrier = threading.Barrier(clients)

        def worker(client):
            barrier.wait(timeout=10)
            records = []
            cases = [c for c in CASES if c != "W07" and (not bypass or (MANIFEST["cases"][c]["status"] == 200 and c not in ("W05", "W06") and not c.startswith(("W11", "W12"))))]
            for serial, case in enumerate(cases):
                records.append(operation(env, case, client, serial, bypass))
            return records
        with concurrent.futures.ThreadPoolExecutor(max_workers=clients) as pool:
            for records in pool.map(worker, range(1, clients + 1)):
                results.extend(records)
        # Real parent -> child -> parent data flow. Up to three children overlap,
        # within the four-client bound; the one-client control is sequential.
        parent_output = []
        parent = operation(env, "W07", 1, 100, bypass, outputs=parent_output)
        results.append(parent)
        if parent["passed"]:
            def child(index):
                output = []
                record = operation(env, "W07", index, 101, bypass, context_input=parent_output[0], outputs=output)
                return record, output
            with concurrent.futures.ThreadPoolExecutor(max_workers=min(clients, 3)) as pool:
                children = list(pool.map(child, (2, 3, 4)))
            results.extend(record for record, _ in children)
            if all(record["passed"] for record, _ in children):
                collected = [item for _, output in children for item in output[0]]
                results.append(operation(env, "W07", 1, 102, bypass, context_input=collected))
        # Metadata logs may contain counts and IDs, never corpus originals.
        audit = (env.directory / "decisions.jsonl").read_bytes() if (env.directory / "decisions.jsonl").exists() else b""
        logs = (env.directory / "proxy.log").read_bytes()
        require(b"privatevalue" not in audit + logs and b"forbiddenvalue" not in audit + logs, "metadata_value_leaked")
        vault_after = env.vault_count()
        for record in results:
            record["vault_before_run"], record["vault_after_run"] = vault_before, vault_after
    return results


def boundary_mutations(binary):
    """Run actual request boundaries with injected faults; all must be rejected."""
    with Environment(binary) as env:
        cases = [("no_sanitization", "W01"), ("swap_restoration", "W01"), ("forward_negative", "W11-policy")]
        records = [operation(env, case, 1, i, mutation=kind) for i, (kind, case) in enumerate(cases)]
        require(all(not r["passed"] and "assertion" in r for r in records), "boundary_mutation_survived")
        return len(records)


def policy_probes(binary, clients):
    records = []
    for case, profile, attachments in [("W11-strict", "strict", "extract_text"),
                                      ("W11-disabled", "sanitize", "block")]:
        with Environment(binary, profile, attachments) as env:
            start = len(records)
            before = env.vault_count()
            with concurrent.futures.ThreadPoolExecutor(max_workers=clients) as pool:
                records.extend(pool.map(lambda client: operation(env, case, client, 200), range(1, clients + 1)))
            after = env.vault_count()
            for record in records[start:]:
                record["vault_before_run"], record["vault_after_run"] = before, after
    return records


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, default=ROOT / "bin/cui-proxy")
    parser.add_argument("--report", type=Path)
    parser.add_argument("--upstream", type=Path, help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.upstream:
        upstream_main(args.upstream)
        return 0
    report = {"contract_version": VERSION, "generator_version": VERSION, "seed": SEED,
              "scope": "deterministic-correctness", "performance_qualified": False,
              "stage_timings": None, "results": [],
              "resource_samples": None,
              "unavailable_reason": 'CPU, allocation and heap profiling belong to Task 04; stage timing is measured by the controlled probe.',
              "environment": {"os": platform.system(), "release": platform.release(), "architecture": platform.machine(), "python": platform.python_version()},
              "policy": {"revision": "workflow-correctness-v1", "regex_declarations": 2, "vault_mode": "file", "audit_on_error": "block"},
              "unmeasured": ["normal-tier performance", "real OCR quality", "W09 compaction", "W10 restart", "resource budgets"]}
    try:
        require(MANIFEST["version"] == VERSION and MANIFEST["seed"] == SEED, "manifest_version_mismatch")
        require(args.binary.is_file(), "binary_missing")
        report["binary_sha256"] = hashlib.sha256(args.binary.read_bytes()).hexdigest()
        p = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, text=True, check=True)
        report["commit"] = p.stdout.strip()
        p = subprocess.run(["git", "status", "--porcelain"], cwd=ROOT, capture_output=True, text=True, check=True)
        report["dirty"] = bool(p.stdout)
        report["mutation_checks_passed"] = mutation_checks()
        report["boundary_mutations_detected"] = boundary_mutations(args.binary.resolve())
        for clients in (1, 4):
            for bypass in (False, True):
                records = run(args.binary.resolve(), clients, bypass)
                for record in records:
                    record["concurrency"] = clients
                report["results"].extend(records)
            records = policy_probes(args.binary.resolve(), clients)
            for record in records:
                record["concurrency"] = clients
            report["results"].extend(records)
        import observation
        report["stage_timings"] = observation.run(args.binary)
        report["passed"] = bool(report["results"]) and all(r["passed"] for r in report["results"])
    except CheckFailure as error:
        report.update(passed=False, assertion=str(error))
    except Exception as error:
        report.update(passed=False, error_type=type(error).__name__)
    if args.report:
        report["evidence"] = {"report": str(args.report.resolve()), "raw_captures_retained": False}
        samples = sorted(r["duration_ms"] for r in report["results"] if "duration_ms" in r)
        report["diagnostic_duration_ms"] = {"sample_count": len(samples), "qualification": "correctness-run only; mixed scenarios, not a performance benchmark"}
        if samples:
            report["diagnostic_duration_ms"].update({f"p{q}": samples[math.ceil(len(samples) * q / 100) - 1] for q in (50, 95, 99)})
        args.report.parent.mkdir(parents=True, exist_ok=True)
        with open(args.report, "w", opener=lambda p, flags: os.open(p, flags, 0o600)) as out:
            json.dump(report, out, indent=2)
    print(json.dumps({"passed": report["passed"], "operations": len(report["results"]),
                      "failed": sum(not r["passed"] for r in report["results"]),
                      "mutation_checks_passed": report.get("mutation_checks_passed", 0)}))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
