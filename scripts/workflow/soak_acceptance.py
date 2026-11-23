"""Apply the original four-hour soak contract to an authenticated campaign."""

import math
import re

from soak_churn_resources import evaluate_churn_resources
from soak_control_acceptance import evaluate_controls
from soak_evidence_bindings import evaluate_evidence_bindings
from soak_finalization_acceptance import evaluate_finalization
from soak_growth_review import evaluate_growth_review
from soak_provenance_acceptance import evaluate_provenance
from soak_resource_acceptance import evaluate_fixed_resources, evaluate_idle_recovery
from soak_resource_series import evaluate_resource_series
from soak_stage_acceptance import evaluate_extraction_windows
from soak_stage_coverage import evaluate_stage_coverage
from soak_trace_acceptance import evaluate_phase_trace, _image_proof
from soak_warmup_acceptance import evaluate_warmup
from soak_window_acceptance import evaluate_text_windows


REQUIRED_GATES = frozenset({
    "provenance", "bindings", "timeline", "controls", "corpus", "stage_coverage",
    "resource_series", "fixed_resources", "churn_resources", "idle_recovery", "growth_review",
    "fixed_trace", "churn_trace", "fixed_text", "churn_text",
    "fixed_extraction", "churn_extraction", "proxy_warmup", "bypass_warmup",
    "proxy_finalization", "bypass_finalization",
})


def summarize_acceptance(gates):
    missing = sorted(REQUIRED_GATES - set(gates))
    states = [value.get("state") for value in gates.values()]
    passed = not missing and set(gates) == REQUIRED_GATES and all(s == "pass" for s in states)
    state = "pass" if passed else "fail" if "fail" in states else "incomplete"
    return dict(state=state, soak_qualified=passed, performance_qualified=False,
                missing_gates=missing, gates=gates)


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _timeline(reports):
    for mode in ("proxy", "bypass"):
        fixed, churn = [reports[mode + "-" + phase + "-summary.json"] for phase in ("fixed", "churn")]
        for row in (fixed, churn):
            if (not all(_number(row.get(key)) for key in ("started_s", "elapsed_s", "duration_s"))
                    or row["duration_s"] != 7200 or row["elapsed_s"] < 7200):
                raise ValueError("soak_timeline_duration")
        if churn["started_s"] < fixed["started_s"] + fixed["elapsed_s"]:
            raise ValueError("soak_timeline_overlap")
    return dict(state="pass", performance_qualified=False)


def _measured(reports, mode):
    operations, images, image_count = set(), {}, 0
    for phase in ("fixed", "churn"):
        for wrapper in reports[mode + "-" + phase + "-summary.json"]["results"]:
            row = wrapper["request"]
            operation = row.get("operation_id")
            if not isinstance(operation, str) or not operation or operation in operations:
                raise ValueError("soak_operation_identity")
            operations.add(operation)
            if row.get("category") == "image":
                image_count += 1
                for proof in _image_proof(row):
                    identity, digest = proof["id"], proof["sha256"]
                    if identity in images and images[identity] != digest:
                        raise ValueError("soak_image_identity_changed")
                    images[identity] = digest
    return operations, images, image_count


def evaluate_completed_soak(campaign, expected_identity, *, growth_review=None):
    """Campaign must come from read_completed_campaign; expected identity is independent."""
    gates = {}
    resource_result = None

    def run(name, function, *args, **kwargs):
        try:
            value = function(*args, **kwargs)
        except (ValueError, TypeError, KeyError, AttributeError, IndexError) as error:
            code = str(error)
            if not re.fullmatch(r"soak_[a-z_]+", code):
                code = "soak_invalid_evidence"
            value = dict(state="incomplete", reason=code, performance_qualified=False)
        gates[name] = value
        return value

    try:
        identity, settings = campaign["identity"], campaign["settings"]
    except (TypeError, KeyError):
        gates["provenance"] = dict(state="incomplete", reason="soak_campaign_shape")
        return summarize_acceptance(gates)
    if run("provenance", evaluate_provenance, identity, expected_identity, settings)["state"] != "pass":
        return summarize_acceptance(gates)
    if run("bindings", evaluate_evidence_bindings, campaign)["state"] != "pass":
        return summarize_acceptance(gates)
    reports, events = campaign["reports"], campaign["events"]
    if run("timeline", _timeline, reports)["state"] != "pass":
        return summarize_acceptance(gates)
    proxy_phases = [reports["proxy-" + phase + "-summary.json"] for phase in ("fixed", "churn")]
    controls = run("controls", evaluate_controls, reports["proxy-controls.json"],
                   phase_reports=dict(zip(("fixed", "churn"), proxy_phases)),
                   snapshots=[e for e in events if e.get("event") == "stage_restart_snapshot"],
                   expected_rule_count=53)
    for phase in ("fixed", "churn"):
        proxy, bypass = [reports[mode + "-" + phase + "-summary.json"] for mode in ("proxy", "bypass")]
        run(phase + "_trace", evaluate_phase_trace, proxy, bypass)
        run(phase + "_text", evaluate_text_windows, proxy, bypass)
    try:
        measured = {mode: _measured(reports, mode) for mode in ("proxy", "bypass")}
        if measured["proxy"][1] != measured["bypass"][1] or len(measured["proxy"][1]) != 400:
            raise ValueError("soak_corpus_coverage")
        gates["corpus"] = dict(state="pass", measured_images=400, performance_qualified=False)
    except (ValueError, KeyError, TypeError) as error:
        gates["corpus"] = dict(state="incomplete", reason="soak_corpus_coverage", performance_qualified=False)
        return summarize_acceptance(gates)
    for mode in ("proxy", "bypass"):
        operations, images, _ = measured[mode]
        run(mode + "_warmup", evaluate_warmup, reports[mode + "-warmup.json"], mode=mode,
            measured_operation_ids=operations, measured_image_ids=set(images))
    if controls["state"] == "pass":
        faults, restarts = controls["fault_intervals"], controls["restart_intervals"]
        fixed, churn = proxy_phases
        started, ended = fixed["started_s"], churn["started_s"] + churn["elapsed_s"]
        stage_records = reports["proxy-stage-records.json"]["records"]
        run("stage_coverage", evaluate_stage_coverage, stage_records,
            expected_images=measured["proxy"][2], started_s=started, ended_s=ended,
            poll_errors=[e for e in events if e.get("event") == "stage_poll_error"],
            restart_intervals=[(r["started_s"], r["ended_s"]) for r in restarts])
        for phase, report in zip(("fixed", "churn"), proxy_phases):
            run(phase + "_extraction", evaluate_extraction_windows, stage_records,
                started_s=report["started_s"], duration_s=report["duration_s"], fault_intervals=faults)
        resources = [e for e in events if e.get("event") == "resource"]
        run("resource_series", evaluate_resource_series, resources, started_s=started,
            ended_s=ended, restart_intervals=restarts)
        resource_result = run("fixed_resources", evaluate_fixed_resources, resources,
                              started_s=started, duration_s=fixed["duration_s"], restart_intervals=restarts)
        run("churn_resources", evaluate_churn_resources, resources,
            started_s=churn["started_s"], duration_s=churn["duration_s"], elapsed_s=churn["elapsed_s"],
            mapping_evidence=churn["mapping_evidence"])
        # Sustained positive growth needs an evidence-backed investigation, even below 10%.
    summary = reports["proxy-mode-summary.json"]
    run("idle_recovery", evaluate_idle_recovery, summary.get("baseline_idle"), summary.get("after_idle"),
        summary.get("baseline_gc"), summary.get("after_gc"))
    for mode in ("proxy", "bypass"):
        try:
            outcomes = len(reports[mode + "-warmup.json"]["results"]) + sum(
                len(reports[mode + "-" + phase + "-summary.json"]["results"]) for phase in ("fixed", "churn"))
            run(mode + "_finalization", evaluate_finalization, reports[mode + "-finalization.json"],
                mode=mode, expected_outcomes=outcomes, expected_generations=settings["control_count"] + 1,
                expected_children=settings["control_count"] + 2 if mode == "proxy" else 2,
                minimum_protected_values=44001)
        except (TypeError, KeyError):
            gates[mode + "_finalization"] = dict(state="incomplete", reason="soak_finalization_shape")
    if resource_result is not None:
        run("growth_review", evaluate_growth_review, campaign, expected_identity, resource_result, growth_review,
            prerequisites={name: gate for name, gate in gates.items() if name != "growth_review"},
            required_prerequisites=REQUIRED_GATES - {"growth_review"})
    return summarize_acceptance(gates)
