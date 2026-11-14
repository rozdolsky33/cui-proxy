"""Standalone original-warmup cancellation diagnostic and evidence retention."""
import argparse
import contextlib
import http.client
import inspect
import json
import os
from pathlib import Path
import shutil
import time
import traceback

import soak
import soak_campaign
from cancellation_gate import GatedOCR
from cancellation_native import NativeTrialDriver, read_processes
from cancellation_probe import run_trial
from cancellation_qualification import evaluate_cancellation
from soak_campaign_reader import _capture, read_report_bytes
from soak_evidence import read_sealed_evidence
from soak_rejections import verify_log_privacy
from soak_schedule import plan_request


class _DiagnosticComplete(Exception):
    pass


def _write(path, value):
    with path.open("x", encoding="utf-8") as stream:
        json.dump(value, stream, sort_keys=True, indent=2, allow_nan=False)
        stream.write("\n")


def _paths(root):
    suffixes = {
        "gate": ".cancellation-gate", "log": ".cancellation-driver.log",
        "partial": ".cancellation-partial.json", "report": ".cancellation.json",
        "evaluation": ".cancellation-evaluation.json", "expected": ".expected-identity.json",
        "failure": ".cancellation-failure.json", "failure_log": ".cancellation-failure.log",
    }
    paths = {key: root.parent / (root.name + suffix) for key, suffix in suffixes.items()}
    if not root.parent.is_dir() or any(os.path.lexists(path) for path in (root, *paths.values())):
        raise ValueError("cancellation_output_not_fresh")
    return paths


def trial_plans(index):
    result = {}
    for client, roles, category in ((0, ("active",), "image"), (1, ("survivor",), "image"),
                                    (2, ("queued", "replacement"), "image"), (3, ("text",), "text")):
        selected = []
        for serial in range(index * 100, (index + 1) * 100):
            plan = plan_request(client, serial, "fixed")
            if plan["category"] == category:
                selected.append(plan)
                if len(selected) == len(roles):
                    break
        if len(selected) != len(roles):
            raise ValueError("cancellation_trial_plan_population")
        result.update(zip(roles, selected))
    return result


def _terminal(identity, processes):
    current = processes.get(identity["pid"])
    if current is None or current["start"] != identity["start"]:
        return "absent"
    if "Z" in current["state"]:
        return "zombie"
    raise ValueError("cancellation_owned_process_still_running")


def _collector(root):
    journal = read_sealed_evidence(root / "events.jsonl", root / "seal.json")
    if (journal.get("framing_complete") is not True or journal.get("trailing_bytes") != 0
            or journal.get("run_completed") is not False):
        raise ValueError("cancellation_collector_scope")
    events = journal["events"]
    starts = [event for event in events if event.get("type") == "campaign_start"]
    warmups = [event for event in events if event.get("type") == "campaign_artifact"
               and event.get("name") == "proxy-warmup.json"]
    if len(starts) != 1 or len(warmups) != 1:
        raise ValueError("cancellation_collector_bindings")
    bound = warmups[0]
    warmup = read_report_bytes(_capture(root / bound["name"], expected_hash=bound["sha256"], expected_size=bound["bytes"]))
    return starts[0]["identity"], starts[0]["settings"], warmup


def run_probe(options, *, clock=time.monotonic):
    root = Path(options.output).absolute()
    paths = _paths(root)
    delegate = shutil.which("tesseract")
    if delegate is None:
        raise ValueError("cancellation_tesseract_missing")
    report = {"schema_version": 1, "completed": False, "mode": "proxy",
              "collector_completed": False, "performance_qualified": False, "trials": []}
    retained = {"drivers": [], "forbidden": set()}
    original_path = os.environ.get("PATH")
    original_phase = soak_campaign.run_recorded_phase
    original_campaign = soak.run_campaign
    original_factory = http.client.HTTPConnection
    original_umask = os.umask(0o077)
    gate = None

    def capture_campaign(*args, **kwargs):
        try:
            return original_campaign(*args, **kwargs)
        except BaseException as error:
            retained["campaign_error"] = error
            retained["campaign_traceback"] = error.__traceback__
            raise

    def hook(controlled, phase, *args, **kwargs):
        if phase != "fixed" or "env" in retained:
            raise ValueError("cancellation_phase_scope")
        report["warmup_ended_s"] = clock()
        env = controlled.env
        retained["env"] = env
        executors = inspect.getclosurevars(controlled.execute).nonlocals.get("executors")
        if not isinstance(executors, dict) or "fixed" not in executors:
            raise ValueError("cancellation_executor_binding")
        metadata = executors["fixed"]
        retained["forbidden"].update(metadata.mappings)
        retained["forbidden"].update(metadata.mappings.values())
        processes = read_processes()
        if len(env.children) != 2 or len({child.pid for child in env.children}) != 2:
            raise ValueError("cancellation_owned_service_population")
        identities = []
        for child in env.children:
            current = processes.get(child.pid)
            if current is None or child.poll() is not None or "Z" in current["state"]:
                raise ValueError("cancellation_owned_service_identity")
            identities.append({"role": "proxy" if child.pid == env.proxy.pid else "backend",
                               "pid": child.pid, "start": current["start"]})
        proxy = next(identity for identity in identities if identity["role"] == "proxy")
        retained["children"] = identities
        for index in range(3):
            driver = NativeTrialDriver(env, controlled.execute, trial_plans(index), gate, proxy)
            retained["drivers"].append(driver)
            trial = {}
            report["trials"].append(trial)
            run_trial(driver, trial)
        retained["forbidden"].update(metadata.forbidden_values)
        raise _DiagnosticComplete()

    try:
        with GatedOCR(paths["gate"], delegate) as gate:
            os.environ["PATH"] = str(gate.launcher.parent) + os.pathsep + (original_path or "")
            soak_campaign.run_recorded_phase = hook
            soak.run_campaign = capture_campaign
            arguments = []
            for flag in ("binary", "seed-binary", "fixtures", "build-manifest", "binary-source"):
                arguments.extend(("--" + flag, str(getattr(options, flag.replace("-", "_")))))
            arguments.extend(("--binary-commit", options.binary_commit, "--output", str(root),
                              "--expected-identity-output", str(paths["expected"])))
            with paths["log"].open("x", encoding="utf-8") as log:
                with contextlib.redirect_stdout(log), contextlib.redirect_stderr(log):
                    try:
                        soak.main(arguments)
                    except _DiagnosticComplete:
                        pass
                    else:
                        error = retained.get("campaign_error")
                        if not isinstance(error, _DiagnosticComplete):
                            if error is not None:
                                raise error.with_traceback(retained["campaign_traceback"])
                            raise ValueError("cancellation_diagnostic_not_executed")
                    if len(report["trials"]) != 3 or any(trial.get("completed") is not True for trial in report["trials"]):
                        raise ValueError("cancellation_trial_completion")
        soak_campaign.run_recorded_phase = original_phase
        soak.run_campaign = original_campaign
        if original_path is None:
            os.environ.pop("PATH", None)
        else:
            os.environ["PATH"] = original_path
        report["identity"], report["settings"], report["warmup"] = _collector(root)
        expected = read_report_bytes(_capture(paths["expected"]))
        _write(paths["partial"], report)
        started = clock()
        env = retained["env"]
        logs_closed = all(log.closed for log in env.logs)
        if any(child.poll() is None for child in env.children):
            raise ValueError("cancellation_service_cleanup")
        processes = read_processes()
        observed = clock()
        children = [dict(identity, state=_terminal(identity, processes), observed_s=observed)
                    for identity in retained["children"]]
        helpers = [{"helper": helper, "state": _terminal(helper, processes), "observed_s": observed}
                   for trial in report["trials"] for helper in trial["final_helpers"]]
        privacy_paths = [Path(log.name) for log in env.logs] + [Path(env.directory) / "audit.jsonl", paths["log"], paths["partial"]]
        privacy = verify_log_privacy(privacy_paths, retained["forbidden"])
        cleanup = {"logs_closed": logs_closed, "gate_released": not gate.armed,
                   "factory_restored": http.client.HTTPConnection is original_factory
                       and all(driver.factory_restored for driver in retained["drivers"]),
                   "threads_joined": sum(driver.workers_joined for driver in retained["drivers"]),
                   "owned_children": children, "helpers_terminal": helpers}
        report["finalization"] = {"started_s": started, "ended_s": clock(), "privacy": privacy, "cleanup": cleanup}
        report["completed"] = True
        evaluation = evaluate_cancellation(report, expected)
        _write(paths["report"], report)
        _write(paths["evaluation"], evaluation)
        return {"report_path": str(paths["report"]), "evaluation_path": str(paths["evaluation"]),
                "cancellation_qualified": True, "performance_qualified": False}
    except BaseException:
        report["completed"] = False
        _write(paths["failure"], report)
        with paths["failure_log"].open("x", encoding="utf-8") as log:
            traceback.print_exc(file=log)
        raise
    finally:
        soak_campaign.run_recorded_phase = original_phase
        soak.run_campaign = original_campaign
        if original_path is None:
            os.environ.pop("PATH", None)
        else:
            os.environ["PATH"] = original_path
        os.umask(original_umask)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Verify original-bound queued and active extraction cancellation")
    for name in ("binary", "seed-binary", "fixtures", "build-manifest", "binary-source", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--binary-commit", required=True)
    options = parser.parse_args(argv)
    try:
        result = run_probe(options)
    except Exception:
        print("cancellation diagnostic failed; private evidence retained")
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
