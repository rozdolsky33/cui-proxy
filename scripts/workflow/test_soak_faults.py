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
