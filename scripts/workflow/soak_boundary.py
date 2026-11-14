"""Resolve captured upstream hashes using independently read vault mappings."""

import hashlib
import re


def verify_deferred_boundary(case, row, mappings):
    result = dict(row, boundary_verified=False)
    if row.get("outcome") != "boundary_pending":
        return result
    if row.get("completed") is not True or ("benchmark_tool_scenario" in case and row.get("semantic_verified") is not True):
        result["outcome"] = "client_or_model_failure"
        return result
    originals = set(case["occurrences"])
    if (not originals or any(not isinstance(value, str) or not value
                            or not isinstance(mappings.get(value), str)
                            or not mappings[value] or mappings[value] == value
                            for value in originals)):
        result.update(outcome="unresolved", boundary_failure="mapping_evidence_missing")
        return result
    # Normal synthetic fixture values are ASCII and need no JSON escaping.
    # Substitute simultaneously so replacement values cannot trigger later replacements.
    replacements = {value.encode(): mappings[value].encode() for value in originals}
    pattern = re.compile(b"|".join(re.escape(value) for value in sorted(replacements, key=len, reverse=True)))
    expected = pattern.sub(lambda match: replacements[match.group()], case["body"])
    digest = hashlib.sha256(expected).hexdigest()
    if digest != row.get("boundary_sha256"):
        result.update(outcome="transformation_error", boundary_failure="upstream_hash_mismatch")
        return result
    result.update(outcome="supported_success", boundary_verified=True)
    return result
