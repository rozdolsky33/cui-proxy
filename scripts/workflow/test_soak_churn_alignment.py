import unittest
from soak_schedule import churn_due_indices
from performance_churn import scheduled_offsets


class ChurnAlignmentTests(unittest.TestCase):
    def test_soak_uses_existing_half_open_arrival_schedule(self):
        offsets = scheduled_offsets(60)
        for index, offset in enumerate(offsets):
            self.assertEqual(churn_due_indices(offset, index, 40), (index,))
        self.assertEqual(churn_due_indices(7199.4, 11990, 40), tuple(range(11990,12000)))

    def test_first_pair_is_due_at_phase_start(self):
        self.assertEqual(churn_due_indices(0, 0, 40), (0,))
        self.assertEqual(churn_due_indices(0.599999, 1, 40), ())
        self.assertEqual(churn_due_indices(0.6, 1, 40), (1,))
