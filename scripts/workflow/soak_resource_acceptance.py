"""Original sustained-load resource gates; never qualifies a whole campaign."""
import math
import statistics

from soak_resource_series import evaluate_resource_series


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _integer(value):
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def evaluate_fixed_resources(rows, *, started_s, duration_s, restart_intervals=None):
    if not _number(started_s) or duration_s != 7200 or isinstance(duration_s, bool):
        raise ValueError("fixed_resource_duration")
    availability = evaluate_resource_series(rows, started_s=started_s, ended_s=started_s+duration_s,
                                            restart_intervals=[] if restart_intervals is None else restart_intervals)
    selected = [row for row in rows if started_s <= row["observed_s"] < started_s+duration_s
                and row["rss_available"] is True and row["runtime_available"] is True]
    windows = []
    for offset in range(0, 7200, 900):
        samples = [row for row in selected if started_s+offset <= row["observed_s"] < started_s+offset+900]
        if not samples:
            raise ValueError("resource_window_missing")
        windows.append(dict(start_s=offset, samples=len(samples),
                            rss_median_bytes=statistics.median(row["proxy_rss_bytes"] for row in samples),
                            live_heap_median_bytes=statistics.median(row["live_heap_bytes"] for row in samples)))
    first, last = windows[0], windows[-1]
    growth = {}
    for key in ("rss_median_bytes", "live_heap_median_bytes"):
        if first[key] <= 0:
            raise ValueError("resource_growth_baseline")
        growth[key] = (last[key]-first[key])/first[key]
    passed = availability["state"] == "pass" and all(value <= .10 for value in growth.values())
    return dict(state="pass" if passed else "fail", windows=windows,
                observed_slots=availability["observed_samples"], available_samples=len(selected),
                proxy_peak_bytes=availability["proxy_peak_bytes"], child_peak_bytes=availability["child_peak_bytes"],
                growth=growth, availability=availability,
                positive_growth_requires_review=any(value > 0 for value in growth.values()),
                performance_qualified=False)


def evaluate_idle_recovery(before, after, before_gc, after_gc):
    for row in (before, after, before_gc, after_gc):
        if (not isinstance(row, dict) or row.get("runtime_available") is not True
                or row.get("activity_available") is not True
                or not _integer(row.get("goroutines"))
                or not _integer(row.get("active_requests"))
                or not _number(row.get("elapsed_s")) or row["elapsed_s"] < 0):
            raise ValueError("idle_observation")
    for row in (before_gc, after_gc):
        if (row.get("procedure") != "two_observed_idle_gc_cycles"
                or any(not _integer(row.get(key)) for key in ("initial_gc_cycles", "gc_cycles", "cycles_observed"))
                or row["cycles_observed"] != row["gc_cycles"] - row["initial_gc_cycles"]
                or row["cycles_observed"] < 2 or row["elapsed_s"] > 300):
            raise ValueError("idle_gc_equivalence")
    passed = (all(row["active_requests"] == 0 for row in (before, after, before_gc, after_gc))
              and after["elapsed_s"] <= 60 and after["goroutines"] <= before["goroutines"] + 5)
    return dict(state="pass" if passed else "fail", performance_qualified=False)
