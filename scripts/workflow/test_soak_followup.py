import json
import unittest
from soak_semantics import build_tool_call, verify_tool_effect


class FollowupTests(unittest.TestCase):
    def test_output_quoting_and_numeric_exit_code_survive_followup(self):
        values = ['quoted "line"\nnext', 'path\\reference']
        call = build_tool_call("W03", values, "call-followup")
        args = json.loads(call["arguments"])
        self.assertEqual(call["name"], "analyze_test_result")
        self.assertEqual(args, {"output": values[0], "reference": values[1], "exit_code": 0})
        self.assertEqual(verify_tool_effect("W03", call, values, "call-followup"), "test_followup_verified")

    def test_wrong_numeric_type_or_association_cannot_pass(self):
        values = ["first", "second"]
        call = build_tool_call("W03", values, "call-followup")
        for code in (False, "0", 0.0, 1):
            bad = dict(call, arguments=json.dumps({"output": values[0], "reference": values[1], "exit_code": code}))
            with self.subTest(code=code), self.assertRaises(ValueError):
                verify_tool_effect("W03", bad, values, "call-followup")
        with self.assertRaises(ValueError):
            verify_tool_effect("W03", dict(call, call_id="wrong"), values, "call-followup")

    def test_case_contains_associated_test_output_and_followup_schema(self):
        from performance_workload import Workload
        from soak_cases import build_tool_case
        base = Workload().case(0, 262144)
        case = build_tool_case(base, "W03", "call-followup", 262144)
        body = json.loads(case["body"])
        from performance import values_from_body
        self.assertEqual(values_from_body(body), base["occurrences"])
        self.assertEqual(body["input"][0]["type"], "function_call")
        self.assertEqual(body["input"][1]["type"], "function_call_output")
        self.assertEqual(body["input"][0]["call_id"], body["input"][1]["call_id"])
        self.assertIn(base["occurrences"][0], body["input"][1]["output"])
        self.assertEqual(body["tools"][0]["parameters"]["properties"]["exit_code"], {"type": "integer"})
        self.assertLessEqual(abs(len(case["body"]) - 262144), 262144 * .01)
