import re
import unittest
from performance_workload import Workload


class ChurnValueTests(unittest.TestCase):
    def test_new_values_are_unique_outside_population_and_match_existing_rules(self):
        workload = Workload()
        originals = {pair[1] for pair in workload.pairs}
        values = [workload.churn_value(index) for index in range(200)]
        self.assertEqual(len(set(values)), 200)
        self.assertFalse(set(values) & originals)
        patterns = [re.compile(rule["pattern"]) for rule in workload.rules if "pattern" in rule]
        self.assertTrue(all(any(pattern.fullmatch(value) for pattern in patterns) for value in values))
        self.assertEqual(values, [workload.churn_value(index) for index in range(200)])

    def test_invalid_churn_indices_are_rejected(self):
        workload = Workload()
        for index in (-1, True, 1.0, "1"):
            with self.assertRaises(ValueError):
                workload.churn_value(index)
