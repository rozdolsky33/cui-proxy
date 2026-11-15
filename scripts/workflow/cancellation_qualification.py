"""Acceptance for the independently bound three-trial cancellation artifact."""
from cancellation_acceptance import _helper, _number, _window
from cancellation_evidence import evaluate_trial_evidence
from soak_provenance_acceptance import evaluate_provenance
from soak_warmup_acceptance import evaluate_warmup


def _count(value, minimum):
    if type(value) is not int or value < minimum:
        raise ValueError("cancellation_finalization_count")
    return value


def _process(value):
    if not isinstance(value, dict):
        raise ValueError("cancellation_cleanup_process")
    pid, start = value.get("pid"), value.get("start")
    if (type(pid) is not int or pid <= 0 or not isinstance(start, str)
            or not start.strip() or not start.isprintable() or len(start) > 128):
        raise ValueError("cancellation_cleanup_process_identity")
    return pid, start


def _terminal(value, started, ended):
    if (not isinstance(value, dict) or value.get("state") not in ("absent", "zombie")
            or not started <= _number(value.get("observed_s")) <= ended):
        raise ValueError("cancellation_cleanup_terminal")


def evaluate_cancellation(report, expected_identity):
    """Qualify cancellation only; require an independent initial identity."""
    try:
        return _evaluate(report, expected_identity)
    except (KeyError, TypeError, AttributeError) as error:
        raise ValueError("cancellation_artifact_shape") from error


def _evaluate(report, expected_identity):
    if (not isinstance(report, dict) or type(report.get("schema_version")) is not int
            or report["schema_version"] != 1 or report.get("completed") is not True
            or report.get("mode") != "proxy" or report.get("collector_completed") is not False
            or report.get("performance_qualified") is not False):
        raise ValueError("cancellation_artifact_scope")
    provenance = evaluate_provenance(report.get("identity"), expected_identity, report.get("settings"))
    if provenance.get("identity_verified") is not True or provenance.get("settings_verified") is not True:
        raise ValueError("cancellation_artifact_provenance")
    trials = report.get("trials")
    if not isinstance(trials, list) or len(trials) != 3:
        raise ValueError("cancellation_artifact_trials")
    previous_end = warmup_end = _number(report.get("warmup_ended_s"))
    operations, audits, images, helpers, generations, parents = set(), set(), set(), set(), set(), set()
    for trial in trials:
        if not isinstance(trial, dict) or trial.get("completed") is not True:
            raise ValueError("cancellation_artifact_partial_trial")
        result = evaluate_trial_evidence(trial)
        first_started = min(_number(row["started_s"]) for row in trial["operations"].values())
        if first_started < previous_end:
            raise ValueError("cancellation_artifact_trial_order")
        previous_end = result["observed_through_s"]
        operation_ids, audit_ids = set(result["operation_ids"]), set(result["audit_ids"])
        if operations & operation_ids or audits & audit_ids:
            raise ValueError("cancellation_artifact_reused_identity")
        operations.update(operation_ids)
        audits.update(audit_ids)
        images.update(result["image_ids"])
        trial_generations = {helper["generation"] for helper in trial["final_helpers"]}
        if len(trial_generations) != 1 or trial_generations & generations:
            raise ValueError("cancellation_artifact_generation")
        generations.update(trial_generations)
        for helper in trial["final_helpers"]:
            identity = _helper(helper)
            if identity in helpers:
                raise ValueError("cancellation_artifact_reused_helper")
            helpers.add(identity)
            parents.add((helper["parent_pid"], helper["parent_start"]))
    if len(operations) != 15 or len(audits) != 6 or len(helpers) != 9 or len(parents) != 1:
        raise ValueError("cancellation_artifact_population")
    warmup = evaluate_warmup(
        report.get("warmup"), mode="proxy", measured_operation_ids=frozenset(operations),
        measured_image_ids=frozenset(images))
    if warmup_end < _number(warmup["elapsed_seconds"]):
        raise ValueError("cancellation_artifact_warmup_time")
    finalization = report.get("finalization")
    started, ended = _window(finalization)
    if started < previous_end:
        raise ValueError("cancellation_artifact_early_finalization")
    privacy, cleanup = finalization.get("privacy"), finalization.get("cleanup")
    if not isinstance(privacy, dict) or privacy.get("privacy_verified") is not True:
        raise ValueError("cancellation_artifact_privacy")
    _count(privacy.get("files_checked"), 3)
    _count(privacy.get("protected_values_checked"), 40)
    if not isinstance(cleanup, dict) or any(cleanup.get(key) is not True for key in (
            "logs_closed", "gate_released", "factory_restored")):
        raise ValueError("cancellation_artifact_cleanup")
    if _count(cleanup.get("threads_joined"), 15) != 15:
        raise ValueError("cancellation_artifact_thread_count")
    children = cleanup.get("owned_children")
    if not isinstance(children, list) or len(children) != 2:
        raise ValueError("cancellation_artifact_owned_children")
    roles, child_pids = {}, set()
    for child in children:
        identity = _process(child)
        _terminal(child, started, ended)
        role = child.get("role")
        if role not in ("proxy", "backend") or role in roles or identity[0] in child_pids:
            raise ValueError("cancellation_artifact_child_identity")
        roles[role] = identity
        child_pids.add(identity[0])
    if roles["proxy"] not in parents:
        raise ValueError("cancellation_artifact_proxy_identity")
    terminal_helpers = cleanup.get("helpers_terminal")
    if not isinstance(terminal_helpers, list) or len(terminal_helpers) != 9:
        raise ValueError("cancellation_artifact_terminal_helpers")
    observed_helpers = set()
    for row in terminal_helpers:
        _terminal(row, started, ended)
        identity = _helper(row.get("helper"))
        if identity in observed_helpers:
            raise ValueError("cancellation_artifact_duplicate_terminal")
        observed_helpers.add(identity)
    if observed_helpers != helpers:
        raise ValueError("cancellation_artifact_helper_cleanup_identity")
    return {
        "state": "pass",
        "trials": 3,
        "operations": 15,
        "warmup_successful": warmup["successful"],
        "privacy_verified": True,
        "cleanup_verified": True,
        "cancellation_qualified": True,
        "performance_qualified": False,
    }
