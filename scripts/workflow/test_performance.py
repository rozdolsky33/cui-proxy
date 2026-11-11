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
