import json
import unittest

from performance_workload import Workload
from soak_cases import build_tool_case


class ToolCaseTests(unittest.TestCase):
    def test_normal_workload_and_size_are_preserved_for_both_tools(self):
        workload = Workload()
        for size in (262144, 786432, 1048576):
            base = workload.case(0, size)
            original = json.loads(base["body"])
            for scenario, name, properties in (("W02", "apply_edit", {"path", "old", "new"}), ("W04", "plan_command", {"command"})):
                with self.subTest(size=size, scenario=scenario):
                    case = build_tool_case(base, scenario, "call-1", size)
                    body = json.loads(case["body"])
                    self.assertEqual(body["input"], original["input"])
                    self.assertEqual(case["occurrences"], base["occurrences"])
                    self.assertEqual(case["near_misses"], base["near_misses"])
                    self.assertEqual(case["operation_id"], base["operation_id"])
                    self.assertLessEqual(abs(len(case["body"]) - size), size * .01)
                    tool = body["tools"][0]
                    self.assertEqual(tool["name"], name)
                    self.assertTrue(tool["strict"])
                    self.assertEqual(set(tool["parameters"]["properties"]), properties)
                    self.assertEqual(set(tool["parameters"]["required"]), properties)
                    self.assertFalse(tool["parameters"]["additionalProperties"])
                    self.assertEqual(body["tool_choice"], {"type": "function", "name": name})
                    self.assertEqual(case["benchmark_tool_scenario"], scenario)
                    self.assertEqual(case["benchmark_tool_call_id"], "call-1")
                    self.assertNotIn("benchmark_tool_call_id", body)
            self.assertEqual(json.loads(base["body"]), original)

    def test_invalid_selection_or_size_cannot_form_a_qualified_case(self):
        case = Workload().case(0, 262144)
        for scenario, call_id, size in (("W01", "call-1", 262144), ("W02", "", 262144), ("W02", "x" * 129, 262144), ("W02", "call-1", 1), ("W02", "call-1", 1048576)):
            with self.subTest(scenario=scenario, size=size):
                with self.assertRaises(ValueError):
                    build_tool_case(case, scenario, call_id, size)


if __name__ == "__main__":
    unittest.main()
