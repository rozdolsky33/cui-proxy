import hashlib
import unittest
from unittest.mock import patch

from soak_schedule import plan_request
import soak_executor as executor


class ExecutorTests(unittest.TestCase):
    def runner(self, bypass=False):
        return executor.ScenarioExecutor("http://127.0.0.1:1", "http://127.0.0.1:2", object(), {}, [], ".", bypass=bypass)

    def plan(self, serial):
        plan = plan_request(0, serial, "fixed")
        plan["churn_indices"] = []
        return plan

    def test_text_keeps_pending_boundary_and_verifies_unique_arrival(self):
        row = {"outcome": "boundary_pending", "boundary_sha256": "a" * 64, "operation_id": "text-1"}
        row.update(completed=True, semantic_verified=True)
        with patch("soak_executor.execute_text", return_value=row), patch("soak_executor.verify_deferred_boundary", return_value=row), patch("soak_executor.build_text_case", return_value={"occurrences": []}), patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": "a" * 64}):
            result = self.runner()(self.churn_plan())
        self.assertEqual(result["outcome"], "boundary_pending")
        self.assertTrue(result["upstream_receipt_verified"])
        self.assertEqual(result["scenario"], "W01")

    def test_wrong_arrival_digest_cannot_pass(self):
        row = {"outcome": "supported_success", "boundary_sha256": "a" * 64, "operation_id": "text-1"}
        with patch("soak_executor.execute_text", return_value=row), patch("soak_executor.verify_deferred_boundary", return_value=row), patch("soak_executor.build_text_case", return_value={"occurrences": []}), patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": "b" * 64}):
            with self.assertRaises(ValueError): self.runner()(self.plan(0))

    def test_negative_receives_unique_phase_client_identity(self):
        case = {"body": b"{}", "operation_id": "old", "forbidden_values": []}
        with patch("soak_executor.build_rejection_case", return_value=case), patch("soak_executor.execute_rejection", return_value={"outcome": "expected_rejection"}) as execute:
            result = self.runner()(self.plan(39))
        self.assertEqual(execute.call_args.args[1]["operation_id"], "soak-fixed-0-39")
        self.assertEqual(execute.call_args.args[2], "blocked")
        self.assertEqual(result["scenario"], "W11")

    def test_fault_success_is_rejected_instead_of_counted_supported(self):
        runner = self.runner()
        case = {"body": b"{}", "occurrences": [], "operation_id": "fault"}
        runner.workload = unittest.mock.Mock()
        runner.workload.case.return_value = case
        with patch("soak_executor.request", return_value={"outcome": "supported_success", "completed": True, "status": 200}):
            with self.assertRaises(ValueError): runner(self.plan(49))

    def test_wrong_plan_is_rejected_before_request(self):
        plan = self.plan(0); plan["scenario"] = "W12"
        with patch("soak_executor.execute_text") as execute:
            with self.assertRaises(ValueError): self.runner()(plan)
        execute.assert_not_called()

    def test_image_operation_override_is_sent_to_request(self):
        from soak_images import execute_image
        with patch("soak_images.build_image_case", return_value=({"body": b"{}", "occurrences": [], "operation_id": "old"}, b"{}")), patch("soak_images.performance.request", return_value={"outcome": "supported_success", "completed": True}) as send:
            execute_image("http://127.0.0.1:1", object(), 0, 262144, [], ".", {}, "session", "W05", operation_id="soak-fixed-0-7")
        self.assertEqual(send.call_args.args[1]["operation_id"], "soak-fixed-0-7")

    def test_immediate_boundary_verification_retains_digest_for_receipt(self):
        from unittest.mock import MagicMock
        from performance import request
        digest = hashlib.sha256(b"{}").hexdigest()
        response = MagicMock(status=200)
        response.getheader.side_effect = lambda name, *args: digest if name == "X-Benchmark-Body-SHA256" else None
        response.read1.return_value = b""
        connection = MagicMock()
        connection.getresponse.return_value = response
        observer = MagicMock(prefix_ms=[], send_started_s=[], send_finished_s=[])
        observer.valid.return_value = True
        case = {"body": b"{}", "occurrences": [], "operation_id": "immediate"}
        with patch("performance.http.client.HTTPConnection", return_value=connection):
            row = request("http://127.0.0.1:1", case, expected_hash=digest, observer_factory=lambda: observer)
        self.assertEqual(row["boundary_sha256"], digest)

    def test_received_error_body_can_complete_without_becoming_success(self):
        from unittest.mock import Mock
        runner = self.runner(); runner.workload = Mock()
        runner.workload.case.return_value = {"body": b"{}", "occurrences": []}
        digest = hashlib.sha256(b"{}").hexdigest()
        with patch("soak_executor.request", return_value={"outcome": "upstream_failure", "completed": True, "status": 503}), patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": digest}):
            result = runner(self.plan(49))
        self.assertEqual(result["outcome"], "upstream_failure")
        self.assertTrue(result["expected_failure_verified"])
        self.assertTrue(result["completed"])

    def test_receipt_failure_retains_the_observed_request(self):
        from soak_phase import OperationFailure
        row = {"outcome": "supported_success", "completed": True, "status": 200, "operation_id": "recorded", "boundary_sha256": "a" * 64}
        with patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 0, "body_sha256": None}):
            with self.assertRaises(OperationFailure) as caught:
                self.runner()._receipt(row)
        self.assertEqual(caught.exception.result["observed_outcome"], "supported_success")
        self.assertEqual(caught.exception.result["status"], 200)
        self.assertEqual(caught.exception.result["outcome"], "scenario_failure")

    def test_nontext_plan_cannot_silently_drop_assigned_churn(self):
        plan = self.plan(7); plan["churn_indices"] = [0]
        with patch("soak_executor.execute_image") as execute:
            with self.assertRaisesRegex(ValueError, "nontext churn assignment"):
                self.runner()(plan)
        execute.assert_not_called()

    def test_proxy_request_id_is_retained_for_audit_reconciliation(self):
        from unittest.mock import MagicMock
        from performance import request
        response = MagicMock(status=200)
        response.getheader.side_effect = lambda name, *args: "a" * 16 if name == "X-Cui-Proxy-Request-Id" else None
        response.read1.return_value = b""
        connection = MagicMock(); connection.getresponse.return_value = response
        observer = MagicMock(prefix_ms=[], send_started_s=[], send_finished_s=[]); observer.valid.return_value = True
        with patch("performance.http.client.HTTPConnection", return_value=connection):
            row = request("http://127.0.0.1:1", {"body": b"{}", "occurrences": [], "operation_id": "audit"}, observer_factory=lambda: observer)
        self.assertEqual(row["server_request_id"], "a" * 16)
        import json
        import tempfile
        from pathlib import Path
        from performance import write_report
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'report.json'
            write_report(path, {'measurements': [{'records': [row]}]})
            retained = json.loads(path.read_text())['measurements'][0]['records'][0]
            self.assertEqual(retained['server_request_id'], row['server_request_id'])

    def test_new_churn_mapping_remains_pending_until_persisted_snapshot(self):
        row = {"outcome": "boundary_pending", "boundary_sha256": "a" * 64, "operation_id": "new"}
        row.update(completed=True, semantic_verified=True)
        with patch("soak_executor.execute_text", return_value=row), patch("soak_executor.build_text_case", return_value={"occurrences": ["not-yet-inspected"]}), patch("soak_executor.verify_deferred_boundary") as verify, patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": "a" * 64}):
            result = self.runner()(self.churn_plan())
        self.assertEqual(result["outcome"], "boundary_pending")
        verify.assert_not_called()

    def test_unexpected_semantic_failure_stops_before_long_run_continues(self):
        from soak_phase import OperationFailure
        row = {"outcome": "supported_success", "completed": True, "semantic_verified": False, "boundary_sha256": "a" * 64, "operation_id": "bad-semantics", "status": 200}
        with patch("soak_executor.execute_text", return_value=row), patch("soak_executor.build_text_case", return_value={"occurrences": []}), patch("soak_executor.verify_deferred_boundary", return_value=row), patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": "a" * 64}):
            with self.assertRaises(OperationFailure) as caught: self.runner()(self.plan(0))
        self.assertEqual(caught.exception.result["observed_outcome"], "supported_success")
        self.assertFalse(caught.exception.result["semantic_verified"])

    def test_pending_boundary_is_not_allowed_without_assigned_churn(self):
        from soak_phase import OperationFailure
        row = {"outcome": "boundary_pending", "completed": True, "semantic_verified": True, "boundary_sha256": "a" * 64, "operation_id": "missing-seed"}
        with patch("soak_executor.execute_text", return_value=row), patch("soak_executor.build_text_case", return_value={"occurrences": ["missing-seed"]}), patch("soak_executor.backend_evidence", return_value={"complete": True, "count": 1, "body_sha256": "a" * 64}):
            with self.assertRaises(OperationFailure): self.runner()(self.plan(0))

    def churn_plan(self):
        plan = plan_request(0, 0, "churn"); plan["churn_indices"] = [0]; return plan

    def test_verified_direct_boundary_satisfies_bypass_warmup(self):
        from soak_warmup import run_warmup
        runner = self.runner(bypass=True)
        digest = hashlib.sha256(b"direct-boundary").hexdigest()
        def execute(plan):
            row = dict(boundary_sha256=digest, operation_id=str(plan["identity"]),
                       outcome="supported_success", completed=True, semantic_verified=True)
            return runner._receipt(row)
        with patch.object(executor, "verify_receipt", return_value=True), patch.object(executor, "backend_evidence", return_value={}):
            report = run_warmup(execute, minimum_seconds=.001, minimum_count=4)
        self.assertTrue(report["completed"])

    def test_proxy_receipt_does_not_promote_pending_sanitization_boundary(self):
        runner = self.runner()
        row = dict(boundary_sha256=hashlib.sha256(b"proxy-boundary").hexdigest(),
                   operation_id="pending", boundary_pending=True)
        with patch.object(executor, "verify_receipt", return_value=True), patch.object(executor, "backend_evidence", return_value={}):
            result = runner._receipt(row)
        self.assertIsNot(result.get("boundary_verified"), True)
