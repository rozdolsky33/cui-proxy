import unittest
from performance_matrix import schedules


class MatrixTests(unittest.TestCase):
    def test_full_schedule_covers_contract_population(self):
        rows = list(schedules("text-qualification"))
        self.assertEqual(len(rows), 12)
        self.assertEqual({r["size"] for r in rows}, {262144, 786432, 1048576})
        self.assertEqual({r["clients"] for r in rows}, {1, 4, 8, 16})
        self.assertTrue(all(r["count"] * r["clients"] >= 1000 and r["repetitions"] == 3 for r in rows))
        self.assertTrue(all(r["warmup_seconds"] >= 60 and r["warmup_count"] >= 100 for r in rows))

    def test_smoke_schedule_cannot_masquerade_as_full_sample_count(self):
        self.assertTrue(all(r["count"] * r["clients"] < 1000 for r in schedules("smoke")))
        with self.assertRaises(ValueError): list(schedules("unknown"))
