"""Require complete measured image timing and explain polling gaps by restarts."""

import math


def _number(value):
    return (isinstance(value, (int, float)) and not isinstance(value, bool)
            and math.isfinite(value))


def evaluate_stage_coverage(records, *, expected_images, started_s, ended_s,
                            poll_errors, restart_intervals):
    if (type(expected_images) is not int or expected_images < 1
            or not _number(started_s) or not _number(ended_s) or ended_s <= started_s
            or not isinstance(records, list) or not isinstance(poll_errors, list)):
        raise ValueError("soak_stage_coverage_shape")
    intervals = []
    for interval in restart_intervals:
        if (not isinstance(interval, (list, tuple)) or len(interval) != 2
                or not all(_number(value) for value in interval)
                or interval[1] < interval[0]):
            raise ValueError("soak_stage_coverage_restart")
        intervals.append(interval)
    for i, error in enumerate(poll_errors):
        if (not isinstance(error, dict) or not _number(error.get("observed_s"))
                or type(error.get("poll_errors")) is not int or error["poll_errors"] != i + 1
                or not any(lo <= error["observed_s"] <= hi for lo, hi in intervals)):
            raise ValueError("soak_stage_coverage_poll_error")
    seen, extraction_count, before_count = set(), 0, 0
    for row in records:
        if not isinstance(row, dict) or not isinstance(row.get("stages_ms"), dict):
            raise ValueError("soak_stage_coverage_record")
        stages = row["stages_ms"]
        if "extraction_run" not in stages:
            continue
        lower, upper = row.get("observation_lower_s"), row.get("observed_s")
        if not _number(upper):
            raise ValueError("soak_stage_coverage_observation")
        # Priming observations before the first measured phase may include warmup.
        if upper < started_s:
            before_count += 1
            continue
        if (not _number(lower) or lower < started_s or upper < lower
                or upper > ended_s + 1):
            raise ValueError("soak_stage_coverage_observation")
        identity = row.get("identity")
        if (not isinstance(identity, dict) or not isinstance(identity.get("session"), str)
                or not identity["session"] or not isinstance(row.get("time"), str) or not row["time"]):
            raise ValueError("soak_stage_coverage_identity")
        key = (identity["session"], row["time"])
        if key in seen:
            raise ValueError("soak_stage_coverage_duplicate")
        seen.add(key)
        run, wait = stages["extraction_run"], stages.get("extraction_wait")
        if (not _number(run) or run < 0 or not _number(wait) or wait < 0
                or run + wait > 30000):
            raise ValueError("soak_stage_coverage_budget")
        extraction_count += 1
    if extraction_count != expected_images:
        raise ValueError("soak_stage_coverage_count")
    return {"state": "pass", "extraction_records": extraction_count,
            "warmup_records_excluded": before_count, "restart_poll_errors": len(poll_errors),
            "performance_qualified": False}
