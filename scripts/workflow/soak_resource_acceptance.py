"""Original sustained-load resource gates; never qualifies a whole campaign."""
import math
import statistics


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _integer(value):
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def evaluate_fixed_resources(rows, *, started_s, duration_s):
    if not _number(started_s) or duration_s != 7200 or isinstance(duration_s, bool):
        raise ValueError("fixed_resource_duration")
    selected = []
    seen = set()
    for row in rows:
        if not isinstance(row, dict) or row.get("event") != "resource":
            raise ValueError("resource_shape")
        observed = row.get("observed_s")
        slot = row.get("slot")
        if not _number(observed) or not _integer(slot) or slot in seen:
            raise ValueError("resource_identity")
        seen.add(slot)
        if started_s <= observed < started_s + duration_s:
            if row.get("rss_available") is not True or row.get("runtime_available") is not True:
                raise ValueError("resource_unavailable")
            for key in ("proxy_rss_bytes", "child_rss_bytes", "live_heap_bytes", "goroutines", "gc_cycles"):
                if not _integer(row.get(key)):
                    raise ValueError("resource_value")
            selected.append(row)
    selected.sort(key=lambda row: row["observed_s"])
    if (not selected or selected[0]["observed_s"] > started_s + 1
            or selected[-1]["observed_s"] < started_s + duration_s - 1):
        raise ValueError("resource_horizon")
    for previous, current in zip(selected, selected[1:]):
        if (current["slot"] != previous["slot"] + 1
                or not 0 < current["observed_s"] - previous["observed_s"] < 2):
            raise ValueError("resource_sampling_gap")
    windows = []
    for offset in range(0, 7200, 900):
        samples = [row for row in selected if started_s + offset <= row["observed_s"] < started_s + offset + 900]
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
        growth[key] = (last[key] - first[key]) / first[key]
    proxy_peak = max(row["proxy_rss_bytes"] for row in selected)
    child_peak = max(row["child_rss_bytes"] for row in selected)
    passed = proxy_peak <= 512 * 1024**2 and child_peak <= 1024**3 and all(value <= .10 for value in growth.values())
    return dict(state="pass" if passed else "fail", windows=windows, observed_slots=len(selected),
                proxy_peak_bytes=proxy_peak, child_peak_bytes=child_peak, growth=growth,
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
