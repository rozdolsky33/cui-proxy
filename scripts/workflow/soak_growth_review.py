"""Capture a manual investigation that can close only bounded fixed-growth review."""
from dataclasses import dataclass
import hashlib
import math
import os
from pathlib import Path, PurePosixPath
import re
import stat

from soak_campaign_reader import read_report_bytes
from soak_evidence import _encode


REVIEW_LIMIT = 64 * 1024
INVESTIGATION_LIMIT = 128 * 1024
METRICS = frozenset(("rss_median_bytes", "live_heap_median_bytes"))
BINDINGS = frozenset(("root_events_sha256", "root_seal_sha256", "campaign_identity_sha256",
                      "expected_identity_sha256", "fixed_resource_result_sha256"))
_DIGEST = re.compile(r"[0-9a-f]{64}\Z")


@dataclass(frozen=True)
class GrowthReview:
    review_bytes: bytes
    investigation_bytes: bytes


def _require(condition, code):
    if not condition:
        raise ValueError("soak_growth_review_" + code)


def _object(value, keys, code):
    _require(type(value) is dict and set(value) == set(keys), code)


def _text(value, limit=2048):
    return type(value) is str and 0 < len(value) <= limit and bool(value.strip()) and "\0" not in value


def _digest(data):
    return hashlib.sha256(data).hexdigest()


def evaluator_module_sha256():
    return _digest(Path(__file__).read_bytes())


def _canonical_digest(value):
    return _digest(_encode(value))


def _bindings(value):
    _object(value, BINDINGS, "bindings_shape")
    _require(all(type(item) is str and _DIGEST.fullmatch(item) for item in value.values()), "bindings_digest")


def _producer(value):
    # This is declared manual authorship, not a claim that a scratch writer was
    # a clean committed generator. Machine context is independently recomputed
    # by this module and its actual clean evaluator provenance is retained by CLI.
    _object(value, ("kind", "identity", "provenance"), "producer_shape")
    _require(value["kind"] == "manual" and _text(value["identity"], 256)
             and _text(value["provenance"]), "producer_attestation")
    return {"kind": "manual", "identity_sha256": _digest(value["identity"].encode()),
            "provenance_sha256": _digest(value["provenance"].encode())}


def _review_document(data):
    _require(type(data) is bytes and 0 < len(data) <= REVIEW_LIMIT, "size")
    value = read_report_bytes(data)
    _object(value, ("schema_version", "scope", "bindings", "decision", "reviewer", "producer", "investigation"),
            "schema")
    _require(type(value["schema_version"]) is int and value["schema_version"] == 1
             and value["scope"] == "fixed_resource_growth_review", "version_scope")
    _require(value["decision"] == "accept_bounded_growth", "decision")
    _bindings(value["bindings"])
    reviewer = value["reviewer"]
    _object(reviewer, ("identity", "independent_of_collection", "independence_basis"), "reviewer_shape")
    _require(_text(reviewer["identity"], 256) and reviewer["independent_of_collection"] is True
             and _text(reviewer["independence_basis"]), "reviewer_attestation")
    _producer(value["producer"])
    artifact = value["investigation"]
    _object(artifact, ("path", "bytes", "sha256"), "artifact_shape")
    _require(type(artifact["path"]) is str and len(artifact["path"]) <= 256, "artifact_path")
    path = PurePosixPath(artifact["path"])
    _require(not path.is_absolute() and 0 < len(path.parts) <= 8 and "\\" not in artifact["path"]
             and all(part not in (".", "..") and re.fullmatch(r"[A-Za-z0-9_.-]+", part) for part in path.parts),
             "artifact_path")
    _require(type(artifact["bytes"]) is int and 0 < artifact["bytes"] <= INVESTIGATION_LIMIT
             and type(artifact["sha256"]) is str and _DIGEST.fullmatch(artifact["sha256"]), "artifact_descriptor")
    return value


def _investigation_document(data):
    _require(type(data) is bytes and 0 < len(data) <= INVESTIGATION_LIMIT, "investigation_size")
    value = read_report_bytes(data)
    _object(value, ("schema_version", "scope", "bindings", "observed_growth", "window_medians",
                    "conclusions", "analysis_summary", "limitations", "producer"), "investigation_schema")
    _require(type(value["schema_version"]) is int and value["schema_version"] == 1
             and value["scope"] == "fixed_resource_growth_investigation", "investigation_scope")
    _bindings(value["bindings"])
    _object(value["conclusions"], METRICS, "conclusions")
    _require(_text(value["analysis_summary"], 8192), "analysis_missing")
    _require(type(value["limitations"]) is list and 0 < len(value["limitations"]) <= 16
             and all(_text(item) for item in value["limitations"]), "limitations_missing")
    _producer(value["producer"])
    return value


def _capture(path, limit, root):
    path = Path(path).absolute()
    _require(".." not in path.parts and all(not part.is_symlink() for part in (path, *path.parents)), "symlink")
    _require(not path.resolve().is_relative_to(Path(root).resolve()), "inside_campaign")
    descriptor = None
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        before = os.fstat(descriptor)
        _require(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= limit, "file_type_or_size")
        chunks = []
        length = 0
        while True:
            chunk = os.read(descriptor, min(65536, limit + 1 - length))
            if not chunk:
                break
            chunks.append(chunk)
            length += len(chunk)
            _require(length <= limit, "file_grew")
        after = os.fstat(descriptor)
        fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
        _require(length == before.st_size and all(getattr(before, name) == getattr(after, name) for name in fields),
                 "file_changed")
        return b"".join(chunks)
    except OSError:
        raise ValueError("soak_growth_review_file_unavailable") from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


def load_growth_review(path, root):
    """Read only external, bounded, nonsymlinked current-review input files."""
    data = _capture(path, REVIEW_LIMIT, root)
    review = _review_document(data)
    artifact = review["investigation"]
    investigation_path = Path(path).absolute().parent / artifact["path"]
    investigation = _capture(investigation_path, INVESTIGATION_LIMIT, root)
    _require(len(investigation) == artifact["bytes"] and _digest(investigation) == artifact["sha256"],
             "artifact_changed")
    _investigation_document(investigation)
    return GrowthReview(data, investigation)


def growth_review_context(campaign, expected_identity, resource_result):
    """Recompute facts only; this API never authors an acceptance attestation."""
    _require(type(campaign) is dict and type(expected_identity) is dict and type(resource_result) is dict,
             "context_shape")
    bindings = {"root_events_sha256": campaign.get("root_events_sha256"),
                "root_seal_sha256": campaign.get("root_seal_sha256"),
                "campaign_identity_sha256": _canonical_digest(campaign.get("identity")),
                "expected_identity_sha256": _canonical_digest(expected_identity),
                "fixed_resource_result_sha256": _canonical_digest(resource_result)}
    _bindings(bindings)
    _require(type(campaign.get("identity")) is dict and type(resource_result.get("windows")) is list,
             "context_missing")
    # Decode canonical bytes to give callers independent, finite metadata values.
    return read_report_bytes(_encode({"bindings": bindings, "observed_growth": resource_result.get("growth"),
                                      "window_medians": resource_result["windows"]}))


def evaluate_growth_review(campaign, expected_identity, resource_result, review, *,
                           prerequisites, required_prerequisites):
    """Accept only an explicit manual review after all unchanged gates pass."""
    if review is None:
        reviewed = (resource_result.get("state") == "pass"
                    and resource_result.get("positive_growth_requires_review") is False)
        return {"state": "pass" if reviewed else "incomplete", "performance_qualified": False}
    _require(type(review) is GrowthReview, "capture_required")
    _require(required_prerequisites and set(prerequisites) == set(required_prerequisites)
             and "growth_review" not in required_prerequisites
             and all(type(gate) is dict and gate.get("state") == "pass" for gate in prerequisites.values()),
             "prerequisites")
    _require(resource_result.get("state") == "pass" and resource_result.get("availability", {}).get("state") == "pass",
             "fixed_resources")
    growth = resource_result.get("growth")
    _object(growth, METRICS, "numeric_context")
    _require(all(type(value) in (int, float) and math.isfinite(value) and value <= .1 for value in growth.values()),
             "numeric_limits")
    _require(resource_result.get("positive_growth_requires_review") is True and any(value > 0 for value in growth.values()),
             "not_required")
    declared = _review_document(review.review_bytes)
    artifact = declared["investigation"]
    _require(type(review.investigation_bytes) is bytes and len(review.investigation_bytes) == artifact["bytes"]
             and _digest(review.investigation_bytes) == artifact["sha256"], "artifact_changed")
    investigation = _investigation_document(review.investigation_bytes)
    context = growth_review_context(campaign, expected_identity, resource_result)
    _require(declared["bindings"] == context["bindings"], "binding_mismatch")
    _require(all(_encode(investigation[key]) == _encode(value) for key, value in context.items()),
             "investigation_context")
    for metric, value in growth.items():
        expected = "bounded_positive_growth" if value > 0 else "nonpositive_growth"
        _require(investigation["conclusions"][metric] == expected, "metric_conclusion")
    reviewer = declared["reviewer"]
    return {"state": "pass", "performance_qualified": False, "scope": "positive_fixed_growth_only",
            "review_sha256": _digest(review.review_bytes), "investigation_sha256": _digest(review.investigation_bytes),
            "bindings": context["bindings"], "reviewer_identity_sha256": _digest(reviewer["identity"].encode()),
            "independence_basis_sha256": _digest(reviewer["independence_basis"].encode()),
            "review_producer": _producer(declared["producer"]),
            "investigation_producer": _producer(investigation["producer"])}
