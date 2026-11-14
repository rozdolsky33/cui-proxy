import hashlib
import json
import unittest
from unittest.mock import patch

from performance import values_from_body
from performance_workload import Workload
from soak_schedule import plan_request
from soak_dispatch import build_text_case, execute_text


class TextDispatchTests(unittest.TestCase):
    def plans(self, phase="fixed"):
        found = {}
        for serial in range(50):
            plan = plan_request(0, serial, phase)
            if plan["category"] == "text":
                plan.update(elapsed_s=60, churn_indices=[] if phase == "fixed" else [0, 1])
                found.setdefault(plan["scenario"], plan)
        return found

    def test_every_text_scenario_reconstructs_exact_values_and_bytes(self):
        workload = Workload()
        plans = self.plans("churn")
        self.assertEqual(set(plans), {"W01", "W02", "W03", "W04", "W08"})
        for scenario, plan in plans.items():
            with self.subTest(scenario=scenario):
                case = build_text_case(workload, plan)
                self.assertEqual(case, build_text_case(workload, plan))
                self.assertEqual(values_from_body(json.loads(case["body"])), case["occurrences"])
                self.assertEqual(case["occurrences"][:2], [workload.churn_value(0), workload.churn_value(1)])
                self.assertEqual(case["benchmark_session"], "soak-client-0")
                self.assertEqual(len(case["occurrences"]), 40)
                self.assertEqual(len(case["near_misses"]), 40)

    def test_proxy_requests_defer_boundary_and_tool_routes_require_effects(self):
        workload = Workload()
        for scenario, plan in self.plans().items():
            with self.subTest(scenario=scenario), patch("soak_dispatch.performance.request", return_value={"outcome": "boundary_pending", "completed": True}) as text_request, patch("soak_dispatch.run_tool_operation", return_value={"outcome": "boundary_pending", "completed": True, "semantic_verified": True}) as tool_request:
                row = execute_text("http://unused", workload, plan)
                chosen = tool_request if scenario in ("W02", "W03", "W04") else text_request
                self.assertEqual(chosen.call_count, 1)
                self.assertTrue(chosen.call_args.kwargs["defer_boundary"])
                self.assertIsNone(chosen.call_args.args[2])
                self.assertEqual(row["outcome"], "boundary_pending")
                self.assertEqual(row["scenario"], scenario)
                self.assertNotIn("body", row)

    def test_bypass_uses_exact_original_request_hash(self):
        workload = Workload()
        plan = self.plans()["W01"]
        case = build_text_case(workload, plan)
        with patch("soak_dispatch.performance.request", return_value={"outcome": "supported_success", "completed": True}) as request:
            execute_text("http://unused", workload, plan, bypass=True)
        self.assertFalse(request.call_args.kwargs["defer_boundary"])
        self.assertEqual(request.call_args.args[2], hashlib.sha256(case["body"]).hexdigest())

    def test_wrong_schedule_and_fixed_phase_churn_are_rejected(self):
        workload = Workload()
        plan = self.plans()["W01"]
        for changed in (dict(plan, scenario="W12"), dict(plan, churn_indices=[0]), dict(plan, size_bytes=1)):
            with self.assertRaises(ValueError):
                build_text_case(workload, changed)
