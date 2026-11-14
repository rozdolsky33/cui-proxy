import unittest
from soak_dispatch import build_text_case
from soak_schedule import plan_request
from performance_workload import Workload


class WarmupIdentityTests(unittest.TestCase):
    def test_text_warmup_has_disjoint_receipt_and_session_identity(self):
        workload=Workload();plan=dict(plan_request(0,0,"fixed"),churn_indices=[])
        measured=build_text_case(workload,plan)
        warmup=build_text_case(workload,plan,warmup=True)
        self.assertEqual(measured["operation_id"],"soak-fixed-0-0")
        self.assertEqual(warmup["operation_id"],"soak-warmup-fixed-0-0")
        self.assertEqual(measured["benchmark_session"],"soak-client-0")
        self.assertEqual(warmup["benchmark_session"],"soak-warmup-client-0")
        self.assertEqual(measured["occurrences"],warmup["occurrences"])

    def test_warmup_rejects_lifecycle_before_any_request(self):
        from pathlib import Path
        from unittest.mock import patch
        from soak_executor import ScenarioExecutor
        runner=ScenarioExecutor("http://127.0.0.1:1","http://127.0.0.1:2",
                                object(),{},[],Path("."),warmup=True)
        plan=dict(plan_request(0,9,"fixed"),churn_indices=[])
        with patch("soak_executor.execute_continuation",side_effect=AssertionError("unexpected request")):
            with self.assertRaises(ValueError):runner(plan)

    def test_text_sender_forwards_warmup_identity_to_http_boundary(self):
        from unittest.mock import patch
        from soak_dispatch import execute_text
        workload=Workload();plan=dict(plan_request(0,0,"fixed"),churn_indices=[])
        def received(url,case,*args,**kwargs):
            return dict(operation_id=case["operation_id"],observed_session=case["benchmark_session"])
        with patch("soak_dispatch.performance.request",side_effect=received):
            row=execute_text("http://127.0.0.1:1",workload,plan,warmup=True)
        self.assertEqual(row["operation_id"],"soak-warmup-fixed-0-0")
        self.assertEqual(row["observed_session"],"soak-warmup-client-0")
