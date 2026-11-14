import hashlib
import unittest

from soak_boundary import verify_deferred_boundary


class DeferredBoundaryTests(unittest.TestCase):
    def case(self):
        return {"body": b"first second first", "occurrences": ["first", "second", "first"]}

    def row(self):
        return {"outcome": "boundary_pending", "completed": True,
                "boundary_sha256": hashlib.sha256(b"one two one").hexdigest()}

    def test_matching_independent_vault_mapping_resolves_pending(self):
        row = self.row()
        result = verify_deferred_boundary(self.case(), row, {"first": "one", "second": "two"})
        self.assertEqual(result["outcome"], "supported_success")
        self.assertTrue(result["boundary_verified"])
        self.assertEqual(row["outcome"], "boundary_pending")

    def test_wrong_hash_or_missing_mapping_cannot_pass(self):
        for mappings in ({"first": "wrong", "second": "two"}, {"first": "one"}):
            result = verify_deferred_boundary(self.case(), self.row(), mappings)
            self.assertNotEqual(result["outcome"], "supported_success")
            self.assertFalse(result["boundary_verified"])

    def test_incomplete_response_cannot_be_promoted_by_matching_hash(self):
        row = dict(self.row(), completed=False)
        result = verify_deferred_boundary(self.case(), row, {"first": "one", "second": "two"})
        self.assertNotEqual(result["outcome"], "supported_success")

    def test_tool_case_requires_verified_effect_as_well(self):
        case = dict(self.case(), benchmark_tool_scenario="W02")
        mappings = {"first": "one", "second": "two"}
        self.assertNotEqual(verify_deferred_boundary(case, self.row(), mappings)["outcome"], "supported_success")
        result = verify_deferred_boundary(case, dict(self.row(), semantic_verified=True), mappings)
        self.assertEqual(result["outcome"], "supported_success")

    def test_failure_outcomes_are_preserved(self):
        row = dict(self.row(), outcome="transformation_error")
        result = verify_deferred_boundary(self.case(), row, {"first": "one", "second": "two"})
        self.assertEqual(result["outcome"], "transformation_error")
