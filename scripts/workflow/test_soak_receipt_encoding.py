import re
import unittest

from soak_receipts import receipt_id, _valid


class ReceiptEncodingTests(unittest.TestCase):
    def test_generated_identifiers_avoid_hex_and_numeric_secret_shapes(self):
        keys = [receipt_id(f"operation-{index}") for index in range(1000)]
        self.assertEqual(len(set(keys)), len(keys))
        self.assertTrue(all(re.fullmatch(r"[g-v]{64}", key) for key in keys))
        self.assertEqual(receipt_id("operation-0"), keys[0])

    def test_store_accepts_new_ids_and_archived_hex_ids(self):
        self.assertTrue(_valid("g" * 64))
        self.assertTrue(_valid("a" * 64))
        self.assertFalse(_valid("g" * 63))
        self.assertFalse(_valid("g" * 63 + "0"))
        self.assertFalse(_valid("G" * 64))
