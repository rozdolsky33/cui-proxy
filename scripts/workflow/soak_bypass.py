"""Matched bypass phases; final identity/privacy checks belong to the campaign."""
import math
from pathlib import Path
import time

from soak_executor import ScenarioExecutor
from soak_fixture_replay import bind_fixture_replay
from soak_phase import PhaseFailure
from soak_recorded import run_recorded_phase
from soak_warmup import WarmupFailure, run_warmup


def run_bypass_phases(env, workload, corpus, fixtures, campaign, proxy_reports, *,
                      warmup_seconds=60, warmup_count=100, drain_seconds=45,
                      clock=time.monotonic):
    """Replay completed proxy traces with the same attested fixture identities.

    The caller supplies reports authenticated against linked campaign artifacts.
    This function deliberately cannot declare performance qualification.
    """
    if not isinstance(proxy_reports, dict) or set(proxy_reports) != {"fixed", "churn"}:
        raise ValueError("bypass requires both proxy phases")
    for phase, report in proxy_reports.items():
        if (not isinstance(report, dict) or report.get("completed") is not True
                or report.get("phase") != phase
                or type(report.get("duration_s")) not in (int, float)
                or not math.isfinite(report["duration_s"]) or report["duration_s"] <= 0
                or not isinstance(report.get("schedule"), list)):
            raise ValueError("invalid proxy phase for bypass")
    record = lambda event: campaign.writer({**event, "mode": "bypass"})
    executors = {}
    replay_executors = {}
    warmup_executor = ScenarioExecutor(env.upstream_url, env.upstream_url, workload, env.mappings,
                                       [], Path(fixtures), bypass=True, corpus=corpus, warmup=True)
    # Resolve every measured fixture proof before even warming up the backend.
    for phase in ("fixed", "churn"):
        executors[phase] = ScenarioExecutor(env.upstream_url, env.upstream_url, workload, env.mappings,
                                            [], Path(fixtures), bypass=True, corpus=corpus)
        replay_executors[phase] = bind_fixture_replay(executors[phase], proxy_reports[phase], corpus)
    try:
        warmup = run_warmup(warmup_executor, minimum_seconds=warmup_seconds,
                            minimum_count=warmup_count, clock=clock, record=record)
    except WarmupFailure as error:
        campaign.save_report("bypass-warmup.json", error.report)
        raise
    campaign.save_report("bypass-warmup.json", warmup)
    reports = {}
    for phase in ("fixed", "churn"):
        record({"event": "phase_start", "phase": phase})
        try:
            report = run_recorded_phase(
                replay_executors[phase], phase, campaign.root / f"bypass-{phase}",
                duration=proxy_reports[phase]["duration_s"], clock=clock,
                replay=proxy_reports[phase]["schedule"],
                drain_seconds=drain_seconds if phase == "churn" else 0)
        except PhaseFailure as error:
            campaign.save_report(f"bypass-{phase}-summary.json", error.report)
            raise
        campaign.save_report(f"bypass-{phase}-summary.json", report)
        if report.get("completed") is not True:
            raise PhaseFailure(report)
        campaign.link_phase(f"bypass-{phase}")
        reports[phase] = report
        record({"event": "phase_end", "phase": phase})
    absence = {phase: executor.verify_final_absence() for phase, executor in executors.items()}
    summary = {"phases_completed": list(reports), "final_absence": absence,
               "lifecycle": {phase: executor.state.summary() for phase, executor in executors.items()},
               "finalization_pending": True, "performance_qualified": False}
    campaign.save_report("bypass-mode-summary.json", summary)
    return {"warmup": warmup, "phases": reports, "summary": summary, "executors": executors}
