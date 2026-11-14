"""Verify retained audit, privacy and owned-process finalization counts."""


def _count(value, minimum=0):
    return type(value) is int and value >= minimum


def evaluate_finalization(report, *, mode, expected_outcomes,
                          expected_generations, expected_children,
                          minimum_protected_values):
    if (mode not in ("proxy", "bypass")
            or not all(_count(value, 1) for value in
                       (expected_outcomes, expected_generations,
                        expected_children, minimum_protected_values))):
        raise ValueError("soak_finalization_expectations")
    if (not isinstance(report, dict) or report.get("verified") is not True
            or report.get("performance_qualified") is not False
            or not _count(report.get("outcomes_reconciled"))
            or report["outcomes_reconciled"] != expected_outcomes):
        raise ValueError("soak_finalization_outcomes")
    cleanup = report.get("cleanup")
    if (not isinstance(cleanup, dict)
            or any(not _count(cleanup.get(key)) or cleanup[key] != expected_children
                   for key in ("children_stopped", "logs_closed"))):
        raise ValueError("soak_finalization_cleanup")
    privacy = report.get("privacy")
    if (not isinstance(privacy, dict) or privacy.get("privacy_verified") is not True
            or not _count(privacy.get("files_checked"), 1)
            or not _count(privacy.get("protected_values_checked"), minimum_protected_values)):
        raise ValueError("soak_finalization_privacy")
    audit = report.get("audit")
    if not isinstance(audit, dict) or not _count(audit.get("audit_records")):
        raise ValueError("soak_finalization_audit")
    if mode == "bypass":
        if audit["audit_records"] != 0 or audit.get("bypass_proxy_absence_verified") is not True:
            raise ValueError("soak_finalization_bypass_audit")
    else:
        categories = ("rule_observation_records", "old_token_records_without_matches",
                      "shape_rejections_without_matches")
        if (audit["audit_records"] != expected_outcomes
                or any(not _count(audit.get(key), 1) for key in categories)
                or sum(audit[key] for key in categories) != expected_outcomes):
            raise ValueError("soak_finalization_audit_counts")
        policy = audit.get("policy")
        if (not isinstance(policy, dict)
                or set(policy) != {str(i) for i in range(expected_generations)}
                or any(not _count(value, 1) for value in policy.values())
                or sum(policy.values()) != audit["rule_observation_records"]):
            raise ValueError("soak_finalization_policy_generations")
    return {"state": "pass", "outcomes_reconciled": expected_outcomes,
            "privacy_verified": True, "cleanup_verified": True,
            "performance_qualified": False}
