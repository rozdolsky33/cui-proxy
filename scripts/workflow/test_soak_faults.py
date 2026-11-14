import io
import unittest
from unittest.mock import Mock

from soak_faults import send_fault_response


class FaultSenderTests(unittest.TestCase):
    def handler(self):
        return Mock(wfile=io.BytesIO())

    def test_upstream_error_is_explicit_and_has_boundary_digest(self):
        handler = self.handler()
        self.assertTrue(send_fault_response(handler, "upstream_error", "a" * 64))
        handler.send_response.assert_called_once_with(503)
        handler.send_header.assert_any_call("X-Benchmark-Body-SHA256", "a" * 64)
        self.assertNotIn(b"response.completed", handler.wfile.getvalue())

    def test_malformed_success_is_not_a_valid_stream(self):
        handler = self.handler()
        self.assertTrue(send_fault_response(handler, "malformed_success", "a" * 64))
        handler.send_response.assert_called_once_with(200)
        handler.send_header.assert_any_call("Content-Type", "text/event-stream")
        self.assertEqual(handler.wfile.getvalue(), b"data: {\n\n")

    def test_default_is_untouched_and_unknown_selector_fails_before_headers(self):
        handler = self.handler()
        self.assertFalse(send_fault_response(handler, None, "a" * 64))
        handler.send_response.assert_not_called()
        with self.assertRaises(ValueError):
            send_fault_response(handler, "unknown", "a" * 64)
        handler.send_response.assert_not_called()

    def test_cancel_stream_records_disconnect_without_success_terminal(self):
        from soak_faults import send_cancel_stream
        class Disconnect(io.BytesIO):
            def write(self, data):
                if self.tell():
                    raise BrokenPipeError()
                return super().write(data)
        handler = self.handler()
        handler.wfile = Disconnect()
        send_cancel_stream(handler, "a" * 64, sleep=lambda _: None)
        self.assertTrue(handler.benchmark_disconnected)
        self.assertNotIn(b"response.completed", handler.wfile.getvalue())

    def test_cancel_timeout_is_not_reported_as_disconnect(self):
        from soak_faults import send_cancel_stream
        handler = self.handler()
        times = iter((0, 46))
        with self.assertRaises(ValueError):
            send_cancel_stream(handler, "a" * 64, clock=lambda: next(times), sleep=lambda _: None)
        self.assertFalse(handler.benchmark_disconnected)
