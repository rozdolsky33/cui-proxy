import re
import soak_rejections as rejection
import copy
import json
import unittest

from performance_workload import Workload
from soak_rejections import negative_policy, build_rejection_case, verify_diagnostic


class RejectionTests(unittest.TestCase):
    def test_negative_policy_preserves_normal_rules_and_adds_explicit_block(self):
        baseline = {"rules": [{"name": "normal", "synth": "org", "type": "dictionary", "action": "sanitize", "entries": ["normal-entry"]}]}
        baseline["rules"].insert(0, {"name": "builtin", "type": "builtin"})
        changed = rejection.negative_policy(baseline)
        self.assertEqual(changed["rules"][:-1], baseline["rules"])
        self.assertEqual(changed["rules"][-1]["action"], "block")
        self.assertEqual(changed["rules"][-1]["synth"], "org")
        self.assertEqual(changed["rules"][-1]["type"], "regex")
        self.assertTrue(re.fullmatch(changed["rules"][-1]["pattern"], rejection.BLOCK_CANARY))
        self.assertEqual(len(baseline["rules"]), 2)

    def test_all_rejection_cases_keep_sensitive_canaries_for_leak_checks(self):
        workload = Workload()
        for variant in ("blocked", "unsupported", "malformed_json"):
            case = build_rejection_case(workload, 0, 262144, variant)
            self.assertTrue(all(value.encode() in case["body"] for value in case["forbidden_values"]))
            if variant == "malformed_json":
                with self.assertRaises(json.JSONDecodeError):
                    json.loads(case["body"])
            else:
                json.loads(case["body"])

    def test_exact_diagnostic_is_required_without_retaining_message(self):
        error = {"error": {"type": "cui_proxy_blocked", "code": "sanitize", "request_id": "a" * 16, "message": "reason=sanitize kind=bad_body", "recovery": {"code": "sanitize_bad_body"}}}
        row = verify_diagnostic(422, json.dumps(error).encode(), "malformed_json", ["private-canary"])
        self.assertEqual(row["reason"], "sanitize")
        self.assertEqual(row["kind"], "bad_body")
        self.assertNotIn("message", row)
        for changed in (dict(error["error"], code="policy"), dict(error["error"], message="private-canary kind=bad_body")):
            with self.assertRaises(ValueError):
                verify_diagnostic(422, json.dumps({"error": changed}).encode(), "malformed_json", ["private-canary"])
        with self.assertRaises(ValueError):
            verify_diagnostic(200, json.dumps(error).encode(), "malformed_json", [])
