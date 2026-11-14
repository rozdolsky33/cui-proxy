"""Compose a trial's timing, transport, semantic and audit evidence."""
from cancellation_acceptance import _helper, _number, _window, evaluate_trial
from cancellation_audit import _index, attribute_cancellation
from soak_receipts import verify_receipt
from soak_trace_acceptance import _image_proof, verify_semantic_outcome


_ROLES = frozenset(("queued", "active", "survivor", "replacement", "text"))
_DETAILS = {"queued": "attachment_image_busy", "active": "attachment_image_timeout"}


def evaluate_trial_evidence(report):
    """Verify one trial; provenance, warmup and finalization remain separate."""
    if not isinstance(report, dict):
        raise ValueError("cancellation_trial_report")
    bounds = report.get("bounds")
    evaluate_trial(bounds)
    operations = report.get("operations")
    if not isinstance(operations, dict) or set(operations) != _ROLES:
        raise ValueError("cancellation_trial_operations")
    release = _number(report.get("release_s"))
    if release < _number(bounds["active"]["after"]["ended_s"]):
        raise ValueError("cancellation_trial_early_release")
    if type(report.get("threads_joined")) is not int or report["threads_joined"] != 5:
        raise ValueError("cancellation_trial_threads")
    observation_start, observation_end = _window(report.get("receipt_observation"))
    if observation_start < release:
        raise ValueError("cancellation_trial_early_receipt")
    final_helpers = report.get("final_helpers")
    expected_helpers = {
        _helper(helper) for helper in bounds["queued"]["before"]["helpers"]
    } | {_helper(bounds["active"]["admitted"]["helper"])}
    if (not isinstance(final_helpers, list) or len(final_helpers) != 3
            or {_helper(helper) for helper in final_helpers} != expected_helpers):
        raise ValueError("cancellation_trial_extra_admission")
    receipts, audits = report.get("receipts"), report.get("audit")
    if (not isinstance(receipts, dict) or set(receipts) != set(_DETAILS)
            or not isinstance(audits, dict) or set(audits) != set(_DETAILS)):
        raise ValueError("cancellation_trial_cancel_proof")
    if any(not isinstance(audits[role], dict) for role in _DETAILS):
        raise ValueError("cancellation_trial_audit")
    queued_history = _index(audits["queued"].get("after"))
    active_history = _index(audits["active"].get("before"))
    if any(active_history.get(identity) != row for identity, row in queued_history.items()):
        raise ValueError("cancellation_trial_audit_history")
    identities, audit_ids, image_ids = set(), set(), set()
    for role, entry in operations.items():
        if not isinstance(entry, dict):
            raise ValueError("cancellation_trial_operation")
        started, ended = _window(entry)
        if ended - started > 45 or ended > observation_start:
            raise ValueError("cancellation_trial_operation_deadline")
        plan, row = entry.get("request"), entry.get("result")
        if not isinstance(plan, dict) or not isinstance(row, dict):
            raise ValueError("cancellation_trial_operation_record")
        identity = plan.get("identity")
        if (not isinstance(identity, str) or not identity.strip()
                or not identity.isprintable() or len(identity) > 512
                or identity in identities or row.get("operation_id") != identity):
            raise ValueError("cancellation_trial_operation_identity")
        identities.add(identity)
        category = "text" if role == "text" else "image"
        if plan.get("category") != category or row.get("category") != category:
            raise ValueError("cancellation_trial_operation_category")
        if category == "image":
            image_ids.update(item["id"] for item in _image_proof(row))
        if role in _DETAILS:
            step = bounds[role]
            if (started != _number(step["request_started_s"])
                    or ended < _number(step["transport_end_s"])
                    or row.get("completed") is not False
                    or type(row.get("status")) is not int or row["status"] != 0
                    or row.get("semantic_verified") is True):
                raise ValueError("cancellation_trial_cancel_outcome")
            verify_receipt(receipts[role], absent=True)
            snapshot = audits[role]
            if not isinstance(snapshot, dict):
                raise ValueError("cancellation_trial_audit")
            attributed = attribute_cancellation(
                snapshot.get("before"), snapshot.get("after"), expected_detail=_DETAILS[role])
            if attributed["request_id"] in audit_ids:
                raise ValueError("cancellation_trial_audit_identity")
            audit_ids.add(attributed["request_id"])
        else:
            verify_semantic_outcome(plan, row, "proxy")
            if row.get("completed") is not True or row.get("semantic_verified") is not True:
                raise ValueError("cancellation_trial_supported_outcome")
            if role in ("survivor", "replacement") and ended < release:
                raise ValueError("cancellation_trial_premature_recovery")
            if role == "text" and not (started <= bounds["queued"]["cancel_s"]
                                         and ended >= bounds["active"]["cancel_s"]):
                raise ValueError("cancellation_trial_text_overlap")
    return {
        "trial_verified": True,
        "operation_ids": sorted(identities),
        "audit_ids": sorted(audit_ids),
        "image_ids": sorted(image_ids),
        "observed_through_s": observation_end,
        "cancellation_qualified": False,
        "performance_qualified": False,
    }
