"""Original fifteen-minute matched text gates; not a whole-campaign verdict."""
import math

from performance import compare_runs
from soak_schedule import plan_request

SIZES = (262144, 786432, 1048576)


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def _identity(value, phase):
    if (not isinstance(value, (tuple, list)) or len(value) != 3 or value[0] != phase
            or type(value[1]) is not int or value[1] not in range(4)
            or type(value[2]) is not int or value[2] < 0):
        raise ValueError("invalid phase request identity")
    return tuple(value)


def _index(report):
    if not isinstance(report, dict) or report.get("phase") not in ("fixed", "churn"):
        raise ValueError("invalid phase report")
    phase = report["phase"]
    if (report.get("completed") is not True or not _number(report.get("duration_s"))
            or report["duration_s"] != 7200 or not _number(report.get("elapsed_s"))
            or report["elapsed_s"] < 7200 or not _number(report.get("started_s"))):
        raise ValueError("original complete two-hour phase required")
    if not isinstance(report.get("schedule"), list) or not isinstance(report.get("results"), list):
        raise ValueError("phase trace and outcomes required")
    plans = {}
    for plan in report["schedule"]:
        if not isinstance(plan, dict):
            raise ValueError("invalid schedule entry")
        key = _identity(plan.get("identity"), phase)
        canonical = plan_request(client=key[1], serial=key[2], phase=phase)
        if any(plan.get(field) != canonical[field] for field in ("category", "scenario", "size_bytes")):
            raise ValueError("schedule differs from original deterministic workload")
        if key in plans or not _number(plan.get("elapsed_s")) or plan["elapsed_s"] < 0:
            raise ValueError("duplicate identity or invalid admission time")
        if type(plan.get("drain", False)) is not bool:
            raise ValueError("invalid drain marker")
        if plan.get("drain", False) != (plan["elapsed_s"] >= 7200):
            raise ValueError("drain marker does not match measured horizon")
        if plan.get("drain", False) and phase != "churn":
            raise ValueError("fixed phase cannot admit drain work")
        plans[key] = plan
    rows, operations = {}, set()
    for entry in report["results"]:
        if not isinstance(entry, dict) or not isinstance(entry.get("request"), dict):
            raise ValueError("invalid phase outcome")
        key = _identity(entry.get("identity"), phase)
        row = entry["request"]
        operation = row.get("operation_id")
        if key not in plans or key in rows or not isinstance(operation, str) or not operation or operation in operations:
            raise ValueError("duplicate or unmatched outcome")
        operations.add(operation)
        if plans[key]["category"] == "text":
            if (row.get("outcome") != "supported_success" or row.get("category") != "text"
                    or row.get("scenario") != plans[key]["scenario"]
                    or any(row.get(field) is not True for field in
                           ("supported", "completed", "semantic_verified", "boundary_verified",
                            "upstream_receipt_verified"))):
                raise ValueError("text outcome lacks complete correctness evidence")
            for field in ("first_byte_ms", "duration_ms"):
                if not _number(row.get(field)) or row[field] < 0:
                    raise ValueError("invalid text timing")
            for field in ("prefix_ms", "relay_ms", "relay_flush_ms"):
                if (not isinstance(row.get(field), list) or len(row[field]) != 32
                        or any(not _number(value) for value in row[field])):
                    raise ValueError("missing or invalid logical-prefix timing")
        rows[key] = row
    if set(rows) != set(plans):
        raise ValueError("schedule and outcomes are not a bijection")
    return plans, rows


def evaluate_text_windows(proxy, bypass):
    """Evaluate every size in every original window using matched differences.

    Window membership uses the proxy trace's admission time. Drain work remains
    counted separately. Phase identity, exact replay timing, resource/extraction
    windows, mix and controls also need campaign-level verification.
    """
    pplans, prows = _index(proxy)
    bplans, brows = _index(bypass)
    if proxy["phase"] != bypass["phase"] or set(pplans) != set(bplans):
        raise ValueError("proxy and bypass request identities differ")
    for key, plan in pplans.items():
        other = bplans[key]
        if (any(plan.get(field) != other.get(field) for field in ("category", "scenario", "size_bytes"))
                or tuple(plan.get("churn_indices", ())) != tuple(other.get("churn_indices", ()))
                or plan.get("drain", False) != other.get("drain", False)
                or prows[key]["operation_id"] != brows[key]["operation_id"]):
            raise ValueError("proxy and bypass logical requests differ")
    windows = []
    states = []
    drain_count = sum(plan["category"] == "text" and plan.get("drain", False) for plan in pplans.values())
    for window in range(8):
        sizes = []
        for size in SIZES:
            keys = [key for key, plan in pplans.items() if plan["category"] == "text"
                    and not plan.get("drain", False) and plan["size_bytes"] == size
                    and window * 900 <= plan["elapsed_s"] < (window + 1) * 900]
            comparison = compare_runs([prows[key] for key in keys], [brows[key] for key in keys])
            if ({key[1] for key in keys} != set(range(4))
                    or comparison.get("paired_samples_valid") is not True
                    or comparison.get("relay_samples_valid") is not True):
                state = "incomplete"
            else:
                first = comparison["added_first_byte_ms"]
                relay = comparison["added_relay_ms"]
                meets = (first["p95"] <= 150 and first["p99"] <= 500 and relay["p99"] <= 100
                         and all(prows[key]["duration_ms"] <= 45000 and brows[key]["duration_ms"] <= 45000
                                 for key in keys))
                state = "pass" if meets else "fail"
            states.append(state)
            sizes.append({"size_bytes": size, "paired_requests": len(keys), "state": state, **comparison})
        windows.append({"start_s": window * 900, "duration_s": 900, "sizes": sizes})
    state = "incomplete" if "incomplete" in states else "fail" if "fail" in states else "pass"
    return {"state": state, "windows": windows, "drain_text_requests": drain_count,
            "performance_qualified": False}
