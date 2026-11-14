import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch, MagicMock

import soak_rejections as rejection


class RejectionExecutionTests(unittest.TestCase):
    def test_request_uses_responses_endpoint_and_requires_absent_receipt(self):
        response = MagicMock()
        response.status = 422
        response.read.return_value = b"diagnostic"
        connection = MagicMock()
        connection.getresponse.return_value = response
        case = {"body": b"{}", "operation_id": "negative-1", "forbidden_values": []}
        with patch("soak_rejections.http.client.HTTPConnection", return_value=connection), patch("soak_rejections.request_headers", return_value={}), patch("soak_rejections.verify_diagnostic", return_value={"kind": "bad_body"}), patch("soak_rejections.backend_evidence", return_value={"complete": True, "count": 0, "body_sha256": None}):
            row = rejection.execute_rejection("http://127.0.0.1:1234", case, "malformed_json", "http://127.0.0.1:4321")
        self.assertEqual(connection.request.call_args.args[:2], ("POST", "/v1/responses"))
        self.assertTrue(row["expected_failure_verified"])
        self.assertEqual(row["outcome"], "expected_rejection")
        connection.close.assert_called_once()

    def test_positive_upstream_receipt_cannot_pass(self):
        response = MagicMock(status=422)
        response.read.return_value = b"diagnostic"
        connection = MagicMock()
        connection.getresponse.return_value = response
        case = {"body": b"{}", "operation_id": "negative-2", "forbidden_values": []}
        with patch("soak_rejections.http.client.HTTPConnection", return_value=connection), patch("soak_rejections.request_headers", return_value={}), patch("soak_rejections.verify_diagnostic", return_value={}), patch("soak_rejections.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": "a" * 64}):
            with self.assertRaises(ValueError):
                rejection.execute_rejection("http://127.0.0.1:1234", case, "blocked", "http://127.0.0.1:4321")
        connection.close.assert_called_once()

    def test_log_scan_checks_boundaries_without_retaining_values(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "owned.log"
            path.write_bytes(b"x" * 65534 + b"private-canary" + b"y")
            with self.assertRaisesRegex(ValueError, "protected value in owned log"):
                rejection.verify_log_privacy([path], ["private-canary"])
            path.write_bytes(b"safe metadata only")
            self.assertEqual(rejection.verify_log_privacy([path], ["private-canary"]), {"files_checked": 1, "protected_values_checked": 1, "privacy_verified": True})
            with self.assertRaises(FileNotFoundError):
                rejection.verify_log_privacy([Path(directory) / "missing"], ["private-canary"])

    def test_audio_rejection_requires_specific_recovery_code(self):
        body = {"error": {"type": "cui_proxy_blocked", "code": "sanitize", "message": "reason=sanitize kind=bad_body", "request_id": "a" * 16, "recovery": {"code": "attachment_audio_unsupported"}}}
        row = rejection.verify_diagnostic(422, json.dumps(body).encode(), "unsupported", [])
        self.assertEqual(row["recovery_code"], "attachment_audio_unsupported")
        body["error"]["recovery"]["code"] = "sanitize_bad_body"
        with self.assertRaises(ValueError):
            rejection.verify_diagnostic(422, json.dumps(body).encode(), "unsupported", [])
