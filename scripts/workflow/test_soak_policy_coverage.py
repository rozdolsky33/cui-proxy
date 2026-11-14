import unittest
import soak_policy_coverage as coverage


class CoverageTests(unittest.TestCase):
    def rows(self):
        return [
            {"server_request_id": "a" * 16, "outcome": "supported_success"},
            {"server_request_id": "b" * 16, "outcome": "supported_success", "old_substitutions_only": True, "completed": True, "semantic_verified": True, "boundary_verified": True},
            {"outcome": "expected_rejection", "scenario": "W11", "expected_failure_verified": True, "boundary_verified": True, "upstream_arrivals": 0, "diagnostic": {"request_id": "c" * 16, "reason": "sanitize", "kind": "bad_body"}},
        ]

    def records(self):
        return [{"request_id": "a" * 16, "rule_ids": ["soak-g0-example"]}, {"request_id": "b" * 16, "rule_ids": []}, {"request_id": "c" * 16, "rule_ids": []}]

    def test_every_request_is_reconciled_and_empty_decisions_are_explained(self):
        result = coverage.verify_policy_coverage(self.records(), self.rows(), [0])
        self.assertEqual(result["audit_records"], 3)
        self.assertEqual(result["old_token_records_without_matches"], 1)
        self.assertEqual(result["shape_rejections_without_matches"], 1)

    def test_missing_duplicate_and_unexplained_records_fail(self):
        mutations = [self.records()[:-1], self.records() + [self.records()[0]], self.records() + [{"request_id": "d" * 16, "rule_ids": []}]]
        for records in mutations:
            with self.assertRaises(ValueError): coverage.verify_policy_coverage(records, self.rows(), [0])
        records = self.records(); records[0]["rule_ids"] = []
        with self.assertRaises(ValueError): coverage.verify_policy_coverage(records, self.rows(), [0])

    def test_empty_old_token_record_requires_verified_restoration(self):
        rows = self.rows(); rows[1]["semantic_verified"] = False
        with self.assertRaises(ValueError): coverage.verify_policy_coverage(self.records(), rows, [0])

    def test_unobserved_generation_and_mixed_policy_are_rejected(self):
        with self.assertRaises(ValueError): coverage.verify_policy_coverage(self.records(), self.rows(), [0,1])
        records = self.records(); records[0]["rule_ids"].append("soak-g1-example")
        with self.assertRaises(ValueError): coverage.verify_policy_coverage(records, self.rows(), [0,1])
