import importlib
import json
import tempfile
import unittest
from unittest import mock
import os
from pathlib import Path


class PerformanceTests(unittest.TestCase):
    def module(self):
        try: return importlib.import_module("performance")
        except ImportError: self.fail("performance runner missing")

    def test_split_stream_reconstructs_response(self):
        p = self.module()
        chunks = p.stream_chunks(["first-notebook", "second-notebook"] + ["page"] * 38)
        self.assertEqual(len(chunks), 32)
        self.assertEqual("".join(chunks), p.response_text(["first-notebook", "second-notebook"] + ["page"] * 38))
        self.assertNotIn("first-notebook", chunks[0])
        self.assertTrue("first-notebook".startswith(chunks[0]))

    def test_frame_parser_keeps_split_frames_and_requires_terminal(self):
        p = self.module()
        parser = p.StreamObserver("hello world")
        frame = b'data: {"type":"response.output_text.delta","delta":"hello world"}\n\n'
        parser.feed(frame[:9], 1.0)
        parser.feed(frame[9:], 2.0)
        self.assertFalse(parser.valid())
        parser.feed(b'data: {"type":"response.completed"}\n\n', 3.0)
        self.assertTrue(parser.valid())
        self.assertTrue(all(v == 2.0 for v in parser.prefix_ms))

    def test_wrong_restoration_and_malformed_events_fail(self):
        p = self.module()
        for frame in [b'data: {"type":"response.output_text.delta","delta":"wrong"}\n\n', b'data: invalid\n\n']:
            parser = p.StreamObserver("expected")
            with self.assertRaises(ValueError): parser.feed(frame, 1.0)

    def test_smoke_and_missing_evidence_cannot_qualify(self):
        p = self.module()
        self.assertFalse(p.qualification({"mode": "smoke"}, [] )["performance_qualified"])
        result = p.qualification({"mode": "qualification"}, [])
        self.assertFalse(result["performance_qualified"])
        self.assertIn("missing_measurements", result["unmet_requirements"])

    def test_closed_loop_preserves_failed_attempts(self):
        p = self.module()
        def operation(client, serial):
            return {"operation_id": f"client-{client}-op-{serial}", "outcome": "unresolved",
                    "completed": False, "supported": True, "duration_ms": 45.0}
        records, stats = p.closed_loop(operation, clients=4, per_client=3)
        self.assertEqual(len(records), 12)
        self.assertEqual(len({r["operation_id"] for r in records}), 12)
        self.assertEqual(stats["attempted"], 12)
        self.assertEqual(stats["supported_success"], 0)
        self.assertEqual(stats["failures"], 12)

    def test_exception_is_recorded_as_failed_attempt(self):
        p = self.module()
        def operation(client, serial): raise TimeoutError()
        rows, counts = p.closed_loop(operation, clients=1, per_client=2)
        self.assertEqual(counts["attempted"], 2)
        self.assertEqual(counts["failures"], 2)
        self.assertTrue(all(r["outcome"] == "harness_invalid" for r in rows))

    def test_report_rejects_payload_fields(self):
        p = self.module()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            with self.assertRaises(ValueError): p.write_report(path, {"body": "private"})
            self.assertFalse(path.exists())

    def test_request_timeout_is_bounded(self):
        p = self.module()
        with p.FakeBackend(delay=1.0, interval=0.0) as backend:
            case = {"body": json.dumps({"input": [{"role":"user", "content":[{"type":"input_text", "text":"page"}]}]}).encode(),
                    "operation_id": "timeout-case", "supported": True}
            result = p.request(backend.url, case, timeout=0.05)
            self.assertEqual(result["outcome"], "unresolved")
            self.assertFalse(result["completed"])
            self.assertLess(result["duration_ms"], 700)

    def test_concurrency_measured_from_actual_overlaps(self):
        p = self.module()
        rows = [dict(started_s=0, finished_s=2), dict(started_s=1, finished_s=3),
                dict(started_s=3, finished_s=4)]
        self.assertEqual(p.peak_concurrency(rows), 2)

    def test_incomplete_pair_set_invalidates_comparison(self):
        p = self.module()
        sample = dict(operation_id="one", first_byte_ms=None, duration_ms=45,
                      completed=False, supported=True, outcome="unresolved", prefix_ms=[])
        result = p.compare_runs([sample], [sample])
        self.assertFalse(result["paired_samples_valid"])
        self.assertIsNone(result["added_first_byte_ms"])

    def test_throughput_excludes_completions_after_window(self):
        p = self.module()
        rows = [dict(outcome="supported_success", finished_s=5),
                dict(outcome="supported_success", finished_s=11),
                dict(outcome="unresolved", finished_s=6)]
        self.assertEqual(p.window_successes(rows, 0, 10), 1)

    def test_http_close_delimited_stream_reaches_terminal(self):
        p = self.module()
        body = {"input": [{"role":"user", "content":[{"type":"input_text", "text":v}]} for v in ["first", "second"]]}
        case = dict(body=json.dumps(body).encode(), operation_id="close-delimited", occurrences=["first", "second"])
        with p.FakeBackend(delay=0.0, interval=0.0) as backend:
            result = p.request(backend.url, case)
        self.assertEqual(result["outcome"], "supported_success")
        self.assertTrue(result["completed"])
        self.assertEqual(len([v for v in result["prefix_ms"] if v is not None]), 32)

    def test_report_rejects_unknown_nested_fields(self):
        p = self.module()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            with self.assertRaises(ValueError): p.write_report(path, {"settings": {"unexpected_private_field": "private"}})
            self.assertFalse(path.exists())

    def test_failed_warmup_retains_every_attempt(self):
        p = self.module()
        rows = [dict(operation_id="one", supported=True, completed=False,
                     outcome="unresolved", duration_ms=45.0)]
        error = p.PhaseFailure("warmup", rows)
        evidence = error.evidence()
        self.assertEqual(evidence["attempted"], 1)
        self.assertEqual(evidence["failures"], 1)
        self.assertEqual(evidence["records"], rows)

    def test_flags_cannot_promote_partial_runner_to_qualification(self):
        p = self.module()
        settings = {name: True for name in ["text_matrix", "throughput_windows", "real_ocr", "pdf", "fairness", "reference_environment", "resource_samples", "stage_metrics"]}
        settings["mode"] = "qualification"
        self.assertFalse(p.qualification(settings, [{"failures": 0}])["performance_qualified"])

    def test_setup_failure_cleans_spawned_backend_and_private_files(self):
        p = self.module()
        env = p.Environment(Path("/nonexistent/cui-proxy"), Path("/nonexistent/cui-seedvault"), p.Workload(), 262144)
        real_killpg = os.killpg
        denied = False
        def transient_probe(pid, signum):
            nonlocal denied
            if signum == 0 and not denied:
                denied = True
                raise PermissionError("transient process group probe")
            return real_killpg(pid, signum)
        with mock.patch("harness.os.killpg", side_effect=transient_probe):
            with self.assertRaises(FileNotFoundError): env.__enter__()
        self.assertTrue(env.children)
        self.assertTrue(all(child.poll() is not None for child in env.children))
        self.assertFalse(env.directory.exists())

    def test_post_load_collection_errors_retain_measured_attempts(self):
        import contextlib
        p = self.module()
        for failure in ("metrics", "vault", "sampler", "environment"):
            with self.subTest(failure=failure):
                workload = p.Workload()
                env = mock.Mock()
                env.url = env.upstream_url = "http://127.0.0.1:1"
                env.mappings = {value: value for _, value in workload.pairs}
                env.population = {"vault_entries":10000, "compiled_detectors":64}
                env.vault_count.side_effect = [10000, 9999 if failure == "vault" else 10000]
                env.metrics.side_effect = OSError() if failure == "metrics" else None
                env.metrics.return_value = {}
                calls = 0
                def operation(url, case, *_):
                    nonlocal calls
                    calls += 1
                    failed = calls >= 3
                    return dict(operation_id=case["operation_id"], supported=True,
                        completed=not failed, outcome="unresolved" if failed else "supported_success",
                        duration_ms=1.0, first_byte_ms=1.0, prefix_ms=[1.0]*32)
                class Context:
                    def __init__(self, value, fail): self.value, self.fail = value, fail
                    def __enter__(self): return self.value
                    def __exit__(self, *_):
                        if self.fail: raise OSError()
                sampler = mock.Mock(samples=[])
                with mock.patch.object(p, "Environment", return_value=Context(env, failure=="environment")), \
                     mock.patch.object(p, "ResourceSampler", return_value=Context(sampler, failure=="sampler")), \
                     mock.patch.object(p, "environment_info", return_value={}), \
                     mock.patch.object(p, "request", side_effect=operation):
                    with self.assertRaises(p.PhaseFailure) as caught:
                        p.run_phase(Path("unused"), Path("unused"), workload, 262144, 1, False, 1)
                evidence = caught.exception.evidence()
                self.assertEqual(evidence["attempted"], 1)
                self.assertEqual(evidence["failures"], 1)
                self.assertEqual(evidence["outcomes"]["unresolved"], 1)

    def test_missing_resources_are_not_measured_zeroes(self):
        p = self.module()
        missing = p.resource_summary([])
        for key in ("peak_proxy_rss_bytes", "peak_child_rss_bytes", "observed_proxy_active_peak"):
            self.assertIsNone(missing[key])
        self.assertEqual(missing["child_rss_samples"], 0)
        zero = p.resource_summary([dict(proxy_rss_bytes=10, child_rss_bytes=0, metrics={"active_requests":0})])
        self.assertEqual(zero["peak_child_rss_bytes"], 0)
        self.assertEqual(zero["observed_proxy_active_peak"], 0)
        self.assertEqual(zero["child_rss_samples"], 1)

    def test_cli_keeps_failed_measured_phase_in_denominators(self):
        import sys
        p = self.module()
        row = dict(operation_id="one", supported=True, completed=False, outcome="unresolved", duration_ms=45.0)
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);binary=root/"binary";binary.write_bytes(b"test")
            report=root/"report.json"
            args=["performance.py","--binary",str(binary),"--seed-binary",str(binary),"--report",str(report)]
            with mock.patch.object(sys,"argv",args), mock.patch.object(p,"run_phase",side_effect=p.PhaseFailure("measurement_collection",[row])):
                with self.assertRaises(SystemExit) as caught: p.main()
            self.assertEqual(caught.exception.code,1)
            result=json.loads(report.read_text())
            self.assertEqual(sum(m["attempted"] for m in result["measurements"]),1)
            self.assertEqual(sum(m["failures"] for m in result["measurements"]),1)
            self.assertFalse(result["performance_qualified"])

    def test_loopback_backend_does_not_depend_on_reverse_dns(self):
        p = self.module()
        body = {"input": [{"role":"user", "content":[{"type":"input_text", "text":v}]} for v in ["first", "second"]]}
        case = dict(body=json.dumps(body).encode(), operation_id="no-dns", occurrences=["first", "second"])
        with mock.patch("socket.getfqdn", side_effect=OSError("reverse DNS unavailable")):
            with p.FakeBackend(delay=0.0, interval=0.0) as backend:
                result = p.request(backend.url, case)
        self.assertEqual(result["outcome"], "supported_success")

    def test_closed_loop_total_count_balances_workers(self):
        import performance
        from collections import Counter
        import threading
        for clients, expected in ((8, [13, 13, 13, 13, 12, 12, 12, 12]),
                                  (16, [7, 7, 7, 7, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6])):
            with self.subTest(clients=clients):
                first = threading.Barrier(clients, timeout=5)
                seen = []
                lock = threading.Lock()
                def operation(client, serial):
                    if serial == 0:
                        first.wait()
                    with lock:
                        seen.append((client, serial))
                    return dict(operation_id=f"capacity-{client}-{serial}",
                                supported=True, completed=True, duration_ms=1.0,
                                outcome="supported_success")
                try:
                    rows, summary = performance.closed_loop(operation, clients, total_count=100)
                except TypeError as error:
                    self.fail(f"Exact total-count scheduling unavailable: {type(error).__name__}")
                self.assertEqual(len(rows), 100)
                self.assertEqual(len(set(seen)), 100)
                counts = Counter(client for client, _ in seen)
                self.assertEqual([counts[i] for i in range(clients)], expected)
                self.assertEqual(summary["attempted"], 100)

    def test_closed_loop_total_count_rejects_invalid_and_conflicting_modes(self):
        import performance
        def operation(client, serial):
            self.fail("Invalid scheduling settings must not dispatch work")
        for total in (0, -1, True, 1.5, float("nan"), float("inf")):
            with self.subTest(total=total):
                with self.assertRaises(ValueError):
                    performance.closed_loop(operation, 4, total_count=total)
        for kwargs in (dict(per_client=1), dict(duration=1)):
            with self.subTest(mode=next(iter(kwargs))):
                with self.assertRaises(ValueError):
                    performance.closed_loop(operation, 4, total_count=100, **kwargs)

    def test_closed_loop_total_count_retains_unsuccessful_operations(self):
        import performance
        def operation(client, serial):
            return dict(operation_id=f"capacity-{client}-{serial}", supported=True,
                        completed=serial != 0, duration_ms=1.0,
                        outcome="supported_success" if serial != 0 else "upstream_failure")
        try:
            rows, _ = performance.closed_loop(operation, 8, total_count=100)
        except TypeError as error:
            self.fail(f"Exact total-count scheduling unavailable: {type(error).__name__}")
        self.assertEqual(len(rows), 100)
        self.assertEqual(sum(row["outcome"] == "upstream_failure" for row in rows), 8)
        self.assertEqual(sum(row["outcome"] == "supported_success" for row in rows), 92)
