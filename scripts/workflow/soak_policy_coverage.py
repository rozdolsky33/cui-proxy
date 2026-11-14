"""Reconcile policy-generation evidence against every proxy request identity."""
import re
from soak_reload import verify_policy_records


def verify_policy_coverage(records, rows, generations):
    by_id = {}
    for row in rows:
        identifier = row.get("server_request_id") or row.get("diagnostic", {}).get("request_id")
        if not isinstance(identifier, str) or not re.fullmatch("[0-9a-f]{16}", identifier) or identifier in by_id:
            raise ValueError("invalid or duplicate proxy request identity")
        by_id[identifier] = row
    identifiers = [record.get("request_id") for record in records]
    if not rows or len(identifiers) != len(set(identifiers)) or set(identifiers) != set(by_id):
        raise ValueError("audit identity coverage mismatch")
    matching = []
    old_tokens = shape_rejections = 0
    for record in records:
        if record.get("rule_ids"):
            matching.append(record)
            continue
        if record.get("rule_ids") != []: raise ValueError("missing rule evidence")
        row = by_id[record["request_id"]]
        if row.get("outcome") == "supported_success" and all(row.get(key) is True for key in ("old_substitutions_only", "completed", "semantic_verified", "boundary_verified")):
            old_tokens += 1
        elif (row.get("outcome") == "expected_rejection" and row.get("scenario") == "W11"
              and row.get("expected_failure_verified") is True and row.get("boundary_verified") is True
              and type(row.get("upstream_arrivals")) is int and row["upstream_arrivals"] == 0
              and row.get("diagnostic", {}).get("reason") == "sanitize"
              and row.get("diagnostic", {}).get("kind") == "bad_body"):
            shape_rejections += 1
        else:
            raise ValueError("unexplained empty policy decision")
    policy = verify_policy_records(matching, generations)
    return {"audit_records": len(records), "rule_observation_records": len(matching),
            "old_token_records_without_matches": old_tokens,
            "shape_rejections_without_matches": shape_rejections, "policy": {str(generation): count for generation, count in policy.items()}}
