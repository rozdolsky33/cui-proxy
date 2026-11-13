#!/usr/bin/env python3
"""Isolated matched-load measurements; stdout is counts/status only.

All HTTP payloads and fixture subprocess output stay in private local files.
This tool never points at the user's running proxy.
"""
import sys

_BACKEND_CHILD = "--upstream" in sys.argv

def backend_stage(stage):
    if _BACKEND_CHILD: print("backend_stage=" + stage, flush=True)

backend_stage("entry")

import argparse
import concurrent.futures
import contextlib
import hashlib
import http.client
import http.server
import io
import json
import math
import os
import platform
import re
from pathlib import Path
import socket
import socketserver
import subprocess
import tempfile
import threading
import time
from urllib.parse import urlsplit

import harness
from performance_stats import OUTCOMES, summarize_outcomes, summarize_pairs
from performance_workload import Workload, PROSE, encode

backend_stage("imports")


def response_text(values):
    return "\n".join(values) + "\n" + (PROSE * 40)[:7000]


def stream_chunks(values):
    """Keep logical boundaries stable when corpus substitutions change length."""
    import re
    text = response_text(values)
    cuts = []
    offset = 0
    for value in values[:2]:
        start = text.find(value, offset)
        if start < 0 or not value:
            raise ValueError("invalid_stream_value")
        middle = start + max(1, len(value) // 2)
        end = start + len(value)
        cuts.append(middle)
        if end != middle:
            cuts.append(end)
        offset = end
    remaining = 32 - len(cuts)
    boundaries = [offset + match.end() for match in re.finditer(r"\s+", text[offset:])]
    if len(boundaries) < remaining:
        raise ValueError("insufficient_stream_words")
    # Fixture originals and their substitutes have identical whitespace shape.
    # Word boundaries therefore preserve association after complete restoration.
    for index in range(1, remaining):
        cuts.append(boundaries[math.ceil(index * len(boundaries) / remaining) - 1])
    cuts.append(len(text))
    start = 0
    result = []
    for end in cuts:
        result.append(text[start:end])
        start = end
    return result


def relay_timings(started_s, prefix_ms, send_started_s, send_finished_s):
    result = dict(upstream_send_ms=None, upstream_flush_ms=None, relay_ms=None, relay_flush_ms=None)
    arrays = (prefix_ms, send_started_s, send_finished_s)
    if any(not isinstance(values, list) or len(values) != len(prefix_ms) or not values for values in arrays):
        return result
    if any(type(value) not in (int,float) or not math.isfinite(value) for values in arrays for value in values):
        return result
    if any(end < start for start,end in zip(send_started_s,send_finished_s)) or any(start < previous for previous,start in zip(send_finished_s,send_started_s[1:])):
        return result
    sent = [(value-started_s)*1000 for value in send_started_s]
    flushed = [(value-started_s)*1000 for value in send_finished_s]
    return dict(upstream_send_ms=sent,upstream_flush_ms=flushed,
                relay_ms=[prefix-send for prefix,send in zip(prefix_ms,sent)],
                relay_flush_ms=[prefix-flush for prefix,flush in zip(prefix_ms,flushed)])



def values_from_body(body, *, allow_attachments=False):
    values = []
    for item in body["input"]:
        if allow_attachments and item.get("type") != "function_call_output":
            content = item.get("content", [])
            if content and content[0].get("type") in ("input_image", "input_file"):
                continue
        text = item.get("output")
        if text is None: text = item["content"][0]["text"]
        values.append(text.split("\n", 1)[0])
    return values


class _LoopbackHTTPServer(http.server.ThreadingHTTPServer):
    def server_bind(self):
        # HTTPServer.server_bind does a reverse-DNS lookup for server_name.
        # This backend is deliberately local and must work with DNS unavailable.
        socketserver.TCPServer.server_bind(self)
        self.server_name, self.server_port = self.server_address[:2]


def text_layout_hash(body):
    """Canonical fixture boundary check: normalize declared text whitespace only."""
    value = json.loads(body)
    items = value.get("input", [])
    if isinstance(items, str):
        value["input"] = " ".join(items.split())
    elif isinstance(items, list):
        for item in items:
            if not isinstance(item, dict):
                continue
            content = item.get("content", [])
            if item.get("type") == "function_call_output":
                content = item.get("output", [])
                if isinstance(content, str):
                    item["output"] = " ".join(content.split())
            if isinstance(content, list):
                for part in content:
                    if isinstance(part, dict) and part.get("type") == "input_text" and isinstance(part.get("text"), str):
                        part["text"] = " ".join(part["text"].split())
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()


class FakeBackend:
    """The real runner runs this in a separate process without the oracle."""
    def __init__(self, delay=1.0, interval=0.02):
        self.delay, self.interval = delay, interval

    def __enter__(self):
        outer = self
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_POST(self):
                try:
                    length = int(self.headers.get("Content-Length", "0"))
                    if not 0 < length <= 12 * 1024 * 1024:
                        self.send_error(400); return
                    raw = self.rfile.read(length)
                    values = values_from_body(json.loads(raw), allow_attachments=self.headers.get("X-Benchmark-Text-Only-Reply") == "1")
                    if self.headers.get("X-Benchmark-Text-Only-Reply") == "1":
                        values = values[:40]
                    if len(values) < 2: values *= 2
                    chunks = stream_chunks(values)
                    time.sleep(outer.delay)
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("X-Benchmark-Body-SHA256", hashlib.sha256(raw).hexdigest())
                    if self.headers.get("X-Benchmark-Text-Layout") == "1":
                        self.send_header("X-Benchmark-Text-Layout-SHA256", text_layout_hash(raw))
                    self.end_headers()
                    send_started = []
                    send_finished = []
                    for chunk in chunks:
                        event = dict(type="response.output_text.delta", delta=chunk)
                        send_started.append(time.monotonic())
                        self.wfile.write(b"data: " + encode(event) + b"\n\n")
                        self.wfile.flush()
                        send_finished.append(time.monotonic())
                        time.sleep(outer.interval)
                    self.wfile.write(b"data: " + encode(dict(type="response.completed", benchmark_send_started_s=send_started, benchmark_send_finished_s=send_finished)) + b"\n\n")
                    self.wfile.flush()
                except (BrokenPipeError, ConnectionResetError, OSError): pass
                except (KeyError, ValueError, TypeError):
                    self.close_connection = True
        backend_stage("binding")
        self.server = _LoopbackHTTPServer(("127.0.0.1", 0), Handler)
        backend_stage("bound")
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        backend_stage("serving")
        self.url = f"http://127.0.0.1:{self.server.server_port}"
        return self

    def __exit__(self, *_):
        self.server.shutdown(); self.server.server_close(); self.thread.join()


class StreamObserver:
    def __init__(self, expected, *, prefix_lengths=None):
        self.expected = expected
        self.pending = b""
        self.text = ""
        self.terminal = False
        self.prefix_ms = [None] * 32
        self.send_started_s = None
        self.send_finished_s = None
        self.prefix_lengths = prefix_lengths if prefix_lengths is not None else [
            math.ceil(len(expected) * (i + 1) / 32) for i in range(32)]
        if (len(self.prefix_lengths) != 32 or
                any(type(n) is not int or n < 0 or n > len(expected) for n in self.prefix_lengths) or
                self.prefix_lengths != sorted(self.prefix_lengths) or
                self.prefix_lengths[-1] != len(expected)):
            raise ValueError("invalid logical prefix lengths")

    def observe_terminal(self, event):
        self.terminal = True
        self.send_started_s = event.get("benchmark_send_started_s")
        self.send_finished_s = event.get("benchmark_send_finished_s")


    def feed(self, chunk, elapsed_ms):
        self.pending += chunk
        if len(self.pending) > 1024 * 1024: raise ValueError("oversized frame")
        while b"\n\n" in self.pending:
            frame, self.pending = self.pending.split(b"\n\n", 1)
            for line in frame.splitlines():
                if not line.startswith(b"data:"): continue
                data = line[5:].strip()
                if data == b"[DONE]": continue
                event = json.loads(data)
                kind = event.get("type")
                if kind == "response.output_text.delta":
                    if self.terminal: raise ValueError("delta after terminal")
                    self.text += event["delta"]
                    if not self.expected.startswith(self.text): raise ValueError("wrong restoration")
                    for i in range(32):
                        length = self.prefix_lengths[i]
                        if self.prefix_ms[i] is None and len(self.text) >= length:
                            self.prefix_ms[i] = elapsed_ms
                elif kind == "response.completed": self.observe_terminal(event)
                else: raise ValueError("unexpected stream event")

    def valid(self):
        return self.terminal and self.text == self.expected and not self.pending.strip()


class _TimedRaw(io.RawIOBase):
    def __init__(self, sock, first, deadline):
        self.sock, self.first, self.deadline = sock, first, deadline
        self.stream = sock.makefile("rb", buffering=0)
    def readable(self): return True
    def readinto(self, buffer):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0: raise TimeoutError()
        self.sock.settimeout(remaining)
        n = self.stream.readinto(buffer)
        if n and not self.first: self.first.append(time.monotonic())
        return n
    def close(self):
        self.stream.close()
        super().close()


def request(url, case, expected_hash=None, timeout=45, expected_layout_hash=None, *, defer_boundary=False):
    if defer_boundary and (expected_hash is not None or expected_layout_hash is not None):
        raise ValueError("deferred_boundary_arguments")
    start = time.monotonic()
    row = dict(operation_id=case["operation_id"], supported=True, completed=False, started_s=start,
               outcome="unresolved", duration_ms=0.0, first_byte_ms=None, prefix_ms=[], status=None)
    address = urlsplit(url)
    if address.hostname != "127.0.0.1" or address.scheme != "http":
        raise ValueError("only isolated loopback endpoints are allowed")
    conn = http.client.HTTPConnection(address.hostname, address.port, timeout=timeout)
    first = []
    deadline = start + timeout
    class Response(http.client.HTTPResponse):
        def __init__(self, sock, *args, **kwargs):
            class Wrapper:
                def makefile(self, *_): return io.BufferedReader(_TimedRaw(sock, first, deadline))
            super().__init__(Wrapper(), *args, **kwargs)
    conn.response_class = Response
    expected = response_text(case.get("occurrences", ["page", "page"]))
    chunks = stream_chunks(case.get('occurrences', ['page', 'page']))
    observer = StreamObserver(expected, prefix_lengths=[
        sum(map(len, chunks[:i + 1])) for i in range(len(chunks))])
    try:
        conn.connect()
        remaining = deadline - time.monotonic()
        if remaining <= 0: raise TimeoutError()
        conn.sock.settimeout(remaining)
        conn.request("POST", "/v1/responses", body=case["body"], headers={'Content-Type': 'application/json', 'Authorization': 'Bearer benchmark-only', 'X-Benchmark-Text-Layout': '1' if expected_layout_hash is not None else '0', 'session_id': case.get('benchmark_session', ''), 'X-Benchmark-Text-Only-Reply': '1' if case.get('benchmark_text_only') else '0'})
        response = conn.getresponse()
        row["status"] = response.status
        boundary_ok = expected_hash is None or response.getheader("X-Benchmark-Body-SHA256") == expected_hash
        boundary_ok = boundary_ok and (expected_layout_hash is None or response.getheader("X-Benchmark-Text-Layout-SHA256") == expected_layout_hash)
        if defer_boundary:
            digest = response.getheader("X-Benchmark-Body-SHA256")
            boundary_ok = (isinstance(digest, str) and len(digest) == 64
                           and all(c in "0123456789abcdef" for c in digest))
            if boundary_ok:
                row["boundary_sha256"] = digest
        size = 0
        while True:
            data = response.read1(65536)
            if not data: break
            size += len(data)
            if size > 1024 * 1024: raise ValueError("oversized response")
            if response.status == 200: observer.feed(data, (time.monotonic() - start) * 1000)
        row["completed"] = True
        if response.status == 200:
            row["outcome"] = "supported_success" if boundary_ok and observer.valid() else "transformation_error"
        else: row["outcome"] = "unexpected_proxy_refusal" if response.status < 500 else "upstream_failure"
    except (TimeoutError, socket.timeout): row["outcome"] = "unresolved"
    except (ValueError, KeyError, TypeError, UnicodeError): row["outcome"] = "transformation_error"
    except (OSError, http.client.HTTPException): row["outcome"] = "client_or_model_failure"
    finally:
        conn.close()
        row["finished_s"] = time.monotonic()
        row["duration_ms"] = (row["finished_s"] - start) * 1000
        row["first_byte_ms"] = (first[0] - start) * 1000 if first else None
        row["prefix_ms"] = observer.prefix_ms
        row.update(relay_timings(start, observer.prefix_ms, observer.send_started_s, observer.send_finished_s))
    if defer_boundary and row["outcome"] == "supported_success":
        row["outcome"] = "boundary_pending"
    return row


def closed_loop(operation, clients, per_client=None, duration=None, *, total_count=None):
    if total_count is not None:
        if per_client is not None or duration is not None:
            raise ValueError('total_count_conflicting_mode')
        if type(total_count) is not int or total_count <= 0:
            raise ValueError('invalid_total_count')
        if type(clients) is not int or clients <= 0:
            raise ValueError('invalid_clients')
        per_client = (total_count + clients - 1) // clients
    if clients not in (1, 4, 8, 16) or (per_client is None) == (duration is None):
        raise ValueError('invalid load parameters')
    if per_client is not None and (type(per_client) is not int or per_client < 1):
        raise ValueError('invalid operation count')
    if duration is not None and (not math.isfinite(duration) or duration <= 0):
        raise ValueError('invalid duration')
    barrier = threading.Barrier(clients)

    def client(index):
        client_count = per_client if total_count is None else total_count // clients + (index < total_count % clients)
        rows = []
        barrier.wait()
        start = time.monotonic()
        serial = 0
        while serial < client_count if client_count is not None else time.monotonic() - start < duration:
            began = time.monotonic()
            try:
                row = operation(index, serial)
            except Exception:
                row = dict(operation_id=f'client-{index}-op-{serial}', supported=True, completed=False, outcome='harness_invalid', duration_ms=(time.monotonic() - began) * 1000)
            row.setdefault('started_s', began)
            row.setdefault('finished_s', time.monotonic())
            row['client'] = index
            rows.append(row)
            serial += 1
        return rows
    with concurrent.futures.ThreadPoolExecutor(max_workers=clients) as pool:
        rows = [row for group in pool.map(client, range(clients)) for row in group]
    return (rows, summarize_outcomes(rows))


def qualification(settings, measurements):
    unmet = ["full_qualification_not_implemented"]
    if settings.get("mode") != "qualification": unmet.append("diagnostic_run")
    if not measurements: unmet.append("missing_measurements")
    # Full performance qualification additionally needs real extraction/fairness,
    # resources, reference environment and all three normal repetitions.
    required = ("text_matrix", "throughput_windows", "real_ocr", "pdf", "fairness",
                "reference_environment", "resource_samples", "stage_metrics")
    for name in required:
        if settings.get(name) is not True: unmet.append(name)
    if any(m.get("failures", 0) for m in measurements): unmet.append("failed_operations")
    return dict(performance_qualified=not unmet, unmet_requirements=unmet)


REPORT_KEYS = frozenset("""
 schema_version settings environment source executable_sha256 workload measurements comparison
 performance_qualified unmet_requirements mode tier clients size_bytes repetitions count duration
 commit dirty generator_version seed regex_declarations dictionary_entries mapping_pairs sizes_bytes
 occurrences_per_request near_misses_per_request window_seconds elapsed_s warmup_seconds
 warmup_completed achieved_client_concurrency observed_proxy_active_peak
 successful_completions_per_second peak_proxy_rss_bytes peak_child_rss_bytes live_heap_available
 goroutines_available resource_samples final_metrics records attempted completed supported_attempted
 supported_success supported_success_rate failures outcomes repetition operation_id supported outcome
 duration_ms first_byte_ms prefix_ms status started_s finished_s client proxy_rss_bytes child_rss_bytes
 metrics unavailable stages_ms active_requests extractor_active extractor_queued vault_size runtime
 available heap_objects_bytes live_heap_bytes goroutines gc_cycles ingress_read metadata_inspect
 body_inspect extraction_wait extraction_run audit_write upstream_wait restore p50 p95 p99 samples
 paired_samples_valid added_first_byte_ms added_prefix_ms added_completion_ms throughput_ratio
 setup_failure phase collection_failed proxy_rss_samples child_rss_samples activity_samples population vault_entries compiled_detectors runtime_environment
 os architecture hardware_model logical_cpus memory_bytes go_version load_average on_ac low_power
 before after runner_sha256 generator_sha256
""".split()) | OUTCOMES | frozenset({"admission", "active", "queued", "reserved_input_bytes", "admission_wait"})
REPORT_KEYS = REPORT_KEYS | frozenset(['boundary_sha256', 'churn_index', 'final_vault_population', 'initial_metrics', 'initial_vault_population', 'live_vault_population', 'observed_new_pairs', 'planned_pairs', 'rate', 'unresolved_indices', 'verified_new_pairs', 'warmup_records', 'warmup_resource_samples'])

REPORT_KEYS = set(REPORT_KEYS) | {"template", "repeated", "stage_samples", "relay_samples_valid", "added_relay_ms", "added_relay_flush_ms", "upstream_send_ms", "upstream_flush_ms", "relay_ms", "relay_flush_ms", "warmup_count", "warmup_completed", "fixture_generator_version", "tool_versions", "swift", "tesseract", "pdftotext", "pdftoppm", "extraction_generator_sha256", "extraction_runner_sha256", "renderer_sha256", "request_bytes", "resources", "extraction_run", "extraction_wait", "achieved_concurrency"}
REPORT_KEYS = set(REPORT_KEYS) | {"workload_kind", "text_records", "image_records", "text_unsent", "aborted", "window_start_s", "stage_poll_errors", "text_outcomes", "image_outcomes", "text_completions_per_second", "correlation_enabled", "scheduled", "sent", "completed", "rejected", "client_unsent", "unsent_records", "scheduled_s", "dispatched_s", "reason"}



class PhaseFailure(Exception):
    def __init__(self, phase, records):
        super().__init__("phase validation failed")
        self.phase, self.records = phase, records
    def evidence(self):
        return dict(phase=self.phase, records=self.records, **summarize_outcomes(self.records))


REPORT_KEYS.update({"collection_failed", "collection_error_kind", "offered_rate",
                    "requested_duration_s", "max_outstanding", "error_kind"})


REPORT_KEYS.add("fixture_manifest_sha256")


def write_report(path, report):
    allowed_top = {"schema_version", "settings", "environment", "source", "executable_sha256",
                   "workload", "measurements", "comparison", "performance_qualified", "unmet_requirements", "setup_failure"}
    allowed_top.update({"completed", "error_kind"})
    if set(report) - allowed_top: raise ValueError("invalid report fields")
    forbidden = {"body", "input", "output", "originals", "occurrences", "near_misses", "oracle", "image", "credentials"}
    def check(value):
        if isinstance(value, dict):
            if set(value) & forbidden or set(value) - REPORT_KEYS: raise ValueError("private or unknown report fields")
            for v in value.values(): check(v)
        elif isinstance(value, list):
            for v in value: check(v)
    check(report)
    raw = json.dumps(report, allow_nan=False, indent=2)
    with open(path, "w", encoding="utf-8", opener=lambda p, f: os.open(p, f, 0o600)) as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(raw + "\n")


def peak_concurrency(rows):
    events = sorted([(r["started_s"], 1) for r in rows] + [(r["finished_s"], -1) for r in rows])
    active = peak = 0
    for _, change in events:
        active += change
        peak = max(peak, active)
    return peak


def window_successes(rows, start, duration):
    return sum(r["outcome"] == "supported_success" and start <= r["finished_s"] <= start + duration for r in rows)


def compare_relay(proxy, bypass):
    result = dict(relay_samples_valid=False, added_relay_ms=None, added_relay_flush_ms=None)
    try:
        paired = {}
        for field in ("relay_ms", "relay_flush_ms"):
            arms = []
            for records in (proxy, bypass):
                samples = []
                for row in records:
                    values = row.get(field)
                    prefixes = row.get("prefix_ms")
                    if row.get("outcome") != "supported_success" or not isinstance(values, list) or not isinstance(prefixes, list) or not values or len(values) != len(prefixes):
                        raise ValueError("incomplete_relay_samples")
                    for index, value in enumerate(values):
                        samples.append(dict(operation_id=f"{row['operation_id']}:{index}", first_byte_ms=value))
                arms.append(samples)
            paired["added_"+field] = summarize_pairs(*arms, allow_negative=True)
        result.update(paired)
        result["relay_samples_valid"] = True
    except (ValueError, TypeError, KeyError):
        pass
    return result


def compare_runs(proxy, bypass):
    result = dict(paired_samples_valid=False, added_first_byte_ms=None,
                  added_prefix_ms=None, added_completion_ms=None)
    try:
        result["added_first_byte_ms"] = summarize_pairs(proxy, bypass)
        result["added_completion_ms"] = summarize_pairs(
            [dict(operation_id=r["operation_id"], first_byte_ms=r["duration_ms"]) for r in proxy],
            [dict(operation_id=r["operation_id"], first_byte_ms=r["duration_ms"]) for r in bypass])
        prefixes = []
        for rows in (proxy, bypass):
            samples = []
            for r in rows:
                if len(r.get("prefix_ms", [])) != 32: raise ValueError("missing prefix samples")
                samples += [dict(operation_id=f'{r["operation_id"]}-prefix-{i}', first_byte_ms=ms)
                            for i, ms in enumerate(r["prefix_ms"])]
            prefixes.append(samples)
        result["added_prefix_ms"] = summarize_pairs(*prefixes)
        result["paired_samples_valid"] = True
    except (ValueError, KeyError, TypeError):
        result = dict(paired_samples_valid=False, added_first_byte_ms=None,
                      added_prefix_ms=None, added_completion_ms=None)
    result.update(compare_relay(proxy, bypass))
    return result


def prepare(directory, workload, seed_binary, proxy_port, ui_port, upstream_url, size, correlate=False):
    directory = Path(directory)
    def private(name, data):
        path = directory / name
        with open(path, "wb", opener=lambda p, f: os.open(p, f, 0o600)) as stream:
            stream.write(data)
    private("dictionary.txt", ("\n".join(workload.dictionary) + "\n").encode())
    private("pairs.json", encode(workload.pairs))
    private("case.json", workload.case(0, size)["body"])
    config = dict(listen=f"127.0.0.1:{proxy_port}", ui=dict(listen=f"127.0.0.1:{ui_port}"),
        upstreams=dict(codex=upstream_url),
        policy=dict(profile="sanitize", on_error="block", attachments="extract_text", max_body_bytes=12 * 1024 * 1024),
        vault=dict(mode="file", key_file=str(directory / "vault.key"), file=str(directory / "vault.enc")),
        audit=dict(enabled=True, file=str(directory / "audit.jsonl"), on_error="block"),
        rules=workload.rules)
    if correlate:
        config["observability"] = dict(correlate=True)
    private("config.json", encode(config))
    seeded = subprocess.run([str(seed_binary), str(directory)], capture_output=True, timeout=60)
    private("seed.log", seeded.stdout + seeded.stderr)
    if seeded.returncode: raise ValueError("vault seeding failed")
    return json.loads((directory / "oracle.json").read_text())


class Environment(harness.Environment):
    def __init__(self, binary, seed_binary, workload, size, correlate=False):
        self.correlate = correlate
        super().__init__(binary)
        self.seed_binary, self.workload, self.size = seed_binary, workload, size

    def __enter__(self):
        self.temp = tempfile.TemporaryDirectory(prefix="cui-performance-")
        self.directory = Path(self.temp.name)
        try:
            backend_dir = self.directory / "backend"
            backend_dir.mkdir(mode=0o700)
            backend = self.spawn([sys.executable, str(Path(__file__).resolve()), "--upstream", str(backend_dir)], "backend.log")
            ready = backend_dir / "ready.json"
            def backend_ready():
                return ready.exists() or backend.poll() is not None
            try:
                harness.wait_until(backend_ready, 10, "backend_start")
                if not ready.exists(): raise RuntimeError("backend exited")
            except Exception as error:
                log = (self.directory / "backend.log").read_text(errors="replace")
                kinds = re.findall(r"(?m)^([A-Za-z_]+(?:Error|Exception)):", log)
                kind = kinds[-1] if kinds else "unknown"
                stages = re.findall(r"(?m)^backend_stage=([a-z]+)$", log)
                stage = stages[-1] if stages else "not-entered"
                status = backend.poll()
                state = "running" if status is None else f"exit-{status}"
                raise RuntimeError(f"backend_start: {state}; error-kind={kind}; stage={stage}") from error
            self.upstream_url = json.loads(ready.read_text())["url"]
            proxy_port, ui_port = harness.port(), harness.port()
            self.mappings = prepare(self.directory, self.workload, self.seed_binary, proxy_port, ui_port, self.upstream_url, self.size, correlate=self.correlate)
            population_file = self.directory / "population.json"
            self.population = json.loads(population_file.read_text()) if population_file.exists() else dict(vault_entries=len(self.mappings), compiled_detectors=None)
            self.proxy = self.spawn([str(self.binary), "serve", "-config", str(self.directory / "config.json")], "proxy.log")
            self.url, self.ui = f"http://127.0.0.1:{proxy_port}", f"http://127.0.0.1:{ui_port}"
            def healthy():
                if self.proxy.poll() is not None: raise ValueError("proxy exited during startup")
                try:
                    with harness.OPENER.open(self.ui + "/api/metrics", timeout=1) as response:
                        return response.status == 200
                except OSError: return False
            harness.wait_until(healthy, 20, "proxy_start")
            if len(self.mappings) != len(self.workload.pairs): raise ValueError("vault population mismatch")
            return self
        except BaseException:
            self.__exit__(*sys.exc_info())
            raise

    def metrics(self, include_recent=False):
        with harness.OPENER.open(self.ui + "/api/metrics", timeout=2) as response:
            value = json.load(response)
        # Explicit projection: neither correlation data nor audit values enter
        # benchmark evidence. Unavailable runtime heap data remains unavailable.
        fields = ("stages_ms", "active_requests", "extractor_active", "extractor_queued", "vault_size", "runtime")
        return ({key: value[key] for key in fields if key in value}) | ({"recent_requests": value.get("recent_requests", [])} if include_recent else {})


class ResourceSampler:
    def __init__(self, env):
        self.env, self.samples = env, []
        self.stop = threading.Event()
    def sample(self):
        row = dict(elapsed_s=time.monotonic() - self.started, proxy_rss_bytes=None, child_rss_bytes=None)
        process = subprocess.run(["ps", "-axo", "pid=,ppid=,rss="], capture_output=True, text=True, timeout=3)
        if process.returncode == 0:
            values = [tuple(map(int, line.split())) for line in process.stdout.splitlines() if len(line.split()) == 3]
            descendants = {self.env.proxy.pid}
            while True:
                expanded = descendants | {pid for pid, parent, _ in values if parent in descendants}
                if expanded == descendants: break
                descendants = expanded
            parent = [rss * 1024 for pid, _, rss in values if pid == self.env.proxy.pid]
            row["proxy_rss_bytes"] = parent[0] if parent else None
            row["child_rss_bytes"] = sum(rss * 1024 for pid, _, rss in values if pid in descendants and pid != self.env.proxy.pid)
        try: row["metrics"] = self.env.metrics()
        except (OSError, ValueError): row["metrics"] = None
        self.samples.append(row)
    def __enter__(self):
        self.started = time.monotonic()
        def run():
            while not self.stop.is_set():
                try: self.sample()
                except (OSError, ValueError, subprocess.SubprocessError):
                    self.samples.append(dict(elapsed_s=time.monotonic() - self.started, unavailable=True))
                self.stop.wait(1)
        self.thread = threading.Thread(target=run, daemon=True); self.thread.start()
        return self
    def __exit__(self, *_):
        self.stop.set(); self.thread.join(timeout=6)
        if self.thread.is_alive(): raise ValueError("resource sampler did not drain")


def environment_info(binary):
    def captured(args):
        try:
            result = subprocess.run(args, capture_output=True, text=True, timeout=3)
            return result.stdout if result.returncode == 0 else ""
        except (OSError, subprocess.SubprocessError): return ""
    model = memory = ac = low_power = None
    if sys.platform == "darwin":
        model = captured(["sysctl", "-n", "hw.model"]).strip() or None
        raw = captured(["sysctl", "-n", "hw.memsize"]).strip()
        memory = int(raw) if raw.isdigit() else None
        battery = captured(["pmset", "-g", "batt"])
        ac = "AC Power" in battery if battery else None
        power = captured(["pmset", "-g", "custom"])
        modes = re.findall(r"lowpowermode\s+(\d+)", power)
        low_power = any(int(value) for value in modes) if modes else None
    version = re.search(r"go[0-9]+\.[0-9]+(?:\.[0-9]+)?", captured(["go", "version", str(binary)]))
    return dict(os=platform.system()+" "+platform.release(), architecture=platform.machine(),
                hardware_model=model, logical_cpus=os.cpu_count(), memory_bytes=memory,
                go_version=version.group(0) if version else None,
                load_average=list(os.getloadavg()) if hasattr(os, "getloadavg") else None,
                on_ac=ac, low_power=low_power)


def resource_summary(samples):
    def observed(values):
        return [v for v in values if type(v) in (int, float) and math.isfinite(v) and v >= 0]
    proxy = observed(s.get("proxy_rss_bytes") for s in samples)
    children = observed(s.get("child_rss_bytes") for s in samples)
    activity = observed((s.get("metrics") or {}).get("active_requests") for s in samples)
    return dict(peak_proxy_rss_bytes=max(proxy) if proxy else None,
                peak_child_rss_bytes=max(children) if children else None,
                observed_proxy_active_peak=max(activity) if activity else None,
                proxy_rss_samples=len(proxy), child_rss_samples=len(children), activity_samples=len(activity))


def run_phase(binary, seed_binary, workload, size, clients, bypass, count, duration=None,
              warmup_seconds=0, warmup_count=1):
    measured = []
    try:
        return _run_phase(measured, binary, seed_binary, workload, size, clients, bypass,
                          count, duration, warmup_seconds, warmup_count)
    except PhaseFailure:
        raise
    except Exception as error:
        if measured:
            raise PhaseFailure("measurement_collection", measured) from error
        # Context-manager cleanup must not hide preflight/warm-up evidence either.
        previous = error.__context__
        for _ in range(8):
            if isinstance(previous, PhaseFailure): raise previous from error
            if previous is None: break
            previous = previous.__context__
        raise


def _run_phase(measured, binary, seed_binary, workload, size, clients, bypass, count, duration=None,
               warmup_seconds=0, warmup_count=1):
    before = environment_info(binary)
    with Environment(binary, seed_binary, workload, size) as env:
        def operation(client, serial):
            case = workload.case(serial * clients + client, size)
            expected = case["body"]
            if not bypass:
                for original in set(case["occurrences"]):
                    expected = expected.replace(original.encode(), env.mappings[original].encode())
            return request(env.upstream_url if bypass else env.url, case, hashlib.sha256(expected).hexdigest())
        # Validate the same production path before either phase. Metrics expose
        # the seeded vault count only after a request has completed.
        preflight = workload.case(0, size)
        expected_preflight = preflight["body"]
        for original in set(preflight["occurrences"]):
            expected_preflight = expected_preflight.replace(original.encode(), env.mappings[original].encode())
        checked = request(env.url, preflight, hashlib.sha256(expected_preflight).hexdigest())
        if checked["outcome"] != "supported_success": raise PhaseFailure("preflight", [checked])
        if env.vault_count() != len(workload.pairs): raise ValueError("preflight population mismatch")
        warmup_started = time.monotonic()
        warm = []
        if warmup_seconds:
            rows, _ = closed_loop(operation, clients, duration=warmup_seconds)
            warm.extend(rows)
        if sum(r["outcome"] == "supported_success" for r in warm) < warmup_count:
            rows, _ = closed_loop(operation, clients, per_client=max(1, math.ceil(warmup_count / clients)))
            warm.extend(rows)
        warmup_elapsed = time.monotonic() - warmup_started
        if any(r["outcome"] != "supported_success" for r in warm):
            raise PhaseFailure("warmup", warm)
        with ResourceSampler(env) as resources:
            started = time.monotonic()
            rows, outcomes = closed_loop(operation, clients, per_client=count if duration is None else None, duration=duration)
            # Preserve the measured denominator before sampler/context cleanup
            # or final metrics/vault collection can fail.
            measured.extend(rows)
            elapsed = time.monotonic() - started
        metrics = env.metrics()
        if env.vault_count() != len(workload.pairs): raise ValueError("steady state mapping churn")
        metric_samples = [r.get("metrics") for r in resources.samples if r.get("metrics")]
        aggregates = resource_summary(resources.samples)
        successes = window_successes(rows, started, duration) if duration is not None else outcomes["supported_success"]
        return dict(mode="bypass" if bypass else "proxy", size_bytes=size, clients=clients,
                    environment=dict(before=before, after=environment_info(binary)), population=env.population,
                    window_seconds=duration, window_start_s=started, elapsed_s=elapsed, warmup_seconds=warmup_elapsed,
                    warmup_completed=sum(r["outcome"] == "supported_success" for r in warm),
                    achieved_client_concurrency=peak_concurrency(rows),
                    successful_completions_per_second=successes / (duration or elapsed),
                    live_heap_available=all(m.get("runtime", {}).get("available", False) for m in metric_samples) and bool(metric_samples),
                    goroutines_available=all(m.get("runtime", {}).get("available", False) for m in metric_samples) and bool(metric_samples),
                    resource_samples=resources.samples, final_metrics=metrics,
                    records=rows, **outcomes, **aggregates)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--upstream", type=Path, help=argparse.SUPPRESS)
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--seed-binary", type=Path)
    parser.add_argument("--tier", choices=("small", "normal", "large", "rules", "vault"), default="normal")
    parser.add_argument("--clients", type=int, choices=(1, 4, 8, 16), default=4)
    parser.add_argument("--size", type=int)
    parser.add_argument("--count", type=int, default=3, help="measured operations per client")
    parser.add_argument("--duration", type=float, help="closed-loop measured window in seconds")
    parser.add_argument("--repetitions", type=int, default=1)
    parser.add_argument("--warmup-seconds", type=float, default=0)
    parser.add_argument("--warmup-count", type=int, default=1)
    parser.add_argument("--report", type=Path)
    parser.add_argument("--prepare", type=Path, help="create private fixtures for component profiles, then exit")
    parser.add_argument("--repetition-offset", type=int, default=0)
    args = parser.parse_args()
    if args.repetition_offset < 0:
        parser.error("repetition offset must be nonnegative")
    if args.upstream:
        with FakeBackend() as backend:
            (args.upstream / "ready.json").write_text(json.dumps(dict(url=backend.url)))
            backend_stage("ready")
            threading.Event().wait()
        return
    if not args.seed_binary or not args.binary: parser.error("binary and seed-binary are required")
    workload = Workload(args.tier)
    size = args.size or workload.sizes[0]
    if size not in workload.sizes: parser.error("size is not in the selected contract tier")
    if args.repetitions < 1 or args.count < 1 or args.warmup_count < 1 or args.warmup_seconds < 0:
        parser.error("invalid measurement schedule")
    if args.prepare:
        args.prepare.mkdir(mode=0o700)
        prepare(args.prepare, workload, args.seed_binary.resolve(), harness.port(), harness.port(), "http://127.0.0.1:1", size)
        print(json.dumps(dict(fixture_ready=True, mapping_count=len(workload.pairs))))
        return
    if not args.report: parser.error("report is required")
    settings = dict(mode="diagnostic", tier=args.tier, clients=args.clients, size_bytes=size,
                    repetitions=args.repetitions, count=args.count, duration=args.duration)
    source = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    dirty = bool(subprocess.run(["git", "status", "--porcelain"], capture_output=True, text=True, check=True).stdout)
    measurements, comparisons = [], []
    report = dict(schema_version=1, settings=settings, source=dict(commit=source, dirty=dirty, runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                    generator_sha256=hashlib.sha256(Path(__file__).with_name("performance_workload.py").read_bytes()).hexdigest()),
                  executable_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), workload=workload.metadata(),
                  measurements=measurements, comparison=comparisons)
    try:
        for repetition in range(args.repetition_offset, args.repetition_offset + args.repetitions):
            phases = {}
            for bypass in ([True, False] if repetition % 2 == 0 else [False, True]):
                result = run_phase(args.binary.resolve(), args.seed_binary.resolve(), workload, size,
                                   args.clients, bypass, args.count, args.duration,
                                   args.warmup_seconds, args.warmup_count)
                result["repetition"] = repetition
                measurements.append(result)
                phases[bypass] = result
                report.update(qualification(settings, measurements))
                write_report(args.report, report)
                print(json.dumps(dict(repetition=repetition, bypass=bypass, attempted=result["attempted"], failures=result["failures"])), flush=True)
            comparison = compare_runs(phases[False]["records"], phases[True]["records"])
            comparison["repetition"] = repetition
            comparison["throughput_ratio"] = phases[False]["successful_completions_per_second"] / phases[True]["successful_completions_per_second"] if phases[True]["successful_completions_per_second"] else None
            comparisons.append(comparison)
        report.update(qualification(settings, measurements))
        write_report(args.report, report)
    except Exception as error:
        if isinstance(error, PhaseFailure):
            if error.phase == "measurement_collection":
                report["measurements"].append(dict(mode="bypass" if bypass else "proxy",
                    size_bytes=size, clients=args.clients, repetition=repetition,
                    collection_failed=True, **error.evidence()))
            else:
                report["setup_failure"] = error.evidence()
        report.update(performance_qualified=False, unmet_requirements=["run_failed"])
        write_report(args.report, report)
        print("performance run failed; evidence retained", file=sys.stderr)
        raise SystemExit(1)
    if any(m["failures"] for m in measurements): raise SystemExit(1)


if __name__ == "__main__":
    main()
