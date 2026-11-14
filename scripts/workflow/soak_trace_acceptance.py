"""Original sustained trace, semantic outcome, and mapping-count acceptance."""
from collections import Counter
import math
import re

from soak_journal import replay_requests
from soak_window_acceptance import _index


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def verify_semantic_outcome(plan, row, mode):
    if mode not in ("proxy", "bypass") or not isinstance(row, dict):
        raise ValueError("trace_outcome_shape")
    scenario = plan["scenario"]
    if scenario == "W11":
        if row.get("completed") is not False:
            raise ValueError("trace_negative_claimed_completion")
        if mode == "proxy":
            if (row.get("outcome") != "expected_rejection"
                    or row.get("expected_failure_verified") is not True
                    or row.get("boundary_verified") is not True
                    or type(row.get("upstream_arrivals")) is not int or row["upstream_arrivals"] != 0):
                raise ValueError("trace_rejection_evidence")
        elif (row.get("outcome") != "bypass_negative_control"
              or row.get("upstream_receipt_verified") is not True
              or not isinstance(row.get("bypass_observed_outcome"), str) or not row["bypass_observed_outcome"]):
            raise ValueError("trace_direct_negative_evidence")
    elif scenario == "W12":
        if (row.get("outcome") not in ("upstream_failure", "transformation_error", "cancelled")
                or type(row.get("completed")) is not bool
                or any(row.get(key) is not True for key in
                       ("expected_failure_verified", "boundary_verified", "upstream_receipt_verified"))):
            raise ValueError("trace_fault_evidence")
        if row["outcome"] == "cancelled" and (row.get("upstream_released") is not True
                or not _number(row.get("upstream_release_observed_ms"))
                or not 0 <= row["upstream_release_observed_ms"] <= 45000):
            raise ValueError("trace_cancellation_release")
    elif (row.get("outcome") != "supported_success" or any(row.get(key) is not True for key in
              ("completed", "semantic_verified", "boundary_verified", "upstream_receipt_verified"))):
        raise ValueError("trace_supported_correctness")


def _image_proof(row):
    proof = row.get("fixture_evidence")
    if not isinstance(proof, list) or len(proof) != 2:
        raise ValueError("trace_image_proof")
    identifiers = set()
    for item in proof:
        if (not isinstance(item, dict) or set(item) != {"id", "sha256"}
                or not isinstance(item["id"], str) or not item["id"] or item["id"] in identifiers
                or not isinstance(item["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", item["sha256"])):
            raise ValueError("trace_image_proof")
        identifiers.add(item["id"])
    return proof


def evaluate_phase_trace(proxy, bypass):
    plans, proxy_rows = _index(proxy)
    other_plans, bypass_rows = _index(bypass)
    phase = proxy["phase"]
    if (bypass["phase"] != phase or proxy.get("replay") is not False or bypass.get("replay") is not True
            or proxy.get("failures") != [] or bypass.get("failures") != []
            or proxy["schedule"] != bypass["schedule"]
            or set(plans) != set(other_plans) or set(proxy_rows) != set(bypass_rows)):
        raise ValueError("trace_pair_correspondence")
    clients = replay_requests(proxy["schedule"], phase, duration=7200, drain_seconds=45 if phase == "churn" else 0)
    categories, complete_categories, scenarios, image_ids = Counter(), Counter(), set(), set()
    supported_work_categories, drain_requests = Counter(), 0
    for client in range(4):
        rows = clients[client]
        if len(rows) < 100:
            raise ValueError("trace_client_cycle_missing")
        complete = len(rows)//100*100
        complete_categories.update(plan["category"] for plan in rows[:complete])
    total_complete = sum(complete_categories.values())
    if any(complete_categories[key]*100 != total_complete*share
           for key, share in (("text", 70), ("image", 20), ("lifecycle", 10))):
        raise ValueError("trace_original_mix")
    assigned = []
    for identity, plan in plans.items():
        categories[plan["category"]] += 1
        if plan.get("drain", False):
            drain_requests += 1
        else:
            supported_work_categories[plan["category"]] += 1
        scenarios.add(plan["scenario"])
        assigned.extend(plan["churn_indices"])
        for mode, rows in (("proxy", proxy_rows), ("bypass", bypass_rows)):
            row = rows[identity]
            if (row.get("category") != plan["category"] or row.get("scenario") != plan["scenario"]
                    or not _number(row.get("duration_ms")) or not 0 <= row["duration_ms"] <= 45000):
                raise ValueError("trace_request_budget_or_identity")
            verify_semantic_outcome(plan, row, mode)
        if proxy_rows[identity]["operation_id"] != bypass_rows[identity]["operation_id"]:
            raise ValueError("trace_operation_correspondence")
        if plan["category"] == "image":
            proof = _image_proof(proxy_rows[identity])
            if proof != _image_proof(bypass_rows[identity]):
                raise ValueError("trace_image_correspondence")
            image_ids.update(item["id"] for item in proof)
    if scenarios != {f"W{i:02}" for i in range(1, 13)}:
        raise ValueError("trace_scenario_missing")
    due = 12000 if phase == "churn" else 0
    if (any(type(value) is not int for value in assigned) or len(assigned) != due
            or sorted(assigned) != list(range(due))):
        raise ValueError("trace_churn_assignment")
    mapping = proxy.get("mapping_evidence")
    expected = dict(initial_pairs=10000, final_pairs=10000+due, assigned_churn_pairs=due,
                    persisted_churn_pairs=due, due_pairs=due, unassigned_due_pairs=0,
                    changed_seed_mappings=0, unexpected_new_mappings=0, unverified_text_boundaries=0)
    if (not isinstance(mapping, dict) or mapping.get("unmet_requirements") != []
            or any(type(mapping.get(key)) is not int or mapping[key] != value for key, value in expected.items())
            or not isinstance(mapping.get("final_mappings_sha256"), str)
            or not re.fullmatch(r"[0-9a-f]{64}", mapping["final_mappings_sha256"])):
        raise ValueError("trace_persisted_mapping_evidence")
    return dict(state="pass", phase=phase, category_counts=dict(categories),
                complete_cycle_categories=dict(complete_categories), scenario_count=len(scenarios),
                supported_work_categories=dict(supported_work_categories), drain_requests=drain_requests,
                distinct_image_ids=len(image_ids), assigned_churn_pairs=len(assigned),
                performance_qualified=False)
