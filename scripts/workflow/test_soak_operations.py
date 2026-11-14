import unittest
from unittest.mock import patch

from performance_workload import Workload
from soak_cases import build_tool_case
from soak_semantics import build_tool_call
from soak_tool_stream import tool_events
from soak_operations import run_tool_operation


class ToolOperationTests(unittest.TestCase):
    def case(self, scenario):
        return build_tool_case(Workload().case(0, 262144), scenario, "call-1", 262144)

    def fake_request(self, scenario, values, complete=True):
        def request(*args, observer_factory, **kwargs):
            observer = observer_factory()
            if complete:
                for index, event in enumerate(tool_events(build_tool_call(scenario, values, "call-1"))):
                    observer.observe(event, index)
            return {"outcome": "supported_success", "completed": True}
        return request

    def test_stream_and_effect_must_both_pass_for_supported_success(self):
        for scenario in ("W02", "W03", "W04"):
            case = self.case(scenario)
            with self.subTest(scenario=scenario), patch("soak_operations.performance.request", side_effect=self.fake_request(scenario, case["occurrences"][:2])):
                row = run_tool_operation("http://unused", case, "boundary")
                self.assertEqual(row["outcome"], "supported_success")
                self.assertTrue(row["semantic_verified"])
                self.assertEqual(row["scenario"], scenario)
                self.assertNotIn("arguments", row)

    def test_transport_success_without_stream_cannot_pass(self):
        case = self.case("W02")
        with patch("soak_operations.performance.request", side_effect=self.fake_request("W02", case["occurrences"][:2], complete=False)):
            row = run_tool_operation("http://unused", case, "boundary")
        self.assertFalse(row["semantic_verified"])
        self.assertEqual(row["outcome"], "client_or_model_failure")

    def test_effect_failure_cannot_pass_even_after_complete_stream(self):
        case = self.case("W02")
        with patch("soak_operations.performance.request", side_effect=self.fake_request("W02", case["occurrences"][:2])), patch("soak_operations.verify_tool_effect", side_effect=ValueError("private detail")):
            row = run_tool_operation("http://unused", case, "boundary")
        self.assertFalse(row["semantic_verified"])
        self.assertEqual(row["outcome"], "client_or_model_failure")
        self.assertNotIn("private detail", str(row))

    def test_transport_failure_is_preserved_and_does_not_apply_effect(self):
        case = self.case("W04")
        with patch("soak_operations.performance.request", return_value={"outcome": "timeout", "completed": False}), patch("soak_operations.verify_tool_effect") as effect:
            row = run_tool_operation("http://unused", case, "boundary")
        effect.assert_not_called()
        self.assertEqual(row["outcome"], "timeout")
        self.assertFalse(row["semantic_verified"])

    def test_new_mapping_tool_effect_is_checked_while_boundary_stays_pending(self):
        case = self.case("W02")
        fake = self.fake_request("W02", case["occurrences"][:2])
        def pending(*args, **kwargs):
            self.assertTrue(kwargs["defer_boundary"])
            return dict(fake(*args, **kwargs), outcome="boundary_pending")
        with patch("soak_operations.performance.request", side_effect=pending):
            row = run_tool_operation("http://unused", case, None, defer_boundary=True)
        self.assertTrue(row["semantic_verified"])
        self.assertEqual(row["outcome"], "boundary_pending")
