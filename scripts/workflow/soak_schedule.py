"""Deterministic metadata for the sustained acceptance request schedule.

This module does not construct payloads, execute requests or qualify a run.
"""

import math
from fractions import Fraction

_TEXT = ("W01", "W02", "W03", "W04", "W08")
_IMAGE = ("W05", "W06")
_LIFECYCLE = ("W07", "W09", "W10", "W11", "W12")
_SIZES = (262144, 786432, 1048576)


def _integer(value, minimum, maximum=None):
    return (type(value) is int and value >= minimum
            and (maximum is None or value <= maximum))


def plan_request(client, serial, phase):
    """Return a stable request identity and its prescribed workload metadata."""
    if (not _integer(client, 0, 3) or not _integer(serial, 0)
            or phase not in ("fixed", "churn")):
        raise ValueError("invalid schedule identity")
    cycle, slot = divmod(serial, 10)
    if slot < 7:
        category = "text"
        scenario = _TEXT[(cycle * 7 + slot + client) % len(_TEXT)]
    elif slot < 9:
        category = "image"
        scenario = _IMAGE[(cycle * 2 + slot - 7 + client) % len(_IMAGE)]
    else:
        category = "lifecycle"
        scenario = _LIFECYCLE[(cycle + client) % len(_LIFECYCLE)]
    return {"identity": (phase, client, serial), "category": category,
            "scenario": scenario, "size_bytes": _SIZES[(serial + client) % 3]}


def churn_due_indices(elapsed_seconds, assigned, capacity):
    """Allocate due indices without dropping backlog or implying persistence.

    Interpret the clock's decimal representation exactly so boundary values
    such as 0.6 seconds do not lose an index through binary rounding. The
    caller serializes clock observation and allocation; replay uses the saved
    indices and never substitutes a new elapsed time.
    """
    try:
        valid_time = (type(elapsed_seconds) in (int, float)
                      and math.isfinite(elapsed_seconds) and elapsed_seconds >= 0)
    except OverflowError:
        valid_time = False
    if (not valid_time or not _integer(assigned, 0)
            or not _integer(capacity, 1, 40)):
        raise ValueError("invalid churn schedule")
    due = math.floor(Fraction(str(elapsed_seconds)) * 100 / 60)
    if assigned > due:
        raise ValueError("churn counter exceeds schedule")
    return tuple(range(assigned, assigned + min(capacity, due - assigned)))
