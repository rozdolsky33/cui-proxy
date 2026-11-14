"""Validate resource cadence and explicitly attributed restart exceptions."""
import math

EPSILON = 1e-6


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def _integer(value):
    return type(value) is int and value >= 0


def _covered(start, end, intervals):
    cursor = start
    for lower, upper in intervals:
        if upper <= cursor:
            continue
        if lower > cursor:
            break
        cursor = max(cursor, upper)
        if cursor >= end:
            return True
    return cursor >= end


def evaluate_resource_series(rows, *, started_s, ended_s, restart_intervals):
    if (not _number(started_s) or not _number(ended_s) or ended_s-started_s < 1
            or not isinstance(rows, list) or not rows or not isinstance(restart_intervals, list)):
        raise ValueError("resource_series_horizon")
    faults = []
    for interval in restart_intervals:
        if (not isinstance(interval, dict) or interval.get("control", "restart") != "restart"
                or not _number(interval.get("started_s")) or not _number(interval.get("ended_s"))
                or interval["ended_s"] <= interval["started_s"]):
            raise ValueError("resource_restart_interval")
        faults.append((interval["started_s"], interval["ended_s"]))
    faults.sort()
    if any(a[1] > b[0] for a, b in zip(faults, faults[1:])):
        raise ValueError("resource_restart_overlap")
    origin, previous_end, previous_slot = None, None, -1
    observed_slots, unavailable, measured = set(), 0, 0
    peaks = {"proxy_rss_bytes": None, "child_rss_bytes": None}
    groups = {"rss_available": ("proxy_rss_bytes", "child_rss_bytes"),
              "runtime_available": ("heap_objects_bytes", "live_heap_bytes", "goroutines", "gc_cycles"),
              "activity_available": ("active_requests",)}
    for row in rows:
        if not isinstance(row, dict) or row.get("event") != "resource":
            raise ValueError("resource_series_shape")
        start, end, elapsed, slot = (row.get(key) for key in
                                    ("observation_started_s", "observed_s", "elapsed_s", "slot"))
        if (any(not _number(value) for value in (start, end, elapsed)) or elapsed < 0
                or end < start or not _integer(slot) or slot <= previous_slot
                or (previous_end is not None and start < previous_end)):
            raise ValueError("resource_series_observation")
        candidate = end-elapsed
        if origin is None:
            origin = candidate
        if abs(origin-candidate) > EPSILON or not -EPSILON <= start-(origin+slot) < 1+EPSILON:
            raise ValueError("resource_series_clock_origin")
        previous_slot, previous_end = slot, end
        for flag, fields in groups.items():
            if type(row.get(flag)) is not bool:
                raise ValueError("resource_series_availability")
            for field in fields:
                value = row.get(field)
                if (value is not None and not _integer(value)) or (row[flag] and value is None):
                    raise ValueError("resource_series_value")
        for field in peaks:
            value = row.get(field)
            if value is not None:
                peaks[field] = value if peaks[field] is None else max(peaks[field], value)
        if end < started_s or start >= ended_s:
            continue
        measured += 1
        observed_slots.add(slot)
        if not all(row[flag] for flag in groups):
            if not any(start < upper and end >= lower for lower, upper in faults):
                raise ValueError("resource_unexplained_unavailability")
            unavailable += 1
    if not measured:
        raise ValueError("resource_series_missing")
    first = math.ceil(started_s-origin-EPSILON)
    last = math.ceil(ended_s-origin-EPSILON)
    if first < 0 or last <= first:
        raise ValueError("resource_series_origin")
    fault_slots = [(math.ceil(a-origin-EPSILON), math.floor(b-origin+EPSILON)+1) for a, b in faults]
    cursor, missing = first, 0
    # Check intervals between retained slots, without materializing a huge range.
    for slot in sorted(s for s in observed_slots if first <= s < last) + [last]:
        if slot > cursor:
            if not _covered(cursor, slot, fault_slots):
                raise ValueError("resource_unexplained_missing_slots")
            missing += slot-cursor
        cursor = slot+1
    if any(value is None for value in peaks.values()):
        state = "incomplete"
    else:
        state = "pass" if peaks["proxy_rss_bytes"] <= 512*1024**2 and peaks["child_rss_bytes"] <= 1024**3 else "fail"
    return dict(state=state, expected_slots=last-first, observed_samples=measured,
                unavailable_fault_samples=unavailable, missing_fault_slots=missing,
                proxy_peak_bytes=peaks["proxy_rss_bytes"], child_peak_bytes=peaks["child_rss_bytes"],
                performance_qualified=False)
