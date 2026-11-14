"""Conservative attribution for a canceled request without response headers.

This is one evidence component, never a standalone cancellation qualification.
The caller must collect snapshots while other image requests remain gated and
independently prove semantic recovery, upstream absence, privacy and cleanup.
"""
import re


def _index(rows):
    if not isinstance(rows, list) or len(rows) > 100000:
        raise ValueError("cancellation_audit_snapshot")
    indexed = {}
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError("cancellation_audit_record")
        identity = row.get("request_id")
        if (not isinstance(identity, str) or not identity.strip()
                or len(identity) > 512 or not identity.isprintable()
                or identity in indexed):
            raise ValueError("cancellation_audit_identity")
        if row.get("decision") not in ("forwarded", "sanitized", "blocked"):
            raise ValueError("cancellation_audit_decision")
        indexed[identity] = row
    return indexed


def attribute_cancellation(before, after, *, expected_detail):
    """Require exactly one newly blocked row, with no lost/changed history."""
    if (not isinstance(expected_detail, str)
            or re.fullmatch(r"[a-z_]{1,128}", expected_detail) is None):
        raise ValueError("cancellation_audit_expected_detail")
    previous, current = _index(before), _index(after)
    if any(current.get(identity) != row for identity, row in previous.items()):
        raise ValueError("cancellation_audit_history")
    new = [row for identity, row in current.items() if identity not in previous]
    blocked = [row for row in new if row["decision"] == "blocked"]
    if len(blocked) != 1:
        raise ValueError("cancellation_audit_ambiguous")
    row = blocked[0]
    if row.get("reason") != "sanitize" or row.get("detail") != expected_detail:
        raise ValueError("cancellation_audit_outcome")
    return {
        "audit_attributed": True,
        "request_id": row["request_id"],
        "new_records": len(new),
        "cancellation_qualified": False,
        "performance_qualified": False,
    }
