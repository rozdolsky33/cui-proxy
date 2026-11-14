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


class ChurnCaseTests(unittest.TestCase):
    def test_repeated_originals_can_become_forty_distinct_new_pairs(self):
        import json
        from performance import values_from_body
        from soak_cases import inject_churn
        workload = Workload()
        for size in (262144, 786432, 1048576):
            base = workload.case(0, size)
            for count in (0, 5, 40):
                with self.subTest(size=size, count=count):
                    case = inject_churn(base, workload, list(range(count)), size)
                    expected = [workload.churn_value(i) for i in range(count)] + base["occurrences"][count:]
                    self.assertEqual(case["occurrences"], expected)
                    self.assertEqual(values_from_body(json.loads(case["body"])), expected)
                    self.assertEqual(len(expected), 40)
                    self.assertEqual(case["near_misses"], base["near_misses"])
                    for value in base["near_misses"]:
                        self.assertEqual(case["body"].count(value.encode()), base["body"].count(value.encode()))
                    self.assertLessEqual(abs(len(case["body"]) - size), size * .01)
                    self.assertEqual(case["operation_id"], base["operation_id"])
            self.assertEqual(base, workload.case(0, size))

    def test_invalid_assignments_are_rejected(self):
        from soak_cases import inject_churn
        workload = Workload()
        base = workload.case(0, 262144)
        for indices in ([0, 0], [-1], [True], list(range(41))):
            with self.assertRaises(ValueError):
                inject_churn(base, workload, indices, 262144)
