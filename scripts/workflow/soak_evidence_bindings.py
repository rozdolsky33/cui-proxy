"""Cross-check sealed phase journals and root events against retained summaries."""


def _without(row, *keys):
    return {key: value for key, value in row.items() if key not in keys}


def _index(rows):
    if not isinstance(rows, list):
        raise ValueError("soak_binding_rows")
    result = {}
    for row in rows:
        identity = row.get("identity")
        if (not isinstance(identity, list) or len(identity) != 3
                or identity[0] not in ("fixed", "churn")
                or any(type(value) is not int or value < 0 for value in identity[1:])):
            raise ValueError("soak_binding_identity")
        key = tuple(identity)
        if key in result:
            raise ValueError("soak_binding_duplicate_identity")
        result[key] = row
    return result


def _match_phase(assigned, completed, report, mode, phase):
    plans, summary_plans = _index(assigned), _index(report["schedule"])
    outcomes, summary_outcomes = _index(completed), _index(report["results"])
    if plans != summary_plans or set(plans) != set(outcomes) or set(outcomes) != set(summary_outcomes):
        raise ValueError("soak_binding_phase_summary")
    for identity, sealed in outcomes.items():
        summary = summary_outcomes[identity]
        if sealed == summary:
            continue
        before, after = sealed["request"], summary["request"]
        mappings = report.get("mapping_evidence", {})
        # Churn boundaries are reconciled only after the persisted vault is inspected.
        # The aggregate separately validates the complete mapping evidence.
        permitted = (mode == "proxy" and phase == "churn"
                     and bool(plans[identity].get("churn_indices"))
                     and before.get("outcome") == "boundary_pending"
                     and before.get("boundary_verified") is not True
                     and all(before.get(key) is True for key in
                             ("completed", "semantic_verified", "upstream_receipt_verified"))
                     and after == dict(before, outcome="supported_success", boundary_verified=True)
                     and _without(sealed, "request") == _without(summary, "request")
                     and type(mappings.get("unverified_text_boundaries")) is int
                     and mappings["unverified_text_boundaries"] == 0 and mappings.get("unmet_requirements") == [])
        if not permitted:
            raise ValueError("soak_binding_completion_summary")


def evaluate_evidence_bindings(campaign):
    try:
        return _evaluate(campaign)
    except (KeyError, TypeError, AttributeError) as error:
        raise ValueError("soak_binding_shape") from error


def _evaluate(campaign):
    reports, phases, events = (campaign[key] for key in ("reports", "phases", "events"))
    if not isinstance(events, list) or any(not isinstance(e, dict) for e in events):
        raise ValueError("soak_binding_events")
    expected_lifecycle = []
    expected_phases = {mode + "-" + phase for mode in ("proxy", "bypass")
                       for phase in ("fixed", "churn")}
    if set(phases) != expected_phases:
        raise ValueError("soak_binding_phases")
    for mode in ("proxy", "bypass"):
        for phase in ("fixed", "churn"):
            name = mode + "-" + phase
            journal, report = phases[name], reports[name + "-summary.json"]
            if (journal["run_completed"] is not True or journal["framing_complete"] is not True
                    or type(journal["trailing_bytes"]) is not int or journal["trailing_bytes"] != 0
                    or report["completed"] is not True or report["phase"] != phase):
                raise ValueError("soak_binding_phase_completion")
            rows = journal["events"]
            if (not isinstance(rows, list) or not rows
                    or any(e.get("event") not in ("assigned", "completed") for e in rows)):
                raise ValueError("soak_binding_phase_events")
            assigned = [_without(e, "event") for e in rows if e["event"] == "assigned"]
            completed = [_without(e, "event") for e in rows if e["event"] == "completed"]
            _match_phase(assigned, completed, report, mode, phase)
            expected_lifecycle.extend([(mode, phase, "phase_start"), (mode, phase, "phase_end")])
        finalized = [e for e in events if e.get("event") == "mode_finalized" and e.get("mode") == mode]
        if (len(finalized) != 1 or finalized[0].get("identity_verified") is not True
                or finalized[0].get("corpus_verified") is not True):
            raise ValueError("soak_binding_mode_identity")
    lifecycle = [(e.get("mode"), e.get("phase"), e["event"]) for e in events
                 if e.get("event") in ("phase_start", "phase_end")]
    # Proxy ordering is also sealed by the ordered child registrations. Older
    # collectors emit explicit phase markers only for the bypass replay.
    bypass_lifecycle = [row for row in expected_lifecycle if row[0] == "bypass"]
    if lifecycle not in (expected_lifecycle, bypass_lifecycle):
        raise ValueError("soak_binding_phase_order")
    stage_events = [e for e in events if e.get("event") == "request_stages"]
    if any(e.get("mode") != "proxy" for e in stage_events):
        raise ValueError("soak_binding_stage_mode")
    stages = [dict(e["request"], observed_s=e["observed_s"],
                   observation_lower_s=e["observation_lower_s"]) for e in stage_events]
    stage_report = reports["proxy-stage-records.json"]
    errors = [e for e in events if e.get("event") == "stage_poll_error"]
    if (stages != stage_report["records"] or type(stage_report["poll_errors"]) is not int
            or stage_report["poll_errors"] != len(errors)
            or any(e.get("mode") != "proxy" or e.get("poll_errors") != i + 1
                   for i, e in enumerate(errors))):
        raise ValueError("soak_binding_stage_summary")
    control_events = [e for e in events if e.get("event") == "control_completed"]
    if any(e.get("mode") != "proxy" or e.get("control") not in ("restart", "reload")
           for e in control_events):
        raise ValueError("soak_binding_control_mode")
    for kind, key in (("restart", "restarts"), ("reload", "reloads")):
        rows = [_without(e, "mode") for e in control_events if e["control"] == kind]
        if rows != reports["proxy-controls.json"][key]:
            raise ValueError("soak_binding_control_summary")
    resources = [e for e in events if e.get("event") == "resource"]
    count = reports["proxy-mode-summary.json"]["resource_samples"]
    if (type(count) is not int or count != len(resources)
            or any(e.get("mode") != "proxy" for e in resources)):
        raise ValueError("soak_binding_resource_summary")
    return {"state": "pass", "phases_verified": 4, "stages_verified": len(stages),
            "resource_samples_verified": count, "performance_qualified": False}
