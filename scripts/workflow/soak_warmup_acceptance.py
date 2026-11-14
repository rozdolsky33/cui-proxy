"""Original warmup duration, correctness, and namespace acceptance."""
from collections import Counter
import math

from soak_schedule import plan_request
from soak_trace_acceptance import _image_proof, verify_semantic_outcome


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def evaluate_warmup(report, *, mode, measured_operation_ids=frozenset(), measured_image_ids=frozenset()):
    if (mode not in ("proxy", "bypass") or not isinstance(report, dict)
            or report.get("completed") is not True or report.get("cancelled") is not False
            or report.get("failures") != [] or not isinstance(report.get("results"), list)):
        raise ValueError("warmup_incomplete")
    elapsed, minimum = report.get("elapsed_seconds"), report.get("minimum_seconds")
    count, required = report.get("successful"), report.get("minimum_count")
    if (not _number(elapsed) or not _number(minimum) or minimum < 60 or elapsed < minimum
            or type(required) is not int or required < 100
            or type(count) is not int or count < required or count != len(report["results"])):
        raise ValueError("warmup_original_thresholds")
    declared = report.get("client_successes")
    if not isinstance(declared, list) or len(declared) != 4 or any(type(n) is not int or n < 1 for n in declared):
        raise ValueError("warmup_clients")
    if (any(not isinstance(value, str) for value in measured_operation_ids)
            or any(not isinstance(value, str) for value in measured_image_ids)):
        raise ValueError("warmup_measured_namespace")
    measured_operations, measured_images = set(measured_operation_ids), set(measured_image_ids)
    successes, serials = [0]*4, [0]*4
    operations, images, categories = set(), set(), Counter()
    for entry in report["results"]:
        if not isinstance(entry, dict) or not isinstance(entry.get("request"), dict) or not isinstance(entry.get("result"), dict):
            raise ValueError("warmup_result_shape")
        plan, row = entry["request"], entry["result"]
        identity = plan.get("identity")
        if (not isinstance(identity, (list, tuple)) or len(identity) != 3 or identity[0] != "fixed"
                or type(identity[1]) is not int or not 0 <= identity[1] < 4 or type(identity[2]) is not int):
            raise ValueError("warmup_plan_identity")
        client = identity[1]
        while True:
            expected = plan_request(client=client, serial=serials[client], phase="fixed")
            serials[client] += 1
            if expected["category"] in ("text", "image"):
                break
        if (tuple(identity) != tuple(expected["identity"])
                or any(plan.get(key) != expected[key] for key in ("category", "scenario", "size_bytes"))
                or plan.get("churn_indices") != []):
            raise ValueError("warmup_original_schedule")
        if (row.get("category") != plan["category"] or row.get("scenario") != plan["scenario"]
                or not _number(row.get("duration_ms")) or not 0 <= row["duration_ms"] <= 45000):
            raise ValueError("warmup_request_evidence")
        verify_semantic_outcome(plan, row, mode)
        operation = row.get("operation_id")
        if not isinstance(operation, str) or not operation or operation in operations or operation in measured_operations:
            raise ValueError("warmup_operation_namespace")
        operations.add(operation)
        if plan["category"] == "image":
            identifiers = {item["id"] for item in _image_proof(row)}
            if identifiers & measured_images:
                raise ValueError("warmup_image_namespace")
            images.update(identifiers)
        categories[plan["category"]] += 1
        successes[client] += 1
    if successes != declared or sum(successes) != count or categories["image"] == 0 or categories["text"] == 0:
        raise ValueError("warmup_coverage")
    return dict(state="pass", successful=count, elapsed_seconds=elapsed, client_successes=successes,
                category_counts=dict(categories), image_operations=categories["image"],
                distinct_image_ids=len(images), performance_qualified=False)
