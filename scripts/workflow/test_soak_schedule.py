import collections
import math
import unittest

import soak_schedule


class SoakScheduleTests(unittest.TestCase):
    def test_complete_cycles_preserve_request_mix_and_workflow_coverage(self):
        expected = {"text": 35, "image": 10, "lifecycle": 5}
        scenarios = {"text": {"W01", "W02", "W03", "W04", "W08"},
                     "image": {"W05", "W06"},
                     "lifecycle": {"W07", "W09", "W10", "W11", "W12"}}
        for client in range(4):
            for offset in (0, 50, 1000):
                rows = [soak_schedule.plan_request(client, serial, "fixed")
                        for serial in range(offset, offset + 50)]
                self.assertEqual(collections.Counter(r["category"] for r in rows), expected)
                for category, required in scenarios.items():
                    self.assertEqual({r["scenario"] for r in rows if r["category"] == category}, required)

    def test_sizes_are_equally_weighted_and_identity_is_stable(self):
        identities = set()
        for phase in ("fixed", "churn"):
            for client in range(4):
                rows = [soak_schedule.plan_request(client, serial, phase) for serial in range(150)]
                self.assertEqual(collections.Counter(r["size_bytes"] for r in rows),
                                 {262144: 50, 786432: 50, 1048576: 50})
                for serial, row in enumerate(rows):
                    self.assertEqual(row, soak_schedule.plan_request(client, serial, phase))
                    self.assertEqual(row["identity"], (phase, client, serial))
                    self.assertNotIn(row["identity"], identities)
                    identities.add(row["identity"])

    def test_invalid_schedule_identifiers_are_rejected(self):
        for client, serial, phase in [(-1, 0, "fixed"), (4, 0, "fixed"),
                                     (True, 0, "fixed"), (0, False, "fixed"),
                                     (0, -1, "fixed"), (0, 1.5, "fixed"),
                                     (0, 0, "unknown")]:
            with self.subTest(client=client, serial=serial, phase=phase):
                with self.assertRaises(ValueError):
                    soak_schedule.plan_request(client, serial, phase)

    def test_churn_pacing_and_capacity_keep_backlog(self):
        self.assertEqual(soak_schedule.churn_due_indices(0, 0, 40), (0,))
        self.assertEqual(soak_schedule.churn_due_indices(0.59, 0, 40), (0,))
        self.assertEqual(soak_schedule.churn_due_indices(0.6, 0, 40), (0, 1))
        self.assertEqual(soak_schedule.churn_due_indices(60, 0, 40), tuple(range(40)))
        self.assertEqual(soak_schedule.churn_due_indices(60, 40, 40), tuple(range(40, 80)))
        self.assertEqual(soak_schedule.churn_due_indices(60, 80, 40), tuple(range(80, 101)))
        self.assertEqual(soak_schedule.churn_due_indices(7200, 11998, 40), (11998, 11999, 12000))

    def test_invalid_churn_evidence_is_rejected(self):
        for elapsed, assigned, capacity in [(math.nan, 0, 40), (math.inf, 0, 40),
                                            (-1, 0, 40), (True, 0, 40),
                                            (1, True, 40), (1, -1, 40),
                                            (1, 0, False), (1, 0, 0),
                                            (1, 0, 41), (0, 2, 40)]:
            with self.subTest(elapsed=elapsed, assigned=assigned, capacity=capacity):
                with self.assertRaises(ValueError):
                    soak_schedule.churn_due_indices(elapsed, assigned, capacity)
