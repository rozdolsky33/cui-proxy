"""Reconstruct scheduled normal-policy text requests and execute their oracles."""

import hashlib

import performance
from soak_cases import build_tool_case, inject_churn
from soak_operations import run_tool_operation
from soak_schedule import plan_request


def build_text_case(workload, plan, *, warmup=False):
    prefix = "soak-warmup" if warmup else "soak"
    phase, client, serial = plan["identity"]
    expected = plan_request(client, serial, phase)
    if expected["category"] != "text" or any(plan.get(key) != value for key, value in expected.items() if key != "identity"):
        raise ValueError("soak_text_schedule")
    indices = plan["churn_indices"]
    if not isinstance(indices, list) or (phase == "fixed" and indices):
        raise ValueError("soak_text_churn")
    size = expected["size_bytes"]
    case = workload.case(serial * 4 + client, size)
    if indices:
        case = inject_churn(case, workload, indices, size)
    identity = f"{prefix}-{phase}-{client}-{serial}"
    if expected["scenario"] in ("W02", "W03", "W04"):
        case = build_tool_case(case, expected["scenario"], identity, size)
    return dict(case, operation_id=identity, benchmark_session=f"{prefix}-client-{client}")


def execute_text(url, workload, plan, *, bypass=False, warmup=False):
    case = build_text_case(workload, plan, warmup=warmup)
    expected_hash = hashlib.sha256(case["body"]).hexdigest() if bypass else None
    if plan["scenario"] in ("W02", "W03", "W04"):
        row = run_tool_operation(url, case, expected_hash, defer_boundary=not bypass)
    else:
        row = performance.request(url, case, expected_hash, defer_boundary=not bypass)
        row["semantic_verified"] = (row.get("completed") is True
                                    and row.get("outcome") in ("supported_success", "boundary_pending"))
    row.update(scenario=plan["scenario"], category="text", size_bytes=plan["size_bytes"])
    return row
