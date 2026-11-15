import copy
import unittest

from cancellation_audit import attribute_cancellation


def row(identity, decision="blocked", detail="attachment_image_busy"):
    return {"request_id": identity, "decision": decision, "reason": "sanitize", "detail": detail}


class CancellationAuditTests(unittest.TestCase):
    def test_attributes_exactly_one_new_block_with_concurrent_success(self):
        before = [row("previous")]
        after = before + [row("text", "sanitized"), row("cancelled")]
        result = attribute_cancellation(before, after, expected_detail="attachment_image_busy")
        self.assertEqual(result["request_id"], "cancelled")
        self.assertEqual(result["new_records"], 2)
        self.assertIs(result["audit_attributed"], True)
        self.assertIs(result["cancellation_qualified"], False)
        self.assertIs(result["performance_qualified"], False)

    def test_rejects_absent_or_multiple_new_blocks_even_if_one_matches_detail(self):
        for after in ([], [row("text", "sanitized")], [row("one"), row("two", detail="different")]):
            with self.subTest(count=len(after)), self.assertRaises(ValueError):
                attribute_cancellation([], after, expected_detail="attachment_image_busy")

    def test_rejects_wrong_reason_detail_and_decision(self):
        for key, value in (("reason", "different"), ("detail", "different"), ("decision", "sanitized")):
            candidate = row("candidate")
            candidate[key] = value
            with self.subTest(field=key), self.assertRaises(ValueError):
                attribute_cancellation([], [candidate], expected_detail="attachment_image_busy")

    def test_rejects_duplicate_ids_in_either_snapshot(self):
        for before, after in (([row("old"), row("old")], [row("new")]), ([], [row("new"), row("new")])):
            with self.assertRaises(ValueError):
                attribute_cancellation(before, after, expected_detail="attachment_image_busy")

    def test_rejects_missing_or_mutated_prior_records(self):
        original = row("old")
        changed = copy.deepcopy(original)
        changed["detail"] = "different"
        for after in ([row("new")], [changed, row("new")]):
            with self.assertRaises(ValueError):
                attribute_cancellation([original], after, expected_detail="attachment_image_busy")

    def test_rejects_malformed_snapshot_and_ids(self):
        for bad in (None, {}, [None], [{}], [row(1)], [row("")], [row("x\n")], [row("x" * 513)]):
            with self.subTest(kind=type(bad).__name__), self.assertRaises(ValueError):
                attribute_cancellation([], bad, expected_detail="attachment_image_busy")

    def test_rejects_unknown_decision_and_invalid_expected_detail(self):
        with self.assertRaises(ValueError):
            attribute_cancellation([], [row("unknown", "unknown"), row("cancelled")], expected_detail="attachment_image_busy")
        for detail in (None, "", "x\n", 1):
            with self.assertRaises(ValueError):
                attribute_cancellation([], [row("cancelled")], expected_detail=detail)

    def test_does_not_mutate_input_or_leak_other_record_fields(self):
        candidate = row("cancelled")
        candidate["private_extra"] = "not part of proof"
        before = []
        after = [candidate]
        original = copy.deepcopy(after)
        result = attribute_cancellation(before, after, expected_detail="attachment_image_busy")
        self.assertEqual(after, original)
        self.assertNotIn("private_extra", result)


if __name__ == "__main__":
    unittest.main()
