"""Bind original soak settings and reference provenance to a trusted identity."""

import hashlib
import json
import math
import re

from performance_campaign import check_reference_environment
from performance_workload import Workload


def _number(value):
    return (isinstance(value, (int, float)) and not isinstance(value, bool)
            and math.isfinite(value))


def _digest(value, length=64):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{%d}" % length, value) is not None


def evaluate_provenance(identity, expected_identity, settings):
    """Require an independently retained expected identity, never infer it here."""
    if not isinstance(identity, dict) or not isinstance(expected_identity, dict) or identity != expected_identity:
        raise ValueError("soak_identity_mismatch")
    required = {"fixed_seconds", "churn_seconds", "warmup_seconds", "warmup_count",
                "drain_seconds", "control_count"}
    if not isinstance(settings, dict) or set(settings) != required:
        raise ValueError("soak_settings_shape")
    if not all(_number(value) for value in settings.values()):
        raise ValueError("soak_settings_number")
    if (settings["fixed_seconds"] != 7200 or settings["churn_seconds"] != 7200
            or settings["warmup_seconds"] < 60 or not 0 <= settings["drain_seconds"] <= 45):
        raise ValueError("soak_settings_duration")
    for key, minimum in (("warmup_count", 100), ("control_count", 10)):
        if type(settings[key]) is not int or settings[key] < minimum:
            raise ValueError("soak_settings_count")
    jobs = [{"section": "soak", "settings": settings}]
    digest = hashlib.sha256(json.dumps(jobs, sort_keys=True).encode()).hexdigest()
    if identity.get("jobs_sha256") != digest:
        raise ValueError("soak_settings_identity")
    if (identity.get("profile") != "soak"
            or identity.get("policy_variant") != "normal-plus-declared-negative-rule"
            or identity.get("workload") != Workload("normal").metadata()):
        raise ValueError("soak_workload_identity")
    for key in ("binary_sha256", "seed_binary_sha256", "fixture_manifest_sha256",
                "policy_sha256", "build_manifest_sha256"):
        if not _digest(identity.get(key)):
            raise ValueError("soak_identity_digest")
    source = identity.get("source")
    if (not isinstance(source, dict) or source.get("dirty") is not False
            or not _digest(source.get("commit"), 40)
            or not _digest(source.get("script_sha256"))):
        raise ValueError("soak_source_identity")
    revision = identity.get("binary_source_commit")
    proof = identity.get("source_proof")
    if (not _digest(revision, 40) or not isinstance(proof, dict)
            or proof.get("source_commit") != revision):
        raise ValueError("soak_binary_source_identity")
    embedded = proof.get("embedded_clean_revision") is True
    reproduced = (proof.get("reproduced_binary") is True
                  and proof.get("binary_sha256") == identity["binary_sha256"])
    if not (embedded or reproduced):
        raise ValueError("soak_binary_source_proof")
    try:
        check_reference_environment(identity.get("host"))
    except (ValueError, TypeError, KeyError, AttributeError, RuntimeError) as error:
        raise ValueError("soak_reference_environment") from error
    return {"state": "pass", "identity_verified": True,
            "settings_verified": True, "performance_qualified": False}
