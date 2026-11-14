"""Validate original recovery controls and retain exact fault intervals."""
import math

from soak_controls import control_times
from soak_lifecycle import lifecycle_step
from soak_schedule import plan_request


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def _integer(value, minimum=0):
    return type(value) is int and value >= minimum


def _identity(value):
    if (not isinstance(value, (list, tuple)) or len(value) != 3
            or value[0] not in ("fixed", "churn")
            or not _integer(value[1]) or value[1] > 3 or not _integer(value[2])):
        raise ValueError("control_request_identity")
    return tuple(value)


def evaluate_controls(report, *, phase_reports, snapshots, expected_rule_count):
    if not isinstance(report, dict) or report.get("failed") is not False:
        raise ValueError("control_incomplete")
    origin = report.get("clock_origin_s")
    if not _number(origin) or not _integer(expected_rule_count, 1):
        raise ValueError("control_reference")
    if not isinstance(phase_reports, dict) or set(phase_reports) != {"fixed", "churn"}:
        raise ValueError("control_phases")
    outcomes, bounds = {}, {}
    for name in ("fixed", "churn"):
        phase = phase_reports[name]
        if (not isinstance(phase, dict) or phase.get("phase") != name
                or phase.get("completed") is not True
                or type(phase.get("duration_s")) not in (int, float) or phase["duration_s"] != 7200
                or not _number(phase.get("elapsed_s")) or phase["elapsed_s"] < 7200
                or not _number(phase.get("started_s")) or phase["started_s"] < origin
                or not isinstance(phase.get("results"), list)):
            raise ValueError("control_phase_horizon")
        bounds[name] = (phase["started_s"], phase["started_s"] + phase["elapsed_s"])
        for row in phase["results"]:
            if not isinstance(row, dict):
                raise ValueError("control_outcome")
            identity = _identity(row.get("identity"))
            if identity[0] != name or identity in outcomes or not isinstance(row.get("request"), dict):
                raise ValueError("control_outcome_identity")
            outcomes[identity] = row["request"]
    if bounds["churn"][0] < bounds["fixed"][1]:
        raise ValueError("control_phase_overlap")
    intervals, restarts, identities = [], [], set()
    for kind in ("restart", "reload"):
        rows = report.get(kind + "s")
        count = report.get(kind + "s_planned")
        completed = report.get(kind + "s_completed")
        if (not _integer(count, 10) or not _integer(completed, 10) or completed != count
                or not isinstance(rows, list) or len(rows) != count):
            raise ValueError("control_counts")
        deadlines = control_times(14400, count)
        for index, row in enumerate(rows, 1):
            if (not isinstance(row, dict) or row.get("control") != kind
                    or type(row.get("index")) is not int or row["index"] != index
                    or not _integer(row.get("active_requests_before"), 1)):
                raise ValueError("control_under_load")
            identity = _identity(row.get("identity"))
            outcome = outcomes.get(identity)
            if (identity in identities or not isinstance(outcome, dict)
                    or any(outcome.get(key) is not True for key in ("completed", "semantic_verified"))):
                raise ValueError("control_semantic_outcome")
            identities.add(identity)
            start, end = row.get("started_s"), row.get("elapsed_s")
            if (not _number(start) or not _number(end) or start < deadlines[index-1]
                    or end <= start or origin + start < bounds[identity[0]][0]
                    or origin + end > bounds[identity[0]][1]):
                raise ValueError("control_interval")
            proof = row.get("evidence")
            if not isinstance(proof, dict):
                raise ValueError("control_proof")
            interval = dict(control=kind, index=index, started_s=origin+start, ended_s=origin+end)
            if kind == "restart":
                plan = plan_request(client=identity[1], serial=identity[2], phase=identity[0])
                if (plan["scenario"] != "W10" or lifecycle_step(plan)["stage"] != "continue"
                        or outcome.get("outcome") != "supported_success"
                        or type(proof.get("restart_index")) is not int or proof["restart_index"] != index
                        or any(proof.get(key) is not True for key in ("graceful_exit", "ready", "continuation_verified"))
                        or any(outcome.get(key) is not True for key in ("boundary_verified", "upstream_receipt_verified"))
                        or not _integer(proof.get("vault_before"), 10000)
                        or not _integer(proof.get("vault_after"), proof["vault_before"])
                        or not _number(proof.get("elapsed_s")) or not 0 < proof["elapsed_s"] <= end-start+1e-6):
                    raise ValueError("control_recovery_proof")
                restarts.append(interval)
            elif (type(proof.get("generation")) is not int or proof["generation"] != index
                  or proof.get("reload_returned_success") is not True
                  or type(proof.get("rule_count")) is not int or proof["rule_count"] != expected_rule_count):
                raise ValueError("control_policy_proof")
            intervals.append(interval)
    intervals.sort(key=lambda row: row["started_s"])
    if any(a["ended_s"] > b["started_s"] for a, b in zip(intervals, intervals[1:])):
        raise ValueError("control_overlap")
    if not isinstance(snapshots, list) or len(snapshots) != len(restarts):
        raise ValueError("control_snapshot_count")
    previous_count = -1
    for snapshot, interval in zip(snapshots, restarts):
        if (not isinstance(snapshot, dict) or snapshot.get("event") != "stage_restart_snapshot"
                or not _number(snapshot.get("observed_s"))
                or not interval["started_s"] <= snapshot["observed_s"] <= interval["ended_s"]
                or not _integer(snapshot.get("records")) or snapshot["records"] < previous_count):
            raise ValueError("control_snapshot_proof")
        previous_count = snapshot["records"]
    return dict(state="pass", fault_intervals=intervals, restart_intervals=restarts,
                restarts_verified=len(restarts), reloads_verified=len(report["reloads"]),
                performance_qualified=False)
