"""Bounded real-binary correctness smoke; never performance qualification."""
import json
import os
from pathlib import Path
import signal
import sys
import tempfile

from performance_campaign import execute_command
from performance_stats import OUTCOMES


def validate_report(report):
    phases = report.get("measurements", [])
    if report.get("performance_qualified") is not False or len(phases) != 2:
        raise ValueError("smoke_phase_coverage")
    if sorted(p.get("mode", "") for p in phases) != ["bypass", "proxy"]:
        raise ValueError("smoke_arm_coverage")
    for phase in phases:
        if any(phase.get(key) != expected for key, expected in (
                ("scheduled", 1), ("sent", 1), ("completed", 1),
                ("client_unsent", 0), ("supported_success", 1),
                ("initial_vault_population", 10000))):
            raise ValueError("smoke_operation_accounting")
        rows = phase.get("records", [])
        if (len(rows) != 1 or rows[0].get("outcome") != "supported_success"
                or len(rows[0].get("prefix_ms", [])) != 32):
            raise ValueError("smoke_stream_or_boundary")
        if phase["mode"] == "proxy":
            if any(phase.get(key) != expected for key, expected in (
                    ("final_vault_population", 10001), ("observed_new_pairs", 1),
                    ("verified_new_pairs", 1))):
                raise ValueError("smoke_growth_evidence")
        elif phase.get("final_vault_population") != 10000:
            raise ValueError("smoke_bypass_population")


def _metadata(report):
    phases = report.get("measurements", [])
    print("smoke_retained_phases", len(phases))
    for index, phase in enumerate(phases):
        counts = {key: phase.get(key) for key in (
            "warmup_completed", "scheduled", "sent", "client_unsent",
            "initial_vault_population", "final_vault_population", "observed_new_pairs",
            "verified_new_pairs") if type(phase.get(key)) in (int, float)}
        outcomes = {name: sum(row.get("outcome") == name for row in phase.get("records", []))
                    for name in sorted(OUTCOMES)}
        print("smoke_phase", index, "counts", counts, "outcomes", outcomes)


def main():
    root = Path(__file__).resolve().parents[2]
    os.chdir(root)
    private = Path(tempfile.mkdtemp(prefix="cui-churn-smoke-"))
    private.chmod(0o700)
    report_path = private / "report.json"
    def interrupted(signum, frame):
        raise TimeoutError("smoke_interrupted")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGALRM):
        signal.signal(signum, interrupted)
    signal.alarm(300)
    stage = "build_proxy"
    try:
        for name, package in (("proxy", "./cmd/cui-proxy"), ("seed", "./scripts/workflow/seedvault")):
            stage = "build_" + name
            if execute_command(["go", "build", "-o", str(private / name), package],
                               private / (name + ".log")):
                raise ValueError("smoke_build_failed")
        stage = "native_churn"
        code = execute_command([
            sys.executable, "scripts/workflow/performance_churn.py",
            "--binary", str(private / "proxy"), "--seed-binary", str(private / "seed"),
            "--report", str(report_path), "--duration", "0.6", "--size", "262144",
            "--warmup-seconds", "0", "--warmup-count", "4", "--repetitions", "1"],
            private / "churn.log")
        report = json.loads(report_path.read_text())
        _metadata(report)
        if code:
            raise ValueError("smoke_runner_failed")
        stage = "verify_report"
        validate_report(report)
    except BaseException:
        print("churn_smoke_failed", stage)
        return 1
    finally:
        signal.alarm(0)
    print("churn_smoke_passed", "measured_operations", 2, "performance_qualified", False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
