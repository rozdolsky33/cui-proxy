"""Actual continuation requests with verified, client-isolated dependencies."""

import base64
import hashlib
import json
import re

import performance
from performance_workload import encode
from soak_boundary import verify_deferred_boundary
from soak_lifecycle import lifecycle_step


def build_continuation_case(workload, plan, mappings, previous):
    step = lifecycle_step(plan)
    phase, client, scenario, number = step["workflow"]
    if scenario not in ("W07", "W09", "W10"):
        raise ValueError("soak_continuation_scenario")
    base = workload.case(number * 4 + client, plan["size_bytes"])
    protected = base["occurrences"]
    if previous and (previous[0]["workflow"] != step["workflow"] or previous[0]["protected"] != protected):
        raise ValueError("soak_continuation_context")
    old_mapping = previous[0]["mapping"] if previous else {value: mappings[value] for value in set(protected)}
    raw = base["body"]
    existing = scenario == "W10" and step["stage"] == "continue"
    if existing:
        replacements = {value.encode(): old_mapping[value].encode() for value in set(protected)}
        pattern = re.compile(b"|".join(re.escape(value) for value in sorted(replacements, key=len, reverse=True)))
        raw = pattern.sub(lambda match: replacements[match.group()], raw)
    body = json.loads(raw)
    tag = f"workflow-{phase}-{client}-{scenario}-{number}-{step['stage']}"
    if scenario == "W07" and previous:
        dependencies = previous[1:] if step["stage"] == "parent_collect" else previous[:1]
        for prior in dependencies:
            body["input"].append({"type": "function_call_output", "call_id": prior["tag"], "output": prior["tag"]})
    elif previous:
        body["input"].append({"role": "assistant", "content": [{"type": "output_text", "text": previous[-1]["tag"]}]})
    if scenario == "W09" and step["stage"] == "compaction":
        body["input"].append({"type": "compaction", "encrypted_content": base64.b64encode(b"opaque-fixture-history").decode()})
    body["input"].append({"role": "user", "content": [{"type": "input_text", "text": tag}]})
    values = performance.values_from_body(body)
    if existing:
        reverse = {value: original for original, value in old_mapping.items()}
        values = [reverse.get(value, value) for value in values]
    session = f"soak-client-{client}"
    if scenario == "W07" and step["stage"].startswith("child_"):
        session += "-" + step["stage"]
    case = dict(base, body=encode(body), occurrences=values,
                protected_occurrences=[] if existing else list(protected),
                benchmark_existing_substitutions=existing, benchmark_session=session,
                operation_id=f"{tag}-{plan['identity'][2]}")
    context = {"workflow": step["workflow"], "protected": list(protected),
               "mapping": dict(old_mapping), "tag": tag}
    return case, context


def execute_continuation(url, workload, plan, mappings, state, *, bypass=False):
    previous = state.begin(plan)
    try:
        case, context = build_continuation_case(workload, plan, mappings, previous)
        direct = bypass or case["benchmark_existing_substitutions"]
        if bypass:
            case = dict(case, occurrences=performance.values_from_body(json.loads(case["body"])))
        row = performance.request(url, case,
                                  expected_hash=hashlib.sha256(case["body"]).hexdigest() if direct else None,
                                  defer_boundary=not direct)
        row["semantic_verified"] = row.get("completed") is True and row.get("outcome") in ("supported_success", "boundary_pending")
        if direct:
            row["boundary_verified"] = row.get("outcome") == "supported_success"
        else:
            boundary_case = dict(case, occurrences=case["protected_occurrences"])
            row = verify_deferred_boundary(boundary_case, row, mappings)
        state.finish(plan, row, context)
    except Exception:
        state.finish(plan, {"outcome": "harness_failure"}, {})
        raise
    step = lifecycle_step(plan)
    row.update(scenario=plan["scenario"], category="lifecycle", stage=step["stage"], workflow=step["workflow"],
               old_substitutions_only=case["benchmark_existing_substitutions"])
    return row
