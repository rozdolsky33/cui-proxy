"""Report net churn resource growth using retained phase-boundary observations."""

import math


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def evaluate_churn_resources(rows, *, started_s, duration_s, elapsed_s, mapping_evidence):
    if (not isinstance(rows, list) or not _number(started_s) or not _number(duration_s)
            or duration_s != 7200 or not _number(elapsed_s) or elapsed_s < duration_s
            or not isinstance(mapping_evidence, dict)):
        raise ValueError("soak_churn_resource_shape")
    expected = dict(initial_pairs=10000, final_pairs=22000, persisted_churn_pairs=12000)
    if any(type(mapping_evidence.get(key)) is not int or mapping_evidence[key] != value
           for key, value in expected.items()):
        raise ValueError("soak_churn_resource_mapping_denominator")
    ended_s = started_s + elapsed_s
    before, after = [], []
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError("soak_churn_resource_sample")
        lower, upper = row.get("observation_started_s"), row.get("observed_s")
        if not _number(lower) or not _number(upper) or upper < lower:
            raise ValueError("soak_churn_resource_observation")
        if upper <= started_s:
            before.append(row)
        if lower >= ended_s:
            after.append(row)
    if not before or not after:
        raise ValueError("soak_churn_resource_boundary_missing")
    first = max(before, key=lambda row: row["observed_s"])
    last = min(after, key=lambda row: row["observation_started_s"])
    distances = dict(before_s=started_s-first["observed_s"],
                     after_s=last["observation_started_s"]-ended_s)
    # One nominal sampling tick plus its bounded scheduling delay. Full series
    # availability and peak caps remain independent required acceptance gates.
    if max(distances.values()) > 2:
        raise ValueError("soak_churn_resource_boundary_distance")
    for row in (first, last):
        if row.get("runtime_available") is not True or row.get("rss_available") is not True:
            raise ValueError("soak_churn_resource_unavailable")
        if any(type(row.get(key)) is not int or row[key] < 0
               for key in ("live_heap_bytes", "proxy_rss_bytes", "gc_cycles")):
            raise ValueError("soak_churn_resource_value")
    new_mappings = mapping_evidence["final_pairs"] - mapping_evidence["initial_pairs"]
    heap_delta = last["live_heap_bytes"] - first["live_heap_bytes"]
    rss_delta = last["proxy_rss_bytes"] - first["proxy_rss_bytes"]
    return dict(state="pass", method="net_phase_boundary_growth_per_persisted_mapping",
                new_mappings=new_mappings, live_heap_change_bytes=heap_delta,
                proxy_rss_change_bytes=rss_delta,
                live_heap_bytes_per_new_mapping=heap_delta/new_mappings,
                proxy_rss_bytes_per_new_mapping=rss_delta/new_mappings,
                live_heap_before_bytes=first["live_heap_bytes"], live_heap_after_bytes=last["live_heap_bytes"],
                proxy_rss_before_bytes=first["proxy_rss_bytes"], proxy_rss_after_bytes=last["proxy_rss_bytes"],
                gc_cycles_before=first["gc_cycles"], gc_cycles_after=last["gc_cycles"],
                boundary_distance_s=distances, performance_qualified=False)
