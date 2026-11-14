"""Resolve private persisted mappings against the exact scheduled wire cases."""
import copy
import hashlib
import json
import math
from fractions import Fraction

from soak_boundary import verify_deferred_boundary
from soak_dispatch import build_text_case
from soak_journal import replay_requests


def _mapping(values):
    if not isinstance(values, dict) or any(not isinstance(key, str) or not key or not isinstance(value, str) or not value for key, value in values.items()):
        raise ValueError("invalid mapping evidence")


def finalize_mapping_evidence(report, workload, initial_mappings, final_mappings):
    _mapping(initial_mappings); _mapping(final_mappings)
    phase = report["phase"]
    replay_requests(report["schedule"], phase, report["duration_s"], drain_seconds=report.get("drain_admission_seconds", 0))
    plans = {tuple(plan["identity"]): plan for plan in report["schedule"]}
    identities = [tuple(entry["identity"]) for entry in report["results"]]
    if len(identities) != len(set(identities)) or set(identities) != set(plans):
        raise ValueError("mapping result identity mismatch")
    indices = [index for plan in report["schedule"] for index in plan["churn_indices"]]
    if sorted(indices) != list(range(len(indices))): raise ValueError("invalid assigned churn sequence")
    values = [workload.churn_value(index) for index in indices]
    if len(set(values)) != len(values) or set(values) & set(initial_mappings):
        raise ValueError("churn values are not new distinct mappings")
    due = math.ceil(Fraction(str(report["duration_s"])) * Fraction(100,60)) if phase == "churn" else 0
    if len(values) > due: raise ValueError("churn exceeds declared horizon")
    changed = sum(final_mappings.get(key) != value for key,value in initial_mappings.items())
    missing = sum(not final_mappings.get(value) or final_mappings.get(value) == value for value in values)
    unexpected = len(set(final_mappings) - set(initial_mappings) - set(values))
    result = copy.deepcopy(report)
    unverified = 0
    for entry in result["results"]:
        plan = plans[tuple(entry["identity"])]
        if plan["category"] != "text": continue
        observed = entry["request"]
        if observed.get("outcome") in {"supported_success", "boundary_pending"}:
            observed = dict(observed, outcome="boundary_pending")
            entry["request"] = verify_deferred_boundary(build_text_case(workload, plan), observed, final_mappings)
        if entry["request"].get("outcome") != "supported_success" or entry["request"].get("boundary_verified") is not True:
            unverified += 1
    unmet = []
    for failed, code in [(changed, "seed_mapping_changed"), (missing, "missing_churn_mapping"), (unexpected, "unexpected_new_mapping"), (due-len(values), "churn_backlog"), (unverified, "unverified_text_boundary")]:
        if failed: unmet.append(code)
    digest = hashlib.sha256(json.dumps(final_mappings, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    result["mapping_evidence"] = {"initial_pairs": len(initial_mappings), "final_pairs": len(final_mappings),
        "assigned_churn_pairs": len(values), "persisted_churn_pairs": len(values)-missing,
        "due_pairs": due, "unassigned_due_pairs": due-len(values), "changed_seed_mappings": changed,
        "unexpected_new_mappings": unexpected, "unverified_text_boundaries": unverified,
        "final_mappings_sha256": digest, "unmet_requirements": unmet}
    return result
