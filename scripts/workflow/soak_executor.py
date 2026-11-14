"""One-request dispatch for the declared four-client soak schedule."""
import hashlib
import re
import threading

from performance import request
from soak_boundary import verify_deferred_boundary
from soak_cancel import backend_evidence, cancel_request
from soak_continuations import execute_continuation
from soak_dispatch import build_text_case, execute_text
from soak_images import execute_image
from soak_lifecycle import lifecycle_step
from soak_lifecycle_state import LifecycleState
from soak_receipts import verify_receipt
from soak_phase import OperationFailure
from soak_rejections import build_rejection_case, execute_rejection
from soak_schedule import plan_request


def _digest(case, mappings, bypass):
    if bypass: return hashlib.sha256(case["body"]).hexdigest()
    originals = set(case["occurrences"])
    if any(not isinstance(mappings.get(value), str) for value in originals):
        raise ValueError("missing boundary mapping")
    replacements = {value.encode(): mappings[value].encode() for value in originals}
    if not replacements: return hashlib.sha256(case["body"]).hexdigest()
    pattern = re.compile(b"|".join(re.escape(value) for value in sorted(replacements, key=len, reverse=True)))
    return hashlib.sha256(pattern.sub(lambda match: replacements[match.group()], case["body"])).hexdigest()


def _failure(row, reason):
    result = dict(row)
    result.update(observed_outcome=row.get("outcome"), observed_completed=row.get("completed"),
                  outcome="scenario_failure", completed=False, evidence_failure=reason)
    return OperationFailure(result)


class ScenarioExecutor:
    def __init__(self, url, upstream_url, workload, mappings, fixtures, directory, *, bypass=False):
        self.url, self.upstream_url = url, upstream_url
        self.workload, self.mappings = workload, mappings
        self.fixtures, self.directory = fixtures, directory
        self.bypass = bypass
        self.state = LifecycleState()
        self.lock = threading.Lock()
        self.rejections = []
        self.forbidden_values = set()

    def _receipt(self, row):
        digest = row.get("boundary_sha256")
        if not isinstance(digest, str) or not re.fullmatch("[0-9a-f]{64}", digest):
            raise _failure(row, "missing_upstream_digest")
        try:
            verify_receipt(backend_evidence(self.upstream_url, "receipt", row["operation_id"], 5), expected_hash=digest)
        except Exception as error:
            raise _failure(row, "upstream_receipt_mismatch") from error
        row["upstream_receipt_verified"] = True
        return row

    def __call__(self, plan):
        phase, client, serial = plan["identity"]
        expected = plan_request(client, serial, phase)
        if any(plan.get(key) != value for key, value in expected.items()):
            raise ValueError("inconsistent scenario plan")
        if plan["category"] != "text" and plan.get("churn_indices") != []:
            raise ValueError("nontext churn assignment")
        operation = f"soak-{phase}-{client}-{serial}"
        if plan["category"] == "text":
            row = execute_text(self.url, self.workload, plan, bypass=self.bypass)
            if not self.bypass:
                case = build_text_case(self.workload, plan)
                if all(value in self.mappings for value in case["occurrences"]):
                    row = verify_deferred_boundary(case, row, self.mappings)
            row = self._receipt(row)
        elif plan["category"] == "image":
            row = execute_image(self.url, self.workload, serial * 4 + client, plan["size_bytes"], self.fixtures,
                                self.directory, self.mappings, f"soak-{phase}-client-{client}", plan["scenario"],
                                bypass=self.bypass, operation_id=operation)
            row = self._receipt(row)
        elif plan["scenario"] in {"W07", "W09", "W10"}:
            row = execute_continuation(self.url, self.workload, plan, self.mappings, self.state, bypass=self.bypass)
            row = self._receipt(row)
        elif plan["scenario"] == "W11":
            variant = lifecycle_step(plan)["stage"]
            case = build_rejection_case(self.workload, serial * 4 + client, plan["size_bytes"], variant)
            case["operation_id"] = operation
            if self.bypass:
                row = request(self.url, case, expected_hash=hashlib.sha256(case["body"]).hexdigest())
                receipt = backend_evidence(self.upstream_url, "receipt", operation, 5)
                verify_receipt(receipt, expected_hash=hashlib.sha256(case["body"]).hexdigest())
                row["bypass_observed_outcome"] = row["outcome"]
                row.update(outcome="bypass_negative_control", upstream_receipt_verified=True)
            else:
                row = execute_rejection(self.url, case, variant, self.upstream_url)
                with self.lock:
                    self.rejections.append(operation)
                    self.forbidden_values.update(case["forbidden_values"])
        else:
            row = self._fault(plan, operation)
        if plan["scenario"] not in {"W11", "W12"}:
            allowed = {"supported_success"}
            if phase == "churn" and plan["category"] == "text" and plan["churn_indices"]:
                allowed.add("boundary_pending")
            if row.get("outcome") not in allowed or row.get("completed") is not True or row.get("semantic_verified") is not True:
                raise _failure(row, "unexpected_supported_operation_result")
        row.update(category=plan["category"], scenario=plan["scenario"])
        return row

    def _fault(self, plan, operation):
        phase, client, serial = plan["identity"]
        stage = lifecycle_step(plan)["stage"]
        case = self.workload.case(serial * 4 + client, plan["size_bytes"])
        case.update(operation_id=operation, benchmark_session=f"soak-{phase}-client-{client}", benchmark_fault=stage)
        digest = _digest(case, self.mappings, self.bypass)
        if stage == "cancel":
            return cancel_request(self.url, case, digest, self.upstream_url)
        row = request(self.url, case, expected_hash=digest)
        expected = {"upstream_error": (503, "upstream_failure"), "malformed_success": (200, "transformation_error")}
        if stage not in expected or (row.get("status"), row.get("outcome")) != expected[stage]:
            raise _failure(row, "unexpected_injected_fault_result")
        verify_receipt(backend_evidence(self.upstream_url, "receipt", operation, 5), expected_hash=digest)
        row.update(expected_failure_verified=True, boundary_verified=True, upstream_receipt_verified=True)
        return row

    def verify_final_absence(self):
        with self.lock: operations = tuple(self.rejections)
        for operation in operations:
            verify_receipt(backend_evidence(self.upstream_url, "receipt", operation, 5), absent=True)
        return {"rejections_rechecked": len(operations), "final_absence_verified": True}
