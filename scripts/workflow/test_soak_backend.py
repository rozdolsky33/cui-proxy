import io
import unittest
from unittest.mock import Mock, patch

import performance
from soak_semantics import build_tool_call
from soak_tool_stream import ToolStreamObserver


class StructuredBackendTests(unittest.TestCase):
    def run_handler(self, scenario, call_id="call-1"):
        captured = []
        def server(address, handler):
            captured.append(handler)
            return Mock(server_port=1)
        with patch.object(performance, "_LoopbackHTTPServer", side_effect=server), patch.object(performance.threading, "Thread"), patch.object(performance, "backend_stage"):
            backend = performance.FakeBackend(delay=0, interval=0).__enter__()
        handler = object.__new__(captured[0])
        handler.headers = {"Content-Length": "2", "X-Benchmark-Tool-Scenario": scenario, "X-Benchmark-Tool-Call-ID": call_id}
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


if __name__ == "__main__":
    unittest.main()
