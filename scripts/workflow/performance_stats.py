"""Strict paired timing statistics; no performance qualification by themselves."""

import math


def _index(samples):
    if not isinstance(samples, (list, tuple)):
        raise ValueError("invalid_sample_collection")
    values = {}
    for sample in samples:
        if not isinstance(sample, dict):
            raise ValueError("invalid_sample")
        ident = sample.get("operation_id")
        value = sample.get("first_byte_ms")
        if not isinstance(ident, str) or not ident:
            raise ValueError("invalid_operation_id")
        if ident in values:
            raise ValueError("duplicate_operation_id")
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise ValueError("invalid_first_byte_ms")
        try:
            value = float(value)
        except OverflowError:
            raise ValueError("invalid_first_byte_ms") from None
        if not math.isfinite(value) or value < 0:
            raise ValueError("invalid_first_byte_ms")
        values[ident] = value
    return values


def summarize_pairs(proxy, bypass):
    """Return nearest-rank quantiles of matched first-byte differences in ms.

    Each input is a list of operation_id/first_byte_ms records. Pair sets must
    be complete and nonempty. Negative differences are retained as jitter;
    this function never subtracts separately computed proxy/bypass quantiles.
    Errors contain fixed identifiers, never sample contents.
    """
    left, right = _index(proxy), _index(bypass)
    if not left or left.keys() != right.keys():
        raise ValueError("incomplete_pair_set")
    differences = sorted(value - right[ident] for ident, value in left.items())
    count = len(differences)
    return {"count": count,
            **{f"p{percent}": differences[math.ceil(count * percent / 100) - 1]
               for percent in (50, 95, 99)}}


OUTCOMES = frozenset((
    "supported_success", "expected_policy_block", "expected_representation_block",
    "unexpected_proxy_refusal", "transformation_error", "dependency_failure",
    "proxy_overload", "upstream_failure", "client_cancelled",
    "client_or_model_failure", "harness_invalid", "unresolved",
))
EXPECTED_BLOCKS = frozenset(("expected_policy_block", "expected_representation_block"))


def summarize_outcomes(records):
    """Keep every attempted operation, including timeouts, in its denominator.

    `supported` comes from the workload manifest, never the observed outcome.
    `completed` means a full HTTP response was received (not necessarily correct).
    Missing responses still require a terminal outcome and observed duration.
    """
    counts = {key: 0 for key in sorted(OUTCOMES)}
    seen = set()
    completed = supported = failures = 0
    for row in records:
        ident = row.get("operation_id")
        outcome = row.get("outcome")
        duration = row.get("duration_ms")
        if (not isinstance(ident, str) or not ident or ident in seen
                or not isinstance(outcome, str) or outcome not in OUTCOMES
                or type(row.get("supported")) is not bool
                or type(row.get("completed")) is not bool
                or type(duration) not in (int, float)
                or not math.isfinite(duration) or duration < 0):
            raise ValueError("invalid outcome record")
        if (outcome == "supported_success" and (not row["supported"] or not row["completed"])
                or outcome in EXPECTED_BLOCKS and (row["supported"] or not row["completed"])):
            raise ValueError("inconsistent outcome record")
        seen.add(ident)
        counts[outcome] += 1
        completed += row["completed"]
        supported += row["supported"]
        failures += outcome != "supported_success" and outcome not in EXPECTED_BLOCKS
    successes = counts["supported_success"]
    return {"attempted": len(seen), "completed": completed,
            "supported_attempted": supported, "supported_success": successes,
            "supported_success_rate": successes / supported if supported else None,
            "failures": failures, "outcomes": counts}
