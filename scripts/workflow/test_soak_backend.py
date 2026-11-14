import io
import unittest
from unittest.mock import Mock, patch

import performance
from soak_semantics import build_tool_call
from soak_tool_stream import ToolStreamObserver


class StructuredBackendTests(unittest.TestCase):
    def run_handler(self, scenario, call_id="call-1", fault=None, receipt=None):
        captured = []
        def server(address, handler):
            captured.append(handler)
            return Mock(server_port=1)
        with patch.object(performance, "_LoopbackHTTPServer", side_effect=server), patch.object(performance.threading, "Thread"), patch.object(performance, "backend_stage"):
            backend = performance.FakeBackend(delay=0, interval=0).__enter__()
        handler = object.__new__(captured[0])
        handler.headers = {"Content-Length": "2", "X-Benchmark-Tool-Scenario": scenario, "X-Benchmark-Tool-Call-ID": call_id}
        if fault is not None:
            handler.headers["X-Benchmark-Fault"] = fault
        if receipt is not None:
            handler.headers["X-Benchmark-Receipt-ID"] = receipt
        handler.rfile = io.BytesIO(b"{}")
        handler.wfile = io.BytesIO()
        handler.send_response = Mock()
        handler.send_header = Mock()
        handler.end_headers = Mock()
        handler.send_error = Mock()
        with patch.object(performance, "values_from_body", return_value=["first", "second"]) as parser:
            handler.do_POST()
        return handler

    def test_supported_tool_scenarios_emit_verifiable_structured_streams(self):
        for scenario in ("W02", "W03", "W04"):
            with self.subTest(scenario=scenario):
                handler = self.run_handler(scenario)
                observer = ToolStreamObserver(build_tool_call(scenario, ["first", "second"], "call-1"))
                observer.feed(handler.wfile.getvalue(), 1000)
                self.assertTrue(observer.valid())
                self.assertEqual(len(observer.send_started_s), 32)
                self.assertEqual(len(observer.send_finished_s), 32)
                handler.send_response.assert_called_once_with(200)

    def test_invalid_scenario_does_not_emit_success(self):
        handler = self.run_handler("unsupported")
        handler.send_response.assert_not_called()
        self.assertEqual(handler.wfile.getvalue(), b"")

    def test_missing_call_association_does_not_emit_success(self):
        handler = self.run_handler("W02", "")
        handler.send_response.assert_not_called()
        self.assertEqual(handler.wfile.getvalue(), b"")

    def test_fault_selector_routes_explicit_failure_responses(self):
        for fault, status in (("upstream_error", 503), ("malformed_success", 200)):
            with self.subTest(fault=fault):
                handler = self.run_handler("W02", fault=fault)
                handler.send_response.assert_called_once_with(status)
                self.assertNotIn(b"response.completed", handler.wfile.getvalue())

    def test_receipt_endpoint_observes_fault_request_before_error_response(self):
        import json
        import hashlib
        from soak_receipts import receipt_id
        key = receipt_id("fault-request")
        handler = self.run_handler("W02", fault="upstream_error", receipt=key)
        handler.wfile = io.BytesIO()
        handler.send_response.reset_mock()
        handler.path = "/benchmark/receipt/" + key
        handler.do_GET()
        handler.send_response.assert_called_once_with(200)
        self.assertEqual(json.loads(handler.wfile.getvalue()), {"count": 1, "body_sha256": hashlib.sha256(b"{}").hexdigest(), "complete": True})

    def test_activity_endpoint_records_handler_completion_on_fault_return(self):
        import json
        from soak_receipts import receipt_id
        key = receipt_id("fault-finished")
        handler = self.run_handler("W02", fault="upstream_error", receipt=key)
        handler.wfile = io.BytesIO()
        handler.path = "/benchmark/activity/" + key
        handler.do_GET()
        self.assertEqual(json.loads(handler.wfile.getvalue()), {"complete": True, "arrived": 1, "active": 0, "finished": 1, "disconnected": 0})


if __name__ == "__main__":
    unittest.main()
