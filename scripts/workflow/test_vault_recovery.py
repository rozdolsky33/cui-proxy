"""Closed refusal classification for durable mapping storage failures."""
import json
import unittest
import performance

class VaultRecoveryClassificationTests(unittest.TestCase):
    def body(self, kind="cui_proxy_blocked", code="vault"):
        return json.dumps({"error": {"type": kind, "code": code}}).encode()

    def test_vault_refusal_is_dependency_failure(self):
        self.assertEqual(performance.failure_outcome(503, self.body()), "dependency_failure")

    def test_unrelated_response_is_not_vault_dependency(self):
        for status, body in [(502, self.body()), (503, self.body(kind="backend_error")),
                             (503, self.body(code="unrecognized")), (200, self.body())]:
            with self.subTest(status=status):
                self.assertNotEqual(performance.failure_outcome(status, body), "dependency_failure")

    def test_malformed_response_is_not_vault_dependency(self):
        for body in [b"", b"null", b"[]", b"not-json"]:
            self.assertNotEqual(performance.failure_outcome(503, body), "dependency_failure")
