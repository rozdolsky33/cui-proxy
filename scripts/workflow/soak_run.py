"""Persistent campaign coordination with explicit finalization boundaries."""
import copy
from pathlib import Path

from performance import Environment
from soak_bypass import run_bypass_phases
from soak_campaign import run_proxy_phases
from soak_campaign_evidence import CampaignEvidence
from soak_corpus_attestation import attest_corpus, load_manifest


def verify_cleanup(env):
    """The retained directory survives, but its owned processes and logs must not."""
    if any(child.poll() is None for child in env.children):
        raise RuntimeError("campaign child remains running after cleanup")
    if any(not log.closed for log in env.logs):
        raise RuntimeError("campaign log remains open after cleanup")
    return {"children_stopped": len(env.children), "logs_closed": len(env.logs)}


def run_campaign(root, binary, seed_binary, workload, fixtures, *, identity_probe,
                 finalize_mode, fixed_seconds=7200, churn_seconds=7200,
                 warmup_seconds=60, warmup_count=100, drain_seconds=45,
                 control_count=10):
    """Compose authenticated phases; the caller supplies strict identity/audit checks.

    identity_probe must return stable, validated source/build/host/workload facts.
    finalize_mode runs after environment cleanup and must verify audit and privacy
    evidence, returning a metadata report with verified=True. Neither callback is
    optional. The separate acceptance evaluator owns performance qualification.
    """
    if not callable(identity_probe) or not callable(finalize_mode):
        raise TypeError("campaign requires identity and finalization checks")
    root, fixtures = Path(root), Path(fixtures)
    identity = copy.deepcopy(identity_probe())
    if not isinstance(identity, dict) or not identity:
        raise ValueError("campaign identity is required")
    corpus, corpus_proof = attest_corpus(fixtures, load_manifest(fixtures))
    corpus_proof = copy.deepcopy(corpus_proof)
    settings = dict(fixed_seconds=fixed_seconds, churn_seconds=churn_seconds,
                    warmup_seconds=warmup_seconds, warmup_count=warmup_count,
                    drain_seconds=drain_seconds, control_count=control_count)
    summary = {"completed": False, "performance_qualified": False, "modes": {}}
    with CampaignEvidence(root, {**identity, "corpus": corpus_proof}, settings) as campaign:
        for mode in ("proxy", "bypass"):
            reports = None
            if mode == "bypass":
                reports = {phase: campaign.load_report(f"proxy-{phase}-summary.json")
                           for phase in ("fixed", "churn")}
            with Environment(binary, seed_binary, workload, 768 * 1024, correlate=True,
                             retained_directory=root / f"{mode}-environment") as env:
                if mode == "proxy":
                    result = run_proxy_phases(env, workload, corpus, fixtures, campaign, **settings)
                else:
                    result = run_bypass_phases(
                        env, workload, corpus, fixtures, campaign, reports,
                        warmup_seconds=warmup_seconds, warmup_count=warmup_count,
                        drain_seconds=drain_seconds)
            cleanup = verify_cleanup(env)
            finalized = finalize_mode(env, mode, result)
            if not isinstance(finalized, dict) or finalized.get("verified") is not True:
                raise ValueError("campaign mode finalization did not verify evidence")
            campaign.save_report(f"{mode}-finalization.json", {**finalized, "cleanup": cleanup})
            observed = identity_probe()
            if observed != identity:
                raise ValueError("campaign source/build/host identity changed")
            _, current_corpus = attest_corpus(fixtures, load_manifest(fixtures))
            if current_corpus != corpus_proof:
                raise ValueError("campaign corpus identity changed")
            campaign.writer({"event": "mode_finalized", "mode": mode,
                             "identity_verified": True, "corpus_verified": True})
            summary["modes"][mode] = {"completed": True, "finalized": True}
        summary["completed"] = True
        campaign.save_report("campaign-summary.json", summary)
        campaign.finish()
    return summary
