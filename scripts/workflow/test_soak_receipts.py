import hashlib
import unittest
from soak_receipts import ReceiptStore, receipt_id


class ReceiptTests(unittest.TestCase):
    def test_records_only_counts_and_hashes_and_detects_duplicates(self):
        store = ReceiptStore()
        key = receipt_id("operation-1")
        self.assertEqual(store.get(key), {"complete": True, "count": 0, "body_sha256": None})
        store.record(key, b"private request")
        first = store.get(key)
        self.assertEqual(first, {"complete": True, "count": 1, "body_sha256": hashlib.sha256(b"private request").hexdigest()})
        first["count"] = 99
        store.record(key, b"private request")
        self.assertEqual(store.get(key)["count"], 2)
        self.assertNotIn("private request", str(store.get(key)))

    def test_unmarked_request_invalidates_absence_evidence(self):
        store = ReceiptStore()
        store.record(None, b"unmarked")
        row = store.get(receipt_id("missing"))
        self.assertEqual(row["count"], 0)
        self.assertFalse(row["complete"])

    def test_capacity_exhaustion_cannot_look_like_zero_upstream_calls(self):
        store = ReceiptStore(capacity=1)
        first, second = receipt_id("one"), receipt_id("two")
        store.record(first, b"one")
        store.record(second, b"two")
        self.assertEqual(store.get(first)["count"], 1)
        self.assertFalse(store.get(second)["complete"])

    def test_invalid_lookup_and_identifier_are_rejected(self):
        with self.assertRaises(ValueError):
            receipt_id(1)
        with self.assertRaises(ValueError):
            ReceiptStore().get("invalid")

    def test_arrival_is_visible_before_body_is_read(self):
        store = ReceiptStore()
        key = receipt_id("arrival")
        store.record(key, None)
        self.assertEqual(store.get(key)["count"], 1)
        self.assertIsNone(store.get(key)["body_sha256"])
        store.complete_body(key, b"body")
        self.assertEqual(store.get(key)["count"], 1)
        self.assertEqual(store.get(key)["body_sha256"], hashlib.sha256(b"body").hexdigest())

    def test_absence_requires_complete_capture_and_positive_requires_exact_hash(self):
        from soak_receipts import verify_receipt
        self.assertTrue(verify_receipt({"complete": True, "count": 0, "body_sha256": None}, absent=True))
        self.assertTrue(verify_receipt({"complete": True, "count": 1, "body_sha256": "a" * 64}, expected_hash="a" * 64))
        for receipt, options in (({"complete": False, "count": 0, "body_sha256": None}, {"absent": True}), ({"complete": True, "count": 1, "body_sha256": None}, {"absent": True}), ({"complete": True, "count": 2, "body_sha256": "a" * 64}, {"expected_hash": "a" * 64}), ({"complete": True, "count": 1, "body_sha256": "b" * 64}, {"expected_hash": "a" * 64})):
            with self.assertRaises(ValueError):
                verify_receipt(receipt, **options)

    def test_handler_activity_is_independent_of_arrival_and_body_hash(self):
        store = ReceiptStore()
        key = receipt_id("active")
        store.record(key, b"body")
        self.assertEqual(store.activity(key), {"complete": True, "arrived": 1, "active": 1, "finished": 0, "disconnected": 0})
        store.finish(key, disconnected=True)
        self.assertEqual(store.activity(key), {"complete": True, "arrived": 1, "active": 0, "finished": 1, "disconnected": 1})
        self.assertEqual(store.get(key)["count"], 1)
        with self.assertRaises(ValueError):
            store.finish(key)
