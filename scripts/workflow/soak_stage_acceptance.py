"""Conservative completion-window OCR gates from retained poll intervals.

This component does not establish request/stage coverage or qualify a campaign.
"""
import bisect
import math


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def worst_nearest_rank_p95(certain, optional):
    """Largest nearest-rank p95 over every subset of optional observations."""
    if (not certain and not optional) or any(not _number(x) or x < 0 for x in [*certain, *optional]):
        raise ValueError("stage_quantile_samples")
    certain, optional = sorted(certain), sorted(optional)
    candidates = sorted(set(certain + optional))
    low, high = 0, len(candidates)
    while low < high:
        middle = (low + high) // 2
        candidate = candidates[middle]
        # Include every optional value >= candidate, and no optional value below.
        # This maximizes the rank while adding no value below the candidate.
        size = len(certain) + len(optional) - bisect.bisect_left(optional, candidate)
        rank = (95 * size + 99) // 100
        if rank > bisect.bisect_left(certain, candidate):
            low = middle + 1
        else:
            high = middle
    return candidates[low - 1]


def evaluate_extraction_windows(records, *, started_s, duration_s, fault_intervals):
    if not _number(started_s) or type(duration_s) not in (int, float) or duration_s != 7200:
        raise ValueError("stage_original_horizon")
    if not isinstance(records, list) or not isinstance(fault_intervals, list):
        raise ValueError("stage_records")
    faults = []
    for row in fault_intervals:
        if (not isinstance(row, dict) or not _number(row.get("started_s"))
                or not _number(row.get("ended_s")) or row["ended_s"] <= row["started_s"]):
            raise ValueError("stage_fault_interval")
        faults.append((row["started_s"], row["ended_s"]))
    faults.sort()
    if any(a[1] > b[0] for a, b in zip(faults, faults[1:])):
        raise ValueError("stage_fault_overlap")
    selected, seen, fault_samples = [], set(), 0
    budget_passed = True
    for row in records:
        if not isinstance(row, dict) or not isinstance(row.get("stages_ms"), dict):
            raise ValueError("stage_record_shape")
        spans = row["stages_ms"]
        if not any(key in spans for key in ("extraction_run", "extraction_wait")):
            if row.get("extraction_cache_hits", 0) != 0:
                raise ValueError("stage_cached_timing_missing")
            continue
        identity = row.get("identity")
        if (not isinstance(identity, dict) or not isinstance(identity.get("session"), str)
                or not identity["session"] or not isinstance(row.get("time"), str) or not row["time"]):
            raise ValueError("stage_identity")
        key = (identity["session"], row["time"])
        if key in seen:
            raise ValueError("stage_duplicate")
        seen.add(key)
        upper, lower = row.get("observed_s"), row.get("observation_lower_s")
        if not _number(upper) or (lower is not None and (not _number(lower) or lower > upper)):
            raise ValueError("stage_observation_bounds")
        if any(not _number(spans.get(key)) or spans[key] < 0 for key in ("extraction_run", "extraction_wait")):
            raise ValueError("stage_duration")
        if type(row.get("extraction_cache_hits")) is not int or not 0 <= row["extraction_cache_hits"] <= 2:
            raise ValueError("stage_cache_evidence")
        if upper < started_s:
            continue
        if lower is None:
            raise ValueError("stage_completion_unbounded")
        if lower >= started_s + duration_s:
            continue
        budget_passed = budget_passed and spans["extraction_run"] + spans["extraction_wait"] <= 30000
        contained_fault = any(a <= lower and upper <= b for a, b in faults)
        if contained_fault:
            fault_samples += 1
            continue
        intersects_fault = any(lower < b and upper >= a for a, b in faults)
        selected.append((lower, upper, spans, intersects_fault))
    windows, states = [], []
    for offset in range(0, 7200, 900):
        start, end = started_s + offset, started_s + offset + 900
        certain, optional = [], []
        for lower, upper, spans, fault_uncertain in selected:
            if lower >= end or upper < start:
                continue
            if start <= lower and upper < end and not fault_uncertain:
                certain.append(spans)
            else:
                optional.append(spans)
        if not certain:
            raise ValueError("stage_supported_window_missing")
        run = worst_nearest_rank_p95([s["extraction_run"] for s in certain], [s["extraction_run"] for s in optional])
        queue = worst_nearest_rank_p95([s["extraction_wait"] for s in certain], [s["extraction_wait"] for s in optional])
        state = "pass" if run <= 5000 and queue <= 2000 else ("incomplete" if optional else "fail")
        states.append(state)
        windows.append(dict(start_s=offset, duration_s=900, certain_samples=len(certain),
                            optional_samples=len(optional), run_p95_upper_ms=run,
                            queue_p95_upper_ms=queue, state=state))
    state = "fail" if not budget_passed or "fail" in states else ("incomplete" if "incomplete" in states else "pass")
    return dict(state=state, windows=windows, fault_samples=fault_samples,
                extraction_budget_passed=budget_passed, window_basis="bounded_stage_completion",
                performance_qualified=False)
