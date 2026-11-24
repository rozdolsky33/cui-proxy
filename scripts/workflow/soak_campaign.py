"""Persistent sustained-campaign orchestration; acceptance is evaluated separately."""
import threading
import time
from pathlib import Path
from soak_stages import SoakStageCollector as StageCollector
from performance_churn import inspect_vault
from soak_controls import ControlledExecutor,control_times
from soak_executor import ScenarioExecutor
from soak_mapping_evidence import finalize_mapping_evidence
from soak_phase import PhaseFailure
from soak_recorded import run_recorded_phase
from soak_reload import PolicyReloader
from soak_resources import ResourceMonitor,measure_resources,observe_idle_gc,wait_for_idle
from soak_warmup import WarmupFailure,run_warmup


class CampaignModeFailure(RuntimeError):
    pass


def run_proxy_phases(env,workload,corpus,fixtures,campaign,*,fixed_seconds=7200,
                     churn_seconds=7200,warmup_seconds=60,warmup_count=100,
                     drain_seconds=45,control_count=10,clock=time.monotonic):
    """Run both measured phases on the caller's single retained environment.

    The caller owns environment cleanup, audit/privacy reconciliation, source
    attestation and subsequent matched bypass. This function cannot qualify or
    finish the root campaign on its own.
    """
    if type(control_count) is not int or control_count<0:
        raise ValueError("campaign_control_count")
    record=lambda event:campaign.writer({**event,"mode":"proxy"})
    warmer=ScenarioExecutor(env.url,env.upstream_url,workload,env.mappings,[],
                            fixtures,corpus=corpus,warmup=True)
    try:
        warmup=run_warmup(warmer,minimum_seconds=warmup_seconds,minimum_count=warmup_count,
                          clock=clock,record=record)
    except WarmupFailure as error:
        campaign.save_report("proxy-warmup.json",error.report)
        raise
    campaign.save_report("proxy-warmup.json",warmup)
    baseline_idle=wait_for_idle(env.metrics,record=record)
    baseline_gc=observe_idle_gc(env.metrics,record=record)
    initial=inspect_vault(env.directory,env.seed_binary,observation_name="campaign-initial.json")["mappings"]
    executors={phase:ScenarioExecutor(env.url,env.upstream_url,workload,initial,[],
                                      fixtures,corpus=corpus) for phase in ("fixed","churn")}
    reloader=PolicyReloader(env)
    control_horizon=fixed_seconds+churn_seconds
    deadlines=control_times(control_horizon,control_count) if control_count else []
    controlled=ControlledExecutor(lambda plan:executors[plan["identity"][0]](plan),env,
                                  reloader,record,restart_times=deadlines,
                                  reload_times=deadlines,clock=clock, image_checkpoint=lambda expected: stages.before_restart(expected_image_completions=expected))
    stop=threading.Event()
    reports={}
    current_mappings=initial
    warm_sessions = {row.get("identity", {}).get("session")
                     for row in env.metrics().get("recent_requests", [])}
    warm_sessions.discard(None)
    stages = None
    try:
        with ResourceMonitor(measure_resources(env),record,phase_stop=stop,clock=clock) as resources, StageCollector(env, warm_sessions, record=record, phase_stop=stop, clock=clock) as stages:
            for phase,duration in (("fixed",fixed_seconds),("churn",churn_seconds)):
                resources.raise_if_failed()
                executors[phase].mappings=current_mappings
                record({"type":"phase_start","phase":phase,"horizon_seconds":duration})
                try:
                    report=run_recorded_phase(controlled,phase,Path(campaign.root)/f"proxy-{phase}",
                                              duration=duration,clock=clock,stop=stop,
                                              drain_seconds=drain_seconds if phase=="churn" else 0)
                except PhaseFailure as error:
                    campaign.save_report(f"proxy-{phase}-summary.json",error.report)
                    resources.raise_if_failed()
                    raise
                final=inspect_vault(env.directory,env.seed_binary,
                                    observation_name=f"campaign-{phase}-final.json")["mappings"]
                report=finalize_mapping_evidence(report,workload,current_mappings,final)
                campaign.save_report(f"proxy-{phase}-summary.json",report)
                resources.raise_if_failed()
                if report.get("completed") is not True or report["mapping_evidence"]["unmet_requirements"]:
                    raise CampaignModeFailure("campaign_mapping_incomplete")
                campaign.link_phase(f"proxy-{phase}")
                reports[phase]=report
                current_mappings=final
                record({"type":"phase_end","phase":phase})
            controls=controlled.summary()
            campaign.save_report("proxy-controls.json",controls)
            if (controls["failed"] or controls["restarts_completed"]!=control_count
                    or controls["reloads_completed"]!=control_count):
                raise CampaignModeFailure("campaign_controls_incomplete")
            after_idle=wait_for_idle(env.metrics,baseline=baseline_idle["goroutines"],record=record)
            after_gc=observe_idle_gc(env.metrics,record=record)
            absence={phase:executor.verify_final_absence() for phase,executor in executors.items()}
    finally:
        if stages is not None:
            campaign.save_report("proxy-stage-records.json",
                                 {"records": stages.records, "poll_errors": stages.errors})
    summary=dict(phases_completed=list(reports),baseline_idle=baseline_idle,
                 baseline_gc=baseline_gc,after_idle=after_idle,after_gc=after_gc,
                 resource_samples=resources.samples,final_absence=absence,
                 lifecycle={phase:executor.state.summary() for phase,executor in executors.items()},
                 finalization_pending=True,performance_qualified=False)
    campaign.save_report("proxy-mode-summary.json",summary)
    return dict(warmup=warmup,phases=reports,summary=summary,controls=controls,
                executors=executors,final_mappings=current_mappings)
