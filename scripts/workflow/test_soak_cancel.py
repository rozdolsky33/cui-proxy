import io
import unittest
from unittest.mock import Mock, patch

from soak_cancel import cancel_request


class CancelTests(unittest.TestCase):
    def connection(self):
        connection = Mock()
        response = Mock(status=200)
        response.getheader.return_value = "text/event-stream"
        stream = io.BytesIO(b'data: {"type":"response.output_text.delta","delta":"cancel-probe"}\n\n')
        response.read.side_effect = stream.read
        connection.getresponse.return_value = response
        return connection, response

    def case(self):
        return {"body": b"{}", "operation_id": "cancel-test", "occurrences": []}

    def test_cancel_requires_verified_boundary_and_upstream_disconnect(self):
        conn, response = self.connection()
        response.getheader.side_effect = lambda name, *args: "a" * 16 if name == "X-Cui-Proxy-Request-Id" else "text/event-stream"
        receipt = {"complete": True, "count": 1, "body_sha256": "a" * 64}
        active = {"complete": True, "arrived": 1, "active": 1, "finished": 0, "disconnected": 0}
        released = dict(active, active=0, finished=1, disconnected=1)
        with patch("soak_cancel.http.client.HTTPConnection", return_value=conn), patch("soak_cancel.backend_evidence", side_effect=[receipt, active, released]):
            row = cancel_request("http://127.0.0.1:1", self.case(), "a" * 64, "http://127.0.0.1:2")
            self.assertEqual(row["operation_id"], self.case()["operation_id"])
            self.assertTrue(row["upstream_receipt_verified"])
        self.assertEqual(row["outcome"], "cancelled")
        self.assertTrue(row["expected_failure_verified"])
        self.assertTrue(row["boundary_verified"])
        self.assertTrue(row["upstream_released"])
        self.assertFalse(row["completed"])
        self.assertEqual(conn.request.call_args.args[1], "/v1/responses")
        conn.sock.shutdown.assert_called_once()
        conn.close.assert_called()
        response.close.assert_called()
        self.assertEqual(conn.request.call_args.kwargs["headers"]["X-Benchmark-Fault"], "cancel")
        self.assertEqual(row["server_request_id"], "a" * 16)

    def test_normal_finish_without_disconnect_is_not_cancellation_proof(self):
        conn, _ = self.connection()
        receipt = {"complete": True, "count": 1, "body_sha256": "a" * 64}
        active = {"complete": True, "arrived": 1, "active": 1, "finished": 0, "disconnected": 0}
        with patch("soak_cancel.http.client.HTTPConnection", return_value=conn), patch("soak_cancel.backend_evidence", side_effect=[receipt, active, dict(active, active=0, finished=1)]):
            with self.assertRaises(ValueError):
                cancel_request("http://127.0.0.1:1/v1/responses", self.case(), "a" * 64, "http://127.0.0.1:2")
        conn.close.assert_called()

    def test_incomplete_receipts_cannot_verify_cancellation(self):
        conn, response = self.connection()
        with patch("soak_cancel.http.client.HTTPConnection", return_value=conn), patch("soak_cancel.backend_evidence", return_value={"complete": False, "count": 1, "body_sha256": "a" * 64}):
            with self.assertRaises(ValueError):
                cancel_request("http://127.0.0.1:1/v1/responses", self.case(), "a" * 64, "http://127.0.0.1:2")
        conn.close.assert_called()
        response.close.assert_called()
