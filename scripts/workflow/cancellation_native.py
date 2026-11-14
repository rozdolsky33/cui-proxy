"""Private native I/O for the cancellation diagnostic; no installed changes."""
from concurrent.futures import ThreadPoolExecutor
import copy
import http.client
import json
import os
import re
from pathlib import Path
import subprocess
import threading
import time
from urllib.parse import urlsplit
from urllib.request import urlopen

from cancellation_audit import _index, attribute_cancellation
from cancellation_transport import ConnectionTracker
from soak_cancel import backend_evidence
from soak_finalization import read_audit
from soak_phase import OperationFailure


def parse_processes(text):
    rows = {}
    for line in text.splitlines():
        if not line.strip():
            continue
        fields = line.split(None, 3)
        if len(fields) != 4:
            raise ValueError("cancellation_process_fields")
        try:
            pid, parent = int(fields[0]), int(fields[1])
        except ValueError as error:
            raise ValueError("cancellation_process_pid") from error
        state, start = fields[2:]
        if (pid <= 0 or parent < 0 or pid in rows or re.fullmatch(r"[A-Za-z?<+]+", state) is None
                or not start.strip() or not start.isprintable() or len(start) > 128):
            raise ValueError("cancellation_process_identity")
        rows[pid] = {"pid": pid, "parent_pid": parent, "state": state, "start": start}
    return rows


def read_processes():
    result = subprocess.run(
        ["ps", "-axo", "pid=,ppid=,stat=,lstart="], capture_output=True,
        text=True, timeout=5, env={**os.environ, "LC_ALL": "C"})
    if result.returncode:
        raise ValueError("cancellation_process_snapshot")
    return parse_processes(result.stdout)


class NativeTrialDriver:
    def __init__(self, env, execute, plans, gate, proxy_identity, *,
                 process_reader=read_processes, metrics_reader=None, clock=time.monotonic):
        self.env, self.execute, self.plans, self.gate = env, execute, plans, gate
        self.proxy_identity = dict(proxy_identity)
        self.process_reader = process_reader
        self.metrics_reader = metrics_reader or self._metrics
        self.clock = clock
        self.pool = self.tracker = None
        self.futures, self.operations, self.known = {}, {}, {}
        self._cleaned = False
        self.factory_restored = False
        self.workers_joined = 0
        self._original_factory = http.client.HTTPConnection

    def sleep(self, seconds):
        time.sleep(seconds)

    def _metrics(self):
        with urlopen(self.env.ui.rstrip("/") + "/api/metrics", timeout=2) as response:
            data = response.read(8 * 1024 * 1024 + 1)
        if len(data) > 8 * 1024 * 1024:
            raise ValueError("cancellation_metrics_size")
        return json.loads(data)

    def arm(self):
        address = urlsplit(self.env.url)
        if address.scheme != "http" or address.hostname not in ("127.0.0.1", "localhost", "::1"):
            raise ValueError("cancellation_private_endpoint")
        self.tracker = ConnectionTracker(address.hostname, address.port or 80, clock=self.clock)
        self.tracker.__enter__()
        self.pool = ThreadPoolExecutor(max_workers=5, thread_name_prefix="cancellation-probe")
        self.gate.arm()

    def start(self, role):
        if role in self.futures or role not in self.plans or self.pool is None:
            raise ValueError("cancellation_native_operation")
        ready = threading.Event()
        plan = copy.deepcopy(self.plans[role])
        entry = {"request": plan}
        self.operations[role] = entry

        def worker():
            try:
                with self.tracker.track(role):
                    entry["started_s"] = self.tracker.snapshot(role)["request_started_s"]
                    ready.set()
                    try:
                        entry["result"] = self.execute(plan)
                    except OperationFailure as error:
                        entry["result"] = error.result
                        if role not in ("queued", "active"):
                            raise
            finally:
                entry["ended_s"] = self.clock()
                ready.set()
            return entry

        future = self.pool.submit(worker)
        self.futures[role] = future
        if not ready.wait(5):
            raise ValueError("cancellation_native_worker_start")
        if "started_s" not in entry:
            future.result(timeout=0)
            raise ValueError("cancellation_native_worker_scope")
        return entry["started_s"]

    def observe(self):
        started = self.clock()
        markers = self.gate.snapshot()
        processes = self.process_reader()
        parent = processes.get(self.proxy_identity["pid"])
        if (parent is None or parent["start"] != self.proxy_identity["start"]
                or "Z" in parent["state"] or "?" in parent["state"]):
            raise ValueError("cancellation_native_proxy_identity")
        live, stopped = [], []
        for marker in markers:
            pid = marker["pid"]
            current = processes.get(pid)
            if current is not None and "?" in current["state"]:
                raise ValueError("cancellation_native_unknown_helper_state")
            known = self.known.get(pid)
            if known is None:
                if current is None:
                    raise ValueError("cancellation_native_unbound_helper")
                if current["parent_pid"] != parent["pid"]:
                    raise ValueError("cancellation_native_helper_parent")
                known = {"pid": pid, "start": current["start"], "parent_pid": parent["pid"],
                         "parent_start": parent["start"], "generation": marker["generation"]}
                self.known[pid] = known
            if current is None or current["start"] != known["start"]:
                stopped.append({"helper": dict(known), "state": "absent"})
            elif "Z" in current["state"]:
                stopped.append({"helper": dict(known), "state": "zombie"})
            elif current["parent_pid"] != known["parent_pid"]:
                raise ValueError("cancellation_native_helper_reparented")
            else:
                live.append(dict(known))
        metrics = self.metrics_reader()
        for key in ("extractor_active", "extractor_queued"):
            if type(metrics.get(key)) is not int or metrics[key] < 0:
                raise ValueError("cancellation_native_gauge")
        armed = self.gate.armed and not os.path.lexists(self.gate.generation / "release")
        ended = self.clock()
        for row in stopped:
            row.update(started_s=started, ended_s=ended)
        return {"started_s": started, "ended_s": ended,
                "active": metrics["extractor_active"], "queued": metrics["extractor_queued"],
                "helpers": live, "stopped_helpers": stopped, "marker_count": len(markers),
                "gate_armed": bool(armed)}

    def cancel(self, role):
        state = self.tracker.cancel(role)
        return {key: state[key] for key in ("request_started_s", "cancel_s", "closed_by_probe", "headers_received_before_cancel")}

    def transport(self, role):
        return self.tracker.snapshot(role)["transport_end_s"]

    def audit(self):
        return read_audit(Path(self.env.directory) / "audit.jsonl")

    def audit_after(self, before, detail, deadline):
        previous = _index(before)
        while self.clock() <= deadline:
            after = self.audit()
            current = _index(after)
            blocked = [row for identity, row in current.items()
                       if identity not in previous and row["decision"] == "blocked"]
            if blocked:
                attribute_cancellation(before, after, expected_detail=detail)
                return after
            self.sleep(0.01)
        raise ValueError("cancellation_native_audit_deadline")

    def release(self):
        if not self.gate.armed or os.path.lexists(self.gate.generation / "release"):
            raise ValueError("cancellation_native_early_release")
        self.gate.release()
        return self.clock()

    def finish(self, deadline):
        for future in self.futures.values():
            remaining = deadline - self.clock()
            if remaining <= 0:
                raise ValueError("cancellation_native_operation_deadline")
            future.result(timeout=remaining)
        self.pool.shutdown(wait=True)
        self.workers_joined = len(self.futures)
        return copy.deepcopy(self.operations), self.workers_joined

    def receipts(self, operations):
        started = self.clock()
        rows = {role: backend_evidence(self.env.upstream_url, "receipt", operations[role]["request"]["identity"], 5)
                for role in ("queued", "active")}
        return rows, {"started_s": started, "ended_s": self.clock()}

    def final_helpers(self):
        markers = self.gate.snapshot()
        if any(row["pid"] not in self.known for row in markers):
            raise ValueError("cancellation_native_unbound_final_helper")
        return [dict(self.known[row["pid"]]) for row in markers]

    def cleanup(self):
        if self._cleaned:
            return
        errors = []
        try:
            self.gate.release()
        except Exception as error:
            errors.append(error)
        if self.tracker is not None:
            try:
                self.tracker.__exit__(None, None, None)
            except Exception as error:
                errors.append(error)
            self.tracker = None
        self.factory_restored = http.client.HTTPConnection is self._original_factory
        if self.pool is not None:
            try:
                self.pool.shutdown(wait=True, cancel_futures=True)
                self.workers_joined = sum(future.done() for future in self.futures.values())
            except Exception as error:
                errors.append(error)
        self._cleaned = not errors
        if errors:
            raise errors[0]
