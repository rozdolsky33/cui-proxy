"""Run the isolated native cancellation sequence with observed evidence."""
from cancellation_acceptance import _helper, _number, _window
from cancellation_evidence import evaluate_trial_evidence


def _identities(observation):
    helpers = observation.get("helpers")
    if not isinstance(helpers, list):
        raise ValueError("cancellation_probe_helpers")
    identities = {_helper(helper) for helper in helpers}
    if len(identities) != len(helpers):
        raise ValueError("cancellation_probe_duplicate_helper")
    return identities


def _state(observation, active, queued, markers):
    return (type(observation.get("active")) is int and observation["active"] == active
            and type(observation.get("queued")) is int and observation["queued"] == queued
            and type(observation.get("marker_count")) is int and observation["marker_count"] == markers
            and len(_identities(observation)) == active)


def _wait(driver, predicate, deadline):
    while driver.clock() <= deadline:
        observed = driver.observe()
        if observed.get("gate_armed") is not True:
            raise ValueError("cancellation_probe_gate_released")
        started, ended = _window(observed)
        if ended > deadline:
            raise ValueError("cancellation_probe_observation_deadline")
        if predicate(observed):
            return observed
        driver.sleep(0.01)
    raise ValueError("cancellation_probe_state_deadline")


def _transport_finished(driver, role, deadline):
    ended = driver.transport(role)
    if ended is None:
        return False
    if _number(ended) > deadline:
        raise ValueError("cancellation_probe_transport_deadline")
    return True


def run_trial(driver, retained=None):
    """Run one trial and always release/restore/join through driver cleanup.

    The driver owns native I/O. Its transport observation must describe the
    worker's socket close, never completion of later semantic-oracle polling.
    A caller-supplied dictionary retains partial evidence when any step fails.
    """
    report = {} if retained is None else retained
    if not isinstance(report, dict) or report:
        raise ValueError("cancellation_probe_fresh_record")
    report["completed"] = False
    report["bounds"] = {"gate_remained_armed": True}
    try:
        driver.arm()
        active_started = driver.start("active")
        deadline = _number(active_started) + 45
        first = _wait(driver, lambda row: _state(row, 1, 0, 1), deadline)
        cancelled_helper = first["helpers"][0]
        driver.start("survivor")
        second = _wait(driver, lambda row: _state(row, 2, 0, 2)
                       and _helper(cancelled_helper) in _identities(row), deadline)
        survivor_helper = next(helper for helper in second["helpers"]
                               if _helper(helper) != _helper(cancelled_helper))
        originals = _identities(second)
        driver.start("text")
        driver.start("queued")
        queued_before = _wait(driver, lambda row: _state(row, 2, 1, 2)
                             and _identities(row) == originals, deadline)
        queued_audit_before = driver.audit()
        queued = driver.cancel("queued")
        report["bounds"]["queued"] = queued
        queued["before"] = queued_before
        queued_deadline = _number(queued["cancel_s"]) + 1
        queued["after"] = _wait(
            driver, lambda row: _state(row, 2, 0, 2) and _identities(row) == originals
            and _transport_finished(driver, "queued", queued_deadline), queued_deadline)
        queued["transport_end_s"] = driver.transport("queued")
        queued_audit_after = driver.audit_after(queued_audit_before, "attachment_image_busy", deadline)
        report["audit"] = {"queued": {"before": queued_audit_before, "after": queued_audit_after}}
        driver.start("replacement")
        active_before = _wait(driver, lambda row: _state(row, 2, 1, 2)
                             and _identities(row) == originals, deadline)
        active_audit_before = driver.audit()
        active = driver.cancel("active")
        report["bounds"]["active"] = active
        active.update(before=active_before, cancelled_helper=cancelled_helper, survivor_helper=survivor_helper)
        active_deadline = _number(active["cancel_s"]) + 2

        def admitted(row):
            if not _state(row, 2, 0, 3):
                return False
            identities = _identities(row)
            if (_helper(survivor_helper) not in identities or _helper(cancelled_helper) in identities
                    or len(identities - originals) != 1):
                return False
            stopped = [entry for entry in row.get("stopped_helpers", [])
                       if _helper(entry.get("helper")) == _helper(cancelled_helper)
                       and entry.get("state") in ("absent", "zombie")]
            return len(stopped) == 1 and _transport_finished(driver, "active", active_deadline)

        after = _wait(driver, admitted, active_deadline)
        active["after"] = after
        active["transport_end_s"] = driver.transport("active")
        active["stopped"] = next(entry for entry in after["stopped_helpers"]
                                 if _helper(entry["helper"]) == _helper(cancelled_helper))
        replacement = next(helper for helper in after["helpers"] if _helper(helper) not in originals)
        active["admitted"] = {"helper": replacement, "started_s": after["started_s"], "ended_s": after["ended_s"]}
        active_audit_after = driver.audit_after(active_audit_before, "attachment_image_timeout", deadline)
        report["audit"]["active"] = {"before": active_audit_before, "after": active_audit_after}
        report["release_s"] = driver.release()
        report["operations"], report["threads_joined"] = driver.finish(deadline)
        report["receipts"], report["receipt_observation"] = driver.receipts(report["operations"])
        report["final_helpers"] = driver.final_helpers()
        evaluate_trial_evidence(report)
    finally:
        driver.cleanup()
    report["completed"] = True
    return report
