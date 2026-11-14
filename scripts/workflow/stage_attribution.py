"""Value-free stage attribution for the normal text/image mixed workload."""
import math

from observation import STAGES


def workload_stages(recent, warm_sessions, text_count, image_count):
    """Keep observed zero separate from missing stages; fail on incomplete coverage.

    The collector identifies completed requests by session and timestamp. Only
    image requests observe extraction stages, including cache hits. This is a
    workload-specific classification, not a general production request label.
    """
    def invalid():
        raise ValueError("stage_coverage_invalid")

    if type(recent) is not list:
        invalid()
    if any(type(count) is not int or count < 0 for count in (text_count, image_count)):
        invalid()
    groups = {
        kind: {"requests": 0, "samples_ms": {stage: [] for stage in STAGES}}
        for kind in ("text_requests", "image_requests")
    }
    seen = set()
    for row in recent:
        if type(row) is not dict or type(row.get("identity")) is not dict:
            invalid()
        session = row["identity"].get("session")
        if type(session) is not str or not session:
            invalid()
        if session in warm_sessions:
            continue
        completed_at = row.get("time")
        if type(completed_at) is not str or not completed_at:
            invalid()
        completion = (session, completed_at)
        if completion in seen:
            invalid()
        seen.add(completion)
        stages = row.get("stages_ms")
        if type(stages) is not dict or not stages or any(stage not in STAGES for stage in stages):
            invalid()
        try:
            valid = all(type(value) in (int, float) and math.isfinite(value) and value >= 0 for value in stages.values())
        except (OverflowError, TypeError):
            valid = False
        if not valid:
            invalid()
        extraction = "extraction_run" in stages
        if extraction != ("extraction_wait" in stages):
            invalid()
        group = groups["image_requests" if extraction else "text_requests"]
        group["requests"] += 1
        for stage, value in stages.items():
            group["samples_ms"][stage].append(value)
    if groups["text_requests"]["requests"] != text_count or groups["image_requests"]["requests"] != image_count:
        invalid()
    for group in groups.values():
        group["coverage"] = {stage: len(values) for stage, values in group["samples_ms"].items()}
    return groups
